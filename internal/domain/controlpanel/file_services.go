package controlpanel

// FileProtocol selects one independently managed File Services module.
type FileProtocol string

const (
	FileProtocolSMB FileProtocol = "smb"
	FileProtocolNFS FileProtocol = "nfs"
)

// SMBProtocol is a stable name for DSM's ordered SMB protocol levels.
type SMBProtocol string

const (
	SMBProtocol1         SMBProtocol = "smb1"
	SMBProtocol2         SMBProtocol = "smb2"
	SMBProtocol2LargeMTU SMBProtocol = "smb2_large_mtu"
	SMBProtocol3         SMBProtocol = "smb3"
)

// SMBPolicy describes DSM's three-state transport encryption and server
// signing controls.
type SMBPolicy string

const (
	SMBPolicyDisabled  SMBPolicy = "disabled"
	SMBPolicyAutomatic SMBPolicy = "automatic"
	SMBPolicyRequired  SMBPolicy = "required"
)

// SMBSigningPolicy keeps DSM's SMB1-only disabled mode distinct from a claim
// that signing is disabled for every protocol.
type SMBSigningPolicy string

const (
	SMBSigningDisabledForSMB1 SMBSigningPolicy = "disabled_for_smb1"
	SMBSigningAutomatic       SMBSigningPolicy = "automatic"
	SMBSigningRequired        SMBSigningPolicy = "required"
)

type SMBState struct {
	Enabled             bool             `json:"enabled" jsonschema:"Whether the global DSM SMB service is enabled"`
	Workgroup           string           `json:"workgroup" jsonschema:"SMB workgroup name"`
	MinimumProtocol     SMBProtocol      `json:"minimum_protocol,omitempty" jsonschema:"Minimum accepted SMB protocol when exposed by this DSM backend"`
	MaximumProtocol     SMBProtocol      `json:"maximum_protocol,omitempty" jsonschema:"Maximum accepted SMB protocol when exposed by this DSM backend"`
	TransportEncryption SMBPolicy        `json:"transport_encryption,omitempty" jsonschema:"SMB transport encryption policy when exposed by this DSM backend"`
	ServerSigning       SMBSigningPolicy `json:"server_signing,omitempty" jsonschema:"SMB server signing policy when exposed by this DSM backend"`
	// Advanced "Others" toggles, populated when the modern SMB backend reports
	// them. They are independent booleans.
	OpportunisticLocking bool `json:"opportunistic_locking" jsonschema:"Whether SMB opportunistic locking is enabled"`
	SMB2Leases           bool `json:"smb2_leases" jsonschema:"Whether SMB2 leasing is enabled"`
	DurableHandles       bool `json:"durable_handles" jsonschema:"Whether SMB durable handles are enabled"`
	LocalMasterBrowser   bool `json:"local_master_browser" jsonschema:"Whether the SMB server acts as a local master browser"`
}

// NFSProtocol is the highest NFS protocol level DSM is configured to serve.
type NFSProtocol string

const (
	NFSProtocol2   NFSProtocol = "nfs2"
	NFSProtocol3   NFSProtocol = "nfs3"
	NFSProtocol4   NFSProtocol = "nfs4"
	NFSProtocol4_1 NFSProtocol = "nfs4.1"
)

type NFSState struct {
	Enabled            bool          `json:"enabled" jsonschema:"Whether the global DSM NFS service is enabled"`
	MaximumProtocol    NFSProtocol   `json:"maximum_protocol" jsonschema:"Highest enabled NFS protocol"`
	SupportedProtocols []NFSProtocol `json:"supported_protocols" jsonschema:"NFS protocol levels advertised as supported by the DSM backend"`
	NFSv4Domain        string        `json:"nfsv4_domain,omitempty" jsonschema:"NFSv4 ID mapping domain when the advanced settings API is available"`
}

type FileServiceCapabilities struct {
	SMB FileServiceModuleCapabilities `json:"smb" jsonschema:"SMB operation availability"`
	NFS FileServiceModuleCapabilities `json:"nfs" jsonschema:"NFS operation availability"`
}

type FileServiceModuleCapabilities struct {
	Module      ModuleName `json:"module" jsonschema:"Stable Control Panel module name"`
	Read        bool       `json:"read" jsonschema:"Whether the module state can be read"`
	Set         bool       `json:"set" jsonschema:"Whether guarded base settings changes are available"`
	SetAdvanced bool       `json:"set_advanced,omitempty" jsonschema:"Whether guarded advanced settings changes are available"`
}

// FileServiceChangeRequest owns exactly one protocol's patch. Omitted pointer
// fields preserve their current DSM values.
type FileServiceChangeRequest struct {
	Protocol FileProtocol `json:"protocol" jsonschema:"File service to change: smb or nfs"`
	SMB      *SMBChange   `json:"smb,omitempty" jsonschema:"SMB patch when protocol is smb"`
	NFS      *NFSChange   `json:"nfs,omitempty" jsonschema:"NFS patch when protocol is nfs"`
}

type SMBChange struct {
	Enabled             *bool             `json:"enabled,omitempty" jsonschema:"Enable or disable SMB"`
	Workgroup           *string           `json:"workgroup,omitempty" jsonschema:"Set the SMB workgroup"`
	MinimumProtocol     *SMBProtocol      `json:"minimum_protocol,omitempty" jsonschema:"Set minimum SMB protocol"`
	MaximumProtocol     *SMBProtocol      `json:"maximum_protocol,omitempty" jsonschema:"Set maximum SMB protocol"`
	TransportEncryption *SMBPolicy        `json:"transport_encryption,omitempty" jsonschema:"Set SMB transport encryption policy"`
	ServerSigning       *SMBSigningPolicy `json:"server_signing,omitempty" jsonschema:"Set SMB server signing policy"`
	OpportunisticLocking *bool `json:"opportunistic_locking,omitempty" jsonschema:"Enable or disable SMB opportunistic locking"`
	SMB2Leases           *bool `json:"smb2_leases,omitempty" jsonschema:"Enable or disable SMB2 leasing"`
	DurableHandles       *bool `json:"durable_handles,omitempty" jsonschema:"Enable or disable SMB durable handles"`
	LocalMasterBrowser   *bool `json:"local_master_browser,omitempty" jsonschema:"Enable or disable acting as the local master browser"`
}

type NFSChange struct {
	Enabled         *bool        `json:"enabled,omitempty" jsonschema:"Enable or disable NFS"`
	MaximumProtocol *NFSProtocol `json:"maximum_protocol,omitempty" jsonschema:"Set highest enabled NFS protocol"`
	NFSv4Domain     *string      `json:"nfsv4_domain,omitempty" jsonschema:"Set or clear the NFSv4 ID mapping domain"`
}
