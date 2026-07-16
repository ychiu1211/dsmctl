package identityinventory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
)

func decodeUsers(data json.RawMessage) ([]identity.User, error) {
	raw, err := decodeObject(data, "user inventory")
	if err != nil {
		return nil, err
	}
	items := objectList(raw, "users")
	users := make([]identity.User, 0, len(items))
	for _, item := range items {
		users = append(users, identity.User{
			ID:                   scalarString(item, "uid", "id"),
			Name:                 scalarString(item, "name"),
			Description:          scalarString(item, "description", "desc"),
			Email:                scalarString(item, "email"),
			Source:               "local",
			Expired:              boolValue(item, "expired", "is_expired"),
			PasswordNeverExpires: boolValue(item, "passwd_never_expire", "password_never_expires"),
			TwoFactorStatus:      twoFactorStatus(item),
		})
	}
	return users, nil
}

func twoFactorStatus(values map[string]any) string {
	value, ok := values["2fa_status"]
	if !ok {
		value = values["two_factor_status"]
	}
	switch typed := value.(type) {
	case bool:
		if typed {
			return "enabled"
		}
		return "disabled"
	case json.Number:
		if typed.String() == "1" {
			return "enabled"
		}
		if typed.String() == "0" {
			return "disabled"
		}
		return typed.String()
	case float64:
		if typed == 1 {
			return "enabled"
		}
		if typed == 0 {
			return "disabled"
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case string:
		if typed == "true" || typed == "1" {
			return "enabled"
		}
		if typed == "false" || typed == "0" {
			return "disabled"
		}
		return typed
	default:
		return ""
	}
}

func decodeGroups(data json.RawMessage) ([]identity.Group, error) {
	raw, err := decodeObject(data, "group inventory")
	if err != nil {
		return nil, err
	}
	items := objectList(raw, "groups")
	groups := make([]identity.Group, 0, len(items))
	for _, item := range items {
		groups = append(groups, identity.Group{
			ID:          scalarString(item, "gid", "id"),
			Name:        scalarString(item, "name"),
			Description: scalarString(item, "description", "desc"),
			Source:      "local",
		})
	}
	return groups, nil
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
		case bool:
			return strconv.FormatBool(typed)
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
			return parsed || typed == "1" || typed == "enabled"
		}
	}
	return false
}
