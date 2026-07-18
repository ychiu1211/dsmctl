package nfsexport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/nfsexport"
)

// decodeRules decodes a SharePrivilege load response. DSM always sets the
// "rule" array (empty when a shared folder has no export rules), so a missing
// key is treated as a malformed response rather than a silent empty result.
func decodeRules(data json.RawMessage) ([]nfsexport.Rule, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("decode NFS export: expected a non-empty object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("decode NFS export object: %w", err)
	}
	value, ok := raw["rule"]
	if !ok {
		return nil, fmt.Errorf("decode NFS export: required field %q is missing", "rule")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(value, &entries); err != nil {
		return nil, fmt.Errorf("decode NFS export field %q: %w", "rule", err)
	}
	rules := make([]nfsexport.Rule, 0, len(entries))
	for index, entry := range entries {
		rule, err := decodeRule(entry)
		if err != nil {
			return nil, fmt.Errorf("decode NFS export rule %d: %w", index, err)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func decodeRule(raw map[string]json.RawMessage) (nfsexport.Rule, error) {
	client, err := requiredString(raw, "client")
	if err != nil {
		return nfsexport.Rule{}, err
	}
	privilege, err := decodePrivilege(raw)
	if err != nil {
		return nfsexport.Rule{}, err
	}
	squash, err := decodeSquash(raw)
	if err != nil {
		return nfsexport.Rule{}, err
	}
	security, err := decodeSecurity(raw)
	if err != nil {
		return nfsexport.Rule{}, err
	}
	async, err := requiredBool(raw, "async")
	if err != nil {
		return nfsexport.Rule{}, err
	}
	insecure, err := requiredBool(raw, "insecure")
	if err != nil {
		return nfsexport.Rule{}, err
	}
	crossmnt, err := requiredBool(raw, "crossmnt")
	if err != nil {
		return nfsexport.Rule{}, err
	}
	return nfsexport.Rule{
		Client:                  strings.TrimSpace(client),
		Privilege:               privilege,
		Squash:                  squash,
		Security:                security,
		Async:                   async,
		AllowNonprivilegedPorts: insecure,
		AllowSubfolderAccess:    crossmnt,
	}, nil
}

func decodePrivilege(raw map[string]json.RawMessage) (nfsexport.Privilege, error) {
	value, err := requiredString(raw, "privilege")
	if err != nil {
		return "", err
	}
	switch value {
	case "rw":
		return nfsexport.PrivilegeReadWrite, nil
	case "ro":
		return nfsexport.PrivilegeReadOnly, nil
	default:
		return "", fmt.Errorf("unsupported NFS export privilege %q", value)
	}
}

func decodeSquash(raw map[string]json.RawMessage) (nfsexport.Squash, error) {
	value, err := requiredString(raw, "root_squash")
	if err != nil {
		return "", err
	}
	switch value {
	case "root":
		return nfsexport.SquashNoMapping, nil
	case "admin":
		return nfsexport.SquashRootToAdmin, nil
	case "guest":
		return nfsexport.SquashRootToGuest, nil
	case "all_admin":
		return nfsexport.SquashAllToAdmin, nil
	case "all_guest":
		return nfsexport.SquashAllToGuest, nil
	default:
		return "", fmt.Errorf("unsupported NFS export root squash %q", value)
	}
}

// decodeSecurity reads the security_flavor object. DSM represents the flavor as
// four booleans ({sys, kerberos, kerberos_integrity, kerberos_privacy}), not a
// string, and requires at least one to be enabled. The normalized model keeps a
// single flavor, so the strongest enabled option wins.
func decodeSecurity(raw map[string]json.RawMessage) (nfsexport.Security, error) {
	value, ok := raw["security_flavor"]
	if !ok {
		return "", fmt.Errorf("required field %q is missing", "security_flavor")
	}
	var flavor map[string]json.RawMessage
	if err := json.Unmarshal(value, &flavor); err != nil {
		return "", fmt.Errorf("field %q must be an object: %w", "security_flavor", err)
	}
	enabled := func(key string) bool {
		result, err := requiredBool(flavor, key)
		return err == nil && result
	}
	switch {
	case enabled("kerberos_privacy"):
		return nfsexport.SecurityKerberosPrivacy, nil
	case enabled("kerberos_integrity"):
		return nfsexport.SecurityKerberosIntegrity, nil
	case enabled("kerberos"):
		return nfsexport.SecurityKerberos, nil
	case enabled("sys"):
		return nfsexport.SecuritySys, nil
	default:
		return "", fmt.Errorf("NFS export security flavor has no enabled option")
	}
}

func requiredString(raw map[string]json.RawMessage, name string) (string, error) {
	value, ok := raw[name]
	if !ok {
		return "", fmt.Errorf("required field %q is missing", name)
	}
	var result string
	if err := json.Unmarshal(value, &result); err != nil {
		return "", fmt.Errorf("field %q: %w", name, err)
	}
	if strings.TrimSpace(result) == "" && name == "client" {
		return "", fmt.Errorf("field %q is empty", name)
	}
	return result, nil
}

func requiredBool(raw map[string]json.RawMessage, name string) (bool, error) {
	value, ok := raw[name]
	if !ok {
		return false, fmt.Errorf("required field %q is missing", name)
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
	return false, fmt.Errorf("field %q: expected boolean", name)
}
