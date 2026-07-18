package admin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/gateway/platformauth"
	"github.com/ychiu1211/dsmctl/internal/gateway/state"
	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
	"github.com/ychiu1211/dsmctl/internal/runtime"
	"github.com/ychiu1211/dsmctl/internal/synology"
	"github.com/ychiu1211/dsmctl/internal/weblogin"
)

const (
	maxAdminBody   = int64(1 << 20)
	enrollmentTTL  = 5 * time.Minute
	networkTimeout = 10 * time.Second
)

type Options struct {
	Repository       *state.Repository
	Manager          *runtime.Manager
	PublicURL        string
	PlatformVerifier *platformauth.Verifier
}

type Handler struct {
	repository       *state.Repository
	manager          *runtime.Manager
	publicURL        string
	platformVerifier *platformauth.Verifier

	pendingMu sync.Mutex
	pending   map[string]pendingEnrollment
}

type pendingEnrollment struct {
	ProfileName string
	Enrollment  *weblogin.Enrollment
	ExpiresAt   time.Time
}

type actorContextKey struct{}

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
	return &Handler{repository: options.Repository, manager: options.Manager, publicURL: publicURL, platformVerifier: options.PlatformVerifier, pending: make(map[string]pendingEnrollment)}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasPrefix(req.URL.Path, "/admin/api/") {
		if req.ContentLength > maxAdminBody {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		req.Body = http.MaxBytesReader(w, req.Body, maxAdminBody)
	}
	switch req.URL.Path {
	case "/admin", "/admin/":
		if req.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		_, _ = io.WriteString(w, indexHTML)
		return
	case "/admin/api/bootstrap":
		if h.platformVerifier != nil {
			http.NotFound(w, req)
			return
		}
		h.bootstrap(w, req)
		return
	}
	if !strings.HasPrefix(req.URL.Path, "/admin/api/") {
		http.NotFound(w, req)
		return
	}
	actorID, err := h.authenticate(req)
	if err != nil {
		_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", Action: adminAuditAction(req), Outcome: "denied", Reason: "denied"})
		w.Header().Set("WWW-Authenticate", `Bearer realm="dsmctl-gateway-admin"`)
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
	req = req.WithContext(context.WithValue(req.Context(), actorContextKey{}, actorID))
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
	if req.URL.Path == "/admin/api/admin-token/rotate" {
		if h.platformVerifier != nil {
			http.NotFound(w, req)
			return
		}
		h.rotateAdminToken(w, req)
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
	if req.URL.Path == "/admin/api/audit" || req.URL.Path == "/admin/api/audit/export" {
		h.audit(w, req)
		return
	}
	if req.URL.Path == "/admin/api/orphan-secrets" || strings.HasPrefix(req.URL.Path, "/admin/api/orphan-secrets/") {
		h.orphanSecrets(w, req)
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

func (h *Handler) authenticate(req *http.Request) (string, error) {
	if h.platformVerifier != nil {
		identity, err := h.platformVerifier.Verify(req.Context(), req.Header.Get(platformauth.HeaderName))
		if err != nil {
			return "", err
		}
		return "dsm:" + identity.Subject, nil
	}
	parts := strings.Fields(req.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", state.ErrUnauthorized
	}
	if err := h.repository.AuthenticateAdministrator(req.Context(), parts[1]); err != nil {
		return "", err
	}
	return "local-admin", nil
}

func (h *Handler) bootstrap(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if err := h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "bootstrap", Action: "admin.bootstrap", Outcome: "started"}); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit storage unavailable")
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(req, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	token, err := h.repository.EstablishAdministrator(req.Context(), input.Token)
	input.Token = ""
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, state.ErrBootstrapConsumed) {
			status = http.StatusConflict
		}
		writeError(w, status, "bootstrap failed")
		_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "bootstrap", Action: "admin.bootstrap", Outcome: "failure"})
		return
	}
	_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", ActorID: "local-admin", Action: "admin.bootstrap", Outcome: "success"})
	writeJSON(w, http.StatusCreated, map[string]string{"admin_token": token})
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
	case "default":
		h.setDefault(w, req, name)
	case "test":
		h.testProfile(w, req, name)
	case "credentials/status":
		h.credentialStatus(w, req, name)
	case "credentials/password":
		h.passwordEnrollment(w, req, name)
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

func (h *Handler) rotateAdminToken(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	token, err := h.repository.RotateAdministrator(req.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rotate administrator token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"admin_token": token})
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
		writeJSON(w, http.StatusOK, map[string]any{"approvals": items})
	case http.MethodPost:
		var input struct {
			PlanHash          string `json:"plan_hash"`
			NAS               string `json:"nas"`
			ProfileRevision   uint64 `json:"profile_revision"`
			RequestingTokenID string `json:"requesting_token_id"`
			TTLSeconds        int    `json:"ttl_seconds,omitempty"`
		}
		if err := decodeJSON(req, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		approval, err := h.repository.CreateApproval(req.Context(), state.ApprovalInput{PlanHash: input.PlanHash, NAS: input.NAS, ProfileRevision: input.ProfileRevision, RequestingTokenID: input.RequestingTokenID, TTL: time.Duration(input.TTLSeconds) * time.Second}, administratorActor(req.Context()))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, approval)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
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
	events, err := h.repository.AuditEvents(req.Context(), state.AuditQuery{After: after, Limit: limit, ActorID: req.URL.Query().Get("actor_id"), Action: req.URL.Query().Get("action")})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read audit events")
		return
	}
	if req.URL.Path == "/admin/api/audit/export" {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="dsmctl-audit.jsonl"`)
		w.WriteHeader(http.StatusOK)
		encoder := json.NewEncoder(w)
		for index := len(events) - 1; index >= 0; index-- {
			_ = encoder.Encode(events[index])
		}
		return
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
		var updated state.Profile
		err := h.manager.MutateProfile(req.Context(), name, func() error {
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

func (h *Handler) setDefault(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if err := h.manager.MutateProfile(req.Context(), "", func() error { return h.repository.SetDefault(req.Context(), name) }); err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"default": name})
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
	_, client, err := h.manager.Client(ctx, name)
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

func (h *Handler) passwordEnrollment(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method == http.MethodDelete {
		var removed bool
		err := h.manager.MutateProfile(req.Context(), name, func() error {
			var err error
			removed, _, err = h.repository.DeletePassword(req.Context(), name)
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
	var input struct {
		Password string `json:"password"`
		OTP      string `json:"otp,omitempty"`
	}
	if err := decodeJSON(req, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer func() { input.Password, input.OTP = "", "" }()
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
		BaseURL: profile.URL, Username: profile.Username, Password: input.Password,
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
		writeError(w, http.StatusBadGateway, "DSM rejected password enrollment")
		return
	}
	if device.ID == "" {
		device = credentials.TrustedDevice{}
	}
	err = h.manager.MutateProfile(req.Context(), name, func() error {
		_, err := h.repository.EnrollPassword(req.Context(), name, input.Password, device)
		return err
	})
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nas": name, "password_stored": true, "trusted_device_stored": device.ID != ""})
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
	profile, err := h.repository.Profile(req.Context(), name)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	cfg, _ := h.repository.Snapshot(req.Context())
	opener := h.publicURL
	if opener == "" {
		scheme := "http"
		host := req.Host
		if req.TLS != nil {
			scheme = "https"
		}
		prefix := ""
		if h.platformVerifier != nil {
			if forwarded := strings.TrimSpace(req.Header.Get("X-Forwarded-Proto")); forwarded == "http" || forwarded == "https" {
				scheme = forwarded
			}
			if forwarded := strings.TrimSpace(req.Header.Get("X-Forwarded-Host")); forwarded != "" && !strings.ContainsAny(forwarded, "\r\n/\\") {
				host = forwarded
			}
			if forwarded := strings.TrimSpace(req.Header.Get("X-Forwarded-Prefix")); forwarded != "" && strings.HasPrefix(forwarded, "/") && !strings.ContainsAny(forwarded, "\r\n\\?") {
				prefix = strings.TrimRight(forwarded, "/")
			}
		}
		opener = scheme + "://" + host + prefix
	}
	enrollment, start, err := weblogin.BeginEnrollment(profile.URL, opener+"/admin/", weblogin.Options{HTTPClient: runtime.HTTPClient(cfg.NAS[name])})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := randomID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create enrollment")
		return
	}
	expires := time.Now().Add(enrollmentTTL)
	h.pendingMu.Lock()
	h.prunePendingLocked(time.Now())
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
	if !ok || pending.ProfileName != name || time.Now().After(pending.ExpiresAt) {
		writeError(w, http.StatusGone, "web-login enrollment expired or was already used")
		return
	}
	result, err := pending.Enrollment.Complete(req.Context(), input.Code, input.RS, input.State)
	input.Code, input.RS = "", ""
	if err != nil {
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
	case strings.HasPrefix(path, "/admin/api/mcp-tokens"):
		return "token.lifecycle"
	case path == "/admin/api/approvals":
		return "approval.lifecycle"
	case strings.HasPrefix(path, "/admin/api/audit"):
		return "audit.query"
	case path == "/admin/api/admin-token/rotate":
		return "admin.rotate"
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
