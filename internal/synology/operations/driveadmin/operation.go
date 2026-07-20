// Package driveadmin implements independently selectable operations for the
// Synology Drive Server Admin Console: service status, active connections,
// team folders (read and guarded set), and Drive server logs.
//
// Drive's WebAPI behavior follows the installed SynologyDrive package version,
// not the DSM release, so every variant composes its API matcher with a
// package-version baseline: the Drive 3+/4 Admin Console API family verified
// against the configured target (Drive 4.0.3). Older Drive or Cloud Station
// generations fail closed instead of receiving untested requests, and a NAS
// without the package reports each operation as unsupported with the package
// evidence in the selection reason.
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
	DBUsageAPIName    = "SYNO.SynologyDrive.DBUsage"
	DashboardAPIName  = "SYNO.SynologyDrive.Dashboard"
	ActivationAPIName = "SYNO.SynologyDrive.Activation"

	StatusCapabilityName            = "drive.admin.status.read"
	ConnectionsCapabilityName       = "drive.admin.connections.read"
	TeamFoldersCapabilityName       = "drive.admin.teamfolders.read"
	LogCapabilityName               = "drive.admin.log.read"
	TeamFoldersSetCapabilityName    = "drive.admin.teamfolders.set"
	ConnectionSummaryCapabilityName = "drive.admin.connections.summary.read"
	DBUsageCapabilityName           = "drive.admin.dbusage.read"
	DashboardCapabilityName         = "drive.admin.dashboard.read"
	ActivationCapabilityName        = "drive.admin.activation.read"
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

// TeamFolderSetInput is one Share.set entry. Enable routes the entry to the
// handler's enable/disable path when present; without it the entry is a
// versioning-only change and DSM merges omitted rotate fields from the stored
// view settings. Field semantics verified against the Drive server source
// (handlers/share/set.cpp) and live on Drive 4.0.3 (WI-050).
type TeamFolderSetInput struct {
	ShareName     string
	Enable        *bool
	MaxVersions   *int
	VersionPolicy string
	RetentionDays *int
}

// TeamFolderMutationResult records the selected backend for one team-folder set.
type TeamFolderMutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

// teamFoldersSetOperation performs one guarded team-folder change. The share
// parameter is a JSON array; exactly one entry is sent so a plan maps to one
// team folder. The handler answers success with an empty data object and
// silently skips ineligible shares, so callers must verify the postcondition
// by re-reading the team-folder list.
var teamFoldersSetOperation = compatibility.Operation[TeamFolderSetInput, TeamFolderMutationResult]{
	Name: TeamFoldersSetCapabilityName,
	Variants: []compatibility.Variant[TeamFolderSetInput, TeamFolderMutationResult]{
		{
			Name: "drive-share-v1", API: ShareAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(ShareAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, input TeamFolderSetInput) (TeamFolderMutationResult, error) {
				entry := map[string]any{"share_name": input.ShareName}
				if input.Enable != nil {
					entry["share_enable"] = *input.Enable
				}
				if input.MaxVersions != nil {
					entry["rotate_cnt"] = *input.MaxVersions
				}
				if input.VersionPolicy != "" {
					entry["rotate_policy"] = input.VersionPolicy
				}
				if input.RetentionDays != nil {
					entry["rotate_days"] = *input.RetentionDays
				}
				_, err := executor.Execute(ctx, compatibility.Request{
					API: ShareAPIName, Version: 1, Method: "set",
					JSONParameters: map[string]any{"share": []map[string]any{entry}},
				})
				if err != nil {
					return TeamFolderMutationResult{}, fmt.Errorf("call %s.set v1: %w", ShareAPIName, err)
				}
				return TeamFolderMutationResult{}, nil
			},
		},
	},
}

// connectionSummaryOperation reads the Admin Console overview counters. The
// summary method exists only at Connection v2 (verified live: v1 answers 103).
var connectionSummaryOperation = compatibility.Operation[Input, driveadmin.ConnectionSummary]{
	Name: ConnectionSummaryCapabilityName,
	Variants: []compatibility.Variant[Input, driveadmin.ConnectionSummary]{
		{
			Name: "drive-connection-v2", API: ConnectionAPIName, Version: 2, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(ConnectionAPIName, 2), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (driveadmin.ConnectionSummary, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: ConnectionAPIName, Version: 2, Method: "summary"})
				if err != nil {
					return driveadmin.ConnectionSummary{}, fmt.Errorf("call %s.summary v2: %w", ConnectionAPIName, err)
				}
				return decodeConnectionSummary(data)
			},
		},
	},
}

var dbUsageOperation = compatibility.Operation[Input, driveadmin.DBUsage]{
	Name: DBUsageCapabilityName,
	Variants: []compatibility.Variant[Input, driveadmin.DBUsage]{
		{
			Name: "drive-dbusage-v1", API: DBUsageAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(DBUsageAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (driveadmin.DBUsage, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: DBUsageAPIName, Version: 1, Method: "get"})
				if err != nil {
					return driveadmin.DBUsage{}, fmt.Errorf("call %s.get v1: %w", DBUsageAPIName, err)
				}
				return decodeDBUsage(data)
			},
		},
	},
}

var dashboardOperation = compatibility.Operation[driveadmin.TopAccessQuery, driveadmin.TopAccessFiles]{
	Name: DashboardCapabilityName,
	Variants: []compatibility.Variant[driveadmin.TopAccessQuery, driveadmin.TopAccessFiles]{
		{
			Name: "drive-dashboard-v1", API: DashboardAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(DashboardAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, query driveadmin.TopAccessQuery) (driveadmin.TopAccessFiles, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: DashboardAPIName, Version: 1, Method: "top_access_files",
					JSONParameters: map[string]any{
						"ranking_by":  query.RankingBy,
						"period_days": query.PeriodDays,
						"limit":       query.Limit,
						"offset":      query.Offset,
					},
				})
				if err != nil {
					return driveadmin.TopAccessFiles{}, fmt.Errorf("call %s.top_access_files v1: %w", DashboardAPIName, err)
				}
				return decodeTopAccessFiles(data)
			},
		},
	},
}

var activationOperation = compatibility.Operation[Input, driveadmin.Activation]{
	Name: ActivationCapabilityName,
	Variants: []compatibility.Variant[Input, driveadmin.Activation]{
		{
			Name: "drive-activation-v1", API: ActivationAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(ActivationAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (driveadmin.Activation, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: ActivationAPIName, Version: 1, Method: "get"})
				if err != nil {
					return driveadmin.Activation{}, fmt.Errorf("call %s.get v1: %w", ActivationAPIName, err)
				}
				return decodeActivation(data)
			},
		},
	},
}

// APINames lists every DSM API this module may use, so the facade can discover
// them in one call before selecting variants.
func APINames() []string {
	return []string{StatusAPIName, ConnectionAPIName, ShareAPIName, LogAPIName, ConfigAPIName, DBUsageAPIName, DashboardAPIName, ActivationAPIName}
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

func ExecuteTeamFoldersSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input TeamFolderSetInput) (TeamFolderMutationResult, compatibility.Selection, error) {
	result, selection, err := teamFoldersSetOperation.Run(ctx, target, executor, input)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

func SelectConnectionSummary(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := connectionSummaryOperation.Select(target)
	return selection, err
}

func SelectDBUsage(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := dbUsageOperation.Select(target)
	return selection, err
}

func SelectDashboard(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := dashboardOperation.Select(target)
	return selection, err
}

func SelectActivation(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := activationOperation.Select(target)
	return selection, err
}

func ExecuteConnectionSummary(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (driveadmin.ConnectionSummary, compatibility.Selection, error) {
	return connectionSummaryOperation.Run(ctx, target, executor, Input{})
}

func ExecuteDBUsage(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (driveadmin.DBUsage, compatibility.Selection, error) {
	return dbUsageOperation.Run(ctx, target, executor, Input{})
}

func ExecuteDashboard(ctx context.Context, target compatibility.Target, executor compatibility.Executor, query driveadmin.TopAccessQuery) (driveadmin.TopAccessFiles, compatibility.Selection, error) {
	return dashboardOperation.Run(ctx, target, executor, query)
}

func ExecuteActivation(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (driveadmin.Activation, compatibility.Selection, error) {
	return activationOperation.Run(ctx, target, executor, Input{})
}
