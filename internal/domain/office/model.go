// Package office contains stable, DSM/package-version-independent models for
// the Synology Office settings surface (SYNO.Office.Info, SYNO.Office.Setting,
// SYNO.Office.Setting.System, SYNO.Office.Setting.Font). DSM request field
// names stay behind the operation package so these contracts remain stable.
package office

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "office"

// PackageEvidence reports the installed Synology Office package (DSM id
// Spreadsheet) as observed during a read, so an installed-but-stopped package
// can be explained.
type PackageEvidence struct {
	ID        string `json:"id" jsonschema:"DSM package identifier (Spreadsheet)"`
	Installed bool   `json:"installed" jsonschema:"Whether the Synology Office package is installed"`
	Version   string `json:"version,omitempty" jsonschema:"Installed package version"`
	Running   bool   `json:"running" jsonschema:"Whether the Synology Office service is running"`
}

// Info is the normalized Synology Office deployment information for the
// session user.
type Info struct {
	Version           string `json:"version" jsonschema:"Synology Office version (major.minor.hotfix-build)"`
	IsManager         bool   `json:"is_manager" jsonschema:"Whether the session user can administer Synology Office"`
	SchemaDocument    int    `json:"schema_document" jsonschema:"Document schema version"`
	SchemaSpreadsheet int    `json:"schema_spreadsheet" jsonschema:"Spreadsheet schema version"`
	SchemaSlides      int    `json:"schema_slides" jsonschema:"Slides schema version"`
}

// SystemSettings is the system-wide Synology Office administration
// configuration.
type SystemSettings struct {
	HistoryPrune bool `json:"history_prune" jsonschema:"Automatically clean up old document version history"`
}

// Preferences is the calling user's own typed Synology Office editor
// preferences. Opaque UI-state blobs (panel widths, dismissed hints, ...) are
// deliberately not modeled.
type Preferences struct {
	Ruler                bool     `json:"ruler" jsonschema:"Show the document ruler"`
	FormulaPreview       bool     `json:"formula_preview" jsonschema:"Preview formula results while editing"`
	FormulaPanelOpened   bool     `json:"formula_panel_opened" jsonschema:"Open the spreadsheet formula panel"`
	FormulaPanelExpanded bool     `json:"formula_panel_expanded" jsonschema:"Expand the spreadsheet formula panel"`
	DefaultLocale        string   `json:"default_locale" jsonschema:"Default locale for new documents (empty means follow DSM)"`
	AITranslatorLanguage string   `json:"ai_translator_language" jsonschema:"AI translator target language (empty means unset)"`
	AIHelperLanguages    []string `json:"ai_helper_languages" jsonschema:"AI helper output languages"`
}

// Font is one entry of the Synology Office font inventory. System fonts ship
// with Office and cannot be changed; custom fonts are name-registered by an
// administrator and can be enabled, disabled, and deleted.
type Font struct {
	Name        string `json:"name" jsonschema:"Font family name"`
	DisplayName string `json:"display_name,omitempty" jsonschema:"Localized display name when it differs from the name"`
	Custom      bool   `json:"custom" jsonschema:"Whether this is an administrator-added custom font (system fonts cannot be changed)"`
	Enabled     bool   `json:"enabled" jsonschema:"Whether the font is offered in the editors"`
}

// Capabilities reports which Office settings operations dsmctl exposes for the
// installed package.
type Capabilities struct {
	Module          string          `json:"module" jsonschema:"Stable module name: office"`
	InfoRead        bool            `json:"info_read" jsonschema:"Whether the deployment info can be read"`
	SystemRead      bool            `json:"system_read" jsonschema:"Whether the system settings can be read"`
	SystemSet       bool            `json:"system_set" jsonschema:"Whether the system settings can be changed through guarded plan/apply"`
	PreferencesRead bool            `json:"preferences_read" jsonschema:"Whether the caller's editor preferences can be read"`
	PreferencesSet  bool            `json:"preferences_set" jsonschema:"Whether the caller's editor preferences can be changed through guarded plan/apply"`
	FontsRead       bool            `json:"fonts_read" jsonschema:"Whether the font inventory can be listed"`
	FontsSet        bool            `json:"fonts_set" jsonschema:"Whether the custom font registry can be changed through guarded plan/apply"`
	Package         PackageEvidence `json:"package" jsonschema:"Installed Synology Office package evidence"`
}

// SystemChange is a patch of the system-wide settings: an omitted (nil) field
// preserves its current DSM value.
type SystemChange struct {
	HistoryPrune *bool `json:"history_prune,omitempty" jsonschema:"Enable or disable automatic version-history cleanup"`
}

// PreferencesChange is a patch of the calling user's own editor preferences:
// an omitted (nil) field preserves its current DSM value.
type PreferencesChange struct {
	Ruler                *bool     `json:"ruler,omitempty" jsonschema:"Show or hide the document ruler"`
	FormulaPreview       *bool     `json:"formula_preview,omitempty" jsonschema:"Enable or disable formula result preview"`
	FormulaPanelOpened   *bool     `json:"formula_panel_opened,omitempty" jsonschema:"Open or close the spreadsheet formula panel"`
	FormulaPanelExpanded *bool     `json:"formula_panel_expanded,omitempty" jsonschema:"Expand or collapse the spreadsheet formula panel"`
	DefaultLocale        *string   `json:"default_locale,omitempty" jsonschema:"Set the default locale for new documents"`
	AITranslatorLanguage *string   `json:"ai_translator_language,omitempty" jsonschema:"Set the AI translator target language"`
	AIHelperLanguages    *[]string `json:"ai_helper_languages,omitempty" jsonschema:"Replace the AI helper output languages"`
}

// FontAction is one custom-font registry operation.
type FontAction string

const (
	FontActionAdd     FontAction = "add"
	FontActionEnable  FontAction = "enable"
	FontActionDisable FontAction = "disable"
	FontActionDelete  FontAction = "delete"
)

// FontChange applies one action to a set of custom font names. System fonts
// cannot be targeted: DSM silently skips them, so dsmctl rejects them during
// planning instead.
type FontChange struct {
	Action FontAction `json:"action" jsonschema:"Custom-font registry action: add, enable, disable, or delete"`
	Names  []string   `json:"names" jsonschema:"Font family names the action applies to"`
}

// Change is one Office settings intent. Exactly one scope must be set: System
// writes the system-wide configuration, Preferences writes the calling user's
// own editor preferences, Fonts manages the custom font name registry.
type Change struct {
	System      *SystemChange      `json:"system,omitempty" jsonschema:"System-wide settings patch (administrators)"`
	Preferences *PreferencesChange `json:"preferences,omitempty" jsonschema:"Calling user's editor preferences patch"`
	Fonts       *FontChange        `json:"fonts,omitempty" jsonschema:"Custom-font registry action (administrators)"`
}
