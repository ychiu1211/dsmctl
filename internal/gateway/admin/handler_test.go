package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/flynn/noise"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/gateway/platformauth"
	"github.com/ychiu1211/dsmctl/internal/gateway/state"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

func TestBootstrapAndAuthenticatedProfileCRUD(t *testing.T) {
	handler, repository, manager, bootstrap := newTestHandler(t)
	defer manager.Close(context.Background())

	unauthorized := performJSON(handler, http.MethodGet, "/admin/api/profiles", "", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	bootstrapResponse := performJSON(handler, http.MethodPost, "/admin/api/bootstrap", `{"token":"`+bootstrap+`"}`, "")
	if bootstrapResponse.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d, body=%s", bootstrapResponse.Code, bootstrapResponse.Body.String())
	}
	var established map[string]string
	if err := json.Unmarshal(bootstrapResponse.Body.Bytes(), &established); err != nil {
		t.Fatal(err)
	}
	adminToken := established["admin_token"]
	if adminToken == "" {
		t.Fatal("bootstrap did not return the one-time admin token")
	}
	replay := performJSON(handler, http.MethodPost, "/admin/api/bootstrap", `{"token":"`+bootstrap+`"}`, "")
	if replay.Code != http.StatusConflict {
		t.Fatalf("bootstrap replay status = %d", replay.Code)
	}

	createBody := `{"name":"office","url":"https://office.example:5001","username":"operator","tls_mode":"system_ca"}`
	created := performJSON(handler, http.MethodPost, "/admin/api/profiles", createBody, adminToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", created.Code, created.Body.String())
	}
	profile, err := repository.Profile(context.Background(), "office")
	if err != nil || profile.Revision != 1 || !profile.Default {
		t.Fatalf("created profile=%#v err=%v", profile, err)
	}

	updateBody := `{"expected_revision":1,"url":"https://office-new.example:5001","username":"operator","tls_mode":"system_ca"}`
	updated := performJSON(handler, http.MethodPut, "/admin/api/profiles/office", updateBody, adminToken)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", updated.Code, updated.Body.String())
	}
	conflict := performJSON(handler, http.MethodPut, "/admin/api/profiles/office", updateBody, adminToken)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("stale update status = %d", conflict.Code)
	}

	secret := "plaintext-must-never-enter-admin-output"
	if _, err := repository.SavePassword(context.Background(), "office", secret); err != nil {
		t.Fatal(err)
	}
	listed := performJSON(handler, http.MethodGet, "/admin/api/profiles", "", adminToken)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), secret) {
		t.Fatalf("list status/body = %d %s", listed.Code, listed.Body.String())
	}
	if !strings.Contains(listed.Body.String(), `"password_stored":true`) {
		t.Fatalf("credential presence missing from list: %s", listed.Body.String())
	}
}

func TestPlatformAdministrationRequiresSignedOneTimeAssertion(t *testing.T) {
	repository, err := state.Open(filepath.Join(t.TempDir(), "gateway.db"), bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	if err := repository.EnablePlatformAdministration(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg, _ := repository.Snapshot(context.Background())
	manager := runtime.NewManager(cfg, repository, runtime.WithConfigSource(repository), runtime.WithDeviceStore(repository), runtime.WithSessionStore(repository))
	defer manager.Close(context.Background())
	key := bytes.Repeat([]byte{9}, 32)
	signer, _ := platformauth.NewSigner(key, platformauth.DefaultAudience)
	verifier, _ := platformauth.NewVerifier(key, platformauth.DefaultAudience)
	handler, err := New(Options{Repository: repository, Manager: manager, PlatformVerifier: verifier})
	if err != nil {
		t.Fatal(err)
	}
	if response := performJSON(handler, http.MethodPost, "/admin/api/bootstrap", `{}`, ""); response.Code != http.StatusNotFound {
		t.Fatalf("platform bootstrap status = %d", response.Code)
	}
	if response := performJSON(handler, http.MethodGet, "/admin/api/status", "", "local-token"); response.Code != http.StatusUnauthorized {
		t.Fatalf("local token status = %d", response.Code)
	}
	assertion, err := signer.Sign("administrator")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/api/status", nil)
	request.Header.Set(platformauth.HeaderName, assertion)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("signed assertion status = %d body=%s", response.Code, response.Body.String())
	}
	replayed := httptest.NewRecorder()
	handler.ServeHTTP(replayed, request.Clone(context.Background()))
	if replayed.Code != http.StatusUnauthorized {
		t.Fatalf("replayed assertion status = %d", replayed.Code)
	}
}

func TestProfileMutationEvictsOnlyChangedNAS(t *testing.T) {
	_, repository, manager, bootstrap := newTestHandler(t)
	ctx := context.Background()
	adminToken, err := repository.EstablishAdministrator(ctx, bootstrap)
	if err != nil || adminToken == "" {
		t.Fatal(err)
	}
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
	handler, repository, manager, bootstrap := newTestHandler(t)
	defer manager.Close(context.Background())
	adminToken, err := repository.EstablishAdministrator(context.Background(), bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "office", URL: "https://office.example"})
	if err != nil {
		t.Fatal(err)
	}
	created := performJSON(handler, http.MethodPost, "/admin/api/mcp-tokens", `{"name":"automation","scopes":["nas.apply"],"nas_allowlist":["office"]}`, adminToken)
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
	listed := performJSON(handler, http.MethodGet, "/admin/api/mcp-tokens", "", adminToken)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), issued.BearerToken) {
		t.Fatalf("list tokens=%d %s", listed.Code, listed.Body.String())
	}
	hash := strings.Repeat("a", 64)
	approvalBody := fmt.Sprintf(`{"plan_hash":%q,"nas":"office","profile_revision":%d,"requesting_token_id":%q}`, hash, profile.Revision, issued.Token.ID)
	approved := performJSON(handler, http.MethodPost, "/admin/api/approvals", approvalBody, adminToken)
	if approved.Code != http.StatusCreated {
		t.Fatalf("create approval=%d %s", approved.Code, approved.Body.String())
	}
	audit := performJSON(handler, http.MethodGet, "/admin/api/audit?limit=50", "", adminToken)
	if audit.Code != http.StatusOK || !strings.Contains(audit.Body.String(), "token.lifecycle") || !strings.Contains(audit.Body.String(), "approval.lifecycle") || strings.Contains(audit.Body.String(), issued.BearerToken) {
		t.Fatalf("audit=%d %s", audit.Code, audit.Body.String())
	}
	export := performJSON(handler, http.MethodGet, "/admin/api/audit/export?limit=50", "", adminToken)
	if export.Code != http.StatusOK || export.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("audit export=%d headers=%v", export.Code, export.Header())
	}
	revoked := performJSON(handler, http.MethodDelete, "/admin/api/mcp-tokens/"+issued.Token.ID, "", adminToken)
	if revoked.Code != http.StatusOK || !strings.Contains(revoked.Body.String(), "revoked_at") {
		t.Fatalf("revoke=%d %s", revoked.Code, revoked.Body.String())
	}
}

func TestMutatingAdminRequestFailsBeforeMutationWhenAuditUnavailable(t *testing.T) {
	fail := atomic.Bool{}
	repository, err := state.OpenWithOptions(filepath.Join(t.TempDir(), "gateway.db"), bytes.Repeat([]byte{4}, 32), state.OpenOptions{AuditFailure: func() error {
		if fail.Load() {
			return errors.New("offline")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	bootstrap := "bootstrap-token-0123456789abcdef0123456789"
	if err := repository.ConfigureBootstrap(context.Background(), bootstrap); err != nil {
		t.Fatal(err)
	}
	adminToken, err := repository.EstablishAdministrator(context.Background(), bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "office", URL: "https://office.example"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := repository.Snapshot(context.Background())
	manager := runtime.NewManager(cfg, repository, runtime.WithConfigSource(repository))
	defer manager.Close(context.Background())
	handler, err := New(Options{Repository: repository, Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	response := performJSON(handler, http.MethodPost, "/admin/api/mcp-tokens", `{"name":"must-not-exist","nas_allowlist":["office"]}`, adminToken)
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
	handler, repository, manager, bootstrap := newTestHandler(t)
	defer manager.Close(context.Background())
	adminToken, err := repository.EstablishAdministrator(context.Background(), bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	pin := strings.Repeat("a", 64)
	unconfirmed := performJSON(handler, http.MethodPost, "/admin/api/profiles", `{"name":"pinned","url":"https://pinned.example:5001","tls_mode":"pinned_fingerprint","certificate_fingerprint":"`+pin+`"}`, adminToken)
	if unconfirmed.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed pin status = %d, body=%s", unconfirmed.Code, unconfirmed.Body.String())
	}
	confirmed := performJSON(handler, http.MethodPost, "/admin/api/profiles", `{"name":"pinned","url":"https://pinned.example:5001","tls_mode":"pinned_fingerprint","certificate_fingerprint":"`+pin+`","confirm_certificate_fingerprint":true}`, adminToken)
	if confirmed.Code != http.StatusCreated {
		t.Fatalf("confirmed pin status = %d, body=%s", confirmed.Code, confirmed.Body.String())
	}
}

func TestAdminUIHasNoEmbeddedCredential(t *testing.T) {
	handler, _, manager, _ := newTestHandler(t)
	defer manager.Close(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "dsmctl Gateway") {
		t.Fatalf("UI response = %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("UI response has no content security policy")
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

	handler, repository, manager, bootstrap := newTestHandler(t)
	defer manager.Close(context.Background())
	adminToken, err := repository.EstablishAdministrator(context.Background(), bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "mfa", URL: dsm.URL, Username: "operator"}); err != nil {
		t.Fatal(err)
	}
	body := `{"password":"enrollment-password","otp":"654321"}`
	response := performJSON(handler, http.MethodPost, "/admin/api/profiles/mfa/credentials/password", body, adminToken)
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
	handler, repository, manager, bootstrap := newTestHandler(t)
	defer manager.Close(context.Background())
	adminToken, err := repository.EstablishAdministrator(context.Background(), bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "web", URL: dsm.URL}); err != nil {
		t.Fatal(err)
	}
	started := performJSON(handler, http.MethodPost, "/admin/api/profiles/web/weblogin/start", `{}`, adminToken)
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
	completed := performJSON(handler, http.MethodPost, "/admin/api/profiles/web/weblogin/complete", string(completeBody), adminToken)
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
	replay := performJSON(handler, http.MethodPost, "/admin/api/profiles/web/weblogin/complete", string(completeBody), adminToken)
	if replay.Code != http.StatusGone {
		t.Fatalf("enrollment replay status = %d", replay.Code)
	}
}

func newTestHandler(t *testing.T) (*Handler, *state.Repository, *runtime.Manager, string) {
	t.Helper()
	repository, err := state.Open(filepath.Join(t.TempDir(), "gateway.db"), bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	bootstrap := "bootstrap-token-0123456789abcdef0123456789"
	if err := repository.ConfigureBootstrap(context.Background(), bootstrap); err != nil {
		t.Fatal(err)
	}
	cfg, err := repository.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	manager := runtime.NewManager(cfg, repository, runtime.WithConfigSource(repository), runtime.WithDeviceStore(repository), runtime.WithSessionStore(repository))
	handler, err := New(Options{Repository: repository, Manager: manager, PublicURL: "https://gateway.example"})
	if err != nil {
		t.Fatal(err)
	}
	return handler, repository, manager, bootstrap
}

func performJSON(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func mustRuntimeProfile(t *testing.T, repository *state.Repository, name string) config.Profile {
	t.Helper()
	cfg, err := repository.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return cfg.NAS[name]
}
