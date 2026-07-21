package taskscheduler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/taskscheduler"
)

// TestDecodeScheduledEmptyLiveShape covers the live-verified empty envelope
// captured on the DSM 7.3 lab: {"tasks":[],"total":0}.
func TestDecodeScheduledEmptyLiveShape(t *testing.T) {
	tasks, err := decodeScheduledTasks(json.RawMessage(`{"tasks":[],"total":0}`))
	if err != nil {
		t.Fatalf("decodeScheduledTasks() error = %v", err)
	}
	if tasks.Total != 0 || len(tasks.Tasks) != 0 {
		t.Fatalf("expected empty inventory, got %#v", tasks)
	}
	if tasks.Tasks == nil {
		t.Fatalf("Tasks must be a non-nil empty slice for stable JSON output")
	}
}

// TestDecodeTriggeredEmptyLiveShape covers the live-verified bare empty array
// captured on the lab: [].
func TestDecodeTriggeredEmptyLiveShape(t *testing.T) {
	tasks, err := decodeTriggeredTasks(json.RawMessage(`[]`))
	if err != nil {
		t.Fatalf("decodeTriggeredTasks() error = %v", err)
	}
	if len(tasks.Tasks) != 0 || tasks.Tasks == nil {
		t.Fatalf("expected empty non-nil inventory, got %#v", tasks)
	}
}

// TestDecodeScheduledPopulated exercises the per-task decoder against a synthetic
// task built from DSM's token vocabulary (documentation-safe values only). It is
// a MODEL of the expected shape, not a captured live response — the lab had no
// tasks configured (see the package doc).
func TestDecodeScheduledPopulated(t *testing.T) {
	payload := json.RawMessage(`{
		"total": 2,
		"tasks": [
			{"id": 3, "name": "nightly backup", "type": "script", "owner": "testuser",
			 "real_owner": "root", "enable": true, "next_trigger_time": "2026-08-01 03:00",
			 "status": "not running", "action": "Run script",
			 "schedule": {"hour": 3, "minute": 0, "week": "1,2,3,4,5"}},
			{"id": 7, "name": "smart test", "type": "smart_test", "owner": "testuser",
			 "real_owner": "testuser", "enable": false}
		]
	}`)
	tasks, err := decodeScheduledTasks(payload)
	if err != nil {
		t.Fatalf("decodeScheduledTasks() error = %v", err)
	}
	if tasks.Total != 2 || len(tasks.Tasks) != 2 {
		t.Fatalf("tasks = %#v", tasks)
	}
	first := tasks.Tasks[0]
	if first.ID != 3 || first.Name != "nightly backup" || first.TypeGroup != taskscheduler.TypeGroupScript {
		t.Fatalf("first task = %#v", first)
	}
	if first.RunAsOwner != "root" || !first.RunAsPrivileged {
		t.Fatalf("root run-as must be flagged privileged: %#v", first)
	}
	if !first.Enabled || first.NextRunTime != "2026-08-01 03:00" || first.LastRunStatus != "not running" {
		t.Fatalf("first task fields = %#v", first)
	}
	if first.Schedule.Hour == nil || *first.Schedule.Hour != 3 || first.Schedule.Weekdays != "1,2,3,4,5" {
		t.Fatalf("schedule = %#v", first.Schedule)
	}
	if first.Schedule.Summary == "" {
		t.Fatalf("expected a schedule summary, got empty")
	}
	second := tasks.Tasks[1]
	if second.TypeGroup != taskscheduler.TypeGroupSystem {
		t.Fatalf("smart_test should normalize to system, got %q", second.TypeGroup)
	}
	if second.RunAsOwner != "testuser" || second.RunAsPrivileged {
		t.Fatalf("non-privileged run-as must not be flagged: %#v", second)
	}
}

func TestDecodeTriggeredPopulated(t *testing.T) {
	payload := json.RawMessage(`[
		{"task_name": "on-boot", "owner": "testuser", "real_owner": "root",
		 "enable": true, "event": "bootup", "action": "Run script"}
	]`)
	tasks, err := decodeTriggeredTasks(payload)
	if err != nil {
		t.Fatalf("decodeTriggeredTasks() error = %v", err)
	}
	if len(tasks.Tasks) != 1 {
		t.Fatalf("tasks = %#v", tasks)
	}
	task := tasks.Tasks[0]
	if task.Name != "on-boot" || task.Event != "bootup" || !task.Enabled || !task.RunAsPrivileged {
		t.Fatalf("triggered task = %#v", task)
	}
}

// TestDecodeRejectsMalformedShapes proves a silently-changed DSM response shape
// fails loudly rather than yielding a zero value.
func TestDecodeRejectsMalformedShapes(t *testing.T) {
	cases := []struct {
		name    string
		decode  func(json.RawMessage) error
		payload string
	}{
		{"scheduled missing tasks", func(d json.RawMessage) error { _, err := decodeScheduledTasks(d); return err }, `{"total":0}`},
		{"scheduled tasks not array", func(d json.RawMessage) error { _, err := decodeScheduledTasks(d); return err }, `{"tasks":{}}`},
		{"scheduled not object", func(d json.RawMessage) error { _, err := decodeScheduledTasks(d); return err }, `[]`},
		{"scheduled entry has no id or name", func(d json.RawMessage) error { _, err := decodeScheduledTasks(d); return err }, `{"tasks":[{"enable":true}]}`},
		{"triggered non-array object", func(d json.RawMessage) error { _, err := decodeTriggeredTasks(d); return err }, `{"tasks":[]}`},
		{"triggered entry has no name", func(d json.RawMessage) error { _, err := decodeTriggeredTasks(d); return err }, `[{"enable":true}]`},
	}
	for _, tc := range cases {
		if err := tc.decode(json.RawMessage(tc.payload)); err == nil {
			t.Fatalf("%s: expected an error for payload %s", tc.name, tc.payload)
		}
	}
}

// TestDecodeNeverLeaksCommandOrCredential asserts the whitelist decoders never
// surface a task's command / script body or any embedded credential, even when a
// (hypothetical) DSM response carries them. This is the secret-hygiene guarantee
// of the read slice.
func TestDecodeNeverLeaksCommandOrCredential(t *testing.T) {
	const sentinelScript = "rm -rf / #SECRET_SCRIPT_BODY"
	const sentinelSecret = "hunter2-SECRET_TOKEN"

	scheduled := json.RawMessage(`{"tasks":[{
		"id": 1, "name": "leaky", "type": "script", "enable": true, "real_owner": "root",
		"script": "` + sentinelScript + `",
		"extra": {"script": "` + sentinelScript + `", "notify_mail": "a@b.c"},
		"command": "` + sentinelScript + `",
		"smtp_password": "` + sentinelSecret + `",
		"password": "` + sentinelSecret + `"
	}],"total":1}`)
	sTasks, err := decodeScheduledTasks(scheduled)
	if err != nil {
		t.Fatalf("decodeScheduledTasks() error = %v", err)
	}
	assertNoSentinel(t, sTasks, sentinelScript, sentinelSecret)

	triggered := json.RawMessage(`[{
		"task_name": "leaky-trigger", "enable": true, "real_owner": "root", "event": "bootup",
		"operation": "` + sentinelScript + `",
		"operation_type": "script",
		"password": "` + sentinelSecret + `"
	}]`)
	tTasks, err := decodeTriggeredTasks(triggered)
	if err != nil {
		t.Fatalf("decodeTriggeredTasks() error = %v", err)
	}
	assertNoSentinel(t, tTasks, sentinelScript, sentinelSecret)
}

func assertNoSentinel(t *testing.T, value any, sentinels ...string) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal decoded value: %v", err)
	}
	for _, sentinel := range sentinels {
		if strings.Contains(string(encoded), sentinel) {
			t.Fatalf("decoded value leaked a command body or credential (%q): %s", sentinel, string(encoded))
		}
	}
}
