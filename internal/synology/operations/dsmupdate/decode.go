package dsmupdate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/dsmupdate"
)

// unmarshalObject rejects an empty or non-object payload before decoding, so a
// silently changed DSM response shape fails loudly instead of yielding a zero
// value.
func unmarshalObject(data json.RawMessage, what string, destination any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return fmt.Errorf("decode %s: empty response", what)
	}
	if trimmed[0] != '{' {
		return fmt.Errorf("decode %s: expected an object", what)
	}
	if err := json.Unmarshal(trimmed, destination); err != nil {
		return fmt.Errorf("decode %s: %w", what, err)
	}
	return nil
}

func decodeLocalStatus(data json.RawMessage) (localStatus, error) {
	var resp struct {
		AllowUpgrade *bool   `json:"allow_upgrade"`
		Status       *string `json:"status"`
	}
	if err := unmarshalObject(data, "DSM update status", &resp); err != nil {
		return localStatus{}, err
	}
	if resp.AllowUpgrade == nil || resp.Status == nil {
		return localStatus{}, errors.New("decode DSM update status: required fields \"allow_upgrade\" and \"status\" are missing")
	}
	return localStatus{
		AllowUpgrade: *resp.AllowUpgrade,
		State:        strings.TrimSpace(*resp.Status),
	}, nil
}

// decodeAvailable decodes the update-server check response, tolerating both the
// flat (v1) {"available":…} shape and the wrapped (v2+)
// {"update":{"available":…,"rss_result":…}} shape. Any additional scalar fields
// DSM returns inside the update object (the offered version, a restart-required
// flag, a criticality/type) are surfaced verbatim under Details by their raw
// DSM key rather than through a guessed typed decoder — no update was available
// on the lab to type them, so nothing is fabricated and nothing is dropped.
func decodeAvailable(data json.RawMessage) (dsmupdate.AvailableUpdate, error) {
	var envelope map[string]json.RawMessage
	if err := unmarshalObject(data, "DSM update-server check", &envelope); err != nil {
		return dsmupdate.AvailableUpdate{}, err
	}
	object := envelope
	if raw, ok := envelope["update"]; ok {
		nested := bytes.TrimSpace(raw)
		if len(nested) == 0 || nested[0] != '{' {
			return dsmupdate.AvailableUpdate{}, errors.New("decode DSM update-server check: \"update\" is not an object")
		}
		object = map[string]json.RawMessage{}
		if err := json.Unmarshal(nested, &object); err != nil {
			return dsmupdate.AvailableUpdate{}, fmt.Errorf("decode DSM update-server check: %w", err)
		}
	}
	availableRaw, ok := object["available"]
	if !ok {
		return dsmupdate.AvailableUpdate{}, errors.New("decode DSM update-server check: required field \"available\" is missing")
	}
	var available bool
	if err := json.Unmarshal(availableRaw, &available); err != nil {
		return dsmupdate.AvailableUpdate{}, fmt.Errorf("decode DSM update-server check: field \"available\": %w", err)
	}
	result := dsmupdate.AvailableUpdate{Checked: true, Available: available}
	details := map[string]string{}
	for key, raw := range object {
		if key == "available" {
			continue
		}
		if key == "rss_result" {
			var value string
			if json.Unmarshal(raw, &value) == nil {
				result.RSSResult = strings.TrimSpace(value)
			}
			continue
		}
		if value, ok := scalarString(raw); ok && value != "" {
			details[key] = value
		}
	}
	if len(details) > 0 {
		result.Details = details
	}
	return result, nil
}

func decodePolicyV2(data json.RawMessage) (dsmupdate.AutoUpdatePolicy, error) {
	var resp struct {
		AutoUpdateEnable *bool   `json:"autoupdate_enable"`
		AutoUpdateType   *string `json:"autoupdate_type"`
		UpgradeType      *string `json:"upgrade_type"`
		SmartNanoEnabled *bool   `json:"smart_nano_enabled"`
		Schedule         *struct {
			Hour    *int             `json:"hour"`
			Minute  *int             `json:"minute"`
			WeekDay *json.RawMessage `json:"week_day"`
		} `json:"schedule"`
	}
	if err := unmarshalObject(data, "DSM auto-update policy", &resp); err != nil {
		return dsmupdate.AutoUpdatePolicy{}, err
	}
	if resp.AutoUpdateEnable == nil {
		return dsmupdate.AutoUpdatePolicy{}, errors.New("decode DSM auto-update policy: required field \"autoupdate_enable\" is missing")
	}
	policy := dsmupdate.AutoUpdatePolicy{
		AutoUpdateEnabled: resp.AutoUpdateEnable,
		AutoUpdateType:    strings.TrimSpace(deref(resp.AutoUpdateType)),
		UpgradeType:       strings.TrimSpace(deref(resp.UpgradeType)),
		SmartNanoEnabled:  resp.SmartNanoEnabled,
	}
	if resp.Schedule != nil {
		schedule := &dsmupdate.PolicySchedule{}
		if resp.Schedule.Hour != nil {
			schedule.Hour = *resp.Schedule.Hour
		}
		if resp.Schedule.Minute != nil {
			schedule.Minute = *resp.Schedule.Minute
		}
		if resp.Schedule.WeekDay != nil {
			if value, ok := scalarString(*resp.Schedule.WeekDay); ok {
				schedule.WeekDay = value
			}
		}
		policy.Schedule = schedule
	}
	return policy, nil
}

func decodePolicyV1(data json.RawMessage) (dsmupdate.AutoUpdatePolicy, error) {
	var resp struct {
		AutoDownload *bool   `json:"auto_download"`
		UpgradeType  *string `json:"upgrade_type"`
	}
	if err := unmarshalObject(data, "DSM auto-update policy", &resp); err != nil {
		return dsmupdate.AutoUpdatePolicy{}, err
	}
	if resp.AutoDownload == nil {
		return dsmupdate.AutoUpdatePolicy{}, errors.New("decode DSM auto-update policy: required field \"auto_download\" is missing")
	}
	return dsmupdate.AutoUpdatePolicy{
		AutoDownload: resp.AutoDownload,
		UpgradeType:  strings.TrimSpace(deref(resp.UpgradeType)),
	}, nil
}

// decodeConfigSettings decodes the config-backup settings. The destination
// password ("pwd") is deliberately never referenced, so this decoder cannot
// learn or leak it.
func decodeConfigSettings(data json.RawMessage) (configSettings, error) {
	var resp struct {
		Enable      *bool   `json:"enable"`
		EncMethod   *string `json:"enc_method"`
		LastStatus  *string `json:"last_status"`
		MyDSAccount *string `json:"myds_account"`
	}
	if err := unmarshalObject(data, "configuration backup settings", &resp); err != nil {
		return configSettings{}, err
	}
	if resp.Enable == nil {
		return configSettings{}, errors.New("decode configuration backup settings: required field \"enable\" is missing")
	}
	return configSettings{
		Enabled:          *resp.Enable,
		Account:          strings.TrimSpace(deref(resp.MyDSAccount)),
		EncryptionMethod: strings.TrimSpace(deref(resp.EncMethod)),
		LastStatus:       strings.TrimSpace(deref(resp.LastStatus)),
	}, nil
}

func decodeConfigVersions(data json.RawMessage) ([]dsmupdate.ConfigBackupVersion, error) {
	var resp struct {
		Versions *[]struct {
			BackupTime *string `json:"backup_time"`
			DSMVersion *string `json:"dsm_version"`
			Host       *string `json:"host"`
			Model      *string `json:"model"`
			Serial     *string `json:"serial"`
		} `json:"versions"`
	}
	if err := unmarshalObject(data, "configuration backup history", &resp); err != nil {
		return nil, err
	}
	if resp.Versions == nil {
		return nil, errors.New("decode configuration backup history: required field \"versions\" is missing")
	}
	versions := make([]dsmupdate.ConfigBackupVersion, 0, len(*resp.Versions))
	for _, entry := range *resp.Versions {
		versions = append(versions, dsmupdate.ConfigBackupVersion{
			BackupTime: strings.TrimSpace(deref(entry.BackupTime)),
			DSMVersion: strings.TrimSpace(deref(entry.DSMVersion)),
			Host:       strings.TrimSpace(deref(entry.Host)),
			Model:      strings.TrimSpace(deref(entry.Model)),
			Serial:     strings.TrimSpace(deref(entry.Serial)),
		})
	}
	return versions, nil
}

// scalarString renders a JSON string, number, or bool as a display string.
// Objects, arrays, and null return ("", false).
func scalarString(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", false
	}
	switch trimmed[0] {
	case '{', '[':
		return "", false
	case '"':
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return "", false
		}
		return strings.TrimSpace(value), true
	case 't', 'f':
		var value bool
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return "", false
		}
		return strconv.FormatBool(value), true
	default:
		var number json.Number
		if err := json.Unmarshal(trimmed, &number); err != nil {
			return "", false
		}
		return number.String(), true
	}
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
