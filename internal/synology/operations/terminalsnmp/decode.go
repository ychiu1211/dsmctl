package terminalsnmp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/terminalsnmp"
)

// The decoders are strict about the response envelope and the presence of the
// primary service switch (so a missing or malformed shape errors instead of
// decoding a silent empty success), and lenient about the remaining fields.
//
// SECRET HYGIENE: the SNMP decoder never reads the value of a secret field into
// the model. The read community string (rocommunity), the SNMPv3 auth/privacy
// passwords, and any trap community are read only to the extent of testing
// whether a value is present (a bool); the bytes are discarded and never enter
// the domain model, a request, a hash, a log, or any output.

func decodeTerminal(data json.RawMessage) (terminalsnmp.TerminalState, error) {
	root, err := decodeObject(data, "terminal configuration")
	if err != nil {
		return terminalsnmp.TerminalState{}, err
	}
	sshEnabled, ok := boolValue(root, "enable_ssh")
	if !ok {
		return terminalsnmp.TerminalState{}, fmt.Errorf("decode terminal configuration: no enable_ssh field among %s", availableKeys(root))
	}
	state := terminalsnmp.TerminalState{
		SSHEnabled: sshEnabled,
		SSHPort:    intValue(root, "ssh_port"),
	}
	state.TelnetEnabled, _ = boolValue(root, "enable_telnet")
	state.ConsoleForbidden, _ = boolValue(root, "forbid_console")
	return state, nil
}

func decodeSNMP(data json.RawMessage) (terminalsnmp.SNMPState, error) {
	root, err := decodeObject(data, "SNMP configuration")
	if err != nil {
		return terminalsnmp.SNMPState{}, err
	}
	enabled, ok := boolValue(root, "enable_snmp")
	if !ok {
		return terminalsnmp.SNMPState{}, fmt.Errorf("decode SNMP configuration: no enable_snmp field among %s", availableKeys(root))
	}
	state := terminalsnmp.SNMPState{
		Enabled:  enabled,
		Location: stringValue(root, "location"),
		Contact:  stringValue(root, "contact"),
		// rouser is the SNMPv3 read-only username, non-secret identity. The v3
		// auth/privacy passwords (secrets) are deliberately never read.
		V3User: stringValue(root, "rouser", "snmpv3_user", "v3_user"),
	}
	state.V1V2cEnabled, _ = boolValue(root, "enable_snmp_v1v2", "enable_snmp_v1", "snmpv1")
	state.V3Enabled, _ = boolValue(root, "enable_snmp_v3", "snmpv3")
	// Community string: presence only, value discarded. Never stored.
	state.CommunityConfigured = hasSecretValue(root, "rocommunity", "community", "get_community")
	// Trap target: WIRE-UNVERIFIED. The lab SNMP service is disabled, so the get
	// response carries no trap fields to confirm against; these candidate names
	// are the author's best knowledge and must be reconciled live once a trap is
	// configured. The trap host is read only to test presence; any trap community
	// is a secret and is never read.
	state.TrapHostPresent = hasSecretValue(root, "trap_host", "trapdst", "trap_target", "trap_ip", "trap_address")
	state.TrapConfigured = state.TrapHostPresent
	if trapEnabled, ok := boolValue(root, "enable_snmp_trap", "trap_enable", "enable_trap"); ok && trapEnabled {
		state.TrapConfigured = true
	}
	return state, nil
}

// hasSecretValue reports whether any of the named keys holds a non-empty string,
// WITHOUT returning or retaining the string. It is how presence of a secret (the
// community string, a trap host) is surfaced without the bytes ever entering the
// model.
func hasSecretValue(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if raw, ok := values[key]; ok {
			if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
				return true
			}
		}
	}
	return false
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
