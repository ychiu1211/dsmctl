// Package hardware implements independently selectable, read-only DSM
// operations for the Control Panel Hardware & Power surface.
//
// Four independent DSM API families are read, each gated in isolation so a NAS
// that advertises one but not the others fails closed only for the missing area:
//
//   - General hardware (three sub-areas, each independently gated):
//   - Beep control:  SYNO.Core.Hardware.BeepControl v1 `get`.
//   - Fan speed:     SYNO.Core.Hardware.FanSpeed v1 `get`.
//   - LED brightness:SYNO.Core.Hardware.Led.Brightness v1 `get`
//     (note the five-segment API name — not SYNO.Core.Hardware.Led).
//   - Power schedule: SYNO.Core.Hardware.PowerSchedule v1 `load`
//     (the method is `load`; `get`/`list` return DSM code 103).
//   - Power recovery: SYNO.Core.Hardware.PowerRecovery v1 `get`.
//   - UPS:            SYNO.Core.ExternalDevice.UPS v1 `get`
//     (the method is `get`; `load` returns DSM code 103). The API is present
//     even when no UPS is attached; the state then reports the no-device path.
//
// Every shape here was live-verified against the lab (DS3018xs, DSM 7.3). The
// per-power-schedule-task fields are decoded tolerantly because that NAS had no
// tasks configured, so the item shape is read through DSM's known key alternates
// rather than fabricated. This module is read-only; guarded writes are WI-075
// Slice B.
package hardware

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/hardware"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	BeepAPI          = "SYNO.Core.Hardware.BeepControl"
	FanAPI           = "SYNO.Core.Hardware.FanSpeed"
	LEDAPI           = "SYNO.Core.Hardware.Led.Brightness"
	PowerScheduleAPI = "SYNO.Core.Hardware.PowerSchedule"
	PowerRecoveryAPI = "SYNO.Core.Hardware.PowerRecovery"
	UPSAPI           = "SYNO.Core.ExternalDevice.UPS"

	BeepReadCapabilityName          = "hardware.beep.read"
	FanReadCapabilityName           = "hardware.fan.read"
	LEDReadCapabilityName           = "hardware.led.read"
	PowerScheduleReadCapabilityName = "hardware.power_schedule.read"
	PowerRecoveryReadCapabilityName = "hardware.power_recovery.read"
	UPSReadCapabilityName           = "hardware.ups.read"
)

// Input is the empty request every Hardware & Power read takes.
type Input struct{}

var beepOp = compatibility.Operation[Input, hardware.BeepControl]{
	Name: BeepReadCapabilityName,
	Variants: []compatibility.Variant[Input, hardware.BeepControl]{
		{
			Name: "core-hardware-beepcontrol-get-v1", API: BeepAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(BeepAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (hardware.BeepControl, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: BeepAPI, Version: 1, Method: "get", ReadOnly: true,
				})
				if err != nil {
					return hardware.BeepControl{}, fmt.Errorf("call %s.get v1: %w", BeepAPI, err)
				}
				return decodeBeepControl(data)
			},
		},
	},
}

var fanOp = compatibility.Operation[Input, hardware.FanSpeed]{
	Name: FanReadCapabilityName,
	Variants: []compatibility.Variant[Input, hardware.FanSpeed]{
		{
			Name: "core-hardware-fanspeed-get-v1", API: FanAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(FanAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (hardware.FanSpeed, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: FanAPI, Version: 1, Method: "get", ReadOnly: true,
				})
				if err != nil {
					return hardware.FanSpeed{}, fmt.Errorf("call %s.get v1: %w", FanAPI, err)
				}
				return decodeFanSpeed(data)
			},
		},
	},
}

var ledOp = compatibility.Operation[Input, hardware.LEDBrightness]{
	Name: LEDReadCapabilityName,
	Variants: []compatibility.Variant[Input, hardware.LEDBrightness]{
		{
			Name: "core-hardware-led-brightness-get-v1", API: LEDAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(LEDAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (hardware.LEDBrightness, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: LEDAPI, Version: 1, Method: "get", ReadOnly: true,
				})
				if err != nil {
					return hardware.LEDBrightness{}, fmt.Errorf("call %s.get v1: %w", LEDAPI, err)
				}
				return decodeLEDBrightness(data)
			},
		},
	},
}

var powerScheduleOp = compatibility.Operation[Input, hardware.PowerSchedule]{
	Name: PowerScheduleReadCapabilityName,
	Variants: []compatibility.Variant[Input, hardware.PowerSchedule]{
		{
			Name: "core-hardware-powerschedule-load-v1", API: PowerScheduleAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(PowerScheduleAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (hardware.PowerSchedule, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: PowerScheduleAPI, Version: 1, Method: "load", ReadOnly: true,
				})
				if err != nil {
					return hardware.PowerSchedule{}, fmt.Errorf("call %s.load v1: %w", PowerScheduleAPI, err)
				}
				return decodePowerSchedule(data)
			},
		},
	},
}

var powerRecoveryOp = compatibility.Operation[Input, hardware.PowerRecovery]{
	Name: PowerRecoveryReadCapabilityName,
	Variants: []compatibility.Variant[Input, hardware.PowerRecovery]{
		{
			Name: "core-hardware-powerrecovery-get-v1", API: PowerRecoveryAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(PowerRecoveryAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (hardware.PowerRecovery, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: PowerRecoveryAPI, Version: 1, Method: "get", ReadOnly: true,
				})
				if err != nil {
					return hardware.PowerRecovery{}, fmt.Errorf("call %s.get v1: %w", PowerRecoveryAPI, err)
				}
				return decodePowerRecovery(data)
			},
		},
	},
}

var upsOp = compatibility.Operation[Input, hardware.UPS]{
	Name: UPSReadCapabilityName,
	Variants: []compatibility.Variant[Input, hardware.UPS]{
		{
			Name: "core-externaldevice-ups-get-v1", API: UPSAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(UPSAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (hardware.UPS, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: UPSAPI, Version: 1, Method: "get", ReadOnly: true,
				})
				if err != nil {
					return hardware.UPS{}, fmt.Errorf("call %s.get v1: %w", UPSAPI, err)
				}
				return decodeUPS(data)
			},
		},
	},
}

// APINames returns every DSM API this module may read, so the facade can
// discover them in a single query before selecting any area.
func APINames() []string {
	return []string{BeepAPI, FanAPI, LEDAPI, PowerScheduleAPI, PowerRecoveryAPI, UPSAPI}
}

// SelectBeep reports the beep-control backend selection.
func SelectBeep(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := beepOp.Select(target)
	return selection, err
}

// SelectFan reports the fan-speed backend selection.
func SelectFan(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := fanOp.Select(target)
	return selection, err
}

// SelectLED reports the LED-brightness backend selection.
func SelectLED(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := ledOp.Select(target)
	return selection, err
}

// SelectPowerSchedule reports the power-schedule backend selection.
func SelectPowerSchedule(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := powerScheduleOp.Select(target)
	return selection, err
}

// SelectPowerRecovery reports the power-recovery backend selection.
func SelectPowerRecovery(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := powerRecoveryOp.Select(target)
	return selection, err
}

// SelectUPS reports the UPS backend selection.
func SelectUPS(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := upsOp.Select(target)
	return selection, err
}

// ReadGeneral reads the three comfort areas — beep, fan, and LED — each gated
// independently. A missing area is skipped (nil) so a model that exposes only
// some of the three still returns the rest. The returned selection is the beep
// area's, the first sub-area; capabilities report all three individually.
func ReadGeneral(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (hardware.GeneralState, compatibility.Selection, error) {
	beepSelection, err := SelectBeep(target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return hardware.GeneralState{}, beepSelection, err
	}

	state := hardware.GeneralState{}
	if beepSelection.Supported {
		beep, _, err := beepOp.Run(ctx, target, executor, Input{})
		if err != nil {
			return hardware.GeneralState{}, beepSelection, err
		}
		state.Beep = &beep
	}
	if fan, ok, err := readOptionalFan(ctx, target, executor); err != nil {
		return hardware.GeneralState{}, beepSelection, err
	} else if ok {
		state.Fan = &fan
	}
	if led, ok, err := readOptionalLED(ctx, target, executor); err != nil {
		return hardware.GeneralState{}, beepSelection, err
	} else if ok {
		state.LED = &led
	}
	return state, beepSelection, nil
}

func readOptionalFan(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (hardware.FanSpeed, bool, error) {
	selection, err := SelectFan(target)
	if err != nil {
		if compatibility.IsUnsupported(err) {
			return hardware.FanSpeed{}, false, nil
		}
		return hardware.FanSpeed{}, false, err
	}
	if !selection.Supported {
		return hardware.FanSpeed{}, false, nil
	}
	result, _, err := fanOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return hardware.FanSpeed{}, false, err
	}
	return result, true, nil
}

func readOptionalLED(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (hardware.LEDBrightness, bool, error) {
	selection, err := SelectLED(target)
	if err != nil {
		if compatibility.IsUnsupported(err) {
			return hardware.LEDBrightness{}, false, nil
		}
		return hardware.LEDBrightness{}, false, err
	}
	if !selection.Supported {
		return hardware.LEDBrightness{}, false, nil
	}
	result, _, err := ledOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return hardware.LEDBrightness{}, false, err
	}
	return result, true, nil
}

// ReadPowerSchedule reads the scheduled power on/off tasks.
func ReadPowerSchedule(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (hardware.PowerSchedule, compatibility.Selection, error) {
	return powerScheduleOp.Run(ctx, target, executor, Input{})
}

// ReadPowerRecovery reads the after-power-loss behavior.
func ReadPowerRecovery(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (hardware.PowerRecovery, compatibility.Selection, error) {
	return powerRecoveryOp.Run(ctx, target, executor, Input{})
}

// ReadUPS reads the UPS configuration and live status.
func ReadUPS(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (hardware.UPS, compatibility.Selection, error) {
	return upsOp.Run(ctx, target, executor, Input{})
}
