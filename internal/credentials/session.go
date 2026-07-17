package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zalando/go-keyring"
)

// SessionCredential is the persisted result of a DSM web login for one NAS
// profile. It bundles the short-lived DSM session (SID + SynoToken) with the
// durable Noise "resume" key material that lets dsmctl re-establish a session
// later without opening a browser and without a password.
//
// Only web-login sessions (DSM Auth v7) carry resume key material; password
// logins cannot be resumed, which is why dsmctl persists a session only for the
// web-login path. Every field except the plain metadata is authentication
// material and is stored only in the OS keyring; it must never be logged or
// displayed. Use SessionMeta for anything user-visible.
type SessionCredential struct {
	// SID and SynoToken are the live DSM session, refreshed on every resume and
	// short-lived server-side.
	SID       string `json:"sid"`
	SynoToken string `json:"syno_token,omitempty"`

	// ServerPublicKey, LocalPublicKey and LocalPrivateKey are the durable
	// Noise_KK_25519 resume material. LocalPrivateKey is the most sensitive
	// value here: whoever holds it can resume this session without the account
	// password.
	ServerPublicKey []byte `json:"server_public_key,omitempty"`
	LocalPublicKey  []byte `json:"local_public_key,omitempty"`
	LocalPrivateKey []byte `json:"local_private_key,omitempty"`

	// DeviceID is the trusted-device identifier DSM issued for this session.
	DeviceID string `json:"device_id,omitempty"`

	// The following fields are non-secret metadata, safe to surface through
	// SessionMeta. ExpiresAt is zero when DSM did not report an expiry, in which
	// case validity can only be confirmed by contacting the NAS.
	Account      string    `json:"account,omitempty"`
	IssuedAt     time.Time `json:"issued_at,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	LastVerified time.Time `json:"last_verified,omitempty"`
}

// CanResume reports whether the credential carries the durable Noise key
// material required to re-establish a session without a browser or password.
func (c SessionCredential) CanResume() bool {
	return len(c.ServerPublicKey) > 0 && len(c.LocalPrivateKey) > 0
}

// SessionMeta is the non-secret projection of a stored SessionCredential. It is
// safe to display and deliberately omits the SID, SynoToken, and Noise keys so
// display and status code paths cannot leak authentication material.
type SessionMeta struct {
	Present      bool      `json:"present"`
	Account      string    `json:"account,omitempty"`
	IssuedAt     time.Time `json:"issued_at,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	LastVerified time.Time `json:"last_verified,omitempty"`
	// CanResume mirrors SessionCredential.CanResume without exposing the keys.
	CanResume bool `json:"can_resume"`
}

// SessionStore persists the DSM web-login session (live tokens plus durable
// Noise resume material) independently for each configured NAS profile, so that
// many NAS can be managed at once without their sessions interfering.
type SessionStore interface {
	Session(ctx context.Context, profileName string) (SessionCredential, error)
	SaveSession(ctx context.Context, profileName string, session SessionCredential) error
	DeleteSession(ctx context.Context, profileName string) (bool, error)
}

// Session reads and decodes the stored web-login session for a profile. A
// zero SessionCredential and a nil error mean none is stored. The returned
// value contains secret material.
func (s *SecureStore) Session(ctx context.Context, profileName string) (SessionCredential, error) {
	if err := ctx.Err(); err != nil {
		return SessionCredential{}, err
	}
	secret, err := s.keyring.Get(keyringService, sessionKey(profileName))
	if errors.Is(err, keyring.ErrNotFound) {
		return SessionCredential{}, nil
	}
	if err != nil {
		return SessionCredential{}, fmt.Errorf("read session for NAS %q from OS credential store: %w", profileName, err)
	}
	var session SessionCredential
	if err := json.Unmarshal([]byte(secret), &session); err != nil {
		return SessionCredential{}, fmt.Errorf("decode session for NAS %q: %w", profileName, err)
	}
	return session, nil
}

// SaveSession persists a web-login session for a profile, replacing any
// existing entry. A session with neither a session ID nor resume key material
// is unusable and is rejected.
func (s *SecureStore) SaveSession(ctx context.Context, profileName string, session SessionCredential) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if session.SID == "" && !session.CanResume() {
		return errors.New("session must carry a session ID or resume key material")
	}
	secret, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	if err := s.keyring.Set(keyringService, sessionKey(profileName), string(secret)); err != nil {
		return fmt.Errorf("save session for NAS %q in OS credential store: %w", profileName, err)
	}
	return nil
}

// SessionMeta reports the non-secret metadata of a stored session for display.
// It reads the stored blob but returns only non-secret fields; when no session
// is stored it returns a zero SessionMeta with Present false and a nil error.
func (s *SecureStore) SessionMeta(ctx context.Context, profileName string) (SessionMeta, error) {
	session, err := s.Session(ctx, profileName)
	if err != nil {
		return SessionMeta{}, err
	}
	if session.SID == "" && !session.CanResume() {
		return SessionMeta{}, nil
	}
	return SessionMeta{
		Present:      true,
		Account:      session.Account,
		IssuedAt:     session.IssuedAt,
		ExpiresAt:    session.ExpiresAt,
		LastVerified: session.LastVerified,
		CanResume:    session.CanResume(),
	}, nil
}

// HasSession reports whether a web-login session exists for the profile. No
// secret value is returned.
func (s *SecureStore) HasSession(ctx context.Context, profileName string) (bool, error) {
	return s.probe(ctx, profileName, sessionKey(profileName), "session")
}

// DeleteSession removes the stored session and reports whether an entry
// existed. Deleting a missing entry is not an error.
func (s *SecureStore) DeleteSession(ctx context.Context, profileName string) (bool, error) {
	return s.remove(ctx, profileName, sessionKey(profileName), "session")
}

func sessionKey(profileName string) string {
	return "session/" + profileName
}

var _ SessionStore = (*SecureStore)(nil)
