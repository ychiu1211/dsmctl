package application

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type HardwareCapabilitiesResult struct {
	NAS          string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.HardwareCapabilities `json:"capabilities" jsonschema:"Hardware & Power read areas currently exposed by dsmctl"`
	Report       synology.CompatibilityReport  `json:"report" jsonschema:"Discovered APIs and selected Hardware & Power compatibility backends"`
}

type HardwareGeneralResult struct {
	NAS     string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	General synology.HardwareGeneralState `json:"general" jsonschema:"Beep control, fan-speed mode, and LED brightness/schedule; model-absent areas are omitted"`
}

type HardwarePowerScheduleResult struct {
	NAS      string                              `json:"nas" jsonschema:"NAS profile used for the request"`
	Schedule synology.HardwarePowerScheduleState `json:"schedule" jsonschema:"Scheduled power on/off tasks"`
}

type HardwarePowerRecoveryResult struct {
	NAS      string                              `json:"nas" jsonschema:"NAS profile used for the request"`
	Recovery synology.HardwarePowerRecoveryState `json:"recovery" jsonschema:"After-power-loss behavior and per-NIC Wake-on-LAN state"`
}

type HardwareUPSResult struct {
	NAS string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	UPS synology.HardwareUPSState `json:"ups" jsonschema:"UPS configuration and live status; reports the no-device path when no UPS is attached"`
}

func (s *Service) GetHardwareCapabilities(ctx context.Context, requestedNAS string) (HardwareCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return HardwareCapabilitiesResult{}, err
	}
	capabilities, report, err := client.HardwareCapabilities(ctx)
	if err != nil {
		return HardwareCapabilitiesResult{}, authenticationError(name, err)
	}
	return HardwareCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetHardwareGeneral(ctx context.Context, requestedNAS string) (HardwareGeneralResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return HardwareGeneralResult{}, err
	}
	state, err := client.HardwareGeneral(ctx)
	if err != nil {
		return HardwareGeneralResult{}, authenticationError(name, err)
	}
	return HardwareGeneralResult{NAS: name, General: state}, nil
}

func (s *Service) GetHardwarePowerSchedule(ctx context.Context, requestedNAS string) (HardwarePowerScheduleResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return HardwarePowerScheduleResult{}, err
	}
	state, err := client.HardwarePowerSchedule(ctx)
	if err != nil {
		return HardwarePowerScheduleResult{}, authenticationError(name, err)
	}
	return HardwarePowerScheduleResult{NAS: name, Schedule: state}, nil
}

func (s *Service) GetHardwarePowerRecovery(ctx context.Context, requestedNAS string) (HardwarePowerRecoveryResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return HardwarePowerRecoveryResult{}, err
	}
	state, err := client.HardwarePowerRecovery(ctx)
	if err != nil {
		return HardwarePowerRecoveryResult{}, authenticationError(name, err)
	}
	return HardwarePowerRecoveryResult{NAS: name, Recovery: state}, nil
}

func (s *Service) GetHardwareUPS(ctx context.Context, requestedNAS string) (HardwareUPSResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return HardwareUPSResult{}, err
	}
	state, err := client.HardwareUPS(ctx)
	if err != nil {
		return HardwareUPSResult{}, authenticationError(name, err)
	}
	return HardwareUPSResult{NAS: name, UPS: state}, nil
}
