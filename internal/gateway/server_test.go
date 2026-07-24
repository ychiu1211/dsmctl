package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/derekvery666/dsmctl/internal/application"
	"github.com/derekvery666/dsmctl/internal/config"
	"github.com/derekvery666/dsmctl/internal/mcpserver"
	"github.com/derekvery666/dsmctl/internal/remotepolicy"
	"github.com/derekvery666/dsmctl/internal/runtime"
)

const testToken = "0123456789abcdef0123456789abcdef"

func TestStreamableHTTPListsNASInReadOnlyMode(t *testing.T) {
	dsmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if err := req.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Form.Get("api") + "." + req.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7},"SYNO.Core.System":{"path":"entry.cgi","minVersion":1,"maxVersion":3}}}`)
		case "SYNO.API.Auth.login":
			fmt.Fprint(w, `{"success":true,"data":{"sid":"test-sid","synotoken":"test-token"}}`)
		case "SYNO.Core.System.info":
			fmt.Fprint(w, `{"success":true,"data":{"hostname":"lab-nas","model":"DS-test","firmware_ver":"DSM 7.2","ram_size":4096}}`)
		case "SYNO.API.Auth.logout":
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		default:
			t.Errorf("unexpected DSM call %s.%s", req.Form.Get("api"), req.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	t.Cleanup(dsmServer.Close)
	t.Setenv("DSMCTL_PASSWORD_LAB", "secret")

	cfg := config.New()
	cfg.DefaultNAS = "lab"
	cfg.NAS["lab"] = config.Profile{URL: dsmServer.URL, Username: "operator"}
	secrets := NewEnvironmentCredentials()
	manager := runtime.NewManager(cfg, secrets)
	service := application.NewService(cfg, manager, application.WithCredentialStore(secrets))
	t.Cleanup(func() { _ = service.Close(context.Background()) })

	gatewayServer, err := New(Options{
		MCPServer:    mcpserver.NewReadOnly(service, "test"),
		BearerToken:  testToken,
		AllowedHosts: []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	httpServer := httptest.NewServer(gatewayServer.Handler())
	defer httpServer.Close()

	httpClient := &http.Client{Transport: authorizationTransport{token: testToken, next: http.DefaultTransport}}
	transport := &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL + "/mcp",
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "gateway-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range tools.Tools {
		if strings.HasPrefix(tool.Name, "plan_") || strings.HasPrefix(tool.Name, "apply_") {
			t.Errorf("developer gateway exposed %q", tool.Name)
		}
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_nas", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool(list_nas) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(list_nas) returned tool error: %#v", result.Content)
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_system_info", Arguments: map[string]any{"nas": "lab"}})
	if err != nil {
		t.Fatalf("CallTool(get_system_info) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(get_system_info) returned tool error: %#v", result.Content)
	}
}

func TestMCPSessionTokenBinderRejectsCrossTokenReuse(t *testing.T) {
	binder := newMCPSessionTokenBinder()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	binder.Bind("session-1", "token-a", now)
	if got := binder.Authorize("session-1", "token-a", now.Add(time.Second)); got != mcpSessionAuthorized {
		t.Fatalf("session owner authorization = %v", got)
	}
	if got := binder.Authorize("session-1", "token-b", now.Add(2*time.Second)); got != mcpSessionTokenMismatch {
		t.Fatalf("cross-token authorization = %v", got)
	}
	if got := binder.Authorize("unknown", "token-a", now.Add(3*time.Second)); got != mcpSessionMissing {
		t.Fatalf("unknown session authorization = %v", got)
	}
	binder.Delete("session-1")
	if got := binder.Authorize("session-1", "token-a", now.Add(4*time.Second)); got != mcpSessionMissing {
		t.Fatalf("deleted session authorization = %v", got)
	}
}

func TestHTTPBoundaryReportsMissingMCPSessionAsNotFound(t *testing.T) {
	authenticator := &memoryAuthenticator{principals: map[string]remotepolicy.Principal{
		testToken: {TokenID: "test-token-id"},
	}}
	server := newBoundaryTestServer(t, Options{MCPAuthenticator: authenticator})
	request := httptest.NewRequest(http.MethodPost, "http://gateway.example.test/mcp", strings.NewReader(`{}`))
	request.Host = "gateway.example.test"
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Mcp-Session-Id", "session-from-before-restart")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"session_not_found"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestHTTPBoundaryRejectsLiveMCPSessionCrossTokenReuse(t *testing.T) {
	authenticator := &memoryAuthenticator{principals: map[string]remotepolicy.Principal{
		"token-a": {TokenID: "token-a-id"},
		"token-b": {TokenID: "token-b-id"},
	}}
	sessions := newMCPSessionTokenBinder()
	sessions.Bind("live-session", "token-a-id", time.Now())
	handler := authenticateMCP(
		authenticator,
		nil,
		nil,
		newIdentityLimiter(),
		sessions,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("cross-token request reached the MCP handler")
		}),
	)
	request := httptest.NewRequest(http.MethodPost, "http://gateway.example.test/mcp", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer token-b")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Mcp-Session-Id", "live-session")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"session_token_mismatch"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestHTTPBoundaryRejectsInvalidRequests(t *testing.T) {
	server := newBoundaryTestServer(t, Options{
		AllowedOrigins: []string{"https://console.example.test"},
		MaxBodyBytes:   64,
	})
	tests := []struct {
		name        string
		host        string
		origin      string
		token       string
		contentType string
		body        string
		wantStatus  int
	}{
		{name: "host", host: "evil.example.test", token: testToken, contentType: "application/json", body: `{}`, wantStatus: http.StatusForbidden},
		{name: "origin", host: "gateway.example.test", origin: "https://evil.example.test", token: testToken, contentType: "application/json", body: `{}`, wantStatus: http.StatusForbidden},
		{name: "authentication", host: "gateway.example.test", contentType: "application/json", body: `{}`, wantStatus: http.StatusUnauthorized},
		{name: "content type", host: "gateway.example.test", token: testToken, contentType: "text/plain", body: `{}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "body", host: "gateway.example.test", token: testToken, contentType: "application/json", body: strings.Repeat("x", 65), wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://gateway.example.test/mcp", strings.NewReader(test.body))
			req.Host = test.host
			if test.name == "body" {
				req.ContentLength = -1 // Exercise the streaming limit, not only the declared length check.
			}
			req.Header.Set("Content-Type", test.contentType)
			if test.origin != "" {
				req.Header.Set("Origin", test.origin)
			}
			if test.token != "" {
				req.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, req)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			if response.Header().Get("X-Request-ID") == "" {
				t.Fatal("X-Request-ID was not set")
			}
		})
	}
}

func TestHTTPBoundaryDelegatesDynamicAdminOriginOnlyWhenNoGlobalOriginIsConfigured(t *testing.T) {
	var reached atomic.Bool
	server := newBoundaryTestServer(t, Options{AdminHandler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		reached.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})})
	request := httptest.NewRequest(http.MethodPost, "http://gateway.example.test/admin/api/login", strings.NewReader(`{}`))
	request.Host = "gateway.example.test"
	request.Header.Set("Origin", "https://dynamic-dsm.example.test")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !reached.Load() {
		t.Fatalf("dynamic admin origin status = %d, reached=%v", response.Code, reached.Load())
	}

	mcpRequest := httptest.NewRequest(http.MethodPost, "http://gateway.example.test/mcp", strings.NewReader(`{}`))
	mcpRequest.Host = "gateway.example.test"
	mcpRequest.Header.Set("Origin", "https://dynamic-dsm.example.test")
	mcpResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(mcpResponse, mcpRequest)
	if mcpResponse.Code != http.StatusForbidden {
		t.Fatalf("dynamic MCP origin status = %d", mcpResponse.Code)
	}
}

func TestHealthAndReadinessAreLocal(t *testing.T) {
	var readyCalls atomic.Int32
	notReady := atomic.Bool{}
	server := newBoundaryTestServer(t, Options{
		Ready: func(context.Context) error {
			readyCalls.Add(1)
			if notReady.Load() {
				return errors.New("local secret unavailable")
			}
			return nil
		},
	})

	request := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://gateway.example.test"+path, nil)
		req.Host = "gateway.example.test"
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, req)
		return response
	}
	if response := request("/healthz"); response.Code != http.StatusOK {
		t.Fatalf("health status = %d", response.Code)
	}
	if readyCalls.Load() != 0 {
		t.Fatal("liveness called readiness dependency")
	}
	if response := request("/readyz"); response.Code != http.StatusOK {
		t.Fatalf("ready status = %d", response.Code)
	}
	notReady.Store(true)
	if response := request("/readyz"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("not-ready status = %d", response.Code)
	}
}

func TestAdminRootRedirectPreservesTrustedProxyPrefix(t *testing.T) {
	server := newBoundaryTestServer(t, Options{AdminHandler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})})

	tests := []struct {
		name       string
		method     string
		path       string
		prefix     string
		wantStatus int
		wantURL    string
	}{
		{name: "direct", method: http.MethodGet, path: "/", wantStatus: http.StatusTemporaryRedirect, wantURL: "/admin/"},
		{name: "reverse proxy", method: http.MethodGet, path: "/", prefix: "/dsmctl", wantStatus: http.StatusTemporaryRedirect, wantURL: "/dsmctl/admin/"},
		{name: "unsafe prefix", method: http.MethodGet, path: "/", prefix: "/dsmctl?next=evil", wantStatus: http.StatusTemporaryRedirect, wantURL: "/admin/"},
		{name: "unknown path", method: http.MethodGet, path: "/missing", wantStatus: http.StatusNotFound},
		{name: "method", method: http.MethodPost, path: "/", wantStatus: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(test.method, "http://gateway.example.test"+test.path, nil)
			req.Host = "gateway.example.test"
			req.Header.Set("X-Forwarded-Prefix", test.prefix)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, req)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if location := response.Header().Get("Location"); location != test.wantURL {
				t.Fatalf("Location = %q, want %q", location, test.wantURL)
			}
		})
	}
}

func TestConcurrencyLimitRejectsWithoutWaiting(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	handler := limitConcurrent(make(chan struct{}, 1), downstream)

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil))
	}()
	<-started
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	close(release)
	<-firstDone
}

func TestServeDrainsRequestBeforeClosingSessions(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	closed := make(chan struct{})
	server := newBoundaryTestServer(t, Options{
		Ready: func(context.Context) error {
			close(requestStarted)
			<-releaseRequest
			return nil
		},
		Close: func(context.Context) error {
			close(closed)
			return nil
		},
		ShutdownTimeout: 2 * time.Second,
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(ctx, listener) }()
	requestDone := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String() + "/readyz")
		if response != nil {
			_ = response.Body.Close()
		}
		requestDone <- err
	}()
	<-requestStarted
	cancel()
	select {
	case <-closed:
		t.Fatal("sessions closed before in-flight request drained")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseRequest)
	if err := <-requestDone; err != nil {
		t.Fatalf("request error = %v", err)
	}
	if err := <-serveDone; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	select {
	case <-closed:
	default:
		t.Fatal("sessions were not closed after drain")
	}
}

func newBoundaryTestServer(t *testing.T, overrides Options) *Server {
	t.Helper()
	options := overrides
	options.MCPServer = mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	options.BearerToken = testToken
	if len(options.AllowedHosts) == 0 {
		options.AllowedHosts = []string{"gateway.example.test", "127.0.0.1"}
	}
	options.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	server, err := New(options)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return server
}

type authorizationTransport struct {
	token string
	next  http.RoundTripper
}

func (t authorizationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.next.RoundTrip(clone)
}
