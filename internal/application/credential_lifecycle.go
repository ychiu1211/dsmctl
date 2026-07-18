package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
)

// CredentialStore is the credential presence surface the application layer
// needs. It is implemented by the desktop OS store and the gateway encrypted
// vault, and never exposes secret values to its callers.
type CredentialStore interface {
	HasPassword(ctx context.Context, profileName string) (bool, error)
	HasTrustedDevice(ctx context.Context, profileName string) (bool, error)
	DeleteSession(ctx context.Context, profileName string) (bool, error)
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
	PasswordStored      bool   `json:"password_stored" jsonschema:"A password for this profile exists in the configured credential store; the value is never returned"`
	TrustedDeviceStored bool   `json:"trusted_device_stored" jsonschema:"A DSM trusted-device credential exists in the configured credential store; name and ID are never returned"`
	PasswordEnv         string `json:"password_env" jsonschema:"Environment variable name consulted as the password fallback; only the name is reported"`
	PasswordEnvSet      bool   `json:"password_env_set" jsonschema:"Whether that variable is currently set to a non-empty value in this process"`
	SessionStored       bool   `json:"session_stored" jsonschema:"A web-login session is stored in the configured credential store; secrets are never returned"`
	SessionRenewable    bool   `json:"session_renewable" jsonschema:"The stored session carries renewal (Noise resume) keys so it can be refreshed without a browser"`
	Account             string `json:"account,omitempty" jsonschema:"DSM account the stored session belongs to"`
	ClientCached        bool   `json:"client_cached" jsonschema:"A DSM client for this profile exists in this process"`
	SessionHeld         bool   `json:"session_held" jsonschema:"The in-process client holds a DSM session ID from an earlier login; it may have expired server-side and is reported without contacting the NAS"`
	StoreError          string `json:"store_error,omitempty" jsonschema:"Credential store probe failure, if any; never contains secret values"`
}

type AuthStatusResult struct {
	Statuses []AuthStatus `json:"statuses" jsonschema:"Per-NAS credential presence and in-process session status; secret values are never returned"`
}

// GetAuthStatus reports credential presence and in-process session state.
// It never resolves passwords, never returns secret values, and never
// contacts a NAS. An empty name reports every configured profile; a store
// probe failure is reported per profile instead of failing the listing.
func (s *Service) GetAuthStatus(ctx context.Context, requestedNAS string) (AuthStatusResult, error) {
	if s.credentialStore == nil {
		return AuthStatusResult{}, errors.New("credential status requires a configured credential store")
	}
	cfg, err := s.configSnapshot(ctx)
	if err != nil {
		return AuthStatusResult{}, err
	}
	summaries := cfg.Summaries(credentials.DefaultEnvironmentVariable)
	summaries = filterRemoteSummaries(ctx, summaries)
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
		profile := cfg.NAS[summary.Name]
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

// LogoutResult reports what sign-out did for one profile. It contains no
// secret values.
type LogoutResult struct {
	NAS string `json:"nas" jsonschema:"NAS profile the sign-out applied to"`
	// Revoked means DSM accepted the sign-out call for the stored session. DSM
	// answers logout success even for an already-expired session ID, so this
	// confirms the session is no longer usable, not that it was live.
	Revoked bool `json:"revoked" jsonschema:"DSM accepted the sign-out for the stored session"`
	// RevocationError is the best-effort revocation failure, if any. It never
	// blocks the local removal: the session then stays valid server-side only
	// until its own expiry.
	RevocationError string `json:"revocation_error,omitempty" jsonschema:"Why the DSM session could not be revoked server-side; the local copy is still removed"`
	Removed         bool   `json:"removed" jsonschema:"A stored session existed and was deleted from the configured credential store"`
	// Configured reports whether the profile exists in the configuration. An
	// orphaned session (profile already removed) can only be deleted locally,
	// because the NAS URL is unknown.
	Configured bool `json:"configured" jsonschema:"The profile is configured, so its NAS could be contacted for revocation"`
}

// revocationTimeout bounds the best-effort server-side revocation inside
// Logout. Sign-out must stay snappy when the NAS is off: the local removal is
// the part the user is waiting for, and an unreachable NAS's session lapses on
// its own expiry anyway.
const revocationTimeout = 10 * time.Second

// Logout signs out of one NAS profile: it asks DSM to revoke the stored
// web-login session (best-effort, bounded by revocationTimeout) and then
// deletes the stored entry. A revocation failure is reported in the result,
// never as an error, so the local removal always proceeds; a removal failure
// after a successful revocation is an error that says the session was already
// revoked, so a retry only needs to clean the store. An explicitly named
// profile does not need to exist in the configuration, so a session orphaned
// by an earlier profile removal stays removable.
func (s *Service) Logout(ctx context.Context, requestedNAS string) (LogoutResult, error) {
	if s.credentialStore == nil {
		return LogoutResult{}, errors.New("sign-out requires a configured credential store")
	}
	name := strings.TrimSpace(requestedNAS)
	cfg, err := s.configSnapshot(ctx)
	if err != nil {
		return LogoutResult{}, err
	}
	if name == "" {
		resolved, _, err := cfg.Resolve("")
		if err != nil {
			return LogoutResult{}, err
		}
		name = resolved
	} else if err := config.ValidateName(name); err != nil {
		return LogoutResult{}, fmt.Errorf("invalid NAS name %q: %w", name, err)
	}
	result := LogoutResult{NAS: name}
	_, result.Configured = cfg.NAS[name]

	revokeCtx, cancel := context.WithTimeout(ctx, revocationTimeout)
	revoked, err := s.manager.RevokeStoredSession(revokeCtx, name)
	cancel()
	result.Revoked = revoked
	if err != nil {
		result.RevocationError = err.Error()
	}

	removed, err := s.credentialStore.DeleteSession(ctx, name)
	if err != nil {
		if revoked {
			return result, fmt.Errorf("the DSM session was revoked, but the stored copy could not be removed (retry 'auth logout'): %w", err)
		}
		return result, err
	}
	result.Removed = removed
	return result, nil
}

var _ CredentialStore = (*credentials.SecureStore)(nil)
