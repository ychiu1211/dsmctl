package taskscheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/taskscheduler"
)

// The decoders are strict about the response ENVELOPE (a malformed or
// wrong-shaped response is an error, never a silently-empty success) and lenient
// about per-field presence, since the field set is sourced from DSM's token
// vocabulary and not yet confirmed against a populated task.
//
// Secret hygiene: the decoders WHITELIST the inventory-metadata fields they read.
// A task's command / script body (DSM "script"/"operation"/"extra"), and any
// embedded credential, are never among the whitelisted fields, so they can never
// be smuggled into a model even if a future DSM response carries them. The
// no-leak unit test asserts this.

// dumpRaw is the live-verification escape hatch documented in
// [[dsm-webapi-live-verify-fields]]: set DSMCTL_DUMP to print the raw DSM payload
// so a populated task's real field names can be captured before the
// wire-unverified flag is cleared. It is a no-op unless the env var is set.
func dumpRaw(what string, data json.RawMessage) {
	if os.Getenv("DSMCTL_DUMP") == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "[DSMCTL_DUMP] %s: %s\n", what, string(data))
}

// decodeScheduledTasks decodes SYNO.Core.TaskScheduler list:
// {"tasks": [ {...}, ... ], "total": N}. An empty task list is valid.
func decodeScheduledTasks(data json.RawMessage) (taskscheduler.ScheduledTasks, error) {
	dumpRaw("scheduled tasks", data)
	root, err := decodeObject(data, "scheduled tasks")
	if err != nil {
		return taskscheduler.ScheduledTasks{}, err
	}
	rawTasks, ok := root["tasks"]
	if !ok {
		return taskscheduler.ScheduledTasks{}, fmt.Errorf("decode scheduled tasks: required field \"tasks\" is missing among %s", availableKeys(root))
	}
	result := taskscheduler.ScheduledTasks{Total: intValue(root, "total"), Tasks: []taskscheduler.ScheduledTask{}}
	if rawTasks == nil {
		return result, nil
	}
	items, ok := rawTasks.([]any)
	if !ok {
		return taskscheduler.ScheduledTasks{}, fmt.Errorf("decode scheduled tasks: \"tasks\" is not an array")
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		task, err := decodeScheduledTask(object)
		if err != nil {
			return taskscheduler.ScheduledTasks{}, err
		}
		result.Tasks = append(result.Tasks, task)
	}
	if result.Total == 0 {
		result.Total = len(result.Tasks)
	}
	return result, nil
}

func decodeScheduledTask(object map[string]any) (taskscheduler.ScheduledTask, error) {
	name := stringValue(object, "name")
	id := int64Value(object, "id", "task_id")
	if name == "" && id == 0 {
		return taskscheduler.ScheduledTask{}, fmt.Errorf("decode scheduled task: entry has neither \"id\" nor \"name\" among %s", availableKeys(object))
	}
	rawType := stringValue(object, "type", "task_type")
	runAs := stringValue(object, "real_owner", "run_as")
	enabled, _ := boolValue(object, "enable", "enabled")
	return taskscheduler.ScheduledTask{
		ID:              id,
		Name:            name,
		RawType:         rawType,
		TypeGroup:       taskscheduler.NormalizeTypeGroup(rawType),
		Owner:           stringValue(object, "owner"),
		RunAsOwner:      runAs,
		RunAsPrivileged: taskscheduler.IsPrivilegedOwner(runAs),
		Enabled:         enabled,
		Schedule:        decodeSchedule(object),
		NextRunTime:     stringValue(object, "next_trigger_time", "next_trigger", "next_run_time"),
		LastRunTime:     stringValue(object, "last_worked_time", "last_run_time", "last_trigger_time"),
		LastRunStatus:   stringValue(object, "status", "last_result", "result", "run_result"),
		Action:          stringValue(object, "action", "desc", "description"),
	}, nil
}

// decodeTriggeredTasks decodes SYNO.Core.EventScheduler list, whose data is a
// BARE JSON array (no {tasks,total} envelope). An empty array is valid.
func decodeTriggeredTasks(data json.RawMessage) (taskscheduler.TriggeredTasks, error) {
	dumpRaw("triggered tasks", data)
	trimmed := bytes.TrimSpace(data)
	result := taskscheduler.TriggeredTasks{Tasks: []taskscheduler.TriggeredTask{}}
	// DSM returns an empty data payload ("") for the get of a missing task; the
	// list, however, must be an array. Treat a truly empty payload as no tasks
	// but reject a non-array, non-empty payload.
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return result, nil
	}
	if trimmed[0] != '[' {
		return taskscheduler.TriggeredTasks{}, fmt.Errorf("decode triggered tasks: expected a JSON array, got %.32s", string(trimmed))
	}
	var items []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&items); err != nil {
		return taskscheduler.TriggeredTasks{}, fmt.Errorf("decode triggered tasks: %w", err)
	}
	for _, object := range items {
		if object == nil {
			continue
		}
		name := stringValue(object, "task_name", "name")
		if name == "" {
			return taskscheduler.TriggeredTasks{}, fmt.Errorf("decode triggered task: entry has no \"task_name\" among %s", availableKeys(object))
		}
		runAs := stringValue(object, "real_owner", "run_as")
		enabled, _ := boolValue(object, "enable", "enabled")
		result.Tasks = append(result.Tasks, taskscheduler.TriggeredTask{
			Name:            name,
			Owner:           stringValue(object, "owner"),
			RunAsOwner:      runAs,
			RunAsPrivileged: taskscheduler.IsPrivilegedOwner(runAs),
			Enabled:         enabled,
			Event:           stringValue(object, "event", "trigger_event", "trigger"),
			Action:          stringValue(object, "action", "desc", "description"),
		})
	}
	return result, nil
}

// decodeSchedule extracts the recognized DSM schedule fields from a task's
// "schedule" object (or, tolerantly, from the task object itself). Only
// recognized fields are populated; unknown fields are ignored.
func decodeSchedule(object map[string]any) taskscheduler.Schedule {
	section := object
	if raw, ok := object["schedule"]; ok {
		if nested, ok := raw.(map[string]any); ok {
			section = nested
		}
	}
	schedule := taskscheduler.Schedule{
		Date:     stringValue(section, "date"),
		Weekdays: stringValue(section, "week", "weekday", "week_day"),
	}
	if hour, ok := intPtr(section, "hour"); ok {
		schedule.Hour = hour
	}
	if minute, ok := intPtr(section, "minute", "min"); ok {
		schedule.Minute = minute
	}
	if repeatHour, ok := intPtr(section, "repeat_hour", "repeat_hour_store_config"); ok {
		schedule.RepeatHour = repeatHour
	}
	if repeatMin, ok := intPtr(section, "repeat_min", "repeat_min_store_config"); ok {
		schedule.RepeatMin = repeatMin
	}
	schedule.Summary = summarizeSchedule(schedule)
	return schedule
}

// summarizeSchedule renders a short recurrence summary from the recognized
// fields. It stays conservative: it only describes what was decoded and never
// invents a recurrence that was not present.
func summarizeSchedule(schedule taskscheduler.Schedule) string {
	parts := make([]string, 0, 4)
	if schedule.Hour != nil && schedule.Minute != nil {
		parts = append(parts, fmt.Sprintf("at %02d:%02d", *schedule.Hour, *schedule.Minute))
	} else if schedule.Hour != nil {
		parts = append(parts, fmt.Sprintf("at hour %d", *schedule.Hour))
	}
	if schedule.RepeatHour != nil && *schedule.RepeatHour > 0 {
		parts = append(parts, fmt.Sprintf("every %dh", *schedule.RepeatHour))
	}
	if schedule.RepeatMin != nil && *schedule.RepeatMin > 0 {
		parts = append(parts, fmt.Sprintf("every %dm", *schedule.RepeatMin))
	}
	if schedule.Date != "" {
		parts = append(parts, "on "+schedule.Date)
	}
	if schedule.Weekdays != "" {
		parts = append(parts, "weekdays "+schedule.Weekdays)
	}
	return strings.Join(parts, " ")
}

// --- shared lenient decoding helpers (mirrors the firewall package) ---

func decodeObject(data json.RawMessage, what string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode %s: %w", what, err)
	}
	if root == nil {
		return nil, fmt.Errorf("decode %s: response is not an object", what)
	}
	return root, nil
}

func stringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				return trimmed
			}
		case json.Number:
			return typed.String()
		}
	}
	return ""
}

func intValue(values map[string]any, keys ...string) int {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return int(parsed)
			}
		case float64:
			return int(typed)
		}
	}
	return 0
}

func int64Value(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return parsed
			}
		case float64:
			return int64(typed)
		}
	}
	return 0
}

// intPtr returns a heap int when a recognized numeric field is present, so an
// absent field stays null in the model rather than defaulting to 0.
func intPtr(values map[string]any, keys ...string) (*int, bool) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				out := int(parsed)
				return &out, true
			}
		case float64:
			out := int(typed)
			return &out, true
		}
	}
	return nil, false
}

func boolValue(values map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed, true
		case json.Number:
			// DSM occasionally serves a boolean toggle as 0/1.
			if parsed, err := typed.Int64(); err == nil {
				return parsed != 0, true
			}
		}
	}
	return false, false
}

func availableKeys(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return "[" + strings.Join(keys, ", ") + "]"
}
