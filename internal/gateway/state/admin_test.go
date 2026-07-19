package state

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

var testPasswordHashParameters = PasswordHashParameters{
	MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16,
}

func TestAdministratorPasswordAndPersistentSessionLifecycle(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "gateway.db")
	key := bytes.Repeat([]byte{3}, 32)
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	repository := openAdministratorTestRepository(t, path, key, func() time.Time { return now })

	if err := repository.Ready(context.Background()); !errors.Is(err, ErrAdministratorRequired) {
		t.Fatalf("uninitialized Ready() = %v", err)
	}
	password := "correct horse battery staple"
	firstToken, first, err := repository.CreateAdministrator(context.Background(), "Owner.Admin", password)
	if err != nil {
		t.Fatal(err)
	}
	if first.Username != "owner.admin" || firstToken == "" || !first.ExpiresAt.Equal(now.Add(AdminSessionTTL)) {
		t.Fatalf("first session = %#v token=%q", first, firstToken)
	}
	if err := repository.Ready(context.Background()); err != nil {
		t.Fatalf("initialized Ready() = %v", err)
	}
	if _, _, err := repository.CreateAdministrator(context.Background(), "other", "another very long password"); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("second CreateAdministrator() = %v", err)
	}
	if _, _, err := repository.LoginAdministrator(context.Background(), "owner.admin", "wrong password"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong password login = %v", err)
	}
	secondToken, second, err := repository.LoginAdministrator(context.Background(), "OWNER.ADMIN", password)
	if err != nil || second.ID == first.ID {
		t.Fatalf("second login = %#v, %v", second, err)
	}
	if err := repository.ChangeAdministratorPassword(context.Background(), secondToken, password, "a newer correct horse battery"); err != nil {
		t.Fatalf("change password = %v", err)
	}
	if _, err := repository.AuthenticateAdministratorSession(context.Background(), firstToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old session survived password change: %v", err)
	}
	if _, err := repository.AuthenticateAdministratorSession(context.Background(), secondToken); err != nil {
		t.Fatalf("current session was revoked: %v", err)
	}
	if _, _, err := repository.LoginAdministrator(context.Background(), "owner.admin", password); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old password login = %v", err)
	}
	thirdToken, _, err := repository.LoginAdministrator(context.Background(), "owner.admin", "a newer correct horse battery")
	if err != nil {
		t.Fatalf("new password login = %v", err)
	}
	if err := repository.RevokeOtherAdministratorSessions(context.Background(), thirdToken); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AuthenticateAdministratorSession(context.Background(), secondToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("other session survived revocation: %v", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{password, firstToken, secondToken, thirdToken, "a newer correct horse battery"} {
		if bytes.Contains(contents, []byte(secret)) {
			t.Fatalf("database contains administrator secret %q", secret)
		}
	}

	repository = openAdministratorTestRepository(t, path, key, func() time.Time { return now })
	defer repository.Close()
	if _, err := repository.AuthenticateAdministratorSession(context.Background(), thirdToken); err != nil {
		t.Fatalf("session did not survive restart: %v", err)
	}
	if err := repository.LogoutAdministrator(context.Background(), thirdToken); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AuthenticateAdministratorSession(context.Background(), thirdToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("logged-out session remained valid: %v", err)
	}
}

func TestAdministratorSessionExpiresAndConcurrentSetupHasOneWinner(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "gateway.db")
	key := bytes.Repeat([]byte{4}, 32)
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	repository := openAdministratorTestRepository(t, path, key, func() time.Time { return now })
	defer repository.Close()

	var successes atomic.Int32
	var wait sync.WaitGroup
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, _, err := repository.CreateAdministrator(context.Background(), "owner", "correct horse battery staple"); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrAlreadyInitialized) {
				t.Errorf("concurrent setup error = %v", err)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("setup successes = %d", successes.Load())
	}
	token, _, err := repository.LoginAdministrator(context.Background(), "owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(AdminSessionTTL)
	if _, err := repository.AuthenticateAdministratorSession(context.Background(), token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("session accepted at expiry: %v", err)
	}
}

func TestAdministratorSessionsAreBoundedByEvictingOldest(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	repository := openAdministratorTestRepository(t, filepath.Join(directory, "gateway.db"), bytes.Repeat([]byte{7}, 32), func() time.Time { return now })
	defer repository.Close()
	oldestToken, _, err := repository.CreateAdministrator(context.Background(), "owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	var newestToken string
	for index := 0; index < MaxAdminSessions; index++ {
		now = now.Add(time.Second)
		newestToken, _, err = repository.LoginAdministrator(context.Background(), "owner", "correct horse battery staple")
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.AuthenticateAdministratorSession(context.Background(), oldestToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("oldest session was not evicted: %v", err)
	}
	if _, err := repository.AuthenticateAdministratorSession(context.Background(), newestToken); err != nil {
		t.Fatalf("newest session was evicted: %v", err)
	}
	if err := repository.db.View(func(tx *bolt.Tx) error {
		if count := tx.Bucket(bucketAdminSessions).Stats().KeyN; count != MaxAdminSessions {
			t.Fatalf("administrator session count = %d", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyLegacyPlatformStateMigratesToUninitializedLocalSetup(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "gateway.db")
	key := bytes.Repeat([]byte{5}, 32)
	repository := openAdministratorTestRepository(t, path, key, time.Now)
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		if err := meta.Put(keySchemaVersion, encodeUint64(3)); err != nil {
			return err
		}
		if err := meta.Put(keyAdminMode, []byte("platform")); err != nil {
			return err
		}
		for _, item := range [][]byte{keyAdminUsername, keyAdminPassword, keyAdminInitializedAt} {
			if err := meta.Delete(item); err != nil {
				return err
			}
		}
		return tx.DeleteBucket(bucketAdminSessions)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	repository = openAdministratorTestRepository(t, path, key, time.Now)
	defer repository.Close()
	status, err := repository.AdministratorStatus(context.Background())
	if err != nil || status.Initialized {
		t.Fatalf("migrated status = %#v, %v", status, err)
	}
	if err := repository.Ready(context.Background()); !errors.Is(err, ErrAdministratorRequired) {
		t.Fatalf("migrated Ready() = %v", err)
	}
}

func TestPartialAdministratorRecordNeverReopensSetup(t *testing.T) {
	repository := openAdministratorTestRepository(t, filepath.Join(t.TempDir(), "gateway.db"), bytes.Repeat([]byte{6}, 32), time.Now)
	defer repository.Close()
	if err := repository.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMeta).Put(keyAdminUsername, []byte("owner"))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AdministratorStatus(context.Background()); err == nil {
		t.Fatal("partial administrator record was reported as uninitialized")
	}
	if _, _, err := repository.CreateAdministrator(context.Background(), "owner", "correct horse battery staple"); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("partial record reopened setup: %v", err)
	}
	if err := repository.Ready(context.Background()); err == nil || errors.Is(err, ErrAdministratorRequired) {
		t.Fatalf("partial record readiness did not fail closed: %v", err)
	}
}

func TestAdministratorPasswordMinimumCountsRunes(t *testing.T) {
	if err := validateNewAdministratorPassword(strings.Repeat("界", 11)); err == nil {
		t.Fatal("11-rune administrator password was accepted")
	}
	if err := validateNewAdministratorPassword(strings.Repeat("界", 12)); err != nil {
		t.Fatalf("12-rune administrator password = %v", err)
	}
}

func openAdministratorTestRepository(t *testing.T, path string, key []byte, now func() time.Time) *Repository {
	t.Helper()
	repository, err := OpenWithOptions(path, key, OpenOptions{Now: now, PasswordHashParameters: &testPasswordHashParameters})
	if err != nil {
		t.Fatal(err)
	}
	return repository
}
