package application

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type DownloadStationCapabilitiesResult struct {
	NAS          string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.DownloadStationCapabilities `json:"capabilities" jsonschema:"Download Station reads currently exposed by dsmctl"`
	Report       synology.CompatibilityReport         `json:"report" jsonschema:"Discovered APIs and selected Download Station backends"`
}

type DownloadStationServiceResult struct {
	NAS     string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Service synology.DownloadStationServiceState `json:"service" jsonschema:"Normalized Download Station service configuration"`
}

type DownloadStationTasksResult struct {
	NAS   string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Tasks synology.DownloadStationTasks `json:"tasks" jsonschema:"Download task list"`
}

type DownloadStationStatisticsResult struct {
	NAS        string                             `json:"nas" jsonschema:"NAS profile used for the request"`
	Statistics synology.DownloadStationStatistics `json:"statistics" jsonschema:"Aggregate transfer statistics"`
}

func (s *Service) GetDownloadStationCapabilities(ctx context.Context, requestedNAS string) (DownloadStationCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DownloadStationCapabilitiesResult{}, err
	}
	capabilities, report, err := client.DownloadStationCapabilities(ctx)
	if err != nil {
		return DownloadStationCapabilitiesResult{}, authenticationError(name, err)
	}
	return DownloadStationCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetDownloadStationService(ctx context.Context, requestedNAS string) (DownloadStationServiceResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DownloadStationServiceResult{}, err
	}
	state, err := client.DownloadStationServiceState(ctx)
	if err != nil {
		return DownloadStationServiceResult{}, authenticationError(name, err)
	}
	return DownloadStationServiceResult{NAS: name, Service: state}, nil
}

func (s *Service) GetDownloadStationTasks(ctx context.Context, requestedNAS string) (DownloadStationTasksResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DownloadStationTasksResult{}, err
	}
	tasks, err := client.DownloadStationTasks(ctx)
	if err != nil {
		return DownloadStationTasksResult{}, authenticationError(name, err)
	}
	return DownloadStationTasksResult{NAS: name, Tasks: tasks}, nil
}

func (s *Service) GetDownloadStationStatistics(ctx context.Context, requestedNAS string) (DownloadStationStatisticsResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DownloadStationStatisticsResult{}, err
	}
	stats, err := client.DownloadStationStatistics(ctx)
	if err != nil {
		return DownloadStationStatisticsResult{}, authenticationError(name, err)
	}
	return DownloadStationStatisticsResult{NAS: name, Statistics: stats}, nil
}
