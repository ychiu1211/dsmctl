package synologyauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/derekvery666/dsmctl/internal/gateway/platformauth"
	"github.com/derekvery666/dsmctl/internal/weblogin"
)

var ErrUnauthorized = errors.New("DSM administrator authentication failed")

const gatewayAdministratorCookie = "dsmctl_admin_session"

const (
	dsmLoginPath         = "/admin/api/dsm-login"
	dsmLoginStartPath    = "/admin/api/dsm-login/start"
	dsmLoginCompletePath = "/admin/api/dsm-login/complete"
	defaultDSMHTTPSPort  = "5001"
	defaultDSMHTTPPort   = "5000"
	adminSessionName     = "dsmctl-gateway-admin"
	maxEnrollments       = 128
	enrollmentLifetime   = 5 * time.Minute
)

type Validator interface {
	Validate(*http.Request) (string, error)
}

type SubjectValidator interface {
	ValidateSubject(context.Context, string) error
}

type Options struct {
	Backend          *url.URL
	Signer           *platformauth.Signer
	Validator        Validator
	SubjectValidator SubjectValidator
	Logger           *slog.Logger
	RequireLoopback  bool
	// RedirectForwardedHTTP upgrades Web Station requests that explicitly
	// arrived over HTTP. Requests without X-Forwarded-Proto are private
	// loopback traffic and remain available for package health checks.
	RedirectForwardedHTTP bool
	DSMHTTPSPort          string
	DSMHTTPPort           string
}

type pendingEnrollment struct {
	flow      *weblogin.Enrollment
	expiresAt time.Time
}

type enrollmentStore struct {
	mu      sync.Mutex
	now     func() time.Time
	pending map[string]pendingEnrollment
}

func newEnrollmentStore() *enrollmentStore {
	return &enrollmentStore{now: time.Now, pending: make(map[string]pendingEnrollment)}
}

func (s *enrollmentStore) put(flow *weblogin.Enrollment) (string, error) {
	identifierBytes := make([]byte, 24)
	if _, err := rand.Read(identifierBytes); err != nil {
		return "", err
	}
	identifier := base64.RawURLEncoding.EncodeToString(identifierBytes)
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, value := range s.pending {
		if !value.expiresAt.After(now) {
			delete(s.pending, id)
		}
	}
	if len(s.pending) >= maxEnrollments {
		return "", errors.New("too many pending DSM logins")
	}
	s.pending[identifier] = pendingEnrollment{flow: flow, expiresAt: now.Add(enrollmentLifetime)}
	return identifier, nil
}

func (s *enrollmentStore) take(identifier string) (*weblogin.Enrollment, bool) {
	if len(identifier) != 32 || strings.TrimSpace(identifier) != identifier {
		return nil, false
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.pending[identifier]
	delete(s.pending, identifier)
	if !ok || !value.expiresAt.After(now) {
		return nil, false
	}
	return value.flow, true
}

func New(options Options) (http.Handler, error) {
	if options.Backend == nil || options.Backend.Scheme != "http" || options.Backend.Host == "" || options.Backend.User != nil {
		return nil, errors.New("Synology auth backend must be an absolute private HTTP URL")
	}
	if options.Signer == nil || options.Validator == nil || options.SubjectValidator == nil {
		return nil, errors.New("Synology auth signer and DSM validators are required")
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.DSMHTTPSPort == "" {
		options.DSMHTTPSPort = defaultDSMHTTPSPort
	}
	if options.DSMHTTPPort == "" {
		options.DSMHTTPPort = defaultDSMHTTPPort
	}
	if err := validatePort(options.DSMHTTPSPort); err != nil {
		return nil, err
	}
	if err := validatePort(options.DSMHTTPPort); err != nil {
		return nil, err
	}
	enrollments := newEnrollmentStore()
	proxy := httputil.NewSingleHostReverseProxy(options.Backend)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = options.Backend.Host
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		options.Logger.Error("gateway backend unavailable", "path", safePath(req.URL.Path), "error", err)
		http.Error(w, "gateway backend unavailable", http.StatusBadGateway)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if options.RequireLoopback && !requestFromLoopback(req) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		forwardedHost := strings.TrimSpace(req.Header.Get("X-Forwarded-Host"))
		if forwardedHost == "" {
			forwardedHost = req.Host
		}
		forwardedProtoHeader := strings.TrimSpace(req.Header.Get("X-Forwarded-Proto"))
		forwardedProto := forwardedProtoHeader
		if forwardedProto != "http" && forwardedProto != "https" {
			forwardedProto = "http"
			if req.TLS != nil {
				forwardedProto = "https"
			}
		}
		// Resolve and strip the deployment path prefix BEFORE any redirect so the
		// http->https redirect can restore it. Web Station forwards
		// X-Forwarded-Prefix=/dsmctl and, depending on the DSM release, may
		// already strip that prefix from the path; stripping here normalizes
		// req.URL.Path to the un-prefixed form in both cases.
		prefix := strings.TrimRight(strings.TrimSpace(req.Header.Get("X-Forwarded-Prefix")), "/")
		if !safeForwardedPrefix(prefix) {
			prefix = ""
		}
		if prefix != "" && strings.HasPrefix(req.URL.Path, prefix+"/") {
			req.URL.Path = strings.TrimPrefix(req.URL.Path, prefix)
		}
		if options.RedirectForwardedHTTP && forwardedProtoHeader == "http" {
			location, err := forwardedHTTPSLocation(forwardedHost, prefix, req.URL)
			if err != nil {
				http.Error(w, "invalid forwarded host", http.StatusBadRequest)
				return
			}
			http.Redirect(w, req, location, http.StatusPermanentRedirect)
			return
		}
		if req.URL.Path == dsmLoginStartPath {
			serveDSMLoginStart(w, req, forwardedProto, forwardedHost, prefix, options, enrollments)
			return
		}
		if req.URL.Path == dsmLoginCompletePath {
			gatewayOrigin, _, originErr := forwardedGatewayOrigin(forwardedProto, forwardedHost)
			if originErr != nil || !dsmLoginOriginAllowed(req, gatewayOrigin) {
				options.Logger.Warn("DSM administrator Web Login denied", "reason", "invalid_origin")
				writeJSONError(w, http.StatusForbidden, "invalid_origin", "DSM administrator login origin does not match")
				return
			}
			subject, reason := completeDSMLogin(req, enrollments, options.SubjectValidator)
			if reason != "" {
				options.Logger.Warn("DSM administrator Web Login denied", "reason", reason)
				writeJSONError(w, http.StatusUnauthorized, "dsm_login_required", "DSM administrator Web Login required")
				return
			}
			assertion, err := options.Signer.Sign(subject)
			if err != nil {
				options.Logger.Error("sign DSM administrator assertion", "error", err)
				writeJSONError(w, http.StatusServiceUnavailable, "login_unavailable", "DSM Web Login is unavailable")
				return
			}
			req.URL.Path = dsmLoginPath
			req.URL.RawPath = ""
			req.Body = io.NopCloser(strings.NewReader("{}"))
			req.ContentLength = 2
			req.Header.Del("Content-Length")
			req.Header.Del(platformauth.HeaderName)
			req.Header.Set(platformauth.HeaderName, assertion)
			retainGatewayCookie(req)
			setForwardedHeaders(req, forwardedProto, forwardedHost, prefix)
			proxy.ServeHTTP(w, req)
			return
		}

		// DSM credentials belong only to the host adapter. The container receives
		// the Gateway session cookie and, only during login, a signed identity
		// assertion created above from the PKCE code exchange.
		req.Header.Del(platformauth.HeaderName)
		if req.URL.Path == dsmLoginPath {
			writeJSONError(w, http.StatusUnauthorized, "dsm_login_required", "DSM administrator Web Login required")
			return
		}
		if req.URL.Path == "/oauth/authorize" && req.Method == http.MethodPost {
			if subject, err := options.Validator.Validate(req); err == nil {
				if assertion, signErr := options.Signer.Sign(subject); signErr == nil {
					req.Header.Set(platformauth.HeaderName, assertion)
				}
			}
		}
		retainGatewayCookie(req)
		setForwardedHeaders(req, forwardedProto, forwardedHost, prefix)
		proxy.ServeHTTP(w, req)
	}), nil
}

func serveDSMLoginStart(w http.ResponseWriter, req *http.Request, forwardedProto, forwardedHost, prefix string, options Options, store *enrollmentStore) {
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	gatewayOrigin, host, err := forwardedGatewayOrigin(forwardedProto, forwardedHost)
	if err != nil || !safeForwardedPrefix(prefix) {
		writeJSONError(w, http.StatusBadRequest, "invalid_gateway_host", "cannot determine DSM login address")
		return
	}
	if !dsmLoginOriginAllowed(req, gatewayOrigin) {
		writeJSONError(w, http.StatusForbidden, "invalid_origin", "DSM administrator login origin does not match")
		return
	}
	dsmOrigin := "https://" + net.JoinHostPort(host, options.DSMHTTPSPort)
	openerURL := gatewayOrigin + prefix + "/admin/"
	flow, start, err := weblogin.BeginEnrollment(dsmOrigin, openerURL, weblogin.Options{
		SessionName: adminSessionName, ExchangeBaseURL: "http://127.0.0.1:" + options.DSMHTTPPort,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	})
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "login_unavailable", "DSM Web Login is unavailable")
		return
	}
	enrollmentID, err := store.put(flow)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "login_unavailable", "DSM Web Login is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"login_url": start.LoginURL, "nas_origin": dsmOrigin, "state": start.State, "enrollment_id": enrollmentID,
	})
}

func completeDSMLogin(req *http.Request, store *enrollmentStore, validator SubjectValidator) (string, string) {
	if req.Method != http.MethodPost {
		return "", "method_not_allowed"
	}
	var input struct {
		EnrollmentID string `json:"enrollment_id"`
		Code         string `json:"code"`
		RS           string `json:"rs"`
		State        string `json:"state"`
	}
	decoder := json.NewDecoder(io.LimitReader(req.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.Code) == "" || strings.TrimSpace(input.RS) == "" {
		return "", "invalid_callback"
	}
	flow, ok := store.take(input.EnrollmentID)
	if !ok {
		return "", "unknown_enrollment"
	}
	ctx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
	defer cancel()
	result, err := flow.Complete(ctx, input.Code, input.RS, input.State)
	if err != nil {
		return "", "code_exchange_failed"
	}
	if err := validator.ValidateSubject(ctx, result.Account); err != nil {
		return "", "not_administrator"
	}
	return result.Account, ""
}

func forwardedGatewayOrigin(proto, authority string) (string, string, error) {
	if proto != "http" && proto != "https" {
		return "", "", errors.New("invalid Gateway scheme")
	}
	host, err := forwardedHostname(authority)
	if err != nil {
		return "", "", err
	}
	if _, port, splitErr := net.SplitHostPort(authority); splitErr == nil {
		if err := validatePort(port); err != nil {
			return "", "", err
		}
	}
	return proto + "://" + authority, host, nil
}

// dsmLoginOriginAllowed enforces a same-origin allowlist on the DSM login
// bridge endpoints. Unlike other /admin/api/* mutations, /admin/api/dsm-login
// /{start,complete} are handled by the reverse proxy before reaching the admin
// backend, so they never pass through validateBrowserMutation. Both legitimate
// callers - the Admin UI page and the OAuth consent page - are served from the
// forwarded Gateway origin and issue same-origin fetches, so a browser attaches
// an Origin header equal to that origin. Requiring an exact match (mirroring the
// admin backend's Origin comparison) is defense-in-depth against cross-origin or
// non-browser callers; the empty-Origin case fails closed.
func dsmLoginOriginAllowed(req *http.Request, gatewayOrigin string) bool {
	origin := strings.TrimRight(strings.TrimSpace(req.Header.Get("Origin")), "/")
	return origin != "" && origin == gatewayOrigin
}

func forwardedHTTPSLocation(authority, prefix string, target *url.URL) (string, error) {
	host, err := forwardedHostname(authority)
	if err != nil {
		return "", err
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	requestURI := target.RequestURI()
	if requestURI == "" || !strings.HasPrefix(requestURI, "/") {
		return "", errors.New("invalid redirect target")
	}
	// Restore the deployment path prefix (stripped above) so the redirect keeps
	// the portal alias, e.g. http://nas/dsmctl/x -> https://nas/dsmctl/x. Web
	// Station alias portals use the default 80/443 web ports, so do not carry an
	// HTTP authority's :80 into the HTTPS redirect.
	return "https://" + host + prefix + requestURI, nil
}

func safeForwardedPrefix(prefix string) bool {
	return prefix == "" || (strings.HasPrefix(prefix, "/") && !strings.Contains(prefix, "..") && !strings.ContainsAny(prefix, "\\?#%\x00\r\n\t "))
}

func setForwardedHeaders(req *http.Request, proto, host, prefix string) {
	req.Header.Set("X-Forwarded-Proto", proto)
	req.Header.Set("X-Forwarded-Host", host)
	if prefix != "" {
		req.Header.Set("X-Forwarded-Prefix", prefix)
	} else {
		req.Header.Del("X-Forwarded-Prefix")
	}
}

func forwardedHostname(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "/\\@?#%\x00\r\n\t ") {
		return "", errors.New("invalid forwarded host")
	}
	host := value
	if parsedHost, _, err := net.SplitHostPort(value); err == nil {
		host = parsedHost
	} else if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	} else if strings.Count(value, ":") == 1 {
		return "", errors.New("forwarded host has an invalid port")
	}
	if host == "" || strings.ContainsAny(host, "[]") {
		return "", errors.New("invalid forwarded host")
	}
	return host, nil
}

func validatePort(value string) error {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != value {
		return errors.New("DSM port must be an integer from 1 to 65535")
	}
	return nil
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "error": message})
}

func retainGatewayCookie(req *http.Request) {
	var values []string
	for _, cookie := range req.Cookies() {
		if cookie.Name == gatewayAdministratorCookie {
			values = append(values, cookie.Name+"="+cookie.Value)
		}
	}
	if len(values) == 0 {
		req.Header.Del("Cookie")
		return
	}
	req.Header.Set("Cookie", strings.Join(values, "; "))
}

func requestFromLoopback(req *http.Request) bool {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	address := net.ParseIP(strings.TrimSpace(host))
	return address != nil && address.IsLoopback()
}

func safePath(path string) string {
	switch {
	case path == "/mcp", path == "/healthz", path == "/readyz", path == "/oauth/authorize", strings.HasPrefix(path, "/admin"):
		return path
	default:
		return "other"
	}
}
