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

func isSessionError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.Code == 106 || apiErr.Code == 119)
}

func isOTPChallenge(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.Code == 403 || apiErr.Code == 406)
}

func isInvalidOTP(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == 404
}
