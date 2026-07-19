// Package filestation contains stable, DSM-version-independent models for the
// Synology FileStation WebAPI: shared-folder and directory listings, file
// information, search results, directory-size and MD5 computations, mounted
// virtual folders, and permission checks. DSM WebAPI names, versions, and field
// names stay behind the operation package. FileStation is a core DSM surface
// (SYNO.FileStation.*), so there is no installed-package evidence.
package filestation

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "file"

// Owner is the owning user and group of a file-system entry, populated when the
// listing requests the "owner" additional field.
type Owner struct {
	User  string `json:"user,omitempty" jsonschema:"Owning user name"`
	Group string `json:"group,omitempty" jsonschema:"Owning group name"`
	UID   int    `json:"uid,omitempty" jsonschema:"Owning user id"`
	GID   int    `json:"gid,omitempty" jsonschema:"Owning group id"`
}

// Time holds an entry's timestamps in Unix seconds, populated when the listing
// requests the "time" additional field.
type Time struct {
	Access   int64 `json:"access,omitempty" jsonschema:"Last access time in Unix seconds"`
	Modified int64 `json:"modified,omitempty" jsonschema:"Last modification time in Unix seconds"`
	Changed  int64 `json:"changed,omitempty" jsonschema:"Last metadata change time in Unix seconds"`
	Created  int64 `json:"created,omitempty" jsonschema:"Creation time in Unix seconds"`
}

// Permission is the normalized permission summary for an entry, populated when
// the listing requests the "perm" additional field.
type Permission struct {
	POSIX      int    `json:"posix,omitempty" jsonschema:"POSIX permission bits as a decimal integer, for example 644"`
	ACLMode    bool   `json:"acl_mode" jsonschema:"Whether the entry is in Windows ACL mode"`
	ShareRight string `json:"share_right,omitempty" jsonschema:"Share-level right for a shared folder, such as RW or RO"`
	ReadOnly   bool   `json:"read_only,omitempty" jsonschema:"Whether the shared folder is read-only for the current user"`
}

// Entry is one file, folder, shared folder, or mounted virtual folder. The
// additional fields are populated only when requested and are otherwise nil.
type Entry struct {
	Path        string      `json:"path" jsonschema:"Absolute FileStation path, for example /share/dir/file.txt"`
	Name        string      `json:"name" jsonschema:"Entry base name"`
	IsDir       bool        `json:"is_dir" jsonschema:"Whether the entry is a directory or shared folder"`
	RealPath    string      `json:"real_path,omitempty" jsonschema:"Physical volume path, such as /volume1/share/dir"`
	Size        int64       `json:"size,omitempty" jsonschema:"Size in bytes for files"`
	ContentType string      `json:"content_type,omitempty" jsonschema:"DSM content classification, such as dir, image, or document"`
	MountType   string      `json:"mount_type,omitempty" jsonschema:"Mount-point type for shared or virtual folders"`
	Owner       *Owner      `json:"owner,omitempty" jsonschema:"Owning user and group when requested"`
	Time        *Time       `json:"time,omitempty" jsonschema:"Timestamps when requested"`
	Permission  *Permission `json:"permission,omitempty" jsonschema:"Permission summary when requested"`
}

// Listing is a shared-folder listing, directory listing, or mounted
// virtual-folder listing. Path is the enumerated folder for a directory listing
// and empty for a shared-folder or virtual-folder listing.
type Listing struct {
	Path    string  `json:"path,omitempty" jsonschema:"Enumerated folder path; empty for a shared-folder or virtual-folder listing"`
	Total   int     `json:"total" jsonschema:"Total number of entries reported by DSM"`
	Offset  int     `json:"offset" jsonschema:"Offset of the first returned entry"`
	Entries []Entry `json:"entries" jsonschema:"Directory entries; empty when none"`
}

// Info is one entry's detail, returned by List.getinfo.
type Info struct {
	Entries []Entry `json:"entries" jsonschema:"Entries whose information was requested"`
}

// SearchResult is a completed FileStation search. Search is asynchronous; the
// operation starts a task, polls until DSM reports it finished, and cleans it
// up, so callers receive a single completed result.
type SearchResult struct {
	Total    int     `json:"total" jsonschema:"Total number of matches reported by DSM"`
	Offset   int     `json:"offset" jsonschema:"Offset of the first returned match"`
	Finished bool    `json:"finished" jsonschema:"Whether the search task completed before results were read"`
	Entries  []Entry `json:"entries" jsonschema:"Matching entries; empty when none"`
}

// DirSize is a completed directory-size computation (asynchronous, polled to
// completion by the operation).
type DirSize struct {
	Finished  bool  `json:"finished" jsonschema:"Whether the size computation completed"`
	NumDir    int64 `json:"num_dir" jsonschema:"Number of directories counted"`
	NumFile   int64 `json:"num_file" jsonschema:"Number of files counted"`
	TotalSize int64 `json:"total_size" jsonschema:"Total size in bytes"`
}

// MD5 is a completed file MD5 computation (asynchronous, polled to completion by
// the operation).
type MD5 struct {
	Finished bool   `json:"finished" jsonschema:"Whether the MD5 computation completed"`
	MD5      string `json:"md5" jsonschema:"Lowercase hexadecimal MD5 digest"`
}

// PermissionCheck reports whether the current session may write to a path,
// returned by CheckPermission.write.
type PermissionCheck struct {
	Path     string `json:"path" jsonschema:"Path that was checked"`
	Writable bool   `json:"writable" jsonschema:"Whether the current session may create or write at the path"`
}

// Service reports FileStation-wide capabilities of the current session, returned
// by Info.get.
type Service struct {
	Hostname                string   `json:"hostname,omitempty" jsonschema:"DSM host name reported by FileStation"`
	IsManager               bool     `json:"is_manager" jsonschema:"Whether the current account has FileStation manager rights"`
	SupportSharing          bool     `json:"support_sharing" jsonschema:"Whether public file sharing links are supported"`
	SupportVirtualProtocols []string `json:"support_virtual_protocols,omitempty" jsonschema:"Virtual mount protocols supported, such as cifs or nfs"`
}

// Capabilities reports which FileStation operations dsmctl exposes for the
// current NAS, each selected independently.
type Capabilities struct {
	Module            string `json:"module" jsonschema:"Stable module name: file"`
	InfoRead          bool   `json:"info_read" jsonschema:"Whether FileStation service information can be read"`
	ListRead          bool   `json:"list_read" jsonschema:"Whether shared folders and directories can be listed"`
	SearchRead        bool   `json:"search_read" jsonschema:"Whether file search is available"`
	DirSizeRead       bool   `json:"dir_size_read" jsonschema:"Whether directory-size computation is available"`
	MD5Read           bool   `json:"md5_read" jsonschema:"Whether file MD5 computation is available"`
	VirtualFolderRead bool   `json:"virtual_folder_read" jsonschema:"Whether mounted virtual folders can be listed"`
	PermissionCheck   bool   `json:"permission_check" jsonschema:"Whether write-permission checks are available"`
	Download          bool   `json:"download" jsonschema:"Whether files can be downloaded (streamed to local disk)"`
	Upload            bool   `json:"upload" jsonschema:"Whether files can be uploaded (streamed from local disk)"`
	CreateFolder      bool   `json:"create_folder" jsonschema:"Whether folders can be created"`
	Rename            bool   `json:"rename" jsonschema:"Whether entries can be renamed"`
	Copy              bool   `json:"copy" jsonschema:"Whether entries can be copied"`
	Move              bool   `json:"move" jsonschema:"Whether entries can be moved"`
	Delete            bool   `json:"delete" jsonschema:"Whether entries can be deleted"`
	Compress          bool   `json:"compress" jsonschema:"Whether entries can be compressed into an archive"`
	Extract           bool   `json:"extract" jsonschema:"Whether archives can be extracted"`
	Favorite          bool   `json:"favorite" jsonschema:"Whether personal favorites can be managed"`
	Sharing           bool   `json:"sharing" jsonschema:"Whether public sharing links can be managed"`
	BackgroundTask    bool   `json:"background_task" jsonschema:"Whether background file-operation tasks can be listed"`
}

// SharingLink is one public file-sharing link.
type SharingLink struct {
	ID            string `json:"id" jsonschema:"DSM sharing-link identifier used to edit or delete the link"`
	Name          string `json:"name,omitempty" jsonschema:"Link display name"`
	Path          string `json:"path" jsonschema:"Absolute path the link exposes"`
	URL           string `json:"url,omitempty" jsonschema:"Public URL of the link"`
	IsFolder      bool   `json:"is_folder" jsonschema:"Whether the shared entry is a folder"`
	HasPassword   bool   `json:"has_password" jsonschema:"Whether the link is password-protected"`
	Status        string `json:"status,omitempty" jsonschema:"Link status, such as valid or broken"`
	DateExpired   string `json:"date_expired,omitempty" jsonschema:"Expiry date, or empty when the link never expires"`
	DateAvailable string `json:"date_available,omitempty" jsonschema:"Date the link becomes available, or empty when immediate"`
}

// SharingLinks is the public sharing-link inventory.
type SharingLinks struct {
	Total int           `json:"total" jsonschema:"Total number of sharing links reported"`
	Links []SharingLink `json:"links" jsonschema:"Public sharing links; empty when none"`
}

// BackgroundTask is one in-progress or finished background file operation.
type BackgroundTask struct {
	TaskID         string  `json:"task_id" jsonschema:"Background task identifier"`
	API            string  `json:"api,omitempty" jsonschema:"DSM API that owns the task, such as SYNO.FileStation.CopyMove"`
	Finished       bool    `json:"finished" jsonschema:"Whether the task has finished"`
	ProcessingPath string  `json:"processing_path,omitempty" jsonschema:"Path currently being processed"`
	Progress       float64 `json:"progress,omitempty" jsonschema:"Progress from 0 to 1 when reported"`
}

// BackgroundTasks is the background file-operation task list.
type BackgroundTasks struct {
	Total int              `json:"total" jsonschema:"Total number of background tasks reported"`
	Tasks []BackgroundTask `json:"tasks" jsonschema:"Background file-operation tasks; empty when none"`
}
