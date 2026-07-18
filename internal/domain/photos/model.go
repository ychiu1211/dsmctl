// Package photos contains stable, DSM/package-version-independent models for the
// Synology Photos package administration surface (SYNO.Foto.Setting.Admin). DSM
// request field names stay behind the operation package so these contracts
// remain stable.
package photos

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "photos"

// PackageEvidence reports the installed SynologyPhotos package as observed
// during a read, so an installed-but-stopped package can be explained.
type PackageEvidence struct {
	ID        string `json:"id" jsonschema:"DSM package identifier (SynologyPhotos)"`
	Installed bool   `json:"installed" jsonschema:"Whether the Synology Photos package is installed"`
	Version   string `json:"version,omitempty" jsonschema:"Installed package version"`
	Running   bool   `json:"running" jsonschema:"Whether the Synology Photos service is running"`
}

// AdminSettings is the normalized Synology Photos administration configuration.
type AdminSettings struct {
	FaceRecognition       bool     `json:"face_recognition" jsonschema:"Group photos by people/faces (enable_person)"`
	ConceptGrouping       bool     `json:"concept_grouping" jsonschema:"Group photos by subject/concept (enable_concept)"`
	SimilarGrouping       bool     `json:"similar_grouping" jsonschema:"Group similar photos (enable_similar)"`
	UserSharing           bool     `json:"user_sharing" jsonschema:"Allow users to share photos and albums (enable_user_sharing)"`
	ShowInfoToGuest       bool     `json:"show_info_to_guest" jsonschema:"Show photo information to guests (display_photo_info_to_guest)"`
	PersonalRecycleBin    bool     `json:"personal_recycle_bin" jsonschema:"Enable the recycle bin for personal space"`
	SharedRecycleBin      bool     `json:"shared_recycle_bin" jsonschema:"Enable the recycle bin for shared space"`
	ConvertedOriginalJPEG bool     `json:"converted_original_jpeg" jsonschema:"Keep a converted original JPEG (enable_converted_original_jpeg)"`
	NeedHEVC              bool     `json:"need_hevc" jsonschema:"Whether HEVC support is required (need_hevc)"`
	DefaultThumbnailSize  string   `json:"default_thumbnail_size" jsonschema:"Default thumbnail size (e.g. sm)"`
	ExcludeExtensions     []string `json:"exclude_extensions" jsonschema:"File extensions excluded from indexing"`
	PackageVersion        string   `json:"package_version,omitempty" jsonschema:"Installed Synology Photos version (read-only)"`
}

// Capabilities reports which Photos administration operations dsmctl exposes for
// the installed package.
type Capabilities struct {
	Module      string          `json:"module" jsonschema:"Stable module name: photos"`
	AdminRead   bool            `json:"admin_read" jsonschema:"Whether the administration settings can be read"`
	AdminSet    bool            `json:"admin_set" jsonschema:"Whether the administration settings can be changed through guarded plan/apply"`
	Package     PackageEvidence `json:"package" jsonschema:"Installed Synology Photos package evidence"`
}

// AdminChange is a patch: an omitted (nil) field preserves its current DSM value.
// PackageVersion is read-only and not writable.
type AdminChange struct {
	FaceRecognition       *bool     `json:"face_recognition,omitempty" jsonschema:"Enable or disable people/face grouping"`
	ConceptGrouping       *bool     `json:"concept_grouping,omitempty" jsonschema:"Enable or disable subject/concept grouping"`
	SimilarGrouping       *bool     `json:"similar_grouping,omitempty" jsonschema:"Enable or disable similar-photo grouping"`
	UserSharing           *bool     `json:"user_sharing,omitempty" jsonschema:"Enable or disable user sharing"`
	ShowInfoToGuest       *bool     `json:"show_info_to_guest,omitempty" jsonschema:"Show or hide photo info to guests"`
	PersonalRecycleBin    *bool     `json:"personal_recycle_bin,omitempty" jsonschema:"Enable or disable the personal-space recycle bin"`
	SharedRecycleBin      *bool     `json:"shared_recycle_bin,omitempty" jsonschema:"Enable or disable the shared-space recycle bin"`
	ConvertedOriginalJPEG *bool     `json:"converted_original_jpeg,omitempty" jsonschema:"Enable or disable keeping a converted original JPEG"`
	NeedHEVC              *bool     `json:"need_hevc,omitempty" jsonschema:"Enable or disable required HEVC support"`
	DefaultThumbnailSize  *string   `json:"default_thumbnail_size,omitempty" jsonschema:"Set the default thumbnail size"`
	ExcludeExtensions     *[]string `json:"exclude_extensions,omitempty" jsonschema:"Replace the excluded-extension list"`
}
