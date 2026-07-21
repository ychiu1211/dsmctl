// Package hyperbackup contains stable, package-version-independent models for
// the Synology Hyper Backup client package (backup tasks, versions, and the
// log feed) and the Hyper Backup Vault view. DSM WebAPI names, versions, and
// field names stay behind the operation package. Installed-package versions
// are carried as evidence because both surfaces follow their package releases,
// and the client and vault sides are gated on two different packages.
package hyperbackup

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "hyper_backup"

// PackageEvidence reports one installed Hyper Backup-family package.
type PackageEvidence struct {
	ID        string `json:"id" jsonschema:"DSM package identifier: HyperBackup or HyperBackupVault"`
	Installed bool   `json:"installed" jsonschema:"Whether the package is installed"`
	Version   string `json:"version,omitempty" jsonschema:"Installed package version"`
	Running   bool   `json:"running" jsonschema:"Whether the package is running"`
}

// Schedule is a task's backup schedule as far as DSM reports it on the task
// list. It is absent when the task has no schedule configured.
type Schedule struct {
	Enabled  bool   `json:"enabled" jsonschema:"Whether the schedule is enabled"`
	Hour     int    `json:"hour" jsonschema:"Scheduled hour of day"`
	Minute   int    `json:"minute" jsonschema:"Scheduled minute"`
	WeekName string `json:"week_name,omitempty" jsonschema:"Comma-separated weekday numbers the schedule runs on"`
}

// Task is one Hyper Backup task as reported by the task list. Times are DSM
// local-time strings (YYYY/MM/DD HH:MM:SS); empty means never/none.
type Task struct {
	TaskID           int       `json:"task_id" jsonschema:"Task identifier"`
	Name             string    `json:"name" jsonschema:"Task display name"`
	Type             string    `json:"type,omitempty" jsonschema:"Task type, such as image:image_local or share:local"`
	TargetType       string    `json:"target_type,omitempty" jsonschema:"Destination class, such as image, share, or cloud_image"`
	TransferType     string    `json:"transfer_type,omitempty" jsonschema:"Destination transport, such as image_local, rsync, or synocloud_swift"`
	TargetID         string    `json:"target_id,omitempty" jsonschema:"Destination directory or target identifier on the backup destination"`
	RepositoryID     int       `json:"repository_id,omitempty" jsonschema:"Identifier of the repository the task backs up into"`
	DataType         string    `json:"data_type,omitempty" jsonschema:"Backed-up data class, such as data"`
	State            string    `json:"state,omitempty" jsonschema:"Task lifecycle state, such as backupable or broken"`
	Status           string    `json:"status,omitempty" jsonschema:"Live activity: none when idle, backup while running, canceling, or deleting"`
	DataEncrypted    bool      `json:"data_encrypted" jsonschema:"Whether client-side encryption is enabled"`
	Modified         bool      `json:"modified" jsonschema:"Whether source data changed since the last backup"`
	LastBackupTime   string    `json:"last_backup_time,omitempty" jsonschema:"Start time of the last backup run; empty when never run"`
	LastBackupEnd    string    `json:"last_backup_end_time,omitempty" jsonschema:"End time of the last backup run"`
	LastBackupResult string    `json:"last_backup_result,omitempty" jsonschema:"Result of the last run: done, cancel, fail, backingup while running, or none"`
	NextBackupTime   string    `json:"next_backup_time,omitempty" jsonschema:"Next scheduled run; empty when unscheduled"`
	Schedule         *Schedule `json:"schedule,omitempty" jsonschema:"Backup schedule; absent when the task has none"`
	SourceFolders    []string  `json:"source_folders,omitempty" jsonschema:"Backed-up folder paths, when the task reports its source"`
	SourceApps       []string  `json:"source_applications,omitempty" jsonschema:"Backed-up application names, when the task reports its source"`
}

// Tasks is the Hyper Backup task list.
type Tasks struct {
	Total   int             `json:"total" jsonschema:"Total number of backup tasks"`
	Tasks   []Task          `json:"tasks" jsonschema:"Backup tasks; empty when none"`
	Package PackageEvidence `json:"package" jsonschema:"Installed HyperBackup package evidence"`
}

// BackupParams are the task's transfer options.
type BackupParams struct {
	CompressionEnabled bool `json:"compression_enabled" jsonschema:"Whether transfer compression is enabled"`
	EncryptionEnabled  bool `json:"encryption_enabled" jsonschema:"Whether client-side encryption is enabled"`
	NotifyEnabled      bool `json:"notify_enabled" jsonschema:"Whether run-result notifications are enabled"`
	VersionFileLog     bool `json:"version_file_log" jsonschema:"Whether per-version file change logs are kept"`
	MaxAutoResumeRetry int  `json:"max_auto_resume_retry,omitempty" jsonschema:"Automatic resume attempts after an interrupted run"`
}

// Repository is the destination repository a task backs up into.
type Repository struct {
	RepositoryID int    `json:"repository_id" jsonschema:"Repository identifier"`
	Name         string `json:"name,omitempty" jsonschema:"Repository display name"`
	Share        string `json:"share,omitempty" jsonschema:"Destination shared folder for local destinations"`
	TargetType   string `json:"target_type,omitempty" jsonschema:"Destination class"`
	TransferType string `json:"transfer_type,omitempty" jsonschema:"Destination transport"`
}

// Progress is the live progress of a running backup. DSM reports several
// counters as strings on the wire; they are normalized to integers here.
type Progress struct {
	Step             string `json:"step,omitempty" jsonschema:"Current backup step, such as backup_prepare or backup_data"`
	Percent          int    `json:"percent" jsonschema:"Overall progress percentage as reported; 0 when not yet computed"`
	ProcessedBytes   int64  `json:"processed_bytes" jsonschema:"Bytes processed so far"`
	TotalBytes       int64  `json:"total_bytes" jsonschema:"Total bytes to process; 0 while still scanning"`
	TransmittedBytes int64  `json:"transmitted_bytes" jsonschema:"Bytes transmitted to the destination"`
	AverageSpeedBps  int64  `json:"average_speed_bps" jsonschema:"Average transfer speed in bytes per second"`
	CanCancel        bool   `json:"can_cancel" jsonschema:"Whether the run can be canceled right now"`
}

// TaskStatus is the live status of one task.
type TaskStatus struct {
	State             string    `json:"state,omitempty" jsonschema:"Task lifecycle state, such as backupable"`
	Status            string    `json:"status,omitempty" jsonschema:"Live activity: none when idle, backup while running, or canceling"`
	LastBackupTime    string    `json:"last_backup_time,omitempty" jsonschema:"Start time of the last run"`
	LastBackupEnd     string    `json:"last_backup_end_time,omitempty" jsonschema:"End time of the last run"`
	LastSuccessTime   string    `json:"last_success_time,omitempty" jsonschema:"End time of the last successful run"`
	LastBackupResult  string    `json:"last_backup_result,omitempty" jsonschema:"Result of the last run: done, cancel, fail, backingup while running, or none"`
	LastBackupError   string    `json:"last_backup_error,omitempty" jsonschema:"Human-readable error of the last run; empty when none"`
	NextBackupTime    string    `json:"next_backup_time,omitempty" jsonschema:"Next scheduled run; empty when unscheduled"`
	Progress          *Progress `json:"progress,omitempty" jsonschema:"Live progress; present only while a run is active"`
}

// Target is the reachability view of a task's destination.
type Target struct {
	Online              bool   `json:"online" jsonschema:"Whether the backup destination is reachable"`
	HostName            string `json:"host_name,omitempty" jsonschema:"Destination host name, when reported"`
	OwnerName           string `json:"owner_name,omitempty" jsonschema:"Task owner account"`
	FormatType          string `json:"format_type,omitempty" jsonschema:"Destination repository format, such as image"`
	MultiVersionSupport bool   `json:"multi_version_support" jsonschema:"Whether the destination stores multiple versions"`
}

// TaskDetail is the full view of one task: the list row, the repository, the
// transfer parameters, the live status, and destination reachability.
type TaskDetail struct {
	Task         Task            `json:"task" jsonschema:"Task identity and last/next run summary"`
	Repository   Repository      `json:"repository" jsonschema:"Destination repository the task backs up into"`
	BackupParams BackupParams    `json:"backup_params" jsonschema:"Transfer options"`
	Status       TaskStatus      `json:"status" jsonschema:"Live task status and progress"`
	Target       Target          `json:"target" jsonschema:"Destination reachability"`
	Package      PackageEvidence `json:"package" jsonschema:"Installed HyperBackup package evidence"`
}

// Version is one backup version of a task.
type Version struct {
	VersionID    string `json:"version_id" jsonschema:"Version identifier"`
	Name         string `json:"name,omitempty" jsonschema:"Version display name (its start time)"`
	Status       string `json:"status,omitempty" jsonschema:"Version integrity status, such as success"`
	Locked       bool   `json:"locked" jsonschema:"Whether the version is locked against rotation"`
	StartTime    string `json:"start_time,omitempty" jsonschema:"Version start time in DSM local time"`
	CompleteTime string `json:"complete_time,omitempty" jsonschema:"Version completion time in DSM local time"`
	Timestamp    int64  `json:"timestamp,omitempty" jsonschema:"Version start time as a Unix timestamp"`
}

// Versions is the version list of one task.
type Versions struct {
	TaskID  int             `json:"task_id" jsonschema:"Task the versions belong to"`
	Total   int             `json:"total" jsonschema:"Total number of versions the task has"`
	Entries []Version       `json:"versions" jsonschema:"Backup versions; empty when none"`
	Package PackageEvidence `json:"package" jsonschema:"Installed HyperBackup package evidence"`
}

// LogEntry is one Hyper Backup log line.
type LogEntry struct {
	Level string `json:"level" jsonschema:"Log level as reported by DSM: info, warn, or err"`
	Time  string `json:"time" jsonschema:"Log time in DSM local time"`
	Event string `json:"event" jsonschema:"Log message"`
	User  string `json:"user,omitempty" jsonschema:"Account that triggered the event"`
}

// Logs is a page of the Hyper Backup log feed plus the server-side level
// counters for the whole feed.
type Logs struct {
	Total      int             `json:"total" jsonschema:"Total number of log entries on the NAS"`
	Offset     int             `json:"offset" jsonschema:"Offset of the next page as reported by DSM"`
	ErrorCount int             `json:"error_count" jsonschema:"Total error entries"`
	WarnCount  int             `json:"warning_count" jsonschema:"Total warning entries"`
	InfoCount  int             `json:"info_count" jsonschema:"Total info entries"`
	Entries    []LogEntry      `json:"entries" jsonschema:"Log entries, newest first; empty when none"`
	Package    PackageEvidence `json:"package" jsonschema:"Installed HyperBackup package evidence"`
}

// VaultTarget is one inbound target stored on this NAS by Hyper Backup Vault.
// The shape is live-verified against a real inbound image_remote backup.
type VaultTarget struct {
	TargetID              int    `json:"target_id" jsonschema:"Vault target identifier"`
	Share                 string `json:"share,omitempty" jsonschema:"Shared folder holding the target"`
	TargetName            string `json:"target_name,omitempty" jsonschema:"Target directory name"`
	TargetPath            string `json:"target_path,omitempty" jsonschema:"Absolute path of the target on this NAS"`
	Status                string `json:"status,omitempty" jsonschema:"Inbound session activity, such as idle or backup"`
	Encrypted             bool   `json:"encrypted" jsonschema:"Whether the stored backup is client-side encrypted"`
	Resumable             bool   `json:"resumable" jsonschema:"Whether an interrupted inbound backup can resume"`
	UsedSizeBytes         int64  `json:"used_size_bytes,omitempty" jsonschema:"Space the target uses in bytes; 0 while still computing"`
	ComputingSize         bool   `json:"computing_size,omitempty" jsonschema:"Whether the size is still being computed"`
	LastBackupStart       int64  `json:"last_backup_start,omitempty" jsonschema:"Start of the last inbound backup as a Unix timestamp; 0 when never"`
	LastBackupDurationSec int64  `json:"last_backup_duration_seconds,omitempty" jsonschema:"Duration of the last inbound backup in seconds"`
}

// Vault is the Hyper Backup Vault service view of this NAS as a backup
// destination.
type Vault struct {
	ParallelBackupLimit int             `json:"parallel_backup_limit" jsonschema:"Maximum concurrent inbound backup sessions"`
	Targets             []VaultTarget   `json:"targets" jsonschema:"Inbound vault targets stored on this NAS; empty when none"`
	Package             PackageEvidence `json:"package" jsonschema:"Installed HyperBackupVault package evidence"`
}

// Application is one package Hyper Backup can include in a backup task.
type Application struct {
	ID              string   `json:"id" jsonschema:"Application identifier used in a task's applications list"`
	Name            string   `json:"name,omitempty" jsonschema:"Display name"`
	Version         string   `json:"version,omitempty" jsonschema:"Installed package version"`
	Running         bool     `json:"running" jsonschema:"Whether the package is currently running"`
	Backupable      bool     `json:"backupable" jsonschema:"Whether this application can be selected for backup"`
	Reason          string   `json:"reason,omitempty" jsonschema:"Why the application cannot be backed up, when it cannot"`
	OnlineBackup    bool     `json:"online_backup" jsonschema:"Whether the application is backed up without stopping it"`
	Summary         string   `json:"summary,omitempty" jsonschema:"What the backup includes, as described by the application"`
	RequiredFolders []string `json:"required_folders,omitempty" jsonschema:"Shared folders the application's data lives in, when it reports any"`
}

// Applications is the list of packages Hyper Backup can back up.
type Applications struct {
	Entries []Application   `json:"applications" jsonschema:"Applications Hyper Backup reports; empty when none"`
	Package PackageEvidence `json:"package" jsonschema:"Installed HyperBackup package evidence"`
}

// Lun is one LUN that legacy Hyper Backup LUN backup can protect. File/regular
// LUNs (volume-based) are what this legacy engine backs up; block-level LUNs use
// the separate multi-version LUN engine, which dsmctl does not implement.
type Lun struct {
	Name      string `json:"name" jsonschema:"LUN name, used as lun_source when creating a LUN backup"`
	Type      string `json:"type,omitempty" jsonschema:"LUN type as reported, such as regular-file"`
	UUID      string `json:"uuid,omitempty" jsonschema:"LUN identifier"`
	SizeBytes int64  `json:"size_bytes" jsonschema:"Configured LUN capacity in bytes"`
}

// Luns is the list of LUNs legacy Hyper Backup LUN backup can protect.
type Luns struct {
	Entries []Lun           `json:"luns" jsonschema:"Backupable LUNs; empty when none"`
	Package PackageEvidence `json:"package" jsonschema:"Installed HyperBackup package evidence"`
}

// LunBackupTask is one legacy LUN backup task (loclunbkp = local destination on
// this NAS). It is a separate task space from the image tasks: identified by
// name, its status is read via the legacy load_task call, not the Task API.
type LunBackupTask struct {
	TaskName         string `json:"task_name" jsonschema:"Task name (also its identifier)"`
	Type             string `json:"type,omitempty" jsonschema:"Task type: loclunbkp (local) or netlunbkp (remote)"`
	LunSource        string `json:"lun_source,omitempty" jsonschema:"Name of the LUN being backed up"`
	DestinationShare string `json:"destination_share,omitempty" jsonschema:"Shared folder holding the backup"`
	DestinationDir   string `json:"destination_directory,omitempty" jsonschema:"Backup directory within the share"`
	Status           string `json:"status,omitempty" jsonschema:"Live activity: none when idle, backup while running"`
	LastBackupResult string `json:"last_backup_result,omitempty" jsonschema:"Result of the last run: success, fail, or none"`
	LastBackupTime   string `json:"last_backup_time,omitempty" jsonschema:"Start time of the last run"`
	Step             string `json:"step,omitempty" jsonschema:"Current backup step while running"`
	Percent          int    `json:"percent" jsonschema:"Progress percentage while running"`
	UUID             string `json:"uuid,omitempty" jsonschema:"Task identifier"`
}

// LunBackupTasks is the list of legacy LUN backup tasks.
type LunBackupTasks struct {
	Entries []LunBackupTask `json:"tasks" jsonschema:"LUN backup tasks; empty when none"`
	Package PackageEvidence `json:"package" jsonschema:"Installed HyperBackup package evidence"`
}

// LunBackupCreate describes a new local LUN backup (loclunbkp): back up one
// file/regular LUN to a shared folder on this NAS.
type LunBackupCreate struct {
	TaskName         string `json:"task_name" jsonschema:"Name of the new LUN backup task; must not collide with an existing one"`
	LunSource        string `json:"lun_source" jsonschema:"Name of the LUN to back up (from the luns read)"`
	SizeBytes        int64  `json:"lun_size_bytes,omitempty" jsonschema:"LUN capacity in bytes (from the luns read); resolved from the LUN if omitted"`
	DestinationShare string `json:"destination_share" jsonschema:"Shared folder on this NAS that will store the backup"`
	Directory        string `json:"directory,omitempty" jsonschema:"Backup directory name; empty uses the destination's proposed name"`
	BackupNow        bool   `json:"backup_now,omitempty" jsonschema:"Run the first backup immediately after creating the task"`
}

// LunBackupChange is the intent for a guarded LUN backup action.
type LunBackupChange struct {
	Action string           `json:"action" jsonschema:"LUN backup action: create"`
	Create *LunBackupCreate `json:"create,omitempty" jsonschema:"New LUN backup description when action is create"`
}

// LunBackupMutationResult records the DSM backend used for a LUN backup action.
type LunBackupMutationResult struct {
	Backend        string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API            string `json:"api" jsonschema:"DSM WebAPI used for the action"`
	Version        int    `json:"version" jsonschema:"DSM WebAPI version used for the action"`
	Method         string `json:"method" jsonschema:"DSM WebAPI method used for the action"`
	TaskName       string `json:"task_name" jsonschema:"Task the action created"`
	DestinationDir string `json:"destination_directory,omitempty" jsonschema:"Backup directory of the created task"`
	BackedUp       bool   `json:"backed_up" jsonschema:"Whether an immediate backup was requested and observed"`
}

// TaskAction is a guarded backup-task action.
type TaskAction string

const (
	TaskActionBackup TaskAction = "backup"
	TaskActionCancel TaskAction = "cancel"
	TaskActionCreate TaskAction = "create"
)

// TaskCreate describes a new folder backup task. The destination is exactly
// one of: a shared folder on the source NAS itself (LocalShare), another NAS
// known to dsmctl (TargetNAS — its address, account, and stored credential are
// resolved at apply time and never enter the plan), or an explicit remote
// Synology NAS (Host + Account + PasswordRef). The created task has no
// schedule; it runs when triggered (run-now action or the DSM UI).
type TaskCreate struct {
	TaskName      string   `json:"task_name" jsonschema:"Name of the new backup task; must not collide with an existing task"`
	SourceFolders []string `json:"source_folders,omitempty" jsonschema:"Absolute shared-folder paths on the source NAS to back up, such as /homes or /Share/projects"`
	Applications  []string `json:"applications,omitempty" jsonschema:"Application identifiers to back up (from the applications read); a task needs at least one folder or application"`

	LocalShare string `json:"local_share,omitempty" jsonschema:"Local destination mode: shared-folder name on the source NAS itself"`

	TargetNAS string `json:"target_nas,omitempty" jsonschema:"Remote destination mode A: dsmctl profile name of the destination NAS; host, account, and the stored credential are resolved at apply time"`

	Host        string  `json:"host,omitempty" jsonschema:"Remote destination mode B: destination NAS address"`
	Account     string  `json:"account,omitempty" jsonschema:"Remote mode B: DSM account on the destination NAS"`
	PasswordRef *string `json:"password_ref,omitempty" jsonschema:"Remote mode B: credential reference (env:NAME) resolved at apply time; the secret never enters the plan"`

	DestinationShare   string `json:"destination_share,omitempty" jsonschema:"Remote modes: shared folder on the destination NAS that stores the backup"`
	Directory          string `json:"directory,omitempty" jsonschema:"Destination directory name; empty uses the destination's proposed name"`
	Port               int    `json:"port,omitempty" jsonschema:"Remote modes: Hyper Backup Vault service port; default 6281"`
	TransferEncryption *bool  `json:"transfer_encryption,omitempty" jsonschema:"Remote modes: encrypt the transfer channel; default true (the destination certificate is not verified)"`
	Compression        bool   `json:"compression,omitempty" jsonschema:"Enable transfer/storage compression"`
	Notify             bool   `json:"notify,omitempty" jsonschema:"Enable DSM notifications for run results"`
}

// TaskChange is the intent for a guarded task action. Backup starts a run on
// an idle task; cancel stops the running backup of a task; create makes a new
// folder backup task.
type TaskChange struct {
	Action TaskAction  `json:"action" jsonschema:"Task action: backup (run now), cancel, or create"`
	TaskID int         `json:"task_id,omitempty" jsonschema:"Target backup task identifier for backup and cancel"`
	Create *TaskCreate `json:"create,omitempty" jsonschema:"New-task description when action is create"`
}

// TaskMutationResult records the DSM backend used for a task action.
type TaskMutationResult struct {
	Backend      string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API          string `json:"api" jsonschema:"DSM WebAPI used for the action"`
	Version      int    `json:"version" jsonschema:"DSM WebAPI version used for the action"`
	Method       string `json:"method" jsonschema:"DSM WebAPI method used for the action"`
	TaskID       int    `json:"task_id" jsonschema:"Task the action targeted or created"`
	RepositoryID int    `json:"repository_id,omitempty" jsonschema:"Repository created for a new task"`
	Directory    string `json:"directory,omitempty" jsonschema:"Destination directory of a created task"`
}

// Capabilities reports which Hyper Backup reads and actions dsmctl exposes for
// a NAS, with both packages' evidence.
type Capabilities struct {
	Module       string          `json:"module" jsonschema:"Stable module name: hyper_backup"`
	Package      PackageEvidence `json:"package" jsonschema:"Installed HyperBackup package evidence"`
	VaultPackage PackageEvidence `json:"vault_package" jsonschema:"Installed HyperBackupVault package evidence"`
	TaskRead     bool            `json:"task_read" jsonschema:"Whether the task list can be read"`
	DetailRead   bool            `json:"detail_read" jsonschema:"Whether task detail (status, repository, target) can be read"`
	VersionRead  bool            `json:"version_read" jsonschema:"Whether task versions can be listed"`
	LogRead      bool            `json:"log_read" jsonschema:"Whether the Hyper Backup log feed can be read"`
	VaultRead    bool            `json:"vault_read" jsonschema:"Whether the Hyper Backup Vault view can be read"`
	TaskRun         bool         `json:"task_run" jsonschema:"Whether guarded run/cancel task actions are available"`
	TaskCreate      bool         `json:"task_create" jsonschema:"Whether the guarded folder/application backup task create is available"`
	AppRead         bool         `json:"application_read" jsonschema:"Whether the backupable-application list can be read"`
	LunRead         bool         `json:"lun_read" jsonschema:"Whether the backupable LUN list and LUN backup tasks can be read"`
	LunBackupCreate bool         `json:"lun_backup_create" jsonschema:"Whether the guarded local LUN backup create is available"`
}
