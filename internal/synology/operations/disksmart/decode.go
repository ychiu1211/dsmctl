package disksmart

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/disksmart"
)

// decodeDiskHealthList normalizes SYNO.Core.Storage.Disk.list into the per-disk
// health-lifecycle model. Field names are read tolerantly; the exact keys were
// live-verified on DSM 7.3 but a few carry alternates seen on other releases.
func decodeDiskHealthList(data json.RawMessage) ([]disksmart.DiskHealth, error) {
	root, err := decodeObject(data)
	if err != nil {
		return nil, fmt.Errorf("decode disk list: %w", err)
	}
	items := objectList(root, "disks", "drives")
	disks := make([]disksmart.DiskHealth, 0, len(items))
	for _, item := range items {
		disks = append(disks, decodeDiskHealth(item))
	}
	return disks, nil
}

func decodeDiskHealth(raw map[string]any) disksmart.DiskHealth {
	disk := disksmart.DiskHealth{
		ID:                          stringValue(raw, "id", "disk_id", "device"),
		Device:                      stringValue(raw, "device", "dev_name"),
		Name:                        firstNonEmpty(stringValue(raw, "name", "display_name"), stringValue(raw, "longName")),
		Model:                       stringValue(raw, "model", "model_name"),
		Firmware:                    stringValue(raw, "firm", "firmware", "firmware_version"),
		Vendor:                      strings.TrimSpace(stringValue(raw, "vendor", "brand")),
		Serial:                      stringValue(raw, "serial", "ui_serial", "serial_number"),
		Interface:                   stringValue(raw, "diskType", "portType", "interface"),
		Slot:                        stringValue(raw, "slot_id", "slot", "num_id", "bay"),
		Location:                    stringValue(raw, "disk_location", "location"),
		SizeBytes:                   uint64Value(raw, "size_total", "total_size", "capacity"),
		Status:                      stringValue(raw, "status", "disk_status"),
		Health:                      firstNonEmpty(stringValue(raw, "overview_status", "summary_status_key", "drive_status_key"), stringValue(raw, "status")),
		SMARTStatus:                 stringValue(raw, "smart_status", "smartStatus"),
		SMARTSupported:              boolValue(raw, "smart_test_support", "smartTestSupport"),
		RemainingLifeDanger:         boolValue(raw, "remain_life_danger"),
		BelowRemainingLifeThreshold: boolValue(raw, "below_remain_life_thr"),
		SpareBlockDaysLeft:          intValueOr(raw, 0, "sb_days_left"),
		SpareBlockCritical:          boolValue(raw, "sb_days_left_critical"),
		SpareBlockWarning:           boolValue(raw, "sb_days_left_warning"),
		UncorrectableCount:          intValueOr(raw, -1, "unc"),
		Testing:                     boolValue(raw, "smart_testing"),
		TestingType:                 stringValue(raw, "testing_type"),
		TestingProgress:             firstNonEmpty(stringValue(raw, "testing_progress"), stringValue(raw, "smart_progress")),
	}
	// Media type: DSM reports the bus through diskType (SATA/NVMe) and the media
	// through the boolean isSsd. Prefer an explicit media type, else derive it.
	if media := stringValue(raw, "media_type"); media != "" {
		disk.Type = media
	} else if _, present := raw["isSsd"]; present {
		if boolValue(raw, "isSsd") {
			disk.Type = "SSD"
		} else {
			disk.Type = "HDD"
		}
	}
	if temp, ok := intValue(raw, "temp", "temperature", "temperature_c"); ok {
		disk.TemperatureC = &temp
	}
	if remain, ok := objectValue(raw, "remain_life"); ok {
		// DSM reports value -1 for drives with no remaining-life estimate (HDDs);
		// treat that sentinel as "not applicable" rather than a real percentage.
		if value, present := intValue(remain, "value"); present && value >= 0 {
			disk.RemainingLifePercent = &value
		}
		disk.RemainingLifeTrustable = boolValue(remain, "trustable")
	}
	if container, ok := objectValue(raw, "container"); ok {
		disk.Unit = stringValue(container, "str", "type")
	}
	return disk
}

// decodeThresholds normalizes SYNO.Storage.CGI.HddMan.get, the NAS-wide disk
// health warning configuration.
func decodeThresholds(data json.RawMessage) (disksmart.HealthThresholds, error) {
	root, err := decodeObject(data)
	if err != nil {
		return disksmart.HealthThresholds{}, fmt.Errorf("decode disk health thresholds: %w", err)
	}
	return disksmart.HealthThresholds{
		BadSectorThresholdEnabled:        boolValue(root, "BadSctrThrEn"),
		RemainingLifeThresholdEnabled:    boolValue(root, "RemainLifeThrEn"),
		RemainingLifeThresholdPercent:    intValueOr(root, 0, "RemainLifeThrVal"),
		SpareBlockMonthsThresholdEnabled: boolValue(root, "SBMonthLeftThrEn"),
		SpareBlockMonthsThreshold:        intValueOr(root, 0, "SBMonthLeftThrVal"),
		HealthReportEnabled:              boolValue(root, "healthReportEn"),
		WriteDurabilityAssuranceEnabled:  boolValue(root, "WddaEn"),
	}, nil
}

// decodeDiskSMART normalizes SYNO.Storage.CGI.Smart.get_health_info into one
// disk's attribute table plus its health/test summary.
func decodeDiskSMART(data json.RawMessage) (disksmart.DiskSMART, error) {
	root, err := decodeObject(data)
	if err != nil {
		return disksmart.DiskSMART{}, fmt.Errorf("decode disk SMART info: %w", err)
	}
	health, ok := objectValue(root, "healthInfo")
	if !ok {
		health = root
	}
	smart := disksmart.DiskSMART{Attributes: []disksmart.SMARTAttribute{}}
	for _, item := range objectList(health, "smartInfo", "attributes") {
		smart.Attributes = append(smart.Attributes, disksmart.SMARTAttribute{
			ID:        stringValue(item, "id"),
			Name:      stringValue(item, "name"),
			Current:   stringValue(item, "current", "value"),
			Worst:     stringValue(item, "worst"),
			Threshold: stringValue(item, "threshold", "thres"),
			Raw:       stringValue(item, "raw", "raw_value"),
			Status:    stringValue(item, "status"),
		})
	}
	smart.AttributeCount = intValueOr(health, len(smart.Attributes), "count")
	if overview, present := objectValue(health, "overview"); present {
		smart.OverallStatus = stringValue(overview, "smart")
		smart.SMARTInfoStatus = stringValue(overview, "smart_info")
		smart.SMARTTestStatus = stringValue(overview, "smart_test")
		smart.RemainingLifeAttribute = stringValue(overview, "remain_life_attr")
		smart.IsNVMe = boolValue(overview, "isNVMeDisk")
		smart.IsSSD = boolValue(overview, "isSsd")
		if remain, hasRemain := objectValue(overview, "remain_life"); hasRemain {
			if value, hasValue := intValue(remain, "value"); hasValue && value >= 0 {
				smart.RemainingLifePercent = &value
			}
		}
	}
	return smart, nil
}

// decodeTestStatus normalizes SYNO.Core.Storage.Disk.get_smart_test_log. It
// selects the entry matching the requested device, falling back to the first.
func decodeTestStatus(data json.RawMessage, device string) (disksmart.SMARTTestStatus, error) {
	root, err := decodeObject(data)
	if err != nil {
		return disksmart.SMARTTestStatus{}, fmt.Errorf("decode SMART test log: %w", err)
	}
	entries := objectList(root, "testInfo", "test_info")
	entry := map[string]any(nil)
	for _, candidate := range entries {
		if stringValue(candidate, "device") == device {
			entry = candidate
			break
		}
	}
	if entry == nil && len(entries) > 0 {
		entry = entries[0]
	}
	status := disksmart.SMARTTestStatus{
		LatestTime: stringValue(root, "latest_test_time"),
	}
	if entry != nil {
		status.Testing = boolValue(entry, "testing")
		status.LatestResult = stringValue(entry, "latest_test_result")
		status.LatestType = intValueOr(entry, 0, "latest_test_type")
		status.Remaining = strings.TrimSpace(stringValue(entry, "remain"))
		status.QuickEstimate = strings.TrimSpace(stringValue(entry, "quickTime"))
		status.ExtendedEstimate = strings.TrimSpace(stringValue(entry, "extendTime"))
		if status.LatestTime == "" {
			status.LatestTime = stringValue(entry, "latest_test_time")
		}
	}
	return status, nil
}

// --- tolerant decode helpers ---

func decodeObject(data json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func objectList(values map[string]any, keys ...string) []map[string]any {
	for _, key := range keys {
		items, ok := values[key].([]any)
		if !ok {
			continue
		}
		result := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if object, ok := item.(map[string]any); ok {
				result = append(result, object)
			}
		}
		return result
	}
	return nil
}

func objectValue(values map[string]any, keys ...string) (map[string]any, bool) {
	for _, key := range keys {
		if value, ok := values[key].(map[string]any); ok {
			return value, true
		}
	}
	return nil, false
}

func stringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			return typed
		case json.Number:
			return typed.String()
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(typed)
		}
	}
	return ""
}

func uint64Value(values map[string]any, keys ...string) uint64 {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case json.Number:
			if result, err := strconv.ParseUint(typed.String(), 10, 64); err == nil {
				return result
			}
			if result, err := typed.Float64(); err == nil && result >= 0 {
				return uint64(result)
			}
		case float64:
			if typed >= 0 {
				return uint64(typed)
			}
		case string:
			if result, err := strconv.ParseUint(typed, 10, 64); err == nil {
				return result
			}
		}
	}
	return 0
}

// intValue reports the first present integer-valued key and whether one was
// found, so a caller can distinguish an absent field from a real zero.
func intValue(values map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case json.Number:
			if result, err := strconv.Atoi(typed.String()); err == nil {
				return result, true
			}
			if result, err := typed.Float64(); err == nil {
				return int(result), true
			}
		case float64:
			return int(typed), true
		case string:
			if result, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
				return result, true
			}
		}
	}
	return 0, false
}

func intValueOr(values map[string]any, fallback int, keys ...string) int {
	if result, ok := intValue(values, keys...); ok {
		return result
	}
	return fallback
}

func boolValue(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case bool:
			return typed
		case json.Number:
			return typed.String() != "0" && typed.String() != ""
		case float64:
			return typed != 0
		case string:
			result, _ := strconv.ParseBool(typed)
			return result || typed == "1"
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
