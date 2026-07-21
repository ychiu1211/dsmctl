// Package hardware contains stable, read-only models for the Control Panel
// Hardware & Power surface: the beep-control event flags, the fan-speed mode,
// the LED brightness/schedule, the scheduled power on/off tasks, the
// power-recovery behavior after an outage, and the UPS configuration and live
// status.
//
// Every field here is model dependent. Beep events, fan-speed values, and LED
// controls vary by physical NAS model and some are simply absent, so the models
// carry only the fields the live DSM `get` actually returns and report the rest
// as not supported rather than fabricating them (WI-075, DS3018xs / DSM 7.3
// live-verified). Four independent DSM API families back these areas — general
// hardware (beep/fan/LED), power schedule, power recovery, and UPS — each read
// and gated in isolation so a NAS missing one keeps the others usable.
//
// This is a read model only. Any UPS authentication material (the SNMP-UPS
// community string and auth/privacy keys) is deliberately reduced to a
// "configured" boolean and never carries a secret value into results or logs.
package hardware

// BeepEvent is one acoustic-alert event and whether the model supports it.
// DSM pairs each event flag with a support_ capability flag; an event absent
// from the model's support set is reported Supported=false and never written.
type BeepEvent struct {
	Event     string `json:"event" jsonschema:"Beep event key such as fan_fail, poweron, poweroff, reset, redundant_power_fail, or volume_or_cache_crash"`
	Enabled   bool   `json:"enabled" jsonschema:"Whether the NAS beeps for this event"`
	Supported bool   `json:"supported" jsonschema:"Whether this model supports controlling the beep for this event"`
}

// BeepControl is the per-event beep configuration read from
// SYNO.Core.Hardware.BeepControl.get.
type BeepControl struct {
	Events []BeepEvent `json:"events" jsonschema:"Per-event beep flags the model reports"`
}

// FanSpeed is the fan-speed configuration read from
// SYNO.Core.Hardware.FanSpeed.get. Mode is the fan-speed dropdown value (for
// example quietfan, coolfan, fullfan, or lownoise) and is model dependent.
type FanSpeed struct {
	Mode                  string `json:"mode,omitempty" jsonschema:"Fan-speed mode enum such as quietfan, coolfan, fullfan, or lownoise"`
	CoolMode              *bool  `json:"cool_mode,omitempty" jsonschema:"Whether the cool-fan mode is on, when the model reports it"`
	FanType               *int   `json:"fan_type,omitempty" jsonschema:"DSM raw fan-hardware type code, when reported"`
	SupportAdjustByExtNIC bool   `json:"support_adjust_by_ext_nic" jsonschema:"Whether fan speed can be adjusted by an external NIC on this model"`
	AllDiskTempFail       string `json:"all_disk_temp_fail,omitempty" jsonschema:"DSM raw all-disk temperature-failure indicator, when reported"`
}

// LEDBrightness is the front-LED configuration read from
// SYNO.Core.Hardware.Led.Brightness.get. Schedule is DSM's 168-character weekly
// on/off mask (7 days x 24 hours) when the model exposes an LED night schedule.
type LEDBrightness struct {
	Brightness int    `json:"brightness" jsonschema:"LED brightness level as DSM reports it"`
	Schedule   string `json:"schedule,omitempty" jsonschema:"168-character weekly LED on/off mask (7 days x 24 hours), when the model has an LED schedule"`
}

// GeneralState is the combined general-hardware read: the three comfort areas,
// each present only when its DSM API is available on the model.
type GeneralState struct {
	Beep *BeepControl   `json:"beep,omitempty" jsonschema:"Per-event beep configuration; absent when the model exposes no beep control"`
	Fan  *FanSpeed      `json:"fan,omitempty" jsonschema:"Fan-speed configuration; absent when the model exposes no fan-speed control"`
	LED  *LEDBrightness `json:"led,omitempty" jsonschema:"LED brightness/schedule; absent when the model exposes no LED control"`
}

// PowerScheduleTask is one scheduled power-on or power-off task. The envelope
// (the two task arrays) was live-verified on DSM 7.3; the per-task fields are
// decoded tolerantly because the verification NAS had no tasks configured, so a
// key DSM renamed across builds is read through its known alternates rather than
// fabricated.
type PowerScheduleTask struct {
	Enabled  bool   `json:"enabled" jsonschema:"Whether this scheduled task is active"`
	Hour     int    `json:"hour" jsonschema:"Hour of day (0-23) the task runs"`
	Minute   int    `json:"minute" jsonschema:"Minute of the hour (0-59) the task runs"`
	Weekdays string `json:"weekdays,omitempty" jsonschema:"DSM weekday mask for the task, such as a comma-separated day list"`
}

// PowerSchedule is the scheduled power on/off configuration read from
// SYNO.Core.Hardware.PowerSchedule.load.
type PowerSchedule struct {
	PowerOnTasks     []PowerScheduleTask `json:"power_on_tasks" jsonschema:"Scheduled power-on tasks"`
	PowerOffTasks    []PowerScheduleTask `json:"power_off_tasks" jsonschema:"Scheduled power-off tasks; a power-off task makes the NAS unreachable at its scheduled time"`
	EnabledTaskCount int                 `json:"enabled_task_count" jsonschema:"Number of enabled power on/off tasks"`
}

// WOLInterface is one NIC's Wake-on-LAN enable, read from the power-recovery
// area.
type WOLInterface struct {
	Index   int  `json:"index" jsonschema:"1-based internal NIC index"`
	Enabled bool `json:"enabled" jsonschema:"Whether Wake-on-LAN is enabled on this NIC"`
}

// PowerRecovery is the after-power-loss behavior read from
// SYNO.Core.Hardware.PowerRecovery.get.
type PowerRecovery struct {
	RestorePowerState bool           `json:"restore_power_state" jsonschema:"Whether the NAS restores its previous power state after a power loss; false means it stays off and needs a manual power-on"`
	InternalLANCount  int            `json:"internal_lan_count" jsonschema:"Number of internal NICs"`
	WOL               []WOLInterface `json:"wol" jsonschema:"Per-NIC Wake-on-LAN enable"`
}

// UPSSNMP is the SNMP-UPS connection configuration. Any secret material (the
// community string and auth/privacy keys) is reduced to a set/not-set boolean
// and never carries a value.
type UPSSNMP struct {
	ServerIP      string `json:"server_ip,omitempty" jsonschema:"SNMP-UPS server IP address"`
	Version       string `json:"version,omitempty" jsonschema:"SNMP version"`
	User          string `json:"user,omitempty" jsonschema:"SNMPv3 user name"`
	MIB           string `json:"mib,omitempty" jsonschema:"SNMP UPS MIB name"`
	AuthType      string `json:"auth_type,omitempty" jsonschema:"SNMPv3 authentication type"`
	PrivacyType   string `json:"privacy_type,omitempty" jsonschema:"SNMPv3 privacy type"`
	CommunitySet  bool   `json:"community_set" jsonschema:"Whether an SNMP community string is configured; the value itself is never surfaced"`
	AuthKeySet    bool   `json:"auth_key_set" jsonschema:"Whether an SNMPv3 authentication key is configured"`
	PrivacyKeySet bool   `json:"privacy_key_set" jsonschema:"Whether an SNMPv3 privacy key is configured"`
}

// UPS is the uninterruptible-power-supply configuration and live status read
// from SYNO.Core.ExternalDevice.UPS.get. The API is present even when no UPS is
// attached: Enabled/USBConnected/Status then report the no-device state.
type UPS struct {
	Enabled                  bool     `json:"enabled" jsonschema:"Whether UPS integration is enabled; disabling it removes safe shutdown on the next power failure"`
	Mode                     string   `json:"mode,omitempty" jsonschema:"UPS mode such as USB (local), SNMP, or SLAVE (network UPS)"`
	USBConnected             bool     `json:"usb_connected" jsonschema:"Whether a local USB UPS is currently attached"`
	Status                   string   `json:"status,omitempty" jsonschema:"DSM raw UPS status string, such as usb_ups_status_unknown"`
	Manufacturer             string   `json:"manufacturer,omitempty" jsonschema:"Attached UPS manufacturer, when reported"`
	Model                    string   `json:"model,omitempty" jsonschema:"Attached UPS model, when reported"`
	ChargePercent            int      `json:"charge_percent" jsonschema:"Battery charge percentage, when a UPS is attached"`
	RuntimeSeconds           int      `json:"runtime_seconds" jsonschema:"Estimated battery runtime in seconds, when a UPS is attached"`
	ShutdownUPS              bool     `json:"shutdown_ups" jsonschema:"Whether the NAS signals the UPS to power off during safe shutdown"`
	SafeShutdownDelaySeconds *int     `json:"safe_shutdown_delay_seconds,omitempty" jsonschema:"Fixed safe-shutdown time threshold (DSM raw delay_time); absent when DSM shuts down only once the battery reaches low"`
	NetworkServerIP          string   `json:"network_server_ip,omitempty" jsonschema:"Master UPS server IP when this NAS is a network-UPS slave"`
	NetworkUPSServerEnabled  bool     `json:"network_ups_server_enabled" jsonschema:"Whether this NAS acts as a network-UPS server for permitted slaves"`
	PermittedSlaves          []string `json:"permitted_slaves" jsonschema:"Allow-list of slave IPs permitted when this NAS is a network-UPS server"`
	SNMP                     *UPSSNMP `json:"snmp,omitempty" jsonschema:"SNMP-UPS connection configuration, when the SNMP mode is configured"`
}

// Capabilities reports which Hardware & Power read areas the NAS exposes. Each
// area selects its backend independently; a missing API family fails closed for
// its own area only and never disables the others.
type Capabilities struct {
	Beep          bool `json:"beep" jsonschema:"Whether beep control can be read (SYNO.Core.Hardware.BeepControl)"`
	Fan           bool `json:"fan" jsonschema:"Whether fan-speed mode can be read (SYNO.Core.Hardware.FanSpeed)"`
	LED           bool `json:"led" jsonschema:"Whether LED brightness/schedule can be read (SYNO.Core.Hardware.Led.Brightness)"`
	PowerSchedule bool `json:"power_schedule" jsonschema:"Whether the scheduled power on/off tasks can be read (SYNO.Core.Hardware.PowerSchedule)"`
	PowerRecovery bool `json:"power_recovery" jsonschema:"Whether the after-power-loss behavior can be read (SYNO.Core.Hardware.PowerRecovery)"`
	UPS           bool `json:"ups" jsonschema:"Whether the UPS configuration/status can be read (SYNO.Core.ExternalDevice.UPS)"`
}
