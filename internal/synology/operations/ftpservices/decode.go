package ftpservices

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func decodeFTP(data json.RawMessage) (FTPSettings, error) {
	raw, err := decodeObject(data)
	if err != nil {
		return FTPSettings{}, err
	}
	plain, err := requiredBool(raw, "enable_ftp")
	if err != nil {
		return FTPSettings{}, err
	}
	ftps, err := requiredBool(raw, "enable_ftps")
	if err != nil {
		return FTPSettings{}, err
	}
	return FTPSettings{Plain: plain, FTPS: ftps}, nil
}

func decodeSFTP(data json.RawMessage) (SFTPSettings, error) {
	raw, err := decodeObject(data)
	if err != nil {
		return SFTPSettings{}, err
	}
	enabled, err := requiredBool(raw, "enable")
	if err != nil {
		return SFTPSettings{}, err
	}
	port, err := requiredInt(raw, "portnum")
	if err != nil {
		return SFTPSettings{}, err
	}
	return SFTPSettings{Enabled: enabled, Port: port}, nil
}

func decodeObject(data json.RawMessage) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("decode FTP services: expected a non-empty object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("decode FTP services object: %w", err)
	}
	return raw, nil
}

func requiredBool(raw map[string]json.RawMessage, name string) (bool, error) {
	value, ok := raw[name]
	if !ok {
		return false, fmt.Errorf("decode FTP services: required field %q is missing", name)
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
	return false, fmt.Errorf("decode FTP services field %q: expected boolean", name)
}

func requiredInt(raw map[string]json.RawMessage, name string) (int, error) {
	value, ok := raw[name]
	if !ok {
		return 0, fmt.Errorf("decode FTP services: required field %q is missing", name)
	}
	var integer int
	if err := json.Unmarshal(value, &integer); err == nil {
		return integer, nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		if parsed, convErr := strconv.Atoi(strings.TrimSpace(text)); convErr == nil {
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("decode FTP services field %q: expected integer", name)
}
