package admin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
	"github.com/ychiu1211/dsmctl/internal/gateway/state"
	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
	"github.com/ychiu1211/dsmctl/internal/runtime"
	"github.com/ychiu1211/dsmctl/internal/synology"
	"github.com/ychiu1211/dsmctl/internal/tlstrust"
	"github.com/ychiu1211/dsmctl/internal/webassets"
	"github.com/ychiu1211/dsmctl/internal/weblogin"
)

const (
	maxAdminBody        = int64(1 << 20)
	enrollmentTTL       = 5 * time.Minute
	networkTimeout      = 10 * time.Second
	defaultSetupWindow  = time.Hour
	administratorCookie = "dsmctl_admin_session"
	requestHeader       = "X-DSMCTL-Request"
)

type Options struct {
	Repository  *state.Repository
	Manager     *runtime.Manager
	Discoverer  DeviceDiscoverer
	PublicURL   string
	Now         func() time.Time
	SetupWindow time.Duration
	// Logger receives server-side diagnostics for failures whose HTTP
	// responses are deliberately redacted (DSM enrollment exchanges).
	Logger *slog.Logger
}

type DeviceDiscoverer interface {
	DiscoverDevices(context.Context, discovery.Query) (application.DiscoverResult, error)
}

type Handler struct {
	repository     *state.Repository
	manager        *runtime.Manager
	discoverer     DeviceDiscoverer
	publicURL      string
	logger         *slog.Logger
	now            func() time.Time
	setupDeadline  time.Time
	setupAttempts  *attemptLimiter
	loginAttempts  *attemptLimiter
	exportAttempts *attemptLimiter

	pendingMu sync.Mutex
	pending   map[string]pendingEnrollment
}

type pendingEnrollment struct {
	ProfileName string
	Enrollment  *weblogin.Enrollment
	ExpiresAt   time.Time
}

type actorContextKey struct{}
type sessionTokenContextKey struct{}

type profileInput struct {
	state.ProfileInput
	ConfirmCertificateFingerprint bool `json:"confirm_certificate_fingerprint,omitempty"`
}

func (input profileInput) validateFingerprintConfirmation() error {
	if input.TLSMode == state.TLSPinnedFingerprint && !input.ConfirmCertificateFingerprint {
		return errors.New("pinned_fingerprint TLS mode requires explicit certificate fingerprint confirmation")
	}
	return nil
}

func New(options Options) (*Handler, error) {
	if options.Repository == nil {
		return nil, errors.New("gateway state repository is required")
	}
	if options.Manager == nil {
		return nil, errors.New("gateway runtime manager is required")
	}
	publicURL := strings.TrimRight(strings.TrimSpace(options.PublicURL), "/")
	if publicURL != "" {
		parsed, err := url.Parse(publicURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			return nil, errors.New("admin public URL must be an absolute http or https origin")
		}
		publicURL = parsed.Scheme + "://" + parsed.Host
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	setupWindow := options.SetupWindow
	if setupWindow == 0 {
		setupWindow = defaultSetupWindow
	}
	if setupWindow < 0 {
		return nil, errors.New("administrator setup window must not be negative")
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	startedAt := now().UTC()
	discoverer := options.Discoverer
	if discoverer == nil {
		discoverer = application.NewService(nil, options.Manager)
	}
	return &Handler{
		repository: options.Repository, manager: options.Manager, discoverer: discoverer, publicURL: publicURL, logger: logger,
		now: now, setupDeadline: startedAt.Add(setupWindow),
		setupAttempts:  newAttemptLimiter(now, 10, time.Minute),
		loginAttempts:  newAttemptLimiter(now, 5, time.Minute),
		exportAttempts: newAttemptLimiter(now, 5, time.Minute),
		pending:        make(map[string]pendingEnrollment),
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasPrefix(req.URL.Path, "/admin/api/") {
		if req.ContentLength > maxAdminBody {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		req.Body = http.MaxBytesReader(w, req.Body, maxAdminBody)
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			if err := h.validateBrowserMutation(req); err != nil {
				writeError(w, http.StatusForbidden, "forbidden browser request")
				return
			}
		}
	}
	switch req.URL.Path {
	case "/admin/favicon.svg":
		webassets.ServeFavicon(w, req)
		return
	case "/admin", "/admin/":
		if req.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		_, _ = io.WriteString(w, indexHTML)
		return
	case "/admin/api/setup/status":
		h.setupStatus(w, req)
		return
	case "/admin/api/setup":
		h.setup(w, req)
		return
	case "/admin/api/login":
		h.login(w, req)
		return
	}
	if !strings.HasPrefix(req.URL.Path, "/admin/api/") {
		http.NotFound(w, req)
		return
	}
	actorID, sessionToken, err := h.authenticate(req)
	if err != nil {
		_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", Action: adminAuditAction(req), Outcome: "denied", Reason: "denied"})
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	action := adminAuditAction(req)
	mutating := req.Method != http.MethodGet && req.Method != http.MethodHead
	if mutating {
		if err := h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", ActorID: actorID, Action: action, Outcome: "started"}); err != nil {
			writeError(w, http.StatusServiceUnavailable, "audit storage unavailable")
			return
		}
	}
	recorder := &auditResponseWriter{ResponseWriter: w, status: http.StatusOK}
	requestContext := context.WithValue(req.Context(), actorContextKey{}, actorID)
	requestContext = context.WithValue(requestContext, sessionTokenContextKey{}, sessionToken)
	req = req.WithContext(requestContext)
	h.serveAuthenticated(recorder, req)
	outcome := "success"
	if recorder.status >= 400 {
		outcome = "failure"
	}
	_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", ActorID: actorID, Action: action, Outcome: outcome})
}

func (h *Handler) serveAuthenticated(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/admin/api/status" {
		h.status(w, req)
		return
	}
	if req.URL.Path == "/admin/api/session" {
		h.session(w, req)
		return
	}
	if req.URL.Path == "/admin/api/logout" {
		h.logout(w, req)
		return
	}
	if req.URL.Path == "/admin/api/password" {
		h.changePassword(w, req)
		return
	}
	if req.URL.Path == "/admin/api/sessions/revoke-others" {
		h.revokeOtherSessions(w, req)
		return
	}
	if req.URL.Path == "/admin/api/mcp-tokens" || strings.HasPrefix(req.URL.Path, "/admin/api/mcp-tokens/") {
		h.mcpTokens(w, req)
		return
	}
	if req.URL.Path == "/admin/api/approvals" {
		h.approvals(w, req)
		return
	}
	if req.URL.Path == "/admin/api/approval-requests" || strings.HasPrefix(req.URL.Path, "/admin/api/approval-requests/") {
		h.approvalRequests(w, req)
		return
	}
	if req.URL.Path == "/admin/api/audit" || req.URL.Path == "/admin/api/audit/export" {
		h.audit(w, req)
		return
	}
	if req.URL.Path == "/admin/api/credentials/export" {
		h.exportCredentials(w, req)
		return
	}
	if req.URL.Path == "/admin/api/orphan-secrets" || strings.HasPrefix(req.URL.Path, "/admin/api/orphan-secrets/") {
		h.orphanSecrets(w, req)
		return
	}
	if req.URL.Path == "/admin/api/discovery" {
		h.discoverLAN(w, req)
		return
	}
	if req.URL.Path == "/admin/api/profiles" {
		h.profiles(w, req)
		return
	}
	if strings.HasPrefix(req.URL.Path, "/admin/api/profiles/") {
		h.profile(w, req)
		return
	}
	http.NotFound(w, req)
}

func (h *Handler) discoverLAN(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input struct {
		TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	}
	if err := decodeJSON(req, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.TimeoutSeconds < 0 || input.TimeoutSeconds > 60 {
		writeError(w, http.StatusBadRequest, "timeout_seconds must be between 0 and 60")
		return
	}
	result, err := h.discoverer.DiscoverDevices(req.Context(), discovery.Query{Timeout: time.Duration(input.TimeoutSeconds) * time.Second})
	if err != nil {
		h.logger.Error("admin LAN discovery failed", "error", err)
		writeError(w, http.StatusBadGateway, "LAN discovery failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) authenticate(req *http.Request) (string, string, error) {
	cookie, err := req.Cookie(administratorCookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return "", "", state.ErrUnauthorized
	}
	session, err := h.repository.AuthenticateAdministratorSession(req.Context(), cookie.Value)
	if err != nil {
		return "", "", err
	}
	return "local:" + session.Username, cookie.Value, nil
}

func (h *Handler) setupStatus(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	status, err := h.repository.AdministratorStatus(req.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read administrator status")
		return
	}
	if status.Initialized {
		writeJSON(w, http.StatusOK, map[string]any{"state": "initialized", "initialized_at": status.InitializedAt})
		return
	}
	if !h.now().UTC().Before(h.setupDeadline) {
		writeJSON(w, http.StatusOK, map[string]any{"state": "setup_expired"})
		return
	}
	remaining := h.setupDeadline.Sub(h.now().UTC())
	if remaining < 0 {
		remaining = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": "setup_available", "setup_expires_at": h.setupDeadline, "setup_remaining_seconds": int64(remaining / time.Second)})
}

func (h *Handler) setup(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	status, err := h.repository.AdministratorStatus(req.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read administrator status")
		return
	}
	if status.Initialized {
		writeError(w, http.StatusConflict, "gateway is already initialized")
		return
	}
	if !h.now().UTC().Before(h.setupDeadline) {
		writeError(w, http.StatusGone, "administrator setup window expired; restart the uninitialized gateway")
		return
	}
	if !h.setupAttempts.Allow(remoteAttemptKey(req, "setup")) {
		writeError(w, http.StatusTooManyRequests, "too many setup attempts")
		return
	}
	if err := h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_setup", Action: "admin.setup", Outcome: "started"}); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit storage unavailable")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(req, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_setup", Action: "admin.setup", Outcome: "failure"})
		return
	}
	token, session, err := h.repository.CreateAdministrator(req.Context(), input.Username, input.Password)
	input.Password = ""
	if err != nil {
		responseStatus := http.StatusBadRequest
		if errors.Is(err, state.ErrAlreadyInitialized) {
			responseStatus = http.StatusConflict
		}
		writeError(w, responseStatus, err.Error())
		_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_setup", Action: "admin.setup", Outcome: "failure"})
		return
	}
	h.setAdministratorCookie(w, req, token, session.ExpiresAt)
	_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", ActorID: "local:" + session.Username, Action: "admin.setup", Outcome: "success"})
	writeJSON(w, http.StatusCreated, map[string]any{"session": session})
}

func (h *Handler) status(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	health, err := h.repository.Health(req.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read gateway status")
		return
	}
	writeJSON(w, http.StatusOK, health)
}

func (h *Handler) profiles(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		profiles, err := h.repository.Profiles(req.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list profiles")
			return
		}
		if profiles == nil {
			profiles = []state.Profile{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
	case http.MethodPost:
		var input profileInput
		if err := decodeJSON(req, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := input.validateFingerprintConfirmation(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Managed profile creation stores connection identity only. The actual
		// DSM account is committed by password/OTP or Web Login enrollment.
		input.Username = ""
		input.TimeoutSeconds = 0
		var created state.Profile
		err := h.manager.MutateProfile(req.Context(), input.Name, func() error {
			var err error
			created, err = h.repository.CreateProfile(req.Context(), input.ProfileInput)
			return err
		})
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h *Handler) profile(w http.ResponseWriter, req *http.Request) {
	relative := strings.TrimPrefix(req.URL.Path, "/admin/api/profiles/")
	parts := strings.Split(relative, "/")
	name, err := url.PathUnescape(parts[0])
	if err != nil || name == "" {
		writeError(w, http.StatusBadRequest, "invalid profile name")
		return
	}
	action := ""
	if len(parts) > 1 {
		action = strings.Join(parts[1:], "/")
	}
	switch action {
	case "":
		h.profileRecord(w, req, name)
	case "test":
		h.testProfile(w, req, name)
	case "tls":
		h.probeProfileTLS(w, req, name)
	case "tls/trust":
		h.trustProfileCertificate(w, req, name)
	case "credentials/status":
		h.credentialStatus(w, req, name)
	case "credentials/accounts":
		h.passwordAccounts(w, req, name)
	case "credentials/password":
		h.passwordEnrollment(w, req, name)
	case "credentials/password/reveal":
		h.revealPassword(w, req, name)
	case "provision":
		h.provisionProfile(w, req, name)
	case "credentials/session":
		h.removeSession(w, req, name)
	case "credentials/trusted-device":
		h.removeTrustedDevice(w, req, name)
	case "weblogin/start":
		h.startWebLogin(w, req, name)
	case "weblogin/complete":
		h.completeWebLogin(w, req, name)
	case "secrets":
		h.applySecrets(w, req, name, "")
	default:
		if strings.HasPrefix(action, "secrets/") {
			id, _ := url.PathUnescape(strings.TrimPrefix(action, "secrets/"))
			h.applySecrets(w, req, name, id)
			return
		}
		http.NotFound(w, req)
	}
}

func (h *Handler) mcpTokens(w http.ResponseWriter, req *http.Request) {
	relative := strings.TrimPrefix(req.URL.Path, "/admin/api/mcp-tokens")
	if relative == "" || relative == "/" {
		switch req.Method {
		case http.MethodGet:
			tokens, err := h.repository.MCPTokens(req.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "list MCP tokens")
				return
			}
			if tokens == nil {
				tokens = []state.MCPToken{}
			}
			writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
		case http.MethodPost:
			var input state.MCPTokenInput
			if err := decodeJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			issued, err := h.repository.CreateMCPToken(req.Context(), input)
			if err != nil {
				writeRepositoryError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, issued)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
		return
	}
	parts := strings.Split(strings.TrimPrefix(relative, "/"), "/")
	id, err := url.PathUnescape(parts[0])
	if err != nil || id == "" {
		writeError(w, http.StatusBadRequest, "invalid token identifier")
		return
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	switch action {
	case "":
		if req.Method == http.MethodGet {
			token, err := h.repository.MCPToken(req.Context(), id)
			if err != nil {
				writeRepositoryError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, token)
			return
		}
		if req.Method == http.MethodDelete {
			token, err := h.repository.RevokeMCPToken(req.Context(), id)
			if err != nil {
				writeRepositoryError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, token)
			return
		}
		methodNotAllowed(w, http.MethodGet, http.MethodDelete)
	case "rotate":
		if req.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		issued, err := h.repository.RotateMCPToken(req.Context(), id)
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, issued)
	case "expire":
		if req.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input struct {
			ExpiresAt time.Time `json:"expires_at"`
		}
		if err := decodeJSON(req, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		token, err := h.repository.ExpireMCPToken(req.Context(), id, input.ExpiresAt)
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, token)
	default:
		http.NotFound(w, req)
	}
}

func (h *Handler) approvals(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		items, err := h.repository.Approvals(req.Context(), req.URL.Query().Get("include_consumed") == "true")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list approvals")
			return
		}
		if items == nil {
			items = []state.Approval{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"approvals": items})
	case http.MethodPost:
		var input struct {
			PlanHash          string `json:"plan_hash"`
			NAS               string `json:"nas"`
			RequestingTokenID string `json:"requesting_token_id"`
			TTLSeconds        int    `json:"ttl_seconds,omitempty"`
		}
		if err := decodeJSON(req, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		approval, err := h.repository.CreateApproval(req.Context(), state.ApprovalInput{PlanHash: input.PlanHash, NAS: input.NAS, RequestingTokenID: input.RequestingTokenID, TTL: time.Duration(input.TTLSeconds) * time.Second}, administratorActor(req.Context()))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, approval)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h *Handler) approvalRequests(w http.ResponseWriter, req *http.Request) {
	relative := strings.TrimPrefix(req.URL.Path, "/admin/api/approval-requests")
	if relative == "" || relative == "/" {
		if req.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		items, err := h.repository.PendingApprovals(req.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list pending approvals")
			return
		}
		if items == nil {
			items = []state.PendingApproval{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"requests": items})
		return
	}
	parts := strings.Split(strings.TrimPrefix(relative, "/"), "/")
	id, err := url.PathUnescape(parts[0])
	if err != nil || id == "" {
		writeError(w, http.StatusBadRequest, "invalid approval request identifier")
		return
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	switch {
	case action == "approve" && req.Method == http.MethodPost:
		approval, err := h.repository.ApprovePendingApproval(req.Context(), id, administratorActor(req.Context()))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, approval)
	case action == "" && req.Method == http.MethodDelete:
		if err := h.repository.DismissPendingApproval(req.Context(), id); err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"dismissed": true})
	default:
		methodNotAllowed(w, http.MethodPost, http.MethodDelete)
	}
}

func administratorActor(ctx context.Context) string {
	actor, _ := ctx.Value(actorContextKey{}).(string)
	if actor == "" {
		return "unknown-admin"
	}
	return actor
}

func (h *Handler) audit(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	var after time.Time
	if value := req.URL.Query().Get("after"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "after must be RFC3339")
			return
		}
		after = parsed
	}
	if req.URL.Path == "/admin/api/audit/export" {
		events, err := h.repository.AuditExport(req.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "export audit events")
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="dsmctl-audit.jsonl"`)
		w.WriteHeader(http.StatusOK)
		encoder := json.NewEncoder(w)
		for _, event := range events {
			_ = encoder.Encode(event)
		}
		return
	}
	events, err := h.repository.AuditEvents(req.Context(), state.AuditQuery{After: after, Limit: limit, ActorID: req.URL.Query().Get("actor_id"), Action: req.URL.Query().Get("action")})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read audit events")
		return
	}
	if events == nil {
		events = []state.AuditEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *Handler) orphanSecrets(w http.ResponseWriter, req *http.Request) {
	id := strings.TrimPrefix(req.URL.Path, "/admin/api/orphan-secrets/")
	if req.URL.Path == "/admin/api/orphan-secrets" {
		id = ""
	}
	if id == "" {
		if req.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		secrets, err := h.repository.OrphanedSecrets(req.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list retained secrets")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"secrets": secrets})
		return
	}
	if req.Method != http.MethodDelete {
		methodNotAllowed(w, http.MethodDelete)
		return
	}
	decodedID, err := url.PathUnescape(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid secret identifier")
		return
	}
	removed, err := h.repository.DeleteOrphanedSecret(req.Context(), decodedID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": removed})
}

func (h *Handler) profileRecord(w http.ResponseWriter, req *http.Request, name string) {
	switch req.Method {
	case http.MethodGet:
		profile, err := h.repository.Profile(req.Context(), name)
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, profile)
	case http.MethodPut:
		var input struct {
			profileInput
			ExpectedRevision uint64 `json:"expected_revision"`
		}
		if err := decodeJSON(req, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := input.validateFingerprintConfirmation(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		current, err := h.repository.Profile(req.Context(), name)
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		if strings.TrimRight(strings.TrimSpace(input.URL), "/") != current.URL {
			// A pin identifies one observed certificate at one endpoint. Moving a
			// profile to another URL always starts again with system trust; the
			// administrator may pin only what the Gateway observes there.
			input.TLSMode = state.TLSSystemCA
			input.CertificateFingerprint = ""
			input.ConfirmCertificateFingerprint = false
		}
		input.Username = current.Username
		var updated state.Profile
		err = h.manager.MutateProfile(req.Context(), name, func() error {
			var err error
			updated, err = h.repository.UpdateProfile(req.Context(), name, input.ExpectedRevision, input.ProfileInput)
			return err
		})
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		revision, err := strconv.ParseUint(req.URL.Query().Get("revision"), 10, 64)
		if err != nil || revision == 0 {
			writeError(w, http.StatusBadRequest, "revision query parameter is required")
			return
		}
		retain := req.URL.Query().Get("retain_credentials") == "true"
		var removed state.Profile
		var revocationError string
		if !retain {
			revokeCtx, cancel := context.WithTimeout(req.Context(), networkTimeout)
			_, revokeErr := h.manager.RevokeStoredSession(revokeCtx, name)
			cancel()
			if revokeErr != nil {
				revocationError = revokeErr.Error()
			}
		}
		err = h.manager.MutateProfile(req.Context(), name, func() error {
			var err error
			removed, err = h.repository.DeleteProfile(req.Context(), name, revision, retain)
			return err
		})
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		response := map[string]any{"removed": removed.Name, "credentials_retained": retain}
		if revocationError != "" {
			response["session_revocation_error"] = revocationError
		}
		if retain {
			retained, _ := h.repository.SecretMetadataForProfile(req.Context(), removed.ID)
			response["retained_secrets"] = retained
		}
		writeJSON(w, http.StatusOK, response)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

type testStage struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Error  string `json:"error,omitempty"`
}

func (h *Handler) testProfile(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	profile, err := h.repository.Profile(req.Context(), name)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	parsed, _ := url.Parse(profile.URL)
	stages := make([]testStage, 0, 4)
	ctx, cancel := context.WithTimeout(req.Context(), networkTimeout)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupHost(ctx, parsed.Hostname())
	if err != nil || len(addresses) == 0 {
		stages = append(stages, failedStage("dns", err))
		writeJSON(w, http.StatusBadGateway, map[string]any{"nas": name, "stages": stages})
		return
	}
	stages = append(stages, testStage{Name: "dns", Passed: true})
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(parsed.Hostname(), port))
	if err != nil {
		stages = append(stages, failedStage("tcp", err))
		writeJSON(w, http.StatusBadGateway, map[string]any{"nas": name, "stages": stages})
		return
	}
	_ = connection.Close()
	stages = append(stages, testStage{Name: "tcp", Passed: true})
	if err := probeTLS(ctx, profile); err != nil {
		stages = append(stages, failedStage("tls_http", err))
		if h.writeTLSProbeError(w, profile, err, map[string]any{"nas": name, "stages": stages}) {
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"nas": name, "stages": stages})
		return
	}
	cfg, _ := h.repository.Snapshot(ctx)
	runtimeProfile := cfg.NAS[name]
	httpRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, profile.URL+"/webapi/query.cgi?api=SYNO.API.Info&version=1&method=query&query=SYNO.API.Auth", nil)
	response, err := runtime.HTTPClient(runtimeProfile).Do(httpRequest)
	if err != nil {
		stages = append(stages, failedStage("tls_http", err))
		writeJSON(w, http.StatusBadGateway, map[string]any{"nas": name, "stages": stages})
		return
	}
	_ = response.Body.Close()
	stages = append(stages, testStage{Name: "tls_http", Passed: true})
	// A connection test verifies reachability + credentials for any profile,
	// including a destination-only target you hold credentials for but do not
	// manage, so it uses the destination client (no managed-role gate).
	_, client, err := h.manager.DestinationClient(ctx, name)
	if err == nil {
		err = client.Authenticate(ctx)
	}
	if err == nil {
		_, err = client.SystemInfo(ctx)
	}
	if err != nil {
		stages = append(stages, failedStage("dsm", err))
		writeJSON(w, http.StatusBadGateway, map[string]any{"nas": name, "stages": stages})
		return
	}
	stages = append(stages, testStage{Name: "dsm", Passed: true})
	writeJSON(w, http.StatusOK, map[string]any{"nas": name, "stages": stages})
}

func (h *Handler) credentialStatus(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	profile, err := h.repository.Profile(req.Context(), name)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	meta, err := h.repository.SessionMeta(req.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read credential status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nas": profile.Name, "revision": profile.Revision,
		"password_stored": profile.PasswordStored, "trusted_device_stored": profile.TrustedDeviceStored,
		"session": meta,
	})
}

// passwordAccounts lists the account entries in a NAS's password book. It returns
// only account labels and provenance timestamps — never a plaintext password — so
// it drives the console "book" view for multi-account profiles without exposing
// any secret. It is never wired to the MCP surface.
func (h *Handler) passwordAccounts(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	accounts, err := h.repository.PasswordAccounts(req.Context(), name)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	if accounts == nil {
		accounts = []state.AccountCredentialInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"nas": name, "accounts": accounts})
}

// revealPassword returns the stored plaintext password for a NAS to the
// signed-in administrator. It is admin-session-gated (like every other route
// here) and audited as credential.reveal; it is never exposed on the MCP
// surface. POST so the plaintext response is not cached. An optional {account}
// body selects one entry from a multi-account password book (default: primary).
func (h *Handler) revealPassword(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input struct {
		Account string `json:"account,omitempty"`
	}
	if err := decodeJSON(req, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	account := strings.TrimSpace(input.Account)
	password, err := h.repository.RevealPasswordForAccount(req.Context(), name, account)
	if err != nil {
		if errors.Is(err, state.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no password stored for this NAS")
			return
		}
		writeRepositoryError(w, err)
		return
	}
	// Reveal discloses a plaintext secret, so record a precise audit entry naming
	// the NAS and the account whose password was shown (account labels are not
	// secrets). This supplements the coarse credential.reveal request audit.
	reason := "account=(primary)"
	if account != "" {
		reason = "account=" + account
	}
	_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{
		CorrelationID: correlationID(req), ActorType: "gateway_admin", ActorID: administratorActor(req.Context()),
		Action: "credential.reveal", NAS: name, Reason: reason, Outcome: "success",
	})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"nas": name, "account": account, "password": password})
}

func (h *Handler) passwordEnrollment(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method == http.MethodDelete {
		// An optional ?account= selects one entry from the password book; absent
		// removes the primary login (the historical single-account behavior).
		account := strings.TrimSpace(req.URL.Query().Get("account"))
		var removed bool
		err := h.manager.MutateProfile(req.Context(), name, func() error {
			var err error
			removed, _, err = h.repository.DeletePasswordForAccount(req.Context(), name, account)
			return err
		})
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"removed": removed})
		return
	}
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost, http.MethodDelete)
		return
	}
	if _, ok := h.requireProfileTLS(w, req, name); !ok {
		return
	}
	var input struct {
		Account          string `json:"account"`
		ExpectedRevision uint64 `json:"expected_revision"`
		Password         string `json:"password"`
		OTP              string `json:"otp,omitempty"`
		Store            *bool  `json:"store,omitempty"`
	}
	if err := decodeJSON(req, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer func() { input.Password, input.OTP = "", "" }()
	if strings.TrimSpace(input.Account) == "" || input.ExpectedRevision == 0 || input.Password == "" {
		writeError(w, http.StatusBadRequest, "account, expected_revision, and password are required")
		return
	}
	// store defaults to true when the field is absent, preserving the behavior of
	// callers that always persist. store=false validates the credential against
	// DSM without writing anything to the vault (a "verify, don't save" check).
	store := input.Store == nil || *input.Store
	cfg, err := h.repository.Snapshot(req.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load profile")
		return
	}
	profile, ok := cfg.NAS[name]
	if !ok {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	device := credentials.TrustedDevice{Name: "dsmctl-gateway"}
	client, err := synology.NewClient(synology.Options{
		BaseURL: profile.URL, Username: strings.TrimSpace(input.Account), Password: input.Password,
		DeviceName: device.Name, HTTPClient: runtime.HTTPClient(profile),
		OTPProvider: func(context.Context) (string, error) {
			if input.OTP == "" {
				return "", errors.New("one-time password was not supplied")
			}
			return input.OTP, nil
		},
		SaveDeviceID: func(_ context.Context, id string) error { device.ID = id; return nil },
	})
	if err == nil {
		err = client.Authenticate(req.Context())
	}
	if client != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), networkTimeout)
		_ = client.Close(closeCtx)
		cancel()
	}
	if err != nil {
		// Redacted response; err carries no credential material (the synology
		// client formats redacted URLs and DSM error codes only).
		h.logger.ErrorContext(req.Context(), "DSM rejected password enrollment",
			"request_id", correlationID(req), "nas", name, "error", err)
		writeError(w, http.StatusBadGateway, "DSM rejected password enrollment")
		return
	}
	if !store {
		// Validate-only: the credential authenticated to DSM but the operator did
		// not opt to store it, so nothing is written to the vault.
		writeJSON(w, http.StatusOK, map[string]any{"nas": name, "account": strings.TrimSpace(input.Account), "validated": true, "password_stored": false, "trusted_device_stored": false})
		return
	}
	if device.ID == "" {
		device = credentials.TrustedDevice{}
	}
	err = h.manager.MutateProfile(req.Context(), name, func() error {
		// SavePasswordForAccount routes the profile login to the primary entry and
		// any other account to an additional labeled secret, so one NAS can hold a
		// book of accounts. A primary enrollment matches EnrollPasswordForAccount.
		_, err := h.repository.SavePasswordForAccount(req.Context(), name, input.ExpectedRevision, input.Account, input.Password, device)
		return err
	})
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nas": name, "account": strings.TrimSpace(input.Account), "validated": true, "password_stored": true, "trusted_device_stored": device.ID != ""})
}

// exportCredentials downloads every NAS profile's connection identity and its
// vault-stored DSM password as a single CSV. It reuses the WI-084 reveal
// human-gate at bulk scope: the administrator re-enters their own password,
// the attempt is rate limited, and the action is audited as credential.export
// with counts only — never any revealed value. A profile without a stored
// account or password exports an empty field for it, and the environment
// password fallback is deliberately not exported (StoredPassword only). The
// CSV is never logged and carries Cache-Control: no-store.
func (h *Handler) exportCredentials(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(req, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer func() { input.Password = "" }()
	attemptKey := remoteAttemptKey(req, "export")
	if !h.exportAttempts.Allow(attemptKey) {
		writeError(w, http.StatusTooManyRequests, "too many export attempts")
		return
	}
	actor := administratorActor(req.Context())
	username := strings.TrimPrefix(actor, "local:")
	if _, err := h.repository.VerifyAdministratorCredentials(req.Context(), username, input.Password); err != nil {
		_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", ActorID: actor, Action: "credential.export", Outcome: "denied", Reason: "administrator reauthentication denied"})
		writeError(w, http.StatusUnauthorized, "administrator password verification failed")
		return
	}
	profiles, err := h.repository.Profiles(req.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list profiles")
		return
	}
	h.exportAttempts.Reset(attemptKey)
	rows := make([][]string, 0, len(profiles)+1)
	rows = append(rows, []string{"name", "host", "url", "account", "password"})
	withPassword := 0
	for _, profile := range profiles {
		password := ""
		if profile.PasswordStored {
			stored, err := h.repository.StoredPassword(req.Context(), profile.Name)
			if err != nil && !errors.Is(err, state.ErrNotFound) {
				h.logger.ErrorContext(req.Context(), "vault password export failed",
					"request_id", correlationID(req), "nas", profile.Name, "error", err)
				writeError(w, http.StatusInternalServerError, "read stored password")
				return
			}
			if stored != "" {
				password = stored
				withPassword++
			}
		}
		host := profile.URL
		if parsed, perr := url.Parse(profile.URL); perr == nil && parsed.Host != "" {
			host = parsed.Hostname()
		}
		rows = append(rows, []string{profile.Name, host, profile.URL, profile.Username, password})
	}
	_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", ActorID: actor, Action: "credential.export", Outcome: "success", Reason: fmt.Sprintf("exported %d profiles, %d with stored passwords", len(profiles), withPassword)})
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="dsmctl-nas-credentials.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// UTF-8 BOM so spreadsheet applications detect the encoding of non-ASCII
	// account or password characters.
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(w)
	writer.UseCRLF = true
	_ = writer.WriteAll(rows)
}

func (h *Handler) removeSession(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method != http.MethodDelete {
		methodNotAllowed(w, http.MethodDelete)
		return
	}
	revokeCtx, cancel := context.WithTimeout(req.Context(), networkTimeout)
	revoked, revokeErr := h.manager.RevokeStoredSession(revokeCtx, name)
	cancel()
	var removed bool
	err := h.manager.MutateProfile(req.Context(), name, func() error {
		var err error
		removed, err = h.repository.DeleteSession(req.Context(), name)
		return err
	})
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	response := map[string]any{"removed": removed, "revoked": revoked}
	if revokeErr != nil {
		response["revocation_error"] = revokeErr.Error()
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) applySecrets(w http.ResponseWriter, req *http.Request, name, id string) {
	profile, err := h.repository.Profile(req.Context(), name)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	if id == "" {
		switch req.Method {
		case http.MethodGet:
			metadata, err := h.repository.SecretMetadataForProfile(req.Context(), profile.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "list vault references")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"secrets": metadata})
		case http.MethodPost:
			var input struct {
				Value string `json:"value"`
			}
			if err := decodeJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			metadata, err := h.repository.StoreApplySecret(req.Context(), name, input.Value)
			input.Value = ""
			if err != nil {
				writeRepositoryError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"secret": metadata, "reference": "vault:" + metadata.ID})
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
		return
	}
	if req.Method != http.MethodDelete {
		methodNotAllowed(w, http.MethodDelete)
		return
	}
	removed, err := h.repository.DeleteApplySecret(req.Context(), profile.ID, id)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": removed})
}

func (h *Handler) removeTrustedDevice(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method != http.MethodDelete {
		methodNotAllowed(w, http.MethodDelete)
		return
	}
	var removed bool
	err := h.manager.MutateProfile(req.Context(), name, func() error {
		var err error
		removed, _, err = h.repository.DeleteTrustedDevice(req.Context(), name)
		return err
	})
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": removed})
}

func (h *Handler) startWebLogin(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	profile, ok := h.requireProfileTLS(w, req, name)
	if !ok {
		return
	}
	cfg, _ := h.repository.Snapshot(req.Context())
	opener := h.publicURL
	if opener == "" {
		external, _ := url.Parse(h.externalOrigin(req))
		scheme, host := external.Scheme, external.Host
		prefix := ""
		if forwarded := strings.TrimSpace(req.Header.Get("X-Forwarded-Prefix")); forwarded != "" && strings.HasPrefix(forwarded, "/") && !strings.ContainsAny(forwarded, "\r\n\\?") {
			prefix = strings.TrimRight(forwarded, "/")
		}
		opener = scheme + "://" + host + prefix
	}
	enrollment, start, err := weblogin.BeginEnrollment(profile.URL, opener+"/admin/", weblogin.Options{HTTPClient: runtime.HTTPClient(cfg.NAS[name])})
	if err != nil {
		h.logger.WarnContext(req.Context(), "DSM web-login start failed",
			"request_id", correlationID(req), "nas", name, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := randomID()
	if err != nil {
		h.logger.ErrorContext(req.Context(), "create web-login enrollment failed",
			"request_id", correlationID(req), "nas", name, "error", err)
		writeError(w, http.StatusInternalServerError, "create enrollment")
		return
	}
	expires := h.now().Add(enrollmentTTL)
	h.pendingMu.Lock()
	h.prunePendingLocked(h.now())
	h.pending[id] = pendingEnrollment{ProfileName: name, Enrollment: enrollment, ExpiresAt: expires}
	h.pendingMu.Unlock()
	parsedNAS, _ := url.Parse(profile.URL)
	writeJSON(w, http.StatusCreated, map[string]any{"enrollment_id": id, "login_url": start.LoginURL, "state": start.State, "nas_origin": parsedNAS.Scheme + "://" + parsedNAS.Host, "expires_at": expires})
}

func (h *Handler) completeWebLogin(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input struct {
		EnrollmentID string `json:"enrollment_id"`
		Code         string `json:"code"`
		RS           string `json:"rs"`
		State        string `json:"state"`
	}
	if err := decodeJSON(req, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.pendingMu.Lock()
	pending, ok := h.pending[input.EnrollmentID]
	delete(h.pending, input.EnrollmentID)
	h.pendingMu.Unlock()
	if !ok || pending.ProfileName != name || h.now().After(pending.ExpiresAt) {
		writeError(w, http.StatusGone, "web-login enrollment expired or was already used")
		return
	}
	result, err := pending.Enrollment.Complete(req.Context(), input.Code, input.RS, input.State)
	input.Code, input.RS = "", ""
	if err != nil {
		// The response is redacted; the cause (for example a TLS pinning
		// failure) is only diagnosable from this server-side record.
		h.logger.ErrorContext(req.Context(), "DSM web-login exchange failed",
			"request_id", correlationID(req), "nas", name, "error", err)
		writeError(w, http.StatusBadGateway, "DSM web-login exchange failed")
		return
	}
	now := time.Now().UTC()
	session := credentials.SessionCredential{
		SID: result.SID, SynoToken: result.SynoToken, DeviceID: result.DeviceID,
		ServerPublicKey: result.ServerPublicKey, LocalPublicKey: result.LocalPublicKey, LocalPrivateKey: result.LocalPrivateKey,
		Account: result.Account, IssuedAt: now, LastVerified: now,
	}
	err = h.manager.MutateProfile(req.Context(), name, func() error {
		_, err := h.repository.EnrollSession(req.Context(), name, session)
		return err
	})
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nas": name, "account": result.Account, "session_stored": true, "renewable": session.CanResume()})
}

func (h *Handler) prunePendingLocked(now time.Time) {
	for id, pending := range h.pending {
		if now.After(pending.ExpiresAt) {
			delete(h.pending, id)
		}
	}
}

func (h *Handler) probeProfileTLS(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	profile, ok := h.requireProfileTLS(w, req, name)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nas": name, "profile_revision": profile.Revision, "verified": true, "tls_mode": profile.TLSMode})
}

func (h *Handler) trustProfileCertificate(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method != http.MethodPut {
		methodNotAllowed(w, http.MethodPut)
		return
	}
	var input struct {
		ExpectedRevision uint64 `json:"expected_revision"`
		Fingerprint      string `json:"fingerprint"`
	}
	if err := decodeJSON(req, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	confirmed, err := tlstrust.NormalizeFingerprint(input.Fingerprint)
	if err != nil || confirmed == "" || input.ExpectedRevision == 0 {
		writeError(w, http.StatusBadRequest, "expected_revision and a SHA-256 fingerprint are required")
		return
	}
	profile, err := h.repository.Profile(req.Context(), name)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	if profile.Revision != input.ExpectedRevision {
		writeRepositoryError(w, fmt.Errorf("%w: expected %d, current %d", state.ErrRevisionConflict, input.ExpectedRevision, profile.Revision))
		return
	}
	probeCtx, cancel := context.WithTimeout(req.Context(), networkTimeout)
	defer cancel()
	probeErr := probeTLS(probeCtx, profile)
	var trustErr *tlstrust.TrustError
	if probeErr == nil || !errors.As(probeErr, &trustErr) {
		if probeErr == nil {
			writeError(w, http.StatusConflict, "the current TLS certificate does not require trust confirmation")
		} else {
			writeError(w, http.StatusBadGateway, probeErr.Error())
		}
		return
	}
	if confirmed != trustErr.Certificate.Fingerprint {
		writeError(w, http.StatusConflict, "the observed TLS certificate changed before confirmation")
		return
	}
	var updated state.Profile
	err = h.manager.MutateProfile(req.Context(), name, func() error {
		var updateErr error
		updated, updateErr = h.repository.UpdateProfile(req.Context(), name, input.ExpectedRevision, state.ProfileInput{
			URL:                    profile.URL,
			Username:               profile.Username,
			TLSMode:                state.TLSPinnedFingerprint,
			CertificateFingerprint: confirmed,
			TimeoutSeconds:         profile.TimeoutSeconds,
		})
		return updateErr
	})
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) requireProfileTLS(w http.ResponseWriter, req *http.Request, name string) (state.Profile, bool) {
	profile, err := h.repository.Profile(req.Context(), name)
	if err != nil {
		writeRepositoryError(w, err)
		return state.Profile{}, false
	}
	ctx, cancel := context.WithTimeout(req.Context(), networkTimeout)
	defer cancel()
	if err := probeTLS(ctx, profile); err != nil {
		if !h.writeTLSProbeError(w, profile, err, nil) {
			writeError(w, http.StatusBadGateway, err.Error())
		}
		return state.Profile{}, false
	}
	return profile, true
}

func probeTLS(ctx context.Context, profile state.Profile) error {
	pin := ""
	if profile.TLSMode == state.TLSPinnedFingerprint {
		pin = profile.CertificateFingerprint
	}
	return tlstrust.Probe(ctx, profile.URL, pin)
}

func (h *Handler) writeTLSProbeError(w http.ResponseWriter, profile state.Profile, err error, extra map[string]any) bool {
	var trustErr *tlstrust.TrustError
	if !errors.As(err, &trustErr) {
		return false
	}
	response := map[string]any{
		"error":            trustErr.Error(),
		"code":             trustErr.Code,
		"nas":              profile.Name,
		"profile_revision": profile.Revision,
		"certificate":      trustErr.Certificate,
	}
	if trustErr.ExpectedFingerprint != "" {
		response["expected_fingerprint"] = trustErr.ExpectedFingerprint
	}
	if len(trustErr.ValidationWarnings) != 0 {
		response["validation_warnings"] = trustErr.ValidationWarnings
	}
	for key, value := range extra {
		response[key] = value
	}
	writeJSON(w, http.StatusConflict, response)
	return true
}

func failedStage(name string, err error) testStage {
	message := "no result"
	if err != nil {
		message = err.Error()
	}
	return testStage{Name: name, Passed: false, Error: message}
}

func decodeJSON(req *http.Request, target any) error {
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(req.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		return errors.New("Content-Type must be application/json")
	}
	decoder := json.NewDecoder(io.LimitReader(req.Body, maxAdminBody+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, state.ErrNotFound):
		writeError(w, http.StatusNotFound, "profile not found")
	case errors.Is(err, state.ErrRevisionConflict):
		writeError(w, http.StatusConflict, "profile revision conflict")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func randomID() (string, error) {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

type auditResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *auditResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *auditResponseWriter) Write(value []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(value)
}

func adminAuditAction(req *http.Request) string {
	path := req.URL.Path
	switch {
	case path == "/admin/api/status":
		return "admin.status"
	case path == "/admin/api/session":
		return "admin.session"
	case path == "/admin/api/logout":
		return "admin.logout"
	case path == "/admin/api/password":
		return "admin.password"
	case path == "/admin/api/sessions/revoke-others":
		return "admin.sessions.revoke"
	case strings.HasPrefix(path, "/admin/api/mcp-tokens"):
		return "token.lifecycle"
	case strings.HasPrefix(path, "/admin/api/approval-requests"):
		return "approval.request"
	case path == "/admin/api/approvals":
		return "approval.lifecycle"
	case strings.HasPrefix(path, "/admin/api/audit"):
		return "audit.query"
	case path == "/admin/api/credentials/export":
		return "credential.export"
	case strings.HasSuffix(path, "/credentials/password/reveal"):
		return "credential.reveal"
	case strings.HasSuffix(path, "/provision"):
		return "profile.provision"
	case strings.Contains(path, "/credentials/") || strings.Contains(path, "/weblogin/"):
		return "credential.manage"
	case strings.Contains(path, "/secrets") || strings.HasPrefix(path, "/admin/api/orphan-secrets"):
		return "secret.manage"
	case strings.HasPrefix(path, "/admin/api/profiles"):
		return "profile.manage"
	default:
		return "admin.request"
	}
}

func correlationID(req *http.Request) string {
	return remotepolicy.CorrelationID(req.Context())
}
