package hardware

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/hardware"
)

// beepEventFields maps each stable beep-event name to the DSM enable-flag key
// and its optional support_ capability key. Only events whose enable flag is
// present in the live response are emitted, so a model that omits an event
// reports it absent rather than inventing a default.
var beepEventFields = []struct {
	event      string
	enableKey  string
	supportKey string // "" when DSM ships no explicit support flag for this event
}{
	{"fan_fail", "fan_fail", "support_fan_fail"},
	{"poweron", "poweron_beep", "support_poweron_beep"},
	{"poweroff", "poweroff_beep", "support_poweroff_beep"},
	{"reset", "reset_beep", "support_reset_beep"},
	{"redundant_power_fail", "redundant_power_fail", "support_redundant_power_fail"},
	{"volume_or_cache_crash", "volume_or_cache_crash", "support_volume_or_cache_crash"},
	{"enclosure_module_fail", "enc_module_fail", ""},
	{"expansion_redundant_power_fail", "eunit_redundant_power_fail", ""},
	{"sas_link_fail", "sas_link_fail", ""},
}

// decodeBeepControl normalizes SYNO.Core.Hardware.BeepControl.get. It emits one
// event per enable flag the model actually reports and carries the model's
// support flag alongside it.
func decodeBeepControl(data json.RawMessage) (hardware.BeepControl, error) {
	root, err := decodeObject(data)
	if err != nil {
		return hardware.BeepControl{}, fmt.Errorf("decode beep control: %w", err)
	}
	control := hardware.BeepControl{Events: []hardware.BeepEvent{}}
	for _, field := range beepEventFields {
		if _, present := root[field.enableKey]; !present {
			continue
		}
		supported := true
		if field.supportKey != "" {
			supported = boolValue(root, field.supportKey)
		}
		control.Events = append(control.Events, hardware.BeepEvent{
			Event:     field.event,
			Enabled:   boolValue(root, field.enableKey),
			Supported: supported,
		})
	}
	return control, nil
}

// decodeFanSpeed normalizes SYNO.Core.Hardware.FanSpeed.get. Every field is
// model dependent and read tolerantly.
func decodeFanSpeed(data json.RawMessage) (hardware.FanSpeed, error) {
	root, err := decodeObject(data)
	if err != nil {
		return hardware.FanSpeed{}, fmt.Errorf("decode fan speed: %w", err)
	}
	fan := hardware.FanSpeed{
		Mode:                  stringValue(root, "dual_fan_speed", "fan_speed", "fan_speed_mode"),
		SupportAdjustByExtNIC: yesNoBool(root, "fan_support_adjust_by_ext_nic", "support_adjust_by_ext_nic"),
		AllDiskTempFail:       stringValue(root, "all_disk_temp_fail"),
	}
	if _, present := root["cool_fan"]; present {
		cool := yesNoBool(root, "cool_fan")
		fan.CoolMode = &cool
	}
	if value, present := intValue(root, "fan_type"); present {
		fan.FanType = &value
	}
	return fan, nil
}

// decodeLEDBrightness normalizes SYNO.Core.Hardware.Led.Brightness.get.
func decodeLEDBrightness(data json.RawMessage) (hardware.LEDBrightness, error) {
	root, err := decodeObject(data)
	if err != nil {
		return hardware.LEDBrightness{}, fmt.Errorf("decode LED brightness: %w", err)
	}
	return hardware.LEDBrightness{
		Brightness: intValueOr(root, 0, "led_brightness", "brightness"),
		Schedule:   stringValue(root, "schedule"),
	}, nil
}

// decodePowerSchedule normalizes SYNO.Core.Hardware.PowerSchedule.load. The two
// task arrays are the live-verified envelope; each task's fields are read
// through their known DSM alternates.
func decodePowerSchedule(data json.RawMessage) (hardware.PowerSchedule, error) {
	root, err := decodeObject(data)
	if err != nil {
		return hardware.PowerSchedule{}, fmt.Errorf("decode power schedule: %w", err)
	}
	schedule := hardware.PowerSchedule{
		PowerOnTasks:  decodePowerTasks(root, "poweron_tasks", "power_on_tasks"),
		PowerOffTasks: decodePowerTasks(root, "poweroff_tasks", "power_off_tasks"),
	}
	for _, task := range schedule.PowerOnTasks {
		if task.Enabled {
			schedule.EnabledTaskCount++
		}
	}
	for _, task := range schedule.PowerOffTasks {
		if task.Enabled {
			schedule.EnabledTaskCount++
		}
	}
	return schedule, nil
}

func decodePowerTasks(root map[string]any, keys ...string) []hardware.PowerScheduleTask {
	tasks := make([]hardware.PowerScheduleTask, 0)
	for _, item := range objectList(root, keys...) {
		tasks = append(tasks, hardware.PowerScheduleTask{
			Enabled:  boolValue(item, "enabled", "enable"),
			Hour:     intValueOr(item, 0, "hour"),
			Minute:   intValueOr(item, 0, "minute", "min"),
			Weekdays: stringValue(item, "weekdays", "week_day", "weekday", "days"),
		})
	}
	return tasks
}

// decodePowerRecovery normalizes SYNO.Core.Hardware.PowerRecovery.get. The wol
// array is preferred; the flat wol1..wolN booleans are a fallback for models
// that report only the legacy shape.
func decodePowerRecovery(data json.RawMessage) (hardware.PowerRecovery, error) {
	root, err := decodeObject(data)
	if err != nil {
		return hardware.PowerRecovery{}, fmt.Errorf("decode power recovery: %w", err)
	}
	recovery := hardware.PowerRecovery{
		RestorePowerState: boolValue(root, "rc_power_config"),
		InternalLANCount:  intValueOr(root, 0, "internal_lan_num", "internal_lan_count"),
		WOL:               []hardware.WOLInterface{},
	}
	if items := objectList(root, "wol"); len(items) > 0 {
		for _, item := range items {
			recovery.WOL = append(recovery.WOL, hardware.WOLInterface{
				Index:   intValueOr(item, 0, "idx", "index"),
				Enabled: boolValue(item, "enable", "enabled"),
			})
		}
	} else {
		count := recovery.InternalLANCount
		for index := 1; index <= count; index++ {
			key := "wol" + strconv.Itoa(index)
			if _, present := root[key]; !present {
				continue
			}
			recovery.WOL = append(recovery.WOL, hardware.WOLInterface{
				Index:   index,
				Enabled: boolValue(root, key),
			})
		}
	}
	return recovery, nil
}

// decodeUPS normalizes SYNO.Core.ExternalDevice.UPS.get. UPS authentication
// material (the SNMP community string and auth/privacy keys) is reduced to a
// set/not-set boolean and never carried as a value.
func decodeUPS(data json.RawMessage) (hardware.UPS, error) {
	root, err := decodeObject(data)
	if err != nil {
		return hardware.UPS{}, fmt.Errorf("decode UPS: %w", err)
	}
	ups := hardware.UPS{
		Enabled:                 boolValue(root, "enable"),
		Mode:                    stringValue(root, "mode"),
		USBConnected:            boolValue(root, "usb_ups_connect"),
		Status:                  stringValue(root, "status"),
		Manufacturer:            strings.TrimSpace(stringValue(root, "manufacture", "manufacturer")),
		Model:                   strings.TrimSpace(stringValue(root, "model")),
		ChargePercent:           intValueOr(root, 0, "charge"),
		RuntimeSeconds:          intValueOr(root, 0, "runtime"),
		ShutdownUPS:             boolValue(root, "shutdown_device"),
		NetworkServerIP:         stringValue(root, "net_server_ip"),
		NetworkUPSServerEnabled: boolValue(root, "ACL_enable"),
		PermittedSlaves:         stringList(root, "ACL_list"),
	}
	// delay_time -1 is DSM's sentinel for "shut down only when the battery
	// reaches low"; a non-negative value is a fixed time threshold.
	if delay, present := intValue(root, "delay_time"); present && delay >= 0 {
		ups.SafeShutdownDelaySeconds = &delay
	}
	if snmp, ok := decodeUPSSNMP(root); ok {
		ups.SNMP = &snmp
	}
	return ups, nil
}

// decodeUPSSNMP extracts the SNMP-UPS connection config. It returns ok=false
// when the response carries no SNMP fields at all, so a USB/network UPS omits
// the SNMP section rather than showing an empty one.
func decodeUPSSNMP(root map[string]any) (hardware.UPSSNMP, bool) {
	present := false
	for key := range root {
		if strings.HasPrefix(key, "snmp_") {
			present = true
			break
		}
	}
	if !present {
		return hardware.UPSSNMP{}, false
	}
	return hardware.UPSSNMP{
		ServerIP:      stringValue(root, "snmp_server_ip"),
		Version:       stringValue(root, "snmp_version"),
		User:          stringValue(root, "snmp_user"),
		MIB:           stringValue(root, "snmp_mib"),
		AuthType:      stringValue(root, "snmp_auth_type"),
		PrivacyType:   stringValue(root, "snmp_privacy_type"),
		CommunitySet:  strings.TrimSpace(stringValue(root, "snmp_community")) != "",
		AuthKeySet:    boolValue(root, "snmp_auth_key"),
		PrivacyKeySet: boolValue(root, "snmp_privacy_key"),
	}, true
}

// --- tolerant decode helpers (kept local to the package, mirroring the
// disksmart module) ---

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

func stringList(values map[string]any, keys ...string) []string {
	for _, key := range keys {
		items, ok := values[key].([]any)
		if !ok {
			continue
		}
		result := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	}
	return []string{}
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

// yesNoBool reads DSM's "yes"/"no" string booleans (and plain booleans) for the
// fan area, where several capability flags are encoded as strings.
func yesNoBool(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			return strings.EqualFold(strings.TrimSpace(typed), "yes")
		case json.Number:
			return typed.String() != "0" && typed.String() != ""
		case float64:
			return typed != 0
		}
	}
	return false
}
