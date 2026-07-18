// Package rsyncservice models the DSM rsync network-backup service
// independently of DSM request field names. It is the "rsync" tab under DSM's
// File Services page, backed by SYNO.Backup.Service.NetworkBackup.
package rsyncservice

// State is the observed rsync-service configuration. SSHPort is the
// rsync-over-SSH port, which DSM shares with the SSH daemon; it is reported
// read-only and never written by this module.
type State struct {
	Enabled      bool `json:"enabled" jsonschema:"Whether the rsync network-backup service is enabled"`
	RsyncAccount bool `json:"rsync_account" jsonschema:"Whether the dedicated rsync account is enabled"`
	SSHPort      int  `json:"ssh_port" jsonschema:"rsync-over-SSH port (shared with the SSH service; read-only)"`
}

// Capabilities reports which rsync-service operations the selected DSM backend
// exposes.
type Capabilities struct {
	Read bool `json:"read" jsonschema:"Whether the rsync-service state can be read"`
	Set  bool `json:"set" jsonschema:"Whether the rsync-service switches can be changed through guarded plan/apply"`
}

// Change is a patch: an omitted (nil) field preserves its current DSM value.
// The rsync-over-SSH port is intentionally not writable here because DSM shares
// it with the SSH daemon.
type Change struct {
	Enabled      *bool `json:"enabled,omitempty" jsonschema:"Enable or disable the rsync network-backup service"`
	RsyncAccount *bool `json:"rsync_account,omitempty" jsonschema:"Enable or disable the dedicated rsync account"`
}
