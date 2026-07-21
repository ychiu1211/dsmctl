package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/hardware"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	hardwareops "github.com/ychiu1211/dsmctl/internal/synology/operations/hardware"
)

type HardwareGeneralState = hardware.GeneralState
type HardwarePowerScheduleState = hardware.PowerSchedule
type HardwarePowerRecoveryState = hardware.PowerRecovery
type HardwareUPSState = hardware.UPS
type HardwareCapabilities = hardware.Capabilities

// HardwareGeneral reads the three general-hardware comfort areas — beep control,
// fan-speed mode, and LED brightness/schedule — each gated independently so a
// model exposing only some of them still returns the rest. All three are model
// dependent; absent areas are omitted rather than fabricated.
func (c *Client) HardwareGeneral(ctx context.Context) (HardwareGeneralState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, hardwareops.APINames()...); err != nil {
		return HardwareGeneralState{}, fmt.Errorf("prepare hardware general target: %w", err)
	}
	state, _, err := hardwareops.ReadGeneral(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return HardwareGeneralState{}, fmt.Errorf("read general hardware: %w", err)
	}
	if state.Beep != nil {
		c.target.AddCapability(hardwareops.BeepReadCapabilityName)
	}
	if state.Fan != nil {
		c.target.AddCapability(hardwareops.FanReadCapabilityName)
	}
	if state.LED != nil {
		c.target.AddCapability(hardwareops.LEDReadCapabilityName)
	}
	return state, nil
}

// HardwarePowerSchedule reads the scheduled power on/off tasks.
func (c *Client) HardwarePowerSchedule(ctx context.Context) (HardwarePowerScheduleState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, hardwareops.APINames()...); err != nil {
		return HardwarePowerScheduleState{}, fmt.Errorf("prepare hardware power-schedule target: %w", err)
	}
	state, selection, err := hardwareops.ReadPowerSchedule(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return HardwarePowerScheduleState{}, fmt.Errorf("read power schedule: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(hardwareops.PowerScheduleReadCapabilityName)
	}
	return state, nil
}

// HardwarePowerRecovery reads the after-power-loss behavior and per-NIC
// Wake-on-LAN state.
func (c *Client) HardwarePowerRecovery(ctx context.Context) (HardwarePowerRecoveryState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, hardwareops.APINames()...); err != nil {
		return HardwarePowerRecoveryState{}, fmt.Errorf("prepare hardware power-recovery target: %w", err)
	}
	state, selection, err := hardwareops.ReadPowerRecovery(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return HardwarePowerRecoveryState{}, fmt.Errorf("read power recovery: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(hardwareops.PowerRecoveryReadCapabilityName)
	}
	return state, nil
}

// HardwareUPS reads the UPS configuration and live status. The UPS API is
// present even when no device is attached; the state then reports the no-device
// path (disabled, not connected, status unknown). UPS authentication material is
// never surfaced as a value.
func (c *Client) HardwareUPS(ctx context.Context) (HardwareUPSState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, hardwareops.APINames()...); err != nil {
		return HardwareUPSState{}, fmt.Errorf("prepare hardware UPS target: %w", err)
	}
	state, selection, err := hardwareops.ReadUPS(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return HardwareUPSState{}, fmt.Errorf("read UPS: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(hardwareops.UPSReadCapabilityName)
	}
	return state, nil
}

// HardwareCapabilities reports which Hardware & Power read areas this NAS
// exposes, each selected independently so one missing API family does not
// disable the others.
func (c *Client) HardwareCapabilities(ctx context.Context) (HardwareCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, hardwareops.APINames()...); err != nil {
		return HardwareCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare hardware capabilities target: %w", err)
	}
	selectors := []struct {
		selectArea func(compatibility.Target) (compatibility.Selection, error)
		capability string
	}{
		{hardwareops.SelectBeep, hardwareops.BeepReadCapabilityName},
		{hardwareops.SelectFan, hardwareops.FanReadCapabilityName},
		{hardwareops.SelectLED, hardwareops.LEDReadCapabilityName},
		{hardwareops.SelectPowerSchedule, hardwareops.PowerScheduleReadCapabilityName},
		{hardwareops.SelectPowerRecovery, hardwareops.PowerRecoveryReadCapabilityName},
		{hardwareops.SelectUPS, hardwareops.UPSReadCapabilityName},
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selector := range selectors {
		selection, err := selector.selectArea(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return HardwareCapabilities{}, CompatibilityReport{}, fmt.Errorf("select hardware backend: %w", err)
		}
		if selection.Supported {
			c.target.AddCapability(selector.capability)
		}
		selections = append(selections, selection)
	}
	capabilities := HardwareCapabilities{
		Beep:          selections[0].Supported,
		Fan:           selections[1].Supported,
		LED:           selections[2].Supported,
		PowerSchedule: selections[3].Supported,
		PowerRecovery: selections[4].Supported,
		UPS:           selections[5].Supported,
	}
	return capabilities, c.target.Report(selections...), nil
}
