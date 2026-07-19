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
	"sync/atomic"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
)

func TestPendingApprovalLifecycleIsDeduplicatedBoundedAndAdvisory(t *testing.T) {
	now := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "gateway.db")
	repository, err := OpenWithOptions(path, bytes.Repeat([]byte{9}, 32), OpenOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	ctx := context.Background()
	profile, err := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://office.example"})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := repository.CreateMCPToken(ctx, MCPTokenInput{Name: "operator", Scopes: []string{remotepolicy.ScopeApply}, NASAllowlist: []string{"office"}})
	if err != nil {
		t.Fatal(err)
	}
	record := func(hash, summary string) {
		t.Helper()
		if err := repository.RecordPendingApproval(ctx, remotepolicy.PendingApprovalRequest{PlanHash: hash, NAS: "office", ProfileRevision: profile.Revision, RequestingTokenID: issued.Token.ID, Tool: "plan_storage_change", Risk: "high", ResourceID: "pool-1", Summary: summary}); err != nil {
			t.Fatal(err)
		}
	}
	hash := fmt.Sprintf("%064x", 1)
	beforeRequest := repository.AdmitRemoteApply(ctx, issued.Token.ID, "office", profile.Revision, hash, "high")
	if !errors.Is(beforeRequest, ErrApprovalRequired) {
		t.Fatalf("admission before pending request = %v", beforeRequest)
	}
	record(hash, "delete pool")
	afterRequest := repository.AdmitRemoteApply(ctx, issued.Token.ID, "office", profile.Revision, hash, "high")
	if !errors.Is(afterRequest, ErrApprovalRequired) || afterRequest.Error() != beforeRequest.Error() {
		t.Fatalf("pending request changed admission: before=%v after=%v", beforeRequest, afterRequest)
	}
	now = now.Add(time.Hour)
	record(hash, "delete pool after refresh")
	items, err := repository.PendingApprovals(ctx)
	if err != nil || len(items) != 1 || items[0].Summary != "delete pool after refresh" || !items[0].CreatedAt.Equal(now) || items[0].RequestingToken != "operator" {
		t.Fatalf("deduplicated requests = %#v, %v", items, err)
	}

	for index := 2; index <= MaxPendingApprovals+2; index++ {
		now = now.Add(time.Second)
		record(fmt.Sprintf("%064x", index), fmt.Sprintf("plan %d", index))
	}
	items, err = repository.PendingApprovals(ctx)
	if err != nil || len(items) != MaxPendingApprovals {
		t.Fatalf("bounded requests = %d, %v", len(items), err)
	}
	for _, item := range items {
		if item.PlanHash == hash {
			t.Fatal("oldest pending approval was not evicted")
		}
	}

	request := items[0]
	approval, err := repository.ApprovePendingApproval(ctx, request.ID, "local:owner")
	if err != nil || approval.PlanHash != request.PlanHash || approval.ProfileRevision != profile.Revision {
		t.Fatalf("one-click approval = %#v, %v", approval, err)
	}
	items, _ = repository.PendingApprovals(ctx)
	for _, item := range items {
		if item.ID == request.ID {
			t.Fatal("approved pending request remains")
		}
	}

	now = now.Add(PendingApprovalTTL + time.Second)
	items, err = repository.PendingApprovals(ctx)
	if err != nil || len(items) != 0 {
		t.Fatalf("expired requests = %#v, %v", items, err)
	}

	now = now.Add(time.Second)
	revokeHash := fmt.Sprintf("%064x", 999)
	record(revokeHash, "revoke cleanup")
	if _, err := repository.RevokeMCPToken(ctx, issued.Token.ID); err != nil {
		t.Fatal(err)
	}
	items, err = repository.PendingApprovals(ctx)
	if err != nil || len(items) != 0 {
		t.Fatalf("revoked-token requests = %#v, %v", items, err)
	}
}

func TestSchemaFourMigratesNASAdminToLANDiscover(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "gateway.db")
	key := bytes.Repeat([]byte{8}, 32)
	repository, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateProfile(context.Background(), ProfileInput{Name: "office", URL: "https://office.example"}); err != nil {
		t.Fatal(err)
	}
	issued, err := repository.CreateMCPToken(context.Background(), MCPTokenInput{Name: "discoverer", Scopes: []string{remotepolicy.ScopeLANDiscover}, NASAllowlist: []string{"office"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Update(func(tx *bolt.Tx) error {
		record, err := readMCPToken(tx, issued.Token.ID)
		if err != nil {
			return err
		}
		record.Scopes = []string{"nas.admin"}
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := tx.Bucket(bucketMCPTokens).Put([]byte(record.ID), encoded); err != nil {
			return err
		}
		return tx.Bucket(bucketMeta).Put(keySchemaVersion, encodeUint64(4))
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	repository, err = Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	token, err := repository.MCPToken(context.Background(), issued.Token.ID)
	if err != nil || len(token.Scopes) != 1 || token.Scopes[0] != remotepolicy.ScopeLANDiscover {
		t.Fatalf("migrated token = %#v, %v", token, err)
	}
	principal, err := repository.AuthenticateMCPToken(context.Background(), issued.BearerToken)
	if err != nil || !principal.HasScope(remotepolicy.ScopeLANDiscover) {
		t.Fatalf("migrated principal = %#v, %v", principal, err)
	}
	if _, err := repository.CreateMCPToken(context.Background(), MCPTokenInput{Name: "legacy", Scopes: []string{"nas.admin"}, NASAllowlist: []string{"office"}}); err == nil {
		t.Fatal("nas.admin token creation was accepted after migration")
	}
	backups, _ := filepath.Glob(path + ".pre-v4-*.bak")
	if len(backups) != 1 {
		t.Fatalf("schema-four backups = %v", backups)
	}
}

func TestManualAndOneClickApprovalRaceCreatesOneStandardApproval(t *testing.T) {
	repository, _ := openTestRepository(t)
	ctx := context.Background()
	profile, _ := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://office.example"})
	issued, _ := repository.CreateMCPToken(ctx, MCPTokenInput{Name: "operator", Scopes: []string{remotepolicy.ScopeApply}, NASAllowlist: []string{"office"}})
	hash := strings.Repeat("c", 64)
	if err := repository.RecordPendingApproval(ctx, remotepolicy.PendingApprovalRequest{PlanHash: hash, NAS: "office", ProfileRevision: profile.Revision, RequestingTokenID: issued.Token.ID, Tool: "plan_storage_change", Risk: "high", Summary: "delete pool"}); err != nil {
		t.Fatal(err)
	}
	requests, _ := repository.PendingApprovals(ctx)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, _ = repository.ApprovePendingApproval(ctx, requests[0].ID, "local:owner")
	}()
	go func() {
		defer wait.Done()
		_, _ = repository.CreateApproval(ctx, ApprovalInput{PlanHash: hash, NAS: "office", RequestingTokenID: issued.Token.ID}, "local:owner")
	}()
	wait.Wait()
	approvals, err := repository.Approvals(ctx, false)
	if err != nil || len(approvals) != 1 {
		t.Fatalf("raced approvals = %#v, %v", approvals, err)
	}
}

func TestDeletingProfileCleansTokenAllowlistsBeforeNameReuse(t *testing.T) {
	repository, _ := openTestRepository(t)
	ctx := context.Background()
	profile, _ := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://office.example"})
	issued, err := repository.CreateMCPToken(ctx, MCPTokenInput{Name: "reader", NASAllowlist: []string{"office"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.DeleteProfile(ctx, "office", profile.Revision, false); err != nil {
		t.Fatal(err)
	}
	token, err := repository.MCPToken(ctx, issued.Token.ID)
	if err != nil || len(token.NASAllowlist) != 0 {
		t.Fatalf("cleaned token = %#v, %v", token, err)
	}
	if _, err := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://replacement.example"}); err != nil {
		t.Fatal(err)
	}
	principal, err := repository.AuthenticateMCPToken(ctx, issued.BearerToken)
	if err != nil || principal.AllowsNAS("office") {
		t.Fatalf("name-reuse principal = %#v, %v", principal, err)
	}
	events, _ := repository.AuditEvents(ctx, AuditQuery{Limit: 20, Action: "token.allowlist.cleanup"})
	if len(events) != 1 || events[0].NAS != "office" {
		t.Fatalf("allowlist cleanup audit = %#v", events)
	}
}

func TestAuditExportReturnsMoreThanInteractiveLimitInChronologicalOrder(t *testing.T) {
	repository, _ := openTestRepository(t)
	base := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	if err := repository.db.Update(func(tx *bolt.Tx) error {
		for index := 0; index < 1105; index++ {
			if err := repository.appendAuditTx(tx, AuditEvent{Time: base.Add(time.Duration(index) * time.Second), ActorType: "test", ActorID: "export", Action: "export.seed", Outcome: "success"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	events, err := repository.AuditExport(context.Background())
	if err != nil || len(events) != 1105 {
		t.Fatalf("export events = %d, %v", len(events), err)
	}
	for index := 1; index < len(events); index++ {
		if events[index].Time.Before(events[index-1].Time) {
			t.Fatalf("export out of order at %d", index)
		}
	}
}

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
		for _, name := range [][]byte{bucketMCPTokens, bucketTokenDigests, bucketApprovals, bucketApprovalRequests, bucketAudit} {
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
