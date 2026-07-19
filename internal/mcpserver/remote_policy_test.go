package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	gatewaystate "github.com/ychiu1211/dsmctl/internal/gateway/state"
	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
)

type approvalRecordingAuditor struct {
	requests []remotepolicy.PendingApprovalRequest
}

func (*approvalRecordingAuditor) AppendAudit(context.Context, remotepolicy.AuditEvent) error {
	return nil
}

func (a *approvalRecordingAuditor) RecordPendingApproval(_ context.Context, request remotepolicy.PendingApprovalRequest) error {
	a.requests = append(a.requests, request)
	return nil
}

func TestHighRiskStructuredPlanRecordsClosedPendingApproval(t *testing.T) {
	auditor := &approvalRecordingAuditor{}
	principal := remotepolicy.Principal{TokenID: "token-1"}
	result := &mcp.CallToolResult{StructuredContent: json.RawMessage(`{"plan":{"nas":"office","profile_revision":42,"hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","risk":"high","summary":["delete pool","verify topology"],"references":{"resource_id":"pool-7"},"observed":{"password":"secret-canary-must-not-persist"}}}`)}
	recordPendingApproval(context.Background(), auditor, principal, "plan_storage_change", result)
	if len(auditor.requests) != 1 {
		t.Fatalf("pending requests = %#v", auditor.requests)
	}
	request := auditor.requests[0]
	if request.RequestingTokenID != "token-1" || request.NAS != "office" || request.ProfileRevision != 42 || request.ResourceID != "pool-7" || request.Summary != "delete pool; verify topology" {
		t.Fatalf("pending request = %#v", request)
	}
	encoded, _ := json.Marshal(request)
	for _, forbidden := range []string{"observed", "password", "synotoken", "ciphertext", "secret-canary-must-not-persist"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("pending request contains forbidden plan material %q: %s", forbidden, encoded)
		}
	}
}

func TestStructuredPlanSecretCanaryNeverReachesPersistedApprovalState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.db")
	repository, err := gatewaystate.Open(path, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	canary := "secret-canary-must-not-persist"
	result := &mcp.CallToolResult{StructuredContent: json.RawMessage(`{"plan":{"nas":"office","profile_revision":42,"hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","risk":"high","summary":["delete pool"],"observed":{"password":"` + canary + `","sid":"` + canary + `"}}}`)}
	recordPendingApproval(context.Background(), repository, remotepolicy.Principal{TokenID: "token-1"}, "plan_storage_change", result)
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(persisted, []byte(canary)) {
		t.Fatal("structured plan secret canary reached persisted approval state")
	}
}

func TestEveryRemoteTargetedToolRejectsOmittedNAS(t *testing.T) {
	service := &application.Service{}
	server := New(service, "test")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "target-test", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	principal := remotepolicy.Principal{TokenID: "all-scopes", Scopes: map[string]struct{}{remotepolicy.ScopeRead: {}, remotepolicy.ScopePlan: {}, remotepolicy.ScopeApply: {}, remotepolicy.ScopeLANDiscover: {}}, NAS: map[string]struct{}{}}
	remoteContext := remotepolicy.WithPrincipal(context.Background(), principal)
	for _, tool := range tools.Tools {
		if strings.Contains(tool.Name, "approval") {
			t.Fatalf("MCP exposes administrator-only approval state through %q", tool.Name)
		}
		called := false
		next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
			called = true
			return &mcp.CallToolResult{}, nil
		}
		handler := remotePolicyMiddleware(service, nil)(next)
		request := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: tool.Name, Arguments: json.RawMessage(`{}`)}}
		_, err := handler(remoteContext, "tools/call", request)
		targetless := tool.Name == "list_nas" || tool.Name == "discover_lan_devices" || tool.Name == "get_auth_status"
		if targetless {
			if err != nil || !called {
				t.Errorf("targetless tool %q err=%v called=%v", tool.Name, err, called)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "explicit nas") || called {
			t.Errorf("targeted tool %q err=%v called=%v", tool.Name, err, called)
		}
	}
}
