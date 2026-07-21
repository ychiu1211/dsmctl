package application

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type UniversalSearchCapabilitiesResult struct {
	NAS          string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.UniversalSearchCapabilities `json:"capabilities" jsonschema:"Universal Search reads currently exposed by dsmctl"`
	Report       synology.CompatibilityReport         `json:"report" jsonschema:"Discovered APIs and selected Universal Search backends"`
}

type UniversalSearchFoldersResult struct {
	NAS     string                                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Folders synology.UniversalSearchIndexedFolders `json:"folders" jsonschema:"Universal Search indexed-folder list"`
}

type UniversalSearchStatusResult struct {
	NAS    string                              `json:"nas" jsonschema:"NAS profile used for the request"`
	Status synology.UniversalSearchIndexStatus `json:"status" jsonschema:"Overall Universal Search index daemon status"`
}

func (s *Service) GetUniversalSearchCapabilities(ctx context.Context, requestedNAS string) (UniversalSearchCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return UniversalSearchCapabilitiesResult{}, err
	}
	capabilities, report, err := client.UniversalSearchCapabilities(ctx)
	if err != nil {
		return UniversalSearchCapabilitiesResult{}, authenticationError(name, err)
	}
	return UniversalSearchCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetUniversalSearchFolders(ctx context.Context, requestedNAS string) (UniversalSearchFoldersResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return UniversalSearchFoldersResult{}, err
	}
	folders, err := client.UniversalSearchIndexedFolders(ctx)
	if err != nil {
		return UniversalSearchFoldersResult{}, authenticationError(name, err)
	}
	return UniversalSearchFoldersResult{NAS: name, Folders: folders}, nil
}

func (s *Service) GetUniversalSearchStatus(ctx context.Context, requestedNAS string) (UniversalSearchStatusResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return UniversalSearchStatusResult{}, err
	}
	status, err := client.UniversalSearchIndexStatus(ctx)
	if err != nil {
		return UniversalSearchStatusResult{}, authenticationError(name, err)
	}
	return UniversalSearchStatusResult{NAS: name, Status: status}, nil
}
