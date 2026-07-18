package photos

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/photos"
)

// DSM field names for SYNO.Foto.Setting.Admin, confirmed live on Synology Photos
// 1.9.1.
const (
	keyFaceRecognition       = "enable_person"
	keyConceptGrouping       = "enable_concept"
	keySimilarGrouping       = "enable_similar"
	keyUserSharing           = "enable_user_sharing"
	keyShowInfoToGuest       = "display_photo_info_to_guest"
	keyPersonalRecycleBin    = "enable_personal_dsm_recycle_bin"
	keySharedRecycleBin      = "enable_shared_dsm_recycle_bin"
	keyConvertedOriginalJPEG = "enable_converted_original_jpeg"
	keyNeedHEVC              = "need_hevc"
	keyDefaultThumbnailSize  = "default_thumbnail_size"
	keyExcludeExtensions     = "exclude_extension"
	keyPackageVersion        = "package_version"
)

func decodeAdminSettings(data json.RawMessage) (photos.AdminSettings, error) {
	raw, err := decodeObject(data)
	if err != nil {
		return photos.AdminSettings{}, err
	}
	// enable_person is a stable core field; require it to catch API drift. The
	// rest decode leniently so a newer Photos that drops or adds a toggle does
	// not fail the whole read.
	face, err := requiredBool(raw, keyFaceRecognition)
	if err != nil {
		return photos.AdminSettings{}, err
	}
	return photos.AdminSettings{
		FaceRecognition:       face,
		ConceptGrouping:       optionalBool(raw, keyConceptGrouping),
		SimilarGrouping:       optionalBool(raw, keySimilarGrouping),
		UserSharing:           optionalBool(raw, keyUserSharing),
		ShowInfoToGuest:       optionalBool(raw, keyShowInfoToGuest),
		PersonalRecycleBin:    optionalBool(raw, keyPersonalRecycleBin),
		SharedRecycleBin:      optionalBool(raw, keySharedRecycleBin),
		ConvertedOriginalJPEG: optionalBool(raw, keyConvertedOriginalJPEG),
		NeedHEVC:              optionalBool(raw, keyNeedHEVC),
		DefaultThumbnailSize:  optionalString(raw, keyDefaultThumbnailSize),
		ExcludeExtensions:     optionalStringSlice(raw, keyExcludeExtensions),
		PackageVersion:        optionalString(raw, keyPackageVersion),
	}, nil
}

// encodeAdminChange builds a partial set: only fields present in the patch are
// sent, so DSM preserves the rest.
func encodeAdminChange(change photos.AdminChange) map[string]any {
	parameters := map[string]any{}
	if change.FaceRecognition != nil {
		parameters[keyFaceRecognition] = *change.FaceRecognition
	}
	if change.ConceptGrouping != nil {
		parameters[keyConceptGrouping] = *change.ConceptGrouping
	}
	if change.SimilarGrouping != nil {
		parameters[keySimilarGrouping] = *change.SimilarGrouping
	}
	if change.UserSharing != nil {
		parameters[keyUserSharing] = *change.UserSharing
	}
	if change.ShowInfoToGuest != nil {
		parameters[keyShowInfoToGuest] = *change.ShowInfoToGuest
	}
	if change.PersonalRecycleBin != nil {
		parameters[keyPersonalRecycleBin] = *change.PersonalRecycleBin
	}
	if change.SharedRecycleBin != nil {
		parameters[keySharedRecycleBin] = *change.SharedRecycleBin
	}
	if change.ConvertedOriginalJPEG != nil {
		parameters[keyConvertedOriginalJPEG] = *change.ConvertedOriginalJPEG
	}
	if change.NeedHEVC != nil {
		parameters[keyNeedHEVC] = *change.NeedHEVC
	}
	if change.DefaultThumbnailSize != nil {
		parameters[keyDefaultThumbnailSize] = *change.DefaultThumbnailSize
	}
	if change.ExcludeExtensions != nil {
		parameters[keyExcludeExtensions] = *change.ExcludeExtensions
	}
	return parameters
}

func decodeObject(data json.RawMessage) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("decode Photos admin settings: expected a non-empty object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("decode Photos admin settings object: %w", err)
	}
	return raw, nil
}

func requiredBool(raw map[string]json.RawMessage, name string) (bool, error) {
	value, ok := raw[name]
	if !ok {
		return false, fmt.Errorf("decode Photos admin settings: required field %q is missing", name)
	}
	result, ok := parseBool(value)
	if !ok {
		return false, fmt.Errorf("decode Photos admin settings field %q: expected boolean", name)
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
