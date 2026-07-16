package share

const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
	ActionSet    = "set"

	ResourceShare      = "share"
	ResourcePermission = "permission"

	PrincipalUser  = "user"
	PrincipalGroup = "group"

	AccessNone   = "none"
	AccessRead   = "read"
	AccessWrite  = "write"
	AccessDeny   = "deny"
	AccessCustom = "custom"
)

type State struct {
	Shares              []SharedFolder `json:"shares" jsonschema:"DSM shared folders"`
	PermissionsIncluded bool           `json:"permissions_included" jsonschema:"Whether permission bindings were requested and included"`
}

type SharedFolder struct {
	Name                string       `json:"name" jsonschema:"Shared-folder name"`
	UUID                string       `json:"uuid,omitempty" jsonschema:"DSM shared-folder UUID"`
	Description         string       `json:"description,omitempty" jsonschema:"Shared-folder description"`
	VolumePath          string       `json:"volume_path,omitempty" jsonschema:"Volume path containing the shared folder"`
	Hidden              bool         `json:"hidden" jsonschema:"Whether the shared folder is hidden from network browsing"`
	Encrypted           bool         `json:"encrypted" jsonschema:"Whether the shared folder is encrypted"`
	EncryptionAutoMount bool         `json:"encryption_auto_mount" jsonschema:"Whether DSM automatically mounts the encrypted shared folder"`
	ACLMode             bool         `json:"acl_mode" jsonschema:"Whether Windows ACL mode is enabled"`
	UnifiedPermissions  bool         `json:"unified_permissions" jsonschema:"Whether DSM unified permissions are enabled"`
	USB                 bool         `json:"usb" jsonschema:"Whether this is a USB-device share"`
	SnapshotSupported   bool         `json:"snapshot_supported" jsonschema:"Whether DSM reports snapshot support"`
	QuotaBytes          uint64       `json:"quota_bytes,omitempty" jsonschema:"Configured shared-folder quota in bytes"`
	QuotaUsedBytes      uint64       `json:"quota_used_bytes,omitempty" jsonschema:"Shared-folder quota usage in bytes"`
	Permissions         []Permission `json:"permissions" jsonschema:"Explicit and inherited user or group permission bindings when requested"`
}

type Permission struct {
	PrincipalType string `json:"principal_type" jsonschema:"Principal kind: user or group"`
	Principal     string `json:"principal" jsonschema:"DSM user or group name"`
	Access        string `json:"access" jsonschema:"Normalized access: none, read, write, deny, or custom"`
	Inherited     bool   `json:"inherited" jsonschema:"Whether DSM reports the permission as inherited"`
	Custom        bool   `json:"custom" jsonschema:"Whether DSM reports a custom ACL"`
	Masked        bool   `json:"masked" jsonschema:"Whether DSM masks this permission entry"`
	ACLMode       bool   `json:"acl_mode" jsonschema:"Whether the share uses ACL mode for this permission result"`
}

type Capabilities struct {
	InventoryRead   bool `json:"inventory_read" jsonschema:"Shared-folder inventory can be read"`
	PermissionRead  bool `json:"permission_read" jsonschema:"User and group permissions can be read"`
	ShareCreate     bool `json:"share_create" jsonschema:"Shared folders can be created through guarded plan/apply"`
	ShareUpdate     bool `json:"share_update" jsonschema:"Shared folders can be updated through guarded plan/apply"`
	ShareDelete     bool `json:"share_delete" jsonschema:"Shared folders can be deleted through guarded plan/apply"`
	PermissionWrite bool `json:"permission_write" jsonschema:"Shared-folder permissions can be changed through guarded plan/apply"`
	Mutations       bool `json:"mutations" jsonschema:"Any shared-folder mutation is currently exposed"`
}

type ChangeRequest struct {
	Action     string            `json:"action" jsonschema:"Change action: create, update, delete, or set"`
	Resource   string            `json:"resource" jsonschema:"Share resource: share or permission"`
	Share      *ShareChange      `json:"share,omitempty" jsonschema:"Shared-folder change when resource is share"`
	Permission *PermissionChange `json:"permission,omitempty" jsonschema:"Permission change when resource is permission"`
}

type ShareChange struct {
	Name                string  `json:"name" jsonschema:"Current or new shared-folder name"`
	NewName             *string `json:"new_name,omitempty" jsonschema:"New shared-folder name for an update"`
	VolumePath          string  `json:"volume_path,omitempty" jsonschema:"Target volume path for creation, for example /volume1"`
	Description         *string `json:"description,omitempty" jsonschema:"Shared-folder description; empty clears it"`
	Hidden              *bool   `json:"hidden,omitempty" jsonschema:"Hide the shared folder from network browsing"`
	RecycleBin          *bool   `json:"recycle_bin,omitempty" jsonschema:"Enable the shared-folder recycle bin"`
	RecycleBinAdminOnly *bool   `json:"recycle_bin_admin_only,omitempty" jsonschema:"Restrict recycle-bin access to administrators"`
	HideUnreadable      *bool   `json:"hide_unreadable,omitempty" jsonschema:"Hide files the current user cannot read"`
	EnableCOW           *bool   `json:"enable_cow,omitempty" jsonschema:"Enable Btrfs copy-on-write for the shared folder"`
	EnableCompression   *bool   `json:"enable_compression,omitempty" jsonschema:"Enable Btrfs compression for the shared folder"`
	QuotaMiB            *uint64 `json:"quota_mib,omitempty" jsonschema:"Shared-folder quota in MiB; zero disables quota"`
}

type PermissionChange struct {
	PrincipalType string            `json:"principal_type" jsonschema:"Principal kind: user or group"`
	Principal     string            `json:"principal" jsonschema:"Local DSM user or group name"`
	Permissions   []PermissionGrant `json:"permissions" jsonschema:"Shared-folder permission changes for this principal"`
}

type PermissionGrant struct {
	ShareName string `json:"share_name" jsonschema:"Shared-folder name"`
	Access    string `json:"access" jsonschema:"Access to set: none, read, write, or deny"`
}
