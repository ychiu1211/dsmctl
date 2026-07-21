// Package taskscheduler contains stable, DSM-version-independent models for the
// Control Panel > Task Scheduler surface: the inventory of scheduled tasks
// (user-defined scripts, service-control tasks, and built-in maintenance tasks)
// and of triggered tasks (boot-up / shutdown / event tasks). WebAPI names and
// DSM field names stay behind the operation package.
//
// Scheduled and triggered tasks are two separate DSM API families and two
// separate compatibility/failure boundaries, so a NAS missing one still reports
// the other rather than erroring the whole module.
//
// Live-verified on DSM 7.3 (lab), WI-073 Slice A:
//   - Scheduled: SYNO.Core.TaskScheduler (versions 1-3). list ->
//     {"tasks": [...], "total": N}. get (v1 and v3) is keyed by the integer
//     "id" (a non-existent id returns code 4801). There is no get_output /
//     get_result method (both return code 103), so a task's last-run status is
//     read from the list/get fields, not a separate output call.
//   - Triggered: SYNO.Core.EventScheduler (version 1 only). list -> a bare JSON
//     array (no {tasks,total} envelope). get (v1) is keyed by the string
//     "task_name" (an integer id returns code 117; a missing name returns an
//     empty data payload).
//
// The lab had NO tasks defined, so the list/get ENVELOPES above are live-verified
// but the per-item FIELD names are sourced from DSM's stable TaskScheduler token
// vocabulary and decoded tolerantly. Capabilities.TaskFieldsWireUnverified stays
// true until a populated task confirms them (the same posture the firewall module
// took for its rule fields before its Slice B round-trip). This module NEVER
// surfaces a task's command / script body or any embedded credential: reads are
// restricted to inventory metadata this pass; task detail (the script body) and
// all writes are the deferred, HIGH-risk follow-on.
package taskscheduler

import "strings"

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "task-scheduler"

// Normalized task-type groups. The raw DSM token is preserved in RawType; these
// coarse groups classify it for display and to mark which types are read-only in
// this module versus owned by another module's write path.
const (
	TypeGroupScript    = "script"    // user-defined script task (runs an arbitrary command)
	TypeGroupService   = "service"   // service / package start-stop-restart control task
	TypeGroupRetention = "retention" // recycle-bin or other retention task
	TypeGroupSystem    = "system"    // built-in maintenance (DSM update, storage scrub, S.M.A.R.T., backup)
	TypeGroupTriggered = "triggered" // boot-up / shutdown / event-triggered task
	TypeGroupOther     = "other"     // an unrecognized raw type
)

// Schedule is the recurrence of a scheduled task, reduced to the recognized DSM
// schedule fields. Every field is optional: DSM's schedule object varies by task
// type and DSM release, and the shape is wire-unverified against a populated task
// (see the package doc), so the decoder fills only what it recognizes.
type Schedule struct {
	Summary    string `json:"summary,omitempty" jsonschema:"A short human-readable recurrence summary when one can be derived from the recognized fields"`
	Hour       *int   `json:"hour,omitempty" jsonschema:"Hour of day the task runs (0-23), when DSM reports it"`
	Minute     *int   `json:"minute,omitempty" jsonschema:"Minute of the hour the task runs (0-59), when DSM reports it"`
	Date       string `json:"date,omitempty" jsonschema:"The run date DSM reports for a date-based schedule"`
	Weekdays   string `json:"weekdays,omitempty" jsonschema:"The DSM weekday bitmask/token for a weekly schedule"`
	RepeatHour *int   `json:"repeat_hour,omitempty" jsonschema:"Repeat interval in hours, when the task repeats within a day"`
	RepeatMin  *int   `json:"repeat_min,omitempty" jsonschema:"Repeat interval in minutes, when the task repeats within a day"`
}

// ScheduledTask is one entry of the SYNO.Core.TaskScheduler list: the inventory
// metadata of a scheduled task. The command / script body is intentionally absent
// from this list view (it is only in the detail path, which is deferred), and no
// credential is ever decoded.
type ScheduledTask struct {
	ID              int64    `json:"id" jsonschema:"DSM task id (stable identifier used by the deferred write path)"`
	Name            string   `json:"name" jsonschema:"Task name as shown in Control Panel > Task Scheduler"`
	RawType         string   `json:"raw_type,omitempty" jsonschema:"The raw DSM task-type token"`
	TypeGroup       string   `json:"type_group" jsonschema:"Normalized task type: script, service, retention, system, or other"`
	Owner           string   `json:"owner,omitempty" jsonschema:"The task's configured owner account"`
	RunAsOwner      string   `json:"run_as_owner,omitempty" jsonschema:"The identity the task's command runs as (DSM real_owner); empty means the DSM default, typically root"`
	RunAsPrivileged bool     `json:"run_as_privileged" jsonschema:"True when the run-as identity is root or an admin account (or unset, which defaults to root): the command runs with elevated privilege"`
	Enabled         bool     `json:"enabled" jsonschema:"Whether the task is enabled (an enabled script task will run its command on schedule)"`
	Schedule        Schedule `json:"schedule" jsonschema:"The task's recurrence, as far as DSM reports it"`
	NextRunTime     string   `json:"next_run_time,omitempty" jsonschema:"When DSM expects the task to run next, when reported"`
	LastRunTime     string   `json:"last_run_time,omitempty" jsonschema:"When the task last ran, when reported"`
	LastRunStatus   string   `json:"last_run_status,omitempty" jsonschema:"The task's current run status or last-run result, when reported"`
	Action          string   `json:"action,omitempty" jsonschema:"A localized description of what the task does, when DSM provides one (never the raw command)"`
}

// TriggeredTask is one entry of the SYNO.Core.EventScheduler list: a boot-up,
// shutdown, or event-triggered task. Like the scheduled inventory it carries no
// command body or credential.
type TriggeredTask struct {
	Name            string `json:"name" jsonschema:"Triggered task name (the EventScheduler task_name key)"`
	Owner           string `json:"owner,omitempty" jsonschema:"The task's configured owner account"`
	RunAsOwner      string `json:"run_as_owner,omitempty" jsonschema:"The identity the task's command runs as (DSM real_owner); empty means the DSM default, typically root"`
	RunAsPrivileged bool   `json:"run_as_privileged" jsonschema:"True when the run-as identity is root or an admin account (or unset, which defaults to root)"`
	Enabled         bool   `json:"enabled" jsonschema:"Whether the triggered task is enabled"`
	Event           string `json:"event,omitempty" jsonschema:"The trigger that fires the task, such as bootup or shutdown, when DSM reports it"`
	Action          string `json:"action,omitempty" jsonschema:"A localized description of what the task does, when DSM provides one (never the raw command)"`
}

// ScheduledTasks is the scheduled-task inventory.
type ScheduledTasks struct {
	Total int             `json:"total" jsonschema:"Total scheduled tasks DSM reports"`
	Tasks []ScheduledTask `json:"tasks" jsonschema:"The scheduled tasks, empty when none are configured"`
}

// TriggeredTasks is the triggered-task inventory.
type TriggeredTasks struct {
	Tasks []TriggeredTask `json:"tasks" jsonschema:"The triggered tasks, empty when none are configured"`
}

// Capabilities reports which Task Scheduler reads dsmctl currently exposes for the
// selected NAS. Scheduled and triggered tasks are independent DSM API families, so
// one being absent still reports the other.
type Capabilities struct {
	Module                   string `json:"module" jsonschema:"Stable module name: task-scheduler"`
	ScheduledRead            bool   `json:"scheduled_read" jsonschema:"Whether the scheduled-task inventory (SYNO.Core.TaskScheduler) can be read"`
	TriggeredRead            bool   `json:"triggered_read" jsonschema:"Whether the triggered-task inventory (SYNO.Core.EventScheduler) can be read"`
	DetailAvailable          bool   `json:"detail_available" jsonschema:"Whether the scheduled-task detail backend (TaskScheduler get) is advertised by the NAS; the detail read (including the script body) is a deferred follow-on and is not surfaced this pass"`
	TaskFieldsWireUnverified bool   `json:"task_fields_wire_unverified" jsonschema:"True while the per-task field decoding is sourced from DSM's token vocabulary rather than confirmed against a populated task; the list/get envelopes are live-verified"`
}

// privilegedOwners are the run-as identities treated as privileged. An empty
// real_owner is also treated as privileged because DSM defaults an unset run-as to
// root.
var privilegedOwners = map[string]struct{}{
	"root":          {},
	"admin":         {},
	"administrator": {},
	"system":        {},
}

// IsPrivilegedOwner reports whether a run-as identity runs the task's command with
// elevated privilege. An empty identity is privileged (defaults to root).
func IsPrivilegedOwner(runAs string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(runAs))
	if trimmed == "" {
		return true
	}
	_, ok := privilegedOwners[trimmed]
	return ok
}

// NormalizeTypeGroup maps a raw DSM task-type token to a coarse group. The DSM
// list serves the type as either a short string token or a small integer; both
// are handled. An unrecognized token maps to TypeGroupOther and the raw value is
// preserved on the task.
func NormalizeTypeGroup(rawType string) string {
	token := strings.ToLower(strings.TrimSpace(rawType))
	switch token {
	case "":
		return TypeGroupOther
	case "script", "user", "user_defined", "userdefined":
		return TypeGroupScript
	case "service", "app", "package", "pkg", "service_control":
		return TypeGroupService
	case "retention", "recycle", "recyclebin", "recycle_bin":
		return TypeGroupRetention
	}
	// Built-in maintenance task types DSM ships (DSM update, storage scrub,
	// S.M.A.R.T. test, backup). These are read-only here; their writes belong to
	// their owning modules.
	if strings.Contains(token, "update") ||
		strings.Contains(token, "scrub") ||
		strings.Contains(token, "smart") ||
		strings.Contains(token, "backup") ||
		strings.Contains(token, "defrag") ||
		strings.Contains(token, "dedup") ||
		strings.Contains(token, "certificate") ||
		strings.Contains(token, "system") {
		return TypeGroupSystem
	}
	if strings.Contains(token, "script") || strings.Contains(token, "user") {
		return TypeGroupScript
	}
	if strings.Contains(token, "service") {
		return TypeGroupService
	}
	return TypeGroupOther
}
