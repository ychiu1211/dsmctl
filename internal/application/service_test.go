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
