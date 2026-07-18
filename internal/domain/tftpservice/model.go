// Package tftpservice models the DSM TFTP service independently of DSM request
// field names. It is the TFTP surface under DSM's File Services page, backed by
// SYNO.Core.TFTP.
package tftpservice

// Permission is the TFTP access level. DSM stores it as "r" (read only) or "rw"
// (read and write); this package uses semantic names.
type Permission string

const (
	PermissionReadOnly  Permission = "read_only"
	PermissionReadWrite Permission = "read_write"
)

// State is the observed TFTP configuration. ClientIPLow and ClientIPHigh bound
// the allowed-client IP range; they are reported read-only because those bounds
// interact with an "allow all" flag, so writing them is deferred.
type State struct {
	Enabled      bool       `json:"enabled" jsonschema:"Whether the TFTP service is enabled"`
	RootPath     string     `json:"root_path" jsonschema:"TFTP root folder path (a shared-folder path)"`
	Permission   Permission `json:"permission" jsonschema:"TFTP access level: read_only or read_write"`
	LogEnabled   bool       `json:"log_enabled" jsonschema:"Whether TFTP transfer logging is enabled"`
	ClientIPLow  string     `json:"client_ip_low,omitempty" jsonschema:"Allowed-client IP range lower bound (read-only)"`
	ClientIPHigh string     `json:"client_ip_high,omitempty" jsonschema:"Allowed-client IP range higher bound (read-only)"`
	Timeout      int        `json:"timeout" jsonschema:"TFTP link timeout in seconds"`
}

// Capabilities reports which TFTP operations the selected DSM backend exposes.
type Capabilities struct {
	Read bool `json:"read" jsonschema:"Whether the TFTP state can be read"`
	Set  bool `json:"set" jsonschema:"Whether the TFTP settings can be changed through guarded plan/apply"`
}

// Change is a patch: an omitted (nil) field preserves its current DSM value.
type Change struct {
	Enabled    *bool       `json:"enabled,omitempty" jsonschema:"Enable or disable the TFTP service"`
	RootPath   *string     `json:"root_path,omitempty" jsonschema:"TFTP root folder path (must be an existing shared-folder path)"`
	Permission *Permission `json:"permission,omitempty" jsonschema:"Set the TFTP access level: read_only or read_write"`
	LogEnabled *bool       `json:"log_enabled,omitempty" jsonschema:"Enable or disable TFTP transfer logging"`
	Timeout    *int        `json:"timeout,omitempty" jsonschema:"TFTP link timeout in seconds (1-3600)"`
}
