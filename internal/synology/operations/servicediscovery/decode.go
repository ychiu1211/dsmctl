package servicediscovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func decodeTimeMachine(data json.RawMessage) (TimeMachine, error) {
	raw, err := decodeObject(data)
	if err != nil {
		return TimeMachine{}, err
	}
	smb, err := requiredBool(raw, "enable_smb_time_machine")
	if err != nil {
		return TimeMachine{}, err
	}
	afp, err := requiredBool(raw, "enable_afp_time_machine")
	if err != nil {
		return TimeMachine{}, err
	}
	return TimeMachine{SMB: smb, AFP: afp}, nil
}

func decodeWSDiscovery(data json.RawMessage) (bool, error) {
	raw, err := decodeObject(data)
	if err != nil {
		return false, err
	}
	return requiredBool(raw, "enable_wstransfer")
}

func decodeObject(data json.RawMessage) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("decode service discovery: expected a non-empty object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("decode service discovery object: %w", err)
	}
	return raw, nil
}

func requiredBool(raw map[string]json.RawMessage, name string) (bool, error) {
	value, ok := raw[name]
	if !ok {
		return false, fmt.Errorf("decode service discovery: required field %q is missing", name)
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
	return false, fmt.Errorf("decode service discovery field %q: expected boolean", name)
}
