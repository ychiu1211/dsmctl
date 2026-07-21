package hardware

import (
	"encoding/json"
	"strings"
	"testing"
)

// The fixtures below are the live DSM 7.3 (DS3018xs) responses captured with a
// read-only probe, verbatim except that they carry no host-identifying values.

const beepFixture = `{
	"enc_module_fail": true,
	"eunit_redundant_power_fail": true,
	"fan_fail": true,
	"poweroff_beep": true,
	"poweron_beep": true,
	"redundant_power_fail": true,
	"reset_beep": true,
	"sas_link_fail": true,
	"support_fan_fail": true,
	"support_poweroff_beep": true,
	"support_poweron_beep": true,
	"support_redundant_power_fail": false,
	"support_reset_beep": false,
	"support_volume_or_cache_crash": true,
	"volume_or_cache_crash": true
}`

func TestDecodeBeepControl(t *testing.T) {
	control, err := decodeBeepControl(json.RawMessage(beepFixture))
	if err != nil {
		t.Fatal(err)
	}
	byEvent := map[string]bool{}
	supported := map[string]bool{}
	for _, event := range control.Events {
		byEvent[event.Event] = event.Enabled
		supported[event.Event] = event.Supported
	}
	if !byEvent["fan_fail"] || !supported["fan_fail"] {
		t.Fatalf("fan_fail = %#v", control.Events)
	}
	// redundant_power_fail is enabled but its support flag is false: the event
	// must carry that model-support state rather than assuming true.
	if !byEvent["redundant_power_fail"] || supported["redundant_power_fail"] {
		t.Fatalf("redundant_power_fail should be enabled but unsupported: %#v", control.Events)
	}
	// enc_module_fail has no explicit support_ flag; presence implies support.
	if !supported["enclosure_module_fail"] {
		t.Fatalf("enclosure_module_fail should be supported by presence: %#v", control.Events)
	}
}

func TestDecodeBeepControlOmitsAbsentEvents(t *testing.T) {
	control, err := decodeBeepControl(json.RawMessage(`{"fan_fail":false,"support_fan_fail":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(control.Events) != 1 || control.Events[0].Event != "fan_fail" || control.Events[0].Enabled {
		t.Fatalf("only the present event should be emitted: %#v", control.Events)
	}
}

const fanFixture = `{
	"all_disk_temp_fail": "no",
	"cool_fan": "yes",
	"dual_fan_speed": "quietfan",
	"fan_support_adjust_by_ext_nic": "no",
	"fan_type": 11
}`

func TestDecodeFanSpeed(t *testing.T) {
	fan, err := decodeFanSpeed(json.RawMessage(fanFixture))
	if err != nil {
		t.Fatal(err)
	}
	if fan.Mode != "quietfan" {
		t.Fatalf("mode = %q", fan.Mode)
	}
	if fan.CoolMode == nil || !*fan.CoolMode {
		t.Fatalf("cool_fan yes should decode true: %#v", fan.CoolMode)
	}
	if fan.FanType == nil || *fan.FanType != 11 {
		t.Fatalf("fan_type = %#v", fan.FanType)
	}
	if fan.SupportAdjustByExtNIC {
		t.Fatalf("fan_support_adjust_by_ext_nic 'no' should be false")
	}
}

func TestDecodeLEDBrightness(t *testing.T) {
	led, err := decodeLEDBrightness(json.RawMessage(`{"led_brightness":3,"schedule":"111000"}`))
	if err != nil {
		t.Fatal(err)
	}
	if led.Brightness != 3 || led.Schedule != "111000" {
		t.Fatalf("led = %#v", led)
	}
}

func TestDecodePowerScheduleEmpty(t *testing.T) {
	schedule, err := decodePowerSchedule(json.RawMessage(`{"poweroff_tasks":[],"poweron_tasks":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(schedule.PowerOnTasks) != 0 || len(schedule.PowerOffTasks) != 0 || schedule.EnabledTaskCount != 0 {
		t.Fatalf("empty schedule = %#v", schedule)
	}
}

// TestDecodePowerScheduleTasks proves the tolerant per-task decoding: the item
// shape was not observable live (the lab had no tasks), so both the primary key
// names and DSM's known alternates decode into the same task.
func TestDecodePowerScheduleTasks(t *testing.T) {
	raw := `{
		"poweron_tasks":[{"enabled":true,"hour":8,"min":30,"weekdays":"1,2,3,4,5"}],
		"poweroff_tasks":[{"enable":false,"hour":23,"minute":0,"week_day":"0,6"}]
	}`
	schedule, err := decodePowerSchedule(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(schedule.PowerOnTasks) != 1 {
		t.Fatalf("power-on tasks = %#v", schedule.PowerOnTasks)
	}
	on := schedule.PowerOnTasks[0]
	if !on.Enabled || on.Hour != 8 || on.Minute != 30 || on.Weekdays != "1,2,3,4,5" {
		t.Fatalf("power-on task = %#v", on)
	}
	off := schedule.PowerOffTasks[0]
	if off.Enabled || off.Hour != 23 || off.Minute != 0 || off.Weekdays != "0,6" {
		t.Fatalf("power-off task via alternate keys = %#v", off)
	}
	if schedule.EnabledTaskCount != 1 {
		t.Fatalf("enabled task count = %d, want 1", schedule.EnabledTaskCount)
	}
}

const powerRecoveryFixture = `{
	"internal_lan_num": 4,
	"rc_power_config": false,
	"wol": [
		{"enable": false, "idx": 1},
		{"enable": true, "idx": 2},
		{"enable": false, "idx": 3},
		{"enable": false, "idx": 4}
	],
	"wol1": false, "wol2": true, "wol3": false, "wol4": false
}`

func TestDecodePowerRecovery(t *testing.T) {
	recovery, err := decodePowerRecovery(json.RawMessage(powerRecoveryFixture))
	if err != nil {
		t.Fatal(err)
	}
	if recovery.RestorePowerState {
		t.Fatalf("rc_power_config false should not restore power")
	}
	if recovery.InternalLANCount != 4 || len(recovery.WOL) != 4 {
		t.Fatalf("recovery = %#v", recovery)
	}
	if recovery.WOL[1].Index != 2 || !recovery.WOL[1].Enabled {
		t.Fatalf("wol[1] = %#v", recovery.WOL[1])
	}
}

func TestDecodePowerRecoveryLegacyFlatWOL(t *testing.T) {
	recovery, err := decodePowerRecovery(json.RawMessage(`{"internal_lan_num":2,"rc_power_config":true,"wol1":true,"wol2":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if !recovery.RestorePowerState || len(recovery.WOL) != 2 {
		t.Fatalf("legacy recovery = %#v", recovery)
	}
	if recovery.WOL[0].Index != 1 || !recovery.WOL[0].Enabled || recovery.WOL[1].Enabled {
		t.Fatalf("flat wol fallback = %#v", recovery.WOL)
	}
}

const upsDisabledFixture = `{
	"ACL_enable": false, "ACL_list": [], "charge": 0, "delay_time": -1,
	"enable": false, "manufacture": "", "mode": "SLAVE", "model": "",
	"net_server_ip": "", "runtime": 0, "shutdown_device": false,
	"snmp_auth": false, "snmp_auth_key": false, "snmp_auth_type": "",
	"snmp_community": "", "snmp_mib": "", "snmp_privacy": false,
	"snmp_privacy_key": false, "snmp_privacy_type": "", "snmp_server_ip": "",
	"snmp_user": "", "snmp_version": "", "status": "usb_ups_status_unknown",
	"usb_ups_connect": false
}`

func TestDecodeUPSNoDevice(t *testing.T) {
	ups, err := decodeUPS(json.RawMessage(upsDisabledFixture))
	if err != nil {
		t.Fatal(err)
	}
	if ups.Enabled || ups.USBConnected || ups.Status != "usb_ups_status_unknown" {
		t.Fatalf("no-device path = %#v", ups)
	}
	// delay_time -1 => shut down when battery reaches low => nil pointer.
	if ups.SafeShutdownDelaySeconds != nil {
		t.Fatalf("delay_time -1 should decode to nil, got %d", *ups.SafeShutdownDelaySeconds)
	}
	if ups.SNMP == nil || ups.SNMP.CommunitySet {
		t.Fatalf("SNMP section present with no community set, got %#v", ups.SNMP)
	}
}

// TestDecodeUPSRedactsSecrets proves the SNMP community and auth/privacy keys
// are reported only as configured/not, never as a value in the model.
func TestDecodeUPSRedactsSecrets(t *testing.T) {
	raw := `{
		"enable": true, "mode": "SNMP", "delay_time": 300, "usb_ups_connect": false,
		"snmp_server_ip": "192.0.2.10", "snmp_version": "3", "snmp_user": "monitor",
		"snmp_community": "s3cr3t-community", "snmp_auth_key": true, "snmp_privacy_key": true,
		"snmp_auth_type": "SHA", "snmp_privacy_type": "AES"
	}`
	ups, err := decodeUPS(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if ups.SNMP == nil {
		t.Fatal("expected SNMP section")
	}
	if !ups.SNMP.CommunitySet || !ups.SNMP.AuthKeySet || !ups.SNMP.PrivacyKeySet {
		t.Fatalf("secret set-flags = %#v", ups.SNMP)
	}
	// The secret value must not appear anywhere in the serialized model.
	blob, err := json.Marshal(ups)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"s3cr3t-community", "snmp_community"} {
		if strings.Contains(string(blob), secret) {
			t.Fatalf("serialized UPS leaks %q: %s", secret, blob)
		}
	}
	if ups.SafeShutdownDelaySeconds == nil || *ups.SafeShutdownDelaySeconds != 300 {
		t.Fatalf("fixed delay = %#v", ups.SafeShutdownDelaySeconds)
	}
}
