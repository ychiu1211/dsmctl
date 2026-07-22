package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/gateway/platformauth"
)

func TestDSMLoginOnlyThenOptionalLocalFallback(t *testing.T) {
	_, repository, manager := newUninitializedTestHandler(t, nil)
	defer manager.Close(context.Background())
	key := bytes.Repeat([]byte{6}, 32)
	signer, err := platformauth.NewSigner(key, platformauth.DefaultAudience)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := platformauth.NewVerifier(key, platformauth.DefaultAudience)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Options{Repository: repository, Manager: manager, PublicURL: "https://gateway.example", PlatformVerifier: verifier})
	if err != nil {
		t.Fatal(err)
	}

	status := performJSON(handler, http.MethodGet, "/admin/api/setup/status", "", "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"state":"dsm_login_only"`) || !strings.Contains(status.Body.String(), `"local_login_available":false`) {
		t.Fatalf("fresh DSM status = %d %s", status.Code, status.Body.String())
	}
	if setup := performJSON(handler, http.MethodPost, "/admin/api/setup", `{"username":"owner","password":"correct horse battery staple"}`, ""); setup.Code != http.StatusNotFound {
		t.Fatalf("DSM unauthenticated setup = %d", setup.Code)
	}
	if local := performJSON(handler, http.MethodPost, "/admin/api/login", `{"username":"owner","password":"correct horse battery staple"}`, ""); local.Code != http.StatusUnauthorized {
		t.Fatalf("local login before setup = %d", local.Code)
	}
	if localSetup := performPlatformJSON(handler, http.MethodPost, "/admin/api/local-administrator", `{"username":"fallback","password":"correct horse battery staple","confirm_password":"correct horse battery staple"}`, "", ""); localSetup.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated local fallback setup = %d %s", localSetup.Code, localSetup.Body.String())
	}

	assertion, _ := signer.Sign("dsm-admin")
	login := performPlatformJSON(handler, http.MethodPost, "/admin/api/dsm-login", `{}`, "", assertion)
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), `"provider":"dsm"`) {
		t.Fatalf("DSM login = %d %s", login.Code, login.Body.String())
	}
	session := responseCookieValue(t, login, administratorCookie)

	if current := performPlatformJSON(handler, http.MethodGet, "/admin/api/session", "", session, ""); current.Code != http.StatusOK {
		t.Fatalf("independent DSM-backed Gateway session = %d %s", current.Code, current.Body.String())
	}

	configured := performPlatformJSON(handler, http.MethodPost, "/admin/api/local-administrator", `{"username":"fallback","password":"correct horse battery staple","confirm_password":"correct horse battery staple"}`, session, "")
	if configured.Code != http.StatusCreated {
		t.Fatalf("configure fallback = %d %s", configured.Code, configured.Body.String())
	}
	status = performJSON(handler, http.MethodGet, "/admin/api/setup/status", "", "")
	if !strings.Contains(status.Body.String(), `"local_login_available":true`) || !strings.Contains(status.Body.String(), `"dsm_weblogin_available":true`) {
		t.Fatalf("configured DSM status = %s", status.Body.String())
	}
	local := performJSON(handler, http.MethodPost, "/admin/api/login", `{"username":"fallback","password":"correct horse battery staple"}`, "")
	if local.Code != http.StatusOK || !strings.Contains(local.Body.String(), `"provider":"local"`) {
		t.Fatalf("fallback login = %d %s", local.Code, local.Body.String())
	}
}

func TestGenericGatewayDoesNotExposeDSMLogin(t *testing.T) {
	handler, _, manager := newUninitializedTestHandler(t, nil)
	defer manager.Close(context.Background())

	status := performJSON(handler, http.MethodGet, "/admin/api/setup/status", "", "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"state":"setup_available"`) || strings.Contains(status.Body.String(), "dsm_weblogin_available") {
		t.Fatalf("generic setup status = %d %s", status.Code, status.Body.String())
	}
	dsm := performPlatformJSON(handler, http.MethodPost, "/admin/api/dsm-login", `{}`, "", "forged")
	if dsm.Code != http.StatusNotFound {
		t.Fatalf("generic DSM login = %d %s", dsm.Code, dsm.Body.String())
	}
}

func performPlatformJSON(handler http.Handler, method, path, body, sessionToken, assertion string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Origin", "https://gateway.example")
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(requestHeader, "1")
	}
	if sessionToken != "" {
		request.AddCookie(&http.Cookie{Name: administratorCookie, Value: sessionToken})
	}
	if assertion != "" {
		request.Header.Set(platformauth.HeaderName, assertion)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
