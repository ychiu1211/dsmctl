package weblogin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/webassets"
)

const (
	testLoginURL  = "https://nas.example.test:5001/?client_id=webui#/signin"
	testDSMOrigin = "https://nas.example.test:5001"
)

// The helper page shares the gateway administration UI's visual identity.
// internal/gateway/admin/ui.go is the source of truth for these literals;
// internal/gateway/admin/handler_test.go pins the same values on that side.
func TestPageCarriesSharedDesignTokens(t *testing.T) {
	page := buildPage(testLoginURL, testDSMOrigin)
	for _, want := range []string{
		`--brand-500:#2588df`,
		`--brand-950:#0d263f`,
		`--slate-900:#162334`,
		`--color-action:var(--brand-500)`,
		`--color-focus:rgba(37,136,223,.28)`,
		`--success:#1f9d68`,
		`--danger:#cf3f3f`,
		`font-family:Inter,"Segoe UI","Noto Sans TC",system-ui,-apple-system,sans-serif`,
		`<meta name="viewport" content="width=device-width,initial-scale=1">`,
		`<meta name="theme-color" content="#0d263f">`,
		`<link rel="icon" href="/favicon.svg" type="image/svg+xml" sizes="any">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing design-token contract value %q", want)
		}
	}
}

func TestPageLocalizesAllFiveLocales(t *testing.T) {
	page := buildPage(testLoginURL, testDSMOrigin)
	for _, want := range []string{
		`"Sign in to {host}"`,
		`"登入 {host}"`,
		`"登录 {host}"`,
		`"{host} にサインイン"`,
		`"Bei {host} anmelden"`,
		`normalizeLocale`,
		`navigator.language`,
		`"zh-Hant"`,
		`"zh-Hans"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing localization marker %q", want)
		}
	}
}

func TestPageDrivesFourStatesFromCallbackStatus(t *testing.T) {
	page := buildPage(testLoginURL, testDSMOrigin)
	for _, want := range []string{
		`data-state="waiting"`,
		`msg-exchanging`,
		`msg-success`,
		`msg-error`,
		`setState(r.ok?"success":"error")`,
		`data-i18n`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing state-machine marker %q", want)
		}
	}
	if strings.Contains(page, ".text()") {
		t.Error("page must choose the terminal state from the HTTP status, not inject server response text")
	}
}

func TestPageIsSelfContained(t *testing.T) {
	page := buildPage(testLoginURL, testDSMOrigin)
	for _, banned := range []string{`@import`, `url(`, `src="http`, `href="http`, `href="//`} {
		if strings.Contains(page, banned) {
			t.Errorf("page must not reference external resources, found %q", banned)
		}
	}
	if got := strings.Count(page, `<link `); got != 1 {
		t.Errorf("page link count = %d, want only the same-origin favicon", got)
	}
	for _, want := range []string{testLoginURL, testDSMOrigin} {
		if !strings.Contains(page, want) {
			t.Errorf("page must embed %q", want)
		}
	}
}

func TestLoopbackHandlerServesSharedFavicon(t *testing.T) {
	handler := newLoopbackHandler("<html></html>", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("favicon status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != webassets.FaviconContentType {
		t.Errorf("favicon Content-Type = %q", got)
	}
	if got := recorder.Body.String(); got != webassets.FaviconSVG() {
		t.Fatal("web-login favicon differs from the shared source")
	}
}

// The page asks the user to authenticate against a NAS; it must say which
// one, so the user can check the origin before typing a password anywhere.
func TestPageShowsTargetNAS(t *testing.T) {
	page := buildPage(testLoginURL, testDSMOrigin)
	if !strings.Contains(page, `<p class="target">`+testDSMOrigin+`</p>`) {
		t.Error("page must visibly name the NAS origin it signs in to")
	}
	if !strings.Contains(page, `<h1 data-i18n="heading">Sign in to nas.example.test</h1>`) {
		t.Error("page heading must name the NAS host")
	}
}
