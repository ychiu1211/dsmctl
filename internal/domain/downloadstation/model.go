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

// Task is one download task. Entry fields are decoded tolerantly: the NAS used
// to model this type had no task, so only fields DSM returns are populated and
// unknown extras are ignored.
type Task struct {
	ID          string       `json:"id,omitempty" jsonschema:"Task identifier"`
	Type        string       `json:"type,omitempty" jsonschema:"Download protocol: bt, http, ftp, emule, or nzb"`
	Username    string       `json:"username,omitempty" jsonschema:"Owner of the task"`
	Title       string       `json:"title,omitempty" jsonschema:"Task title"`
	Size        int64        `json:"size" jsonschema:"Total size in bytes"`
	Status      string       `json:"status,omitempty" jsonschema:"Task status, such as downloading, paused, finished, or error"`
	Destination string       `json:"destination,omitempty" jsonschema:"Download destination, when reported"`
	Transfer    TaskTransfer `json:"transfer" jsonschema:"Live transfer progress"`
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

// Capabilities reports which Download Station reads dsmctl exposes for the
// installed package.
type Capabilities struct {
	Module        string          `json:"module" jsonschema:"Stable module name: download"`
	Package       PackageEvidence `json:"package" jsonschema:"Installed Download Station package evidence"`
	ServiceRead   bool            `json:"service_read" jsonschema:"Whether service configuration can be read"`
	TaskRead      bool            `json:"task_read" jsonschema:"Whether the download task list can be read"`
	StatisticRead bool            `json:"statistic_read" jsonschema:"Whether transfer statistics can be read"`
}
