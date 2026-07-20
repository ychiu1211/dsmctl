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

// Connection is one active Drive client connection. Field names follow the
// Drive server's connection enumeration (handlers/connection/list.cpp);
// sessions are not attributed to an account name by the API.
type Connection struct {
	User         string `json:"user,omitempty" jsonschema:"Account name when reported; Drive's session enumeration usually omits it"`
	DeviceName   string `json:"device_name,omitempty" jsonschema:"Client device or computer name"`
	ClientType   string `json:"client_type,omitempty" jsonschema:"Client type as reported by Drive, for example a desktop, mobile, or ShareSync session"`
	Address      string `json:"address,omitempty" jsonschema:"Client IP address"`
	SessionID    string `json:"session_id,omitempty" jsonschema:"Drive client session identifier; the target for a guarded kick"`
	ClientID     string `json:"client_id,omitempty" jsonschema:"Drive client identifier"`
	Status       string `json:"status,omitempty" jsonschema:"Connection status as reported by Drive"`
	Version      string `json:"version,omitempty" jsonschema:"Client software version"`
	Location     string `json:"location,omitempty" jsonschema:"Client location as reported by Drive"`
	DeviceUUID   string `json:"device_uuid,omitempty" jsonschema:"Stable device identifier"`
	IsRelay      bool   `json:"is_relay,omitempty" jsonschema:"Whether the connection goes through a QuickConnect relay"`
	CanWipe      bool   `json:"can_wipe,omitempty" jsonschema:"Whether the client supports remote data wipe (not exposed by dsmctl)"`
	LoginUnix    int64  `json:"login_unix,omitempty" jsonschema:"Unix time the session logged in"`
	LastAuthUnix int64  `json:"last_auth_unix,omitempty" jsonschema:"Unix time of the last authentication"`
}

// ConnectionKick is the guarded disconnect intent for one client session.
type ConnectionKick struct {
	SessionID string `json:"session_id" jsonschema:"Drive client session identifier exactly as listed by the connections read"`
}

// Connections is a point-in-time view of active Drive client connections.
type Connections struct {
	Total       int          `json:"total" jsonschema:"Total connections reported by Drive; falls back to the item count when absent"`
	Connections []Connection `json:"connections" jsonschema:"Active Drive client connections"`
}

// TeamFolder is one shared folder as shown in the Admin Console team-folder
// view. Enabled reports whether the share is activated as a Drive team folder;
// Status carries Drive's own state vocabulary (for example "normal"). The
// versioning fields apply only to an enabled team folder: Drive reports them
// as the literal string "-" otherwise, surfaced here as absent.
type TeamFolder struct {
	Name    string `json:"name" jsonschema:"Shared folder name; Drive's home entry appears as homes/mydrive_home"`
	Enabled bool   `json:"enabled" jsonschema:"Whether the shared folder is enabled as a Drive team folder"`
	Status  string `json:"status,omitempty" jsonschema:"Share state as reported by Drive, lowercased, for example normal"`
	Type    string `json:"type,omitempty" jsonschema:"Share type as reported by Drive, for example normal or encryption"`
	// MaxVersions is Drive's kept-version count (0 = versioning off).
	MaxVersions *int `json:"max_versions,omitempty" jsonschema:"Versions Drive keeps per file (0 disables versioning); absent when the folder is not an enabled team folder"`
	// VersionPolicy is fifo (rotate earliest) or smart (Intelliversioning);
	// empty while versioning is off.
	VersionPolicy string `json:"version_policy,omitempty" jsonschema:"Version rotation policy: fifo or smart; absent while versioning is off"`
	// RetentionDays prunes versions older than this many days (0 = keep).
	RetentionDays *int `json:"retention_days,omitempty" jsonschema:"Days versions are retained (0 keeps them until rotated); absent when the folder is not an enabled team folder"`
}

// TeamFolders is the admin view of Drive team folders.
type TeamFolders struct {
	Total       int          `json:"total" jsonschema:"Total team folders reported by Drive; falls back to the item count when absent"`
	TeamFolders []TeamFolder `json:"team_folders" jsonschema:"Team folders reported by the Drive Admin Console"`
}

// NodeQuery selects one Drive view to browse: a team folder by shared-folder
// name, or the calling account's My Drive when TeamFolder is empty. The node
// view is Drive's admin/rescue perspective and includes removed entries by
// default.
type NodeQuery struct {
	TeamFolder     string `json:"team_folder,omitempty" jsonschema:"Team folder (shared-folder name) to browse; empty browses the calling account's My Drive"`
	Pattern        string `json:"pattern,omitempty" jsonschema:"Substring filter on the node name, applied by Drive"`
	Recursive      bool   `json:"recursive,omitempty" jsonschema:"Search the whole view instead of one directory level"`
	ExcludeRemoved bool   `json:"exclude_removed,omitempty" jsonschema:"Hide removed entries (they are included by default — this is the rescue view)"`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum nodes to return; defaults to a bounded page size"`
	Offset         int    `json:"offset,omitempty" jsonschema:"Nodes to skip for pagination"`
}

// Node is one file or folder in a Drive view, including removed entries.
type Node struct {
	Name          string `json:"name" jsonschema:"Node name"`
	Path          string `json:"path,omitempty" jsonschema:"Path inside the Drive view"`
	NodeID        string `json:"node_id,omitempty" jsonschema:"Drive node identifier"`
	SyncID        string `json:"sync_id,omitempty" jsonschema:"Drive sync identifier; the restore write uses it to identify the node"`
	IsFolder      bool   `json:"is_folder" jsonschema:"Whether the node is a folder"`
	IsRemoved     bool   `json:"is_removed" jsonschema:"Whether the node is deleted in the Drive view (restorable while versions remain)"`
	IsEncrypted   bool   `json:"is_encrypted,omitempty" jsonschema:"Whether the node is encrypted"`
	IsLocked      bool   `json:"is_locked,omitempty" jsonschema:"Whether the node is locked"`
	SizeBytes     int64  `json:"size_bytes,omitempty" jsonschema:"Current version size in bytes"`
	VersionCount  int    `json:"version_count,omitempty" jsonschema:"Stored version count"`
	ModifiedUnix  int64  `json:"modified_unix,omitempty" jsonschema:"Unix time of the last modification"`
	PermanentLink string `json:"permanent_link,omitempty" jsonschema:"Drive permanent link identifier"`
}

// Nodes is one page of a Drive view.
type Nodes struct {
	Total int    `json:"total" jsonschema:"Total nodes matching the query"`
	Items []Node `json:"items" jsonschema:"Nodes in the requested page"`
}

// NodeVersionQuery selects one node's version history.
type NodeVersionQuery struct {
	TeamFolder string `json:"team_folder,omitempty" jsonschema:"Team folder (shared-folder name); empty targets the calling account's My Drive"`
	Path       string `json:"path" jsonschema:"Node path inside the Drive view, as returned by the files read"`
}

// NodeVersion is one stored version of a node.
type NodeVersion struct {
	CreatedUnix    int64  `json:"created_unix,omitempty" jsonschema:"Unix time the version was stored"`
	ModifiedUnix   int64  `json:"modified_unix,omitempty" jsonschema:"Unix time of the content modification"`
	SizeBytes      int64  `json:"size_bytes,omitempty" jsonschema:"Version size in bytes"`
	Hash           string `json:"hash,omitempty" jsonschema:"Content hash Drive stores for the version"`
	VersionUpdater string `json:"version_updater,omitempty" jsonschema:"Client or host that stored the version"`
}

// NodeVersions is one node's version history from the Drive view.
type NodeVersions struct {
	Path           string        `json:"path" jsonschema:"Node path the history belongs to"`
	IsRemoved      bool          `json:"is_removed" jsonschema:"Whether the node is currently deleted in the Drive view"`
	RestoreBlocked bool          `json:"restore_blocked,omitempty" jsonschema:"Whether Drive reports restoring is disabled for this view"`
	PermanentLink  string        `json:"permanent_link,omitempty" jsonschema:"Drive permanent link identifier"`
	Versions       []NodeVersion `json:"versions" jsonschema:"Stored versions, as reported by Drive"`
}

// NodeRestore restores nodes in a Drive view to their latest stored version.
// The primary use is recovering removed (deleted) files and folders — the
// write half of the WI-057 rescue reads. Paths come from the files read;
// planning resolves each to a node in the view.
type NodeRestore struct {
	TeamFolder string   `json:"team_folder,omitempty" jsonschema:"Team folder (shared-folder name) to restore in; empty targets the signed-in account's My Drive"`
	Paths      []string `json:"paths" jsonschema:"Node paths to restore, exactly as listed by the files read"`
	// CopyTo restores the content into this folder path instead of in place;
	// empty restores each node at its original location.
	CopyTo string `json:"copy_to,omitempty" jsonschema:"Restore into this folder path instead of the original location; the account must have write access there"`
	// Overwrite replaces a currently-present node's content; it has no effect
	// on removed nodes. Defaults to true.
	Overwrite *bool `json:"overwrite,omitempty" jsonschema:"Overwrite a currently-present node when restoring in place; defaults to true, ignored for removed nodes"`
}

// NodeRestoreResult reports the outcome of a completed restore task.
type NodeRestoreResult struct {
	Backend  string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API      string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version  int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Restored int    `json:"restored" jsonschema:"Nodes the restore task processed, as reported by Drive"`
}

// PrivilegedUser is one account row from the Admin Console user view: the
// Drive privilege flag plus DSM account context. Status reflects the DSM
// account and home service (normal, disabled, or home_disabled), not the
// Drive privilege itself.
type PrivilegedUser struct {
	Name    string `json:"name" jsonschema:"Account name"`
	Enabled bool   `json:"enabled" jsonschema:"Whether the account may use Synology Drive"`
	Status  string `json:"status,omitempty" jsonschema:"DSM account context: normal, disabled (account deactivated or expired), or home_disabled"`
}

// PrivilegeList is one page of the Drive user-privilege view.
type PrivilegeList struct {
	Total int              `json:"total" jsonschema:"Total accounts in the queried realm"`
	Users []PrivilegedUser `json:"users" jsonschema:"Accounts with their Drive privilege state"`
}

// PrivilegeQuery selects the account realm to list.
type PrivilegeQuery struct {
	Type       string `json:"type,omitempty" jsonschema:"Account realm: local (default), domain, or ldap"`
	DomainName string `json:"domain_name,omitempty" jsonschema:"Domain to query when type is domain or ldap"`
}

// ConnectionSummary counts active Drive client connections by family, as
// shown on the Admin Console overview.
type ConnectionSummary struct {
	Desktop   int `json:"desktop" jsonschema:"Desktop sync clients (Drive, Drive Client backup, legacy Cloud Station)"`
	Mobile    int `json:"mobile" jsonschema:"Mobile clients (Drive mobile, DS cloud)"`
	ShareSync int `json:"sharesync" jsonschema:"Server-to-server sync connections (ShareSync)"`
	Total     int `json:"total" jsonschema:"All active connections"`
}

// DBUsage is Drive's cached database usage breakdown in bytes.
type DBUsage struct {
	RepositorySize int64 `json:"repository_size" jsonschema:"Version repository size in bytes"`
	DatabaseSize   int64 `json:"database_size" jsonschema:"Drive database size in bytes"`
	OfficeSize     int64 `json:"office_size" jsonschema:"Synology Office document size in bytes"`
	UpdatedUnix    int64 `json:"updated_unix,omitempty" jsonschema:"Unix time the cached usage was calculated"`
}

// TopAccessQuery selects the Admin Console top-accessed-files ranking.
type TopAccessQuery struct {
	RankingBy  string `json:"ranking_by,omitempty" jsonschema:"Ranking source: both (default), preview, or download"`
	PeriodDays int    `json:"period_days,omitempty" jsonschema:"Days of history to rank, defaults to 1"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum files to return; defaults to a bounded page size"`
	Offset     int    `json:"offset,omitempty" jsonschema:"Number of entries to skip for pagination"`
}

// TopAccessFile is one ranked entry. Drive reports rows from its access log
// aggregation; field presence varies, so entries are decoded leniently.
type TopAccessFile struct {
	Path        string `json:"path,omitempty" jsonschema:"File path when reported"`
	Name        string `json:"name,omitempty" jsonschema:"File name when reported"`
	AccessCount int    `json:"access_count,omitempty" jsonschema:"Aggregated access count when reported"`
}

// TopAccessFiles is the ranked list.
type TopAccessFiles struct {
	Files []TopAccessFile `json:"files" jsonschema:"Ranked files, most accessed first"`
}

// Activation reports Drive's package activation (the Admin Console's online
// registration against the NAS serial number). An unactivated Drive still
// serves clients; activation gates nothing dsmctl manages.
type Activation struct {
	Activated      bool   `json:"activated" jsonschema:"Whether the Drive package has been activated"`
	SerialNumber   string `json:"serial_number,omitempty" jsonschema:"NAS serial number the activation binds to"`
	ActivationUnix int64  `json:"activation_unix,omitempty" jsonschema:"Unix time of activation; 0 when not activated"`
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

// Team-folder change actions.
const (
	// TeamFolderActionEnable activates a shared folder as a Drive team folder.
	TeamFolderActionEnable = "enable"
	// TeamFolderActionDisable deactivates a team folder. Drive deletes its
	// team-folder database including version history; shared-folder files are
	// not touched.
	TeamFolderActionDisable = "disable"
	// TeamFolderActionSetVersioning patches versioning on an enabled team
	// folder. Omitted fields keep their current values (DSM merges them from
	// the stored view settings).
	TeamFolderActionSetVersioning = "set_versioning"
)

// TeamFolderChange is one guarded team-folder mutation. Enable requires
// MaxVersions because DSM refuses to enable a team folder without rotate_cnt,
// and an explicit VersionPolicy whenever versioning is on so the stored policy
// never depends on server-side defaults. SetVersioning is patch-only.
type TeamFolderChange struct {
	Action string `json:"action" jsonschema:"Team-folder action: enable, disable, or set_versioning"`
	Name   string `json:"name" jsonschema:"Shared-folder name exactly as listed in the team-folder view"`
	// MaxVersions is required for enable (0..32; 0 = versioning off).
	MaxVersions *int `json:"max_versions,omitempty" jsonschema:"Versions Drive keeps per file, 0..32; 0 disables versioning. Required for enable"`
	// VersionPolicy is required when MaxVersions > 0 on enable.
	VersionPolicy string `json:"version_policy,omitempty" jsonschema:"Version rotation policy: fifo (rotate earliest) or smart (Intelliversioning)"`
	// RetentionDays defaults to 0 (keep until rotated) on enable.
	RetentionDays *int `json:"retention_days,omitempty" jsonschema:"Days versions are retained, 0..120; 0 keeps them until rotated"`
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
type Capabilities struct {
	Module          string          `json:"module" jsonschema:"Stable module name: drive-admin"`
	Package         PackageEvidence `json:"package" jsonschema:"Installed SynologyDrive package evidence observed before selection"`
	StatusRead      bool            `json:"status_read" jsonschema:"Whether the Drive service status can be read"`
	ConnectionsRead bool            `json:"connections_read" jsonschema:"Whether active Drive client connections can be listed"`
	TeamFoldersRead bool            `json:"team_folders_read" jsonschema:"Whether team folders can be listed"`
	LogRead         bool            `json:"log_read" jsonschema:"Whether Drive server logs can be read"`
	TeamFoldersSet  bool            `json:"team_folders_set" jsonschema:"Whether guarded team-folder enable/disable and versioning changes are available"`
	ConfigRead      bool            `json:"config_read" jsonschema:"Whether the Drive server database configuration can be read"`
	ConfigSet       bool            `json:"config_set" jsonschema:"Whether guarded Drive server database configuration changes are available"`
	ConnectionSummaryRead bool      `json:"connection_summary_read" jsonschema:"Whether the connection-count summary can be read"`
	ConnectionsKick bool            `json:"connections_kick" jsonschema:"Whether a guarded client-session disconnect is available"`
	DBUsageRead     bool            `json:"db_usage_read" jsonschema:"Whether the cached database usage can be read"`
	DashboardRead   bool            `json:"dashboard_read" jsonschema:"Whether the top-accessed-files ranking can be read"`
	ActivationRead  bool            `json:"activation_read" jsonschema:"Whether the package activation state can be read"`
	PrivilegeRead   bool            `json:"privilege_read" jsonschema:"Whether the per-user Drive privilege view can be listed; granting or revoking access goes through the account module's application privilege"`
	NodesRead       bool            `json:"nodes_read" jsonschema:"Whether Drive views (team folders and My Drive, including removed entries) can be browsed"`
	NodeVersionsRead bool           `json:"node_versions_read" jsonschema:"Whether a node's stored version history can be listed"`
	NodeRestore     bool            `json:"node_restore" jsonschema:"Whether a guarded restore of removed nodes is available"`
	LogExport       bool            `json:"log_export" jsonschema:"Whether the Drive server log can be exported to a file"`
}
