// Package driveadmin implements independently selectable read operations for
// the Synology Drive Server Admin Console: service status, active connections,
// team folders, and Drive server logs.
//
// Drive's WebAPI behavior follows the installed SynologyDrive package version,
// not the DSM release, so every variant composes its API matcher with a
// package-version baseline: the Drive 3+/4 Admin Console API family verified
// against the configured target (Drive 4.0.3). Older Drive or Cloud Station
// generations fail closed instead of receiving untested requests, and a NAS
// without the package reports each operation as unsupported with the package
// evidence in the selection reason.
//
// Team-folder changes are modeled as a variant-less operation so capabilities
// can name them, but they have no backend in this slice and always fail closed.
package driveadmin

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/driveadmin"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// PackageID is the stable DSM package identifier that owns every API in this
// module.
const PackageID = "SynologyDrive"

// DSM WebAPI anchors, verified by SYNO.API.Info discovery on the configured
// target and the Admin Console client evidence recorded in WI-022.
const (
	StatusAPIName     = "SYNO.SynologyDrive"
	ConnectionAPIName = "SYNO.SynologyDrive.Connection"
	ShareAPIName      = "SYNO.SynologyDrive.Share"
	LogAPIName        = "SYNO.SynologyDrive.Log"

	StatusCapabilityName         = "drive.admin.status.read"
	ConnectionsCapabilityName    = "drive.admin.connections.read"
	TeamFoldersCapabilityName    = "drive.admin.teamfolders.read"
	LogCapabilityName            = "drive.admin.log.read"
	TeamFoldersSetCapabilityName = "drive.admin.teamfolders.set"
)

// baselinePackage gates every variant on the verified Drive 3+/4 Admin Console
// API family. The exclusive maximum is unbounded; a future Drive release with a
// verified behavior difference adds a higher-priority variant with a narrower
// range instead of editing this baseline.
var baselinePackage = compatibility.PackageVersionRange(
	PackageID, compatibility.ParsePackageVersion("3.0"), compatibility.PackageVersion{},
)

// Input is the empty input for parameterless reads.
type Input struct{}

// teamFolderPageLimit bounds the verified Share.list read. Team folders map to
// shared folders, which DSM caps far below this value.
const teamFolderPageLimit = 1000

var statusOperation = compatibility.Operation[Input, driveadmin.ServiceStatus]{
	Name: StatusCapabilityName,
	Variants: []compatibility.Variant[Input, driveadmin.ServiceStatus]{
		{
			Name: "drive-status-v1", API: StatusAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(StatusAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (driveadmin.ServiceStatus, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: StatusAPIName, Version: 1, Method: "get_status",
				})
				if err != nil {
					return driveadmin.ServiceStatus{}, fmt.Errorf("call %s.get_status v1: %w", StatusAPIName, err)
				}
				return decodeServiceStatus(data)
			},
		},
	},
}

var connectionsOperation = compatibility.Operation[Input, driveadmin.Connections]{
	Name: ConnectionsCapabilityName,
	Variants: []compatibility.Variant[Input, driveadmin.Connections]{
		{
			Name: "drive-connection-v1", API: ConnectionAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(ConnectionAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (driveadmin.Connections, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: ConnectionAPIName, Version: 1, Method: "list",
				})
				if err != nil {
					return driveadmin.Connections{}, fmt.Errorf("call %s.list v1: %w", ConnectionAPIName, err)
				}
				return decodeConnections(data)
			},
		},
	},
}

var teamFoldersOperation = compatibility.Operation[Input, driveadmin.TeamFolders]{
	Name: TeamFoldersCapabilityName,
	Variants: []compatibility.Variant[Input, driveadmin.TeamFolders]{
		{
			Name: "drive-share-v1", API: ShareAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(ShareAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (driveadmin.TeamFolders, error) {
				// Verified live on Drive 4.0.3: list rejects the request (120)
				// unless paging and a valid sort column are present.
				data, err := executor.Execute(ctx, compatibility.Request{
					API: ShareAPIName, Version: 1, Method: "list",
					JSONParameters: map[string]any{
						"offset": 0, "limit": teamFolderPageLimit,
						"sort_by": "share_name", "sort_direction": "ASC",
					},
				})
				if err != nil {
					return driveadmin.TeamFolders{}, fmt.Errorf("call %s.list v1: %w", ShareAPIName, err)
				}
				return decodeTeamFolders(data)
			},
		},
	},
}

var logOperation = compatibility.Operation[driveadmin.LogQuery, driveadmin.Log]{
	Name: LogCapabilityName,
	Variants: []compatibility.Variant[driveadmin.LogQuery, driveadmin.Log]{
		{
			Name: "drive-log-v1", API: LogAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(LogAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, query driveadmin.LogQuery) (driveadmin.Log, error) {
				// Verified live on Drive 4.0.3: target is required. The Admin
				// Console's "all" view sends share_type "all" with target
				// "user"; one team folder is share_type "share" with an
				// @-prefixed shared-folder name. log_type is Drive's numeric
				// event-code array filter; empty means every event type.
				parameters := map[string]any{
					"share_type": "all",
					"target":     "user",
					"log_type":   []int{},
					"get_all":    false,
					"offset":     query.Offset,
					"limit":      query.Limit,
				}
				if query.TeamFolder != "" {
					parameters["share_type"] = "share"
					parameters["target"] = "@" + query.TeamFolder
				}
				if query.Keyword != "" {
					parameters["keyword"] = query.Keyword
				}
				if query.Username != "" {
					parameters["username"] = query.Username
				}
				if query.From > 0 {
					parameters["datefrom"] = query.From
				}
				if query.To > 0 {
					parameters["dateto"] = query.To
				}
				data, err := executor.Execute(ctx, compatibility.Request{
					API: LogAPIName, Version: 1, Method: "list", JSONParameters: parameters,
				})
				if err != nil {
					return driveadmin.Log{}, fmt.Errorf("call %s.list v1: %w", LogAPIName, err)
				}
				return decodeLog(data)
			},
		},
	},
}

// teamFoldersSetOperation is modeled but has no variants, so it always reports
// unsupported and fails closed until the first verified write backend ships.
var teamFoldersSetOperation = compatibility.Operation[Input, struct{}]{Name: TeamFoldersSetCapabilityName}

// APINames lists every DSM API this module may use, so the facade can discover
// them in one call before selecting variants.
func APINames() []string {
	return []string{StatusAPIName, ConnectionAPIName, ShareAPIName, LogAPIName, ConfigAPIName}
}

func SelectStatus(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := statusOperation.Select(target)
	return selection, err
}

func SelectConnections(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := connectionsOperation.Select(target)
	return selection, err
}

func SelectTeamFolders(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := teamFoldersOperation.Select(target)
	return selection, err
}

func SelectLog(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := logOperation.Select(target)
	return selection, err
}

func SelectTeamFoldersSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := teamFoldersSetOperation.Select(target)
	return selection, err
}

// Select returns every Drive Admin operation selection in a stable order for
// capability reporting: status, connections, team folders, log, team-folder set.
func Select(target compatibility.Target) ([]compatibility.Selection, error) {
	selectors := []func(compatibility.Target) (compatibility.Selection, error){
		SelectStatus, SelectConnections, SelectTeamFolders, SelectLog, SelectTeamFoldersSet,
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selectOperation := range selectors {
		selection, err := selectOperation(target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return nil, err
		}
		selections = append(selections, selection)
	}
	return selections, nil
}

func ExecuteStatus(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (driveadmin.ServiceStatus, compatibility.Selection, error) {
	return statusOperation.Run(ctx, target, executor, Input{})
}

func ExecuteConnections(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (driveadmin.Connections, compatibility.Selection, error) {
	return connectionsOperation.Run(ctx, target, executor, Input{})
}

func ExecuteTeamFolders(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (driveadmin.TeamFolders, compatibility.Selection, error) {
	return teamFoldersOperation.Run(ctx, target, executor, Input{})
}

func ExecuteLog(ctx context.Context, target compatibility.Target, executor compatibility.Executor, query driveadmin.LogQuery) (driveadmin.Log, compatibility.Selection, error) {
	return logOperation.Run(ctx, target, executor, query)
}
