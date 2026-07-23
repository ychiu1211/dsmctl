package synologyauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/gateway/platformauth"
)

type validatorFunc func(*http.Request) (string, error)

func (f validatorFunc) Validate(req *http.Request) (string, error) { return f(req) }

type subjectValidatorFunc func(context.Context, string) error

func (f subjectValidatorFunc) ValidateSubject(ctx context.Context, subject string) error {
	return f(ctx, subject)
}

func testOptions(t *testing.T, backend *url.URL, keyByte byte) Options {
	t.Helper()
	signer, err := platformauth.NewSigner(bytes.Repeat([]byte{keyByte}, 32), platformauth.DefaultAudience)
	if err != nil {
		t.Fatal(err)
	}
	return Options{
		Backend: backend, Signer: signer,
		Validator:        validatorFunc(func(*http.Request) (string, error) { return "", ErrUnauthorized }),
		SubjectValidator: subjectValidatorFunc(func(context.Context, string) error { return nil }),
	}
}

func TestProxyAttachesLegacyOAuthAssertionAndStripsDSMCookie(t *testing.T) {
	key := bytes.Repeat([]byte{3}, 32)
	verifier, _ := platformauth.NewVerifier(key, platformauth.DefaultAudience)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		identity, err := verifier.Verify(context.Background(), req.Header.Get(platformauth.HeaderName))
		if err != nil || identity.Subject != "dsm-admin" {
			http.Error(w, "bad assertion", http.StatusUnauthorized)
			return
		}
		if cookie := req.Header.Get("Cookie"); cookie != "dsmctl_admin_session=gateway" {
			http.Error(w, "cookie leaked: "+cookie, http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, req.URL.Path)
	}))
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)
	options := testOptions(t, backendURL, 3)
	options.Validator = validatorFunc(func(req *http.Request) (string, error) {
		if !strings.Contains(req.Header.Get("Cookie"), "DSM_SESSION=secret") {
			return "", ErrUnauthorized
		}
		return "dsm-admin", nil
	})
	handler, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/dsmctl/oauth/authorize", nil)
	request.Header.Set("Cookie", "DSM_SESSION=secret; dsmctl_admin_session=gateway")
	request.Header.Set("X-Forwarded-Prefix", "/dsmctl")
	request.Header.Set(platformauth.HeaderName, "forged")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "/oauth/authorize" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestProxyLetsGatewaySessionPassWithoutDSMIdentity(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get(platformauth.HeaderName) != "" {
			http.Error(w, "unexpected assertion", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)
	options := testOptions(t, backendURL, 4)
	handler, _ := New(options)
	request := httptest.NewRequest(http.MethodGet, "/admin/api/session", nil)
	request.Header.Set(platformauth.HeaderName, "forged")
	request.AddCookie(&http.Cookie{Name: gatewayAdministratorCookie, Value: "gateway"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("Gateway session response = %d", response.Code)
	}
}

func TestProxyFailsClosedForDirectDSMLoginAndNonLoopback(t *testing.T) {
	backendURL, _ := url.Parse("http://127.0.0.1:1")
	options := testOptions(t, backendURL, 5)
	options.RequireLoopback = true
	handler, _ := New(options)

	nonLoopback := httptest.NewRequest(http.MethodPost, "/admin/api/dsm-login", nil)
	nonLoopback.RemoteAddr = "192.0.2.1:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, nonLoopback)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-loopback status = %d", response.Code)
	}

	loopback := httptest.NewRequest(http.MethodPost, "/admin/api/dsm-login", nil)
	loopback.RemoteAddr = "127.0.0.1:1234"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, loopback)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("direct DSM login status = %d", response.Code)
	}
}

func TestProxyRedirectsForwardedHTTPToHTTPSWithoutBreakingLoopbackHealth(t *testing.T) {
	backendCalls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		backendCalls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)
	options := testOptions(t, backendURL, 9)
	options.RequireLoopback = true
	options.RedirectForwardedHTTP = true
	handler, err := New(options)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/dsmctl/admin/?view=connections", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Forwarded-Host", "nas.example:80")
	request.Header.Set("X-Forwarded-Proto", "http")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusPermanentRedirect {
		t.Fatalf("HTTP portal response = %d %q", response.Code, response.Body.String())
	}
	if location := response.Header().Get("Location"); location != "https://nas.example/dsmctl/admin/?view=connections" {
		t.Fatalf("HTTP portal redirect = %q", location)
	}
	if backendCalls != 0 {
		t.Fatalf("HTTP portal reached backend %d times", backendCalls)
	}

	unsafe := httptest.NewRequest(http.MethodGet, "/dsmctl/", nil)
	unsafe.RemoteAddr = "127.0.0.1:1234"
	unsafe.Header.Set("X-Forwarded-Host", "nas.example@attacker.example")
	unsafe.Header.Set("X-Forwarded-Proto", "http")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, unsafe)
	if response.Code != http.StatusBadRequest || backendCalls != 0 {
		t.Fatalf("unsafe redirect response = %d, backend calls = %d", response.Code, backendCalls)
	}

	health := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	health.RemoteAddr = "127.0.0.1:1234"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, health)
	if response.Code != http.StatusNoContent || backendCalls != 1 {
		t.Fatalf("loopback health response = %d, backend calls = %d", response.Code, backendCalls)
	}
}

func TestProxyCompletesDSMWebLoginCodeGrant(t *testing.T) {
	dsm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if err := req.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if req.Form.Get("api") != "SYNO.API.Auth" || req.Form.Get("method") != "login" || req.Form.Get("type") != "code" || req.Form.Get("session") != adminSessionName || req.Form.Get("code") != "one-time-code" || req.Form.Get("code_verifier") == "" {
			t.Errorf("unexpected DSM exchange form: %#v", req.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true,"data":{"account":"dsm-admin","sid":"private-sid","synotoken":"private-token","device_id":"device"}}`)
	}))
	defer dsm.Close()
	_, dsmPort, _ := net.SplitHostPort(strings.TrimPrefix(dsm.URL, "http://"))

	key := bytes.Repeat([]byte{6}, 32)
	verifier, _ := platformauth.NewVerifier(key, platformauth.DefaultAudience)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		identity, err := verifier.Verify(req.Context(), req.Header.Get(platformauth.HeaderName))
		if err != nil || identity.Subject != "dsm-admin" || req.URL.Path != dsmLoginPath {
			http.Error(w, "invalid promoted login", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(req.Body)
		if string(body) != "{}" || strings.Contains(string(body), "one-time-code") {
			http.Error(w, "OAuth material leaked", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)
	options := testOptions(t, backendURL, 6)
	options.DSMHTTPPort = dsmPort
	checkedSubject := ""
	options.SubjectValidator = subjectValidatorFunc(func(_ context.Context, subject string) error {
		checkedSubject = subject
		return nil
	})
	handler, err := New(options)
	if err != nil {
		t.Fatal(err)
	}

	startRequest := httptest.NewRequest(http.MethodPost, "/dsmctl/admin/api/dsm-login/start", strings.NewReader("{}"))
	startRequest.Header.Set("X-Forwarded-Host", "nas.example:443")
	startRequest.Header.Set("X-Forwarded-Proto", "https")
	startRequest.Header.Set("X-Forwarded-Prefix", "/dsmctl")
	startResponse := httptest.NewRecorder()
	handler.ServeHTTP(startResponse, startRequest)
	if startResponse.Code != http.StatusOK {
		t.Fatalf("start response = %d %q", startResponse.Code, startResponse.Body.String())
	}
	var start map[string]string
	if err := json.Unmarshal(startResponse.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	loginURL, _ := url.Parse(start["login_url"])
	if loginURL.Scheme != "https" || loginURL.Host != "nas.example:5001" || loginURL.Query().Get("client_id") != "webui" || loginURL.Query().Get("session") != adminSessionName || loginURL.Query().Get("opener") != "https://nas.example:443/dsmctl/admin/" || loginURL.Fragment != "/signin" {
		t.Fatalf("login URL = %s", start["login_url"])
	}

	serverKey := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	completeBody, _ := json.Marshal(map[string]string{
		"enrollment_id": start["enrollment_id"], "code": "one-time-code", "rs": serverKey, "state": start["state"],
	})
	completeRequest := httptest.NewRequest(http.MethodPost, "/admin/api/dsm-login/complete", bytes.NewReader(completeBody))
	completeRequest.Header.Set("Origin", "https://nas.example:443")
	completeResponse := httptest.NewRecorder()
	handler.ServeHTTP(completeResponse, completeRequest)
	if completeResponse.Code != http.StatusCreated || checkedSubject != "dsm-admin" {
		t.Fatalf("complete response = %d %q, subject=%q", completeResponse.Code, completeResponse.Body.String(), checkedSubject)
	}

	replayRequest := httptest.NewRequest(http.MethodPost, "/admin/api/dsm-login/complete", bytes.NewReader(completeBody))
	replayResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayResponse, replayRequest)
	if replayResponse.Code != http.StatusUnauthorized {
		t.Fatalf("replayed enrollment status = %d", replayResponse.Code)
	}
}

func TestProxyRejectsUnsafeDSMLoginHost(t *testing.T) {
	backendURL, _ := url.Parse("http://127.0.0.1:1")
	options := testOptions(t, backendURL, 7)
	handler, _ := New(options)
	request := httptest.NewRequest(http.MethodPost, "/admin/api/dsm-login/start", strings.NewReader("{}"))
	request.Header.Set("X-Forwarded-Host", "nas.example@attacker.example")
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unsafe host response = %d %q", response.Code, response.Body.String())
	}
}

func TestProxyRejectsNonAdministratorCodeGrant(t *testing.T) {
	backendURL, _ := url.Parse("http://127.0.0.1:1")
	options := testOptions(t, backendURL, 8)
	options.SubjectValidator = subjectValidatorFunc(func(context.Context, string) error { return errors.New("not administrator") })
	if _, err := New(options); err != nil {
		t.Fatalf("validator configuration rejected: %v", err)
	}
}
