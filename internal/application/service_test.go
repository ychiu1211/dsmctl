package application

import (
	"errors"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

func TestAuthenticationErrorDirectsNonInteractiveCallerToCLI(t *testing.T) {
	err := authenticationError("office", &synology.OTPRequiredError{Cause: errors.New("challenge")})
	if !strings.Contains(err.Error(), "dsmctl auth login --nas office") {
		t.Fatalf("authenticationError() = %q", err)
	}
}

func TestAuthenticationErrorReportsSessionEnded(t *testing.T) {
	err := authenticationError("office", &synology.SessionExpiredError{Cause: errors.New("resume rejected")})
	if !synology.IsSessionExpired(err) {
		t.Fatalf("authenticationError() dropped the session-expired type: %v", err)
	}
	if !strings.Contains(err.Error(), "session for NAS \"office\" has ended") || !strings.Contains(err.Error(), "dsmctl auth login --nas office") {
		t.Fatalf("authenticationError() = %q", err)
	}
}
