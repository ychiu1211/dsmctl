package synologyauth

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type CommandValidator struct {
	AuthenticatePath string
	IDPath           string
	Timeout          time.Duration
}

func (v CommandValidator) Validate(req *http.Request) (string, error) {
	if strings.TrimSpace(req.Header.Get("Cookie")) == "" {
		return "", ErrUnauthorized
	}
	timeout := v.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(req.Context(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, v.AuthenticatePath)
	command.Env = cgiEnvironment(req)
	var output bytes.Buffer
	command.Stdout = &boundedWriter{buffer: &output, remaining: 512}
	command.Stderr = &boundedWriter{buffer: &bytes.Buffer{}, remaining: 512}
	if err := command.Run(); err != nil {
		return "", ErrUnauthorized
	}
	subject := strings.TrimSpace(output.String())
	if subject == "" || len(subject) > 256 || strings.ContainsAny(subject, "\r\n\x00") {
		return "", ErrUnauthorized
	}
	idCommand := exec.CommandContext(ctx, v.IDPath, "-Gn", subject)
	groups, err := idCommand.Output()
	if err != nil || len(groups) > 4096 {
		return "", ErrUnauthorized
	}
	for _, group := range strings.Fields(string(groups)) {
		if group == "administrators" {
			return subject, nil
		}
	}
	return "", ErrUnauthorized
}

func cgiEnvironment(req *http.Request) []string {
	remoteHost, _, _ := net.SplitHostPort(req.RemoteAddr)
	if forwarded := strings.TrimSpace(strings.Split(req.Header.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(forwarded) != nil {
		remoteHost = forwarded
	}
	serverHost, serverPort, _ := net.SplitHostPort(req.Host)
	if remoteHost == "" {
		remoteHost = req.RemoteAddr
	}
	if serverHost == "" {
		serverHost = req.Host
	}
	if serverPort == "" {
		serverPort = "443"
	}
	values := []string{
		"PATH=/usr/syno/bin:/usr/syno/sbin:/usr/bin:/bin",
		"GATEWAY_INTERFACE=CGI/1.1",
		"REQUEST_METHOD=" + req.Method,
		"REQUEST_URI=" + req.URL.RequestURI(),
		"QUERY_STRING=" + req.URL.RawQuery,
		"SERVER_PROTOCOL=" + req.Proto,
		"SERVER_NAME=" + serverHost,
		"SERVER_ADDR=127.0.0.1",
		"SERVER_PORT=" + serverPort,
		"REMOTE_ADDR=" + remoteHost,
		"HTTP_HOST=" + req.Host,
		"HTTP_COOKIE=" + req.Header.Get("Cookie"),
		"HTTP_USER_AGENT=" + req.UserAgent(),
		"CONTENT_LENGTH=" + strconv.FormatInt(req.ContentLength, 10),
	}
	if strings.EqualFold(req.Header.Get("X-Forwarded-Proto"), "https") {
		values = append(values, "HTTPS=on")
	}
	return values
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
