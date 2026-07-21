// Package dsmupdate contains stable, read-only models for the DSM Control
// Panel → Update & Restore surface: the installed DSM version and local update
// state, the update-server's offered-update check, the DSM auto-update policy,
// and the configuration-backup status/history.
//
// Each area owns a separate state type and a separate DSM API family, so one
// area being unavailable (a missing config-backup API, an unreachable update
// server) never disables the others.
//
// These models are deliberately free of any authentication material. The
// configuration-backup destination password (SYNO.Backup.Config.AutoBackup's
// "pwd") is never decoded, so a display or MCP path cannot leak it. Installing
// a DSM update and restoring a configuration backup are out of scope for this
// read module (see WI-074): both reboot or overwrite the whole system and are
// deferred with reason, so no download/install/restore state is modeled here.
package dsmupdate

// UpdateStatus is the local DSM update state plus the installed version. It is
// side-effect-free: it reports what the NAS already knows without contacting
// the update server.
type UpdateStatus struct {
	InstalledVersion string `json:"installed_version,omitempty" jsonschema:"Installed DSM version/build as DSM reports it, such as DSM 7.3.2-86009; empty when the release could not be discovered"`
	AllowUpgrade     bool   `json:"allow_upgrade" jsonschema:"Whether DSM currently permits an upgrade on this NAS"`
	State            string `json:"state" jsonschema:"Local update state DSM reports: none (no update pending), available, downloading, downloaded (install-ready), or installing"`
}

// AvailableUpdate is the update-server offered-update check. Because the check
// is a network egress to Synology's update server, an unreachable server is
// reported as Checked=false rather than erroring the whole module.
//
// When an update is available DSM returns the offered version and its
// restart/criticality flags inside a nested object; those detail fields were
// not observable on the lab (no update was pending) and are surfaced verbatim
// under Details by their DSM key rather than through a guessed typed decoder,
// so nothing is fabricated and nothing is dropped.
type AvailableUpdate struct {
	Checked   bool              `json:"checked" jsonschema:"Whether the update-server check completed; false when the update server was unreachable (treat availability as unknown)"`
	Available bool              `json:"available" jsonschema:"Whether the update server offers an update for this model; only meaningful when checked is true"`
	RSSResult string            `json:"rss_result,omitempty" jsonschema:"Update-server feed result reported by DSM, such as success"`
	Details   map[string]string `json:"details,omitempty" jsonschema:"Additional offered-update fields DSM returned (such as the offered version, restart-required flag, and criticality/type) under their raw DSM keys; present only when an update is available"`
}

// PolicySchedule is the DSM auto-update maintenance window.
type PolicySchedule struct {
	Hour    int    `json:"hour" jsonschema:"Scheduled hour of the auto-update maintenance window, 0-23"`
	Minute  int    `json:"minute" jsonschema:"Scheduled minute of the auto-update maintenance window, 0-59"`
	WeekDay string `json:"week_day,omitempty" jsonschema:"Scheduled day-of-week selector DSM reports, such as 0 (Sunday) or a comma-separated list; empty means every day"`
}

// AutoUpdatePolicy is the DSM auto-update policy (the firmware analog of the
// Package Center auto-update policy). Fields DSM does not report on a given API
// version are left nil/empty rather than defaulted, so "off" is never confused
// with "not reported by this DSM version".
type AutoUpdatePolicy struct {
	AutoUpdateEnabled *bool           `json:"auto_update_enabled,omitempty" jsonschema:"Whether automatic DSM update install is enabled; null when this DSM version does not report the field"`
	AutoUpdateType    string          `json:"auto_update_type,omitempty" jsonschema:"Which updates are auto-installed, such as hotfix-security; empty when not reported"`
	AutoDownload      *bool           `json:"auto_download,omitempty" jsonschema:"Whether updates are downloaded automatically (older DSM download-only policy); null when not reported"`
	UpgradeType       string          `json:"upgrade_type,omitempty" jsonschema:"Which update channel DSM checks, such as hotfix; empty when not reported"`
	SmartNanoEnabled  *bool           `json:"smart_nano_enabled,omitempty" jsonschema:"Whether small (nano) updates are handled automatically; null when not reported"`
	Schedule          *PolicySchedule `json:"schedule,omitempty" jsonschema:"Auto-update maintenance window; null when this DSM version does not report a schedule"`
}

// ConfigBackupVersion is one stored configuration-backup version (the history
// the config-backup API exposes). It carries no destination credential.
type ConfigBackupVersion struct {
	BackupTime string `json:"backup_time,omitempty" jsonschema:"When the configuration backup was taken, as DSM reports it"`
	DSMVersion string `json:"dsm_version,omitempty" jsonschema:"DSM version the configuration backup was taken from"`
	Host       string `json:"host,omitempty" jsonschema:"Host name recorded on the backup"`
	Model      string `json:"model,omitempty" jsonschema:"NAS model recorded on the backup"`
	Serial     string `json:"serial,omitempty" jsonschema:"NAS serial recorded on the backup"`
}

// ConfigBackup is the normalized configuration-backup state: whether the
// scheduled backup to the Synology account is enabled, its destination account
// and encryption mode, the last-backup result, and the stored backup history.
//
// The destination account password is never decoded. The account identifier is
// surfaced (like an SMTP auth user) so an operator can see where the
// configuration is backed up, but no token or password is ever read.
type ConfigBackup struct {
	Enabled          bool                  `json:"enabled" jsonschema:"Whether scheduled configuration backup to the Synology account is enabled"`
	Account          string                `json:"account,omitempty" jsonschema:"Synology account the configuration is backed up to; the account password is never read"`
	EncryptionMethod string                `json:"encryption_method,omitempty" jsonschema:"How the configuration backup is encrypted, as DSM reports it, such as manual"`
	LastStatus       string                `json:"last_status,omitempty" jsonschema:"Result of the most recent configuration backup, such as none or success"`
	Versions         []ConfigBackupVersion `json:"versions" jsonschema:"Stored configuration-backup history; empty when none or when the history could not be read"`
}

// Capabilities reports which Update & Restore read areas are currently exposed
// for a NAS. Each is independent: a NAS may expose the update status and policy
// while the configuration-backup API is absent.
type Capabilities struct {
	Status       bool `json:"status" jsonschema:"Whether the local DSM update status can be read"`
	Available    bool `json:"available" jsonschema:"Whether the update-server offered-update check is available"`
	Policy       bool `json:"policy" jsonschema:"Whether the DSM auto-update policy can be read"`
	ConfigBackup bool `json:"config_backup" jsonschema:"Whether the configuration-backup status can be read"`
}
