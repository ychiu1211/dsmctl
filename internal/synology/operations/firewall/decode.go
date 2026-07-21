package firewall

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/firewall"
)

// The decoders are strict about the response envelope (a malformed shape is an
// error, never a silently-empty success) and lenient about per-field presence,
// since DSM field sets vary across releases. Firewall configuration carries no
// secrets, but the decoders still whitelist fields so an unexpected response
// never smuggles surprise data into a model.

func decodeStatus(data json.RawMessage) (firewall.Status, error) {
	root, err := decodeObject(data, "firewall status")
	if err != nil {
		return firewall.Status{}, err
	}
	if !hasAny(root, "enable_firewall", "enabled", "profile_name") {
		return firewall.Status{}, fmt.Errorf("decode firewall status: no recognized fields among %s", availableKeys(root))
	}
	enabled, _ := boolValue(root, "enable_firewall", "enabled", "enable")
	return firewall.Status{
		Enabled:       enabled,
		ActiveProfile: stringValue(root, "profile_name", "profile", "name"),
	}, nil
}

// decodeNameList decodes a {"<key>": ["a","b"]} string-array response. It rejects
// a response that carries none of the accepted keys as an array.
func decodeNameList(data json.RawMessage, keys ...string) ([]string, error) {
	root, err := decodeObject(data, "firewall name list")
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		value, ok := root[key]
		if !ok || value == nil {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			continue
		}
		names := make([]string, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok {
				if trimmed := strings.TrimSpace(s); trimmed != "" {
					names = append(names, trimmed)
				}
			}
		}
		return names, nil
	}
	return nil, fmt.Errorf("decode firewall name list: no string array among %s (wanted one of %s)", availableKeys(root), strings.Join(keys, ", "))
}

// decodeProfileRules decodes SYNO.Core.Security.Firewall.Profile get. The response
// is {"<adapter>": {"policy": "...", "rules": [...]}, ..., "name": "<profile>"}.
// Every top-level key other than "name" whose value is an object carrying a policy
// or rules field is treated as an adapter section.
func decodeProfileRules(data json.RawMessage, requestedProfile string) (firewall.ProfileRules, error) {
	root, err := decodeObject(data, "firewall profile rules")
	if err != nil {
		return firewall.ProfileRules{}, err
	}
	profileName := stringValue(root, "name")
	if profileName == "" {
		profileName = requestedProfile
	}
	// A valid response must at least name the profile; a response that is neither
	// the expected profile object nor carries any adapter section is unrecognized.
	adapters := make([]firewall.AdapterPolicy, 0, len(root))
	names := make([]string, 0, len(root))
	for key := range root {
		names = append(names, key)
	}
	sort.Strings(names)
	for _, key := range names {
		if key == "name" {
			continue
		}
		section, ok := root[key].(map[string]any)
		if !ok {
			continue
		}
		if _, hasPolicy := section["policy"]; !hasPolicy {
			if _, hasRules := section["rules"]; !hasRules {
				continue
			}
		}
		adapters = append(adapters, decodeAdapterPolicy(key, section))
	}
	if profileName == "" && len(adapters) == 0 {
		return firewall.ProfileRules{}, fmt.Errorf("decode firewall profile rules: no name or adapter sections among %s", availableKeys(root))
	}
	return firewall.ProfileRules{
		Profile:  profileName,
		Adapters: adapters,
	}, nil
}

func decodeAdapterPolicy(adapter string, section map[string]any) firewall.AdapterPolicy {
	policy := firewall.AdapterPolicy{
		Adapter: adapter,
		Policy:  stringValue(section, "policy", "default_policy"),
	}
	if policy.Policy == "" {
		policy.Policy = firewall.PolicyNone
	}
	rules := decodeRules(section)
	policy.Rules = rules
	policy.Total = intValue(section, "total")
	if policy.Total == 0 {
		policy.Total = len(rules)
	}
	return policy
}

// decodeRules decodes an adapter section's ordered rule array. DSM reports rules
// as [] (empty array) or null when there are none; both yield an empty list, not
// an error. The per-rule field names are live-verified (WI-066 Slice B): a
// throwaway rule written with the firewall disabled and read back confirmed the
// exact DSM tokens (enable, name, policy, protocol, port_direction, port_group,
// ports, source_ip_group, source_ip, log). They are still read tolerantly.
func decodeRules(section map[string]any) []firewall.Rule {
	value, ok := section["rules"]
	if !ok || value == nil {
		return []firewall.Rule{}
	}
	items, ok := value.([]any)
	if !ok {
		return []firewall.Rule{}
	}
	rules := make([]firewall.Rule, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		enabled, _ := boolValue(object, "enable", "enabled")
		logged, _ := boolValue(object, "log")
		rules = append(rules, firewall.Rule{
			Enabled:       enabled,
			Policy:        stringValue(object, "policy", "action"),
			Protocol:      stringValue(object, "protocol", "proto"),
			PortDirection: stringValue(object, "port_direction", "direction", "dir"),
			PortGroup:     stringValue(object, "port_group", "port_type"),
			Ports:         stringValue(object, "ports", "port", "service"),
			SourceGroup:   stringValue(object, "source_ip_group", "src_type", "source_type"),
			Source:        stringValue(object, "source_ip", "src", "source"),
			Log:           logged,
			Name:          stringValue(object, "name", "desc", "description"),
		})
	}
	return rules
}

// encodeRule renders a domain Rule back into the exact DSM Profile.set rule object
// shape confirmed live: {enable, name, policy, protocol, port_direction,
// port_group, ports, source_ip_group, source_ip, log}.
func encodeRule(rule firewall.Rule) map[string]any {
	return map[string]any{
		"enable":          rule.Enabled,
		"name":            rule.Name,
		"policy":          rule.Policy,
		"protocol":        rule.Protocol,
		"port_direction":  rule.PortDirection,
		"port_group":      rule.PortGroup,
		"ports":           rule.Ports,
		"source_ip_group": rule.SourceGroup,
		"source_ip":       rule.Source,
		"log":             rule.Log,
	}
}

// encodeProfile renders a full ProfileRules back into the DSM Profile.set "profile"
// parameter: {<adapter>:{policy, rules[]}, ..., name}.
func encodeProfile(profile firewall.ProfileRules) map[string]any {
	out := map[string]any{"name": profile.Profile}
	for _, adapter := range profile.Adapters {
		rules := make([]any, 0, len(adapter.Rules))
		for _, rule := range adapter.Rules {
			rules = append(rules, encodeRule(rule))
		}
		out[adapter.Adapter] = map[string]any{
			"policy": adapter.Policy,
			"rules":  rules,
		}
	}
	return out
}

// decodeCurrentConnection normalizes SYNO.Core.CurrentConnection list into the
// operator's session source. It never decodes any session secret (SID, token,
// device id): only the source address and the is-current marker. The returned
// slice preserves each entry so the guard can pick the current one and fall back
// to protecting all active sources.
func decodeCurrentConnection(data json.RawMessage) ([]firewall.SessionSource, error) {
	root, err := decodeObject(data, "current connection")
	if err != nil {
		return nil, err
	}
	value, ok := root["items"]
	if !ok || value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("decode current connection: items is not an array")
	}
	sources := make([]firewall.SessionSource, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		from := stringValue(object, "from", "ip", "ip_addr")
		if from == "" {
			continue
		}
		current, _ := boolValue(object, "is_current_connected", "is_current", "current")
		sources = append(sources, firewall.SessionSource{
			From:    from,
			Who:     stringValue(object, "who", "user"),
			Current: current,
		})
	}
	return sources, nil
}

// --- shared lenient decoding helpers (mirrors the accountprotection pkg) ---

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
