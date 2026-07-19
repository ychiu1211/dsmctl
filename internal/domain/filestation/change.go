package filestation

// Mutating FileStation actions. Every action that changes NAS state rides the
// plan/apply contract; upload additionally binds the local file's size and hash
// so a plan cannot be applied against a swapped source. Favorites are per-user
// bookmarks handled by direct commands, not this union.
const (
	ActionCreateFolder    = "create_folder"
	ActionRename          = "rename"
	ActionCopy            = "copy"
	ActionMove            = "move"
	ActionDelete          = "delete"
	ActionCompress        = "compress"
	ActionExtract         = "extract"
	ActionUpload          = "upload"
	ActionShareLinkCreate = "sharelink_create"
	ActionShareLinkDelete = "sharelink_delete"
)

// ChangeRequest is the typed union of FileStation mutations. Exactly one payload
// matches Action.
type ChangeRequest struct {
	Action       string              `json:"action" jsonschema:"Mutation: create_folder, rename, copy, move, delete, compress, extract, or upload"`
	CreateFolder *CreateFolderChange `json:"create_folder,omitempty" jsonschema:"Payload when action is create_folder"`
	Rename       *RenameChange       `json:"rename,omitempty" jsonschema:"Payload when action is rename"`
	Transfer     *TransferChange     `json:"transfer,omitempty" jsonschema:"Payload when action is copy or move"`
	Delete       *DeleteChange       `json:"delete,omitempty" jsonschema:"Payload when action is delete"`
	Compress     *CompressChange     `json:"compress,omitempty" jsonschema:"Payload when action is compress"`
	Extract      *ExtractChange      `json:"extract,omitempty" jsonschema:"Payload when action is extract"`
	Upload       *UploadChange       `json:"upload,omitempty" jsonschema:"Payload when action is upload"`
	ShareLink    *ShareLinkChange    `json:"share_link,omitempty" jsonschema:"Payload when action is sharelink_create or sharelink_delete"`
}

// ShareLinkChange creates or deletes a public sharing link. Path is set for
// creation; LinkID is set for deletion.
type ShareLinkChange struct {
	Path        string `json:"path,omitempty" jsonschema:"Absolute path to expose publicly (creation)"`
	LinkID      string `json:"link_id,omitempty" jsonschema:"Sharing-link id to delete (deletion)"`
	PasswordRef string `json:"password_ref,omitempty" jsonschema:"Optional env:NAME reference to a link password (creation)"`
	ExpireDate  string `json:"expire_date,omitempty" jsonschema:"Optional expiry date YYYY-MM-DD (creation)"`
}

// CreateFolderChange creates one folder under an existing parent.
type CreateFolderChange struct {
	Parent        string `json:"parent" jsonschema:"Existing parent folder, for example /home"`
	Name          string `json:"name" jsonschema:"New folder name to create under the parent"`
	CreateParents bool   `json:"create_parents,omitempty" jsonschema:"Create missing intermediate parents"`
}

// RenameChange renames one entry in place.
type RenameChange struct {
	Path    string `json:"path" jsonschema:"Absolute path of the entry to rename"`
	NewName string `json:"new_name" jsonschema:"New base name (not a path)"`
}

// TransferChange copies or moves one or more entries into a destination folder.
type TransferChange struct {
	Sources    []string `json:"sources" jsonschema:"Absolute source paths to copy or move"`
	DestFolder string   `json:"dest_folder" jsonschema:"Absolute destination folder"`
	Overwrite  bool     `json:"overwrite,omitempty" jsonschema:"Overwrite entries that already exist at the destination"`
}

// DeleteChange deletes one or more entries. Deletion is recursive and permanent
// (it does not use the recycle bin).
type DeleteChange struct {
	Paths []string `json:"paths" jsonschema:"Absolute paths to delete permanently"`
}

// CompressChange archives one or more entries into a single archive file.
type CompressChange struct {
	Sources     []string `json:"sources" jsonschema:"Absolute source paths to archive"`
	DestArchive string   `json:"dest_archive" jsonschema:"Absolute destination archive path, for example /home/out.zip"`
	Format      string   `json:"format,omitempty" jsonschema:"Archive format: zip (default) or 7z"`
	Level       string   `json:"level,omitempty" jsonschema:"Compression level: moderate, fast, best, or store"`
	PasswordRef string   `json:"password_ref,omitempty" jsonschema:"Optional env:NAME reference to an archive password"`
}

// ExtractChange extracts an archive into a destination folder.
type ExtractChange struct {
	Archive     string `json:"archive" jsonschema:"Absolute archive path to extract"`
	DestFolder  string `json:"dest_folder" jsonschema:"Absolute destination folder"`
	Overwrite   bool   `json:"overwrite,omitempty" jsonschema:"Overwrite existing files in the destination"`
	PasswordRef string `json:"password_ref,omitempty" jsonschema:"Optional env:NAME reference to the archive password"`
}

// UploadChange uploads a local file into a destination folder. LocalPath is
// resolved on the machine running dsmctl; its size and content hash are bound
// into the plan precondition.
type UploadChange struct {
	LocalPath     string `json:"local_path" jsonschema:"Path to the local file to upload"`
	DestFolder    string `json:"dest_folder" jsonschema:"Absolute destination folder on the NAS"`
	Overwrite     bool   `json:"overwrite,omitempty" jsonschema:"Overwrite an existing destination file"`
	CreateParents bool   `json:"create_parents,omitempty" jsonschema:"Create missing parent folders"`
}

// PathObservation records what was observed at one path during planning, so a
// stale plan (a target that changed, a destination that appeared) is rejected at
// apply time.
type PathObservation struct {
	Path        string `json:"path" jsonschema:"Observed path"`
	Exists      bool   `json:"exists" jsonschema:"Whether the path existed during planning"`
	IsDir       bool   `json:"is_dir,omitempty" jsonschema:"Whether the path was a directory"`
	Size        int64  `json:"size,omitempty" jsonschema:"File size in bytes when observed"`
	Modified    int64  `json:"modified,omitempty" jsonschema:"Modification time in Unix seconds when observed"`
	ContentHash string `json:"content_hash,omitempty" jsonschema:"SHA-256 of a local upload source bound into the plan"`
}

// FilePrecondition is the multi-path observed state a FileStation plan binds to.
// Because it holds slices it is not comparable with ==; apply compares its
// Fingerprint together with the plan hash instead.
type FilePrecondition struct {
	Targets     []PathObservation `json:"targets" jsonschema:"Observed state of the source or target paths"`
	Destination *PathObservation  `json:"destination,omitempty" jsonschema:"Observed state of the destination path when the action has one"`
	ResourceID  string            `json:"resource_id" jsonschema:"Stable identifier: the sorted canonical paths the plan affects"`
	Fingerprint string            `json:"fingerprint" jsonschema:"SHA-256 of the observed target and destination state"`
}

// Favorite is one personal FileStation sidebar bookmark.
type Favorite struct {
	Name   string `json:"name" jsonschema:"Display name of the favorite"`
	Path   string `json:"path" jsonschema:"Absolute path the favorite points to"`
	Status string `json:"status,omitempty" jsonschema:"DSM-reported status, such as valid or broken"`
}

// Favorites is the personal favorite list.
type Favorites struct {
	Total     int        `json:"total" jsonschema:"Total number of favorites reported"`
	Favorites []Favorite `json:"favorites" jsonschema:"Personal favorites; empty when none"`
}
