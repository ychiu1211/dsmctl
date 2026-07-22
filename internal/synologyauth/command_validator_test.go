package synologyauth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

func TestCGIEnvironmentUsesDSMManagementOrigin(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/admin/api/dsm-login", strings.NewReader("{}"))
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Forwarded-For", "192.0.2.45")
	request.Header.Set("X-Forwarded-Host", "nas.example:443")
	request.Header.Set("Cookie", "id=secret")
	values := cgiEnvironment(request, "5443")
	environment := make(map[string]string, len(values))
	for _, value := range values {
		name, content, _ := strings.Cut(value, "=")
		environment[name] = content
	}
	for name, expected := range map[string]string{
		"REQUEST_METHOD": "GET",
		"REQUEST_URI":    "/webman/3rdparty/dsmctl-gateway/",
		"SERVER_NAME":    "nas.example",
		"SERVER_ADDR":    "nas.example",
		"SERVER_PORT":    "5443",
		"REMOTE_ADDR":    "192.0.2.45",
		"HTTP_HOST":      "nas.example:5443",
		"HTTP_COOKIE":    "id=secret",
		"CONTENT_LENGTH": "0",
		"HTTPS":          "on",
	} {
		if environment[name] != expected {
			t.Errorf("%s = %q, want %q", name, environment[name], expected)
		}
	}
}

func TestAuthenticateExitStatusIsNotAnExecutionFailure(t *testing.T) {
	if !authenticationCommandStarted(&exec.ExitError{}) {
		t.Fatal("a non-zero authenticate.cgi exit must still allow stdout validation")
	}
	if authenticationCommandStarted(errors.New("fork/exec failed")) {
		t.Fatal("a command start failure must fail before stdout validation")
	}
}

func TestCommandValidatorReportsMissingCookieWithoutRunningCommand(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/admin/api/dsm-login", nil)
	_, err := (CommandValidator{AuthenticatePath: "missing", IDPath: "missing"}).Validate(request)
	if !errors.Is(err, ErrUnauthorized) || validationFailureReason(err) != "missing_cookie" {
		t.Fatalf("missing cookie error = %#v (%s)", err, validationFailureReason(err))
	}
}
