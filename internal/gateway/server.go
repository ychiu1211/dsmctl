// Package gateway contains the platform-neutral HTTP process boundary for the
// remote MCP gateway. It deliberately knows nothing about DSM package paths or
// container runtimes.
package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
)

const (
	defaultMaxBodyBytes    = int64(1 << 20)
	defaultMaxConcurrent   = 8
	defaultShutdownTimeout = 10 * time.Second
)

// Options configures the gateway HTTP boundary. BearerToken is required only
// for the WI-014 static read-only mode. Managed mode supplies an
// MCPAuthenticator backed by persistent scoped-token state instead.
type Options struct {
	MCPServer        *mcp.Server
	MCPAuthenticator MCPAuthenticator
	MCPAuditor       remotepolicy.Auditor
	AdminHandler     http.Handler
	BearerToken      string
	AllowedHosts     []string
	AllowedOrigins   []string
	TrustedProxies   []netip.Prefix
	MaxBodyBytes     int64
	MaxConcurrent    int
	Ready            func(context.Context) error
	Close            func(context.Context) error
	Logger           *slog.Logger

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

type MCPAuthenticator interface {
	AuthenticateMCPToken(context.Context, string) (remotepolicy.Principal, error)
}

// Server is a configured gateway HTTP server.
type Server struct {
	httpServer      *http.Server
	close           func(context.Context) error
	closeOnce       sync.Once
	closeErr        error
	shutdownTimeout time.Duration
}

// New builds a hardened stateless Streamable HTTP gateway.
func New(options Options) (*Server, error) {
	if options.MCPServer == nil {
		return nil, errors.New("MCP server is required")
	}
	if options.MCPAuthenticator == nil {
		if err := ValidateDevelopmentToken(options.BearerToken); err != nil {
			return nil, err
		}
	}
	allowedHosts, err := normalizeHosts(options.AllowedHosts)
	if err != nil {
		return nil, err
	}
	if len(allowedHosts) == 0 {
		return nil, errors.New("at least one allowed host is required")
	}
	allowedOrigins, err := normalizeOrigins(options.AllowedOrigins)
	if err != nil {
		return nil, err
	}

	maxBodyBytes := options.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultMaxBodyBytes
	}
	maxConcurrent := options.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrent
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	ready := options.Ready
	if ready == nil {
		ready = func(context.Context) error { return nil }
	}

	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return options.MCPServer },
		&mcp.StreamableHTTPOptions{
			Stateless:    true,
			JSONResponse: true,
		},
	)
	semaphore := make(chan struct{}, maxConcurrent)
	protectedMCP := limitConcurrent(semaphore, requireMCPRequest(maxBodyBytes, mcpHandler))
	if options.MCPAuthenticator != nil {
		protectedMCP = authenticateMCP(options.MCPAuthenticator, options.MCPAuditor, newIdentityLimiter(), protectedMCP)
	} else {
		tokenDigest := sha256.Sum256([]byte(options.BearerToken))
		protectedMCP = requireBearer(tokenDigest, protectedMCP)
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", protectedMCP)
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/readyz", readinessHandler(ready, logger))
	if options.AdminHandler != nil {
		mux.Handle("/admin", options.AdminHandler)
		mux.Handle("/admin/", options.AdminHandler)
	}

	handler := correlateAndLog(logger, options.TrustedProxies,
		protectHostAndOrigin(allowedHosts, allowedOrigins, options.AdminHandler != nil, mux))
	shutdownTimeout := durationOr(options.ShutdownTimeout, defaultShutdownTimeout)
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: durationOr(options.ReadHeaderTimeout, 5*time.Second),
		ReadTimeout:       durationOr(options.ReadTimeout, 15*time.Second),
		WriteTimeout:      durationOr(options.WriteTimeout, 2*time.Minute+10*time.Second),
		IdleTimeout:       durationOr(options.IdleTimeout, time.Minute),
		MaxHeaderBytes:    32 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	return &Server{
		httpServer:      server,
		close:           options.Close,
		shutdownTimeout: shutdownTimeout,
	}, nil
}

func durationOr(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

// Handler exposes the configured handler for tests and embedding.
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// Serve runs until the listener fails or ctx is cancelled. Cancellation
// drains in-flight HTTP work before closing cached DSM sessions.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- s.httpServer.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		closeErr := s.closeSessions()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		return errors.Join(err, closeErr)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		shutdownErr := s.httpServer.Shutdown(shutdownCtx)
		cancel()
		if shutdownErr != nil {
			_ = s.httpServer.Close()
		}
		err := <-serveErr
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		return errors.Join(shutdownErr, err, s.closeSessions())
	}
}

func (s *Server) closeSessions() error {
	s.closeOnce.Do(func() {
		if s.close == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		s.closeErr = s.close(ctx)
	})
	return s.closeErr
}

func healthHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func readinessHandler(ready func(context.Context) error, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := ready(req.Context()); err != nil {
			logger.WarnContext(req.Context(), "gateway not ready", "request_id", requestID(req.Context()), "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requireBearer(expected [sha256.Size]byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		parts := strings.Fields(req.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			unauthorized(w)
			return
		}
		actual := sha256.Sum256([]byte(parts[1]))
		if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			unauthorized(w)
			return
		}
		next.ServeHTTP(w, req)
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="dsmctl-gateway"`)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}

type identityLimiter struct {
	mu      sync.Mutex
	windows map[string]identityWindow
}

type identityWindow struct {
	Started time.Time
	Count   int
}

func newIdentityLimiter() *identityLimiter {
	return &identityLimiter{windows: make(map[string]identityWindow)}
}

func (l *identityLimiter) Allow(id string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	window := l.windows[id]
	if window.Started.IsZero() || now.Sub(window.Started) >= time.Minute {
		window = identityWindow{Started: now}
	}
	if window.Count >= 120 {
		l.windows[id] = window
		return false
	}
	window.Count++
	l.windows[id] = window
	if len(l.windows) > 1024 {
		for key, item := range l.windows {
			if now.Sub(item.Started) >= 2*time.Minute {
				delete(l.windows, key)
			}
		}
	}
	return true
}

func authenticateMCP(authenticator MCPAuthenticator, auditor remotepolicy.Auditor, limiter *identityLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		parts := strings.Fields(req.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			auditMCPHTTP(req.Context(), auditor, "", "denied", "invalid_token")
			unauthorized(w)
			return
		}
		raw := parts[1]
		principal, err := authenticator.AuthenticateMCPToken(req.Context(), raw)
		raw = ""
		if err != nil {
			auditMCPHTTP(req.Context(), auditor, "", "denied", "invalid_token")
			unauthorized(w)
			return
		}
		if !limiter.Allow(principal.TokenID, time.Now()) {
			auditMCPHTTP(req.Context(), auditor, principal.TokenID, "denied", "rate_limited")
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
			return
		}
		ctx := remotepolicy.WithPrincipal(req.Context(), principal)
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

func auditMCPHTTP(ctx context.Context, auditor remotepolicy.Auditor, actorID, outcome, reason string) {
	if auditor == nil {
		return
	}
	_ = auditor.AppendAudit(ctx, remotepolicy.AuditEvent{CorrelationID: remotepolicy.CorrelationID(ctx), ActorType: "mcp_token", ActorID: actorID, Action: "mcp.authenticate", Outcome: outcome, Reason: reason})
}

func requireMCPRequest(maxBodyBytes int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mediaType, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		if req.ContentLength > maxBodyBytes {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		body, err := io.ReadAll(io.LimitReader(req.Body, maxBodyBytes+1))
		if err != nil {
			http.Error(w, "read request body", http.StatusBadRequest)
			return
		}
		_ = req.Body.Close()
		if int64(len(body)) > maxBodyBytes {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		next.ServeHTTP(w, req)
	})
}

func limitConcurrent(semaphore chan struct{}, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case semaphore <- struct{}{}:
			defer func() { <-semaphore }()
			next.ServeHTTP(w, req)
		default:
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "concurrency_limit"})
		}
	})
}

func protectHostAndOrigin(allowedHosts, allowedOrigins map[string]struct{}, delegateDynamicAdminOrigin bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		host, err := normalizeHost(req.Host)
		if err != nil {
			http.Error(w, "invalid Host", http.StatusBadRequest)
			return
		}
		if _, ok := allowedHosts[host]; !ok {
			http.Error(w, "forbidden Host", http.StatusForbidden)
			return
		}
		if rawOrigin := req.Header.Get("Origin"); rawOrigin != "" {
			origin, err := normalizeOrigin(rawOrigin)
			if err != nil {
				http.Error(w, "invalid Origin", http.StatusForbidden)
				return
			}
			_, explicitlyAllowed := allowedOrigins[origin]
			dynamicAdminOrigin := delegateDynamicAdminOrigin && len(allowedOrigins) == 0 && (req.URL.Path == "/admin" || strings.HasPrefix(req.URL.Path, "/admin/"))
			if !explicitlyAllowed && !dynamicAdminOrigin {
				http.Error(w, "forbidden Origin", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, req)
	})
}

func normalizeHosts(values []string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		host, err := normalizeHost(value)
		if err != nil {
			return nil, fmt.Errorf("allowed host %q: %w", value, err)
		}
		result[host] = struct{}{}
	}
	return result, nil
}

func normalizeHost(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("host is empty")
	}
	if ip := net.ParseIP(value); ip != nil {
		return ip.String(), nil
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip.String(), nil
		}
		value = host
	} else if strings.Contains(value, ":") {
		return "", errors.New("host has an invalid port")
	}
	value = strings.TrimSuffix(strings.ToLower(value), ".")
	if value == "" || strings.ContainsAny(value, " /\\") {
		return "", errors.New("host is invalid")
	}
	return value, nil
}

func normalizeOrigins(values []string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		origin, err := normalizeOrigin(value)
		if err != nil {
			return nil, fmt.Errorf("allowed origin %q: %w", value, err)
		}
		result[origin] = struct{}{}
	}
	return result, nil
}

func normalizeOrigin(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("origin must be an absolute http or https origin")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("origin must not contain credentials, a path, query, or fragment")
	}
	host, err := normalizeHost(parsed.Host)
	if err != nil {
		return "", err
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := parsed.Port(); port != "" {
		host += ":" + port
	}
	return strings.ToLower(parsed.Scheme) + "://" + host, nil
}

type requestIDKey struct{}

func correlateAndLog(logger *slog.Logger, trustedProxies []netip.Prefix, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requestID := newRequestID()
		ctx := context.WithValue(req.Context(), requestIDKey{}, requestID)
		ctx = remotepolicy.WithCorrelationID(ctx, requestID)
		req = req.WithContext(ctx)
		w.Header().Set("X-Request-ID", requestID)
		recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		started := time.Now()
		next.ServeHTTP(recorder, req)
		logger.InfoContext(ctx, "http request",
			"request_id", requestID,
			"method", req.Method,
			"path", safeLogPath(req.URL.Path),
			"status", recorder.status,
			"response_bytes", recorder.bytes,
			"duration_ms", time.Since(started).Milliseconds(),
			"client_ip", clientIP(req, trustedProxies),
		)
	})
}

func safeLogPath(path string) string {
	switch path {
	case "/mcp", "/healthz", "/readyz", "/admin":
		return path
	default:
		return "other"
	}
}

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value[:])
}

func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func clientIP(req *http.Request, trustedProxies []netip.Prefix) string {
	remote := addressFromRemote(req.RemoteAddr)
	if !addressTrusted(remote, trustedProxies) {
		return remote.String()
	}
	forwarded := strings.Split(req.Header.Get("X-Forwarded-For"), ",")
	if len(forwarded) > 0 {
		if address, err := netip.ParseAddr(strings.TrimSpace(forwarded[0])); err == nil {
			return address.Unmap().String()
		}
	}
	return remote.String()
}

func addressFromRemote(value string) netip.Addr {
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		host = value
	}
	address, _ := netip.ParseAddr(strings.TrimSpace(host))
	return address.Unmap()
}

func addressTrusted(address netip.Addr, trustedProxies []netip.Prefix) bool {
	if !address.IsValid() {
		return false
	}
	for _, prefix := range trustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

type responseRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	written, err := r.ResponseWriter.Write(data)
	r.bytes += written
	return written, err
}

func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
