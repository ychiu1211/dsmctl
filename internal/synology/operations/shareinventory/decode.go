package shareinventory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ychiu1211/dsmctl/internal/domain/share"
)

func decodeShares(data json.RawMessage) ([]share.SharedFolder, error) {
	raw, err := decodeObject(data, "shared-folder inventory")
	if err != nil {
		return nil, err
	}
	items := objectList(raw, "shares")
	shares := make([]share.SharedFolder, 0, len(items))
	for _, item := range items {
		shares = append(shares, share.SharedFolder{
			Name:                scalarString(item, "name"),
			UUID:                scalarString(item, "uuid"),
			Description:         scalarString(item, "desc", "description"),
			VolumePath:          scalarString(item, "vol_path", "volume_path"),
			Hidden:              boolValue(item, "hidden"),
			Encrypted:           boolValue(item, "encryption", "encrypted"),
			EncryptionAutoMount: boolValue(item, "enc_auto_mount", "encryption_auto_mount"),
			ACLMode:             boolValue(item, "is_aclmode", "acl_mode"),
			UnifiedPermissions:  boolValue(item, "unite_permission", "is_unite_permission"),
			USB:                 boolValue(item, "is_usb_share", "usb"),
			SnapshotSupported:   boolValue(item, "support_snapshot", "snapshot_supported"),
			QuotaBytes:          uint64Value(item, "quota_value"),
			QuotaUsedBytes:      uint64Value(item, "share_quota_used", "share_quota_logical_size"),
			Permissions:         make([]share.Permission, 0),
		})
	}
	return shares, nil
}

func decodePermissions(data json.RawMessage, input PermissionInput) ([]permissionResult, error) {
	raw, err := decodeObject(data, "shared-folder permissions")
	if err != nil {
		return nil, err
	}
	items := objectList(raw, "shares")
	results := make([]permissionResult, 0, len(items))
	for _, item := range items {
		results = append(results, permissionResult{
			ShareName: scalarString(item, "name"),
			Binding: share.Permission{
				PrincipalType: input.PrincipalType,
				Principal:     input.Principal,
				Access:        permissionAccess(item),
				Inherited:     boolValue(item, "inherit", "inherited"),
				Custom:        boolValue(item, "is_custom", "custom"),
				Masked:        boolValue(item, "is_mask", "masked"),
				ACLMode:       boolValue(item, "is_aclmode", "acl_mode"),
			},
		})
	}
	return results, nil
}

func permissionAccess(values map[string]any) string {
	if boolValue(values, "is_deny") {
		return share.AccessDeny
	}
	if boolValue(values, "is_custom") {
		return share.AccessCustom
	}
	if boolValue(values, "is_writable") {
		return share.AccessWrite
	}
	if boolValue(values, "is_readonly") {
		return share.AccessRead
	}
	return share.AccessNone
}

func decodeObject(data json.RawMessage, label string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode %s: %w", label, err)
	}
	return raw, nil
}

func objectList(values map[string]any, key string) []map[string]any {
	items, _ := values[key].([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			result = append(result, object)
		}
	}
	return result
}

func scalarString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case string:
			return typed
		case json.Number:
			return typed.String()
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64)
		}
	}
	return ""
}

func boolValue(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case bool:
			return typed
		case json.Number:
			return typed.String() == "1"
		case float64:
			return typed != 0
		case string:
			parsed, _ := strconv.ParseBool(typed)
			return parsed || typed == "1" || typed == "yes"
		}
	}
	return false
}

func uint64Value(values map[string]any, keys ...string) uint64 {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case json.Number:
			value, _ := strconv.ParseUint(typed.String(), 10, 64)
			return value
		case float64:
			if typed >= 0 {
				return uint64(typed)
			}
		case string:
			value, _ := strconv.ParseUint(typed, 10, 64)
			return value
		}
	}
	return 0
}
