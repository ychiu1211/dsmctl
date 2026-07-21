package snapshotreplication

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/snapshotreplication"
)

// DSM field names, live-verified on DSM 7.3-81168 unless noted.
const (
	keySnapshotTime        = "time"
	keySnapshotDescription = "desc"
	keySnapshotLock        = "lock"
	keySnapshotSchedule    = "schedule_snapshot"
	keySnapshotWormLock    = "worm_lock"

	keyConfSnapshotBrowsing = "enable_snapshot_browsing"
	keyConfLocalTimeFormat  = "snapshot_local_time_format"
)

func decodeShareSnapshots(share string, data json.RawMessage) (snapshotreplication.ShareSnapshots, error) {
	raw, err := decodeObject(data, "share snapshots")
	if err != nil {
		return snapshotreplication.ShareSnapshots{}, err
	}
	items, ok := raw["snapshots"]
	if !ok {
		return snapshotreplication.ShareSnapshots{}, fmt.Errorf("decode share snapshots: required field %q is missing", "snapshots")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(items, &entries); err != nil {
		return snapshotreplication.ShareSnapshots{}, fmt.Errorf("decode share snapshots list: %w", err)
	}
	snapshots := make([]snapshotreplication.Snapshot, 0, len(entries))
	for index, entry := range entries {
		time := optionalString(entry, keySnapshotTime)
		if time == "" {
			return snapshotreplication.ShareSnapshots{}, fmt.Errorf("decode share snapshots: snapshot %d has no time name", index)
		}
		snapshots = append(snapshots, snapshotreplication.Snapshot{
			Time:            time,
			Description:     optionalString(entry, keySnapshotDescription),
			Locked:          optionalBool(entry, keySnapshotLock),
			ScheduleCreated: optionalBool(entry, keySnapshotSchedule),
			WormLocked:      optionalBool(entry, keySnapshotWormLock),
		})
	}
	result := snapshotreplication.ShareSnapshots{Share: share, Snapshots: snapshots}
	result.Total = intOr(raw, "total", len(snapshots))
	return result, nil
}

func decodeShareConfig(share string, data json.RawMessage) (snapshotreplication.ShareConfig, error) {
	raw, err := decodeObject(data, "share snapshot configuration")
	if err != nil {
		return snapshotreplication.ShareConfig{}, err
	}
	browsing, err := requiredBool(raw, keyConfSnapshotBrowsing, "share snapshot configuration")
	if err != nil {
		return snapshotreplication.ShareConfig{}, err
	}
	return snapshotreplication.ShareConfig{
		Share:            share,
		SnapshotBrowsing: browsing,
		LocalTimeFormat:  optionalBool(raw, keyConfLocalTimeFormat),
	}, nil
}

func decodeRetentionPolicy(share string, data json.RawMessage) (snapshotreplication.RetentionPolicy, error) {
	raw, err := decodeObject(data, "retention policy")
	if err != nil {
		return snapshotreplication.RetentionPolicy{}, err
	}
	// tid is the stable field: -1 means no retention task; require it to catch
	// API drift, decode the policy numbers leniently.
	taskID, ok := parseInt(raw["tid"])
	if !ok {
		return snapshotreplication.RetentionPolicy{}, fmt.Errorf("decode retention policy: required field %q is missing or not a number", "tid")
	}
	scheduled := false
	if schedule, present := raw["schedule"]; present {
		trimmed := bytes.TrimSpace(schedule)
		scheduled = len(trimmed) != 0 && !bytes.Equal(trimmed, []byte("null"))
	}
	return snapshotreplication.RetentionPolicy{
		Share:      share,
		TaskID:     taskID,
		PolicyType: intOr(raw, "policyType", 0),
		KeepRecent: intOr(raw, "recently", 0),
		RetainDays: intOr(raw, "retainDay", 0),
		Hourly:     intOr(raw, "hourly", 0),
		Daily:      intOr(raw, "daily", 0),
		Weekly:     intOr(raw, "weekly", 0),
		Monthly:    intOr(raw, "monthly", 0),
		Yearly:     intOr(raw, "yearly", 0),
		Scheduled:  scheduled,
	}, nil
}

func decodeLogPage(data json.RawMessage) (snapshotreplication.LogPage, error) {
	raw, err := decodeObject(data, "snapshot replication log")
	if err != nil {
		return snapshotreplication.LogPage{}, err
	}
	items, ok := raw["log_list"]
	if !ok {
		return snapshotreplication.LogPage{}, fmt.Errorf("decode snapshot replication log: required field %q is missing", "log_list")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(items, &entries); err != nil {
		return snapshotreplication.LogPage{}, fmt.Errorf("decode snapshot replication log list: %w", err)
	}
	page := snapshotreplication.LogPage{
		Total:      intOr(raw, "total", len(entries)),
		ErrorCount: intOr(raw, "error_count", 0),
		WarnCount:  intOr(raw, "warn_count", 0),
		InfoCount:  intOr(raw, "info_count", 0),
		Entries:    make([]snapshotreplication.LogEntry, 0, len(entries)),
	}
	// Entry shape live-verified on DSM 7.3-81168: time is a formatted string
	// and the text lives under "event".
	for _, entry := range entries {
		page.Entries = append(page.Entries, snapshotreplication.LogEntry{
			Time:    optionalString(entry, "time"),
			Level:   optionalString(entry, "level"),
			User:    optionalString(entry, "user"),
			Message: firstString(entry, "event", "log", "msg"),
		})
	}
	return page, nil
}

func decodeNodeIdentity(data json.RawMessage) (snapshotreplication.NodeIdentity, error) {
	raw, err := decodeObject(data, "replication node identity")
	if err != nil {
		return snapshotreplication.NodeIdentity{}, err
	}
	identity := snapshotreplication.NodeIdentity{
		Hostname: optionalString(raw, "hostname"),
		NodeID:   optionalString(raw, "node_id"),
		Serial:   optionalString(raw, "serial"),
	}
	if identity.Hostname == "" && identity.NodeID == "" {
		return snapshotreplication.NodeIdentity{}, fmt.Errorf("decode replication node identity: neither hostname nor node_id is present")
	}
	return identity, nil
}

func decodeReplicationPlans(data json.RawMessage) (snapshotreplication.ReplicationPlans, error) {
	raw, err := decodeObject(data, "replication plans")
	if err != nil {
		return snapshotreplication.ReplicationPlans{}, err
	}
	items := firstRaw(raw, "plans", "plan_list")
	if items == nil {
		return snapshotreplication.ReplicationPlans{}, fmt.Errorf("decode replication plans: no plan list field is present")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(items, &entries); err != nil {
		return snapshotreplication.ReplicationPlans{}, fmt.Errorf("decode replication plan list: %w", err)
	}
	plans := make([]snapshotreplication.ReplicationPlan, 0, len(entries))
	for _, entry := range entries {
		plans = append(plans, decodeReplicationPlan(entry))
	}
	result := snapshotreplication.ReplicationPlans{Plans: plans}
	result.Total = intOr(raw, "total", len(plans))
	return result, nil
}

// decodeReplicationPlan decodes one plan. The base identity fields are on the
// plan object; the enrichment blocks (site info, can_do, sync_report) are
// nested under an "additional" sub-object (live-verified against a real
// nas51→nas255 relation on DSM 7.4.7). Every block is optional — a missing one
// never fails the read.
func decodeReplicationPlan(entry map[string]json.RawMessage) snapshotreplication.ReplicationPlan {
	plan := snapshotreplication.ReplicationPlan{
		ID:         firstString(entry, "plan_id", "id"),
		RemoteID:   firstString(entry, "remote_plan_id"),
		Role:       decodeRole(entry),
		Status:     decodeStatus(entry),
		TargetName: firstString(entry, "target_name", "target_id"),
		TargetType: decodeTargetType(entry),
	}
	// The "additional" blocks are nested; fall back to the top level for a DSM
	// build that flattens them.
	additional := entry
	if block, ok := entry["additional"]; ok {
		var nested map[string]json.RawMessage
		if json.Unmarshal(block, &nested) == nil && len(nested) > 0 {
			additional = nested
		}
	}
	if plan.Status == "" {
		plan.Status = decodeStatus(additional)
	}
	plan.SnapshotCount = int(firstInt64(additional, "snapshot_count"))
	plan.MainSite = decodeSiteInfo(additional, "main_site_info")
	plan.DRSite = decodeSiteInfo(additional, "dr_site_info")
	// target_name may only be resolvable from the site blocks.
	if plan.TargetName == "" {
		if plan.MainSite.TargetName != "" {
			plan.TargetName = plan.MainSite.TargetName
		} else if plan.DRSite.TargetName != "" {
			plan.TargetName = plan.DRSite.TargetName
		}
	}
	plan.LastSyncTime, plan.LastSyncBytes = decodeSyncReport(additional)
	plan.Can = decodeCanDo(additional)
	return plan
}

func decodeRole(entry map[string]json.RawMessage) string {
	switch firstInt64(entry, "role") {
	case 1:
		return "main"
	case 2:
		return "dr"
	}
	return firstString(entry, "role")
}

func decodeTargetType(entry map[string]json.RawMessage) string {
	switch firstInt64(entry, "target_type") {
	case 2:
		return "share"
	case 1:
		return "lun"
	}
	return firstString(entry, "target_type", "type")
}

// decodeStatus tolerates both an integer status and a string state.
func decodeStatus(entry map[string]json.RawMessage) string {
	if s := firstString(entry, "status", "state"); s != "" {
		return s
	}
	if value, ok := entry["status"]; ok {
		if n, ok := parseInt64(value); ok {
			return strconv.FormatInt(n, 10)
		}
	}
	return ""
}

func decodeSiteInfo(entry map[string]json.RawMessage, key string) snapshotreplication.ReplicationSiteInfo {
	block, ok := entry[key]
	if !ok {
		return snapshotreplication.ReplicationSiteInfo{}
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(block, &raw) != nil {
		return snapshotreplication.ReplicationSiteInfo{}
	}
	return snapshotreplication.ReplicationSiteInfo{
		Hostname:   firstString(raw, "hostname", "location_hostname"),
		NodeID:     firstString(raw, "node_id"),
		TargetName: firstString(raw, "target_name"),
		Status:     firstString(raw, "status"),
	}
}

func decodeSyncReport(entry map[string]json.RawMessage) (string, int64) {
	block, ok := entry["sync_report"]
	if !ok {
		return "", 0
	}
	var report struct {
		Recent []map[string]json.RawMessage `json:"recent_records"`
	}
	if json.Unmarshal(block, &report) != nil || len(report.Recent) == 0 {
		return "", 0
	}
	first := report.Recent[0]
	return strings.TrimSpace(firstString(first, "readable_begin_time", "finish_time")), firstInt64(first, "sync_size_byte")
}

func decodeCanDo(entry map[string]json.RawMessage) snapshotreplication.ReplicationCapabilities {
	block, ok := entry["can_do"]
	if !ok {
		return snapshotreplication.ReplicationCapabilities{}
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(block, &raw) != nil {
		return snapshotreplication.ReplicationCapabilities{}
	}
	return snapshotreplication.ReplicationCapabilities{
		CanSync:         optionalBool(raw, "can_sync"),
		CanEdit:         optionalBool(raw, "can_edit"),
		CanDelete:       optionalBool(raw, "can_delete"),
		CanSwitchover:   optionalBool(raw, "can_switchover"),
		CanFailover:     optionalBool(raw, "can_failover"),
		CanReprotect:    optionalBool(raw, "can_reprotect"),
		CanTestFailover: optionalBool(raw, "can_testfailover"),
	}
}

func decodeCredID(data json.RawMessage) (string, error) {
	raw, err := decodeObject(data, "pairing credential")
	if err != nil {
		return "", err
	}
	credID := firstString(raw, "cred_id")
	if credID == "" {
		return "", fmt.Errorf("decode pairing credential: no cred_id returned")
	}
	return credID, nil
}

func decodeTaskID(data json.RawMessage) (string, error) {
	raw, err := decodeObject(data, "replication create")
	if err != nil {
		return "", err
	}
	taskID := firstString(raw, "task_id")
	if taskID == "" {
		return "", fmt.Errorf("decode replication create: no task_id returned")
	}
	return taskID, nil
}

func decodePollTask(data json.RawMessage) (snapshotreplication.RelationTaskStatus, error) {
	raw, err := decodeObject(data, "replication task poll")
	if err != nil {
		return snapshotreplication.RelationTaskStatus{}, err
	}
	status := snapshotreplication.RelationTaskStatus{
		Finished:     optionalBool(raw, "finish"),
		Success:      optionalBool(raw, "success"),
		PlanID:       firstString(raw, "plan_id"),
		RemotePlanID: firstString(raw, "remote_plan_id"),
		Error:        firstString(raw, "error", "error_msg"),
	}
	if info, ok := raw["info"]; ok {
		var infoRaw struct {
			Param struct {
				Target struct {
					TargetID string `json:"target_id"`
				} `json:"target"`
			} `json:"param"`
		}
		if json.Unmarshal(info, &infoRaw) == nil {
			status.TargetID = infoRaw.Param.Target.TargetID
		}
	}
	return status, nil
}

func firstInt64(raw map[string]json.RawMessage, names ...string) int64 {
	for _, name := range names {
		if value, ok := raw[name]; ok {
			if n, ok := parseInt64(value); ok {
				return n
			}
		}
	}
	return 0
}

// decodeCreatedSnapshot reads the create response: DSM returns the new
// snapshot's time name as a bare JSON string (live-verified); an object with a
// snapshot/time field is tolerated.
func decodeCreatedSnapshot(data json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(data)
	var name string
	if err := json.Unmarshal(trimmed, &name); err == nil && name != "" {
		return name, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err == nil {
		if name := firstString(raw, "snapshot", keySnapshotTime); name != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("decode snapshot create response: no snapshot time name returned")
}

func decodeObject(data json.RawMessage, what string) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("decode %s: expected a non-empty object", what)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("decode %s object: %w", what, err)
	}
	return raw, nil
}

func firstRaw(raw map[string]json.RawMessage, names ...string) json.RawMessage {
	for _, name := range names {
		if value, ok := raw[name]; ok {
			return value
		}
	}
	return nil
}

func firstString(raw map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		if value := optionalString(raw, name); value != "" {
			return value
		}
	}
	return ""
}

func optionalString(raw map[string]json.RawMessage, name string) string {
	if value, ok := raw[name]; ok {
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			return text
		}
	}
	return ""
}

func requiredBool(raw map[string]json.RawMessage, name, what string) (bool, error) {
	value, ok := raw[name]
	if !ok {
		return false, fmt.Errorf("decode %s: required field %q is missing", what, name)
	}
	result, ok := parseBool(value)
	if !ok {
		return false, fmt.Errorf("decode %s field %q: expected boolean", what, name)
	}
	return result, nil
}

func optionalBool(raw map[string]json.RawMessage, name string) bool {
	if value, ok := raw[name]; ok {
		if result, parsed := parseBool(value); parsed {
			return result
		}
	}
	return false
}

func parseBool(value json.RawMessage) (bool, bool) {
	var result bool
	if err := json.Unmarshal(value, &result); err == nil {
		return result, true
	}
	var integer int
	if err := json.Unmarshal(value, &integer); err == nil && (integer == 0 || integer == 1) {
		return integer == 1, true
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		if parsed, convErr := strconv.Atoi(strings.TrimSpace(text)); convErr == nil && (parsed == 0 || parsed == 1) {
			return parsed == 1, true
		}
	}
	return false, false
}

func intOr(raw map[string]json.RawMessage, name string, fallback int) int {
	if value, ok := parseInt(raw[name]); ok {
		return value
	}
	return fallback
}

func parseInt(value json.RawMessage) (int, bool) {
	parsed, ok := parseInt64(value)
	return int(parsed), ok
}

func parseInt64(value json.RawMessage) (int64, bool) {
	if value == nil {
		return 0, false
	}
	var number int64
	if err := json.Unmarshal(value, &number); err == nil {
		return number, true
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		if parsed, convErr := strconv.ParseInt(strings.TrimSpace(text), 10, 64); convErr == nil {
			return parsed, true
		}
	}
	return 0, false
}
