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

	"github.com/derekvery666/dsmctl/internal/gateway/platformauth"
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

	// Production shape: Web Station strips the /dsmctl prefix from the path but
	// forwards X-Forwarded-Prefix=/dsmctl. The redirect must restore the prefix
	// (regression: it used to drop it, redirecting to /admin/ and 404ing).
	stripped := httptest.NewRequest(http.MethodGet, "/admin/?view=connections", nil)
	stripped.RemoteAddr = "127.0.0.1:1234"
	stripped.Header.Set("X-Forwarded-Host", "nas.example:80")
	stripped.Header.Set("X-Forwarded-Proto", "http")
	stripped.Header.Set("X-Forwarded-Prefix", "/dsmctl")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, stripped)
	if location := response.Header().Get("Location"); response.Code != http.StatusPermanentRedirect || location != "https://nas.example/dsmctl/admin/?view=connections" {
		t.Fatalf("prefix-stripped redirect = %d %q", response.Code, location)
	}

	// The prefix must not be duplicated when the forwarded path still carries it.
	kept := httptest.NewRequest(http.MethodGet, "/dsmctl/admin/", nil)
	kept.RemoteAddr = "127.0.0.1:1234"
	kept.Header.Set("X-Forwarded-Host", "nas.example:80")
	kept.Header.Set("X-Forwarded-Proto", "http")
	kept.Header.Set("X-Forwarded-Prefix", "/dsmctl")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, kept)
	if location := response.Header().Get("Location"); response.Code != http.StatusPermanentRedirect || location != "https://nas.example/dsmctl/admin/" {
		t.Fatalf("prefix-kept redirect = %d %q", response.Code, location)
	}
	if backendCalls != 0 {
		t.Fatalf("prefix redirects reached backend %d times", backendCalls)
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
	startRequest.Header.Set("Origin", "https://nas.example:443")
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
	completeRequest.Header.Set("X-Forwarded-Host", "nas.example:443")
	completeRequest.Header.Set("X-Forwarded-Proto", "https")
	completeRequest.Header.Set("Origin", "https://nas.example:443")
	completeResponse := httptest.NewRecorder()
	handler.ServeHTTP(completeResponse, completeRequest)
	if completeResponse.Code != http.StatusCreated || checkedSubject != "dsm-admin" {
		t.Fatalf("complete response = %d %q, subject=%q", completeResponse.Code, completeResponse.Body.String(), checkedSubject)
	}

	replayRequest := httptest.NewRequest(http.MethodPost, "/admin/api/dsm-login/complete", bytes.NewReader(completeBody))
	replayRequest.Header.Set("X-Forwarded-Host", "nas.example:443")
	replayRequest.Header.Set("X-Forwarded-Proto", "https")
	replayRequest.Header.Set("Origin", "https://nas.example:443")
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

func TestProxyEnforcesDSMLoginOrigin(t *testing.T) {
	backendURL, _ := url.Parse("http://127.0.0.1:1")
	options := testOptions(t, backendURL, 10)
	handler, err := New(options)
	if err != nil {
		t.Fatal(err)
	}

	// The forwarded Gateway origin here is https://nas.example:443. A browser on
	// the Admin UI or OAuth consent page attaches exactly that Origin; a missing
	// or foreign Origin must be rejected before the bridge does any DSM work.
	forbidden := func(path, origin string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		request.Header.Set("X-Forwarded-Host", "nas.example:443")
		request.Header.Set("X-Forwarded-Proto", "https")
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s origin %q status = %d %q", path, origin, response.Code, response.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s origin %q body decode: %v", path, origin, err)
		}
		if body["code"] != "invalid_origin" {
			t.Fatalf("%s origin %q error code = %q", path, origin, body["code"])
		}
	}

	for _, path := range []string{dsmLoginStartPath, dsmLoginCompletePath} {
		forbidden(path, "")                       // fail closed when the browser sends no Origin
		forbidden(path, "https://evil.example")   // cross-origin caller
		forbidden(path, "http://nas.example:443") // scheme mismatch
		forbidden(path, "https://nas.example")    // port/authority mismatch
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
