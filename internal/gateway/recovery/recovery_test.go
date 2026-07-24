package recovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gatewaystate "github.com/derekvery666/dsmctl/internal/gateway/state"
)

func TestQueueAndApplyPendingRestoresValidatedState(t *testing.T) {
	fixture := newFixture(t)
	backupName := fixture.createBackup(t, "7.3.2-28", time.Date(2026, 7, 24, 8, 30, 0, 0, time.UTC))
	fixture.appendAudit(t, "newer-state")

	status, err := fixture.manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Backups) != 1 {
		t.Fatalf("backups = %#v", status.Backups)
	}
	backup := status.Backups[0]
	if backup.Name != backupName || backup.Version != "7.3.2-28" || !backup.Complete || !backup.Restorable || backup.SizeBytes <= 64 {
		t.Fatalf("backup = %#v", backup)
	}
	if err := fixture.manager.Queue(context.Background(), backupName, "RESTORE "+backupName); err != nil {
		t.Fatal(err)
	}
	status, err = fixture.manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.PendingBackup != backupName {
		t.Fatalf("pending backup = %q", status.PendingBackup)
	}

	result, err := fixture.manager.ApplyPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Status != "success" || result.Backup != backupName {
		t.Fatalf("result = %#v", result)
	}
	actions := fixture.auditActions(t)
	if strings.Join(actions, ",") != "backup-state" {
		t.Fatalf("restored audit actions = %v", actions)
	}
	safety, err := filepath.Glob(filepath.Join(fixture.root, "pre-restore-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(safety) != 1 {
		t.Fatalf("pre-restore safety copies = %v", safety)
	}
	for _, name := range requiredFiles {
		info, err := os.Lstat(filepath.Join(safety[0], name))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("safety file %s: info=%v err=%v", name, info, err)
		}
	}
	status, err = fixture.manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Backups) != 2 || status.Backups[0].Name != filepath.Base(safety[0]) ||
		status.Backups[0].Version != "pre-restore" || !status.Backups[0].Restorable {
		t.Fatalf("post-restore backups = %#v", status.Backups)
	}
}

func TestRestoreSafetyCopyIsVisibleAndKeepsTotalRetentionBounded(t *testing.T) {
	fixture := newFixture(t)
	var target string
	for index := 0; index < defaultRecoveryRetention; index++ {
		created := time.Date(2026, 7, 23, 22+index/2, 30*(index%2), 0, 0, time.UTC)
		target = fixture.createBackup(t, "7.3.2-"+string(rune('a'+index)), created)
	}
	fixture.appendAudit(t, "newer-state")
	if err := fixture.manager.Queue(context.Background(), target, "RESTORE "+target); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.ApplyPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := fixture.manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Backups) != defaultRecoveryRetention {
		t.Fatalf("retained backups = %d, want %d: %#v", len(status.Backups), defaultRecoveryRetention, status.Backups)
	}
	if status.Backups[0].Version != "pre-restore" || !status.Backups[0].Restorable {
		t.Fatalf("newest safety backup = %#v", status.Backups[0])
	}
	for _, backup := range status.Backups {
		if backup.Version == "7.3.2-a" {
			t.Fatalf("oldest backup was not pruned: %#v", status.Backups)
		}
	}
}

func TestTamperedBackupFailsWithoutChangingLiveState(t *testing.T) {
	fixture := newFixture(t)
	backupName := fixture.createBackup(t, "7.3.2-28", time.Date(2026, 7, 24, 8, 30, 0, 0, time.UTC))
	fixture.appendAudit(t, "current-state")
	if err := fixture.manager.Queue(context.Background(), backupName, "RESTORE "+backupName); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(fixture.root, backupName, "gateway.db")
	file, err := os.OpenFile(dbPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("tampered")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := fixture.manager.ApplyPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Status != "failure" || !strings.Contains(result.Message, "changed after confirmation") {
		t.Fatalf("result = %#v", result)
	}
	actions := fixture.auditActions(t)
	if strings.Join(actions, ",") != "current-state,backup-state" {
		t.Fatalf("live audit actions = %v", actions)
	}
	status, err := fixture.manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.PendingBackup != "" || status.LastResult == nil || status.LastResult.Status != "failure" {
		t.Fatalf("status = %#v", status)
	}
}

func TestKeyMismatchAndConfirmationFailClosed(t *testing.T) {
	fixture := newFixture(t)
	backupName := fixture.createBackup(t, "7.3.2-28", time.Date(2026, 7, 24, 8, 30, 0, 0, time.UTC))
	if err := os.WriteFile(filepath.Join(fixture.root, backupName, "master.key"), []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := fixture.manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Backups) != 1 || !status.Backups[0].Complete || status.Backups[0].Restorable || !strings.Contains(status.Backups[0].Reason, "master key") {
		t.Fatalf("backup = %#v", status.Backups)
	}
	if err := fixture.manager.Queue(context.Background(), backupName, backupName); !errors.Is(err, ErrConfirmation) {
		t.Fatalf("confirmation error = %v", err)
	}
	if err := fixture.manager.Queue(context.Background(), backupName, "RESTORE "+backupName); !errors.Is(err, ErrNotRestorable) {
		t.Fatalf("not-restorable error = %v", err)
	}
}

func TestMalformedRecoveryEntryIsReportedButNotRestorable(t *testing.T) {
	fixture := newFixture(t)
	name := "pre-upgrade-invalid"
	if err := os.MkdirAll(filepath.Join(fixture.root, name), 0o700); err != nil {
		t.Fatal(err)
	}
	status, err := fixture.manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Backups) != 1 || status.Backups[0].Name != name || status.Backups[0].Complete || status.Backups[0].Restorable {
		t.Fatalf("backups = %#v", status.Backups)
	}
}

type recoveryFixture struct {
	root        string
	statePath   string
	masterPath  string
	platformKey string
	master      []byte
	manager     *Manager
}

func newFixture(t *testing.T) recoveryFixture {
	t.Helper()
	base := t.TempDir()
	fixture := recoveryFixture{
		root:        filepath.Join(base, "backups"),
		statePath:   filepath.Join(base, "data", "gateway.db"),
		masterPath:  filepath.Join(base, "secrets", "master.key"),
		platformKey: filepath.Join(base, "secrets", "dsm-sso.key"),
		master:      []byte("0123456789abcdef0123456789abcdef"),
	}
	if err := os.MkdirAll(filepath.Dir(fixture.masterPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.masterPath, fixture.master, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.platformKey, []byte("fedcba9876543210fedcba9876543210"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository, err := gatewaystate.Open(fixture.statePath, fixture.master)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.AppendAudit(context.Background(), gatewaystate.AuditEvent{Action: "backup-state", Outcome: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	fixture.manager, err = New(Options{
		Root:            fixture.root,
		StatePath:       fixture.statePath,
		MasterKeyPath:   fixture.masterPath,
		PlatformKeyPath: fixture.platformKey,
		Now:             func() time.Time { return time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f recoveryFixture) createBackup(t *testing.T, version string, created time.Time) string {
	t.Helper()
	name := "pre-upgrade-" + version + "-" + created.Format("20060102150405")
	directory := filepath.Join(f.root, name)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	copyTestFile(t, f.statePath, filepath.Join(directory, "gateway.db"))
	copyTestFile(t, f.masterPath, filepath.Join(directory, "master.key"))
	copyTestFile(t, f.platformKey, filepath.Join(directory, "dsm-sso.key"))
	if err := os.Chtimes(directory, created, created); err != nil {
		t.Fatal(err)
	}
	return name
}

func (f recoveryFixture) appendAudit(t *testing.T, action string) {
	t.Helper()
	repository, err := gatewaystate.Open(f.statePath, f.master)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.AppendAudit(context.Background(), gatewaystate.AuditEvent{Action: action, Outcome: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
}

func (f recoveryFixture) auditActions(t *testing.T) []string {
	t.Helper()
	repository, err := gatewaystate.Open(f.statePath, f.master)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	events, err := repository.AuditEvents(context.Background(), gatewaystate.AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	actions := make([]string, 0, len(events))
	for _, event := range events {
		actions = append(actions, event.Action)
	}
	return actions
}

func copyTestFile(t *testing.T, source, destination string) {
	t.Helper()
	value, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, value, 0o600); err != nil {
		t.Fatal(err)
	}
}
