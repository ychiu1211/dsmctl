package application

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type ExternalDeviceCapabilitiesResult struct {
	NAS          string                              `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.ExternalDeviceCapabilities `json:"capabilities" jsonschema:"External Devices read areas currently exposed by dsmctl"`
	Report       synology.CompatibilityReport        `json:"report" jsonschema:"Discovered APIs and selected External Devices compatibility backends"`
}

type ExternalStorageResult struct {
	NAS     string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Storage synology.ExternalStorageState `json:"storage" jsonschema:"Attached USB and eSATA external disks; a bus whose API is absent is omitted"`
}

type ExternalPrinterResult struct {
	NAS      string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Printers synology.ExternalPrinterState `json:"printers" jsonschema:"Connected printers and the Bonjour/AirPrint sharing toggle"`
}

func (s *Service) GetExternalDeviceCapabilities(ctx context.Context, requestedNAS string) (ExternalDeviceCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ExternalDeviceCapabilitiesResult{}, err
	}
	capabilities, report, err := client.ExternalDeviceCapabilitiesState(ctx)
	if err != nil {
		return ExternalDeviceCapabilitiesResult{}, authenticationError(name, err)
	}
	return ExternalDeviceCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetExternalStorage(ctx context.Context, requestedNAS string) (ExternalStorageResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ExternalStorageResult{}, err
	}
	state, err := client.ExternalStorage(ctx)
	if err != nil {
		return ExternalStorageResult{}, authenticationError(name, err)
	}
	return ExternalStorageResult{NAS: name, Storage: state}, nil
}

func (s *Service) GetExternalPrinters(ctx context.Context, requestedNAS string) (ExternalPrinterResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ExternalPrinterResult{}, err
	}
	state, err := client.ExternalPrinters(ctx)
	if err != nil {
		return ExternalPrinterResult{}, authenticationError(name, err)
	}
	return ExternalPrinterResult{NAS: name, Printers: state}, nil
}
