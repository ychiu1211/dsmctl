// Package driveadmin contains stable, package-version-independent models for
// the Synology Drive Server Admin Console: service status, active client
// connections, team folders, and Drive server logs. DSM WebAPI names, versions,
// and field names stay behind the operation package, and the installed
// SynologyDrive package version is carried as evidence because Drive's WebAPI
// behavior follows the package release rather than the DSM release.
package driveadmin

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "drive-admin"

// PackageEvidence reports the installed SynologyDrive package observed through
// the Package Center inventory immediately before operations were selected.
type PackageEvidence struct {
	ID        string `json:"id" jsonschema:"Stable DSM package identifier: SynologyDrive"`
	Installed bool   `json:"installed" jsonschema:"Whether the Synology Drive Server package is installed"`
	Version   string `json:"version,omitempty" jsonschema:"Installed package version observed before selection"`
	Running   bool   `json:"running" jsonschema:"Whether the package service was running when observed"`
}

// ServiceStatus is the normalized Drive service state from the Admin Console
// overview. DSM's status vocabulary varies across package versions, so the
// reported value is surfaced lowercased rather than remapped.
type ServiceStatus struct {
	Status  string          `json:"status" jsonschema:"Drive service status as reported by the package, lowercased"`
	Package PackageEvidence `json:"package" jsonschema:"Installed package evidence observed with this read"`
}

// Connection is one active Drive client connection.
type Connection struct {
	User       string `json:"user,omitempty" jsonschema:"Account name of the connected user"`
	DeviceName string `json:"device_name,omitempty" jsonschema:"Client device or computer name"`
	ClientType string `json:"client_type,omitempty" jsonschema:"Client type as reported by Drive, for example a desktop, mobile, or web session"`
	Address    string `json:"address,omitempty" jsonschema:"Client IP address"`
}

// Connections is a point-in-time view of active Drive client connections.
type Connections struct {
	Total       int          `json:"total" jsonschema:"Total connections reported by Drive; falls back to the item count when absent"`
	Connections []Connection `json:"connections" jsonschema:"Active Drive client connections"`
}

// TeamFolder is one shared folder as shown in the Admin Console team-folder
// view. Enabled reports whether the share is activated as a Drive team folder;
// Status carries Drive's own state vocabulary (for example "normal").
type TeamFolder struct {
	Name    string `json:"name" jsonschema:"Shared folder name; Drive's home entry appears as homes/mydrive_home"`
	Enabled bool   `json:"enabled" jsonschema:"Whether the shared folder is enabled as a Drive team folder"`
	Status  string `json:"status,omitempty" jsonschema:"Share state as reported by Drive, lowercased, for example normal"`
}

// TeamFolders is the admin view of Drive team folders.
type TeamFolders struct {
	Total       int          `json:"total" jsonschema:"Total team folders reported by Drive; falls back to the item count when absent"`
	TeamFolders []TeamFolder `json:"team_folders" jsonschema:"Team folders reported by the Drive Admin Console"`
}

// LogQuery selects and pages Drive server log entries. All filters are applied
// by the Drive package. TeamFolder narrows the scope to one Drive team folder;
// when empty, logs from every scope are returned.
type LogQuery struct {
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum entries to return; defaults to a bounded page size"`
	Offset     int    `json:"offset,omitempty" jsonschema:"Number of newest entries to skip for pagination"`
	Keyword    string `json:"keyword,omitempty" jsonschema:"Substring filter applied by Drive"`
	Username   string `json:"username,omitempty" jsonschema:"Filter to one account name"`
	TeamFolder string `json:"team_folder,omitempty" jsonschema:"Filter to one Drive team folder by shared-folder name"`
	From       int64  `json:"from,omitempty" jsonschema:"Inclusive lower bound as a Unix time in seconds"`
	To         int64  `json:"to,omitempty" jsonschema:"Inclusive upper bound as a Unix time in seconds"`
}

// LogEntry is one Drive server log record. Drive encodes log text as an event
// code plus substitution parameters rather than a rendered description, so the
// structured fields are surfaced directly.
type LogEntry struct {
	TimeUnix   int64  `json:"time_unix,omitempty" jsonschema:"Event time as a Unix time in seconds"`
	Username   string `json:"username,omitempty" jsonschema:"Account that performed the action; empty for system events"`
	ClientType string `json:"client_type,omitempty" jsonschema:"Originating client as reported by Drive, for example web_portal"`
	IPAddress  string `json:"ip_address,omitempty" jsonschema:"Client IP address when reported"`
	EventType  int    `json:"event_type" jsonschema:"Drive's numeric event code for this entry"`
	Path       string `json:"path,omitempty" jsonschema:"File or folder path the event applied to, when reported"`
	TeamFolder string `json:"team_folder,omitempty" jsonschema:"Team folder the event belongs to; empty for My Drive events"`
}

// Log is a page of Drive server log entries.
type Log struct {
	Total   int        `json:"total" jsonschema:"Total entries matching the query before pagination; falls back to the item count when absent"`
	Entries []LogEntry `json:"entries" jsonschema:"Drive log entries for the requested page"`
}

// ServerConfig is the normalized Drive server database configuration from the
// Admin Console (SYNO.SynologyDrive.Config). VolumePath is read-only: DSM changes
// it by physically moving the Drive database between volumes, which is out of
// scope for a guarded settings write.
type ServerConfig struct {
	VolumePath        string          `json:"volume_path" jsonschema:"Volume holding the Drive database (read-only)"`
	VMTouchEnabled    bool            `json:"vmtouch_enabled" jsonschema:"Whether the Drive database is pinned in memory (vmtouch)"`
	VMTouchReserveMem int             `json:"vmtouch_reserve_mem" jsonschema:"Memory reserved for the pinned database, in MB"`
	Package           PackageEvidence `json:"package" jsonschema:"Installed SynologyDrive package evidence observed with this read"`
}

// ServerConfigChange patches the Drive server database configuration. The
// vmtouch enable flag and its reserved memory are a coupled pair; the facade
// submits both, merged from the current configuration. VolumePath is not
// writable.
type ServerConfigChange struct {
	VMTouchEnabled    *bool `json:"vmtouch_enabled,omitempty" jsonschema:"Enable or disable pinning the Drive database in memory"`
	VMTouchReserveMem *int  `json:"vmtouch_reserve_mem,omitempty" jsonschema:"Memory reserved for the pinned database, in MB"`
}

// Capabilities reports which Drive Admin operations dsmctl currently exposes
// for the selected backends, plus the package evidence the selection used.
// TeamFoldersSet is modeled but fails closed in this slice.
type Capabilities struct {
	Module          string          `json:"module" jsonschema:"Stable module name: drive-admin"`
	Package         PackageEvidence `json:"package" jsonschema:"Installed SynologyDrive package evidence observed before selection"`
	StatusRead      bool            `json:"status_read" jsonschema:"Whether the Drive service status can be read"`
	ConnectionsRead bool            `json:"connections_read" jsonschema:"Whether active Drive client connections can be listed"`
	TeamFoldersRead bool            `json:"team_folders_read" jsonschema:"Whether team folders can be listed"`
	LogRead         bool            `json:"log_read" jsonschema:"Whether Drive server logs can be read"`
	TeamFoldersSet  bool            `json:"team_folders_set" jsonschema:"Whether guarded team-folder changes are available; deferred, currently always false"`
	ConfigRead      bool            `json:"config_read" jsonschema:"Whether the Drive server database configuration can be read"`
	ConfigSet       bool            `json:"config_set" jsonschema:"Whether guarded Drive server database configuration changes are available"`
}
