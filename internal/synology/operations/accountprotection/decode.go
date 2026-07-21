package accountprotection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/accountprotection"
)

// The decoders are strict about the response envelope (a malformed shape is an
// error, never a silently-empty success) and lenient about per-field presence,
// since DSM field sets vary across releases. No OTP secret, seed, or recovery
// material is ever decoded into a model — only the public policy fields.

func decodeAutoBlockSettings(data json.RawMessage) (accountprotection.AutoBlockSettings, error) {
	root, err := decodeObject(data, "auto block settings")
	if err != nil {
		return accountprotection.AutoBlockSettings{}, err
	}
	// The response must carry at least the enable/attempts shape; a response with
	// none of the expected keys is treated as an unrecognized shape.
	if !hasAny(root, "enable", "enabled", "attempts") {
		return accountprotection.AutoBlockSettings{}, fmt.Errorf("decode auto block settings: no recognized fields among %s", availableKeys(root))
	}
	enabled, _ := boolValue(root, "enable", "enabled")
	settings := accountprotection.AutoBlockSettings{
		Enabled:       enabled,
		Attempts:      intValue(root, "attempts"),
		WithinMinutes: intValue(root, "within_mins", "within_minutes", "within"),
		ExpireDays:    intValue(root, "expire_day", "expire_days"),
	}
	// DSM has no separate expire-enable flag in the read; expiration is on when a
	// positive expire_day is configured. Honor an explicit flag if a future
	// release adds one.
	if explicit, ok := boolValue(root, "expire_enable", "expire_enabled"); ok {
		settings.ExpireEnabled = explicit
	} else {
		settings.ExpireEnabled = settings.ExpireDays > 0
	}
	return settings, nil
}

func decodeIPList(data json.RawMessage, kind string) (accountprotection.IPList, error) {
	root, err := decodeObject(data, "auto block "+kind+" list")
	if err != nil {
		return accountprotection.IPList{}, err
	}
	items, ok := objectList(root, "ip_info", "ip_list", "rules")
	if !ok {
		return accountprotection.IPList{}, fmt.Errorf("decode auto block %s list: no ip_info array among %s", kind, availableKeys(root))
	}
	list := accountprotection.IPList{Kind: kind, Entries: make([]accountprotection.IPRule, 0, len(items))}
	for _, item := range items {
		ip := stringValue(item, "ip", "ip_addr", "address")
		if ip == "" {
			// An entry with no address is unusable; skip it rather than surface a
			// blank rule.
			continue
		}
		list.Entries = append(list.Entries, accountprotection.IPRule{
			IP:         ip,
			Reason:     stringValue(item, "reason", "type"),
			RecordTime: int64(intValue(item, "record_time", "recordtime", "time")),
		})
	}
	list.Total = intValue(root, "total")
	if list.Total == 0 {
		list.Total = len(list.Entries)
	}
	return list, nil
}

func decodeAccountProtection(data json.RawMessage) (accountprotection.AccountProtection, error) {
	root, err := decodeObject(data, "account protection")
	if err != nil {
		return accountprotection.AccountProtection{}, err
	}
	if !hasAny(root, "enabled", "enable", "untrust_try", "trust_try") {
		return accountprotection.AccountProtection{}, fmt.Errorf("decode account protection: no recognized fields among %s", availableKeys(root))
	}
	enabled, _ := boolValue(root, "enabled", "enable")
	return accountprotection.AccountProtection{
		Enabled:                enabled,
		UntrustedAttempts:      intValue(root, "untrust_try"),
		UntrustedWithinMinutes: intValue(root, "untrust_minute"),
		UntrustedBlockMinutes:  intValue(root, "untrust_lock"),
		TrustedAttempts:        intValue(root, "trust_try"),
		TrustedWithinMinutes:   intValue(root, "trust_minute"),
		TrustedBlockMinutes:    intValue(root, "trust_lock"),
	}, nil
}

func decodeEnforceTwoFactor(data json.RawMessage) (accountprotection.EnforceTwoFactor, error) {
	root, err := decodeObject(data, "enforce 2fa policy")
	if err != nil {
		return accountprotection.EnforceTwoFactor{}, err
	}
	option := stringValue(root, "otp_enforce_option", "enforce_option", "option")
	if option == "" {
		return accountprotection.EnforceTwoFactor{}, fmt.Errorf("decode enforce 2fa policy: no otp_enforce_option among %s", availableKeys(root))
	}
	return accountprotection.EnforceTwoFactor{
		Option:  option,
		Enabled: !strings.EqualFold(option, "none"),
	}, nil
}

// decodeActiveConnections normalizes the SYNO.Core.CurrentConnection list into
// the source IPs of active clients. It feeds the self-lockout guardrail only, so
// it carries no session identity (no SID, SynoToken, or device id) and a missing
// items array is treated as "no active connections" rather than an error, since
// this read is best-effort.
func decodeActiveConnections(data json.RawMessage) ([]accountprotection.ActiveConnection, error) {
	root, err := decodeObject(data, "active connections")
	if err != nil {
		return nil, err
	}
	items, ok := objectList(root, "items", "connections")
	if !ok {
		return nil, nil
	}
	connections := make([]accountprotection.ActiveConnection, 0, len(items))
	for _, item := range items {
		from := stringValue(item, "from", "ip", "ip_addr")
		if from == "" {
			continue
		}
		current, _ := boolValue(item, "is_current_connected", "is_current", "current")
		connections = append(connections, accountprotection.ActiveConnection{
			From:    from,
			Who:     stringValue(item, "who", "user"),
			Current: current,
		})
	}
	return connections, nil
}

// --- shared lenient decoding helpers (mirrors the certificate operation pkg) ---

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

func hasAny(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
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
