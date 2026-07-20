package synology

import "errors"

// Category is a stable, closed classification of a DSM failure. The string
// values are part of the CLI/MCP contract and must not change without a new
// work item; adding a category is likewise a contract change.
type Category string

const (
	// CategoryAuth: the session or credentials are the problem — expired
	// session, wrong password, or a required/failed second factor.
	CategoryAuth Category = "auth"
	// CategoryPermission: authenticated, but the account lacks privilege for
	// the operation (or is blocked by an access rule).
	CategoryPermission Category = "permission"
	// CategoryNotFound: the target API, method, or resource does not exist.
	CategoryNotFound Category = "not-found"
	// CategoryConflict: the request conflicts with current state (a duplicate,
	// or a resource busy/in use).
	CategoryConflict Category = "conflict"
	// CategoryRateLimit: DSM is throttling the caller.
	CategoryRateLimit Category = "rate-limit"
	// CategoryTransient: a temporary failure worth retrying (timeout, a 5xx
	// from the web server, a connection reset).
	CategoryTransient Category = "transient"
	// CategoryUnsupported: the operation or API version is not supported on
	// this DSM.
	CategoryUnsupported Category = "unsupported"
	// CategoryInvalidInput: a parameter was missing, malformed, or rejected.
	CategoryInvalidInput Category = "invalid-input"
	// CategoryUnknown: no confident classification (the fallback).
	CategoryUnknown Category = "unknown"
)

// AllCategories lists every category once, for exhaustive tests and docs.
func AllCategories() []Category {
	return []Category{
		CategoryAuth, CategoryPermission, CategoryNotFound, CategoryConflict,
		CategoryRateLimit, CategoryTransient, CategoryUnsupported,
		CategoryInvalidInput, CategoryUnknown,
	}
}

// categoryByCode maps a DSM API error code to a category. The common DSM codes
// (100-120) and the auth-domain codes (400-407) are covered; anything else
// falls through to CategoryUnknown. Session codes 106/107/119 and OTP codes
// 403/406/404 are classified auth, matching isSessionError / isOTPChallenge /
// isInvalidOTP.
var categoryByCode = map[int]Category{
	// Common API codes.
	101: CategoryInvalidInput, // no/invalid parameter
	102: CategoryNotFound,     // API does not exist
	103: CategoryNotFound,     // method does not exist
	104: CategoryUnsupported,  // API version not supported
	105: CategoryPermission,   // insufficient user privilege
	106: CategoryAuth,         // session timeout
	107: CategoryAuth,         // session interrupted by a duplicate sign-in
	108: CategoryPermission,   // upload/permission
	114: CategoryInvalidInput, // WEBAPI_ERR_NO_REQUIRED_PARAM
	119: CategoryAuth,         // SID missing/invalid
	120: CategoryInvalidInput, // invalid parameter(s)
	// Auth-domain codes (SYNO.API.Auth and login).
	400: CategoryAuth,       // no such account or wrong password
	401: CategoryAuth,       // account disabled
	402: CategoryPermission, // permission denied
	403: CategoryAuth,       // 2-step verification required
	404: CategoryAuth,       // failed 2-step verification code
	406: CategoryAuth,       // enforced 2-step verification / OTP needed
	407: CategoryPermission, // IP blocked
}

// Category classifies this DSM API error.
func (e *APIError) Category() Category {
	if category, ok := categoryByCode[e.Code]; ok {
		return category
	}
	return CategoryUnknown
}

// Classify returns the stable category of err by unwrapping to the first typed
// error it recognizes. It works after the application layer has wrapped the
// error with %w. A session-expired or OTP-required error classifies as auth; a
// DSM APIError uses its code table; anything else is CategoryUnknown.
func Classify(err error) Category {
	if err == nil {
		return CategoryUnknown
	}
	var sessionErr *SessionExpiredError
	if errors.As(err, &sessionErr) {
		return CategoryAuth
	}
	var otpErr *OTPRequiredError
	if errors.As(err, &otpErr) {
		return CategoryAuth
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Category()
	}
	return CategoryUnknown
}
