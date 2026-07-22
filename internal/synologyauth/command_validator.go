package synologyauth

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// CommandValidator follows Synology's package-CGI authentication contract:
// authenticate.cgi receives the original request environment and prints the
// current DSM username. Group membership is checked separately without a shell.
type CommandValidator struct {
	AuthenticatePath string
	IDPath           string
	Timeout          time.Duration
	DSMHTTPSPort     string
}

func (v CommandValidator) Validate(req *http.Request) (string, error) {
	if strings.TrimSpace(req.Header.Get("Cookie")) == "" {
		return "", unauthorized("missing_cookie")
	}
	timeout := v.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(req.Context(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, v.AuthenticatePath)
	command.Env = cgiEnvironment(req, v.DSMHTTPSPort)
	var output bytes.Buffer
	command.Stdout = &boundedWriter{buffer: &output, remaining: 512}
	command.Stderr = &boundedWriter{buffer: &bytes.Buffer{}, remaining: 512}
	if err := command.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", unauthorized("authenticate_timeout")
		}
		// DSM's authenticate.cgi communicates authentication through stdout.
		// Some DSM releases exit non-zero even after the program ran normally,
		// so an ExitError is not itself an authentication result. A start/exec
		// failure still fails closed before stdout is considered.
		if !authenticationCommandStarted(err) {
			return "", unauthorized("authenticate_failed")
		}
	}
	subject := strings.TrimSpace(output.String())
	if subject == "" || len(subject) > 256 || strings.ContainsAny(subject, "\r\n\x00") {
		return "", unauthorized("invalid_subject")
	}
	if err := v.ValidateSubject(ctx, subject); err != nil {
		return "", err
	}
	return subject, nil
}

// ValidateSubject verifies that a DSM-authenticated account is an effective
// member of the administrators group. Browser code-grant login uses this after
// DSM has returned the authenticated account from the PKCE exchange.
func (v CommandValidator) ValidateSubject(ctx context.Context, subject string) error {
	if strings.TrimSpace(subject) == "" || len(subject) > 256 || strings.ContainsAny(subject, "\r\n\x00") {
		return unauthorized("invalid_subject")
	}
	idCommand := exec.CommandContext(ctx, v.IDPath, "-Gn", subject)
	groups, err := idCommand.Output()
	if err != nil || len(groups) > 4096 {
		return unauthorized("group_lookup_failed")
	}
	for _, group := range strings.Fields(string(groups)) {
		if group == "administrators" {
			return nil
		}
	}
	return unauthorized("not_administrator")
}

func authenticationCommandStarted(err error) bool {
	if err == nil {
		return true
	}
	var exitError *exec.ExitError
	return errors.As(err, &exitError)
}

func cgiEnvironment(req *http.Request, dsmHTTPSPort string) []string {
	if dsmHTTPSPort == "" {
		dsmHTTPSPort = defaultDSMHTTPSPort
	}
	remoteHost, _, _ := net.SplitHostPort(req.RemoteAddr)
	if forwarded := strings.TrimSpace(strings.Split(req.Header.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(forwarded) != nil {
		remoteHost = forwarded
	}
	forwardedHost := strings.TrimSpace(req.Header.Get("X-Forwarded-Host"))
	serverHost, err := forwardedHostname(forwardedHost)
	if err != nil {
		serverHost, _, _ = net.SplitHostPort(req.Host)
	}
	if remoteHost == "" {
		remoteHost = req.RemoteAddr
	}
	if serverHost == "" {
		serverHost = req.Host
	}
	serverAuthority := net.JoinHostPort(serverHost, dsmHTTPSPort)
	values := []string{
		"PATH=/usr/syno/bin:/usr/syno/sbin:/usr/bin:/bin",
		"GATEWAY_INTERFACE=CGI/1.1",
		"REQUEST_METHOD=GET",
		"REQUEST_URI=/webman/3rdparty/dsmctl-gateway/",
		"QUERY_STRING=",
		"SERVER_PROTOCOL=" + req.Proto,
		"SERVER_NAME=" + serverHost,
		"SERVER_ADDR=" + serverHost,
		"SERVER_PORT=" + dsmHTTPSPort,
		"REMOTE_ADDR=" + remoteHost,
		"HTTP_HOST=" + serverAuthority,
		"HTTP_COOKIE=" + req.Header.Get("Cookie"),
		"HTTP_USER_AGENT=" + req.UserAgent(),
		"CONTENT_LENGTH=0",
		"HTTPS=on",
	}
	return values
}

type validationFailure struct {
	reason string
}

func (e validationFailure) Error() string { return ErrUnauthorized.Error() }
func (e validationFailure) Unwrap() error { return ErrUnauthorized }

func unauthorized(reason string) error {
	return validationFailure{reason: reason}
}

func validationFailureReason(err error) string {
	var failure validationFailure
	if errors.As(err, &failure) && failure.reason != "" {
		return failure.reason
	}
	return "unauthorized"
}

type boundedWriter struct {
	buffer    *bytes.Buffer
	remaining int
}

func (w *boundedWriter) Write(value []byte) (int, error) {
	original := len(value)
	if len(value) > w.remaining {
		value = value[:w.remaining]
	}
	if len(value) > 0 {
		_, _ = w.buffer.Write(value)
		w.remaining -= len(value)
	}
	if original > len(value) {
		return original, errors.New("command output exceeded limit")
	}
	return original, nil
}
