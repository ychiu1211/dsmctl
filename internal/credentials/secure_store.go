package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	password, keyringErr := s.keyring.Get(keyringService, passwordKey(profileName, ""))
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
	return "", fmt.Errorf("password for NAS %q is unavailable; run 'dsmctl auth login --nas %s', store one with 'dsmctl auth password set --nas %s', or set %s", profileName, profileName, profileName, name)
}

// ErrNoStoredPassword reports that the OS credential store holds no password
// entry for the profile. It deliberately does not consider environment
// fallbacks: reveal and removal operate on the stored entry only.
var ErrNoStoredPassword = errors.New("no password is stored in the OS credential store for this NAS")

// RevealPasswordForAccount reveals a specific account's password from a profile's
// password book. An empty account selects the primary entry (the one auth login
// writes). It reads the OS credential store ONLY — never the environment-variable
// fallback — with the same human-facing gating obligations as RevealPassword. A
// missing entry returns ErrNoStoredPassword.
func (s *SecureStore) RevealPasswordForAccount(ctx context.Context, profileName, account string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	password, err := s.keyring.Get(keyringService, passwordKey(profileName, account))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNoStoredPassword
	}
	if err != nil {
		return "", fmt.Errorf("read password for NAS %q from OS credential store: %w", profileName, err)
	}
	if password == "" {
		return "", ErrNoStoredPassword
	}
	return password, nil
}

// StoredPassword returns the password persisted in the OS credential store for
// the profile's primary account. Unlike Password it never consults environment
// variables, so callers that display or audit the stored entry see exactly what
// the store holds.
func (s *SecureStore) StoredPassword(ctx context.Context, profileName string) (string, error) {
	return s.RevealPasswordForAccount(ctx, profileName, "")
}

// RevealPassword returns the stored plaintext password for a profile from the
// OS credential store ONLY — never the environment-variable fallback. It is the
// single method that yields a plaintext password to a human-facing sink and
// MUST NOT be called from the MCP server, the application service, or any log
// path; callers gate it behind an interactive-terminal check. A missing entry
// returns ErrNoStoredPassword.
func (s *SecureStore) RevealPassword(ctx context.Context, profileName string) (string, error) {
	// Identical to StoredPassword; kept as a distinct, intention-revealing name
	// for the human-gated reveal call sites that must never be reachable from the
	// MCP server or application service.
	return s.StoredPassword(ctx, profileName)
}

func (s *SecureStore) SavePassword(ctx context.Context, profileName, password string) error {
	return s.SavePasswordForAccount(ctx, profileName, "", password)
}

// SavePasswordForAccount stores a password for a named account in a profile's
// password book. An empty account writes the primary entry (identical to
// SavePassword); any other account is stored under an account-scoped key.
func (s *SecureStore) SavePasswordForAccount(ctx context.Context, profileName, account, password string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if password == "" {
		return errors.New("password cannot be empty")
	}
	if err := s.keyring.Set(keyringService, passwordKey(profileName, account), password); err != nil {
		return fmt.Errorf("save password for NAS %q in OS credential store: %w", profileName, err)
	}
	return nil
}

// PasswordForAccount resolves a named account's password. An empty account
// selects the primary and mirrors Password (keyring first, then the environment
// fallback); a named secondary resolves from the OS credential store only. It is
// an internal resolver and its value must never reach MCP or a log.
func (s *SecureStore) PasswordForAccount(ctx context.Context, profileName string, profile config.Profile, account string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(account) == "" {
		return s.Password(ctx, profileName, profile)
	}
	password, err := s.keyring.Get(keyringService, passwordKey(profileName, account))
	if err == nil && password != "" {
		return password, nil
	}
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return "", fmt.Errorf("read password for NAS %q from OS credential store: %w", profileName, err)
	}
	return "", fmt.Errorf("password for account %q on NAS %q is unavailable in the OS credential store", strings.TrimSpace(account), profileName)
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
	return s.probe(ctx, profileName, passwordKey(profileName, ""), "password")
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
	return s.DeletePasswordForAccount(ctx, profileName, "")
}

// DeletePasswordForAccount removes a named account's password from a profile's
// password book. An empty account removes the primary entry.
func (s *SecureStore) DeletePasswordForAccount(ctx context.Context, profileName, account string) (bool, error) {
	return s.remove(ctx, profileName, passwordKey(profileName, account), "password")
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

// passwordKey namespaces a profile's stored password in the OS keyring. An empty
// account is the primary entry ("password/<profile>", what auth login writes);
// any other account is an account-scoped secondary ("password/<profile>#<account>").
func passwordKey(profileName, account string) string {
	account = strings.TrimSpace(account)
	if account == "" {
		return "password/" + profileName
	}
	return "password/" + profileName + "#" + account
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
