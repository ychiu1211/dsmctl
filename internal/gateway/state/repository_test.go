package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/ychiu1211/dsmctl/internal/credentials"
)

func TestRepositoryProfileCRUDLimitAndCAS(t *testing.T) {
	repository, _ := openTestRepository(t)
	ctx := context.Background()

	created, err := repository.CreateProfile(ctx, ProfileInput{Name: "nas-00", URL: "https://nas-00.example:5001"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || !created.Default || created.TLSMode != TLSSystemCA {
		t.Fatalf("created profile = %#v", created)
	}

	var wait sync.WaitGroup
	results := make(chan error, 2)
	for _, username := range []string{"first", "second"} {
		wait.Add(1)
		go func(username string) {
			defer wait.Done()
			_, err := repository.UpdateProfile(ctx, "nas-00", 1, ProfileInput{URL: "https://nas-00.example:5001", Username: username})
			results <- err
		}(username)
	}
	wait.Wait()
	close(results)
	var successes, conflicts int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("concurrent update error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}

	for index := 1; index < MaxProfiles; index++ {
		name := fmt.Sprintf("nas-%02d", index)
		if _, err := repository.CreateProfile(ctx, ProfileInput{Name: name, URL: "http://" + name + ".example:5000"}); err != nil {
			t.Fatalf("create profile %d: %v", index, err)
		}
	}
	if _, err := repository.CreateProfile(ctx, ProfileInput{Name: "overflow", URL: "https://overflow.example"}); err == nil {
		t.Fatal("33rd profile was accepted")
	}
	profiles, err := repository.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != MaxProfiles {
		t.Fatalf("profile count = %d", len(profiles))
	}
	for index, profile := range profiles {
		want := fmt.Sprintf("nas-%02d", index)
		if profile.Name != want {
			t.Fatalf("profiles[%d].Name = %q, want %q", index, profile.Name, want)
		}
	}
	if err := repository.SetDefault(ctx, "nas-31"); err != nil {
		t.Fatal(err)
	}
	profile, _ := repository.Profile(ctx, "nas-31")
	if !profile.Default {
		t.Fatal("default selection did not persist")
	}
}

func TestVaultEncryptsDistinctNoncesAndSessionRenewalKeepsRevision(t *testing.T) {
	repository, path := openTestRepository(t)
	ctx := context.Background()
	if _, err := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://office.example:5001", Username: "operator"}); err != nil {
		t.Fatal(err)
	}
	password := "known-password-do-not-leak"
	if _, err := repository.SavePassword(ctx, "office", password); err != nil {
		t.Fatal(err)
	}
	firstNonce := secretNonce(t, repository, "office", secretPassword)
	if _, err := repository.SavePassword(ctx, "office", password); err != nil {
		t.Fatal(err)
	}
	secondNonce := secretNonce(t, repository, "office", secretPassword)
	if firstNonce == secondNonce {
		t.Fatal("password rewrite reused an AES-GCM nonce")
	}
	device := credentials.TrustedDevice{Name: "gateway-device", ID: "known-device-id-do-not-leak"}
	if _, err := repository.SaveTrustedDeviceRevision(ctx, "office", device); err != nil {
		t.Fatal(err)
	}
	session := credentials.SessionCredential{
		SID: "known-sid-do-not-leak", SynoToken: "known-synotoken-do-not-leak", Account: "operator",
		ServerPublicKey: bytes.Repeat([]byte{1}, 32), LocalPublicKey: bytes.Repeat([]byte{2}, 32), LocalPrivateKey: bytes.Repeat([]byte{3}, 32),
		IssuedAt: time.Now().UTC(),
	}
	revision, err := repository.EnrollSession(ctx, "office", session)
	if err != nil {
		t.Fatal(err)
	}
	firstSessionNonce := secretNonce(t, repository, "office", secretSession)
	session.SID = "rotated-sid-do-not-leak"
	if err := repository.SaveSession(ctx, "office", session); err != nil {
		t.Fatal(err)
	}
	profile, _ := repository.Profile(ctx, "office")
	if profile.Revision != revision {
		t.Fatalf("session renewal advanced revision from %d to %d", revision, profile.Revision)
	}
	if firstSessionNonce == secretNonce(t, repository, "office", secretSession) {
		t.Fatal("session renewal reused an AES-GCM nonce")
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, plaintext := range [][]byte{[]byte(password), []byte(device.ID), []byte("known-sid-do-not-leak"), []byte("rotated-sid-do-not-leak"), []byte("known-synotoken-do-not-leak")} {
		if bytes.Contains(persisted, plaintext) {
			t.Fatalf("database contains plaintext %q", plaintext)
		}
	}
}

func TestWrongMasterKeyAndBootstrapFailClosed(t *testing.T) {
	repository, path := openTestRepository(t)
	ctx := context.Background()
	bootstrap := "bootstrap-token-0123456789abcdef0123456789"
	if err := repository.ConfigureBootstrap(ctx, bootstrap); err != nil {
		t.Fatal(err)
	}
	adminToken, err := repository.EstablishAdministrator(ctx, bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	if adminToken == "" || repository.AuthenticateAdministrator(ctx, adminToken) != nil {
		t.Fatal("administrator token was not established")
	}
	if _, err := repository.EstablishAdministrator(ctx, bootstrap); !errors.Is(err, ErrBootstrapConsumed) {
		t.Fatalf("bootstrap replay error = %v", err)
	}
	if err := repository.Ready(ctx); err != nil {
		t.Fatalf("Ready() = %v", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if _, err := Open(path, bytes.Repeat([]byte{9}, 32)); err == nil {
		t.Fatal("wrong master key opened the repository")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("wrong-key attempt modified encrypted state")
	}
}

func TestPlatformAdministrationDisablesLocalBootstrap(t *testing.T) {
	repository, _ := openTestRepository(t)
	ctx := context.Background()
	if err := repository.EnablePlatformAdministration(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repository.Ready(ctx); err != nil {
		t.Fatalf("Ready() = %v", err)
	}
	health, err := repository.Health(ctx)
	if err != nil || !health.Initialized || health.AdminMode != AdminModePlatform {
		t.Fatalf("Health() = %#v, %v", health, err)
	}
	bootstrap := "bootstrap-token-0123456789abcdef0123456789"
	if err := repository.ConfigureBootstrap(ctx, bootstrap); err == nil {
		t.Fatal("platform repository accepted generic bootstrap configuration")
	}
	if _, err := repository.EstablishAdministrator(ctx, bootstrap); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("EstablishAdministrator() = %v", err)
	}
	if err := repository.AuthenticateAdministrator(ctx, "anything"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("AuthenticateAdministrator() = %v", err)
	}
	if _, err := repository.RotateAdministrator(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("RotateAdministrator() = %v", err)
	}
}

func TestPlatformAdministrationCannotReplaceLocalAdministration(t *testing.T) {
	repository, _ := openTestRepository(t)
	ctx := context.Background()
	bootstrap := "bootstrap-token-0123456789abcdef0123456789"
	if err := repository.ConfigureBootstrap(ctx, bootstrap); err != nil {
		t.Fatal(err)
	}
	if err := repository.EnablePlatformAdministration(ctx); err == nil {
		t.Fatal("platform administration replaced configured local administration")
	}
}

func TestApplySecretResolvesOnlyByVaultReferenceAndRetainedSecretsStayCleanable(t *testing.T) {
	repository, _ := openTestRepository(t)
	ctx := context.Background()
	profile, err := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://office.example:5001"})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := repository.StoreApplySecret(ctx, "office", "chap-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := repository.ResolveSecret(ctx, "vault:"+metadata.ID)
	if err != nil || resolved != "chap-secret-value" {
		t.Fatalf("ResolveSecret() = %q, %v", resolved, err)
	}
	if _, err := repository.ResolveSecret(ctx, "env:NOT_ALLOWED"); err == nil {
		t.Fatal("gateway vault accepted an environment reference")
	}
	removed, err := repository.DeleteProfile(ctx, "office", profile.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	orphans, err := repository.OrphanedSecrets(ctx)
	if err != nil || len(orphans) != 1 || orphans[0].ID != metadata.ID || removed.ID != metadata.ProfileID {
		t.Fatalf("orphaned secrets = %#v, removed=%#v, err=%v", orphans, removed, err)
	}
	recreated, err := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://replacement.example:5001"})
	if err != nil {
		t.Fatal(err)
	}
	if recreated.ID == removed.ID || recreated.Revision <= removed.Revision {
		t.Fatalf("same-name recreation reused identity/revision: removed=%#v recreated=%#v", removed, recreated)
	}
	deleted, err := repository.DeleteOrphanedSecret(ctx, metadata.ID)
	if err != nil || !deleted {
		t.Fatalf("DeleteOrphanedSecret() = %v, %v", deleted, err)
	}
}

func TestExistingSchemaZeroIsBackedUpBeforeMigration(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "gateway.db")
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketMeta)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	repository, err := Open(path, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	_ = repository.Close()
	backups, err := filepath.Glob(path + ".pre-v0-*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("migration backups = %v, err=%v", backups, err)
	}
}

func TestFailedMigrationRollsBackAndLeavesRecoverableBackup(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "gateway.db")
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		meta, err := tx.CreateBucketIfNotExists(bucketMeta)
		if err != nil {
			return err
		}
		return meta.Put([]byte("legacy_marker"), []byte("still-recoverable"))
	}); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	key := bytes.Repeat([]byte{6}, 32)
	_, err = OpenWithOptions(path, key, OpenOptions{BeforeMigrationCommit: func(_, _ uint64) error {
		return errors.New("injected migration failure")
	}})
	if err == nil || !strings.Contains(err.Error(), "injected migration failure") {
		t.Fatalf("failed migration error = %v", err)
	}
	backups, _ := filepath.Glob(path + ".pre-v0-*.bak")
	if len(backups) != 1 {
		t.Fatalf("migration backups = %v", backups)
	}
	backupDB, err := bolt.Open(backups[0], 0o600, &bolt.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := backupDB.View(func(tx *bolt.Tx) error {
		if got := string(tx.Bucket(bucketMeta).Get([]byte("legacy_marker"))); got != "still-recoverable" {
			t.Fatalf("backup marker = %q", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_ = backupDB.Close()
	repository, err := Open(path, key)
	if err != nil {
		t.Fatalf("repository was not recoverable after rollback: %v", err)
	}
	_ = repository.Close()
}

func openTestRepository(t *testing.T) (*Repository, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gateway.db")
	repository, err := Open(path, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	return repository, path
}

func secretNonce(t *testing.T, repository *Repository, profileName, secretType string) string {
	t.Helper()
	var nonce string
	err := repository.db.View(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, profileName)
		if err != nil {
			return err
		}
		id := map[string]string{secretPassword: record.PasswordSecretID, secretTrustedDevice: record.TrustedDeviceSecretID, secretSession: record.SessionSecretID}[secretType]
		var sealed sealedSecret
		if err := json.Unmarshal(tx.Bucket(bucketSecrets).Get([]byte(id)), &sealed); err != nil {
			return err
		}
		nonce = sealed.Nonce
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return nonce
}
