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

	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
)

func TestMCPTokenLifecycleStoresDigestOnlyAndScopesIndependently(t *testing.T) {
	repository, path := openTestRepository(t)
	ctx := context.Background()
	if _, err := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://office.example"}); err != nil {
		t.Fatal(err)
	}
	issued, err := repository.CreateMCPToken(ctx, MCPTokenInput{Name: "reader", NASAllowlist: []string{"office"}})
	if err != nil {
		t.Fatal(err)
	}
	if issued.BearerToken == "" || len(issued.Token.Scopes) != 1 || issued.Token.Scopes[0] != remotepolicy.ScopeRead {
		t.Fatalf("issued token = %#v", issued)
	}
	principal, err := repository.AuthenticateMCPToken(ctx, issued.BearerToken)
	if err != nil || !principal.HasScope(remotepolicy.ScopeRead) || principal.HasScope(remotepolicy.ScopePlan) || !principal.AllowsNAS("office") {
		t.Fatalf("principal=%#v err=%v", principal, err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(contents, []byte(issued.BearerToken)) {
		t.Fatal("state database contains plaintext bearer token")
	}

	old := issued.BearerToken
	rotated, err := repository.RotateMCPToken(ctx, issued.Token.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AuthenticateMCPToken(ctx, old); !errors.Is(err, ErrTokenUnauthorized) {
		t.Fatalf("old token authentication = %v", err)
	}
	if _, err := repository.AuthenticateMCPToken(ctx, rotated.BearerToken); err != nil {
		t.Fatalf("rotated token authentication = %v", err)
	}
	if _, err := repository.RevokeMCPToken(ctx, issued.Token.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AuthenticateMCPToken(ctx, rotated.BearerToken); !errors.Is(err, ErrTokenUnauthorized) {
		t.Fatalf("revoked token authentication = %v", err)
	}

	independent, err := repository.CreateMCPToken(ctx, MCPTokenInput{Name: "planner", Scopes: []string{remotepolicy.ScopePlan}, NASAllowlist: []string{"office"}})
	if err != nil {
		t.Fatal(err)
	}
	principal, err = repository.AuthenticateMCPToken(ctx, independent.BearerToken)
	if err != nil || !principal.HasScope(remotepolicy.ScopePlan) || principal.HasScope(remotepolicy.ScopeRead) || principal.HasScope(remotepolicy.ScopeApply) {
		t.Fatalf("independent scopes principal=%#v err=%v", principal, err)
	}
	if _, err := repository.ExpireMCPToken(ctx, independent.Token.ID, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AuthenticateMCPToken(ctx, independent.BearerToken); !errors.Is(err, ErrTokenUnauthorized) {
		t.Fatalf("expired token authentication = %v", err)
	}
}

func TestHighRiskApprovalIsExactAndSingleUseUnderConcurrency(t *testing.T) {
	repository, _ := openTestRepository(t)
	ctx := remotepolicy.WithCorrelationID(context.Background(), "correlation-test")
	profile, err := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://office.example"})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := repository.CreateMCPToken(ctx, MCPTokenInput{Name: "operator", Scopes: []string{remotepolicy.ScopeApply}, NASAllowlist: []string{"office"}})
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	approval, err := repository.CreateApproval(ctx, ApprovalInput{PlanHash: hash, NAS: "office", ProfileRevision: profile.Revision, RequestingTokenID: issued.Token.ID}, "local-admin")
	if err != nil {
		t.Fatal(err)
	}
	if approval.ExpiresAt.Sub(approval.CreatedAt) != DefaultApprovalTTL {
		t.Fatalf("approval TTL = %s", approval.ExpiresAt.Sub(approval.CreatedAt))
	}

	var admitted atomic.Int32
	var mutated atomic.Int32
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if repository.AdmitRemoteApply(ctx, issued.Token.ID, "office", profile.Revision, hash, "high") == nil {
				admitted.Add(1)
				mutated.Add(1)
			}
		}()
	}
	wait.Wait()
	if admitted.Load() != 1 || mutated.Load() != 1 {
		t.Fatalf("admitted=%d fake mutations=%d", admitted.Load(), mutated.Load())
	}
	if err := repository.AdmitRemoteApply(ctx, issued.Token.ID, "office", profile.Revision, hash, "high"); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("approval replay = %v", err)
	}
	items, err := repository.Approvals(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ConsumedAt == nil {
		t.Fatalf("approvals = %#v", items)
	}

	if err := repository.AdmitRemoteApply(ctx, issued.Token.ID, "office", profile.Revision, strings.Repeat("b", 64), "medium"); err != nil {
		t.Fatalf("medium-risk admission = %v", err)
	}
	failedHash := strings.Repeat("e", 64)
	if _, err := repository.CreateApproval(ctx, ApprovalInput{PlanHash: failedHash, NAS: "office", ProfileRevision: profile.Revision, RequestingTokenID: issued.Token.ID}, "local-admin"); err != nil {
		t.Fatal(err)
	}
	if err := repository.AdmitRemoteApply(ctx, issued.Token.ID, "office", profile.Revision, failedHash, "high"); err != nil {
		t.Fatal(err)
	}
	// A simulated stale-state/postcondition failure happens after admission.
	// Retrying proves the already admitted approval is not restored.
	if err := repository.AdmitRemoteApply(ctx, issued.Token.ID, "office", profile.Revision, failedHash, "high"); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("failed apply restored approval: %v", err)
	}
	events, err := repository.AuditEvents(ctx, AuditQuery{Action: "apply.admit", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].CorrelationID != "correlation-test" {
		t.Fatalf("apply admission audit = %#v", events)
	}
}

func TestApprovalMismatchExpiryAndAuditFailureFailClosed(t *testing.T) {
	directory := t.TempDir()
	failAudit := atomic.Bool{}
	repository, err := OpenWithOptions(filepath.Join(directory, "gateway.db"), bytes.Repeat([]byte{9}, 32), OpenOptions{AuditFailure: func() error {
		if failAudit.Load() {
			return errors.New("audit offline")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	ctx := context.Background()
	profile, _ := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://office.example"})
	issued, _ := repository.CreateMCPToken(ctx, MCPTokenInput{Name: "operator", Scopes: []string{remotepolicy.ScopeApply}, NASAllowlist: []string{"office"}})
	hash := strings.Repeat("c", 64)
	if _, err := repository.CreateApproval(ctx, ApprovalInput{PlanHash: hash, NAS: "office", ProfileRevision: profile.Revision, RequestingTokenID: issued.Token.ID, TTL: time.Minute}, "local-admin"); err != nil {
		t.Fatal(err)
	}
	if err := repository.AdmitRemoteApply(ctx, issued.Token.ID, "office", profile.Revision, strings.Repeat("d", 64), "high"); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("hash mismatch = %v", err)
	}
	failAudit.Store(true)
	if err := repository.AdmitRemoteApply(ctx, issued.Token.ID, "office", profile.Revision, hash, "high"); err == nil || !strings.Contains(err.Error(), "audit") {
		t.Fatalf("audit failure admission = %v", err)
	}
	failAudit.Store(false)
	if err := repository.AdmitRemoteApply(ctx, issued.Token.ID, "office", profile.Revision, hash, "high"); err != nil {
		t.Fatalf("approval was consumed despite audit rollback: %v", err)
	}
}

func TestAuditRetentionAndClosedSchemaDoNotPersistCanary(t *testing.T) {
	repository, path := openTestRepository(t)
	old := remotepolicy.AuditEvent{Time: time.Now().Add(-31 * 24 * time.Hour), ActorType: "test", Action: "old", Outcome: "success"}
	if err := repository.AppendAudit(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	canary := "password-otp-sid-token-ciphertext-canary"
	if err := repository.AppendAudit(context.Background(), remotepolicy.AuditEvent{ActorType: "test", Action: "new", Outcome: "failure", Reason: canary}); err != nil {
		t.Fatal(err)
	}
	events, err := repository.AuditEvents(context.Background(), AuditQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "new" || events[0].Reason != "failure" {
		t.Fatalf("retained audit = %#v", events)
	}
	if err := repository.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketAudit).Stats().KeyN != 1 {
			t.Fatalf("audit bucket count = %d", tx.Bucket(bucketAudit).Stats().KeyN)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(contents, []byte(canary)) {
		t.Fatal("audit database contains an untrusted secret canary")
	}
}

func TestSchemaOneStateMigratesToAuthorizationBucketsWithBackup(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "gateway.db")
	key := bytes.Repeat([]byte{3}, 32)
	repository, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateProfile(context.Background(), ProfileInput{Name: "office", URL: "https://office.example"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(bucketMeta).Put(keySchemaVersion, encodeUint64(1)); err != nil {
			return err
		}
		for _, name := range [][]byte{bucketMCPTokens, bucketTokenDigests, bucketApprovals, bucketAudit} {
			if err := tx.DeleteBucket(name); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	repository, err = Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	health, err := repository.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if health.SchemaVersion != SchemaVersion || health.ProfileCount != 1 {
		t.Fatalf("migrated health = %#v", health)
	}
	backups, _ := filepath.Glob(path + ".pre-v1-*.bak")
	if len(backups) != 1 {
		t.Fatalf("schema-one backups = %v", backups)
	}
}

func TestSchemaThreeLegacyAdministratorWithManagedStateFailsClosed(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "gateway.db")
	key := bytes.Repeat([]byte{5}, 32)
	repository, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://office.example"}); err != nil {
		t.Fatal(err)
	}
	issued, err := repository.CreateMCPToken(ctx, MCPTokenInput{Name: "reader", NASAllowlist: []string{"office"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.AppendAudit(ctx, AuditEvent{ActorType: "test", ActorID: "schema-two", Action: "upgrade.prepare", Outcome: "success"}); err != nil {
		t.Fatal(err)
	}
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
		if err := meta.Put(keyAdminMode, []byte("local")); err != nil {
			return err
		}
		if err := meta.Put(keyAdminDigest, bytes.Repeat([]byte{7}, 32)); err != nil {
			return err
		}
		for _, key := range [][]byte{keyAdminUsername, keyAdminPassword, keyAdminInitializedAt} {
			if err := meta.Delete(key); err != nil {
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
	if _, err = Open(path, key); err == nil || !strings.Contains(err.Error(), "automatic local-administrator reset is refused") {
		t.Fatalf("legacy managed state migration error = %v", err)
	}
	_ = issued
	backups, _ := filepath.Glob(path + ".pre-v3-*.bak")
	if len(backups) != 1 {
		t.Fatalf("schema-three backups = %v", backups)
	}
}
