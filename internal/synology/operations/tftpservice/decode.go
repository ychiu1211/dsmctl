package tftpservice

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
	// Live DSM 7.3.2 get returns the same field names the set uses: "enable",
	// "enable_log", "startip"/"endip", "permission", "root_path", "timeout" (the
	// source doc comment names enabled/log_enabled/ip_low/ip_high are stale). Only
	// the service switch is treated as required; the rest default gracefully.
	enabled, err := requiredBool(raw, "enable")
	if err != nil {
		return Settings{}, err
	}
	logEnabled, err := optionalBool(raw, "enable_log")
	if err != nil {
		return Settings{}, err
	}
	allowWrite, err := decodePermission(raw)
	if err != nil {
		return Settings{}, err
	}
	timeout, err := optionalInt(raw, "timeout")
	if err != nil {
		return Settings{}, err
	}
	return Settings{
		Enabled:      enabled,
		RootPath:     optionalString(raw, "root_path"),
		AllowWrite:   allowWrite,
		LogEnabled:   logEnabled,
		ClientIPLow:  optionalString(raw, "startip"),
		ClientIPHigh: optionalString(raw, "endip"),
		Timeout:      timeout,
	}, nil
}

func decodeObject(data json.RawMessage) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("decode TFTP service: expected a non-empty object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("decode TFTP service object: %w", err)
	}
	return raw, nil
}

// decodePermission maps DSM's "rw"/"r" to AllowWrite. A missing value defaults to
// read-only; an unrecognized value is an error.
func decodePermission(raw map[string]json.RawMessage) (bool, error) {
	value, ok := raw["permission"]
	if !ok {
		return false, nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return false, fmt.Errorf("decode TFTP service field \"permission\": expected string")
	}
	switch strings.TrimSpace(text) {
	case "rw":
		return true, nil
	case "r", "":
		return false, nil
	default:
		return false, fmt.Errorf("decode TFTP service field \"permission\": unexpected value %q", text)
	}
}

func requiredBool(raw map[string]json.RawMessage, name string) (bool, error) {
	value, ok := raw[name]
	if !ok {
		return false, fmt.Errorf("decode TFTP service: required field %q is missing", name)
	}
	return parseBool(value, name)
}

func optionalBool(raw map[string]json.RawMessage, name string) (bool, error) {
	value, ok := raw[name]
	if !ok {
		return false, nil
	}
	return parseBool(value, name)
}

func parseBool(value json.RawMessage, name string) (bool, error) {
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
	return false, fmt.Errorf("decode TFTP service field %q: expected boolean", name)
}

func optionalInt(raw map[string]json.RawMessage, name string) (int, error) {
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
	return 0, fmt.Errorf("decode TFTP service field %q: expected integer", name)
}

func optionalString(raw map[string]json.RawMessage, name string) string {
	value, ok := raw[name]
	if !ok {
		return ""
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		return text
	}
	return ""
}
