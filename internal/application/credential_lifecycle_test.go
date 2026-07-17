package application

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

type fakeCredentialStore struct {
	passwords        map[string]bool
	devices          map[string]bool
	envSet           map[string]bool
	sessions         map[string]credentials.SessionMeta
	probeErr         error
	deleteSessionErr error
}

func (store *fakeCredentialStore) HasPassword(_ context.Context, profileName string) (bool, error) {
	if store.probeErr != nil {
		return false, store.probeErr
	}
	return store.passwords[profileName], nil
}

func (store *fakeCredentialStore) HasTrustedDevice(_ context.Context, profileName string) (bool, error) {
	if store.probeErr != nil {
		return false, store.probeErr
	}
	return store.devices[profileName], nil
}

func (store *fakeCredentialStore) DeleteSession(_ context.Context, profileName string) (bool, error) {
	if store.deleteSessionErr != nil {
		return false, store.deleteSessionErr
	}
	existed := store.sessions[profileName].Present
	delete(store.sessions, profileName)
	return existed, nil
}

func (store *fakeCredentialStore) PasswordEnvironment(profileName string, profile config.Profile) (string, bool) {
	name := profile.PasswordEnv
	if name == "" {
		name = credentials.DefaultEnvironmentVariable(profileName)
	}
	return name, store.envSet[name]
}

func (store *fakeCredentialStore) SessionMeta(_ context.Context, profileName string) (credentials.SessionMeta, error) {
	return store.sessions[profileName], nil
}

func credentialTestService(store CredentialStore) *Service {
	cfg := config.New()
	cfg.DefaultNAS = "office"
	cfg.NAS["office"] = config.Profile{URL: "https://office.example:5001", Username: "admin"}
	cfg.NAS["lab"] = config.Profile{URL: "https://lab.example:5001", Username: "admin", PasswordEnv: "LAB_PASSWORD"}
	manager := runtime.NewManager(cfg, credentials.NewEnvironment())
	return NewService(cfg, manager, WithCredentialStore(store))
}

func TestGetAuthStatusReportsAllProfilesWithoutSecrets(t *testing.T) {
	store := &fakeCredentialStore{
		passwords: map[string]bool{"office": true},
		devices:   map[string]bool{"office": true},
		envSet:    map[string]bool{"LAB_PASSWORD": true},
	}
	service := credentialTestService(store)

	result, err := service.GetAuthStatus(context.Background(), "")
	if err != nil {
		t.Fatalf("GetAuthStatus() error = %v", err)
	}
	if len(result.Statuses) != 2 {
		t.Fatalf("statuses = %#v", result.Statuses)
	}
	lab, office := result.Statuses[0], result.Statuses[1]
	if lab.NAS != "lab" || office.NAS != "office" {
		t.Fatalf("status order = %q, %q", lab.NAS, office.NAS)
	}
	if !office.Default || !office.PasswordStored || !office.TrustedDeviceStored || office.PasswordEnv != "DSMCTL_PASSWORD_OFFICE" || office.PasswordEnvSet {
		t.Fatalf("office status = %#v", office)
	}
	if lab.Default || lab.PasswordStored || lab.TrustedDeviceStored || lab.PasswordEnv != "LAB_PASSWORD" || !lab.PasswordEnvSet {
		t.Fatalf("lab status = %#v", lab)
	}
	if office.ClientCached || office.SessionHeld {
		t.Fatalf("office session state = %#v", office)
	}
}

func TestGetAuthStatusFiltersToOneProfile(t *testing.T) {
	service := credentialTestService(&fakeCredentialStore{})
	result, err := service.GetAuthStatus(context.Background(), "lab")
	if err != nil {
		t.Fatalf("GetAuthStatus(lab) error = %v", err)
	}
	if len(result.Statuses) != 1 || result.Statuses[0].NAS != "lab" {
		t.Fatalf("statuses = %#v", result.Statuses)
	}
	if _, err := service.GetAuthStatus(context.Background(), "missing"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("GetAuthStatus(missing) error = %v", err)
	}
}

func TestGetAuthStatusSurfacesStoreErrorPerProfile(t *testing.T) {
	service := credentialTestService(&fakeCredentialStore{probeErr: errors.New("keychain is locked")})
	result, err := service.GetAuthStatus(context.Background(), "")
	if err != nil {
		t.Fatalf("GetAuthStatus() error = %v", err)
	}
	for _, status := range result.Statuses {
		if !strings.Contains(status.StoreError, "keychain is locked") {
			t.Fatalf("status = %#v", status)
		}
		if status.PasswordStored || status.TrustedDeviceStored {
			t.Fatalf("status with probe error claimed stored credentials: %#v", status)
		}
	}
}

func TestGetAuthStatusRequiresStore(t *testing.T) {
	cfg := config.New()
	manager := runtime.NewManager(cfg, credentials.NewEnvironment())
	service := NewService(cfg, manager)
	if _, err := service.GetAuthStatus(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "credential store") {
		t.Fatalf("GetAuthStatus() error = %v", err)
	}
}

// fakeSessionStore backs the runtime manager in Logout tests; it stands in
// for the OS keyring's session entries.
type fakeSessionStore struct {
	sessions map[string]credentials.SessionCredential
}

func (f *fakeSessionStore) Session(_ context.Context, name string) (credentials.SessionCredential, error) {
	return f.sessions[name], nil
}

func (f *fakeSessionStore) SaveSession(_ context.Context, name string, session credentials.SessionCredential) error {
	f.sessions[name] = session
	return nil
}

func (f *fakeSessionStore) DeleteSession(_ context.Context, name string) (bool, error) {
	_, ok := f.sessions[name]
	delete(f.sessions, name)
	return ok, nil
}

// logoutTestService wires a Service whose manager can reach a fake DSM for
// revocation and whose credential store tracks the local removal.
func logoutTestService(nasURL string, sessionStore *fakeSessionStore, store CredentialStore) *Service {
	cfg := config.New()
	cfg.DefaultNAS = "office"
	cfg.NAS["office"] = config.Profile{URL: nasURL, Username: "admin"}
	manager := runtime.NewManager(cfg, credentials.NewEnvironment(), runtime.WithSessionStore(sessionStore))
	return NewService(cfg, manager, WithCredentialStore(store))
}

func newLogoutDSMServer(t *testing.T, wantSID string, logoutCount *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7}}}`)
		case "SYNO.API.Auth.logout":
			*logoutCount++
			if r.Form.Get("_sid") != wantSID {
				t.Errorf("logout SID = %q, want %q", r.Form.Get("_sid"), wantSID)
			}
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		default:
			t.Errorf("unexpected API call %s.%s", r.Form.Get("api"), r.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
}

func TestLogoutRevokesServerSideAndRemovesStoredSession(t *testing.T) {
	logoutCount := 0
	server := newLogoutDSMServer(t, "stored-sid", &logoutCount)
	defer server.Close()

	sessionStore := &fakeSessionStore{sessions: map[string]credentials.SessionCredential{
		"office": {SID: "stored-sid", SynoToken: "stored-token"},
	}}
	store := &fakeCredentialStore{sessions: map[string]credentials.SessionMeta{
		"office": {Present: true},
	}}
	service := logoutTestService(server.URL, sessionStore, store)

	result, err := service.Logout(context.Background(), "")
	if err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	want := LogoutResult{NAS: "office", Revoked: true, Removed: true, Configured: true}
	if result != want {
		t.Fatalf("Logout() = %#v, want %#v", result, want)
	}
	if logoutCount != 1 {
		t.Fatalf("logout called %d times, want 1", logoutCount)
	}
}

func TestLogoutRemovesLocallyWhenRevocationFails(t *testing.T) {
	server := newLogoutDSMServer(t, "stored-sid", new(int))
	url := server.URL
	server.Close()

	sessionStore := &fakeSessionStore{sessions: map[string]credentials.SessionCredential{
		"office": {SID: "stored-sid"},
	}}
	store := &fakeCredentialStore{sessions: map[string]credentials.SessionMeta{
		"office": {Present: true},
	}}
	service := logoutTestService(url, sessionStore, store)

	result, err := service.Logout(context.Background(), "office")
	if err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if result.Revoked || result.RevocationError == "" {
		t.Fatalf("Logout() = %#v, want a revocation error and Revoked=false", result)
	}
	if !result.Removed || !result.Configured {
		t.Fatalf("Logout() = %#v, want the local copy removed for a configured profile", result)
	}
}

func TestLogoutHandlesOrphanedProfilesLocally(t *testing.T) {
	sessionStore := &fakeSessionStore{sessions: map[string]credentials.SessionCredential{
		"retired": {SID: "stored-sid"},
	}}
	store := &fakeCredentialStore{sessions: map[string]credentials.SessionMeta{
		"retired": {Present: true},
	}}
	service := logoutTestService("https://office.example:5001", sessionStore, store)

	result, err := service.Logout(context.Background(), "retired")
	if err != nil {
		t.Fatalf("Logout(orphan) error = %v", err)
	}
	want := LogoutResult{NAS: "retired", Removed: true}
	if result != want {
		t.Fatalf("Logout(orphan) = %#v, want %#v", result, want)
	}
	if _, err := service.Logout(context.Background(), "bad name!"); err == nil || !strings.Contains(err.Error(), "invalid NAS name") {
		t.Fatalf("invalid name error = %v", err)
	}
}

func TestLogoutReportsRemovalFailureAfterRevocation(t *testing.T) {
	logoutCount := 0
	server := newLogoutDSMServer(t, "stored-sid", &logoutCount)
	defer server.Close()

	sessionStore := &fakeSessionStore{sessions: map[string]credentials.SessionCredential{
		"office": {SID: "stored-sid"},
	}}
	store := &fakeCredentialStore{deleteSessionErr: errors.New("keychain is locked")}
	service := logoutTestService(server.URL, sessionStore, store)

	result, err := service.Logout(context.Background(), "office")
	if err == nil || !strings.Contains(err.Error(), "was revoked") {
		t.Fatalf("Logout() error = %v, want a removal failure that mentions the completed revocation", err)
	}
	if !result.Revoked || result.Removed {
		t.Fatalf("Logout() = %#v, want Revoked=true and Removed=false", result)
	}
}
