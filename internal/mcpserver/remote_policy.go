package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
)

// NewRemote adds enforceable request policy to the complete MCP surface. New
// remains the local CLI/stdio server and is intentionally unaffected.
func NewRemote(service *application.Service, version string, auditor remotepolicy.Auditor) *mcp.Server {
	server := New(service, version)
	// get_certificate_export is named like a read but writes a certificate
	// archive containing the PRIVATE KEY to the gateway HOST's filesystem at a
	// caller-controlled path. The prefix-based ToolScope classifies any get_ tool
	// as ScopeRead, so without this it would be reachable by a nas.read-only
	// remote token. Exporting a private key to the gateway host is meaningless for
	// a remote caller, so it is stripped from the remote surface entirely — the
	// same posture NewReadOnly takes.
	server.RemoveTools("get_certificate_export")
	server.AddReceivingMiddleware(remotePolicyMiddleware(service, auditor))
	return server
}

// ToolScope is the authorization table for the MCP surface. Prefix-based
// grouping is deliberate and tested against every registered tool: planning
// and applying are independent scopes even when a plan tool is read-only at
// the DSM protocol level.
func ToolScope(name string) (string, bool) {
	switch {
	case name == "discover_lan_devices":
		return remotepolicy.ScopeLANDiscover, true
	case name == "run_security_scan":
		// A load-heavy NAS action with no plan/apply cycle. It mutates no
		// configuration but must not be reachable by a read-only token, so it is
		// classified under the apply scope alongside the other write actions.
		return remotepolicy.ScopeApply, true
	case strings.HasPrefix(name, "plan_"):
		return remotepolicy.ScopePlan, true
	case strings.HasPrefix(name, "apply_"):
		return remotepolicy.ScopeApply, true
	case name == "list_nas", strings.HasPrefix(name, "get_"), strings.HasPrefix(name, "explain_"):
		return remotepolicy.ScopeRead, true
	default:
		return "", false
	}
}

type remotePlanResult struct {
	Plan struct {
		NAS             string   `json:"nas"`
		ProfileRevision uint64   `json:"profile_revision"`
		Hash            string   `json:"hash"`
		Risk            string   `json:"risk"`
		Summary         []string `json:"summary"`
		References      struct {
			ResourceID string `json:"resource_id"`
		} `json:"references"`
	} `json:"plan"`
}

type remoteToolTarget struct {
	NAS  string `json:"nas"`
	Plan struct {
		NAS             string `json:"nas"`
		ProfileRevision uint64 `json:"profile_revision"`
		Hash            string `json:"hash"`
		Risk            string `json:"risk"`
	} `json:"plan"`
}

func remotePolicyMiddleware(service *application.Service, auditor remotepolicy.Auditor) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			principal, remote := remotepolicy.PrincipalFromContext(ctx)
			if !remote {
				return nil, remotepolicy.ErrDenied
			}
			if method == "tools/list" {
				result, err := next(ctx, method, request)
				if err != nil {
					return result, err
				}
				listed, ok := result.(*mcp.ListToolsResult)
				if !ok {
					return result, nil
				}
				allowed := listed.Tools[:0]
				for _, tool := range listed.Tools {
					scope, known := ToolScope(tool.Name)
					if known && principal.HasScope(scope) {
						allowed = append(allowed, tool)
					}
				}
				listed.Tools = allowed
				return result, nil
			}
			if method != "tools/call" {
				return next(ctx, method, request)
			}
			params, ok := request.GetParams().(*mcp.CallToolParamsRaw)
			if !ok {
				return nil, remotepolicy.ErrDenied
			}
			scope, known := ToolScope(params.Name)
			if !known || !principal.HasScope(scope) {
				auditRemote(ctx, auditor, principal, params.Name, "", "denied", "denied")
				return nil, remotepolicy.ErrDenied
			}
			var target remoteToolTarget
			if len(params.Arguments) > 0 && string(params.Arguments) != "null" {
				if err := json.Unmarshal(params.Arguments, &target); err != nil {
					return next(ctx, method, request)
				}
			}
			nas := target.NAS
			if strings.HasPrefix(params.Name, "apply_") {
				nas = target.Plan.NAS
			}
			needsTarget := params.Name != "list_nas" && params.Name != "discover_lan_devices" && !(params.Name == "get_auth_status" && nas == "")
			if needsTarget {
				if strings.TrimSpace(nas) == "" {
					auditRemote(ctx, auditor, principal, params.Name, "", "denied", "denied")
					return nil, fmt.Errorf("remote MCP tool %q requires an explicit nas argument", params.Name)
				}
				resolved, err := service.AuthorizeRemoteTarget(ctx, nas)
				if err != nil {
					auditRemote(ctx, auditor, principal, params.Name, "", "denied", "denied")
					return nil, remotepolicy.ErrDenied
				}
				nas = resolved
			} else if nas != "" && !principal.AllowsNAS(nas) {
				auditRemote(ctx, auditor, principal, params.Name, "", "denied", "denied")
				return nil, remotepolicy.ErrDenied
			}
			result, err := next(ctx, method, request)
			outcome := "success"
			if err != nil {
				outcome = "failure"
			}
			// A dispatch to an unknown/removed tool returns a typed-nil
			// *mcp.CallToolResult boxed in the result interface (ok is true,
			// callResult is nil), so the nil check is required before IsError —
			// without it a remote tools/call for any unregistered name panics
			// this middleware, crashing the gateway process.
			if callResult, ok := result.(*mcp.CallToolResult); ok && callResult != nil && callResult.IsError {
				outcome = "failure"
			}
			if err == nil && outcome == "success" && strings.HasPrefix(params.Name, "plan_") {
				recordPendingApproval(ctx, auditor, principal, params.Name, result)
			}
			auditRemote(ctx, auditor, principal, params.Name, nas, outcome, "")
			return result, err
		}
	}
}

func recordPendingApproval(ctx context.Context, target any, principal remotepolicy.Principal, tool string, result mcp.Result) {
	recorder, ok := target.(remotepolicy.ApprovalRequestRecorder)
	if !ok {
		return
	}
	callResult, ok := result.(*mcp.CallToolResult)
	if !ok || callResult == nil || callResult.IsError || callResult.StructuredContent == nil {
		return
	}
	encoded, err := json.Marshal(callResult.StructuredContent)
	if raw, ok := callResult.StructuredContent.(json.RawMessage); ok {
		encoded = raw
		err = nil
	}
	if err != nil {
		return
	}
	var value remotePlanResult
	if json.Unmarshal(encoded, &value) != nil || !strings.EqualFold(value.Plan.Risk, "high") || value.Plan.ProfileRevision == 0 {
		return
	}
	_ = recorder.RecordPendingApproval(ctx, remotepolicy.PendingApprovalRequest{
		PlanHash: value.Plan.Hash, NAS: value.Plan.NAS, ProfileRevision: value.Plan.ProfileRevision,
		RequestingTokenID: principal.TokenID, Tool: tool, Risk: value.Plan.Risk,
		ResourceID: value.Plan.References.ResourceID, Summary: strings.Join(value.Plan.Summary, "; "),
	})
}

func auditRemote(ctx context.Context, auditor remotepolicy.Auditor, principal remotepolicy.Principal, tool, nas, outcome, reason string) {
	if auditor == nil {
		return
	}
	_ = auditor.AppendAudit(ctx, remotepolicy.AuditEvent{
		CorrelationID: remotepolicy.CorrelationID(ctx), ActorType: "mcp_token", ActorID: principal.TokenID,
		Action: "mcp.tool", Tool: tool, NAS: nas, Outcome: outcome, Reason: reason,
	})
}
