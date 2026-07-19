// Package oauth implements the managed gateway's embedded OAuth authorization
// server for URL-only MCP clients. It deliberately has no DSM dependency.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ychiu1211/dsmctl/internal/gateway/state"
	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
	"github.com/ychiu1211/dsmctl/internal/webassets"
)

const (
	maxBodyBytes       = int64(64 << 10)
	authorizationTTL   = 5 * time.Minute
	maxPendingCodes    = 256
	defaultScopeString = "nas.read nas.plan nas.apply lan.discover"
)

var supportedScopes = map[string]struct{}{
	remotepolicy.ScopeRead: {}, remotepolicy.ScopePlan: {},
	remotepolicy.ScopeApply: {}, remotepolicy.ScopeLANDiscover: {},
}

type Options struct {
	Repository *state.Repository
	PublicURL  string
	Now        func() time.Time
	Logger     *slog.Logger
}

type Handler struct {
	repository *state.Repository
	publicURL  string
	now        func() time.Time
	logger     *slog.Logger

	mu     sync.Mutex
	codes  map[[sha256.Size]byte]authorizationGrant
	limits map[string]attemptWindow
}

type authorizationGrant struct {
	ClientID      string
	ClientName    string
	RedirectURI   string
	Resource      string
	Scopes        []string
	NASAllowlist  []string
	Challenge     string
	Administrator string
	ExpiresAt     time.Time
}

type attemptWindow struct {
	Started time.Time
	Count   int
}

type authorizationRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	State               string
	Scope               string
	Resource            string
	CodeChallenge       string
	CodeChallengeMethod string
	Client              state.OAuthClient
	Scopes              []string
	NASAllowlist        []string
}

func New(options Options) (*Handler, error) {
	if options.Repository == nil {
		return nil, errors.New("OAuth state repository is required")
	}
	publicURL := strings.TrimRight(strings.TrimSpace(options.PublicURL), "/")
	if publicURL != "" {
		parsed, err := url.Parse(publicURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			return nil, errors.New("OAuth public URL must be an absolute http or https origin")
		}
		publicURL = parsed.Scheme + "://" + parsed.Host
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return &Handler{
		repository: options.Repository, publicURL: publicURL, now: now, logger: logger,
		codes: make(map[[sha256.Size]byte]authorizationGrant), limits: make(map[string]attemptWindow),
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch req.URL.Path {
	case "/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp":
		h.protectedResourceMetadata(w, req)
	case "/.well-known/oauth-authorization-server/oauth", "/.well-known/openid-configuration/oauth", "/oauth/.well-known/oauth-authorization-server", "/oauth/.well-known/openid-configuration":
		h.authorizationServerMetadata(w, req)
	case "/oauth/register":
		h.register(w, req)
	case "/oauth/authorize":
		h.authorize(w, req)
	case "/oauth/token":
		h.token(w, req)
	case "/oauth/favicon.svg":
		webassets.ServeFavicon(w, req)
	default:
		http.NotFound(w, req)
	}
}

// ResourceMetadataURL is included in /mcp challenges so prefix deployments do
// not depend on root-level well-known routes owned by another reverse proxy.
func (h *Handler) ResourceMetadataURL(req *http.Request) string {
	return h.externalBase(req) + "/.well-known/oauth-protected-resource"
}

func (h *Handler) ScopeChallenge() string { return defaultScopeString }

func (h *Handler) protectedResourceMetadata(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resource": h.resource(req), "resource_name": "dsmctl MCP Server",
		"authorization_servers":    []string{h.issuer(req)},
		"scopes_supported":         []string{remotepolicy.ScopeRead, remotepolicy.ScopePlan, remotepolicy.ScopeApply, remotepolicy.ScopeLANDiscover},
		"bearer_methods_supported": []string{"header"},
	})
}

func (h *Handler) authorizationServerMetadata(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	issuer := h.issuer(req)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/authorize",
		"token_endpoint":                        issuer + "/token",
		"registration_endpoint":                 issuer + "/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{remotepolicy.ScopeRead, remotepolicy.ScopePlan, remotepolicy.ScopeApply, remotepolicy.ScopeLANDiscover},
	})
}

func (h *Handler) register(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !h.allowAttempt(remoteKey(req, "register"), 30, time.Minute) {
		w.Header().Set("Retry-After", "60")
		writeOAuthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "too many client registrations")
		return
	}
	var input state.OAuthClientInput
	if err := decodeJSON(w, req, &input); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", err.Error())
		return
	}
	if err := h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "oauth_client", Action: "oauth.client.register", Outcome: "started"}); err != nil {
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "audit storage unavailable")
		return
	}
	client, err := h.repository.RegisterOAuthClient(req.Context(), input)
	if err != nil {
		_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "oauth_client", Action: "oauth.client.register", Outcome: "failure"})
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", err.Error())
		return
	}
	_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "oauth_client", ActorID: client.ID, Action: "oauth.client.register", Outcome: "success"})
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id": client.ID, "client_id_issued_at": client.CreatedAt.Unix(),
		"client_name": client.Name, "redirect_uris": client.RedirectURIs,
		"grant_types": client.GrantTypes, "response_types": client.ResponseTypes,
		"token_endpoint_auth_method": client.TokenEndpointAuthMethod,
	})
}

func (h *Handler) authorize(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
		return
	}
	if req.Method == http.MethodPost {
		if !h.sameOriginAuthorization(req) {
			h.renderAuthorizationError(w, req, http.StatusForbidden, "Authorization form origin does not match this Gateway.")
			return
		}
		req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
		if err := req.ParseForm(); err != nil {
			h.renderAuthorizationError(w, req, http.StatusBadRequest, "Invalid authorization request.")
			return
		}
	}
	values := req.URL.Query()
	if req.Method == http.MethodPost {
		values = req.PostForm
	}
	authorization, err := h.validateAuthorizationRequest(req.Context(), req, values)
	if err != nil {
		h.renderAuthorizationError(w, req, http.StatusBadRequest, err.Error())
		return
	}
	if req.Method == http.MethodGet {
		h.renderAuthorization(w, req, authorization, "")
		return
	}
	if req.PostForm.Get("decision") != "allow" {
		h.redirectOAuthError(w, req, authorization, "access_denied", "The administrator denied access.")
		return
	}
	username, password := req.PostForm.Get("username"), req.PostForm.Get("password")
	key := remoteKey(req, "authorize:"+username)
	if !h.allowAttempt(key, 5, time.Minute) {
		password = ""
		h.renderAuthorization(w, req, authorization, "Too many login attempts. Try again later.")
		return
	}
	if err := h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", Action: "oauth.authorize", Outcome: "started"}); err != nil {
		password = ""
		h.renderAuthorizationError(w, req, http.StatusServiceUnavailable, "Audit storage is unavailable.")
		return
	}
	administrator, err := h.repository.VerifyAdministratorCredentials(req.Context(), username, password)
	password = ""
	if err != nil {
		_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", Action: "oauth.authorize", Outcome: "denied", Reason: "denied"})
		h.renderAuthorization(w, req, authorization, "Invalid administrator credentials.")
		return
	}
	h.resetAttempt(key)
	code, err := h.storeAuthorizationCode(authorization, "local:"+administrator)
	if err != nil {
		h.renderAuthorizationError(w, req, http.StatusServiceUnavailable, "Could not create authorization code.")
		return
	}
	_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "gateway_admin", ActorID: "local:" + administrator, Action: "oauth.authorize", Outcome: "success"})
	redirect, _ := url.Parse(authorization.RedirectURI)
	query := redirect.Query()
	query.Set("code", code)
	if authorization.State != "" {
		query.Set("state", authorization.State)
	}
	redirect.RawQuery = query.Encode()
	http.Redirect(w, req, redirect.String(), http.StatusFound)
}

func (h *Handler) token(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	mediaType, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "Content-Type must be application/x-www-form-urlencoded")
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
	if err := req.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
		return
	}
	if req.Header.Get("Authorization") != "" {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "only public clients are supported")
		return
	}
	switch req.PostForm.Get("grant_type") {
	case "authorization_code":
		h.exchangeAuthorizationCode(w, req)
	case "refresh_token":
		h.exchangeRefreshToken(w, req)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "supported grants are authorization_code and refresh_token")
	}
}

func (h *Handler) exchangeAuthorizationCode(w http.ResponseWriter, req *http.Request) {
	code := req.PostForm.Get("code")
	grant, ok := h.consumeAuthorizationCode(code)
	code = ""
	if !ok {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid or expired")
		return
	}
	clientID := strings.TrimSpace(req.PostForm.Get("client_id"))
	redirectURI := strings.TrimSpace(req.PostForm.Get("redirect_uri"))
	resource := strings.TrimSpace(req.PostForm.Get("resource"))
	verifier := strings.TrimSpace(req.PostForm.Get("code_verifier"))
	if clientID != grant.ClientID || redirectURI != grant.RedirectURI || !sameCanonicalResource(resource, grant.Resource) || !verifyPKCE(verifier, grant.Challenge) {
		verifier = ""
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code binding is invalid")
		return
	}
	verifier = ""
	if err := h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "oauth_client", ActorID: clientID, Action: "oauth.token.issue", Outcome: "started"}); err != nil {
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "audit storage unavailable")
		return
	}
	tokenSet, err := h.repository.IssueOAuthTokenSet(req.Context(), state.OAuthTokenInput{
		Name: oauthTokenName(grant.ClientName), Scopes: grant.Scopes, NASAllowlist: grant.NASAllowlist,
		ClientID: clientID, Resource: grant.Resource,
	})
	if err != nil {
		h.logger.Error("OAuth token issue failed", "error", err)
		_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "oauth_client", ActorID: clientID, Action: "oauth.token.issue", Outcome: "failure"})
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "could not issue token")
		return
	}
	_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "oauth_client", ActorID: tokenSet.Token.ID, Action: "oauth.token.issue", Outcome: "success"})
	writeTokenSet(w, tokenSet, grant.Scopes, grant.Resource)
}

func (h *Handler) exchangeRefreshToken(w http.ResponseWriter, req *http.Request) {
	clientID := strings.TrimSpace(req.PostForm.Get("client_id"))
	resource := strings.TrimSpace(req.PostForm.Get("resource"))
	// Some standards-compliant OAuth libraries do not repeat RFC 8707's
	// resource parameter on refresh. The refresh record is already bound to
	// this gateway, so defaulting to the current canonical resource preserves
	// the audience boundary without weakening possession checks.
	if resource == "" {
		resource = h.resource(req)
	}
	refresh := req.PostForm.Get("refresh_token")
	if clientID == "" || refresh == "" {
		refresh = ""
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id, resource, and refresh_token are required")
		return
	}
	if err := h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "oauth_client", ActorID: clientID, Action: "oauth.token.refresh", Outcome: "started"}); err != nil {
		refresh = ""
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "audit storage unavailable")
		return
	}
	tokenSet, err := h.repository.RefreshOAuthTokenSet(req.Context(), refresh, clientID, resource)
	refresh = ""
	if err != nil {
		_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "oauth_client", ActorID: clientID, Action: "oauth.token.refresh", Outcome: "denied", Reason: "invalid_grant"})
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid or expired")
		return
	}
	_ = h.repository.AppendAudit(req.Context(), state.AuditEvent{CorrelationID: correlationID(req), ActorType: "oauth_client", ActorID: tokenSet.Token.ID, Action: "oauth.token.refresh", Outcome: "success"})
	writeTokenSet(w, tokenSet, tokenSet.Token.Scopes, resource)
}

func (h *Handler) validateAuthorizationRequest(ctx context.Context, req *http.Request, values url.Values) (authorizationRequest, error) {
	authorization := authorizationRequest{
		ResponseType: values.Get("response_type"), ClientID: strings.TrimSpace(values.Get("client_id")),
		RedirectURI: strings.TrimSpace(values.Get("redirect_uri")), State: values.Get("state"),
		Scope: strings.TrimSpace(values.Get("scope")), Resource: strings.TrimSpace(values.Get("resource")),
		CodeChallenge: strings.TrimSpace(values.Get("code_challenge")), CodeChallengeMethod: values.Get("code_challenge_method"),
	}
	if authorization.ResponseType != "code" {
		return authorizationRequest{}, errors.New("Only authorization code response_type is supported.")
	}
	client, err := h.repository.OAuthClient(ctx, authorization.ClientID)
	if err != nil {
		return authorizationRequest{}, errors.New("Unknown OAuth client.")
	}
	if !contains(client.RedirectURIs, authorization.RedirectURI) {
		return authorizationRequest{}, errors.New("The redirect URI is not registered for this client.")
	}
	if !sameCanonicalResource(authorization.Resource, h.resource(req)) {
		return authorizationRequest{}, errors.New("The requested MCP resource does not match this server.")
	}
	if authorization.CodeChallengeMethod != "S256" || !validPKCEValue(authorization.CodeChallenge) {
		return authorizationRequest{}, errors.New("S256 PKCE is required.")
	}
	scopes, err := normalizeScopes(authorization.Scope)
	if err != nil {
		return authorizationRequest{}, err
	}
	profiles, err := h.repository.Profiles(ctx)
	if err != nil {
		return authorizationRequest{}, errors.New("Could not load NAS access.")
	}
	nas := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		nas = append(nas, profile.Name)
	}
	if includesNASScope(scopes) && len(nas) == 0 {
		return authorizationRequest{}, errors.New("Add at least one NAS profile before authorizing this client.")
	}
	authorization.Client = client
	authorization.Scopes = scopes
	authorization.NASAllowlist = nas
	authorization.Resource = h.resource(req)
	return authorization, nil
}

func (h *Handler) storeAuthorizationCode(request authorizationRequest, administrator string) (string, error) {
	raw, err := randomSecret(32)
	if err != nil {
		return "", err
	}
	code := "dsmctl_oauth_code_" + raw
	digest := sha256.Sum256([]byte(code))
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now().UTC()
	h.cleanupCodesLocked(now)
	if len(h.codes) >= maxPendingCodes {
		return "", errors.New("too many pending OAuth codes")
	}
	h.codes[digest] = authorizationGrant{
		ClientID: request.ClientID, ClientName: request.Client.Name, RedirectURI: request.RedirectURI,
		Resource: request.Resource, Scopes: append([]string(nil), request.Scopes...),
		NASAllowlist: append([]string(nil), request.NASAllowlist...), Challenge: request.CodeChallenge,
		Administrator: administrator, ExpiresAt: now.Add(authorizationTTL),
	}
	return code, nil
}

func (h *Handler) consumeAuthorizationCode(raw string) (authorizationGrant, bool) {
	if !strings.HasPrefix(raw, "dsmctl_oauth_code_") {
		return authorizationGrant{}, false
	}
	digest := sha256.Sum256([]byte(raw))
	raw = ""
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now().UTC()
	h.cleanupCodesLocked(now)
	grant, ok := h.codes[digest]
	delete(h.codes, digest)
	return grant, ok && now.Before(grant.ExpiresAt)
}

func (h *Handler) cleanupCodesLocked(now time.Time) {
	for digest, grant := range h.codes {
		if !now.Before(grant.ExpiresAt) {
			delete(h.codes, digest)
		}
	}
}

func (h *Handler) renderAuthorization(w http.ResponseWriter, req *http.Request, authorization authorizationRequest, message string) {
	data := authorizationPageData{
		TraditionalChinese: prefersTraditionalChinese(req), Message: message,
		ClientName: authorization.Client.Name, RedirectHost: redirectDisplayHost(authorization.RedirectURI),
		Resource: authorization.Resource, Scopes: authorization.Scopes, NAS: authorization.NASAllowlist,
		Hidden: map[string]string{
			"response_type": authorization.ResponseType, "client_id": authorization.ClientID,
			"redirect_uri": authorization.RedirectURI, "state": authorization.State,
			"scope": strings.Join(authorization.Scopes, " "), "resource": authorization.Resource,
			"code_challenge": authorization.CodeChallenge, "code_challenge_method": authorization.CodeChallengeMethod,
		},
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	if err := authorizationTemplate.Execute(w, data); err != nil {
		h.logger.Error("render OAuth authorization page", "error", err)
	}
}

func (h *Handler) renderAuthorizationError(w http.ResponseWriter, req *http.Request, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.WriteHeader(status)
	_ = errorTemplate.Execute(w, errorPageData{TraditionalChinese: prefersTraditionalChinese(req), Message: message, AdminURL: h.externalBase(req) + "/admin/"})
}

func (h *Handler) redirectOAuthError(w http.ResponseWriter, req *http.Request, authorization authorizationRequest, code, description string) {
	redirect, _ := url.Parse(authorization.RedirectURI)
	query := redirect.Query()
	query.Set("error", code)
	query.Set("error_description", description)
	if authorization.State != "" {
		query.Set("state", authorization.State)
	}
	redirect.RawQuery = query.Encode()
	http.Redirect(w, req, redirect.String(), http.StatusFound)
}

func (h *Handler) externalBase(req *http.Request) string {
	origin := h.publicURL
	if origin == "" {
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
		origin = scheme + "://" + host
	}
	prefix := strings.TrimRight(strings.TrimSpace(req.Header.Get("X-Forwarded-Prefix")), "/")
	if prefix == "" || !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, "\r\n\\?") {
		prefix = ""
	}
	return origin + prefix
}

func (h *Handler) sameOriginAuthorization(req *http.Request) bool {
	origin := strings.TrimRight(strings.TrimSpace(req.Header.Get("Origin")), "/")
	parsed, err := url.Parse(h.externalBase(req))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	return origin == parsed.Scheme+"://"+parsed.Host
}

func (h *Handler) issuer(req *http.Request) string   { return h.externalBase(req) + "/oauth" }
func (h *Handler) resource(req *http.Request) string { return h.externalBase(req) + "/mcp" }

func (h *Handler) allowAttempt(key string, limit int, duration time.Duration) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now().UTC()
	window := h.limits[key]
	if window.Started.IsZero() || !now.Before(window.Started.Add(duration)) {
		window = attemptWindow{Started: now}
	}
	if window.Count >= limit {
		h.limits[key] = window
		return false
	}
	window.Count++
	h.limits[key] = window
	if len(h.limits) > 2048 {
		for candidate, existing := range h.limits {
			if !now.Before(existing.Started.Add(duration)) {
				delete(h.limits, candidate)
			}
		}
	}
	return true
}

func (h *Handler) resetAttempt(key string) {
	h.mu.Lock()
	delete(h.limits, key)
	h.mu.Unlock()
}

func normalizeScopes(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = defaultScopeString
	}
	seen := make(map[string]struct{})
	result := make([]string, 0, 4)
	for _, scope := range strings.Fields(raw) {
		if _, ok := supportedScopes[scope]; !ok {
			return nil, fmt.Errorf("Unsupported scope %q.", scope)
		}
		if _, duplicate := seen[scope]; duplicate {
			continue
		}
		seen[scope] = struct{}{}
		result = append(result, scope)
	}
	if len(result) == 0 {
		return nil, errors.New("At least one scope is required.")
	}
	sort.Strings(result)
	return result, nil
}

func includesNASScope(scopes []string) bool {
	for _, scope := range scopes {
		if strings.HasPrefix(scope, "nas.") {
			return true
		}
	}
	return false
}

func sameCanonicalResource(left, right string) bool {
	a, errA := url.Parse(strings.TrimSpace(left))
	b, errB := url.Parse(strings.TrimSpace(right))
	if errA != nil || errB != nil || a.Host == "" || b.Host == "" {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host) && a.EscapedPath() == b.EscapedPath() && a.RawQuery == "" && b.RawQuery == "" && a.Fragment == "" && b.Fragment == ""
}

func validPKCEValue(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && !strings.ContainsRune("-._~", char) {
			return false
		}
	}
	return true
}

func verifyPKCE(verifier, expectedChallenge string) bool {
	if !validPKCEValue(verifier) {
		return false
	}
	digest := sha256.Sum256([]byte(verifier))
	actual := base64.RawURLEncoding.EncodeToString(digest[:])
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expectedChallenge)) == 1
}

func oauthTokenName(clientName string) string {
	name := "OAuth: " + strings.TrimSpace(clientName)
	for len([]byte(name)) > state.MaxMCPTokenNameBytes {
		runes := []rune(name)
		name = string(runes[:len(runes)-1])
	}
	return name
}

func writeTokenSet(w http.ResponseWriter, tokenSet state.OAuthTokenSet, scopes []string, resource string) {
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": tokenSet.AccessToken, "token_type": "Bearer", "expires_in": tokenSet.ExpiresIn,
		"refresh_token": tokenSet.RefreshToken, "scope": strings.Join(scopes, " "), "resource": resource,
	})
}

func decodeJSON(w http.ResponseWriter, req *http.Request, value any) error {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(req.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "application/json" {
		return errors.New("Content-Type must be application/json")
	}
	req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
	decoder := json.NewDecoder(req.Body)
	// RFC 7591 clients may send optional registration metadata such as
	// application_type, client_uri, or software_version. This private public-
	// client server deliberately ignores fields it does not use instead of
	// rejecting otherwise interoperable MCP clients.
	if err := decoder.Decode(value); err != nil {
		return errors.New("invalid JSON body")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
}

func randomSecret(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func remoteKey(req *http.Request, suffix string) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	return strings.TrimSpace(host) + "\x00" + strings.ToLower(strings.TrimSpace(suffix))
}

func correlationID(req *http.Request) string {
	value := strings.TrimSpace(req.Header.Get("X-Request-ID"))
	if value == "" {
		value = strings.TrimSpace(req.Header.Get("X-DSMCTL-Correlation-ID"))
	}
	if len(value) > 128 {
		value = value[:128]
	}
	return value
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func redirectDisplayHost(raw string) string {
	parsed, _ := url.Parse(raw)
	if parsed == nil {
		return raw
	}
	return parsed.Scheme + "://" + parsed.Host
}

func prefersTraditionalChinese(req *http.Request) bool {
	return strings.Contains(strings.ToLower(req.Header.Get("Accept-Language")), "zh-tw") || strings.Contains(strings.ToLower(req.Header.Get("Accept-Language")), "zh-hant")
}

var _ http.Handler = (*Handler)(nil)
