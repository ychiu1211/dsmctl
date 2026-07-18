package synology

import (
	"errors"
	"fmt"
)

type APIError struct {
	API    string
	Method string
	Code   int
}

// SessionExpiredError indicates DSM rejected the session (it timed out, was
// invalidated, or was displaced by another sign-in) and the client could not
// renew it automatically — the browserless resume had no key material or was
// itself rejected — so a new interactive sign-in is required. Both the CLI and
// the MCP server surface this as an actionable "session ended" result; detect
// it with IsSessionExpired rather than string-matching.
type SessionExpiredError struct {
	// NAS is the profile whose session ended; empty at the transport layer and
	// filled in by the application layer, which knows the profile name.
	NAS   string
	Cause error
}

func (e *SessionExpiredError) Error() string {
	if e.NAS != "" {
		return fmt.Sprintf("the DSM session for NAS %q has ended; sign in again with 'dsmctl auth login --nas %s'", e.NAS, e.NAS)
	}
	return "the DSM session has ended; sign in again with 'dsmctl auth login'"
}

func (e *SessionExpiredError) Unwrap() error {
	return e.Cause
}

// IsSessionExpired reports whether err is, or wraps, a SessionExpiredError.
func IsSessionExpired(err error) bool {
	var sessionErr *SessionExpiredError
	return errors.As(err, &sessionErr)
}

// OTPRequiredError indicates that DSM requires an OTP but the caller did not
// provide an interactive OTP source. MCP callers should ask the user to finish
// an interactive CLI login instead of transporting the OTP through the model.
type OTPRequiredError struct {
	Cause error
}

func (e *OTPRequiredError) Error() string {
	return "DSM requires a one-time password"
}

func (e *OTPRequiredError) Unwrap() error {
	return e.Cause
}

func IsOTPRequired(err error) bool {
	var otpErr *OTPRequiredError
	return errors.As(err, &otpErr)
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Synology API %s.%s failed with code %d", e.API, e.Method, e.Code)
}

// isSessionError reports a DSM error that means the session is no longer usable:
// 106 (timeout), 107 (interrupted by a duplicate sign-in elsewhere), or 119
// (SID missing/invalid). Each triggers a browserless resume attempt.
func isSessionError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.Code == 106 || apiErr.Code == 107 || apiErr.Code == 119)
}

func isOTPChallenge(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.Code == 403 || apiErr.Code == 406)
}

func isInvalidOTP(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == 404
}
