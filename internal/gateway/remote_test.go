package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	gatewayoauth "github.com/ychiu1211/dsmctl/internal/gateway/oauth"
	"github.com/ychiu1211/dsmctl/internal/gateway/state"
	"github.com/ychiu1211/dsmctl/internal/mcpserver"
	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

type memoryAuthenticator struct {
	mu         sync.Mutex
	principals map[string]remotepolicy.Principal
}

func TestManagedMCPCompletesOfficialOAuthURLLogin(t *testing.T) {
	repository, err := state.OpenWithOptions(filepath.Join(t.TempDir(), "gateway.db"), make([]byte, 32), state.OpenOptions{
		PasswordHashParameters: &state.PasswordHashParameters{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	ctx := context.Background()
	if _, _, err := repository.CreateAdministrator(ctx, "owner", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateProfile(ctx, state.ProfileInput{Name: "office", URL: "https://10.0.0.20:5001", TLSMode: state.TLSSystemCA}); err != nil {
		t.Fatal(err)
	}
	oauthProvider, err := gatewayoauth.New(gatewayoauth.Options{Repository: repository})
	if err != nil {
		t.Fatal(err)
	}
	gatewayServer, err := New(Options{
		MCPServer:        mcp.NewServer(&mcp.Implementation{Name: "oauth-test", Version: "test"}, nil),
		MCPAuthenticator: repository, MCPAuditor: repository, OAuthProvider: oauthProvider,
		AllowedHosts: []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(gatewayServer.Handler())
	defer httpServer.Close()

	oauthHTTPClient := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	redirectURI := "http://127.0.0.1:32123/callback"
	authorizationHandler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName: "Official Go SDK test", RedirectURIs: []string{redirectURI},
			GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"}, TokenEndpointAuthMethod: "none",
		}},
		RedirectURL: redirectURI,
		Client:      oauthHTTPClient,
		AuthorizationCodeFetcher: func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			authorizationURL, err := url.Parse(args.URL)
			if err != nil {
				return nil, err
			}
			pageRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, authorizationURL.String(), nil)
			pageResponse, err := oauthHTTPClient.Do(pageRequest)
			if err != nil {
				return nil, err
			}
			_ = pageResponse.Body.Close()
			if pageResponse.StatusCode != http.StatusOK {
				return nil, errors.New("authorization page did not open")
			}
			form := authorizationURL.Query()
			form.Set("decision", "allow")
			form.Set("username", "owner")
			form.Set("password", "correct horse battery staple")
			postRequest, _ := http.NewRequestWithContext(ctx, http.MethodPost, authorizationURL.Scheme+"://"+authorizationURL.Host+authorizationURL.Path, strings.NewReader(form.Encode()))
			postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			postRequest.Header.Set("Origin", httpServer.URL)
			postResponse, err := oauthHTTPClient.Do(postRequest)
			if err != nil {
				return nil, err
			}
			_ = postResponse.Body.Close()
			if postResponse.StatusCode != http.StatusFound {
				return nil, errors.New("administrator authorization failed")
			}
			callback, err := url.Parse(postResponse.Header.Get("Location"))
			if err != nil {
				return nil, err
			}
			return &auth.AuthorizationResult{Code: callback.Query().Get("code"), State: callback.Query().Get("state")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "oauth-client", Version: "test"}, nil)
	clientCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.Connect(clientCtx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL + "/mcp", OAuthHandler: authorizationHandler,
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("OAuth MCP connect: %v", err)
	}
	defer session.Close()
	tokens, err := repository.MCPTokens(ctx)
	if err != nil || len(tokens) != 1 || tokens[0].AuthMode != "oauth" || tokens[0].LastUsedAt == nil {
		t.Fatalf("OAuth credential was not used by MCP initialize: tokens=%#v err=%v", tokens, err)
	}
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

type memoryOAuthProvider struct{}

func (*memoryOAuthProvider) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"resource":"https://gateway.example/mcp"}`))
}

func (*memoryOAuthProvider) ResourceMetadataURL(*http.Request) string {
	return "https://gateway.example/.well-known/oauth-protected-resource"
}

func (*memoryOAuthProvider) ScopeChallenge() string {
	return "nas.read nas.plan nas.apply lan.discover"
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
	server, err := New(Options{MCPServer: mcpserver.NewRemote(service, "test", auditor), MCPAuthenticator: authenticator, MCPAuditor: auditor, OAuthProvider: &memoryOAuthProvider{}, AllowedHosts: []string{"127.0.0.1"}})
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
	challenge := response.Header.Get("WWW-Authenticate")
	if !strings.Contains(challenge, `resource_metadata="https://gateway.example/.well-known/oauth-protected-resource"`) || !strings.Contains(challenge, `scope="nas.read nas.plan nas.apply lan.discover"`) {
		t.Fatalf("missing OAuth challenge metadata: %q", challenge)
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
