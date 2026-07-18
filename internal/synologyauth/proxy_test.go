package synologyauth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/gateway/platformauth"
)

type validatorFunc func(*http.Request) (string, error)

func (f validatorFunc) Validate(req *http.Request) (string, error) { return f(req) }

func TestProxyAuthenticatesOnlyAdminRoutesAndReplacesAssertion(t *testing.T) {
	key := bytes.Repeat([]byte{3}, 32)
	signer, _ := platformauth.NewSigner(key, platformauth.DefaultAudience)
	verifier, _ := platformauth.NewVerifier(key, platformauth.DefaultAudience)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/admin/api/status" {
			identity, err := verifier.Verify(context.Background(), req.Header.Get(platformauth.HeaderName))
			if err != nil || identity.Subject != "dsm-admin" {
				http.Error(w, "bad assertion", http.StatusUnauthorized)
				return
			}
		}
		_, _ = io.WriteString(w, req.URL.Path)
	}))
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)
	handler, err := New(Options{Backend: backendURL, Signer: signer, Validator: validatorFunc(func(*http.Request) (string, error) { return "dsm-admin", nil })})
	if err != nil {
		t.Fatal(err)
	}
	adminRequest := httptest.NewRequest(http.MethodGet, "/admin/api/status", nil)
	adminRequest.Header.Set(platformauth.HeaderName, "forged")
	adminResponse := httptest.NewRecorder()
	handler.ServeHTTP(adminResponse, adminRequest)
	if adminResponse.Code != http.StatusOK {
		t.Fatalf("admin response = %d %s", adminResponse.Code, adminResponse.Body.String())
	}
	mcpRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	mcpRequest.Header.Set(platformauth.HeaderName, "forged")
	mcpResponse := httptest.NewRecorder()
	handler.ServeHTTP(mcpResponse, mcpRequest)
	if mcpResponse.Code != http.StatusOK {
		t.Fatalf("MCP response = %d", mcpResponse.Code)
	}
}

func TestProxyFailsClosedForNonAdminAndNonLoopback(t *testing.T) {
	key := bytes.Repeat([]byte{3}, 32)
	signer, _ := platformauth.NewSigner(key, platformauth.DefaultAudience)
	backendURL, _ := url.Parse("http://127.0.0.1:1")
	handler, _ := New(Options{Backend: backendURL, Signer: signer, RequireLoopback: true, Validator: validatorFunc(func(*http.Request) (string, error) { return "", errors.New("not admin") })})
	nonLoopback := httptest.NewRequest(http.MethodGet, "/admin", nil)
	nonLoopback.RemoteAddr = "192.0.2.1:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, nonLoopback)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-loopback status = %d", response.Code)
	}
	loopback := httptest.NewRequest(http.MethodGet, "/admin", nil)
	loopback.RemoteAddr = "127.0.0.1:1234"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, loopback)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("non-admin status = %d", response.Code)
	}
}

func TestProxyStripsPortalPrefixAndRejectsCrossOrigin(t *testing.T) {
	key := bytes.Repeat([]byte{3}, 32)
	signer, _ := platformauth.NewSigner(key, platformauth.DefaultAudience)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/admin/api/status" || req.Header.Get("X-Forwarded-Prefix") != "/dsmctl" {
			http.Error(w, "bad forwarding", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)
	handler, _ := New(Options{Backend: backendURL, Signer: signer, Validator: validatorFunc(func(*http.Request) (string, error) { return "admin", nil })})
	request := httptest.NewRequest(http.MethodGet, "/dsmctl/admin/api/status", nil)
	request.Header.Set("X-Forwarded-Prefix", "/dsmctl")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "nas.example:5001")
	request.Header.Set("Origin", "https://nas.example:5001")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("portal response = %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/dsmctl/admin/api/profiles", nil)
	request.Header.Set("X-Forwarded-Prefix", "/dsmctl")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "nas.example:5001")
	request.Header.Set("Origin", "https://attacker.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin response = %d", response.Code)
	}
}
