package credentials

import (
	"context"
	"errors"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ychiu1211/dsmctl/internal/config"
)

type memoryKeyring struct {
	values map[string]string
}

func newMemoryKeyring() *memoryKeyring {
	return &memoryKeyring{values: make(map[string]string)}
}

func (m *memoryKeyring) Get(service, user string) (string, error) {
	value, ok := m.values[service+":"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (m *memoryKeyring) Set(service, user, secret string) error {
	m.values[service+":"+user] = secret
	return nil
}

func (m *memoryKeyring) Delete(service, user string) error {
	key := service + ":" + user
	if _, ok := m.values[key]; !ok {
		return keyring.ErrNotFound
	}
	delete(m.values, key)
	return nil
}

func TestSecureStoreUsesKeyringBeforeEnvironment(t *testing.T) {
	backend := newMemoryKeyring()
	backend.values[keyringService+":"+passwordKey("office")] = "keyring-password"
	store := &SecureStore{
		keyring: backend,
		environment: &Environment{lookup: func(string) (string, bool) {
			return "environment-password", true
		}},
	}

	password, err := store.Password(context.Background(), "office", config.Profile{})
	if err != nil {
		t.Fatalf("Password() error = %v", err)
	}
	if password != "keyring-password" {
		t.Fatalf("Password() = %q", password)
	}
}

func TestSecureStoreFallsBackToEnvironment(t *testing.T) {
	store := &SecureStore{
		keyring: newMemoryKeyring(),
		environment: &Environment{lookup: func(name string) (string, bool) {
			return "environment-password", name == "OFFICE_PASSWORD"
		}},
	}

	password, err := store.Password(context.Background(), "office", config.Profile{PasswordEnv: "OFFICE_PASSWORD"})
	if err != nil {
		t.Fatalf("Password() error = %v", err)
	}
	if password != "environment-password" {
		t.Fatalf("Password() = %q", password)
	}
}

func TestSecureStoreRoundTripsTrustedDevice(t *testing.T) {
	store := &SecureStore{keyring: newMemoryKeyring(), environment: &Environment{lookup: func(string) (string, bool) {
		return "", false
	}}}
	want := TrustedDevice{Name: "dsmctl@test-host", ID: "device-id"}
	if err := store.SaveTrustedDevice(context.Background(), "office", want); err != nil {
		t.Fatalf("SaveTrustedDevice() error = %v", err)
	}
	got, err := store.TrustedDevice(context.Background(), "office")
	if err != nil {
		t.Fatalf("TrustedDevice() error = %v", err)
	}
	if got != want {
		t.Fatalf("TrustedDevice() = %#v, want %#v", got, want)
	}
	missing, err := store.TrustedDevice(context.Background(), "lab")
	if err != nil {
		t.Fatalf("TrustedDevice(missing) error = %v", err)
	}
	if missing != (TrustedDevice{}) {
		t.Fatalf("TrustedDevice(missing) = %#v", missing)
	}
}

type errorKeyring struct{}

func (errorKeyring) Get(string, string) (string, error) {
	return "", errors.New("keychain is locked")
}

func (errorKeyring) Set(string, string, string) error {
	return errors.New("keychain is locked")
}

func (errorKeyring) Delete(string, string) error {
	return errors.New("keychain is locked")
}

func TestSecureStoreProbesReportPresenceWithoutValues(t *testing.T) {
	backend := newMemoryKeyring()
	backend.values[keyringService+":"+passwordKey("office")] = "secret"
	store := &SecureStore{keyring: backend, environment: &Environment{lookup: func(string) (string, bool) {
		return "", false
	}}}

	if has, err := store.HasPassword(context.Background(), "office"); err != nil || !has {
		t.Fatalf("HasPassword(office) = %v, %v", has, err)
	}
	if has, err := store.HasPassword(context.Background(), "lab"); err != nil || has {
		t.Fatalf("HasPassword(lab) = %v, %v", has, err)
	}
	if has, err := store.HasTrustedDevice(context.Background(), "office"); err != nil || has {
		t.Fatalf("HasTrustedDevice(office) = %v, %v", has, err)
	}

	broken := &SecureStore{keyring: errorKeyring{}, environment: &Environment{lookup: func(string) (string, bool) {
		return "", false
	}}}
	if _, err := broken.HasPassword(context.Background(), "office"); err == nil {
		t.Fatal("HasPassword() with broken backend returned nil error")
	}
}

func TestSecureStoreDeletesAreIdempotentAndScoped(t *testing.T) {
	backend := newMemoryKeyring()
	store := &SecureStore{keyring: backend, environment: &Environment{lookup: func(string) (string, bool) {
		return "", false
	}}}
	if err := store.SavePassword(context.Background(), "office", "secret"); err != nil {
		t.Fatalf("SavePassword() error = %v", err)
	}
	if err := store.SavePassword(context.Background(), "lab", "other"); err != nil {
		t.Fatalf("SavePassword(lab) error = %v", err)
	}
	if err := store.SaveTrustedDevice(context.Background(), "office", TrustedDevice{Name: "dsmctl@host", ID: "device"}); err != nil {
		t.Fatalf("SaveTrustedDevice() error = %v", err)
	}

	if removed, err := store.DeletePassword(context.Background(), "office"); err != nil || !removed {
		t.Fatalf("DeletePassword() = %v, %v", removed, err)
	}
	if removed, err := store.DeletePassword(context.Background(), "office"); err != nil || removed {
		t.Fatalf("repeat DeletePassword() = %v, %v", removed, err)
	}
	if has, err := store.HasPassword(context.Background(), "lab"); err != nil || !has {
		t.Fatalf("other profile password was affected: %v, %v", has, err)
	}
	if removed, err := store.DeleteTrustedDevice(context.Background(), "office"); err != nil || !removed {
		t.Fatalf("DeleteTrustedDevice() = %v, %v", removed, err)
	}
	if removed, err := store.DeleteTrustedDevice(context.Background(), "office"); err != nil || removed {
		t.Fatalf("repeat DeleteTrustedDevice() = %v, %v", removed, err)
	}
}

func TestPasswordEnvironmentReportsNameAndState(t *testing.T) {
	store := &SecureStore{keyring: newMemoryKeyring(), environment: &Environment{lookup: func(name string) (string, bool) {
		if name == "OFFICE_PASSWORD" {
			return "value", true
		}
		if name == "DSMCTL_PASSWORD_EMPTY" {
			return "", true
		}
		return "", false
	}}}

	name, set := store.PasswordEnvironment("office", config.Profile{PasswordEnv: "OFFICE_PASSWORD"})
	if name != "OFFICE_PASSWORD" || !set {
		t.Fatalf("PasswordEnvironment(explicit) = %q, %v", name, set)
	}
	name, set = store.PasswordEnvironment("lab", config.Profile{})
	if name != "DSMCTL_PASSWORD_LAB" || set {
		t.Fatalf("PasswordEnvironment(default) = %q, %v", name, set)
	}
	name, set = store.PasswordEnvironment("empty", config.Profile{})
	if name != "DSMCTL_PASSWORD_EMPTY" || set {
		t.Fatalf("PasswordEnvironment(empty value) = %q, %v", name, set)
	}
}

func TestSecureStoreUnavailablePasswordHasActionableError(t *testing.T) {
	store := &SecureStore{keyring: newMemoryKeyring(), environment: &Environment{lookup: func(string) (string, bool) {
		return "", false
	}}}
	_, err := store.Password(context.Background(), "office", config.Profile{})
	if err == nil {
		t.Fatal("Password() error = nil")
	}
	if errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("Password() exposed backend not-found error: %v", err)
	}
}
