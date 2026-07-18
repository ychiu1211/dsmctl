package fileservices

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
)

func decodeSMB(data json.RawMessage, modern bool) (controlpanel.SMBState, error) {
	raw, err := decodeObject(data, "SMB")
	if err != nil {
		return controlpanel.SMBState{}, err
	}
	enabled, err := requiredBool(raw, "enable_samba", "SMB")
	if err != nil {
		return controlpanel.SMBState{}, err
	}
	workgroup, err := requiredString(raw, "workgroup", "SMB", true)
	if err != nil {
		return controlpanel.SMBState{}, err
	}
	state := controlpanel.SMBState{Enabled: enabled, Workgroup: strings.TrimSpace(workgroup)}
	if !modern {
		return state, nil
	}
	minimum, err := requiredInt(raw, "smb_min_protocol", "SMB")
	if err != nil {
		return controlpanel.SMBState{}, err
	}
	maximum, err := requiredInt(raw, "smb_max_protocol", "SMB")
	if err != nil {
		return controlpanel.SMBState{}, err
	}
	encryption, err := requiredInt(raw, "smb_encrypt_transport", "SMB")
	if err != nil {
		return controlpanel.SMBState{}, err
	}
	signing, err := requiredInt(raw, "enable_server_signing", "SMB")
	if err != nil {
		return controlpanel.SMBState{}, err
	}
	state.MinimumProtocol, err = smbProtocol(minimum)
	if err != nil {
		return controlpanel.SMBState{}, err
	}
	state.MaximumProtocol, err = smbProtocol(maximum)
	if err != nil {
		return controlpanel.SMBState{}, err
	}
	state.TransportEncryption, err = smbPolicy(encryption)
	if err != nil {
		return controlpanel.SMBState{}, err
	}
	state.ServerSigning, err = smbSigningPolicy(signing)
	if err != nil {
		return controlpanel.SMBState{}, err
	}
	// Advanced "Others" toggles are read optionally so an older backend that
	// omits one leaves it false rather than failing the whole read.
	state.OpportunisticLocking = optionalBool(raw, "enable_op_lock")
	state.SMB2Leases = optionalBool(raw, "enable_smb2_leases")
	state.DurableHandles = optionalBool(raw, "enable_durable_handles")
	state.LocalMasterBrowser = optionalBool(raw, "enable_local_master_browser")
	return state, nil
}

// optionalBool reads a DSM boolean that may be absent or encoded as 0/1,
// defaulting to false when missing or unparseable.
func optionalBool(raw map[string]json.RawMessage, name string) bool {
	value, ok := raw[name]
	if !ok {
		return false
	}
	var result bool
	if err := json.Unmarshal(value, &result); err == nil {
		return result
	}
	if integer, err := decodeInt(value); err == nil {
		return integer == 1
	}
	return false
}

func decodeNFS(data json.RawMessage, modern bool) (controlpanel.NFSState, error) {
	raw, err := decodeObject(data, "NFS")
	if err != nil {
		return controlpanel.NFSState{}, err
	}
	enabled, err := requiredBool(raw, "enable_nfs", "NFS")
	if err != nil {
		return controlpanel.NFSState{}, err
	}
	state := controlpanel.NFSState{
		Enabled:            enabled,
		MaximumProtocol:    controlpanel.NFSProtocol3,
		SupportedProtocols: []controlpanel.NFSProtocol{controlpanel.NFSProtocol2, controlpanel.NFSProtocol3},
	}
	if !modern {
		return state, nil
	}
	v4, err := requiredBool(raw, "enable_nfs_v4", "NFS")
	if err != nil {
		return controlpanel.NFSState{}, err
	}
	minor, err := requiredInt(raw, "enabled_minor_ver", "NFS")
	if err != nil {
		return controlpanel.NFSState{}, err
	}
	if v4 {
		switch minor {
		case 0:
			state.MaximumProtocol = controlpanel.NFSProtocol4
		case 1:
			state.MaximumProtocol = controlpanel.NFSProtocol4_1
		default:
			return controlpanel.NFSState{}, fmt.Errorf("decode NFS: unsupported enabled_minor_ver %d", minor)
		}
	}
	state.SupportedProtocols = supportedNFSProtocols(raw, state.MaximumProtocol)
	return state, nil
}

func decodeNFSAdvanced(data json.RawMessage) (string, error) {
	raw, err := decodeObject(data, "NFS advanced")
	if err != nil {
		return "", err
	}
	return decodeNFSAdvancedDomain(raw), nil
}

func decodeNFSAdvancedDomain(raw map[string]json.RawMessage) string {
	value, ok := raw["nfs_v4_domain"]
	if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return ""
	}
	var domain string
	if err := json.Unmarshal(value, &domain); err != nil {
		return ""
	}
	return strings.TrimSpace(domain)
}

// decodeNFSAdvancedSnapshot captures the raw advanced get response verbatim and
// parses the NFSv4 domain. It deliberately does not require any particular field
// because the DSM AdvancedSetting get response omits base-service fields such as
// enable_nfs; the write path resubmits exactly the advanced fields DSM returned.
// decodeNFSAdvancedSnapshot decodes the advanced get response into the types the
// set method's validation requires. enable_nfs is intentionally not read here:
// the get response omits it, and the facade supplies the current base service
// state instead. requiredBool tolerates DSM returning booleans as integers,
// which the AdvancedSetting get does for custom_port_enable.
func decodeNFSAdvancedSnapshot(data json.RawMessage) (NFSAdvancedSnapshot, error) {
	raw, err := decodeObject(data, "NFS advanced")
	if err != nil {
		return NFSAdvancedSnapshot{}, err
	}
	readSize, err := requiredInt(raw, "read_size", "NFS advanced")
	if err != nil {
		return NFSAdvancedSnapshot{}, err
	}
	writeSize, err := requiredInt(raw, "write_size", "NFS advanced")
	if err != nil {
		return NFSAdvancedSnapshot{}, err
	}
	unixPermissions, err := requiredBool(raw, "unix_pri_enable", "NFS advanced")
	if err != nil {
		return NFSAdvancedSnapshot{}, err
	}
	customPort, err := requiredBool(raw, "custom_port_enable", "NFS advanced")
	if err != nil {
		return NFSAdvancedSnapshot{}, err
	}
	statdPort, _ := optionalInt(raw, "statd_port")
	nlmPort, _ := optionalInt(raw, "nlm_port")
	return NFSAdvancedSnapshot{
		CustomPortEnable: customPort,
		ReadSize:         readSize,
		WriteSize:        writeSize,
		UnixPermissions:  unixPermissions,
		StatdPort:        statdPort,
		NLMPort:          nlmPort,
		Domain:           decodeNFSAdvancedDomain(raw),
	}, nil
}

func supportedNFSProtocols(raw map[string]json.RawMessage, configured controlpanel.NFSProtocol) []controlpanel.NFSProtocol {
	protocols := []controlpanel.NFSProtocol{controlpanel.NFSProtocol2, controlpanel.NFSProtocol3}
	major, majorOK := optionalInt(raw, "support_major_ver")
	minor, minorOK := optionalInt(raw, "support_minor_ver")
	if majorOK && major >= 4 {
		protocols = append(protocols, controlpanel.NFSProtocol4)
		if minorOK && minor >= 1 {
			protocols = append(protocols, controlpanel.NFSProtocol4_1)
		}
	}
	if configured == controlpanel.NFSProtocol4 && !containsNFSProtocol(protocols, configured) {
		protocols = append(protocols, configured)
	}
	if configured == controlpanel.NFSProtocol4_1 {
		if !containsNFSProtocol(protocols, controlpanel.NFSProtocol4) {
			protocols = append(protocols, controlpanel.NFSProtocol4)
		}
		if !containsNFSProtocol(protocols, configured) {
			protocols = append(protocols, configured)
		}
	}
	return protocols
}

func containsNFSProtocol(values []controlpanel.NFSProtocol, wanted controlpanel.NFSProtocol) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func decodeObject(data json.RawMessage, label string) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("decode %s: expected a non-empty object", label)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("decode %s object: %w", label, err)
	}
	if raw == nil {
		return nil, fmt.Errorf("decode %s: expected an object", label)
	}
	return raw, nil
}

func requiredString(raw map[string]json.RawMessage, name, label string, nonEmpty bool) (string, error) {
	value, ok := raw[name]
	if !ok {
		return "", fmt.Errorf("decode %s: required field %q is missing", label, name)
	}
	var result string
	if err := json.Unmarshal(value, &result); err != nil {
		return "", fmt.Errorf("decode %s field %q: %w", label, name, err)
	}
	if nonEmpty && strings.TrimSpace(result) == "" {
		return "", fmt.Errorf("decode %s: required field %q is empty", label, name)
	}
	return result, nil
}

func requiredBool(raw map[string]json.RawMessage, name, label string) (bool, error) {
	value, ok := raw[name]
	if !ok {
		return false, fmt.Errorf("decode %s: required field %q is missing", label, name)
	}
	var result bool
	if err := json.Unmarshal(value, &result); err == nil {
		return result, nil
	}
	integer, err := decodeInt(value)
	if err != nil || (integer != 0 && integer != 1) {
		return false, fmt.Errorf("decode %s field %q: expected boolean", label, name)
	}
	return integer == 1, nil
}

func requiredInt(raw map[string]json.RawMessage, name, label string) (int, error) {
	value, ok := raw[name]
	if !ok {
		return 0, fmt.Errorf("decode %s: required field %q is missing", label, name)
	}
	result, err := decodeInt(value)
	if err != nil {
		return 0, fmt.Errorf("decode %s field %q: %w", label, name, err)
	}
	return result, nil
}

func optionalInt(raw map[string]json.RawMessage, name string) (int, bool) {
	value, ok := raw[name]
	if !ok {
		return 0, false
	}
	result, err := decodeInt(value)
	return result, err == nil
}

func decodeInt(value json.RawMessage) (int, error) {
	var integer int
	if err := json.Unmarshal(value, &integer); err == nil {
		return integer, nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return 0, fmt.Errorf("expected integer")
	}
	integer, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, fmt.Errorf("expected integer")
	}
	return integer, nil
}

func smbProtocol(value int) (controlpanel.SMBProtocol, error) {
	switch value {
	case 0:
		return controlpanel.SMBProtocol1, nil
	case 1:
		return controlpanel.SMBProtocol2, nil
	case 2:
		return controlpanel.SMBProtocol2LargeMTU, nil
	case 3:
		return controlpanel.SMBProtocol3, nil
	default:
		return "", fmt.Errorf("decode SMB: unsupported protocol value %d", value)
	}
}

func smbPolicy(value int) (controlpanel.SMBPolicy, error) {
	switch value {
	case 0:
		return controlpanel.SMBPolicyDisabled, nil
	case 1:
		return controlpanel.SMBPolicyAutomatic, nil
	case 2:
		return controlpanel.SMBPolicyRequired, nil
	default:
		return "", fmt.Errorf("decode SMB: unsupported policy value %d", value)
	}
}

func smbSigningPolicy(value int) (controlpanel.SMBSigningPolicy, error) {
	switch value {
	case 0:
		return controlpanel.SMBSigningDisabledForSMB1, nil
	case 1:
		return controlpanel.SMBSigningAutomatic, nil
	case 2:
		return controlpanel.SMBSigningRequired, nil
	default:
		return "", fmt.Errorf("decode SMB: unsupported signing policy value %d", value)
	}
}
