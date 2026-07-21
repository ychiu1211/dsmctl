// Package snapshotreplication contains stable models for the Snapshot
// Replication surface: btrfs shared-folder snapshots with their attributes,
// per-share snapshot configuration and retention policy, the local replication
// node identity, replication plans, and the Snapshot Replication log feed.
//
// The snapshot lifecycle (list/create/set/delete), share snapshot
// configuration, retention policy, log feed, and node identity are core DSM
// APIs that answer without the SnapshotReplication package; replication plans
// exist only once the package is installed, so only the replication surface
// carries a package gate. DSM WebAPI names, versions, and field names stay
// behind the operation package.
package snapshotreplication

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "snapshot-replication"

// PackageEvidence reports the installed SnapshotReplication package. The
// snapshot surface works without it; replication requires it.
type PackageEvidence struct {
	ID        string `json:"id" jsonschema:"DSM package identifier: SnapshotReplication"`
	Installed bool   `json:"installed" jsonschema:"Whether the Snapshot Replication package is installed"`
	Version   string `json:"version,omitempty" jsonschema:"Installed package version"`
	Running   bool   `json:"running" jsonschema:"Whether the package service is running"`
}

// Snapshot is one btrfs snapshot of a shared folder. DSM identifies a share
// snapshot by its GMT-stamped time name, unique within the share.
type Snapshot struct {
	Time            string `json:"time" jsonschema:"Snapshot identifier: GMT-stamped creation time, unique within the share"`
	Description     string `json:"description,omitempty" jsonschema:"Operator-supplied snapshot description"`
	Locked          bool   `json:"locked" jsonschema:"Whether the snapshot is locked (protected from retention pruning)"`
	ScheduleCreated bool   `json:"schedule_created" jsonschema:"Whether a snapshot schedule created this snapshot (false for manual snapshots)"`
	WormLocked      bool   `json:"worm_locked" jsonschema:"Whether an immutable (WORM) lock protects this snapshot"`
}

// ShareSnapshots is the snapshot inventory of one shared folder.
type ShareSnapshots struct {
	Share     string     `json:"share" jsonschema:"Shared-folder name"`
	Total     int        `json:"total" jsonschema:"Total snapshots reported by DSM"`
	Snapshots []Snapshot `json:"snapshots" jsonschema:"Snapshots ordered as reported by DSM"`
}

// ShareConfig is the per-shared-folder snapshot configuration.
type ShareConfig struct {
	Share            string `json:"share" jsonschema:"Shared-folder name"`
	SnapshotBrowsing bool   `json:"snapshot_browsing" jsonschema:"Whether users can browse snapshots under the #snapshot directory"`
	LocalTimeFormat  bool   `json:"local_time_format" jsonschema:"Whether snapshot names use local time instead of GMT"`
}

// RetentionPolicy is the snapshot retention policy of one shared folder as
// reported by DSM. A TaskID of -1 means no retention task is configured.
type RetentionPolicy struct {
	Share       string `json:"share" jsonschema:"Shared-folder name"`
	TaskID      int    `json:"task_id" jsonschema:"Retention task identifier; -1 when no retention task is configured"`
	PolicyType  int    `json:"policy_type" jsonschema:"DSM retention policy selector (0 keeps by count/days, advanced GFS rules use other values)"`
	KeepRecent  int    `json:"keep_recent" jsonschema:"Number of most recent snapshots always kept"`
	RetainDays  int    `json:"retain_days" jsonschema:"Days a snapshot is retained under the basic policy"`
	Hourly      int    `json:"hourly" jsonschema:"GFS rule: hourly snapshots kept"`
	Daily       int    `json:"daily" jsonschema:"GFS rule: daily snapshots kept"`
	Weekly      int    `json:"weekly" jsonschema:"GFS rule: weekly snapshots kept"`
	Monthly     int    `json:"monthly" jsonschema:"GFS rule: monthly snapshots kept"`
	Yearly      int    `json:"yearly" jsonschema:"GFS rule: yearly snapshots kept"`
	Scheduled   bool   `json:"scheduled" jsonschema:"Whether a snapshot schedule is attached to this policy"`
}

// NodeIdentity is the local replication-node identity DSM reports for this NAS.
type NodeIdentity struct {
	Hostname string `json:"hostname,omitempty" jsonschema:"NAS hostname"`
	NodeID   string `json:"node_id,omitempty" jsonschema:"Replication node UUID"`
	Serial   string `json:"serial,omitempty" jsonschema:"NAS serial number"`
}

// ReplicationSiteInfo is one end of a replication relation (main or DR site).
type ReplicationSiteInfo struct {
	Hostname   string `json:"hostname,omitempty" jsonschema:"Site NAS hostname"`
	NodeID     string `json:"node_id,omitempty" jsonschema:"Site replication node UUID"`
	TargetName string `json:"target_name,omitempty" jsonschema:"Replicated target name at this site"`
	Status     string `json:"status,omitempty" jsonschema:"Site status when reported (DR site)"`
}

// ReplicationCapabilities reports which role-flipping operations DSM says are
// currently possible for a relation. This module surfaces them read-only —
// executing failover/switchover/reprotect is deliberately out of scope.
type ReplicationCapabilities struct {
	CanSync       bool `json:"can_sync" jsonschema:"Whether a manual sync is currently possible"`
	CanEdit       bool `json:"can_edit" jsonschema:"Whether the relation can be edited"`
	CanDelete     bool `json:"can_delete" jsonschema:"Whether the relation can be deleted"`
	CanSwitchover bool `json:"can_switchover" jsonschema:"Whether a planned switchover is currently possible (not executable here)"`
	CanFailover   bool `json:"can_failover" jsonschema:"Whether a failover is currently possible (not executable here)"`
	CanReprotect  bool `json:"can_reprotect" jsonschema:"Whether re-protect is currently possible (not executable here)"`
	CanTestFailover bool `json:"can_test_failover" jsonschema:"Whether a test failover is currently possible (not executable here)"`
}

// ReplicationPlan is one replication relation, live-verified against a real
// nas51→nas255 relation on DSM 7.3.2/7.3.1. Per-plan fields decode tolerantly
// (every additional block is null-guarded in the DSM UI).
type ReplicationPlan struct {
	ID            string                  `json:"id,omitempty" jsonschema:"Replication plan identifier"`
	RemoteID      string                  `json:"remote_id,omitempty" jsonschema:"Plan identifier at the remote site"`
	Role          string                  `json:"role,omitempty" jsonschema:"This NAS's role in the relation: main or dr"`
	Status        string                  `json:"status,omitempty" jsonschema:"Plan status as reported by DSM"`
	TargetName    string                  `json:"target_name,omitempty" jsonschema:"Replicated shared-folder (or LUN) name"`
	TargetType    string                  `json:"target_type,omitempty" jsonschema:"Protected target type: share or lun"`
	SnapshotCount int                     `json:"snapshot_count,omitempty" jsonschema:"Replicated snapshot count when reported"`
	MainSite      ReplicationSiteInfo     `json:"main_site" jsonschema:"The main (source) site"`
	DRSite        ReplicationSiteInfo     `json:"dr_site" jsonschema:"The DR (destination) site"`
	LastSyncTime  string                  `json:"last_sync_time,omitempty" jsonschema:"Most recent successful sync time when reported"`
	LastSyncBytes int64                   `json:"last_sync_bytes,omitempty" jsonschema:"Bytes moved in the most recent successful sync"`
	Can           ReplicationCapabilities `json:"can" jsonschema:"Operations DSM currently reports as possible (read-only)"`
}

// RelationTaskStatus is one poll of an in-flight create task.
type RelationTaskStatus struct {
	Finished     bool   `json:"finished" jsonschema:"Whether the create task has finished"`
	Success      bool   `json:"success" jsonschema:"Whether the finished task succeeded"`
	PlanID       string `json:"plan_id,omitempty" jsonschema:"Created plan id (present on success)"`
	RemotePlanID string `json:"remote_plan_id,omitempty" jsonschema:"Created plan id at the remote site (present on success)"`
	TargetID     string `json:"target_id,omitempty" jsonschema:"Target id this task created a relation for"`
	Error        string `json:"error,omitempty" jsonschema:"Task error text when the task failed"`
}

// RelationCreate is the guarded intent to create a shared-folder replication
// relation from a source NAS to a destination NAS. The source and destination
// are named by profile in the application plan; this carries only the
// replication parameters. There is no credential field: the destination
// credential is resolved from its own vault profile at apply time.
type RelationCreate struct {
	SourceShare  string `json:"source_share" jsonschema:"Shared folder on the source NAS to replicate"`
	DestVolume   string `json:"dest_volume" jsonschema:"Destination volume path that will hold the replica, for example /volume1"`
	SendEncrypted *bool `json:"send_encrypted,omitempty" jsonschema:"Encrypt replication traffic; defaults to true when the destination is reached over HTTPS"`
}

// ReplicationPlans is the replication relation inventory.
type ReplicationPlans struct {
	Total int               `json:"total" jsonschema:"Total plans reported; falls back to the item count when absent"`
	Plans []ReplicationPlan `json:"plans" jsonschema:"Replication plans reported by DSM"`
}

// LogEntry is one Snapshot Replication log record.
type LogEntry struct {
	Time    string `json:"time,omitempty" jsonschema:"Entry time as reported by DSM (for example 2026/07/21 02:11:05)"`
	Level   string `json:"level,omitempty" jsonschema:"Entry level: info, warn, or error"`
	User    string `json:"user,omitempty" jsonschema:"Account that performed the logged action"`
	Message string `json:"message,omitempty" jsonschema:"Log text"`
}

// LogPage is one page of the Snapshot Replication log feed.
type LogPage struct {
	Total      int        `json:"total" jsonschema:"Total matching entries reported by DSM"`
	ErrorCount int        `json:"error_count" jsonschema:"Total error entries reported"`
	WarnCount  int        `json:"warn_count" jsonschema:"Total warning entries reported"`
	InfoCount  int        `json:"info_count" jsonschema:"Total information entries reported"`
	Entries    []LogEntry `json:"entries" jsonschema:"Entries in this page"`
}

// ShareOverview summarizes one snapshot-capable shared folder for the module
// state view.
type ShareOverview struct {
	Share            string `json:"share" jsonschema:"Shared-folder name"`
	VolumePath       string `json:"volume_path,omitempty" jsonschema:"Volume containing the shared folder"`
	Total            int    `json:"total" jsonschema:"Snapshot count"`
	Latest           string `json:"latest,omitempty" jsonschema:"Most recent snapshot time name when any exist"`
	SnapshotBrowsing bool   `json:"snapshot_browsing" jsonschema:"Whether snapshot browsing is enabled for users"`
	RetentionTask    bool   `json:"retention_task" jsonschema:"Whether a retention task is configured for the share"`
}

// Change actions supported by the guarded snapshot mutation surface.
const (
	ActionCreate         = "create"
	ActionSetAttributes  = "set_attributes"
	ActionDelete         = "delete"
	ActionSetShareConfig = "set_share_config"
)

// Change is the guarded snapshot mutation intent. Exactly one action applies
// per change; nil fields are preserved (patch semantics).
type Change struct {
	Action           string   `json:"action" jsonschema:"Change action: create, set_attributes, delete, or set_share_config"`
	Share            string   `json:"share" jsonschema:"Shared-folder name the change applies to"`
	Snapshot         string   `json:"snapshot,omitempty" jsonschema:"Snapshot time name for set_attributes"`
	Snapshots        []string `json:"snapshots,omitempty" jsonschema:"Snapshot time names to delete for delete"`
	Description      *string  `json:"description,omitempty" jsonschema:"Snapshot description for create or set_attributes; empty clears it"`
	Lock             *bool    `json:"lock,omitempty" jsonschema:"Lock state for create or set_attributes; omitted on create uses the DSM default (locked)"`
	SnapshotBrowsing *bool    `json:"snapshot_browsing,omitempty" jsonschema:"Enable user snapshot browsing for set_share_config"`
	LocalTimeFormat  *bool    `json:"local_time_format,omitempty" jsonschema:"Use local-time snapshot names for set_share_config"`
}

// Capabilities reports which Snapshot Replication operations dsmctl exposes on
// the target NAS.
type Capabilities struct {
	Module                string          `json:"module" jsonschema:"Stable module name: snapshot-replication"`
	Package               PackageEvidence `json:"package" jsonschema:"Installed SnapshotReplication package evidence (required only for replication)"`
	SnapshotsRead         bool            `json:"snapshots_read" jsonschema:"Whether share snapshots can be listed"`
	ShareConfigRead       bool            `json:"share_config_read" jsonschema:"Whether per-share snapshot configuration can be read"`
	RetentionRead         bool            `json:"retention_read" jsonschema:"Whether retention policies can be read"`
	LogRead               bool            `json:"log_read" jsonschema:"Whether the Snapshot Replication log feed can be read"`
	NodeRead              bool            `json:"node_read" jsonschema:"Whether the local replication node identity can be read"`
	ReplicationRead       bool            `json:"replication_read" jsonschema:"Whether replication plans can be listed (requires the SnapshotReplication package)"`
	SnapshotCreate        bool            `json:"snapshot_create" jsonschema:"Whether snapshots can be taken through guarded plan/apply"`
	SnapshotSetAttributes bool            `json:"snapshot_set_attributes" jsonschema:"Whether snapshot description/lock can be edited through guarded plan/apply"`
	SnapshotDelete        bool            `json:"snapshot_delete" jsonschema:"Whether snapshots can be deleted through guarded plan/apply"`
	ShareConfigSet        bool            `json:"share_config_set" jsonschema:"Whether per-share snapshot configuration can be changed through guarded plan/apply"`
	ReplicationPair       bool            `json:"replication_pair" jsonschema:"Whether the DR pairing credential API is available (requires the package)"`
	ReplicationCreate     bool            `json:"replication_create" jsonschema:"Whether a replication relation can be created through guarded plan/apply (requires the package)"`
	ReplicationManage     bool            `json:"replication_manage" jsonschema:"Whether an existing relation can be synced/stopped by plan id (requires the package)"`
}
