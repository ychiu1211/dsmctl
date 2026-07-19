package filestation

// Read query parameters. Field names are stable and semantic; the operation
// package maps them onto DSM WebAPI parameter names.

// ListShareQuery selects how shared folders are enumerated.
type ListShareQuery struct {
	Offset        int    `json:"offset,omitempty" jsonschema:"Offset of the first shared folder to return"`
	Limit         int    `json:"limit,omitempty" jsonschema:"Maximum number of shared folders to return; 0 uses the DSM default"`
	OnlyWritable  bool   `json:"only_writable,omitempty" jsonschema:"Return only shared folders the current session can write"`
	SortBy        string `json:"sort_by,omitempty" jsonschema:"Sort key: name, size, user, group, mtime, atime, ctime, crtime, or posix"`
	SortDirection string `json:"sort_direction,omitempty" jsonschema:"Sort direction: asc or desc"`
}

// ListQuery selects how one folder's entries are enumerated.
type ListQuery struct {
	Path          string `json:"path" jsonschema:"Absolute folder path to enumerate, for example /share/dir"`
	Offset        int    `json:"offset,omitempty" jsonschema:"Offset of the first entry to return"`
	Limit         int    `json:"limit,omitempty" jsonschema:"Maximum number of entries to return; 0 uses the DSM default"`
	SortBy        string `json:"sort_by,omitempty" jsonschema:"Sort key: name, size, user, group, mtime, atime, ctime, crtime, posix, or type"`
	SortDirection string `json:"sort_direction,omitempty" jsonschema:"Sort direction: asc or desc"`
	Pattern       string `json:"pattern,omitempty" jsonschema:"Glob pattern that entry names must match"`
	FileType      string `json:"file_type,omitempty" jsonschema:"Restrict to file, dir, or all (default)"`
}

// GetInfoQuery selects the entries whose details are requested.
type GetInfoQuery struct {
	Paths []string `json:"paths" jsonschema:"Absolute paths whose information is requested"`
}

// SearchQuery selects how a folder subtree is searched.
type SearchQuery struct {
	Path      string `json:"path" jsonschema:"Absolute folder path to search within"`
	Pattern   string `json:"pattern,omitempty" jsonschema:"Glob pattern that entry names must match"`
	Extension string `json:"extension,omitempty" jsonschema:"File extension filter, without a leading dot"`
	FileType  string `json:"file_type,omitempty" jsonschema:"Restrict to file, dir, or all (default)"`
	Recursive bool   `json:"recursive,omitempty" jsonschema:"Search subdirectories recursively"`
}

// DirSizeQuery selects the paths whose aggregate size is computed.
type DirSizeQuery struct {
	Paths []string `json:"paths" jsonschema:"Absolute folder paths whose aggregate size is computed"`
}

// MD5Query selects the file whose MD5 digest is computed.
type MD5Query struct {
	Path string `json:"path" jsonschema:"Absolute file path whose MD5 digest is computed"`
}

// VirtualFolderQuery selects how mounted virtual folders are enumerated.
type VirtualFolderQuery struct {
	Offset int `json:"offset,omitempty" jsonschema:"Offset of the first virtual folder to return"`
	Limit  int `json:"limit,omitempty" jsonschema:"Maximum number of virtual folders to return; 0 uses the DSM default"`
}

// CheckPermissionQuery probes whether a write is permitted at a path.
type CheckPermissionQuery struct {
	Path          string `json:"path" jsonschema:"Absolute folder path where a write is probed"`
	Filename      string `json:"filename,omitempty" jsonschema:"Optional file name to probe within the folder"`
	Overwrite     bool   `json:"overwrite,omitempty" jsonschema:"Probe assuming an existing file would be overwritten"`
	CreateParents bool   `json:"create_parents,omitempty" jsonschema:"Probe assuming missing parent folders would be created"`
}
