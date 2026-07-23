package oauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/derekvery666/dsmctl/internal/gateway/platformauth"
	"github.com/derekvery666/dsmctl/internal/gateway/state"
)

func TestDSMOnlyOAuthAuthorization(t *testing.T) {
	repository, err := state.OpenWithOptions(filepath.Join(t.TempDir(), "gateway.db"), make([]byte, 32), state.OpenOptions{
		PasswordHashParameters: &state.PasswordHashParameters{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	ctx := context.Background()
	if _, err := repository.CreateProfile(ctx, state.ProfileInput{Name: "office", URL: "https://10.0.0.20:5001", TLSMode: state.TLSSystemCA}); err != nil {
		t.Fatal(err)
	}
	client, err := repository.RegisterOAuthClient(ctx, state.OAuthClientInput{Name: "Codex", RedirectURIs: []string{"http://127.0.0.1:32123/callback"}})
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{8}, 32)
	signer, _ := platformauth.NewSigner(key, platformauth.DefaultAudience)
	verifier, _ := platformauth.NewVerifier(key, platformauth.DefaultAudience)
	handler, err := New(Options{Repository: repository, PlatformVerifier: verifier})
	if err != nil {
		t.Fatal(err)
	}

	pkceVerifier := strings.Repeat("v", 64)
	digest := sha256.Sum256([]byte(pkceVerifier))
	values := url.Values{
		"response_type": {"code"}, "client_id": {client.ID},
		"redirect_uri": {"http://127.0.0.1:32123/callback"}, "state": {"client-state"},
		"scope": {defaultScopeString}, "resource": {"http://127.0.0.1/mcp"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"},
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/oauth/authorize?"+values.Encode(), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Sign in with DSM Web Login") || strings.Contains(response.Body.String(), `name="username"`) {
		t.Fatalf("DSM-only authorization page = %d %s", response.Code, response.Body.String())
	}

	values.Set("auth_method", state.AdminProviderDSM)
	assertion, _ := signer.Sign("dsm-admin")
	request = httptest.NewRequest(http.MethodPost, "http://127.0.0.1/oauth/authorize", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "http://127.0.0.1")
	request.Header.Set(platformauth.HeaderName, assertion)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("DSM authorize = %d %s", response.Code, response.Body.String())
	}
	redirect, err := url.Parse(response.Header().Get("Location"))
	if err != nil || redirect.Query().Get("code") == "" || redirect.Query().Get("state") != "client-state" {
		t.Fatalf("DSM authorize redirect = %q, err=%v", response.Header().Get("Location"), err)
	}
}
