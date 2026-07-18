package rsyncservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func decodeSettings(data json.RawMessage) (Settings, error) {
	raw, err := decodeObject(data)
	if err != nil {
		return Settings{}, err
	}
	// DSM 7.3.2 get returns the service switch as "enable" (the same name the set
	// uses), the account switch as "enable_rsync_account", and the port as a
	// string; confirmed live.
	enabled, err := requiredBool(raw, "enable")
	if err != nil {
		return Settings{}, err
	}
	account, err := requiredBool(raw, "enable_rsync_account")
	if err != nil {
		return Settings{}, err
	}
	port, err := optionalPort(raw, "rsync_sshd_port")
	if err != nil {
		return Settings{}, err
	}
	return Settings{Enabled: enabled, RsyncAccount: account, SSHPort: port}, nil
}

func decodeObject(data json.RawMessage) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("decode rsync service: expected a non-empty object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("decode rsync service object: %w", err)
	}
	return raw, nil
}

func requiredBool(raw map[string]json.RawMessage, name string) (bool, error) {
	value, ok := raw[name]
	if !ok {
		return false, fmt.Errorf("decode rsync service: required field %q is missing", name)
	}
	var result bool
	if err := json.Unmarshal(value, &result); err == nil {
		return result, nil
	}
	var integer int
	if err := json.Unmarshal(value, &integer); err == nil && (integer == 0 || integer == 1) {
		return integer == 1, nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		if parsed, convErr := strconv.Atoi(strings.TrimSpace(text)); convErr == nil && (parsed == 0 || parsed == 1) {
			return parsed == 1, nil
		}
	}
	return false, fmt.Errorf("decode rsync service field %q: expected boolean", name)
}

// optionalPort reads a port that DSM returns as a string; a missing or empty
// value yields 0.
func optionalPort(raw map[string]json.RawMessage, name string) (int, error) {
	value, ok := raw[name]
	if !ok {
		return 0, nil
	}
	var integer int
	if err := json.Unmarshal(value, &integer); err == nil {
		return integer, nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return 0, nil
		}
		if parsed, convErr := strconv.Atoi(trimmed); convErr == nil {
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("decode rsync service field %q: expected a port number", name)
}
