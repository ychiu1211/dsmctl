package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

type fakeCredentialStore struct {
	passwords map[string]bool
	devices   map[string]bool
	envSet    map[string]bool
	sessions  map[string]credentials.SessionMeta
	probeErr  error
	deleted   []string
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

func (store *fakeCredentialStore) DeletePassword(_ context.Context, profileName string) (bool, error) {
	store.deleted = append(store.deleted, "password/"+profileName)
	existed := store.passwords[profileName]
	delete(store.passwords, profileName)
	return existed, nil
}

func (store *fakeCredentialStore) DeleteTrustedDevice(_ context.Context, profileName string) (bool, error) {
	store.deleted = append(store.deleted, "trusted-device/"+profileName)
	existed := store.devices[profileName]
	delete(store.devices, profileName)
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

func TestRemoveCredentialsScopes(t *testing.T) {
	tests := []struct {
		name  string
		scope CredentialScope
		want  CredentialRemoval
	}{
		{name: "both", scope: CredentialScope{Password: true, TrustedDevice: true}, want: CredentialRemoval{NAS: "office", PasswordRemoved: true, TrustedDeviceRemoved: true}},
		{name: "password only", scope: CredentialScope{Password: true}, want: CredentialRemoval{NAS: "office", PasswordRemoved: true}},
		{name: "device only", scope: CredentialScope{TrustedDevice: true}, want: CredentialRemoval{NAS: "office", TrustedDeviceRemoved: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeCredentialStore{
				passwords: map[string]bool{"office": true},
				devices:   map[string]bool{"office": true},
			}
			service := credentialTestService(store)
			result, err := service.RemoveCredentials(context.Background(), "", test.scope)
			if err != nil {
				t.Fatalf("RemoveCredentials() error = %v", err)
			}
			if result != test.want {
				t.Fatalf("result = %#v, want %#v", result, test.want)
			}
			if test.scope.Password != result.PasswordRemoved || test.scope.TrustedDevice != result.TrustedDeviceRemoved {
				t.Fatalf("scope/result mismatch: %#v", result)
			}
		})
	}
}

func TestRemoveCredentialsSupportsOrphanedProfiles(t *testing.T) {
	store := &fakeCredentialStore{passwords: map[string]bool{"retired": true}, devices: map[string]bool{}}
	service := credentialTestService(store)
	result, err := service.RemoveCredentials(context.Background(), "retired", CredentialScope{Password: true, TrustedDevice: true})
	if err != nil {
		t.Fatalf("RemoveCredentials(orphan) error = %v", err)
	}
	if !result.PasswordRemoved || result.TrustedDeviceRemoved {
		t.Fatalf("result = %#v", result)
	}
	if _, err := service.RemoveCredentials(context.Background(), "bad name!", CredentialScope{Password: true}); err == nil || !strings.Contains(err.Error(), "invalid NAS name") {
		t.Fatalf("invalid name error = %v", err)
	}
	if _, err := service.RemoveCredentials(context.Background(), "office", CredentialScope{}); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("empty scope error = %v", err)
	}
}
