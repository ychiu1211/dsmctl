package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"

	"github.com/ychiu1211/dsmctl/internal/config"
)

const keyringService = "dsmctl"

type keyringBackend interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
	Delete(service, user string) error
}

type systemKeyring struct{}

func (systemKeyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (systemKeyring) Set(service, user, secret string) error {
	return keyring.Set(service, user, secret)
}

func (systemKeyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

// TrustedDevice is the credential DSM returns after a successful OTP login.
// Both fields are authentication material and are stored in the OS keyring.
type TrustedDevice struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// DeviceStore persists the DSM trusted-device credential independently for
// every configured NAS profile.
type DeviceStore interface {
	TrustedDevice(ctx context.Context, profileName string) (TrustedDevice, error)
	SaveTrustedDevice(ctx context.Context, profileName string, device TrustedDevice) error
}

// SecureStore resolves passwords from the OS keyring first, with the existing
// environment variable mechanism retained as an automation fallback.
type SecureStore struct {
	keyring     keyringBackend
	environment *Environment
}

func NewSecureStore() *SecureStore {
	return &SecureStore{keyring: systemKeyring{}, environment: NewEnvironment()}
}

func (s *SecureStore) Password(ctx context.Context, profileName string, profile config.Profile) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	password, keyringErr := s.keyring.Get(keyringService, passwordKey(profileName))
	if keyringErr == nil && password != "" {
		return password, nil
	}

	password, environmentErr := s.environment.Password(ctx, profileName, profile)
	if environmentErr == nil {
		return password, nil
	}
	if keyringErr != nil && !errors.Is(keyringErr, keyring.ErrNotFound) {
		return "", fmt.Errorf("read password for NAS %q from OS credential store: %w", profileName, keyringErr)
	}
	name := profile.PasswordEnv
	if name == "" {
		name = DefaultEnvironmentVariable(profileName)
	}
	return "", fmt.Errorf("password for NAS %q is unavailable; run 'dsmctl auth login --nas %s' or set %s", profileName, profileName, name)
}

func (s *SecureStore) SavePassword(ctx context.Context, profileName, password string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if password == "" {
		return errors.New("password cannot be empty")
	}
	if err := s.keyring.Set(keyringService, passwordKey(profileName), password); err != nil {
		return fmt.Errorf("save password for NAS %q in OS credential store: %w", profileName, err)
	}
	return nil
}

func (s *SecureStore) TrustedDevice(ctx context.Context, profileName string) (TrustedDevice, error) {
	if err := ctx.Err(); err != nil {
		return TrustedDevice{}, err
	}
	secret, err := s.keyring.Get(keyringService, trustedDeviceKey(profileName))
	if errors.Is(err, keyring.ErrNotFound) {
		return TrustedDevice{}, nil
	}
	if err != nil {
		return TrustedDevice{}, fmt.Errorf("read trusted device for NAS %q from OS credential store: %w", profileName, err)
	}
	var device TrustedDevice
	if err := json.Unmarshal([]byte(secret), &device); err != nil {
		return TrustedDevice{}, fmt.Errorf("decode trusted device for NAS %q: %w", profileName, err)
	}
	if device.ID == "" {
		return TrustedDevice{}, fmt.Errorf("trusted device for NAS %q has no device ID", profileName)
	}
	return device, nil
}

func (s *SecureStore) SaveTrustedDevice(ctx context.Context, profileName string, device TrustedDevice) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if device.Name == "" || device.ID == "" {
		return errors.New("trusted device name and ID are required")
	}
	secret, err := json.Marshal(device)
	if err != nil {
		return fmt.Errorf("encode trusted device: %w", err)
	}
	if err := s.keyring.Set(keyringService, trustedDeviceKey(profileName), string(secret)); err != nil {
		return fmt.Errorf("save trusted device for NAS %q in OS credential store: %w", profileName, err)
	}
	return nil
}

// HasPassword reports whether a password for the profile exists in the OS
// credential store. The stored value is never returned.
func (s *SecureStore) HasPassword(ctx context.Context, profileName string) (bool, error) {
	return s.probe(ctx, profileName, passwordKey(profileName), "password")
}

// HasTrustedDevice reports whether a trusted-device credential exists for
// the profile. Device name and ID are both authentication material and are
// never returned.
func (s *SecureStore) HasTrustedDevice(ctx context.Context, profileName string) (bool, error) {
	return s.probe(ctx, profileName, trustedDeviceKey(profileName), "trusted device")
}

// DeletePassword removes the stored password and reports whether an entry
// existed. Deleting a missing entry is not an error.
func (s *SecureStore) DeletePassword(ctx context.Context, profileName string) (bool, error) {
	return s.remove(ctx, profileName, passwordKey(profileName), "password")
}

// DeleteTrustedDevice removes the stored trusted-device credential and
// reports whether an entry existed.
func (s *SecureStore) DeleteTrustedDevice(ctx context.Context, profileName string) (bool, error) {
	return s.remove(ctx, profileName, trustedDeviceKey(profileName), "trusted device")
}

// PasswordEnvironment returns the environment variable name that would be
// consulted as the password fallback for this profile and whether it is
// currently set to a non-empty value. The value itself is never returned.
func (s *SecureStore) PasswordEnvironment(profileName string, profile config.Profile) (string, bool) {
	return s.environment.Status(profileName, profile)
}

func (s *SecureStore) probe(ctx context.Context, profileName, key, kind string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if _, err := s.keyring.Get(keyringService, key); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("probe %s for NAS %q in OS credential store: %w", kind, profileName, err)
	}
	return true, nil
}

func (s *SecureStore) remove(ctx context.Context, profileName, key, kind string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := s.keyring.Delete(keyringService, key); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("delete %s for NAS %q from OS credential store: %w", kind, profileName, err)
	}
	return true, nil
}

func passwordKey(profileName string) string {
	return "password/" + profileName
}

func trustedDeviceKey(profileName string) string {
	return "trusted-device/" + profileName
}

type passwordOverride struct {
	base        Resolver
	profileName string
	password    string
}

// WithPassword returns a resolver that uses an in-memory password for one NAS
// and delegates every other profile to the base resolver. It lets auth login
// verify a password before persisting it.
func WithPassword(base Resolver, profileName, password string) Resolver {
	return &passwordOverride{base: base, profileName: profileName, password: password}
}

func (r *passwordOverride) Password(ctx context.Context, profileName string, profile config.Profile) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if profileName == r.profileName {
		return r.password, nil
	}
	return r.base.Password(ctx, profileName, profile)
}
