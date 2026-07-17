package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
)

// CredentialStore is the credential presence and removal surface the
// application layer needs. It is implemented by *credentials.SecureStore and
// never exposes secret values to its callers.
type CredentialStore interface {
	HasPassword(ctx context.Context, profileName string) (bool, error)
	HasTrustedDevice(ctx context.Context, profileName string) (bool, error)
	DeletePassword(ctx context.Context, profileName string) (bool, error)
	DeleteTrustedDevice(ctx context.Context, profileName string) (bool, error)
	PasswordEnvironment(profileName string, profile config.Profile) (string, bool)
	SessionMeta(ctx context.Context, profileName string) (credentials.SessionMeta, error)
}

func WithCredentialStore(store CredentialStore) ServiceOption {
	return func(service *Service) {
		if store != nil {
			service.credentialStore = store
		}
	}
}

// AuthStatus reports credential presence and in-process session state for
// one NAS profile. It contains booleans and the password environment
// variable name only; passwords, device IDs, and device names never appear.
type AuthStatus struct {
	NAS                 string `json:"nas" jsonschema:"NAS profile name"`
	Default             bool   `json:"default" jsonschema:"Whether this is the default NAS"`
	PasswordStored      bool   `json:"password_stored" jsonschema:"A password for this profile exists in the OS credential store; the value is never returned"`
	TrustedDeviceStored bool   `json:"trusted_device_stored" jsonschema:"A DSM trusted-device credential exists in the OS credential store; name and ID are never returned"`
	PasswordEnv         string `json:"password_env" jsonschema:"Environment variable name consulted as the password fallback; only the name is reported"`
	PasswordEnvSet      bool   `json:"password_env_set" jsonschema:"Whether that variable is currently set to a non-empty value in this process"`
	SessionStored       bool   `json:"session_stored" jsonschema:"A web-login session is stored in the OS credential store; secrets are never returned"`
	SessionRenewable    bool   `json:"session_renewable" jsonschema:"The stored session carries renewal (Noise resume) keys so it can be refreshed without a browser"`
	Account             string `json:"account,omitempty" jsonschema:"DSM account the stored session belongs to"`
	ClientCached        bool   `json:"client_cached" jsonschema:"A DSM client for this profile exists in this process"`
	SessionHeld         bool   `json:"session_held" jsonschema:"The in-process client holds a DSM session ID from an earlier login; it may have expired server-side and is reported without contacting the NAS"`
	StoreError          string `json:"store_error,omitempty" jsonschema:"OS credential store probe failure, if any; never contains secret values"`
}

type AuthStatusResult struct {
	Statuses []AuthStatus `json:"statuses" jsonschema:"Per-NAS credential presence and in-process session status; secret values are never returned"`
}

// CredentialScope selects which stored credentials RemoveCredentials
// deletes. At least one field must be true.
type CredentialScope struct {
	Password      bool `json:"password"`
	TrustedDevice bool `json:"trusted_device"`
}

type CredentialRemoval struct {
	NAS                  string `json:"nas" jsonschema:"NAS profile the removal applied to"`
	PasswordRemoved      bool   `json:"password_removed" jsonschema:"A stored password existed and was deleted"`
	TrustedDeviceRemoved bool   `json:"trusted_device_removed" jsonschema:"A stored trusted-device credential existed and was deleted"`
}

// GetAuthStatus reports credential presence and in-process session state.
// It never resolves passwords, never returns secret values, and never
// contacts a NAS. An empty name reports every configured profile; a store
// probe failure is reported per profile instead of failing the listing.
func (s *Service) GetAuthStatus(ctx context.Context, requestedNAS string) (AuthStatusResult, error) {
	if s.credentialStore == nil {
		return AuthStatusResult{}, errors.New("credential status requires the OS credential store, which is not configured for this process")
	}
	summaries := s.config.Summaries(credentials.DefaultEnvironmentVariable)
	requested := strings.TrimSpace(requestedNAS)
	if requested != "" {
		var match *config.Summary
		for index := range summaries {
			if summaries[index].Name == requested {
				match = &summaries[index]
				break
			}
		}
		if match == nil {
			return AuthStatusResult{}, fmt.Errorf("NAS profile %q is not configured", requested)
		}
		summaries = []config.Summary{*match}
	}
	result := AuthStatusResult{Statuses: make([]AuthStatus, 0, len(summaries))}
	for _, summary := range summaries {
		status := AuthStatus{NAS: summary.Name, Default: summary.Default}
		profile := s.config.NAS[summary.Name]
		status.PasswordEnv, status.PasswordEnvSet = s.credentialStore.PasswordEnvironment(summary.Name, profile)
		var probeErrors []error
		if stored, err := s.credentialStore.HasPassword(ctx, summary.Name); err != nil {
			probeErrors = append(probeErrors, err)
		} else {
			status.PasswordStored = stored
		}
		if stored, err := s.credentialStore.HasTrustedDevice(ctx, summary.Name); err != nil {
			probeErrors = append(probeErrors, err)
		} else {
			status.TrustedDeviceStored = stored
		}
		if meta, err := s.credentialStore.SessionMeta(ctx, summary.Name); err != nil {
			probeErrors = append(probeErrors, err)
		} else {
			status.SessionStored = meta.Present
			status.SessionRenewable = meta.CanResume
			status.Account = meta.Account
		}
		if len(probeErrors) > 0 {
			status.StoreError = errors.Join(probeErrors...).Error()
		}
		session := s.manager.SessionInfo(summary.Name)
		status.ClientCached, status.SessionHeld = session.ClientCached, session.SessionHeld
		result.Statuses = append(result.Statuses, status)
	}
	return result, nil
}

// RemoveCredentials deletes stored secrets for one profile. An explicitly
// named profile does not need to exist in the configuration, so credentials
// orphaned by an earlier profile removal stay removable. The action is
// local-only and reversible by running auth login again, which is why it is
// exempt from the plan/apply contract.
func (s *Service) RemoveCredentials(ctx context.Context, requestedNAS string, scope CredentialScope) (CredentialRemoval, error) {
	if s.credentialStore == nil {
		return CredentialRemoval{}, errors.New("credential removal requires the OS credential store, which is not configured for this process")
	}
	if !scope.Password && !scope.TrustedDevice {
		return CredentialRemoval{}, errors.New("credential removal requires at least one of password or trusted device")
	}
	name := strings.TrimSpace(requestedNAS)
	if name == "" {
		resolved, _, err := s.config.Resolve("")
		if err != nil {
			return CredentialRemoval{}, err
		}
		name = resolved
	} else if err := config.ValidateName(name); err != nil {
		return CredentialRemoval{}, fmt.Errorf("invalid NAS name %q: %w", name, err)
	}
	removal := CredentialRemoval{NAS: name}
	if scope.Password {
		removed, err := s.credentialStore.DeletePassword(ctx, name)
		if err != nil {
			return CredentialRemoval{}, err
		}
		removal.PasswordRemoved = removed
	}
	if scope.TrustedDevice {
		removed, err := s.credentialStore.DeleteTrustedDevice(ctx, name)
		if err != nil {
			return CredentialRemoval{}, err
		}
		removal.TrustedDeviceRemoved = removed
	}
	return removal, nil
}

var _ CredentialStore = (*credentials.SecureStore)(nil)
