package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flynn/noise"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
	"github.com/ychiu1211/dsmctl/internal/gateway/state"
	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
	"github.com/ychiu1211/dsmctl/internal/runtime"
	"github.com/ychiu1211/dsmctl/internal/webassets"
)

type discovererFunc func(context.Context, discovery.Query) (application.DiscoverResult, error)

func (fn discovererFunc) DiscoverDevices(ctx context.Context, query discovery.Query) (application.DiscoverResult, error) {
	return fn(ctx, query)
}

func TestAuthenticatedLANDiscovery(t *testing.T) {
	handler, _, manager, token := newTestHandler(t)
	defer manager.Close(context.Background())

	var received discovery.Query
	handler.discoverer = discovererFunc(func(_ context.Context, query discovery.Query) (application.DiscoverResult, error) {
		received = query
		return application.DiscoverResult{Devices: []discovery.Device{{Hostname: "office-nas", Model: "DS923+", IPAddress: "192.0.2.82", IPv4Addresses: []string{"192.0.2.82"}, State: discovery.StateReady}}}, nil
	})

	unauthorized := performJSON(handler, http.MethodPost, "/admin/api/discovery", `{"timeout_seconds":3}`, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized discovery status = %d", unauthorized.Code)
	}
	response := performJSON(handler, http.MethodPost, "/admin/api/discovery", `{"timeout_seconds":3}`, token)
	if response.Code != http.StatusOK {
		t.Fatalf("discovery status = %d body=%s", response.Code, response.Body.String())
	}
	if received.Timeout != 3*time.Second {
		t.Fatalf("discovery timeout = %s", received.Timeout)
	}
	if body := response.Body.String(); !strings.Contains(body, `"hostname":"office-nas"`) || !strings.Contains(body, `"ip_address":"192.0.2.82"`) {
		t.Fatalf("discovery response = %s", body)
	}
	invalid := performJSON(handler, http.MethodPost, "/admin/api/discovery", `{"timeout_seconds":61}`, token)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid discovery timeout status = %d", invalid.Code)
	}
}

func TestFirstRunSetupAndAuthenticatedProfileCRUD(t *testing.T) {
	handler, repository, manager := newUninitializedTestHandler(t, nil)
	defer manager.Close(context.Background())

	unauthorized := performJSON(handler, http.MethodGet, "/admin/api/profiles", "", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	password := "correct horse battery staple"
	setupResponse := performJSON(handler, http.MethodPost, "/admin/api/setup", `{"username":"Owner","password":"`+password+`"}`, "")
	if setupResponse.Code != http.StatusCreated {
		t.Fatalf("setup status = %d, body=%s", setupResponse.Code, setupResponse.Body.String())
	}
	adminSession := responseCookieValue(t, setupResponse, administratorCookie)
	if adminSession == "" || strings.Contains(setupResponse.Body.String(), password) || strings.Contains(setupResponse.Body.String(), adminSession) {
		t.Fatalf("setup response leaked a credential: %s", setupResponse.Body.String())
	}
	for path, field := range map[string]string{
		"/admin/api/profiles":                        "profiles",
		"/admin/api/mcp-tokens":                      "tokens",
		"/admin/api/approvals?include_consumed=true": "approvals",
	} {
		emptyList := performJSON(handler, http.MethodGet, path, "", adminSession)
		if emptyList.Code != http.StatusOK || !strings.Contains(emptyList.Body.String(), `"`+field+`":[]`) {
			t.Fatalf("empty %s must be a JSON array: status=%d body=%s", field, emptyList.Code, emptyList.Body.String())
		}
	}
	replay := performJSON(handler, http.MethodPost, "/admin/api/setup", `{"username":"other","password":"another correct horse password"}`, "")
	if replay.Code != http.StatusConflict {
		t.Fatalf("setup replay status = %d", replay.Code)
	}

	createBody := `{"name":"office","url":"https://office.example:5001","username":"operator","tls_mode":"system_ca"}`
	created := performJSON(handler, http.MethodPost, "/admin/api/profiles", createBody, adminSession)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", created.Code, created.Body.String())
	}
	profile, err := repository.Profile(context.Background(), "office")
	if err != nil || profile.Revision != 1 || !profile.Default {
		t.Fatalf("created profile=%#v err=%v", profile, err)
	}

	updateBody := `{"expected_revision":1,"url":"https://office-new.example:5001","username":"operator","tls_mode":"system_ca"}`
	updated := performJSON(handler, http.MethodPut, "/admin/api/profiles/office", updateBody, adminSession)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", updated.Code, updated.Body.String())
	}
	conflict := performJSON(handler, http.MethodPut, "/admin/api/profiles/office", updateBody, adminSession)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("stale update status = %d", conflict.Code)
	}

	secret := "plaintext-must-never-enter-admin-output"
	if _, err := repository.SavePassword(context.Background(), "office", secret); err != nil {
		t.Fatal(err)
	}
	listed := performJSON(handler, http.MethodGet, "/admin/api/profiles", "", adminSession)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), secret) {
		t.Fatalf("list status/body = %d %s", listed.Code, listed.Body.String())
	}
	if !strings.Contains(listed.Body.String(), `"password_stored":true`) {
		t.Fatalf("credential presence missing from list: %s", listed.Body.String())
	}
}

func TestSetupWindowExpiresAndRestartReopensOnlyUninitializedState(t *testing.T) {
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	handler, repository, manager := newUninitializedTestHandler(t, func() time.Time { return now })
	defer manager.Close(context.Background())
	now = now.Add(time.Hour)
	if response := performJSON(handler, http.MethodPost, "/admin/api/setup", `{"username":"owner","password":"correct horse battery staple"}`, ""); response.Code != http.StatusGone {
		t.Fatalf("expired setup status = %d body=%s", response.Code, response.Body.String())
	}
	restarted, err := New(Options{Repository: repository, Manager: manager, PublicURL: "https://gateway.example", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	setup := performJSON(restarted, http.MethodPost, "/admin/api/setup", `{"username":"owner","password":"correct horse battery staple"}`, "")
	if setup.Code != http.StatusCreated {
		t.Fatalf("restart setup status = %d body=%s", setup.Code, setup.Body.String())
	}
	newProcess, err := New(Options{Repository: repository, Manager: manager, PublicURL: "https://gateway.example", Now: func() time.Time { return now.Add(2 * time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	if response := performJSON(newProcess, http.MethodPost, "/admin/api/setup", `{"username":"other","password":"another correct horse password"}`, ""); response.Code != http.StatusConflict {
		t.Fatalf("initialized restart setup status = %d", response.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/api/status", nil)
	request.Header.Set("Authorization", "Bearer legacy-token")
	request.Header.Set("X-DSMCTL-Platform-Assertion", "legacy-assertion")
	response := httptest.NewRecorder()
	newProcess.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("legacy credential status = %d", response.Code)
	}
}

func TestAdministratorLoginCookieLogoutAndBrowserRequestBoundary(t *testing.T) {
	handler, repository, manager, firstSession := newTestHandler(t)
	defer manager.Close(context.Background())

	wrongOrigin := httptest.NewRequest(http.MethodPost, "/admin/api/login", strings.NewReader(`{"username":"owner","password":"correct horse battery staple"}`))
	wrongOrigin.Header.Set("Content-Type", "application/json")
	wrongOrigin.Header.Set(requestHeader, "1")
	wrongOrigin.Header.Set("Origin", "https://attacker.example")
	wrongOriginResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongOriginResponse, wrongOrigin)
	if wrongOriginResponse.Code != http.StatusForbidden {
		t.Fatalf("wrong-origin login status = %d", wrongOriginResponse.Code)
	}

	for attempt := 1; attempt <= 6; attempt++ {
		response := performJSON(handler, http.MethodPost, "/admin/api/login", `{"username":"owner","password":"wrong password"}`, "")
		want := http.StatusUnauthorized
		if attempt == 6 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("login attempt %d status = %d body=%s", attempt, response.Code, response.Body.String())
		}
	}

	// A process restart clears the in-memory limiter while preserving the
	// initialized administrator and its persistent sessions.
	handler, err := New(Options{Repository: repository, Manager: manager, PublicURL: "https://gateway.example"})
	if err != nil {
		t.Fatal(err)
	}
	login := performJSON(handler, http.MethodPost, "/admin/api/login", `{"username":"OWNER","password":"correct horse battery staple"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", login.Code, login.Body.String())
	}
	secondSession := responseCookieValue(t, login, administratorCookie)
	if strings.Contains(login.Body.String(), secondSession) {
		t.Fatal("login response leaked session token")
	}

	missingBoundary := httptest.NewRequest(http.MethodPost, "/admin/api/sessions/revoke-others", strings.NewReader(`{}`))
	missingBoundary.AddCookie(&http.Cookie{Name: administratorCookie, Value: secondSession})
	missingBoundary.Header.Set("Content-Type", "application/json")
	missingBoundary.Header.Set("Origin", "https://gateway.example")
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingBoundary)
	if missingResponse.Code != http.StatusForbidden {
		t.Fatalf("simple browser mutation status = %d", missingResponse.Code)
	}

	if response := performJSON(handler, http.MethodGet, "/admin/api/session", "", secondSession); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"username":"owner"`) {
		t.Fatalf("session status = %d body=%s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodGet, "/admin/api/session", "", firstSession); response.Code != http.StatusOK {
		t.Fatalf("first session unexpectedly invalid = %d", response.Code)
	}
	if response := performJSON(handler, http.MethodPost, "/admin/api/sessions/revoke-others", `{}`, secondSession); response.Code != http.StatusOK {
		t.Fatalf("revoke others status = %d body=%s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodGet, "/admin/api/session", "", firstSession); response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status = %d", response.Code)
	}
	logout := performJSON(handler, http.MethodPost, "/admin/api/logout", `{}`, secondSession)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status = %d body=%s", logout.Code, logout.Body.String())
	}
	if response := performJSON(handler, http.MethodGet, "/admin/api/session", "", secondSession); response.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out session status = %d", response.Code)
	}
}

func TestProfileMutationEvictsOnlyChangedNAS(t *testing.T) {
	_, repository, manager, _ := newTestHandler(t)
	ctx := context.Background()
	for _, name := range []string{"office", "lab"} {
		if _, err := repository.CreateProfile(ctx, state.ProfileInput{Name: name, URL: "https://" + name + ".example:5001", Username: "operator"}); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.SavePassword(ctx, name, "password-"+name); err != nil {
			t.Fatal(err)
		}
		if _, _, err := manager.Client(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	if !manager.SessionInfo("office").ClientCached || !manager.SessionInfo("lab").ClientCached {
		t.Fatal("clients were not cached")
	}
	if err := manager.MutateProfile(ctx, "office", func() error {
		_, err := repository.SavePassword(ctx, "office", "rotated-office-password")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if manager.SessionInfo("office").ClientCached {
		t.Fatal("changed NAS client was not evicted")
	}
	if !manager.SessionInfo("lab").ClientCached {
		t.Fatal("unrelated NAS client was evicted")
	}
}

func TestMCPTokenApprovalAndAuditAdministration(t *testing.T) {
	handler, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	_, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "office", URL: "https://office.example"})
	if err != nil {
		t.Fatal(err)
	}
	created := performJSON(handler, http.MethodPost, "/admin/api/mcp-tokens", `{"name":"automation","scopes":["nas.apply"],"nas_allowlist":["office"]}`, adminSession)
	if created.Code != http.StatusCreated {
		t.Fatalf("create token status=%d body=%s", created.Code, created.Body.String())
	}
	var issued state.IssuedMCPToken
	if err := json.Unmarshal(created.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if issued.BearerToken == "" || issued.Token.ID == "" {
		t.Fatalf("issued = %#v", issued)
	}
	listed := performJSON(handler, http.MethodGet, "/admin/api/mcp-tokens", "", adminSession)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), issued.BearerToken) {
		t.Fatalf("list tokens=%d %s", listed.Code, listed.Body.String())
	}
	hash := strings.Repeat("a", 64)
	approvalBody := fmt.Sprintf(`{"plan_hash":%q,"nas":"office","requesting_token_id":%q}`, hash, issued.Token.ID)
	approved := performJSON(handler, http.MethodPost, "/admin/api/approvals", approvalBody, adminSession)
	if approved.Code != http.StatusCreated {
		t.Fatalf("create approval=%d %s", approved.Code, approved.Body.String())
	}
	audit := performJSON(handler, http.MethodGet, "/admin/api/audit?limit=50", "", adminSession)
	if audit.Code != http.StatusOK || !strings.Contains(audit.Body.String(), "token.lifecycle") || !strings.Contains(audit.Body.String(), "approval.lifecycle") || strings.Contains(audit.Body.String(), issued.BearerToken) {
		t.Fatalf("audit=%d %s", audit.Code, audit.Body.String())
	}
	export := performJSON(handler, http.MethodGet, "/admin/api/audit/export?limit=50", "", adminSession)
	if export.Code != http.StatusOK || export.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("audit export=%d headers=%v", export.Code, export.Header())
	}
	revoked := performJSON(handler, http.MethodDelete, "/admin/api/mcp-tokens/"+issued.Token.ID, "", adminSession)
	if revoked.Code != http.StatusOK || !strings.Contains(revoked.Body.String(), "revoked_at") {
		t.Fatalf("revoke=%d %s", revoked.Code, revoked.Body.String())
	}
}

func TestPendingApprovalAdministrationSupportsOneClickAndDismiss(t *testing.T) {
	handler, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	profile, _ := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "office", URL: "https://office.example"})
	issued, _ := repository.CreateMCPToken(context.Background(), state.MCPTokenInput{Name: "operator", Scopes: []string{remotepolicy.ScopeApply}, NASAllowlist: []string{"office"}})
	request := remotepolicy.PendingApprovalRequest{PlanHash: strings.Repeat("d", 64), NAS: "office", ProfileRevision: profile.Revision, RequestingTokenID: issued.Token.ID, Tool: "plan_storage_change", Risk: "high", Summary: "delete storage pool"}
	if err := repository.RecordPendingApproval(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	listed := performJSON(handler, http.MethodGet, "/admin/api/approval-requests", "", adminSession)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "delete storage pool") || !strings.Contains(listed.Body.String(), "operator") {
		t.Fatalf("pending list = %d %s", listed.Code, listed.Body.String())
	}
	var payload struct {
		Requests []state.PendingApproval `json:"requests"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &payload); err != nil || len(payload.Requests) != 1 {
		t.Fatalf("decode pending list = %#v, %v", payload, err)
	}
	approved := performJSON(handler, http.MethodPost, "/admin/api/approval-requests/"+payload.Requests[0].ID+"/approve", `{}`, adminSession)
	if approved.Code != http.StatusCreated || !strings.Contains(approved.Body.String(), request.PlanHash) {
		t.Fatalf("one-click approval = %d %s", approved.Code, approved.Body.String())
	}
	listed = performJSON(handler, http.MethodGet, "/admin/api/approval-requests", "", adminSession)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"requests":[]`) {
		t.Fatalf("pending list after approval = %d %s", listed.Code, listed.Body.String())
	}

	request.PlanHash = strings.Repeat("e", 64)
	if err := repository.RecordPendingApproval(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	requests, _ := repository.PendingApprovals(context.Background())
	dismissed := performJSON(handler, http.MethodDelete, "/admin/api/approval-requests/"+requests[0].ID, "", adminSession)
	if dismissed.Code != http.StatusOK {
		t.Fatalf("dismiss = %d %s", dismissed.Code, dismissed.Body.String())
	}
	approvals, _ := repository.Approvals(context.Background(), false)
	if len(approvals) != 1 {
		t.Fatalf("dismiss created an approval: %#v", approvals)
	}
}

func TestManagedProfileCreationAndEditDoNotAcceptDSMIdentity(t *testing.T) {
	handler, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	created := performJSON(handler, http.MethodPost, "/admin/api/profiles", `{"name":"office","url":"https://office.example","username":"unverified","tls_mode":"system_ca"}`, adminSession)
	if created.Code != http.StatusCreated {
		t.Fatalf("create profile = %d %s", created.Code, created.Body.String())
	}
	profile, _ := repository.Profile(context.Background(), "office")
	if profile.Username != "" {
		t.Fatalf("profile creation stored unverified account %q", profile.Username)
	}
	updated := performJSON(handler, http.MethodPut, "/admin/api/profiles/office", fmt.Sprintf(`{"expected_revision":%d,"url":"https://office-new.example","username":"attacker","tls_mode":"system_ca","timeout_seconds":25}`, profile.Revision), adminSession)
	if updated.Code != http.StatusOK {
		t.Fatalf("update profile = %d %s", updated.Code, updated.Body.String())
	}
	profile, _ = repository.Profile(context.Background(), "office")
	if profile.Username != "" || profile.URL != "https://office-new.example" || profile.TimeoutSeconds != 25 {
		t.Fatalf("updated profile = %#v", profile)
	}
}

func TestAdministratorPasswordChangeRequiresConfirmation(t *testing.T) {
	handler, _, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	response := performJSON(handler, http.MethodPut, "/admin/api/password", `{"current_password":"correct horse battery staple","new_password":"another correct horse","confirm_new_password":"different password"}`, adminSession)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "do not match") {
		t.Fatalf("password mismatch = %d %s", response.Code, response.Body.String())
	}
}

func TestAuditExportEndpointReturnsEveryRetainedEvent(t *testing.T) {
	handler, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	base := time.Now().UTC().Add(-time.Hour)
	for index := 0; index < 1005; index++ {
		if err := repository.AppendAudit(context.Background(), state.AuditEvent{Time: base.Add(time.Duration(index) * time.Millisecond), ActorType: "test", ActorID: "export", Action: "export.seed", Outcome: "success"}); err != nil {
			t.Fatal(err)
		}
	}
	response := performJSON(handler, http.MethodGet, "/admin/api/audit/export?limit=1", "", adminSession)
	if response.Code != http.StatusOK {
		t.Fatalf("audit export = %d %s", response.Code, response.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
	if len(lines) < 1005 {
		t.Fatalf("audit export returned %d lines", len(lines))
	}
	var first, last state.AuditEvent
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatal(err)
	}
	if last.Time.Before(first.Time) {
		t.Fatalf("audit export order first=%s last=%s", first.Time, last.Time)
	}
}

func TestMutatingAdminRequestFailsBeforeMutationWhenAuditUnavailable(t *testing.T) {
	fail := atomic.Bool{}
	passwordParameters := state.PasswordHashParameters{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	repository, err := state.OpenWithOptions(filepath.Join(t.TempDir(), "gateway.db"), bytes.Repeat([]byte{4}, 32), state.OpenOptions{PasswordHashParameters: &passwordParameters, AuditFailure: func() error {
		if fail.Load() {
			return errors.New("offline")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	adminSession, _, err := repository.CreateAdministrator(context.Background(), "owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "office", URL: "https://office.example"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := repository.Snapshot(context.Background())
	manager := runtime.NewManager(cfg, repository, runtime.WithConfigSource(repository))
	defer manager.Close(context.Background())
	handler, err := New(Options{Repository: repository, Manager: manager, PublicURL: "https://gateway.example"})
	if err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	response := performJSON(handler, http.MethodPost, "/admin/api/mcp-tokens", `{"name":"must-not-exist","nas_allowlist":["office"]}`, adminSession)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	fail.Store(false)
	tokens, err := repository.MCPTokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Fatalf("mutation ran despite audit failure: %#v", tokens)
	}
}

func TestPinnedFingerprintRequiresExplicitConfirmation(t *testing.T) {
	handler, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	pin := strings.Repeat("a", 64)
	unconfirmed := performJSON(handler, http.MethodPost, "/admin/api/profiles", `{"name":"pinned","url":"https://pinned.example:5001","tls_mode":"pinned_fingerprint","certificate_fingerprint":"`+pin+`"}`, adminSession)
	if unconfirmed.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed pin status = %d, body=%s", unconfirmed.Code, unconfirmed.Body.String())
	}
	confirmed := performJSON(handler, http.MethodPost, "/admin/api/profiles", `{"name":"pinned","url":"https://pinned.example:5001","tls_mode":"pinned_fingerprint","certificate_fingerprint":"`+pin+`","confirm_certificate_fingerprint":true}`, adminSession)
	if confirmed.Code != http.StatusCreated {
		t.Fatalf("confirmed pin status = %d, body=%s", confirmed.Code, confirmed.Body.String())
	}
	profile, err := repository.Profile(context.Background(), "pinned")
	if err != nil {
		t.Fatal(err)
	}
	changedURL := performJSON(handler, http.MethodPut, "/admin/api/profiles/pinned", fmt.Sprintf(`{"expected_revision":%d,"url":"https://replacement.example:5001","tls_mode":"pinned_fingerprint","certificate_fingerprint":%q,"confirm_certificate_fingerprint":true}`, profile.Revision, pin), adminSession)
	if changedURL.Code != http.StatusOK {
		t.Fatalf("changed URL status = %d, body=%s", changedURL.Code, changedURL.Body.String())
	}
	if body := changedURL.Body.String(); !strings.Contains(body, `"tls_mode":"system_ca"`) || strings.Contains(body, `"certificate_fingerprint"`) {
		t.Fatalf("URL change carried the old pin: %s", body)
	}
}

func TestObservedCertificateTrustPrecedesGatewayEnrollment(t *testing.T) {
	var httpRequests atomic.Int64
	dsm := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		httpRequests.Add(1)
	}))
	defer dsm.Close()
	handler, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	profile, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "tofu", URL: dsm.URL})
	if err != nil {
		t.Fatal(err)
	}

	started := performJSON(handler, http.MethodPost, "/admin/api/profiles/tofu/weblogin/start", `{}`, adminSession)
	if started.Code != http.StatusConflict {
		t.Fatalf("untrusted start status = %d, body=%s", started.Code, started.Body.String())
	}
	var challenge struct {
		Code               string   `json:"code"`
		ProfileRevision    uint64   `json:"profile_revision"`
		ValidationWarnings []string `json:"validation_warnings"`
		Certificate        struct {
			Fingerprint string `json:"fingerprint"`
			Subject     string `json:"subject"`
		} `json:"certificate"`
	}
	if err := json.Unmarshal(started.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	if challenge.Code != "certificate_trust_required" || challenge.ProfileRevision != profile.Revision || len(challenge.Certificate.Fingerprint) != 64 || challenge.Certificate.Subject == "" || len(challenge.ValidationWarnings) == 0 {
		t.Fatalf("challenge = %#v", challenge)
	}
	if httpRequests.Load() != 0 {
		t.Fatalf("HTTP request reached DSM before certificate trust: %d", httpRequests.Load())
	}
	passwordBody := fmt.Sprintf(`{"account":"operator","expected_revision":%d,"password":"must-not-leave-gateway"}`, profile.Revision)
	passwordAttempt := performJSON(handler, http.MethodPost, "/admin/api/profiles/tofu/credentials/password", passwordBody, adminSession)
	if passwordAttempt.Code != http.StatusConflict || strings.Contains(passwordAttempt.Body.String(), "must-not-leave-gateway") || httpRequests.Load() != 0 {
		t.Fatalf("password preflight = %d %s, requests=%d", passwordAttempt.Code, passwordAttempt.Body.String(), httpRequests.Load())
	}

	trustBody := fmt.Sprintf(`{"expected_revision":%d,"fingerprint":%q}`, challenge.ProfileRevision, challenge.Certificate.Fingerprint)
	trusted := performJSON(handler, http.MethodPut, "/admin/api/profiles/tofu/tls/trust", trustBody, adminSession)
	if trusted.Code != http.StatusOK {
		t.Fatalf("trust status = %d, body=%s", trusted.Code, trusted.Body.String())
	}
	profile, err = repository.Profile(context.Background(), "tofu")
	if err != nil || profile.TLSMode != state.TLSPinnedFingerprint || profile.CertificateFingerprint != challenge.Certificate.Fingerprint {
		t.Fatalf("trusted profile = %#v, %v", profile, err)
	}
	started = performJSON(handler, http.MethodPost, "/admin/api/profiles/tofu/weblogin/start", `{}`, adminSession)
	if started.Code != http.StatusCreated || httpRequests.Load() != 0 {
		t.Fatalf("trusted start = %d %s, requests=%d", started.Code, started.Body.String(), httpRequests.Load())
	}

	wrongPin := strings.Repeat("ab", 32)
	profile, err = repository.UpdateProfile(context.Background(), "tofu", profile.Revision, state.ProfileInput{
		URL: dsm.URL, TLSMode: state.TLSPinnedFingerprint, CertificateFingerprint: wrongPin,
	})
	if err != nil {
		t.Fatal(err)
	}
	mismatch := performJSON(handler, http.MethodPost, "/admin/api/profiles/tofu/tls", `{}`, adminSession)
	if mismatch.Code != http.StatusConflict || !strings.Contains(mismatch.Body.String(), `"code":"certificate_pin_mismatch"`) || !strings.Contains(mismatch.Body.String(), `"expected_fingerprint":"`+wrongPin+`"`) {
		t.Fatalf("pin mismatch = %d %s", mismatch.Code, mismatch.Body.String())
	}
}

func TestAdminUIHasNoEmbeddedCredential(t *testing.T) {
	handler, _, manager, _ := newTestHandler(t)
	defer manager.Close(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "dsmctl MCP Server") {
		t.Fatalf("UI response = %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("UI response has no content security policy")
	}
	for _, forbidden := range []string{"sessionStorage", "admin_token", "platform assertion", "bootstrap token", "--blue:", "--navy:", "window.prompt", "prompt(", `value="nas.admin"`, `onclick="setDefault`, `id="profileNextStep"`, `id="tokenNAS"`, `show(JSON.stringify(await api`, `async function copyText(value){`, `<div class="table-wrap"><table><thead><tr><th data-i18n="name">Name</th><th data-i18n="address"`, `id="profileDialog"`, `function openProfileEdit(`, `data-i18n="connectClient"`, `data-i18n="createConnection"`, `data-i18n="connectionCreated"`, `id="tls"`, `id="fingerprint"`, `function toggleFingerprint`, `confirmWizardTrust`} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("UI contains superseded administrator mechanism %q", forbidden)
		}
	}
	if !strings.Contains(recorder.Body.String(), "/setup/status") || !strings.Contains(recorder.Body.String(), "HttpOnly/SameSite") {
		t.Fatal("UI does not expose the local administrator setup/login flow")
	}
	for _, required := range []string{
		`id="view-overview"`, `data-nav="nas"`, `aria-live="polite"`, `@media(max-width:760px)`,
		`--brand-500:#2588df`, `--brand-950:#0d263f`, `--slate-900:#162334`,
		`--color-action:var(--brand-500)`, `--color-nav:var(--brand-950)`,
		`<meta name="theme-color" content="#0d263f">`, `<link rel="icon" href="/admin/favicon.svg" type="image/svg+xml" sizes="any">`,
		`data-locale-select`, `localStorage.getItem('dsmctl.locale')`, `dataset.i18nDiagnostics`,
		`.view>.panel+.panel{margin-top:18px}`, `.button-row+.notice{margin-top:16px}`,
		`.row-menu-body`, `.wizard-steps`, `.choice-grid`, `.diagnostic-list`, `.panel-stack{display:grid;gap:18px}`,
		`.profile-list-head,.profile-list-row`, `.profile-list-row{grid-template-columns:1fr`, `<div id="profiles" role="list">`,
		`.profile-subline`, `id="nasSourceStep"`, `id="nasConnectionStep" hidden`, `id="nasSignInStep" hidden`, `id="nasStepThree"`,
		`id="discoverLANButton"`, `id="discoveredNAS"`, `id="nasSourceAddress"`, `onclick="discoverLAN()"`, `onclick="useManualNAS()"`, `api('/discovery'`,
		`id="nasConnectionSubmit"`, `id="wizardConnectionURL"`, `id="wizardConnectionTLS"`, `function tlsModeLabel(mode)`, `function persistWizardConnection(input)`,
		`data-i18n="automaticTLS"`, `function isTLSChallenge(error)`, `function resolveTLSChallenge(profile,error)`, `certificate_trust_required`, `certificate_pin_mismatch`, `api('/profiles/'+encodeURIComponent(profile.name)+'/tls'`,
		`summary.setAttribute('aria-haspopup','menu')`, `menu.setAttribute('role','menu')`, `document.querySelectorAll('.row-menu[open]')`,
		`id="messageText"`, `id="messageClose"`, `data-i18n-aria="dismissMessage"`, `setTimeout(hideMessage,4000)`,
		`class="field field-span-2"><label class="required" for="currentPassword"`, `Mindestens 8 Zeichen`,
		`English`, `繁體中文`, `简体中文`, `日本語`, `Deutsch`, `MCP endpoint`, `/mcp`,
		`data-i18n="configureMCP">Configure MCP access`, `configureMCP:'設定 MCP 存取'`,
		`data-i18n="createManualToken">Create manual token`, `data-i18n="manualTokenWizardDetail"`,
		`data-i18n="credentialName">Credential name`, `credentialName:'憑證名稱'`,
		`data-i18n="generateTokenConfiguration">Generate token and configuration`, `generateTokenConfiguration:'產生 Token 與設定'`,
		`data-i18n="manualTokenCreated">Manual token created`, `manualTokenCreated:'手動 Token 已建立'`,
		`id="nasWizard"`, `id="accessWizard"`, `id="diagnosticDialog"`, `id="manualApprovalDialog"`, `id="auditFilterDialog"`, `id="passwordDialog"`,
		`id="view-admin"`, `<div class="panel-stack"><div class="panel">`,
		`value="365" selected`, `value="nas.read" checked`, `value="nas.plan" checked`, `value="nas.apply" checked`, `value="lan.discover" checked`,
		`error.payload=value`, `location.pathname.replace(/\/admin\/?$/,'/mcp')`, `transport:'streamable-http'`, `show(message)`,
		`id="credentialDialog"`, `id="approvalRequests"`, `id="auditRows"`, `confirm_new_password`,
		`id="revealDialog"`, `/credentials/password/reveal`, `data-i18n="revealPassword"`,
		`onclick="copyText(revealedAccount,t('accountCopied'))"`, `data-i18n="copyAccount"`, `{label:t('openNAS'),action:()=>window.open(profile.url,'_blank','noopener')}`,
		`openNAS:'Open NAS'`, `openNAS:'開啟 NAS'`, `copyAccount:'複製帳號'`,
		`id="provisionDialog"`, `async function submitProvision(event)`, `/profiles/'+encodeURIComponent(name)+'/provision'`,
		`if(profile.role!=='target'&&!hasAuthentication)items.push({label:t('provision')`, `provision:'Provision fresh NAS'`, `provision:'安裝全新 NAS'`,
		`id="exportDialog"`, `onclick="openExportDialog()"`, `data-i18n="exportCredentials"`, `async function submitExport(event)`, `apiBase+'/credentials/export'`, `link.download='dsmctl-nas-credentials.csv'`,
		`exportCredentials:'Export credentials'`, `exportCredentials:'匯出憑證'`, `downloadCSV:'CSV herunterladen'`,
	} {
		if !strings.Contains(recorder.Body.String(), required) {
			t.Fatalf("UI is missing redesigned application shell marker %q", required)
		}
	}
	for _, externalAsset := range []string{"<script src=", `<img src="http`, `href="http`, `href="//`, "@import"} {
		if strings.Contains(recorder.Body.String(), externalAsset) {
			t.Fatalf("UI loads an external asset matching %q", externalAsset)
		}
	}
}

func TestAdminServesSharedFavicon(t *testing.T) {
	handler, _, manager, _ := newTestHandler(t)
	defer manager.Close(context.Background())

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/favicon.svg", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("favicon status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != webassets.FaviconContentType {
		t.Errorf("favicon Content-Type = %q", got)
	}
	if got := recorder.Body.String(); got != webassets.FaviconSVG() {
		t.Fatal("admin favicon differs from the shared source")
	}
}

func TestPasswordOTPEnrollmentStoresTrustedDeviceWithoutReturningSecrets(t *testing.T) {
	var loginCount int
	dsm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		switch req.Form.Get("api") + "." + req.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7}}}`)
		case "SYNO.API.Auth.login":
			loginCount++
			if req.Form.Get("passwd") != "enrollment-password" {
				t.Errorf("password form value = %q", req.Form.Get("passwd"))
			}
			if loginCount == 1 {
				fmt.Fprint(w, `{"success":false,"error":{"code":403}}`)
				return
			}
			if req.Form.Get("otp_code") != "654321" || req.Form.Get("enable_device_token") != "yes" {
				t.Errorf("OTP login form = %#v", req.Form)
			}
			fmt.Fprint(w, `{"success":true,"data":{"sid":"temporary-sid","did":"trusted-device-id"}}`)
		case "SYNO.API.Auth.logout":
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		default:
			t.Errorf("unexpected DSM call %s.%s", req.Form.Get("api"), req.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer dsm.Close()

	handler, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	profile, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "mfa", URL: dsm.URL})
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"account":"operator","expected_revision":%d,"password":"enrollment-password","otp":"654321"}`, profile.Revision)
	response := performJSON(handler, http.MethodPost, "/admin/api/profiles/mfa/credentials/password", body, adminSession)
	if response.Code != http.StatusOK {
		t.Fatalf("enrollment status = %d, body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "enrollment-password") || strings.Contains(response.Body.String(), "654321") || strings.Contains(response.Body.String(), "trusted-device-id") {
		t.Fatalf("enrollment response leaked a secret: %s", response.Body.String())
	}
	password, err := repository.Password(context.Background(), "mfa", mustRuntimeProfile(t, repository, "mfa"))
	if err != nil || password != "enrollment-password" {
		t.Fatalf("stored password = %q, %v", password, err)
	}
	device, err := repository.TrustedDevice(context.Background(), "mfa")
	if err != nil || device.ID != "trusted-device-id" {
		t.Fatalf("stored device = %#v, %v", device, err)
	}
	updated, err := repository.Profile(context.Background(), "mfa")
	if err != nil || updated.Username != "operator" || updated.Revision <= profile.Revision {
		t.Fatalf("enrolled profile = %#v, %v", updated, err)
	}
}

func TestPasswordEnrollmentValidateOnlyDoesNotStore(t *testing.T) {
	var loginCount int
	dsm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		switch req.Form.Get("api") + "." + req.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7}}}`)
		case "SYNO.API.Auth.login":
			loginCount++
			fmt.Fprint(w, `{"success":true,"data":{"sid":"temporary-sid"}}`)
		case "SYNO.API.Auth.logout":
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		default:
			t.Errorf("unexpected DSM call %s.%s", req.Form.Get("api"), req.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer dsm.Close()

	handler, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	profile, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "verify", URL: dsm.URL})
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"account":"operator","expected_revision":%d,"password":"enrollment-password","store":false}`, profile.Revision)
	response := performJSON(handler, http.MethodPost, "/admin/api/profiles/verify/credentials/password", body, adminSession)
	if response.Code != http.StatusOK {
		t.Fatalf("validate-only status = %d, body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"validated":true`) || !strings.Contains(response.Body.String(), `"password_stored":false`) {
		t.Fatalf("validate-only response = %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "enrollment-password") {
		t.Fatalf("validate-only response leaked the password: %s", response.Body.String())
	}
	if loginCount != 1 {
		t.Fatalf("DSM login count = %d, want exactly one validation login", loginCount)
	}
	// Nothing persisted: the profile keeps no password, binds no account, and its
	// revision does not advance (no MutateProfile on the validate-only path).
	updated, err := repository.Profile(context.Background(), "verify")
	if err != nil {
		t.Fatal(err)
	}
	if updated.PasswordStored || updated.Username != "" || updated.Revision != profile.Revision {
		t.Fatalf("validate-only mutated the profile: %#v", updated)
	}
}

func TestRevealPasswordReturnsStoredPasswordToAdmin(t *testing.T) {
	dsm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		switch req.Form.Get("api") + "." + req.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7}}}`)
		case "SYNO.API.Auth.login":
			fmt.Fprint(w, `{"success":true,"data":{"sid":"temporary-sid"}}`)
		case "SYNO.API.Auth.logout":
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		default:
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer dsm.Close()

	handler, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	profile, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "reveal", URL: dsm.URL})
	if err != nil {
		t.Fatal(err)
	}
	// Nothing stored yet -> 404.
	if missing := performJSON(handler, http.MethodPost, "/admin/api/profiles/reveal/credentials/password/reveal", `{}`, adminSession); missing.Code != http.StatusNotFound {
		t.Fatalf("reveal with no password = %d, want 404", missing.Code)
	}
	// Enroll and store a password.
	enroll := fmt.Sprintf(`{"account":"operator","expected_revision":%d,"password":"top-secret-pw"}`, profile.Revision)
	if resp := performJSON(handler, http.MethodPost, "/admin/api/profiles/reveal/credentials/password", enroll, adminSession); resp.Code != http.StatusOK {
		t.Fatalf("enroll status = %d, body=%s", resp.Code, resp.Body.String())
	}
	// A signed-in admin can reveal the stored plaintext.
	response := performJSON(handler, http.MethodPost, "/admin/api/profiles/reveal/credentials/password/reveal", `{}`, adminSession)
	if response.Code != http.StatusOK {
		t.Fatalf("reveal status = %d, body=%s", response.Code, response.Body.String())
	}
	var out struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Password != "top-secret-pw" {
		t.Fatalf("revealed password = %q, want the stored password", out.Password)
	}
}

func TestAdminWebLoginEnrollmentStoresRenewableVaultSession(t *testing.T) {
	dsm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		if req.Form.Get("api") != "SYNO.API.Auth" || req.Form.Get("method") != "login" || req.Form.Get("type") != "code" || req.Form.Get("code_verifier") == "" {
			t.Errorf("web-login exchange form = %#v", req.Form)
		}
		fmt.Fprint(w, `{"success":true,"data":{"account":"web-operator","sid":"vault-web-sid","synotoken":"vault-web-token","device_id":"vault-web-device"}}`)
	}))
	defer dsm.Close()
	handler, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	if _, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "web", URL: dsm.URL}); err != nil {
		t.Fatal(err)
	}
	started := performJSON(handler, http.MethodPost, "/admin/api/profiles/web/weblogin/start", `{}`, adminSession)
	if started.Code != http.StatusCreated {
		t.Fatalf("start status = %d, body=%s", started.Code, started.Body.String())
	}
	var start struct {
		EnrollmentID string `json:"enrollment_id"`
		State        string `json:"state"`
		LoginURL     string `json:"login_url"`
	}
	if err := json.Unmarshal(started.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	if start.EnrollmentID == "" || start.State == "" || !strings.HasPrefix(start.LoginURL, dsm.URL) {
		t.Fatalf("start response = %#v", start)
	}
	suite := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2b)
	serverKey, err := suite.GenerateKeypair(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	completeBody, _ := json.Marshal(map[string]string{
		"enrollment_id": start.EnrollmentID,
		"code":          "one-time-code",
		"rs":            base64.RawURLEncoding.EncodeToString(serverKey.Public),
		"state":         start.State,
	})
	completed := performJSON(handler, http.MethodPost, "/admin/api/profiles/web/weblogin/complete", string(completeBody), adminSession)
	if completed.Code != http.StatusOK {
		t.Fatalf("complete status = %d, body=%s", completed.Code, completed.Body.String())
	}
	if strings.Contains(completed.Body.String(), "vault-web-sid") || strings.Contains(completed.Body.String(), "vault-web-token") {
		t.Fatalf("web-login response leaked session material: %s", completed.Body.String())
	}
	meta, err := repository.SessionMeta(context.Background(), "web")
	if err != nil || !meta.Present || !meta.CanResume || meta.Account != "web-operator" {
		t.Fatalf("session metadata = %#v, %v", meta, err)
	}
	stored, err := repository.Session(context.Background(), "web")
	if err != nil || stored.SID != "vault-web-sid" || stored.SynoToken != "vault-web-token" || len(stored.LocalPrivateKey) == 0 {
		t.Fatalf("stored session = %#v, %v", stored, err)
	}
	replay := performJSON(handler, http.MethodPost, "/admin/api/profiles/web/weblogin/complete", string(completeBody), adminSession)
	if replay.Code != http.StatusGone {
		t.Fatalf("enrollment replay status = %d", replay.Code)
	}
}

func TestWebLoginExchangeFailureIsLoggedServerSideAndRedacted(t *testing.T) {
	dsm := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fixture rejected exchange", http.StatusBadGateway)
	}))
	defer dsm.Close()
	_, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	var logs bytes.Buffer
	handler, err := New(Options{Repository: repository, Manager: manager, PublicURL: "https://gateway.example", Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := sha256.Sum256(dsm.Certificate().Raw)
	if _, err := repository.CreateProfile(context.Background(), state.ProfileInput{
		Name: "pinned-web", URL: dsm.URL,
		TLSMode: state.TLSPinnedFingerprint, CertificateFingerprint: hex.EncodeToString(fingerprint[:]),
	}); err != nil {
		t.Fatal(err)
	}
	started := performJSON(handler, http.MethodPost, "/admin/api/profiles/pinned-web/weblogin/start", `{}`, adminSession)
	if started.Code != http.StatusCreated {
		t.Fatalf("start status = %d, body=%s", started.Code, started.Body.String())
	}
	var start struct {
		EnrollmentID string `json:"enrollment_id"`
		State        string `json:"state"`
	}
	if err := json.Unmarshal(started.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	oneTimeCode := "secret-one-time-code"
	serverKey := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	completeBody, _ := json.Marshal(map[string]string{
		"enrollment_id": start.EnrollmentID,
		"code":          oneTimeCode,
		"rs":            serverKey,
		"state":         start.State,
	})
	request := httptest.NewRequest(http.MethodPost, "/admin/api/profiles/pinned-web/weblogin/complete", strings.NewReader(string(completeBody)))
	request.Header.Set("Origin", "https://gateway.example")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(requestHeader, "1")
	request.AddCookie(&http.Cookie{Name: administratorCookie, Value: adminSession})
	request = request.WithContext(remotepolicy.WithCorrelationID(request.Context(), "weblogin-request-1"))
	completed := httptest.NewRecorder()
	handler.ServeHTTP(completed, request)
	if completed.Code != http.StatusBadGateway {
		t.Fatalf("complete status = %d, body=%s", completed.Code, completed.Body.String())
	}
	if body := completed.Body.String(); !strings.Contains(body, "DSM web-login exchange failed") || strings.Contains(body, "fingerprint") || strings.Contains(body, dsm.URL) {
		t.Fatalf("web-login failure response must stay redacted: %s", body)
	}
	logText := logs.String()
	if !strings.Contains(logText, "DSM web-login exchange failed") || !strings.Contains(logText, `"nas":"pinned-web"`) || !strings.Contains(logText, `"request_id":"weblogin-request-1"`) {
		t.Fatalf("web-login failure cause missing from server log: %s", logText)
	}
	for _, secret := range []string{oneTimeCode, serverKey, start.State} {
		if strings.Contains(logText, secret) {
			t.Fatalf("server log leaked web-login exchange material %q: %s", secret, logText)
		}
	}
}

func TestPasswordEnrollmentFailureIsLoggedServerSideAndRedacted(t *testing.T) {
	dsm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		switch req.Form.Get("api") + "." + req.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7}}}`)
		case "SYNO.API.Auth.login":
			fmt.Fprint(w, `{"success":false,"error":{"code":400}}`)
		default:
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer dsm.Close()
	_, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	var logs bytes.Buffer
	handler, err := New(Options{Repository: repository, Manager: manager, PublicURL: "https://gateway.example", Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "reject", URL: dsm.URL})
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"account":"operator","expected_revision":%d,"password":"wrong-enrollment-password","otp":"654321"}`, profile.Revision)
	response := performJSON(handler, http.MethodPost, "/admin/api/profiles/reject/credentials/password", body, adminSession)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("enrollment status = %d, body=%s", response.Code, response.Body.String())
	}
	if responseBody := response.Body.String(); !strings.Contains(responseBody, "DSM rejected password enrollment") || strings.Contains(responseBody, "code 400") || strings.Contains(responseBody, "wrong-enrollment-password") {
		t.Fatalf("password enrollment failure response must stay redacted: %s", responseBody)
	}
	logText := logs.String()
	if !strings.Contains(logText, `"nas":"reject"`) || !strings.Contains(logText, "code 400") {
		t.Fatalf("password enrollment failure cause missing from server log: %s", logText)
	}
	if strings.Contains(logText, "wrong-enrollment-password") || strings.Contains(logText, "654321") {
		t.Fatalf("server log leaked an enrollment secret: %s", logText)
	}
}

func TestExportCredentialsRequiresAdministratorReverification(t *testing.T) {
	handler, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	ctx := context.Background()
	if _, err := repository.CreateProfile(ctx, state.ProfileInput{Name: "office", URL: "https://office.example:5001", Username: "operator"}); err != nil {
		t.Fatal(err)
	}
	// A second profile with neither a stored account nor a stored password
	// exercises the empty-field requirement.
	if _, err := repository.CreateProfile(ctx, state.ProfileInput{Name: "lab", URL: "https://10.0.0.9:5001"}); err != nil {
		t.Fatal(err)
	}
	const secret = "export-only-with-admin-password"
	if _, err := repository.SavePassword(ctx, "office", secret); err != nil {
		t.Fatal(err)
	}

	if getResp := performJSON(handler, http.MethodGet, "/admin/api/credentials/export", "", adminSession); getResp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET export status = %d", getResp.Code)
	}

	unauth := performJSON(handler, http.MethodPost, "/admin/api/credentials/export", `{"password":"correct horse battery staple"}`, "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated export status = %d", unauth.Code)
	}

	wrong := performJSON(handler, http.MethodPost, "/admin/api/credentials/export", `{"password":"not the admin password"}`, adminSession)
	if wrong.Code != http.StatusUnauthorized || strings.Contains(wrong.Body.String(), secret) {
		t.Fatalf("wrong-admin-password export status = %d body=%s", wrong.Code, wrong.Body.String())
	}

	ok := performJSON(handler, http.MethodPost, "/admin/api/credentials/export", `{"password":"correct horse battery staple"}`, adminSession)
	if ok.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", ok.Code, ok.Body.String())
	}
	if contentType := ok.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/csv") {
		t.Fatalf("export Content-Type = %q", contentType)
	}
	if disposition := ok.Header().Get("Content-Disposition"); !strings.Contains(disposition, "dsmctl-nas-credentials.csv") {
		t.Fatalf("export Content-Disposition = %q", disposition)
	}
	body := strings.TrimPrefix(ok.Body.String(), "\ufeff")
	records, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("parse export CSV: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("export rows = %d, want header + 2 profiles: %#v", len(records), records)
	}
	if got := strings.Join(records[0], ","); got != "name,host,url,account,password" {
		t.Fatalf("export header = %q", got)
	}
	byName := map[string][]string{}
	for _, row := range records[1:] {
		byName[row[0]] = row
	}
	office, lab := byName["office"], byName["lab"]
	if office == nil || lab == nil {
		t.Fatalf("export missing a profile row: %#v", records)
	}
	if office[1] != "office.example" || office[2] != "https://office.example:5001" || office[3] != "operator" || office[4] != secret {
		t.Fatalf("office row = %#v", office)
	}
	if lab[1] != "10.0.0.9" || lab[3] != "" || lab[4] != "" {
		t.Fatalf("lab row must have an empty account and password: %#v", lab)
	}

	events, err := repository.AuditEvents(ctx, state.AuditQuery{Action: "credential.export", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	var success, denied int
	for _, event := range events {
		if strings.Contains(fmt.Sprintf("%v", event), secret) {
			t.Fatalf("audit event leaked the exported password: %#v", event)
		}
		switch event.Outcome {
		case "success":
			success++
		case "denied":
			denied++
		}
	}
	if success == 0 || denied == 0 {
		t.Fatalf("expected both success and denied credential.export audit events, got success=%d denied=%d", success, denied)
	}
}

func TestExportCredentialsRateLimited(t *testing.T) {
	handler, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	if _, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "office", URL: "https://office.example:5001", Username: "operator"}); err != nil {
		t.Fatal(err)
	}
	var limited bool
	for attempt := 1; attempt <= 6; attempt++ {
		response := performJSON(handler, http.MethodPost, "/admin/api/credentials/export", `{"password":"wrong administrator password"}`, adminSession)
		if response.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("export attempt %d status = %d", attempt, response.Code)
		}
	}
	if !limited {
		t.Fatal("repeated failed exports were not rate limited")
	}
}

func newTestHandler(t *testing.T) (*Handler, *state.Repository, *runtime.Manager, string) {
	t.Helper()
	handler, repository, manager := newUninitializedTestHandler(t, nil)
	token, _, err := repository.CreateAdministrator(context.Background(), "owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	return handler, repository, manager, token
}

func newUninitializedTestHandler(t *testing.T, now func() time.Time) (*Handler, *state.Repository, *runtime.Manager) {
	t.Helper()
	passwordParameters := state.PasswordHashParameters{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	options := state.OpenOptions{PasswordHashParameters: &passwordParameters, Now: now}
	repository, err := state.OpenWithOptions(filepath.Join(t.TempDir(), "gateway.db"), bytes.Repeat([]byte{8}, 32), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	cfg, err := repository.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	manager := runtime.NewManager(cfg, repository, runtime.WithConfigSource(repository), runtime.WithDeviceStore(repository), runtime.WithSessionStore(repository))
	handler, err := New(Options{Repository: repository, Manager: manager, PublicURL: "https://gateway.example", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return handler, repository, manager
}

func performJSON(handler http.Handler, method, path, body, sessionToken string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Origin", "https://gateway.example")
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(requestHeader, "1")
	}
	if sessionToken != "" {
		request.AddCookie(&http.Cookie{Name: administratorCookie, Value: sessionToken})
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func responseCookieValue(t *testing.T, recorder *httptest.ResponseRecorder, name string) string {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || !cookie.Secure || cookie.Path != "/admin" {
				t.Fatalf("administrator cookie flags = %#v", cookie)
			}
			return cookie.Value
		}
	}
	t.Fatalf("response did not set cookie %q", name)
	return ""
}

func TestAdministratorCookieUsesForwardedPortalPrefix(t *testing.T) {
	handler, _, manager, _ := newTestHandler(t)
	defer manager.Close(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/admin/api/login", nil)
	request.Header.Set("X-Forwarded-Prefix", "/dsmctl")
	recorder := httptest.NewRecorder()
	handler.setAdministratorCookie(recorder, request, "test-session", time.Now().Add(time.Hour))
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Path != "/dsmctl/admin" {
		t.Fatalf("portal cookie = %#v", cookies)
	}
}

func mustRuntimeProfile(t *testing.T, repository *state.Repository, name string) config.Profile {
	t.Helper()
	cfg, err := repository.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return cfg.NAS[name]
}
