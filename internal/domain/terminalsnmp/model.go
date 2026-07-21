// Package terminalsnmp contains stable, DSM-version-independent models for the
// Control Panel → Terminal & SNMP surface: whether SSH and Telnet are enabled
// (and on which SSH port), and the SNMP service state. WebAPI names and field
// names stay behind the operation package.
//
// The SNMP model carries non-secret configuration only. The v1/v2c community
// string, the SNMPv3 auth/privacy passwords, and any trap community are secrets
// under the secrets-and-identity contract: they are never decoded into these
// types, never returned by a read, and only ever supplied to a future guarded
// write through a credential reference. Reads surface presence flags
// (community_configured) and non-secret identity (v3_user), never the bytes.
package terminalsnmp

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "terminal-snmp"

// TerminalState is the normalized Control Panel → Terminal tab configuration:
// the SSH and Telnet service switches and the SSH listening port. dsmctl drives
// DSM over the WebAPI session, not SSH, so these describe the human remote-shell
// exposure only.
type TerminalState struct {
	SSHEnabled       bool `json:"ssh_enabled" jsonschema:"Whether the SSH service is enabled"`
	SSHPort          int  `json:"ssh_port" jsonschema:"TCP port the SSH service listens on"`
	TelnetEnabled    bool `json:"telnet_enabled" jsonschema:"Whether the Telnet service is enabled (Telnet is unauthenticated cleartext and deprecated)"`
	ConsoleForbidden bool `json:"console_forbidden" jsonschema:"Whether local console access is forbidden (the Terminal tab 'forbid console' switch)"`
}

// SNMPState is the normalized Control Panel → SNMP tab configuration. It reports
// the service switch, which protocol versions are enabled, the device
// location/contact, and whether a read community, an SNMPv3 user, and a trap
// target are configured — never the community string, the SNMPv3 passwords, or
// any trap community.
type SNMPState struct {
	Enabled             bool   `json:"enabled" jsonschema:"Whether the SNMP service is enabled"`
	V1V2cEnabled        bool   `json:"v1_v2c_enabled" jsonschema:"Whether SNMPv1/v2c is enabled"`
	V3Enabled           bool   `json:"v3_enabled" jsonschema:"Whether SNMPv3 is enabled"`
	Location            string `json:"location,omitempty" jsonschema:"Device location string (sysLocation); non-secret"`
	Contact             string `json:"contact,omitempty" jsonschema:"Device contact string (sysContact); non-secret"`
	CommunityConfigured bool   `json:"community_configured" jsonschema:"Whether a read-only community string is set. The community string itself is a secret and is never decoded or returned"`
	V3User              string `json:"v3_user,omitempty" jsonschema:"SNMPv3 read-only username (non-secret identity). The v3 auth/privacy passwords are secrets and are never decoded or returned"`
	TrapConfigured      bool   `json:"trap_configured" jsonschema:"Whether an SNMP trap target is configured. The trap community, if any, is a secret and is never decoded or returned"`
	TrapHostPresent     bool   `json:"trap_host_present" jsonschema:"Whether a trap destination host is set"`
}

// Capabilities reports which terminal-snmp reads and guarded writes dsmctl
// currently exposes. Terminal and SNMP are independent DSM API families with
// independent failure boundaries: one being absent reports (not supported)
// without disabling the other.
type Capabilities struct {
	Module        string `json:"module" jsonschema:"Stable module name: terminal-snmp"`
	TerminalRead  bool   `json:"terminal_read" jsonschema:"Whether the Terminal (SSH/Telnet) state can be read"`
	SNMPRead      bool   `json:"snmp_read" jsonschema:"Whether the SNMP state can be read"`
	TerminalWrite bool   `json:"terminal_write" jsonschema:"Whether the guarded Terminal (SSH enable/port/Telnet/console) write is available"`
	SNMPWrite     bool   `json:"snmp_write" jsonschema:"Whether the guarded SNMP write (service/versions/location/contact/community) is available"`
}

// TerminalChange is the patch-only intent for the guarded Terminal write. A nil
// field keeps the currently configured DSM value; the apply path reads the
// complete current Terminal state, merges this patch, and submits the whole
// merged configuration so an unspecified switch is never silently reset.
//
// dsmctl drives DSM over the WebAPI session (HTTPS), not SSH, so its own
// connectivity survives any change here — but a change to the human remote-shell
// exposure is exactly what these fields control, which is why every enabling
// change is high risk (see the write layer).
type TerminalChange struct {
	SSHEnabled       *bool `json:"ssh_enabled,omitempty" jsonschema:"Whether SSH is enabled; omit to keep the current setting. Enabling opens a remote shell (high risk)"`
	SSHPort          *int  `json:"ssh_port,omitempty" jsonschema:"TCP port SSH listens on; omit to keep the current value. Changing it can break a firewall rule or port forward pinned to the old port"`
	TelnetEnabled    *bool `json:"telnet_enabled,omitempty" jsonschema:"Whether Telnet is enabled; omit to keep the current setting. Telnet is unauthenticated cleartext and deprecated — enabling it is high risk"`
	ConsoleForbidden *bool `json:"console_forbidden,omitempty" jsonschema:"Whether local console access is forbidden; omit to keep the current setting"`
}

// IsEmpty reports whether the patch carries no fields.
func (c TerminalChange) IsEmpty() bool {
	return c.SSHEnabled == nil && c.SSHPort == nil && c.TelnetEnabled == nil && c.ConsoleForbidden == nil
}

// SNMPChange is the patch-only intent for the guarded SNMP write. A nil field
// keeps the current DSM value; the apply path merges this patch into the freshly
// read (non-secret) state and submits the whole configuration.
//
// SECRET: the read community string is supplied by CommunityCredentialRef as an
// env:NAME reference, resolved to bytes ONLY at apply time and sent solely in the
// SNMP set request body. The reference name (never the secret value) is all that
// enters the plan, the approval hash, the result, or a log line. Omit the
// reference to leave the currently configured community unchanged.
//
// WIRE-UNVERIFIED (not writable through this module): enabling SNMPv3 (which DSM
// rejects without a v3 auth passphrase whose set-field names could not be
// confirmed live), the SNMPv3 auth/privacy passwords, and the trap target. The
// v3 auth/privacy password set-field names returned DSM error 2202 for every
// candidate, and no trap field appears in the SNMP get response even while the
// service is enabled. V3Enabled therefore accepts only false (disable).
type SNMPChange struct {
	Enabled                *bool   `json:"enabled,omitempty" jsonschema:"Whether the SNMP service is enabled; omit to keep the current setting"`
	V1V2cEnabled           *bool   `json:"v1_v2c_enabled,omitempty" jsonschema:"Whether SNMPv1/v2c is enabled; omit to keep the current setting. Enabling requires a community (community_credential_ref) if none is configured"`
	V3Enabled              *bool   `json:"v3_enabled,omitempty" jsonschema:"Whether SNMPv3 is enabled; only false (disable) is supported — enabling v3 needs an auth passphrase whose DSM write wire is unverified"`
	Location               *string `json:"location,omitempty" jsonschema:"Device location string (sysLocation); non-secret. Omit to keep the current value. Clearing to empty takes effect only while SNMP is enabled"`
	Contact                *string `json:"contact,omitempty" jsonschema:"Device contact string (sysContact); non-secret. Omit to keep the current value"`
	CommunityCredentialRef string  `json:"community_credential_ref,omitempty" jsonschema:"env:NAME reference to the SNMP read community (SECRET); resolved only at apply time and sent only in the SNMP set request. Omit to keep the current community"`
}

// IsEmpty reports whether the patch carries no fields.
func (c SNMPChange) IsEmpty() bool {
	return c.Enabled == nil && c.V1V2cEnabled == nil && c.V3Enabled == nil &&
		c.Location == nil && c.Contact == nil && c.CommunityCredentialRef == ""
}
