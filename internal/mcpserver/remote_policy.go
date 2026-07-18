package mcpserver

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
)

// NewRemote adds enforceable request policy to the complete MCP surface. New
// remains the local CLI/stdio server and is intentionally unaffected.
func NewRemote(service *application.Service, version string, auditor remotepolicy.Auditor) *mcp.Server {
	server := New(service, version)
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
		return remotepolicy.ScopeAdmin, true
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
			if callResult, ok := result.(*mcp.CallToolResult); ok && callResult.IsError {
				outcome = "failure"
			}
			auditRemote(ctx, auditor, principal, params.Name, nas, outcome, "")
			return result, err
		}
	}
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
