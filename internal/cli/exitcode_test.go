package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitOK},
		{"invalid-input", &synology.APIError{Code: 120}, ExitInvalidInput},
		{"auth", &synology.APIError{Code: 119}, ExitAuth},
		{"permission", &synology.APIError{Code: 105}, ExitPermission},
		{"not-found", &synology.APIError{Code: 102}, ExitNotFound},
		{"unsupported", &synology.APIError{Code: 104}, ExitUnsupported},
		{"session-expired", &synology.SessionExpiredError{NAS: "lab"}, ExitAuth},
		{"wrapped auth", fmt.Errorf("apply: %w", &synology.APIError{Code: 400}), ExitAuth},
		{"plain", errors.New("boom"), ExitError},
	}
	for _, test := range cases {
		if got := ExitCode(test.err); got != test.want {
			t.Errorf("%s: ExitCode = %d, want %d", test.name, got, test.want)
		}
	}
}

// Every category must map to a distinct, non-zero exit code so scripts can
// branch unambiguously; the map must be total over the taxonomy.
func TestExitCodeIsTotalAndDistinct(t *testing.T) {
	// Representative DSM code per category (conflict/rate-limit/transient have no
	// common DSM code, so exercise them via the code constants directly below).
	byCategory := map[synology.Category]int{
		synology.CategoryInvalidInput: ExitInvalidInput,
		synology.CategoryAuth:         ExitAuth,
		synology.CategoryPermission:   ExitPermission,
		synology.CategoryNotFound:     ExitNotFound,
		synology.CategoryConflict:     ExitConflict,
		synology.CategoryRateLimit:    ExitRateLimit,
		synology.CategoryTransient:    ExitTransient,
		synology.CategoryUnsupported:  ExitUnsupported,
		synology.CategoryUnknown:      ExitError,
	}
	if len(byCategory) != len(synology.AllCategories()) {
		t.Fatalf("exit-code map has %d categories, taxonomy has %d", len(byCategory), len(synology.AllCategories()))
	}
	seen := map[int]synology.Category{}
	for category, code := range byCategory {
		if code == ExitOK {
			t.Errorf("category %v maps to the success code", category)
		}
		if other, dup := seen[code]; dup {
			t.Errorf("exit code %d shared by %v and %v", code, other, category)
		}
		seen[code] = category
	}
}

func TestFormatErrorPrefixesCategory(t *testing.T) {
	if got := FormatError(&synology.APIError{Code: 119}); got != "Error (auth): "+(&synology.APIError{Code: 119}).Error() {
		t.Errorf("FormatError(auth) = %q", got)
	}
	if got := FormatError(errors.New("boom")); got != "Error: boom" {
		t.Errorf("FormatError(plain) = %q", got)
	}
}
