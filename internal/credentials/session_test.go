package credentials

import (
	"context"
	"testing"
	"time"
)

func newSessionStore(backend keyringBackend) *SecureStore {
	return &SecureStore{keyring: backend, environment: &Environment{lookup: func(string) (string, bool) {
		return "", false
	}}}
}

func sampleSession() SessionCredential {
	return SessionCredential{
		SID:             "sid-abc",
		SynoToken:       "syno-token-xyz",
		ServerPublicKey: []byte{0x01, 0x02, 0x03},
		LocalPublicKey:  []byte{0x04, 0x05, 0x06},
		LocalPrivateKey: []byte{0x07, 0x08, 0x09},
		Account:         "admin",
		IssuedAt:        time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC),
		ExpiresAt:       time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
		LastVerified:    time.Date(2026, 7, 17, 10, 5, 0, 0, time.UTC),
	}
}

func sessionsEqual(a, b SessionCredential) bool {
	if a.SID != b.SID || a.SynoToken != b.SynoToken || a.Account != b.Account {
		return false
	}
	if !bytesEqual(a.ServerPublicKey, b.ServerPublicKey) ||
		!bytesEqual(a.LocalPublicKey, b.LocalPublicKey) ||
		!bytesEqual(a.LocalPrivateKey, b.LocalPrivateKey) {
		return false
	}
	return a.IssuedAt.Equal(b.IssuedAt) && a.ExpiresAt.Equal(b.ExpiresAt) && a.LastVerified.Equal(b.LastVerified)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSecureStoreRoundTripsSession(t *testing.T) {
	store := newSessionStore(newMemoryKeyring())
	want := sampleSession()
	if err := store.SaveSession(context.Background(), "office", want); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	got, err := store.Session(context.Background(), "office")
	if err != nil {
		t.Fatalf("Session() error = %v", err)
	}
	if !sessionsEqual(got, want) {
		t.Fatalf("Session() = %#v, want %#v", got, want)
	}

	missing, err := store.Session(context.Background(), "lab")
	if err != nil {
		t.Fatalf("Session(missing) error = %v", err)
	}
	if !sessionsEqual(missing, SessionCredential{}) {
		t.Fatalf("Session(missing) = %#v, want zero", missing)
	}
}

func TestSecureStoreSaveSessionRejectsUnusable(t *testing.T) {
	store := newSessionStore(newMemoryKeyring())
	if err := store.SaveSession(context.Background(), "office", SessionCredential{}); err == nil {
		t.Fatal("SaveSession(empty) error = nil, want error")
	}
	// A session with only a live SID (no resume keys) is still savable: it is
	// usable until it expires, it just cannot be resumed afterwards.
	if err := store.SaveSession(context.Background(), "office", SessionCredential{SID: "sid-only"}); err != nil {
		t.Fatalf("SaveSession(sid only) error = %v", err)
	}
	// Resume material with no live SID is also savable.
	if err := store.SaveSession(context.Background(), "lab", SessionCredential{
		ServerPublicKey: []byte{0x01}, LocalPrivateKey: []byte{0x02},
	}); err != nil {
		t.Fatalf("SaveSession(keys only) error = %v", err)
	}
}

func TestSecureStoreSessionMetaOmitsSecrets(t *testing.T) {
	store := newSessionStore(newMemoryKeyring())
	if err := store.SaveSession(context.Background(), "office", sampleSession()); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	meta, err := store.SessionMeta(context.Background(), "office")
	if err != nil {
		t.Fatalf("SessionMeta() error = %v", err)
	}
	if !meta.Present || meta.Account != "admin" || !meta.CanResume {
		t.Fatalf("SessionMeta() = %#v", meta)
	}
	if !meta.ExpiresAt.Equal(time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("SessionMeta().ExpiresAt = %v", meta.ExpiresAt)
	}

	absent, err := store.SessionMeta(context.Background(), "lab")
	if err != nil {
		t.Fatalf("SessionMeta(missing) error = %v", err)
	}
	if absent.Present {
		t.Fatalf("SessionMeta(missing).Present = true, want false")
	}
}

func TestSecureStoreSessionMetaCanResumeReflectsKeys(t *testing.T) {
	store := newSessionStore(newMemoryKeyring())
	if err := store.SaveSession(context.Background(), "office", SessionCredential{SID: "sid-only"}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	meta, err := store.SessionMeta(context.Background(), "office")
	if err != nil {
		t.Fatalf("SessionMeta() error = %v", err)
	}
	if !meta.Present || meta.CanResume {
		t.Fatalf("SessionMeta(sid only) = %#v, want present and not resumable", meta)
	}
}

func TestSecureStoreHasAndDeleteSessionAreScoped(t *testing.T) {
	store := newSessionStore(newMemoryKeyring())
	if err := store.SaveSession(context.Background(), "office", sampleSession()); err != nil {
		t.Fatalf("SaveSession(office) error = %v", err)
	}
	if err := store.SaveSession(context.Background(), "lab", sampleSession()); err != nil {
		t.Fatalf("SaveSession(lab) error = %v", err)
	}
	// A stored password for the same profile must not be mistaken for a session.
	if err := store.SavePassword(context.Background(), "office", "secret"); err != nil {
		t.Fatalf("SavePassword() error = %v", err)
	}

	if has, err := store.HasSession(context.Background(), "office"); err != nil || !has {
		t.Fatalf("HasSession(office) = %v, %v", has, err)
	}
	if has, err := store.HasSession(context.Background(), "unknown"); err != nil || has {
		t.Fatalf("HasSession(unknown) = %v, %v", has, err)
	}

	if removed, err := store.DeleteSession(context.Background(), "office"); err != nil || !removed {
		t.Fatalf("DeleteSession(office) = %v, %v", removed, err)
	}
	if removed, err := store.DeleteSession(context.Background(), "office"); err != nil || removed {
		t.Fatalf("repeat DeleteSession(office) = %v, %v", removed, err)
	}
	// Deleting office's session must not touch lab's session or office's password.
	if has, err := store.HasSession(context.Background(), "lab"); err != nil || !has {
		t.Fatalf("HasSession(lab) after deleting office = %v, %v", has, err)
	}
	if has, err := store.HasPassword(context.Background(), "office"); err != nil || !has {
		t.Fatalf("HasPassword(office) after deleting session = %v, %v", has, err)
	}
}

func TestSecureStoreSessionSurfacesBackendErrors(t *testing.T) {
	broken := newSessionStore(errorKeyring{})
	if _, err := broken.Session(context.Background(), "office"); err == nil {
		t.Fatal("Session() with broken backend returned nil error")
	}
	if _, err := broken.HasSession(context.Background(), "office"); err == nil {
		t.Fatal("HasSession() with broken backend returned nil error")
	}
}
