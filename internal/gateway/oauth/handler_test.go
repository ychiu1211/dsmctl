package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/gateway/state"
)

func TestMetadataIsPrefixAware(t *testing.T) {
	handler, _ := newOAuthTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/.well-known/oauth-protected-resource", nil)
	req.Host = "127.0.0.1"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "nas.example")
	req.Header.Set("X-Forwarded-Prefix", "/dsmctl")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("metadata status=%d body=%s", response.Code, response.Body.String())
	}
	var metadata map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["resource"] != "https://nas.example/dsmctl/mcp" {
		t.Fatalf("resource=%v", metadata["resource"])
	}
	servers, _ := metadata["authorization_servers"].([]any)
	if len(servers) != 1 || servers[0] != "https://nas.example/dsmctl/oauth" {
		t.Fatalf("authorization_servers=%#v", metadata["authorization_servers"])
	}
	if got := handler.ResourceMetadataURL(req); got != "https://nas.example/dsmctl/.well-known/oauth-protected-resource" {
		t.Fatalf("resource metadata URL=%q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/oauth/.well-known/openid-configuration", nil)
	req.Host = "127.0.0.1"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "nas.example")
	req.Header.Set("X-Forwarded-Prefix", "/dsmctl")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"authorization_endpoint":"https://nas.example/dsmctl/oauth/authorize"`) || !strings.Contains(response.Body.String(), `"code_challenge_methods_supported":["S256"]`) {
		t.Fatalf("authorization metadata status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDynamicRegistrationAndAuthorizationCodeFlow(t *testing.T) {
	handler, repository := newOAuthTestHandler(t)
	registration := `{"client_name":"Codex desktop","redirect_uris":["http://127.0.0.1:32123/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none","application_type":"native","client_uri":"https://client.example"}`
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/oauth/register", strings.NewReader(registration))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusCreated {
		t.Fatalf("registration status=%d body=%s", response.Code, response.Body.String())
	}
	var registered map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}
	clientID, _ := registered["client_id"].(string)
	if clientID == "" {
		t.Fatal("registration omitted client_id")
	}

	verifier := strings.Repeat("v", 64)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	authorizationValues := url.Values{
		"response_type": {"code"}, "client_id": {clientID},
		"redirect_uri": {"http://127.0.0.1:32123/callback"}, "state": {"client-state"},
		"scope": {defaultScopeString}, "resource": {"http://127.0.0.1/mcp"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/oauth/authorize?"+authorizationValues.Encode(), nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Codex desktop") || !strings.Contains(response.Body.String(), "office") || !strings.Contains(response.Body.String(), "nas.apply") {
		t.Fatalf("authorization page status=%d body=%s", response.Code, response.Body.String())
	}

	authorizationValues.Set("decision", "allow")
	authorizationValues.Set("username", "owner")
	authorizationValues.Set("password", "correct horse battery staple")
	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1/oauth/authorize", strings.NewReader(authorizationValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://127.0.0.1")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusFound {
		t.Fatalf("authorize status=%d body=%s", response.Code, response.Body.String())
	}
	redirect, err := url.Parse(response.Header().Get("Location"))
	if err != nil || redirect.Host != "127.0.0.1:32123" || redirect.Query().Get("state") != "client-state" {
		t.Fatalf("redirect=%q err=%v", response.Header().Get("Location"), err)
	}
	code := redirect.Query().Get("code")
	if code == "" {
		t.Fatal("authorization redirect omitted code")
	}

	tokenValues := url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID},
		"redirect_uri": {"http://127.0.0.1:32123/callback"}, "resource": {"http://127.0.0.1/mcp"},
		"code_verifier": {verifier},
	}
	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1/oauth/token", strings.NewReader(tokenValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("token status=%d body=%s", response.Code, response.Body.String())
	}
	var token map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	access, _ := token["access_token"].(string)
	refresh, _ := token["refresh_token"].(string)
	if access == "" || refresh == "" || token["token_type"] != "Bearer" {
		t.Fatalf("unexpected token response: %#v", token)
	}
	principal, err := repository.AuthenticateMCPToken(context.Background(), access)
	if err != nil || principal.TokenID == "" || len(principal.NAS) != 1 || !principal.AllowsNAS("office") {
		t.Fatalf("principal=%#v err=%v", principal, err)
	}

	// Authorization codes are single-use even when the first exchange succeeds.
	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1/oauth/token", strings.NewReader(tokenValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_grant") {
		t.Fatalf("reused code status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAuthorizationPostRejectsWrongOrigin(t *testing.T) {
	handler, repository := newOAuthTestHandler(t)
	client, err := repository.RegisterOAuthClient(context.Background(), state.OAuthClientInput{Name: "client", RedirectURIs: []string{"http://localhost:32123/callback"}})
	if err != nil {
		t.Fatal(err)
	}
	verifier := strings.Repeat("a", 64)
	digest := sha256.Sum256([]byte(verifier))
	values := url.Values{
		"response_type": {"code"}, "client_id": {client.ID}, "redirect_uri": {"http://localhost:32123/callback"},
		"resource": {"http://127.0.0.1/mcp"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"},
		"decision": {"allow"}, "username": {"owner"}, "password": {"correct horse battery staple"},
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/oauth/authorize", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden || response.Header().Get("Location") != "" {
		t.Fatalf("status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
}

func TestAuthorizationRejectsUnregisteredRedirectWithoutRedirecting(t *testing.T) {
	handler, repository := newOAuthTestHandler(t)
	client, err := repository.RegisterOAuthClient(context.Background(), state.OAuthClientInput{Name: "client", RedirectURIs: []string{"http://localhost:32123/callback"}})
	if err != nil {
		t.Fatal(err)
	}
	verifier := strings.Repeat("a", 64)
	digest := sha256.Sum256([]byte(verifier))
	values := url.Values{
		"response_type": {"code"}, "client_id": {client.ID}, "redirect_uri": {"https://evil.example/callback"},
		"resource": {"http://127.0.0.1/mcp"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/oauth/authorize?"+values.Encode(), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest || response.Header().Get("Location") != "" {
		t.Fatalf("status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
}

func newOAuthTestHandler(t *testing.T) (*Handler, *state.Repository) {
	t.Helper()
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
	handler, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatal(err)
	}
	return handler, repository
}
