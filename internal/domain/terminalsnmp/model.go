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

// Capabilities reports which terminal-snmp reads dsmctl currently exposes.
// Terminal and SNMP are independent DSM API families with independent failure
// boundaries: one being absent reports (not supported) without disabling the
// other. Guarded writes are a deferred follow-on and are not represented here.
type Capabilities struct {
	Module       string `json:"module" jsonschema:"Stable module name: terminal-snmp"`
	TerminalRead bool   `json:"terminal_read" jsonschema:"Whether the Terminal (SSH/Telnet) state can be read"`
	SNMPRead     bool   `json:"snmp_read" jsonschema:"Whether the SNMP state can be read"`
}
