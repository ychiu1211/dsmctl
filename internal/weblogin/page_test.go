package weblogin

import (
	"strings"
	"testing"
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
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing design-token contract value %q", want)
		}
	}
}

func TestPageLocalizesAllFiveLocales(t *testing.T) {
	page := buildPage(testLoginURL, testDSMOrigin)
	for _, want := range []string{
		`"Sign in to DSM"`,
		`"登入 DSM"`,
		`"登录 DSM"`,
		`"DSM にサインイン"`,
		`"Bei DSM anmelden"`,
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
	for _, banned := range []string{`<link`, `@import`, `url(`, `src="http`} {
		if strings.Contains(page, banned) {
			t.Errorf("page must not reference external resources, found %q", banned)
		}
	}
	for _, want := range []string{testLoginURL, testDSMOrigin} {
		if !strings.Contains(page, want) {
			t.Errorf("page must embed %q", want)
		}
	}
}
