package application

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type KMIPCapabilitiesResult struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.KMIPCapabilities    `json:"capabilities" jsonschema:"KMIP read surface currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and the selected KMIP compatibility backend"`
}

type KMIPStatusResult struct {
	NAS    string              `json:"nas" jsonschema:"NAS profile used for the request"`
	Status synology.KMIPStatus `json:"status" jsonschema:"KMIP role/status: local server and external client configuration with non-secret certificate bindings"`
}

func (s *Service) GetKMIPCapabilities(ctx context.Context, requestedNAS string) (KMIPCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return KMIPCapabilitiesResult{}, err
	}
	capabilities, report, err := client.KMIPCapabilitiesState(ctx)
	if err != nil {
		return KMIPCapabilitiesResult{}, authenticationError(name, err)
	}
	return KMIPCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetKMIPStatus(ctx context.Context, requestedNAS string) (KMIPStatusResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return KMIPStatusResult{}, err
	}
	status, err := client.KMIPStatusState(ctx)
	if err != nil {
		return KMIPStatusResult{}, authenticationError(name, err)
	}
	return KMIPStatusResult{NAS: name, Status: status}, nil
}
