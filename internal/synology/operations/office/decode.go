package office

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/office"
)

// DSM field names for the Synology Office settings surface, confirmed live on
// Synology Office 3.7.2-22592 and against the Office 3.7 WebAPI definitions.
const (
	keyIsManager         = "is_manager"
	keyVersion           = "version"
	keySchemaDocument    = "schema_doc"
	keySchemaSpreadsheet = "schema_sheet"
	keySchemaSlides      = "schema_slide"

	keyHistoryPrune = "history_prune"

	keyRuler                = "ruler"
	keyFormulaPreview       = "formula_preview"
	keyFormulaPanelOpened   = "formula_panel_opened"
	keyFormulaPanelExpanded = "formula_panel_expanded"
	keyDefaultLocale        = "default_locale"
	keyAITranslatorLanguage = "ai_translator_language"
	keyAIHelperLanguages    = "ai_helper_languages"

	keyFontDisplay  = "display"
	keyFontSystem   = "system"
	keyFontDisabled = "disable"
	keyFonts        = "fonts"
)

func decodeInfo(data json.RawMessage) (office.Info, error) {
	raw, err := decodeObject(data, "Office info")
	if err != nil {
		return office.Info{}, err
	}
	// version carries the component parts as strings; require it to catch API
	// drift. The rest decode leniently.
	versionValue, ok := raw[keyVersion]
	if !ok {
		return office.Info{}, fmt.Errorf("decode Office info: required field %q is missing", keyVersion)
	}
	var version struct {
		Major  string `json:"major"`
		Minor  string `json:"minor"`
		Hotfix string `json:"hotfix"`
		Build  string `json:"build"`
	}
	if err := json.Unmarshal(versionValue, &version); err != nil {
		return office.Info{}, fmt.Errorf("decode Office info field %q: %w", keyVersion, err)
	}
	rendered := version.Major
	if version.Minor != "" {
		rendered += "." + version.Minor
	}
	if version.Hotfix != "" {
		rendered += "." + version.Hotfix
	}
	if version.Build != "" {
		rendered += "-" + version.Build
	}
	if rendered == "" {
		return office.Info{}, fmt.Errorf("decode Office info: empty %q object", keyVersion)
	}
	return office.Info{
		Version:           rendered,
		IsManager:         optionalBool(raw, keyIsManager),
		SchemaDocument:    optionalInt(raw, keySchemaDocument),
		SchemaSpreadsheet: optionalInt(raw, keySchemaSpreadsheet),
		SchemaSlides:      optionalInt(raw, keySchemaSlides),
	}, nil
}

func decodeSystemSettings(data json.RawMessage) (office.SystemSettings, error) {
	raw, err := decodeObject(data, "Office system settings")
	if err != nil {
		return office.SystemSettings{}, err
	}
	// history_prune is the whole system surface; require it to catch API drift.
	prune, err := requiredBool(raw, keyHistoryPrune, "Office system settings")
	if err != nil {
		return office.SystemSettings{}, err
	}
	return office.SystemSettings{HistoryPrune: prune}, nil
}

func decodePreferences(data json.RawMessage) (office.Preferences, error) {
	raw, err := decodeObject(data, "Office preferences")
	if err != nil {
		return office.Preferences{}, err
	}
	// ruler is a stable core field; require it to catch API drift. The rest
	// decode leniently so a newer Office that drops or adds a field does not
	// fail the whole read.
	ruler, err := requiredBool(raw, keyRuler, "Office preferences")
	if err != nil {
		return office.Preferences{}, err
	}
	return office.Preferences{
		Ruler:                ruler,
		FormulaPreview:       optionalBool(raw, keyFormulaPreview),
		FormulaPanelOpened:   optionalBool(raw, keyFormulaPanelOpened),
		FormulaPanelExpanded: optionalBool(raw, keyFormulaPanelExpanded),
		DefaultLocale:        optionalString(raw, keyDefaultLocale),
		AITranslatorLanguage: optionalString(raw, keyAITranslatorLanguage),
		AIHelperLanguages:    optionalStringSlice(raw, keyAIHelperLanguages),
	}, nil
}

// decodeFonts normalizes the font inventory, a JSON object keyed by font name,
// into a stable name-sorted slice. A system entry is `{}` or `{"display":..}`;
// a custom entry carries `"system": false` and, when disabled,
// `"disable": true` (live-verified on 3.7.2).
func decodeFonts(data json.RawMessage) ([]office.Font, error) {
	raw, err := decodeObject(data, "Office fonts")
	if err != nil {
		return nil, err
	}
	fonts := make([]office.Font, 0, len(raw))
	for name, value := range raw {
		var detail struct {
			Display  string `json:"display"`
			System   *bool  `json:"system"`
			Disabled bool   `json:"disable"`
		}
		if err := json.Unmarshal(value, &detail); err != nil {
			return nil, fmt.Errorf("decode Office font %q: %w", name, err)
		}
		fonts = append(fonts, office.Font{
			Name:        name,
			DisplayName: detail.Display,
			Custom:      detail.System != nil && !*detail.System,
			Enabled:     !detail.Disabled,
		})
	}
	sort.Slice(fonts, func(i, j int) bool { return fonts[i].Name < fonts[j].Name })
	return fonts, nil
}

// encodeFontChange builds the `fonts` parameter: DSM expects a JSON array of
// font family names (an array of objects is rejected with error 120).
func encodeFontChange(change office.FontChange) map[string]any {
	return map[string]any{keyFonts: change.Names}
}

// encodeSystemChange builds a partial set: only fields present in the patch are
// sent, so DSM preserves the rest.
func encodeSystemChange(change office.SystemChange) map[string]any {
	parameters := map[string]any{}
	if change.HistoryPrune != nil {
		parameters[keyHistoryPrune] = *change.HistoryPrune
	}
	return parameters
}

// encodePreferencesChange builds a partial set: only fields present in the
// patch are sent, so DSM preserves the rest.
func encodePreferencesChange(change office.PreferencesChange) map[string]any {
	parameters := map[string]any{}
	if change.Ruler != nil {
		parameters[keyRuler] = *change.Ruler
	}
	if change.FormulaPreview != nil {
		parameters[keyFormulaPreview] = *change.FormulaPreview
	}
	if change.FormulaPanelOpened != nil {
		parameters[keyFormulaPanelOpened] = *change.FormulaPanelOpened
	}
	if change.FormulaPanelExpanded != nil {
		parameters[keyFormulaPanelExpanded] = *change.FormulaPanelExpanded
	}
	if change.DefaultLocale != nil {
		parameters[keyDefaultLocale] = *change.DefaultLocale
	}
	if change.AITranslatorLanguage != nil {
		parameters[keyAITranslatorLanguage] = *change.AITranslatorLanguage
	}
	if change.AIHelperLanguages != nil {
		parameters[keyAIHelperLanguages] = *change.AIHelperLanguages
	}
	return parameters
}

func decodeObject(data json.RawMessage, what string) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("decode %s: expected a non-empty object", what)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("decode %s object: %w", what, err)
	}
	return raw, nil
}

func requiredBool(raw map[string]json.RawMessage, name, what string) (bool, error) {
	value, ok := raw[name]
	if !ok {
		return false, fmt.Errorf("decode %s: required field %q is missing", what, name)
	}
	result, ok := parseBool(value)
	if !ok {
		return false, fmt.Errorf("decode %s field %q: expected boolean", what, name)
	}
	return result, nil
}

func optionalBool(raw map[string]json.RawMessage, name string) bool {
	if value, ok := raw[name]; ok {
		if result, parsed := parseBool(value); parsed {
			return result
		}
	}
	return false
}

func parseBool(value json.RawMessage) (bool, bool) {
	var result bool
	if err := json.Unmarshal(value, &result); err == nil {
		return result, true
	}
	var integer int
	if err := json.Unmarshal(value, &integer); err == nil && (integer == 0 || integer == 1) {
		return integer == 1, true
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		if parsed, convErr := strconv.Atoi(strings.TrimSpace(text)); convErr == nil && (parsed == 0 || parsed == 1) {
			return parsed == 1, true
		}
	}
	return false, false
}

func optionalInt(raw map[string]json.RawMessage, name string) int {
	if value, ok := raw[name]; ok {
		var integer int
		if err := json.Unmarshal(value, &integer); err == nil {
			return integer
		}
	}
	return 0
}

func optionalString(raw map[string]json.RawMessage, name string) string {
	if value, ok := raw[name]; ok {
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			return text
		}
	}
	return ""
}

func optionalStringSlice(raw map[string]json.RawMessage, name string) []string {
	if value, ok := raw[name]; ok {
		var slice []string
		if err := json.Unmarshal(value, &slice); err == nil {
			return slice
		}
	}
	return []string{}
}
