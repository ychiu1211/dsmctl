// Package universalsearch contains stable, read-only models for the Synology
// Universal Search package (internal name Finder / SynoFinder): the list of
// file-index folders, each folder's paused/content-type state, and the overall
// index daemon status.
//
// DSM WebAPI names, versions, and field names stay behind the operation package,
// and the installed Universal Search package version is carried as evidence
// because the SYNO.Finder.* WebAPI follows the package release. This package
// manages the file INDEX (indexed folders + status); it is not a search client
// and does not model the search-experience settings (see the WI-080 non-goals).
package universalsearch

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "universal-search"

// PackageEvidence reports the installed Universal Search (SynoFinder) package.
type PackageEvidence struct {
	ID        string `json:"id" jsonschema:"DSM package identifier: SynoFinder"`
	Installed bool   `json:"installed" jsonschema:"Whether the Universal Search package is installed"`
	Version   string `json:"version,omitempty" jsonschema:"Installed package version"`
	Running   bool   `json:"running" jsonschema:"Whether the Universal Search index service is running"`
}

// ContentTypes reports which media/content categories are indexed for a folder.
type ContentTypes struct {
	Audio    bool `json:"audio" jsonschema:"Whether audio files in this folder are indexed"`
	Video    bool `json:"video" jsonschema:"Whether video files in this folder are indexed"`
	Photo    bool `json:"photo" jsonschema:"Whether photos in this folder are indexed"`
	Document bool `json:"document" jsonschema:"Whether documents in this folder are indexed"`
}

// IndexedFolder is one folder in the Universal Search file index. The folder
// path is the stable identifier (there is no separate numeric id); owner is the
// app or subsystem that registered the folder (for example SynologyDrive). The
// shape is live-verified on SynoFinder 1.9.0; unknown extra fields are ignored
// so the model tolerates version drift.
type IndexedFolder struct {
	Path                 string       `json:"path" jsonschema:"Indexed folder path; the stable identifier for the folder"`
	Name                 string       `json:"name,omitempty" jsonschema:"Display name of the indexed folder"`
	Owner                string       `json:"owner,omitempty" jsonschema:"App or subsystem that registered the folder, such as SynologyDrive"`
	Group                string       `json:"group,omitempty" jsonschema:"DSM display-name group key the owning app reports"`
	Paused               bool         `json:"paused" jsonschema:"Whether indexing of this folder is paused; a paused folder is excluded from indexing until resumed"`
	Privileged           bool         `json:"privileged" jsonschema:"Whether the folder is a privileged (system/app-owned) index entry"`
	SharePathBeforePause string       `json:"share_path_before_pause,omitempty" jsonschema:"Share path recorded before the folder was paused, when present"`
	ContentTypes         ContentTypes `json:"content_types" jsonschema:"Which content categories are indexed for this folder"`
}

// IndexedFolders is the Universal Search indexed-folder list.
type IndexedFolders struct {
	Total   int             `json:"total" jsonschema:"Total number of indexed folders reported"`
	Folders []IndexedFolder `json:"folders" jsonschema:"Indexed folders; empty when none"`
	Package PackageEvidence `json:"package" jsonschema:"Installed Universal Search package evidence"`
}

// IndexStatus is the overall Universal Search index daemon status, read from
// SYNO.Finder.FileIndexing.Status get. The two sub-states are DSM's raw status
// strings ("finished" when idle; other values such as an in-progress state are
// passed through verbatim). Progress/queued-count/last-time fields are captured
// tolerantly only when the running index reports them.
type IndexStatus struct {
	Index    string          `json:"index,omitempty" jsonschema:"File-content index state; finished when idle, otherwise DSM's raw in-progress state"`
	Term     string          `json:"term,omitempty" jsonschema:"Search-term index state; finished when idle, otherwise DSM's raw in-progress state"`
	Indexing bool            `json:"indexing" jsonschema:"Whether the index is currently working (any sub-state is not finished)"`
	Progress *int            `json:"progress,omitempty" jsonschema:"Overall index progress percentage, when the running index reports it"`
	Package  PackageEvidence `json:"package" jsonschema:"Installed Universal Search package evidence"`
}

// Capabilities reports which Universal Search reads dsmctl exposes for the
// installed package. Each area selects its backend independently and is gated on
// the installed SynoFinder package.
type Capabilities struct {
	Module     string          `json:"module" jsonschema:"Stable module name: universal-search"`
	Package    PackageEvidence `json:"package" jsonschema:"Installed Universal Search package evidence"`
	FolderRead bool            `json:"folder_read" jsonschema:"Whether the indexed-folder list can be read"`
	StatusRead bool            `json:"status_read" jsonschema:"Whether the overall index status can be read"`
}
