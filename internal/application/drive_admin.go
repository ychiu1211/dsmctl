package application

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/driveadmin"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

// Drive Admin log paging bounds mirror the DSM system log module.
const (
	driveAdminDefaultLogLimit = 100
	driveAdminMaxLogLimit     = 1000
)

type DriveAdminCapabilitiesResult struct {
	NAS          string                          `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.DriveAdminCapabilities `json:"capabilities" jsonschema:"Drive Admin operations currently exposed by dsmctl, with installed-package evidence"`
	Report       synology.CompatibilityReport    `json:"report" jsonschema:"Discovered APIs, installed packages, and selected Drive Admin backends"`
}

type DriveAdminStatusResult struct {
	NAS    string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	Status synology.DriveAdminStatus `json:"status" jsonschema:"Normalized Drive service status with installed-package evidence"`
}

type DriveAdminConnectionsResult struct {
	NAS         string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Connections synology.DriveAdminConnections `json:"connections" jsonschema:"Active Drive client connections"`
}

type DriveAdminTeamFoldersResult struct {
	NAS         string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	TeamFolders synology.DriveAdminTeamFolders `json:"team_folders" jsonschema:"Drive team folders from the admin perspective"`
}

type DriveAdminLogResult struct {
	NAS string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Log synology.DriveAdminLog `json:"log" jsonschema:"Drive server log entries for the requested page"`
}

type driveAdminClient interface {
	DriveAdminStatus(context.Context) (synology.DriveAdminStatus, error)
	DriveAdminConnections(context.Context) (synology.DriveAdminConnections, error)
	DriveAdminTeamFolders(context.Context) (synology.DriveAdminTeamFolders, error)
	DriveAdminLog(context.Context, synology.DriveAdminLogQuery) (synology.DriveAdminLog, error)
	DriveAdminCapabilities(context.Context) (synology.DriveAdminCapabilities, synology.CompatibilityReport, error)
}

func (s *Service) GetDriveAdminCapabilities(ctx context.Context, requestedNAS string) (DriveAdminCapabilitiesResult, error) {
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveAdminCapabilitiesResult{}, err
	}
	capabilities, report, err := client.DriveAdminCapabilities(ctx)
	if err != nil {
		return DriveAdminCapabilitiesResult{}, authenticationError(name, err)
	}
	return DriveAdminCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetDriveAdminStatus(ctx context.Context, requestedNAS string) (DriveAdminStatusResult, error) {
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveAdminStatusResult{}, err
	}
	status, err := client.DriveAdminStatus(ctx)
	if err != nil {
		return DriveAdminStatusResult{}, authenticationError(name, err)
	}
	return DriveAdminStatusResult{NAS: name, Status: status}, nil
}

func (s *Service) GetDriveAdminConnections(ctx context.Context, requestedNAS string) (DriveAdminConnectionsResult, error) {
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveAdminConnectionsResult{}, err
	}
	connections, err := client.DriveAdminConnections(ctx)
	if err != nil {
		return DriveAdminConnectionsResult{}, authenticationError(name, err)
	}
	return DriveAdminConnectionsResult{NAS: name, Connections: connections}, nil
}

func (s *Service) GetDriveAdminTeamFolders(ctx context.Context, requestedNAS string) (DriveAdminTeamFoldersResult, error) {
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveAdminTeamFoldersResult{}, err
	}
	folders, err := client.DriveAdminTeamFolders(ctx)
	if err != nil {
		return DriveAdminTeamFoldersResult{}, authenticationError(name, err)
	}
	return DriveAdminTeamFoldersResult{NAS: name, TeamFolders: folders}, nil
}

func (s *Service) GetDriveAdminLog(ctx context.Context, requestedNAS string, query driveadmin.LogQuery) (DriveAdminLogResult, error) {
	if err := validateDriveAdminLogQuery(&query); err != nil {
		return DriveAdminLogResult{}, err
	}
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveAdminLogResult{}, err
	}
	log, err := client.DriveAdminLog(ctx, query)
	if err != nil {
		return DriveAdminLogResult{}, authenticationError(name, err)
	}
	return DriveAdminLogResult{NAS: name, Log: log}, nil
}

func validateDriveAdminLogQuery(query *driveadmin.LogQuery) error {
	if query.Limit < 0 {
		return fmt.Errorf("log limit cannot be negative")
	}
	if query.Limit == 0 {
		query.Limit = driveAdminDefaultLogLimit
	}
	if query.Limit > driveAdminMaxLogLimit {
		return fmt.Errorf("log limit %d exceeds the maximum %d", query.Limit, driveAdminMaxLogLimit)
	}
	if query.From < 0 || query.To < 0 {
		return fmt.Errorf("log time bounds must be Unix seconds at or after 0")
	}
	if query.From > 0 && query.To > 0 && query.To < query.From {
		return fmt.Errorf("log time upper bound is before the lower bound")
	}
	return nil
}

func (s *Service) driveAdminClient(ctx context.Context, requestedNAS string) (string, driveAdminClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(driveAdminClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement Drive Admin management")
	}
	return name, client, nil
}

var _ driveAdminClient = (*synology.Client)(nil)
