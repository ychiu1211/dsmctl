package cli

import (
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

// Stable process exit codes keyed by DSM error category. These are part of the
// CLI contract (documented in docs/) so scripts can branch on the failure class
// instead of parsing messages. 0 is success; 1 is any non-DSM/unknown failure.
const (
	ExitOK           = 0
	ExitError        = 1 // generic or unclassified failure
	ExitInvalidInput = 2
	ExitAuth         = 3
	ExitPermission   = 4
	ExitNotFound     = 5
	ExitConflict     = 6
	ExitRateLimit    = 7
	ExitTransient    = 8
	ExitUnsupported  = 9
)

// ExitCode maps err to its stable process exit code by DSM error category. A nil
// error is ExitOK; an unclassified (non-DSM) failure is ExitError.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	switch synology.Classify(err) {
	case synology.CategoryInvalidInput:
		return ExitInvalidInput
	case synology.CategoryAuth:
		return ExitAuth
	case synology.CategoryPermission:
		return ExitPermission
	case synology.CategoryNotFound:
		return ExitNotFound
	case synology.CategoryConflict:
		return ExitConflict
	case synology.CategoryRateLimit:
		return ExitRateLimit
	case synology.CategoryTransient:
		return ExitTransient
	case synology.CategoryUnsupported:
		return ExitUnsupported
	default:
		return ExitError
	}
}

// FormatError renders err for stderr, prefixing the DSM error category when one
// is confidently classified so the human sees the failure class that the exit
// code also encodes.
func FormatError(err error) string {
	if category := synology.Classify(err); category != synology.CategoryUnknown {
		return fmt.Sprintf("Error (%s): %s", category, err)
	}
	return fmt.Sprintf("Error: %s", err)
}
