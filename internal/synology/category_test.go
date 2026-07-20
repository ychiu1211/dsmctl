package synology

import (
	"errors"
	"fmt"
	"testing"
)

func TestCategorySpellings(t *testing.T) {
	want := map[Category]string{
		CategoryAuth: "auth", CategoryPermission: "permission", CategoryNotFound: "not-found",
		CategoryConflict: "conflict", CategoryRateLimit: "rate-limit", CategoryTransient: "transient",
		CategoryUnsupported: "unsupported", CategoryInvalidInput: "invalid-input", CategoryUnknown: "unknown",
	}
	if len(AllCategories()) != len(want) {
		t.Fatalf("AllCategories() has %d entries, want %d", len(AllCategories()), len(want))
	}
	seen := map[Category]bool{}
	for _, category := range AllCategories() {
		if string(category) != want[category] {
			t.Errorf("category %v spelled %q", category, category)
		}
		if seen[category] {
			t.Errorf("category %v listed twice", category)
		}
		seen[category] = true
	}
}

func TestClassifyByCode(t *testing.T) {
	cases := map[int]Category{
		101: CategoryInvalidInput, 102: CategoryNotFound, 103: CategoryNotFound,
		104: CategoryUnsupported, 105: CategoryPermission, 106: CategoryAuth,
		107: CategoryAuth, 108: CategoryPermission, 114: CategoryInvalidInput,
		119: CategoryAuth, 120: CategoryInvalidInput,
		400: CategoryAuth, 401: CategoryAuth, 402: CategoryPermission,
		403: CategoryAuth, 404: CategoryAuth, 406: CategoryAuth, 407: CategoryPermission,
		999: CategoryUnknown, // unmapped code falls back
	}
	for code, want := range cases {
		got := (&APIError{API: "SYNO.Test", Method: "get", Code: code}).Category()
		if got != want {
			t.Errorf("code %d classified %q, want %q", code, got, want)
		}
		if Classify(&APIError{Code: code}) != want {
			t.Errorf("Classify(code %d) = %q, want %q", code, Classify(&APIError{Code: code}), want)
		}
	}
}

func TestClassifyThroughWrapping(t *testing.T) {
	base := &APIError{API: "SYNO.Core.QuickConnect", Method: "set", Code: 105}
	wrapped := fmt.Errorf("apply QuickConnect: %w", base)
	doubleWrapped := fmt.Errorf(`NAS "lab": %w`, wrapped)
	if got := Classify(doubleWrapped); got != CategoryPermission {
		t.Fatalf("Classify(double-wrapped) = %q, want permission", got)
	}
	if !errors.Is(doubleWrapped, error(base)) {
		t.Fatal("wrapping lost the base error")
	}
}

func TestClassifySessionAndOTPAndNilAndPlain(t *testing.T) {
	if got := Classify(&SessionExpiredError{NAS: "lab"}); got != CategoryAuth {
		t.Errorf("session-expired classified %q, want auth", got)
	}
	if got := Classify(fmt.Errorf("wrap: %w", &OTPRequiredError{})); got != CategoryAuth {
		t.Errorf("otp-required classified %q, want auth", got)
	}
	if got := Classify(nil); got != CategoryUnknown {
		t.Errorf("nil classified %q, want unknown", got)
	}
	if got := Classify(errors.New("plain non-DSM failure")); got != CategoryUnknown {
		t.Errorf("plain error classified %q, want unknown", got)
	}
}

// APIError carries no secret material; its rendered message is API/method/code
// only. This guards against a future field leaking a SID/token into the string
// a CLI/MCP surface would display.
func TestAPIErrorMessageCarriesNoSecrets(t *testing.T) {
	msg := (&APIError{API: "SYNO.API.Auth", Method: "login", Code: 400}).Error()
	for _, secret := range []string{"passwd", "password", "_sid", "SynoToken", "otp"} {
		if containsFold(msg, secret) {
			t.Fatalf("APIError message %q contains %q", msg, secret)
		}
	}
}

func containsFold(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexFold(haystack, needle) >= 0
}

func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
