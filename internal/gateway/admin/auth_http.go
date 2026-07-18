package admin

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ychiu1211/dsmctl/internal/gateway/state"
)

type attemptWindow struct {
	started time.Time
	count   int
}

type attemptLimiter struct {
	mu       sync.Mutex
	now      func() time.Time
	limit    int
	duration time.Duration
	windows  map[string]attemptWindow
}

const maxAttemptWindows = 4096

func newAttemptLimiter(now func() time.Time, limit int, duration time.Duration) *attemptLimiter {
	return &attemptLimiter{now: now, limit: limit, duration: duration, windows: make(map[string]attemptWindow)}
}

func (limiter *attemptLimiter) Allow(key string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.now().UTC()
	if len(limiter.windows) >= maxAttemptWindows {
		for candidate, window := range limiter.windows {
			if !now.Before(window.started.Add(limiter.duration)) {
				delete(limiter.windows, candidate)
			}
		}
		if len(limiter.windows) >= maxAttemptWindows {
			for candidate := range limiter.windows {
				delete(limiter.windows, candidate)
				break
			}
		}
	}
	window := limiter.windows[key]
	if window.started.IsZero() || !now.Before(window.started.Add(limiter.duration)) {
		limiter.windows[key] = attemptWindow{started: now, count: 1}
		return true
	}
	if window.count >= limiter.limit {
		return false
	}
	window.count++
	limiter.windows[key] = window
	return true
}

func (limiter *attemptLimiter) Reset(key string) {
	limiter.mu.Lock()
	delete(limiter.windows, key)
	limiter.mu.Unlock()
}

func remoteAttemptKey(req *http.Request, suffix string) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	return strings.TrimSpace(host) + "\x00" + strings.ToLower(strings.TrimSpace(suffix))
}

func (h *Handler) validateBrowserMutation(req *http.Request) error {
	if req.Header.Get(requestHeader) != "1" {
		return errors.New("missing browser request header")
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(req.Header.Get("Content-Type"), ";")[0]))
	if contentType != "application/json" {
		return errors.New("administrator mutations require application/json")
	}
	origin := strings.TrimRight(strings.TrimSpace(req.Header.Get("Origin")), "/")
	if origin == "" || origin != h.externalOrigin(req) {
		return errors.New("administrator mutation origin does not match")
	}
	return nil
}

func (h *Handler) externalOrigin(req *http.Request) string {
	if h.publicURL != "" {
		return h.publicURL
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(req.Header.Get("X-Forwarded-Proto")); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	host := req.Host
	if forwarded := strings.TrimSpace(req.Header.Get("X-Forwarded-Host")); forwarded != "" && !strings.ContainsAny(forwarded, "\r\n/\\") {
		host = forwarded
	}
	return scheme + "://" + host
}

func (h *Handler) secureCookie(req *http.Request) bool {
	parsed, _ := url.Parse(h.externalOrigin(req))
	return parsed != nil && parsed.Scheme == "https"
}

func (h *Handler) setAdministratorCookie(w http.ResponseWriter, req *http.Request, token string, expiresAt time.Time) {
	maxAge := int(expiresAt.Sub(h.now().UTC()).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name: administratorCookie, Value: token, Path: h.administratorCookiePath(req), Expires: expiresAt,
		MaxAge: maxAge, HttpOnly: true,
		Secure: h.secureCookie(req), SameSite: http.SameSiteStrictMode,
	})
}

func (h *Handler) clearAdministratorCookie(w http.ResponseWriter, req *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: administratorCookie, Value: "", Path: h.administratorCookiePath(req), MaxAge: -1,
		Expires: time.Unix(1, 0), HttpOnly: true, Secure: h.secureCookie(req),
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *Handler) administratorCookiePath(req *http.Request) string {
	prefix := strings.TrimRight(strings.TrimSpace(req.Header.Get("X-Forwarded-Prefix")), "/")
	if prefix != "" && strings.HasPrefix(prefix, "/") && !strings.ContainsAny(prefix, "\r\n\\?") {
		return prefix + "/admin"
	}
	return "/admin"
}

func (h *Handler) login(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(req, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	attemptKey := remoteAttemptKey(req, "login")
	if !h.loginAttempts.Allow(attemptKey) {
		input.Password = ""
		writeError(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}
	if err := h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", Action: "admin.login", Outcome: "started"}); err != nil {
		input.Password = ""
		writeError(w, http.StatusServiceUnavailable, "audit storage unavailable")
		return
	}
	token, session, err := h.repository.LoginAdministrator(req.Context(), input.Username, input.Password)
	input.Password = ""
	if err != nil {
		_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", Action: "admin.login", Outcome: "denied", Reason: "denied"})
		writeError(w, http.StatusUnauthorized, "invalid administrator credentials")
		return
	}
	h.loginAttempts.Reset(attemptKey)
	h.setAdministratorCookie(w, req, token, session.ExpiresAt)
	_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", ActorID: "local:" + session.Username, Action: "admin.login", Outcome: "success"})
	writeJSON(w, http.StatusOK, map[string]any{"session": session})
}

func (h *Handler) session(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	token, _ := req.Context().Value(sessionTokenContextKey{}).(string)
	session, err := h.repository.AuthenticateAdministratorSession(req.Context(), token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": session})
}

func (h *Handler) logout(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	token, _ := req.Context().Value(sessionTokenContextKey{}).(string)
	if err := h.repository.LogoutAdministrator(req.Context(), token); err != nil {
		writeError(w, http.StatusInternalServerError, "logout failed")
		return
	}
	h.clearAdministratorCookie(w, req)
	writeJSON(w, http.StatusOK, map[string]bool{"logged_out": true})
}

func (h *Handler) changePassword(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPut {
		methodNotAllowed(w, http.MethodPut)
		return
	}
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(req, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	token, _ := req.Context().Value(sessionTokenContextKey{}).(string)
	err := h.repository.ChangeAdministratorPassword(req.Context(), token, input.CurrentPassword, input.NewPassword)
	input.CurrentPassword, input.NewPassword = "", ""
	if err != nil {
		if errors.Is(err, state.ErrUnauthorized) {
			writeError(w, http.StatusUnauthorized, "current password is invalid")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"password_changed": true, "other_sessions_revoked": true})
}

func (h *Handler) revokeOtherSessions(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	token, _ := req.Context().Value(sessionTokenContextKey{}).(string)
	if err := h.repository.RevokeOtherAdministratorSessions(req.Context(), token); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"other_sessions_revoked": true})
}
