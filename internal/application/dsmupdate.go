package application

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type DSMUpdateCapabilitiesResult struct {
	NAS          string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.DSMUpdateCapabilities `json:"capabilities" jsonschema:"Update & Restore read areas currently exposed by dsmctl"`
	Report       synology.CompatibilityReport   `json:"report" jsonschema:"Discovered APIs and selected DSM update compatibility backends"`
}

type DSMUpdateStatusResult struct {
	NAS    string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Status synology.DSMUpdateStatus `json:"status" jsonschema:"Local DSM update state: installed version, whether an upgrade is allowed, and any in-progress state"`
}

type DSMUpdateAvailableResult struct {
	NAS       string                      `json:"nas" jsonschema:"NAS profile used for the request"`
	Available synology.DSMUpdateAvailable `json:"available" jsonschema:"Update-server offered-update check; availability is unknown when the update server is unreachable"`
}

type DSMUpdatePolicyResult struct {
	NAS    string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Policy synology.DSMUpdatePolicy `json:"policy" jsonschema:"DSM auto-update policy"`
}

type DSMUpdateConfigBackupResult struct {
	NAS          string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	ConfigBackup synology.DSMUpdateConfigBackup `json:"config_backup" jsonschema:"Configuration-backup status and history without any destination password"`
}

func (s *Service) GetDSMUpdateCapabilities(ctx context.Context, requestedNAS string) (DSMUpdateCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DSMUpdateCapabilitiesResult{}, err
	}
	capabilities, report, err := client.DSMUpdateCapabilities(ctx)
	if err != nil {
		return DSMUpdateCapabilitiesResult{}, authenticationError(name, err)
	}
	return DSMUpdateCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetDSMUpdateStatus(ctx context.Context, requestedNAS string) (DSMUpdateStatusResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DSMUpdateStatusResult{}, err
	}
	state, err := client.DSMUpdateStatus(ctx)
	if err != nil {
		return DSMUpdateStatusResult{}, authenticationError(name, err)
	}
	return DSMUpdateStatusResult{NAS: name, Status: state}, nil
}

func (s *Service) GetDSMUpdateAvailable(ctx context.Context, requestedNAS string) (DSMUpdateAvailableResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DSMUpdateAvailableResult{}, err
	}
	state, err := client.DSMUpdateAvailable(ctx)
	if err != nil {
		return DSMUpdateAvailableResult{}, authenticationError(name, err)
	}
	return DSMUpdateAvailableResult{NAS: name, Available: state}, nil
}

func (s *Service) GetDSMUpdatePolicy(ctx context.Context, requestedNAS string) (DSMUpdatePolicyResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DSMUpdatePolicyResult{}, err
	}
	state, err := client.DSMUpdatePolicy(ctx)
	if err != nil {
		return DSMUpdatePolicyResult{}, authenticationError(name, err)
	}
	return DSMUpdatePolicyResult{NAS: name, Policy: state}, nil
}

func (s *Service) GetDSMUpdateConfigBackup(ctx context.Context, requestedNAS string) (DSMUpdateConfigBackupResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DSMUpdateConfigBackupResult{}, err
	}
	state, err := client.DSMUpdateConfigBackup(ctx)
	if err != nil {
		return DSMUpdateConfigBackupResult{}, authenticationError(name, err)
	}
	return DSMUpdateConfigBackupResult{NAS: name, ConfigBackup: state}, nil
}
