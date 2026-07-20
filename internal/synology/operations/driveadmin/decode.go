package driveadmin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/driveadmin"
)

// Decoders are strict about the response envelope and the list container so a
// changed Drive response shape surfaces as an explicit error instead of a
// silently empty state, and lenient about per-item fields because their
// presence varies across package versions.

// decodeServiceStatus reads get_status. Verified live on Drive 4.0.3: the
// service state is enable_status ("enabled"); the response also carries
// QuickConnect relay fields and freeze flags that stay unmodeled.
func decodeServiceStatus(data json.RawMessage) (driveadmin.ServiceStatus, error) {
	root, err := decodeObject(data, "Drive service status")
	if err != nil {
		return driveadmin.ServiceStatus{}, err
	}
	status := stringValue(root, "enable_status", "status", "service_status", "state")
	if status == "" {
		return driveadmin.ServiceStatus{}, fmt.Errorf("decode Drive service status: no status field among %s", availableKeys(root))
	}
	return driveadmin.ServiceStatus{Status: strings.ToLower(status)}, nil
}

func decodeConnections(data json.RawMessage) (driveadmin.Connections, error) {
	root, err := decodeObject(data, "Drive connection list")
	if err != nil {
		return driveadmin.Connections{}, err
	}
	items, ok := objectList(root, "items", "connections", "data")
	if !ok {
		return driveadmin.Connections{}, fmt.Errorf("decode Drive connection list: no connection array among %s", availableKeys(root))
	}
	result := driveadmin.Connections{Connections: make([]driveadmin.Connection, 0, len(items))}
	for _, item := range items {
		result.Connections = append(result.Connections, driveadmin.Connection{
			User:       stringValue(item, "username", "user", "owner"),
			DeviceName: stringValue(item, "device_name", "computer_name", "hostname", "device"),
			ClientType: strings.ToLower(stringValue(item, "client_type", "type", "platform")),
			Address:    stringValue(item, "address", "ip", "ip_address"),
		})
	}
	result.Total = intValue(root, "total")
	if result.Total == 0 {
		result.Total = len(result.Connections)
	}
	return result, nil
}

// decodeTeamFolders reads Share.list. Verified live on Drive 4.0.3: items carry
// share_name, the share_enable team-folder activation flag, share_status
// ("normal"), share_type, and — only while enabled — the versioning triple
// rotate_cnt/rotate_policy/rotate_days. Fields that do not apply to an item
// are reported as the literal string "-" and surface as absent; watermark and
// download-restriction settings stay unmodeled.
func decodeTeamFolders(data json.RawMessage) (driveadmin.TeamFolders, error) {
	root, err := decodeObject(data, "Drive team folder list")
	if err != nil {
		return driveadmin.TeamFolders{}, err
	}
	items, ok := objectList(root, "items", "shares", "team_folders", "data")
	if !ok {
		return driveadmin.TeamFolders{}, fmt.Errorf("decode Drive team folder list: no team folder array among %s", availableKeys(root))
	}
	result := driveadmin.TeamFolders{TeamFolders: make([]driveadmin.TeamFolder, 0, len(items))}
	for index, item := range items {
		name := stringValue(item, "share_name", "name", "title")
		if name == "" {
			return driveadmin.TeamFolders{}, fmt.Errorf("decode Drive team folder %d: no name field among %s", index, availableKeys(item))
		}
		enabled, _ := boolValue(item, "share_enable", "enabled")
		folder := driveadmin.TeamFolder{
			Name:    name,
			Enabled: enabled,
			Status:  strings.ToLower(stringValue(item, "share_status", "status", "state")),
			Type:    strings.ToLower(stringValue(item, "share_type")),
		}
		if count, ok := optionalIntValue(item, "rotate_cnt"); ok {
			folder.MaxVersions = &count
			// Drive reports "-" for the policy while versioning is off.
			if policy := strings.ToLower(stringValue(item, "rotate_policy")); policy != "" && policy != "-" {
				folder.VersionPolicy = policy
			}
			if days, ok := optionalIntValue(item, "rotate_days"); ok {
				folder.RetentionDays = &days
			}
		}
		result.TeamFolders = append(result.TeamFolders, folder)
	}
	result.Total = intValue(root, "total")
	if result.Total == 0 {
		result.Total = len(result.TeamFolders)
	}
	return result, nil
}

// decodeLog reads Log.list. Verified live on Drive 4.0.3: entries are
// template-coded — a numeric event type plus substitution slots (s1..s5 paths,
// p1..p5 values) — rather than rendered text, so the structured fields are
// surfaced directly.
func decodeLog(data json.RawMessage) (driveadmin.Log, error) {
	root, err := decodeObject(data, "Drive log list")
	if err != nil {
		return driveadmin.Log{}, err
	}
	items, ok := objectList(root, "items", "logs", "data")
	if !ok {
		return driveadmin.Log{}, fmt.Errorf("decode Drive log list: no log array among %s", availableKeys(root))
	}
	result := driveadmin.Log{Entries: make([]driveadmin.LogEntry, 0, len(items))}
	for _, item := range items {
		result.Entries = append(result.Entries, driveadmin.LogEntry{
			TimeUnix:   int64Value(item, "time"),
			Username:   stringValue(item, "username", "user"),
			ClientType: strings.ToLower(stringValue(item, "client_type")),
			IPAddress:  stringValue(item, "ip_address", "ip"),
			EventType:  intValue(item, "type"),
			Path:       stringValue(item, "s1"),
			TeamFolder: stringValue(item, "share_name"),
		})
	}
	result.Total = intValue(root, "total")
	if result.Total == 0 {
		result.Total = len(result.Entries)
	}
	return result, nil
}

// decodeConnectionSummary reads Connection.summary v2. Verified live on Drive
// 4.0.3: {"summary":{"desktop":0,"mobile":0,"sharesync":0,"total":0}}.
func decodeConnectionSummary(data json.RawMessage) (driveadmin.ConnectionSummary, error) {
	root, err := decodeObject(data, "Drive connection summary")
	if err != nil {
		return driveadmin.ConnectionSummary{}, err
	}
	summary, ok := root["summary"].(map[string]any)
	if !ok {
		return driveadmin.ConnectionSummary{}, fmt.Errorf("decode Drive connection summary: no summary object among %s", availableKeys(root))
	}
	return driveadmin.ConnectionSummary{
		Desktop:   intValue(summary, "desktop"),
		Mobile:    intValue(summary, "mobile"),
		ShareSync: intValue(summary, "sharesync"),
		Total:     intValue(summary, "total"),
	}, nil
}

// decodeDBUsage reads DBUsage.get. Verified live on Drive 4.0.3:
// {"database_size":…,"office_size":…,"repo_size":…,"update_time":…}.
func decodeDBUsage(data json.RawMessage) (driveadmin.DBUsage, error) {
	root, err := decodeObject(data, "Drive database usage")
	if err != nil {
		return driveadmin.DBUsage{}, err
	}
	if _, ok := root["repo_size"]; !ok {
		return driveadmin.DBUsage{}, fmt.Errorf("decode Drive database usage: no repo_size field among %s", availableKeys(root))
	}
	return driveadmin.DBUsage{
		RepositorySize: int64Value(root, "repo_size"),
		DatabaseSize:   int64Value(root, "database_size"),
		OfficeSize:     int64Value(root, "office_size"),
		UpdatedUnix:    int64Value(root, "update_time"),
	}, nil
}

// decodeTopAccessFiles reads Dashboard.top_access_files. The envelope
// ({"files":[…]}) was verified live on Drive 4.0.3; row fields come from
// Drive's access-log aggregation and are decoded leniently.
func decodeTopAccessFiles(data json.RawMessage) (driveadmin.TopAccessFiles, error) {
	root, err := decodeObject(data, "Drive top access files")
	if err != nil {
		return driveadmin.TopAccessFiles{}, err
	}
	items, ok := objectList(root, "files", "items", "data")
	if !ok {
		return driveadmin.TopAccessFiles{}, fmt.Errorf("decode Drive top access files: no file array among %s", availableKeys(root))
	}
	result := driveadmin.TopAccessFiles{Files: make([]driveadmin.TopAccessFile, 0, len(items))}
	for _, item := range items {
		result.Files = append(result.Files, driveadmin.TopAccessFile{
			Path:        stringValue(item, "path", "file_path", "display_path"),
			Name:        stringValue(item, "name", "file_name"),
			AccessCount: intValue(item, "access_count", "count", "total"),
		})
	}
	return result, nil
}

// decodeActivation reads Activation.get. Verified live on Drive 4.0.3:
// {"activated":false,"activation_time":0,"serial_number":"…"}.
func decodeActivation(data json.RawMessage) (driveadmin.Activation, error) {
	root, err := decodeObject(data, "Drive activation")
	if err != nil {
		return driveadmin.Activation{}, err
	}
	activated, ok := boolValue(root, "activated")
	if !ok {
		return driveadmin.Activation{}, fmt.Errorf("decode Drive activation: required field \"activated\" is missing or not boolean among %s", availableKeys(root))
	}
	return driveadmin.Activation{
		Activated:      activated,
		SerialNumber:   stringValue(root, "serial_number"),
		ActivationUnix: int64Value(root, "activation_time"),
	}, nil
}

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

// objectList reads the first present array field, keeping object items. It
// reports whether any candidate key held an array so callers can distinguish
// an empty list from an unrecognized response shape.
func objectList(root map[string]any, keys ...string) ([]map[string]any, bool) {
	for _, key := range keys {
		value, ok := root[key]
		if !ok || value == nil {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			continue
		}
		result := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if object, ok := item.(map[string]any); ok {
				result = append(result, object)
			}
		}
		return result, true
	}
	return nil, false
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
	return int(int64Value(values, keys...))
}

// optionalIntValue distinguishes a present integer from Drive's "-" not-
// applicable marker (and from a missing field), which intValue folds to 0.
func optionalIntValue(values map[string]any, key string) (int, bool) {
	value, ok := values[key]
	if !ok || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed), true
		}
	case float64:
		return int(typed), true
	}
	return 0, false
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

// boolValue reads the first present boolean field. Drive reports "-" for
// fields that do not apply to an item (seen live on disabled shares), so
// non-boolean values are skipped rather than treated as false.
func boolValue(values map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		if typed, ok := value.(bool); ok {
			return typed, true
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
