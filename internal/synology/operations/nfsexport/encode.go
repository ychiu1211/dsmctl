package nfsexport

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/nfsexport"
)

// encodeRules renders the complete desired rule set as the DSM "rule" array.
// Each entry carries an "id": the previous client for a rule that already
// exists (an edit) or an empty string for a new rule (a creation). Rules that
// exist on DSM but are absent from the desired set are simply omitted, which
// makes the save a full replacement.
func encodeRules(input SaveInput) (json.RawMessage, error) {
	entries := make([]map[string]any, 0, len(input.Rules))
	for _, rule := range input.Rules {
		client := strings.TrimSpace(rule.Client)
		privilege, err := encodePrivilege(rule.Privilege)
		if err != nil {
			return nil, err
		}
		squash, err := encodeSquash(rule.Squash)
		if err != nil {
			return nil, err
		}
		security, err := encodeSecurity(rule.Security)
		if err != nil {
			return nil, err
		}
		id := ""
		if _, ok := input.ExistingClients[client]; ok {
			id = client
		}
		entries = append(entries, map[string]any{
			"id":              id,
			"client":          client,
			"privilege":       privilege,
			"root_squash":     squash,
			"security_flavor": security,
			"async":           rule.Async,
			"insecure":        rule.AllowNonprivilegedPorts,
			"crossmnt":        rule.AllowSubfolderAccess,
		})
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("encode NFS export rules: %w", err)
	}
	return encoded, nil
}

func encodePrivilege(value nfsexport.Privilege) (string, error) {
	switch value {
	case nfsexport.PrivilegeReadWrite:
		return "rw", nil
	case nfsexport.PrivilegeReadOnly:
		return "ro", nil
	default:
		return "", fmt.Errorf("unsupported NFS export privilege %q", value)
	}
}

func encodeSquash(value nfsexport.Squash) (string, error) {
	switch value {
	case nfsexport.SquashNoMapping:
		return "root", nil
	case nfsexport.SquashRootToAdmin:
		return "admin", nil
	case nfsexport.SquashRootToGuest:
		return "guest", nil
	case nfsexport.SquashAllToAdmin:
		return "all_admin", nil
	case nfsexport.SquashAllToGuest:
		return "all_guest", nil
	default:
		return "", fmt.Errorf("unsupported NFS export root squash %q", value)
	}
}

// encodeSecurity renders the security flavor as the DSM boolean object with
// exactly the selected flavor enabled. DSM requires at least one enabled.
func encodeSecurity(value nfsexport.Security) (map[string]bool, error) {
	flavor := map[string]bool{
		"sys":                false,
		"kerberos":           false,
		"kerberos_integrity": false,
		"kerberos_privacy":   false,
	}
	switch value {
	case nfsexport.SecuritySys:
		flavor["sys"] = true
	case nfsexport.SecurityKerberos:
		flavor["kerberos"] = true
	case nfsexport.SecurityKerberosIntegrity:
		flavor["kerberos_integrity"] = true
	case nfsexport.SecurityKerberosPrivacy:
		flavor["kerberos_privacy"] = true
	default:
		return nil, fmt.Errorf("unsupported NFS export security flavor %q", value)
	}
	return flavor, nil
}
