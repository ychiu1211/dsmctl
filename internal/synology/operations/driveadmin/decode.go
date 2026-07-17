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

func decodeServiceStatus(data json.RawMessage) (driveadmin.ServiceStatus, error) {
	root, err := decodeObject(data, "Drive service status")
	if err != nil {
		return driveadmin.ServiceStatus{}, err
	}
	status := stringValue(root, "status", "service_status", "state")
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
		name := stringValue(item, "name", "share_name", "title")
		if name == "" {
			return driveadmin.TeamFolders{}, fmt.Errorf("decode Drive team folder %d: no name field among %s", index, availableKeys(item))
		}
		result.TeamFolders = append(result.TeamFolders, driveadmin.TeamFolder{
			ID:     stringValue(item, "id", "share_id", "uuid"),
			Name:   name,
			Status: strings.ToLower(stringValue(item, "status", "state")),
		})
	}
	result.Total = intValue(root, "total")
	if result.Total == 0 {
		result.Total = len(result.TeamFolders)
	}
	return result, nil
}

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
			Time:        stringValue(item, "time", "date", "timestamp"),
			Username:    stringValue(item, "username", "user", "who"),
			Action:      strings.ToLower(stringValue(item, "action", "event", "category")),
			Target:      stringValue(item, "target", "filename", "file_name", "path"),
			Description: stringValue(item, "descr", "description", "message"),
		})
	}
	result.Total = intValue(root, "total")
	if result.Total == 0 {
		result.Total = len(result.Entries)
	}
	return result, nil
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

func availableKeys(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return "[" + strings.Join(keys, ", ") + "]"
}
