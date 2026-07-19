// Package downloadstation contains stable, package-version-independent models
// for the Synology Download Station package: service configuration, the download
// task list, and transfer statistics. DSM WebAPI names, versions, and field
// names stay behind the operation package, and the installed DownloadStation
// package version is carried as evidence because Download Station's WebAPI
// follows the package release.
package downloadstation

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "download"

// PackageEvidence reports the installed DownloadStation package.
type PackageEvidence struct {
	ID        string `json:"id" jsonschema:"DSM package identifier: DownloadStation"`
	Installed bool   `json:"installed" jsonschema:"Whether the Download Station package is installed"`
	Version   string `json:"version,omitempty" jsonschema:"Installed package version"`
	Running   bool   `json:"running" jsonschema:"Whether the Download Station service is running"`
}

// Config is the global Download Station configuration. Rate limits are in
// kilobytes per second; 0 means unlimited.
type Config struct {
	DefaultDestination  string `json:"default_destination,omitempty" jsonschema:"Default download destination shared folder; empty when unset"`
	EmuleEnabled        bool   `json:"emule_enabled" jsonschema:"Whether eMule is enabled"`
	UnzipServiceEnabled bool   `json:"unzip_service_enabled" jsonschema:"Whether the auto-unzip service is enabled"`
	BTMaxDownloadKBs    int    `json:"bt_max_download_kbs" jsonschema:"BitTorrent maximum download rate in KB/s; 0 = unlimited"`
	BTMaxUploadKBs      int    `json:"bt_max_upload_kbs" jsonschema:"BitTorrent maximum upload rate in KB/s; 0 = unlimited"`
	EmuleMaxDownloadKBs int    `json:"emule_max_download_kbs" jsonschema:"eMule maximum download rate in KB/s; 0 = unlimited"`
	EmuleMaxUploadKBs   int    `json:"emule_max_upload_kbs" jsonschema:"eMule maximum upload rate in KB/s; 0 = unlimited"`
	FTPMaxDownloadKBs   int    `json:"ftp_max_download_kbs" jsonschema:"FTP maximum download rate in KB/s; 0 = unlimited"`
	HTTPMaxDownloadKBs  int    `json:"http_max_download_kbs" jsonschema:"HTTP maximum download rate in KB/s; 0 = unlimited"`
	NZBMaxDownloadKBs   int    `json:"nzb_max_download_kbs" jsonschema:"NZB maximum download rate in KB/s; 0 = unlimited"`
}

// Schedule is the Download Station bandwidth-schedule switch.
type Schedule struct {
	Enabled      bool `json:"enabled" jsonschema:"Whether the download schedule is enabled"`
	EmuleEnabled bool `json:"emule_enabled" jsonschema:"Whether the eMule schedule is enabled"`
}

// ServiceState is the normalized Download Station service configuration: the
// version and manager flag, the global config, and the schedule switch.
type ServiceState struct {
	Version   string          `json:"version,omitempty" jsonschema:"Download Station version reported by the service"`
	IsManager bool            `json:"is_manager" jsonschema:"Whether the current account is a Download Station manager"`
	Config    Config          `json:"config" jsonschema:"Global Download Station configuration"`
	Schedule  Schedule        `json:"schedule" jsonschema:"Bandwidth-schedule switches"`
	Package   PackageEvidence `json:"package" jsonschema:"Installed Download Station package evidence"`
}

// TaskTransfer is one task's live transfer progress.
type TaskTransfer struct {
	SizeDownloaded int64 `json:"size_downloaded" jsonschema:"Bytes downloaded so far"`
	SizeUploaded   int64 `json:"size_uploaded" jsonschema:"Bytes uploaded so far"`
	SpeedDownload  int   `json:"speed_download" jsonschema:"Current download speed in bytes/s"`
	SpeedUpload    int   `json:"speed_upload" jsonschema:"Current upload speed in bytes/s"`
}

// Task is one download task. The shape is live-verified on Download Station
// 4.1.2; unknown extra fields are ignored so the model tolerates version drift.
type Task struct {
	ID          string       `json:"id,omitempty" jsonschema:"Task identifier"`
	Type        string       `json:"type,omitempty" jsonschema:"Download protocol: bt, http, ftp, emule, or nzb"`
	Username    string       `json:"username,omitempty" jsonschema:"Owner of the task"`
	Title       string       `json:"title,omitempty" jsonschema:"Task title"`
	Size        int64        `json:"size" jsonschema:"Total size in bytes"`
	Status      string       `json:"status,omitempty" jsonschema:"Task status, such as waiting, downloading, paused, finished, or error"`
	Destination string       `json:"destination,omitempty" jsonschema:"Download destination, when reported"`
	URI         string       `json:"uri,omitempty" jsonschema:"Source URI (URL or magnet), when reported"`
	CreateTime  int64        `json:"create_time,omitempty" jsonschema:"Task creation time as a Unix timestamp, when reported"`
	Transfer    TaskTransfer `json:"transfer" jsonschema:"Live transfer progress"`
}

// TaskAction is a guarded download-task mutation.
type TaskAction string

const (
	TaskActionCreate TaskAction = "create"
	TaskActionPause  TaskAction = "pause"
	TaskActionResume TaskAction = "resume"
	TaskActionDelete TaskAction = "delete"
)

// TaskChange is the intent for a guarded task mutation. Exactly one action is
// performed. Create uses URIs (+ optional Destination); pause/resume/delete use
// TaskIDs. Passwords for a protected source are supplied via a credential
// reference, never inline (create only).
type TaskChange struct {
	Action        TaskAction `json:"action" jsonschema:"Task action: create, pause, resume, or delete"`
	URIs          []string   `json:"uris,omitempty" jsonschema:"Source URIs (HTTP/HTTPS/FTP URL or magnet) for create"`
	Destination   string     `json:"destination,omitempty" jsonschema:"Destination shared folder or path for create; omit to use the DSM default"`
	TaskIDs       []string   `json:"task_ids,omitempty" jsonschema:"Target task identifiers for pause, resume, or delete"`
	ForceComplete bool       `json:"force_complete,omitempty" jsonschema:"On delete, mark a moving/finishing task complete instead of removing partial data"`
}

// TaskMutationResult records the DSM backend and the affected task identifiers.
type TaskMutationResult struct {
	Backend     string   `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API         string   `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version     int      `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method      string   `json:"method" jsonschema:"DSM WebAPI method used for the change"`
	AffectedIDs []string `json:"affected_ids" jsonschema:"Task identifiers the action acted on (control actions)"`
}

// Tasks is the download task list.
type Tasks struct {
	Total   int             `json:"total" jsonschema:"Total number of tasks reported"`
	Tasks   []Task          `json:"tasks" jsonschema:"Download tasks; empty when none"`
	Package PackageEvidence `json:"package" jsonschema:"Installed Download Station package evidence"`
}

// Statistics is the current aggregate transfer rate.
type Statistics struct {
	SpeedDownload int             `json:"speed_download" jsonschema:"Aggregate download speed in bytes/s"`
	SpeedUpload   int             `json:"speed_upload" jsonschema:"Aggregate upload speed in bytes/s"`
	Package       PackageEvidence `json:"package" jsonschema:"Installed Download Station package evidence"`
}

// GlobalSettings is the Download Station general configuration.
type GlobalSettings struct {
	DownloadVolume      string `json:"download_volume,omitempty" jsonschema:"Default download volume mount point"`
	EmuleEnabled        bool   `json:"emule_enabled" jsonschema:"Whether eMule is enabled"`
	UnzipServiceEnabled bool   `json:"unzip_service_enabled" jsonschema:"Whether the auto-unzip service is enabled"`
}

// BTSettings is the BitTorrent configuration. Rates are in KB/s (0 = unlimited).
type BTSettings struct {
	TCPPort                 int    `json:"tcp_port" jsonschema:"BitTorrent listening TCP port"`
	DHTPort                 int    `json:"dht_port" jsonschema:"DHT UDP port"`
	EnableDHT               bool   `json:"enable_dht" jsonschema:"Whether DHT is enabled"`
	EnablePortForwarding    bool   `json:"enable_port_forwarding" jsonschema:"Whether UPnP/NAT-PMP port forwarding is enabled"`
	EnablePreview           bool   `json:"enable_preview" jsonschema:"Whether download preview is enabled"`
	Encryption              string `json:"encryption,omitempty" jsonschema:"Protocol encryption policy, such as auto, on, or off"`
	MaxDownloadRate         int    `json:"max_download_rate" jsonschema:"Maximum download rate in KB/s; 0 = unlimited"`
	MaxUploadRate           int    `json:"max_upload_rate" jsonschema:"Maximum upload rate in KB/s; 0 = unlimited"`
	MaxPeer                 int    `json:"max_peer" jsonschema:"Maximum peers per torrent"`
	SeedingRatio            int    `json:"seeding_ratio" jsonschema:"Stop seeding at this ratio (percent); 0 = no limit"`
	SeedingInterval         int    `json:"seeding_interval" jsonschema:"Stop seeding after this many minutes; 0 = no limit"`
	EnableSeedingAutoRemove bool   `json:"enable_seeding_auto_remove" jsonschema:"Whether completed tasks are auto-removed when seeding stops"`
}

// BTSettingsChange is a patch-only BitTorrent settings intent. A nil field
// keeps the current value.
type BTSettingsChange struct {
	TCPPort                 *int    `json:"tcp_port,omitempty" jsonschema:"Desired BitTorrent TCP port"`
	DHTPort                 *int    `json:"dht_port,omitempty" jsonschema:"Desired DHT UDP port"`
	EnableDHT               *bool   `json:"enable_dht,omitempty" jsonschema:"Enable or disable DHT"`
	EnablePortForwarding    *bool   `json:"enable_port_forwarding,omitempty" jsonschema:"Enable or disable UPnP/NAT-PMP port forwarding"`
	EnablePreview           *bool   `json:"enable_preview,omitempty" jsonschema:"Enable or disable download preview"`
	Encryption              *string `json:"encryption,omitempty" jsonschema:"Protocol encryption policy: auto, on, or off"`
	MaxDownloadRate         *int    `json:"max_download_rate,omitempty" jsonschema:"Maximum download rate in KB/s; 0 = unlimited"`
	MaxUploadRate           *int    `json:"max_upload_rate,omitempty" jsonschema:"Maximum upload rate in KB/s; 0 = unlimited"`
	MaxPeer                 *int    `json:"max_peer,omitempty" jsonschema:"Maximum peers per torrent"`
	SeedingRatio            *int    `json:"seeding_ratio,omitempty" jsonschema:"Stop-seeding ratio in percent; 0 = no limit"`
	SeedingInterval         *int    `json:"seeding_interval,omitempty" jsonschema:"Stop-seeding interval in minutes; 0 = no limit"`
	EnableSeedingAutoRemove *bool   `json:"enable_seeding_auto_remove,omitempty" jsonschema:"Auto-remove completed tasks when seeding stops"`
}

// FtpHttpSettingsChange is a patch-only FTP/HTTP settings intent.
type FtpHttpSettingsChange struct {
	MaxDownloadRate *int  `json:"max_download_rate,omitempty" jsonschema:"FTP/HTTP maximum download rate in KB/s; 0 = unlimited"`
	EnableMaxConn   *bool `json:"enable_max_conn,omitempty" jsonschema:"Enforce the per-task FTP connection limit"`
	MaxConn         *int  `json:"max_conn,omitempty" jsonschema:"Maximum FTP connections per task"`
}

// RssSettingsChange is a patch-only RSS settings intent.
type RssSettingsChange struct {
	UpdateIntervalMinutes *int `json:"update_interval_minutes,omitempty" jsonschema:"RSS feed refresh interval in minutes"`
}

// LocationSettingsChange is a patch-only destination/watch-folder settings intent.
type LocationSettingsChange struct {
	DefaultDestination          *string `json:"default_destination,omitempty" jsonschema:"Default download destination shared folder"`
	EnableTorrentNzbWatch       *bool   `json:"enable_torrent_nzb_watch,omitempty" jsonschema:"Enable the torrent/NZB watch folder"`
	EnableDeleteTorrentNzbWatch *bool   `json:"enable_delete_torrent_nzb_watch,omitempty" jsonschema:"Delete watched torrent/NZB files after import"`
	TorrentNzbWatchFolder       *string `json:"torrent_nzb_watch_folder,omitempty" jsonschema:"Watch folder path"`
}

// SchedulerSettingsChange is a patch-only bandwidth-schedule intent. ScheduleBitmap
// is DSM's raw 168-character weekly on/off bitmap (7 days x 24 hours).
type SchedulerSettingsChange struct {
	EnableSchedule *bool   `json:"enable_schedule,omitempty" jsonschema:"Enable the download schedule"`
	DownloadRate   *int    `json:"download_rate,omitempty" jsonschema:"Scheduled download rate in KB/s; 0 = unlimited"`
	UploadRate     *int    `json:"upload_rate,omitempty" jsonschema:"Scheduled upload rate in KB/s; 0 = unlimited"`
	MaxTasks       *int    `json:"max_tasks,omitempty" jsonschema:"Maximum simultaneous tasks"`
	Order          *string `json:"order,omitempty" jsonschema:"Task ordering policy, such as request"`
	ScheduleBitmap *string `json:"schedule_bitmap,omitempty" jsonschema:"Raw 168-character weekly on/off bitmap"`
}

// GlobalSettingsChange is a patch-only general settings intent.
type GlobalSettingsChange struct {
	DownloadVolume      *string `json:"download_volume,omitempty" jsonschema:"Default download volume mount point"`
	EmuleEnabled        *bool   `json:"emule_enabled,omitempty" jsonschema:"Enable or disable eMule"`
	UnzipServiceEnabled *bool   `json:"unzip_service_enabled,omitempty" jsonschema:"Enable or disable the auto-unzip service"`
}

// SettingsChange is a patch across Download Station settings groups. Exactly one
// group patch is present per change. More groups are added as they are
// implemented as guarded writes.
type SettingsChange struct {
	BT        *BTSettingsChange        `json:"bt,omitempty" jsonschema:"BitTorrent settings patch"`
	FtpHttp   *FtpHttpSettingsChange   `json:"ftp_http,omitempty" jsonschema:"FTP/HTTP settings patch"`
	Rss       *RssSettingsChange       `json:"rss,omitempty" jsonschema:"RSS settings patch"`
	Location  *LocationSettingsChange  `json:"location,omitempty" jsonschema:"Destination/watch-folder settings patch"`
	Scheduler *SchedulerSettingsChange `json:"scheduler,omitempty" jsonschema:"Bandwidth-schedule settings patch"`
	Global    *GlobalSettingsChange    `json:"global,omitempty" jsonschema:"General settings patch"`
}

// SettingsMutationResult records the DSM backend that accepted a settings write.
type SettingsMutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
	Group   string `json:"group" jsonschema:"Settings group changed, such as bt"`
}

// EmuleSettings is the eMule configuration.
type EmuleSettings struct {
	Enabled            bool   `json:"enabled" jsonschema:"Whether eMule is enabled"`
	DefaultDestination string `json:"default_destination,omitempty" jsonschema:"eMule default download destination"`
}

// FtpHttpSettings is the FTP/HTTP download configuration.
type FtpHttpSettings struct {
	MaxDownloadRate int  `json:"max_download_rate" jsonschema:"FTP/HTTP maximum download rate in KB/s; 0 = unlimited"`
	EnableMaxConn   bool `json:"enable_max_conn" jsonschema:"Whether the per-task FTP connection limit is enforced"`
	MaxConn         int  `json:"max_conn" jsonschema:"Maximum FTP connections per task"`
}

// NzbSettings is the NZB (Usenet) configuration. The news-server password is
// never surfaced.
type NzbSettings struct {
	Server               string `json:"server,omitempty" jsonschema:"News server host"`
	Port                 int    `json:"port" jsonschema:"News server port"`
	Username             string `json:"username,omitempty" jsonschema:"News server username"`
	EnableAuth           bool   `json:"enable_auth" jsonschema:"Whether news-server authentication is enabled"`
	EnableEncryption     bool   `json:"enable_encryption" jsonschema:"Whether SSL to the news server is enabled"`
	EnableParchive       bool   `json:"enable_parchive" jsonschema:"Whether PAR2 repair is enabled"`
	EnableRemoveParfiles bool   `json:"enable_remove_parfiles" jsonschema:"Whether PAR2 files are removed after repair"`
	ConnPerDownload      int    `json:"conn_per_download" jsonschema:"Connections per download"`
	MaxDownloadRate      int    `json:"max_download_rate" jsonschema:"NZB maximum download rate in KB/s; 0 = unlimited"`
}

// AutoExtractionSettings is the automatic archive extraction configuration.
// Archive passwords are never surfaced.
type AutoExtractionSettings struct {
	EnableUnzip        bool   `json:"enable_unzip" jsonschema:"Whether automatic extraction is enabled"`
	EnableUnzipService bool   `json:"enable_unzip_service" jsonschema:"Whether the unzip service is enabled"`
	CreateSubfolder    bool   `json:"create_subfolder" jsonschema:"Whether a subfolder is created per archive"`
	DeleteArchive      bool   `json:"delete_archive" jsonschema:"Whether the archive is deleted after extraction"`
	UnzipOverwrite     bool   `json:"unzip_overwrite" jsonschema:"Whether existing files are overwritten"`
	UnzipLocation      string `json:"unzip_location,omitempty" jsonschema:"Extraction location mode, such as current_folder"`
	UnzipToPath        string `json:"unzip_to_path,omitempty" jsonschema:"Extraction destination when a fixed path is used"`
	PasswordConfigured bool   `json:"password_configured" jsonschema:"Whether one or more extraction passwords are configured; the values are never returned"`
}

// LocationSettings is the destination and watch-folder configuration.
type LocationSettings struct {
	DefaultDestination          string `json:"default_destination,omitempty" jsonschema:"Default download destination shared folder"`
	EnableTorrentNzbWatch       bool   `json:"enable_torrent_nzb_watch" jsonschema:"Whether the torrent/NZB watch folder is enabled"`
	EnableDeleteTorrentNzbWatch bool   `json:"enable_delete_torrent_nzb_watch" jsonschema:"Whether watched torrent/NZB files are deleted after import"`
	TorrentNzbWatchFolder       string `json:"torrent_nzb_watch_folder,omitempty" jsonschema:"Watch folder path"`
}

// RssSettings is the RSS auto-download configuration.
type RssSettings struct {
	UpdateIntervalMinutes int `json:"update_interval_minutes" jsonschema:"RSS feed refresh interval in minutes"`
}

// SchedulerSettings is the bandwidth/alternative-rate schedule. Schedule is the
// raw 168-character weekly bitmap DSM stores (7 days x 24 hours).
type SchedulerSettings struct {
	EnableSchedule bool   `json:"enable_schedule" jsonschema:"Whether the download schedule is enabled"`
	DownloadRate   int    `json:"download_rate" jsonschema:"Scheduled download rate in KB/s; 0 = unlimited"`
	UploadRate     int    `json:"upload_rate" jsonschema:"Scheduled upload rate in KB/s; 0 = unlimited"`
	MaxTasks       int    `json:"max_tasks" jsonschema:"Maximum simultaneous tasks"`
	MaxTasksLimit  int    `json:"max_tasks_limit" jsonschema:"Upper bound DSM allows for max_tasks"`
	Order          string `json:"order,omitempty" jsonschema:"Task ordering policy, such as request"`
	ScheduleBitmap string `json:"schedule_bitmap,omitempty" jsonschema:"Raw 168-character weekly on/off bitmap (7 days x 24 hours)"`
}

// Settings is the full normalized Download Station configuration composed from
// the SYNO.DownloadStation2.Settings.* APIs.
type Settings struct {
	Global         GlobalSettings         `json:"global" jsonschema:"General settings"`
	BT             BTSettings             `json:"bt" jsonschema:"BitTorrent settings"`
	Emule          EmuleSettings          `json:"emule" jsonschema:"eMule settings"`
	FtpHttp        FtpHttpSettings        `json:"ftp_http" jsonschema:"FTP/HTTP settings"`
	Nzb            NzbSettings            `json:"nzb" jsonschema:"NZB settings"`
	AutoExtraction AutoExtractionSettings `json:"auto_extraction" jsonschema:"Automatic extraction settings"`
	Location       LocationSettings       `json:"location" jsonschema:"Destination and watch-folder settings"`
	Rss            RssSettings            `json:"rss" jsonschema:"RSS settings"`
	Scheduler      SchedulerSettings      `json:"scheduler" jsonschema:"Bandwidth-schedule settings"`
	Package        PackageEvidence        `json:"package" jsonschema:"Installed Download Station package evidence"`
}

// Capabilities reports which Download Station reads dsmctl exposes for the
// installed package.
type Capabilities struct {
	Module        string          `json:"module" jsonschema:"Stable module name: download"`
	Package       PackageEvidence `json:"package" jsonschema:"Installed Download Station package evidence"`
	ServiceRead   bool            `json:"service_read" jsonschema:"Whether service configuration can be read"`
	TaskRead      bool            `json:"task_read" jsonschema:"Whether the download task list can be read"`
	StatisticRead bool            `json:"statistic_read" jsonschema:"Whether transfer statistics can be read"`
	SettingsRead  bool            `json:"settings_read" jsonschema:"Whether the full detailed settings can be read"`
	TaskWrite     bool            `json:"task_write" jsonschema:"Whether guarded task create/pause/resume/delete is available"`
	SettingsWrite bool            `json:"settings_write" jsonschema:"Whether guarded settings changes (BT, FTP/HTTP, RSS, location, scheduler, global groups) are available"`
}
