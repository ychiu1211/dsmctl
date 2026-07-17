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

// TeamFolder is one Drive team folder as shown in the Admin Console.
type TeamFolder struct {
	ID     string `json:"id,omitempty" jsonschema:"Stable Drive identifier for the team folder when reported"`
	Name   string `json:"name" jsonschema:"Team folder (shared folder) name"`
	Status string `json:"status,omitempty" jsonschema:"Team folder state as reported by Drive, lowercased, for example enabled or disabled"`
}

// TeamFolders is the admin view of Drive team folders.
type TeamFolders struct {
	Total       int          `json:"total" jsonschema:"Total team folders reported by Drive; falls back to the item count when absent"`
	TeamFolders []TeamFolder `json:"team_folders" jsonschema:"Team folders reported by the Drive Admin Console"`
}

// LogQuery selects and pages Drive server log entries. All filters are applied
// by the Drive package. Offset paging is not exposed because the verified Drive
// log API pages by limit only in this slice.
type LogQuery struct {
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum entries to return; defaults to a bounded page size"`
	Keyword  string `json:"keyword,omitempty" jsonschema:"Substring filter applied by Drive"`
	Username string `json:"username,omitempty" jsonschema:"Filter to one account name"`
	Target   string `json:"target,omitempty" jsonschema:"Filter to one file or folder path"`
	From     int64  `json:"from,omitempty" jsonschema:"Inclusive lower bound as a Unix time in seconds"`
	To       int64  `json:"to,omitempty" jsonschema:"Inclusive upper bound as a Unix time in seconds"`
}

// LogEntry is one Drive server log record.
type LogEntry struct {
	Time        string `json:"time,omitempty" jsonschema:"Timestamp as reported by Drive"`
	Username    string `json:"username,omitempty" jsonschema:"Account that performed the action"`
	Action      string `json:"action,omitempty" jsonschema:"Drive action or event type as reported"`
	Target      string `json:"target,omitempty" jsonschema:"File or folder the action applied to"`
	Description string `json:"description,omitempty" jsonschema:"Human-readable log description"`
}

// Log is a page of Drive server log entries.
type Log struct {
	Total   int        `json:"total" jsonschema:"Total entries matching the query before pagination; falls back to the item count when absent"`
	Entries []LogEntry `json:"entries" jsonschema:"Drive log entries for the requested page"`
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
}
