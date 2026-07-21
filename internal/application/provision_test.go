package application

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/provision"
)

type fakeProvisioner struct {
	steps           []string
	createErr       error
	disableAdminErr error
	loginErr        error
	completeErr     error
	hardenErr       error
	seenPasswords   []string
	seenScramble    string
}

func (f *fakeProvisioner) EstablishSetupSession(context.Context, provision.Target) error {
	f.steps = append(f.steps, "session")
	return nil
}

func (f *fakeProvisioner) CreateFirstAdmin(_ context.Context, _ provision.Target, req provision.AdminRequest) error {
	f.steps = append(f.steps, "create")
	f.seenPasswords = append(f.seenPasswords, req.Password)
	return f.createErr
}

func (f *fakeProvisioner) DisableBuiltinAdmin(_ context.Context, _ provision.Target, scramble string) error {
	f.steps = append(f.steps, "disable_admin")
	f.seenScramble = scramble
	return f.disableAdminErr
}

func (f *fakeProvisioner) Login(_ context.Context, _ provision.Target, _, password string) error {
	f.steps = append(f.steps, "login")
	f.seenPasswords = append(f.seenPasswords, password)
	return f.loginErr
}

func (f *fakeProvisioner) CompleteSetup(context.Context, provision.Target, provision.SetupOptions) error {
	f.steps = append(f.steps, "complete")
	return f.completeErr
}

func (f *fakeProvisioner) Harden(context.Context, provision.Target, provision.AdminRequest) error {
	f.steps = append(f.steps, "harden")
	return f.hardenErr
}

type fakeSink struct {
	persisted []ProvisionPersist
	created   []ProvisionProfileSpec
	err       error
	createErr error
}

func (s *fakeSink) PersistProvisioned(_ context.Context, p ProvisionPersist) error {
	if s.err != nil {
		return s.err
	}
	s.persisted = append(s.persisted, p)
	return nil
}

func (s *fakeSink) CreateProvisionProfile(_ context.Context, spec ProvisionProfileSpec) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.created = append(s.created, spec)
	return nil
}

// persistOnlySink implements ProvisionSink but not ProvisionProfileCreator, so
// ProvisionDiscoveredNAS must refuse it.
type persistOnlySink struct{}

func (persistOnlySink) PersistProvisioned(context.Context, ProvisionPersist) error { return nil }

func newProvisionService(p Provisioner) *Service {
	return NewService(nil, nil, WithProvisioner(p))
}

func TestProvisionFirstAdminHappyPath(t *testing.T) {
	prov := &fakeProvisioner{}
	sink := &fakeSink{}
	service := newProvisionService(prov)
	req := ProvisionRequest{Name: "lab", URL: "https://10.0.0.9:5001", AdminUser: "operator"}
	target := provision.Target{BaseURL: req.URL}

	result, err := service.ProvisionFirstAdmin(context.Background(), target, req, sink)
	if err != nil {
		t.Fatalf("ProvisionFirstAdmin() error = %v", err)
	}
	if got := strings.Join(prov.steps, ","); got != "session,create,disable_admin,login,complete,harden" {
		t.Fatalf("step order = %q", got)
	}
	if !result.AdministratorCreated || !result.PasswordStored || !result.WizardFinished || !result.Hardened || !result.BuiltinAdminDisabled {
		t.Fatalf("result flags = %#v", result)
	}
	if len(sink.persisted) != 1 || sink.persisted[0].Username != "operator" || sink.persisted[0].Name != "lab" {
		t.Fatalf("sink persisted = %#v", sink.persisted)
	}
	password := sink.persisted[0].Password
	if len(password) < 16 {
		t.Fatalf("stored password too short: %q", password)
	}
	// The generated password must reach the sink and the DSM create/login steps,
	// but never the returned result.
	for _, seen := range prov.seenPasswords {
		if seen != password {
			t.Fatalf("provisioner saw a different password than the sink stored")
		}
	}
	// The built-in admin scramble must be a distinct, non-empty password so the
	// account is never left with the empty setup password once it is disabled.
	if prov.seenScramble == "" || prov.seenScramble == password {
		t.Fatalf("built-in admin scramble must be a distinct non-empty password, got %q", prov.seenScramble)
	}
	if strings.Contains(result.NAS+result.AdminUser+result.URL, password) {
		t.Fatal("result leaked the generated password")
	}
}

func TestProvisionFirstAdminRejectsPlainHTTP(t *testing.T) {
	service := newProvisionService(&fakeProvisioner{})
	req := ProvisionRequest{Name: "lab", URL: "http://10.0.0.9:5000", AdminUser: "operator"}
	_, err := service.ProvisionFirstAdmin(context.Background(), provision.Target{BaseURL: req.URL}, req, &fakeSink{})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("plain-http provision error = %v, want https requirement", err)
	}
}

func TestProvisionFirstAdminRequiresAdminUser(t *testing.T) {
	service := newProvisionService(&fakeProvisioner{})
	req := ProvisionRequest{Name: "lab", URL: "https://10.0.0.9:5001", AdminUser: "  "}
	_, err := service.ProvisionFirstAdmin(context.Background(), provision.Target{BaseURL: req.URL}, req, &fakeSink{})
	if err == nil || !strings.Contains(err.Error(), "administrator username") {
		t.Fatalf("missing-admin error = %v", err)
	}
}

func TestProvisionFirstAdminStopsWhenCreateFails(t *testing.T) {
	prov := &fakeProvisioner{createErr: errors.New("DSM error code 400")}
	sink := &fakeSink{}
	service := newProvisionService(prov)
	req := ProvisionRequest{Name: "lab", URL: "https://10.0.0.9:5001", AdminUser: "operator"}
	result, err := service.ProvisionFirstAdmin(context.Background(), provision.Target{BaseURL: req.URL}, req, sink)
	if err == nil {
		t.Fatal("expected create failure to propagate")
	}
	if result.AdministratorCreated || result.PasswordStored || len(sink.persisted) != 0 {
		t.Fatalf("no credential must be persisted when create fails: %#v / %#v", result, sink.persisted)
	}
}

func TestProvisionFirstAdminFailsWhenSinkFails(t *testing.T) {
	prov := &fakeProvisioner{}
	sink := &fakeSink{err: errors.New("keyring locked")}
	service := newProvisionService(prov)
	req := ProvisionRequest{Name: "lab", URL: "https://10.0.0.9:5001", AdminUser: "operator"}
	result, err := service.ProvisionFirstAdmin(context.Background(), provision.Target{BaseURL: req.URL}, req, sink)
	if err == nil || !strings.Contains(err.Error(), "rotate it") {
		t.Fatalf("sink failure error = %v", err)
	}
	if !result.AdministratorCreated || result.PasswordStored {
		t.Fatalf("account created but password not stored expected: %#v", result)
	}
	// Best-effort steps must not run after a persistence failure.
	if got := strings.Join(prov.steps, ","); got != "session,create,disable_admin,login" {
		t.Fatalf("steps after sink failure = %q", got)
	}
}

func provisionNASService(prov Provisioner, sink ProvisionSink, cred CredentialStore) *Service {
	cfg := config.New()
	cfg.NAS["fresh"] = config.Profile{URL: "https://fresh.example:5001"}
	cfg.DefaultNAS = "fresh"
	return NewService(cfg, nil, WithProvisioner(prov), WithProvisionSink(sink), WithCredentialStore(cred))
}

func TestProvisionNASHappyPathUsesInjectedSink(t *testing.T) {
	prov := &fakeProvisioner{}
	sink := &fakeSink{}
	service := provisionNASService(prov, sink, &fakeCredentialStore{})
	result, err := service.ProvisionNAS(context.Background(), ProvisionRequest{Name: "fresh", AdminUser: "operator"})
	if err != nil {
		t.Fatalf("ProvisionNAS() error = %v", err)
	}
	if !result.AdministratorCreated || !result.PasswordStored {
		t.Fatalf("result = %#v", result)
	}
	if result.URL != "https://fresh.example:5001" {
		t.Fatalf("URL not filled from profile: %q", result.URL)
	}
	if len(sink.persisted) != 1 || sink.persisted[0].Password == "" || sink.persisted[0].Username != "operator" {
		t.Fatalf("sink persisted = %#v", sink.persisted)
	}
}

func TestProvisionNASRefusesAlreadyCredentialledProfile(t *testing.T) {
	prov := &fakeProvisioner{}
	sink := &fakeSink{}
	cred := &fakeCredentialStore{passwords: map[string]bool{"fresh": true}}
	service := provisionNASService(prov, sink, cred)
	_, err := service.ProvisionNAS(context.Background(), ProvisionRequest{Name: "fresh", AdminUser: "operator"})
	if err == nil || !strings.Contains(err.Error(), "already has a stored administrator credential") {
		t.Fatalf("re-provision guard error = %v", err)
	}
	if len(prov.steps) != 0 || len(sink.persisted) != 0 {
		t.Fatalf("guard must run before any provisioning: steps=%v persisted=%#v", prov.steps, sink.persisted)
	}
}

func TestProvisionNASRequiresConfiguredProfileAndSink(t *testing.T) {
	// Missing sink.
	cfg := config.New()
	cfg.NAS["fresh"] = config.Profile{URL: "https://fresh.example:5001"}
	noSink := NewService(cfg, nil, WithProvisioner(&fakeProvisioner{}), WithCredentialStore(&fakeCredentialStore{}))
	if _, err := noSink.ProvisionNAS(context.Background(), ProvisionRequest{Name: "fresh", AdminUser: "operator"}); err == nil || !strings.Contains(err.Error(), "not configured on this server") {
		t.Fatalf("missing-sink error = %v", err)
	}
	// Missing profile.
	service := provisionNASService(&fakeProvisioner{}, &fakeSink{}, &fakeCredentialStore{})
	if _, err := service.ProvisionNAS(context.Background(), ProvisionRequest{Name: "absent", AdminUser: "operator"}); err == nil || !strings.Contains(err.Error(), "add it before provisioning") {
		t.Fatalf("missing-profile error = %v", err)
	}
}

func discoveredProvisionService(prov Provisioner, sink ProvisionSink) *Service {
	cfg := config.New()
	return NewService(cfg, nil, WithProvisioner(prov), WithProvisionSink(sink), WithCredentialStore(&fakeCredentialStore{}))
}

func TestProvisionDiscoveredNASTOFUCreatesProfileAndProvisions(t *testing.T) {
	// A loopback TLS server stands in for a factory-fresh NAS: loopback is
	// LAN-scoped, and TOFU observes its self-signed certificate.
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()

	prov := &fakeProvisioner{}
	sink := &fakeSink{}
	service := discoveredProvisionService(prov, sink)
	result, err := service.ProvisionDiscoveredNAS(context.Background(), ProvisionRequest{URL: server.URL, AdminUser: "operator"})
	if err != nil {
		t.Fatalf("ProvisionDiscoveredNAS() error = %v", err)
	}
	if !result.AdministratorCreated || !result.PasswordStored {
		t.Fatalf("result = %#v", result)
	}
	if len(sink.created) != 1 || sink.created[0].TLSMode != "pinned_fingerprint" || sink.created[0].CertificateFingerprint == "" {
		t.Fatalf("profile not created with a pinned TOFU fingerprint: %#v", sink.created)
	}
	if len(sink.persisted) != 1 || sink.persisted[0].Password == "" {
		t.Fatalf("credential not persisted: %#v", sink.persisted)
	}
	if sink.created[0].Name != sink.persisted[0].Name {
		t.Fatalf("profile name mismatch between create %q and persist %q", sink.created[0].Name, sink.persisted[0].Name)
	}
}

func TestProvisionDiscoveredNASRejectsPublicAndPlainHTTP(t *testing.T) {
	service := discoveredProvisionService(&fakeProvisioner{}, &fakeSink{})
	if _, err := service.ProvisionDiscoveredNAS(context.Background(), ProvisionRequest{URL: "http://127.0.0.1:5000", AdminUser: "operator"}); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("plain-http error = %v", err)
	}
	if _, err := service.ProvisionDiscoveredNAS(context.Background(), ProvisionRequest{URL: "https://8.8.8.8:5001", AdminUser: "operator"}); err == nil || !strings.Contains(err.Error(), "LAN/VPN") {
		t.Fatalf("public-address error = %v", err)
	}
}

func TestProvisionDiscoveredNASRefusesExistingNameAndMissingCreator(t *testing.T) {
	// Existing profile name collision (no network contact needed — the check is
	// before the certificate observation).
	prov := &fakeProvisioner{}
	cfg := config.New()
	cfg.NAS["nas-127.0.0.1"] = config.Profile{URL: "https://127.0.0.1:5001"}
	service := NewService(cfg, nil, WithProvisioner(prov), WithProvisionSink(&fakeSink{}), WithCredentialStore(&fakeCredentialStore{}))
	if _, err := service.ProvisionDiscoveredNAS(context.Background(), ProvisionRequest{URL: "https://127.0.0.1:5001", AdminUser: "operator"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing-name error = %v", err)
	}

	// A sink that cannot create profiles is refused before any network contact.
	noCreator := NewService(config.New(), nil, WithProvisioner(prov), WithProvisionSink(persistOnlySink{}), WithCredentialStore(&fakeCredentialStore{}))
	if _, err := noCreator.ProvisionDiscoveredNAS(context.Background(), ProvisionRequest{URL: "https://127.0.0.1:5001", AdminUser: "operator"}); err == nil || !strings.Contains(err.Error(), "cannot create a profile") {
		t.Fatalf("missing-creator error = %v", err)
	}
}

func TestDeriveProvisionProfileName(t *testing.T) {
	if got := deriveProvisionProfileName("10.17.37.51"); got != "nas-10.17.37.51" {
		t.Fatalf("deriveProvisionProfileName = %q", got)
	}
	if got := deriveProvisionProfileName("!!!"); got != "provisioned-nas" {
		t.Fatalf("deriveProvisionProfileName(junk) = %q", got)
	}
}

func TestProvisionFirstAdminCollectsBestEffortWarnings(t *testing.T) {
	prov := &fakeProvisioner{completeErr: errors.New("hide_welcome rejected"), hardenErr: errors.New("autoblock unsupported")}
	sink := &fakeSink{}
	service := newProvisionService(prov)
	req := ProvisionRequest{Name: "lab", URL: "https://10.0.0.9:5001", AdminUser: "operator"}
	result, err := service.ProvisionFirstAdmin(context.Background(), provision.Target{BaseURL: req.URL}, req, sink)
	if err != nil {
		t.Fatalf("best-effort failures must not fail the operation: %v", err)
	}
	if result.WizardFinished || result.Hardened {
		t.Fatalf("flags should be false on best-effort failure: %#v", result)
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	if !result.AdministratorCreated || !result.PasswordStored {
		t.Fatalf("account should still be created and stored: %#v", result)
	}
}
