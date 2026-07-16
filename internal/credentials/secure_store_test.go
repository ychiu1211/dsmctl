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
