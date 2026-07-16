package fileservices

import (
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
)

func encodeSMBChange(change controlpanel.SMBChange) (map[string]any, error) {
	parameters := make(map[string]any)
	if change.Enabled != nil {
		parameters["enable_samba"] = *change.Enabled
	}
	if change.Workgroup != nil {
		parameters["workgroup"] = strings.TrimSpace(*change.Workgroup)
	}
	if change.MinimumProtocol != nil {
		value, err := encodeSMBProtocol(*change.MinimumProtocol)
		if err != nil {
			return nil, err
		}
		parameters["smb_min_protocol"] = value
	}
	if change.MaximumProtocol != nil {
		value, err := encodeSMBProtocol(*change.MaximumProtocol)
		if err != nil {
			return nil, err
		}
		parameters["smb_max_protocol"] = value
	}
	if change.TransportEncryption != nil {
		value, err := encodeSMBPolicy(*change.TransportEncryption)
		if err != nil {
			return nil, err
		}
		parameters["smb_encrypt_transport"] = value
	}
	if change.ServerSigning != nil {
		value, err := encodeSMBSigningPolicy(*change.ServerSigning)
		if err != nil {
			return nil, err
		}
		parameters["enable_server_signing"] = value
	}
	if len(parameters) == 0 {
		return nil, fmt.Errorf("SMB change has no fields")
	}
	return parameters, nil
}

func encodeNFSBaseChange(change controlpanel.NFSChange) (map[string]any, error) {
	if change.NFSv4Domain != nil {
		return nil, fmt.Errorf("NFS base mutation does not accept nfsv4_domain")
	}
	parameters := make(map[string]any)
	if change.Enabled != nil {
		parameters["enable_nfs"] = *change.Enabled
	}
	if change.MaximumProtocol != nil {
		switch *change.MaximumProtocol {
		case controlpanel.NFSProtocol3:
			parameters["nfs_max_protocol"] = 0
			parameters["enable_nfs_v4"] = false
			parameters["enabled_minor_ver"] = 0
		case controlpanel.NFSProtocol4:
			parameters["nfs_max_protocol"] = 1
			parameters["enable_nfs_v4"] = true
			parameters["enabled_minor_ver"] = 0
		case controlpanel.NFSProtocol4_1:
			parameters["nfs_max_protocol"] = 2
			parameters["enable_nfs_v4"] = true
			parameters["enabled_minor_ver"] = 1
		default:
			return nil, fmt.Errorf("unsupported NFS maximum protocol %q", *change.MaximumProtocol)
		}
	}
	if len(parameters) == 0 {
		return nil, fmt.Errorf("NFS base change has no fields")
	}
	return parameters, nil
}

func encodeSMBSigningPolicy(value controlpanel.SMBSigningPolicy) (int, error) {
	switch value {
	case controlpanel.SMBSigningDisabledForSMB1:
		return 0, nil
	case controlpanel.SMBSigningAutomatic:
		return 1, nil
	case controlpanel.SMBSigningRequired:
		return 2, nil
	default:
		return 0, fmt.Errorf("unsupported SMB signing policy %q", value)
	}
}

func encodeSMBProtocol(value controlpanel.SMBProtocol) (int, error) {
	switch value {
	case controlpanel.SMBProtocol1:
		return 0, nil
	case controlpanel.SMBProtocol2:
		return 1, nil
	case controlpanel.SMBProtocol2LargeMTU:
		return 2, nil
	case controlpanel.SMBProtocol3:
		return 3, nil
	default:
		return 0, fmt.Errorf("unsupported SMB protocol %q", value)
	}
}

func encodeSMBPolicy(value controlpanel.SMBPolicy) (int, error) {
	switch value {
	case controlpanel.SMBPolicyDisabled:
		return 0, nil
	case controlpanel.SMBPolicyAutomatic:
		return 1, nil
	case controlpanel.SMBPolicyRequired:
		return 2, nil
	default:
		return 0, fmt.Errorf("unsupported SMB policy %q", value)
	}
}
