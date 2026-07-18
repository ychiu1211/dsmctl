package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/mcpserver"
	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

type memoryAuthenticator struct {
	mu         sync.Mutex
	principals map[string]remotepolicy.Principal
}

func (a *memoryAuthenticator) AuthenticateMCPToken(_ context.Context, token string) (remotepolicy.Principal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	principal, ok := a.principals[token]
	if !ok {
		return remotepolicy.Principal{}, errors.New("invalid")
	}
	return principal, nil
}

type memoryAuditor struct {
	mu     sync.Mutex
	events []remotepolicy.AuditEvent
}

func (a *memoryAuditor) AppendAudit(_ context.Context, event remotepolicy.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
	return nil
}

func TestManagedMCPAuthenticatesBeforeInitializeFiltersToolsAndNAS(t *testing.T) {
	cfg := config.New()
	cfg.DefaultNAS = "hidden"
	cfg.NAS["allowed"] = config.Profile{URL: "https://allowed.invalid", Revision: 1}
	cfg.NAS["hidden"] = config.Profile{URL: "https://hidden.invalid", Revision: 2}
	manager := runtime.NewManager(cfg, credentials.NewEnvironment())
	service := application.NewService(cfg, manager)
	t.Cleanup(func() { _ = service.Close(context.Background()) })
	reader := remotepolicy.Principal{TokenID: "reader-id", Name: "reader", Scopes: map[string]struct{}{remotepolicy.ScopeRead: {}}, NAS: map[string]struct{}{"allowed": {}}}
	planner := remotepolicy.Principal{TokenID: "planner-id", Name: "planner", Scopes: map[string]struct{}{remotepolicy.ScopePlan: {}}, NAS: map[string]struct{}{"allowed": {}}}
	authenticator := &memoryAuthenticator{principals: map[string]remotepolicy.Principal{"reader-token": reader, "planner-token": planner}}
	auditor := &memoryAuditor{}
	server, err := New(Options{MCPServer: mcpserver.NewRemote(service, "test", auditor), MCPAuthenticator: authenticator, MCPAuditor: auditor, AllowedHosts: []string{"127.0.0.1"}})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	request, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing bearer status = %d", response.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	readerSession := connectManagedClient(t, ctx, httpServer.URL, "reader-token")
	defer readerSession.Close()
	tools, err := readerSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		scope, known := mcpserver.ToolScope(tool.Name)
		if !known || scope != remotepolicy.ScopeRead {
			t.Fatalf("read-only token saw tool %q scope %q", tool.Name, scope)
		}
	}
	result, err := readerSession.CallTool(ctx, &mcp.CallToolParams{Name: "list_nas", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if !strings.Contains(string(encoded), "allowed") || strings.Contains(string(encoded), "hidden") || strings.Contains(string(encoded), "hidden.invalid") {
		t.Fatalf("filtered list_nas = %s", encoded)
	}
	result, err = readerSession.CallTool(ctx, &mcp.CallToolParams{Name: "plan_storage_change", Arguments: map[string]any{"nas": "allowed"}})
	if err == nil && !result.IsError {
		t.Fatal("read-only token called plan tool")
	}
	if strings.Contains(strings.ToLower(errString(err)), "hidden") {
		t.Fatalf("denial leaked hidden profile: %v", err)
	}

	plannerSession := connectManagedClient(t, ctx, httpServer.URL, "planner-token")
	defer plannerSession.Close()
	tools, err = plannerSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("plan-only token saw no plan tools")
	}
	for _, tool := range tools.Tools {
		scope, _ := mcpserver.ToolScope(tool.Name)
		if scope != remotepolicy.ScopePlan {
			t.Fatalf("plan-only token saw %q scope %q", tool.Name, scope)
		}
	}
}

func TestIdentityRateLimiterIsPerPrincipal(t *testing.T) {
	limiter := newIdentityLimiter()
	now := time.Now()
	for index := 0; index < 120; index++ {
		if !limiter.Allow("one", now) {
			t.Fatalf("request %d was unexpectedly limited", index)
		}
	}
	if limiter.Allow("one", now) {
		t.Fatal("121st request was allowed")
	}
	if !limiter.Allow("two", now) {
		t.Fatal("one identity exhausted another identity's quota")
	}
	if !limiter.Allow("one", now.Add(time.Minute)) {
		t.Fatal("identity limit did not reset")
	}
}

func connectManagedClient(t *testing.T, ctx context.Context, baseURL, token string) *mcp.ClientSession {
	t.Helper()
	httpClient := &http.Client{Transport: authorizationTransport{token: token, next: http.DefaultTransport}}
	transport := &mcp.StreamableClientTransport{Endpoint: baseURL + "/mcp", HTTPClient: httpClient, DisableStandaloneSSE: true, MaxRetries: -1}
	client := mcp.NewClient(&mcp.Implementation{Name: "managed-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	return session
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
