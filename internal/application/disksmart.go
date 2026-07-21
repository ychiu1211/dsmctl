package application

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type DiskSMARTCapabilitiesResult struct {
	NAS          string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.DiskSMARTCapabilities `json:"capabilities" jsonschema:"Disk-SMART read areas currently exposed by dsmctl"`
	Report       synology.CompatibilityReport   `json:"report" jsonschema:"Discovered APIs and selected disk-SMART compatibility backends"`
}

type DiskHealthResult struct {
	NAS    string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Health synology.DiskHealthState `json:"health" jsonschema:"Per-disk health, lifespan, and coarse self-test state plus global warning thresholds"`
}

type DiskSMARTAttributesResult struct {
	NAS   string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	SMART synology.DiskSMARTState `json:"smart" jsonschema:"Per-disk SMART attribute tables, summaries, and self-test status"`
}

func (s *Service) GetDiskSMARTCapabilities(ctx context.Context, requestedNAS string) (DiskSMARTCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DiskSMARTCapabilitiesResult{}, err
	}
	capabilities, report, err := client.DiskSMARTCapabilities(ctx)
	if err != nil {
		return DiskSMARTCapabilitiesResult{}, authenticationError(name, err)
	}
	return DiskSMARTCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetDiskHealth(ctx context.Context, requestedNAS string) (DiskHealthResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DiskHealthResult{}, err
	}
	state, err := client.DiskHealth(ctx)
	if err != nil {
		return DiskHealthResult{}, authenticationError(name, err)
	}
	return DiskHealthResult{NAS: name, Health: state}, nil
}

func (s *Service) GetDiskSMARTAttributes(ctx context.Context, requestedNAS string) (DiskSMARTAttributesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DiskSMARTAttributesResult{}, err
	}
	state, err := client.DiskSMARTAttributes(ctx)
	if err != nil {
		return DiskSMARTAttributesResult{}, authenticationError(name, err)
	}
	return DiskSMARTAttributesResult{NAS: name, SMART: state}, nil
}
