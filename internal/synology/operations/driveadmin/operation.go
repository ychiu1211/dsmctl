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
	PrivilegeAPIName  = "SYNO.SynologyDrive.Privilege"
	NodeAPIName        = "SYNO.SynologyDrive.Node"
	NodeRestoreAPIName = "SYNO.SynologyDrive.Node.Restore"
	DashboardAPIName   = "SYNO.SynologyDrive.Dashboard"
	ActivationAPIName = "SYNO.SynologyDrive.Activation"

	StatusCapabilityName            = "drive.admin.status.read"
	ConnectionsCapabilityName       = "drive.admin.connections.read"
	TeamFoldersCapabilityName       = "drive.admin.teamfolders.read"
	LogCapabilityName               = "drive.admin.log.read"
	LogExportCapabilityName         = "drive.admin.log.export"
	TeamFoldersSetCapabilityName    = "drive.admin.teamfolders.set"
	ConnectionSummaryCapabilityName = "drive.admin.connections.summary.read"
	ConnectionKickCapabilityName    = "drive.admin.connections.kick"
	DBUsageCapabilityName           = "drive.admin.dbusage.read"
	DashboardCapabilityName         = "drive.admin.dashboard.read"
	ActivationCapabilityName        = "drive.admin.activation.read"
	PrivilegeReadCapabilityName     = "drive.admin.privilege.read"
	NodesReadCapabilityName         = "drive.admin.nodes.read"
	NodeVersionsReadCapabilityName  = "drive.admin.nodeversions.read"
	NodeRestoreCapabilityName       = "drive.admin.node.restore"
)

// NodeTarget maps the stable team-folder model to Drive's target parameter:
// verified live on Drive 4.0.3, "user" is the calling account's My Drive and
// "@<shared-folder-name>" is a team folder view.
func NodeTarget(teamFolder string) string {
	if teamFolder == "" {
		return "user"
	}
	return "@" + teamFolder
}

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

// ConnectionKickInput disconnects one client session by its session id.
type ConnectionKickInput struct {
	SessionID string
}

// ConnectionMutationResult records the selected backend for one kick.
type ConnectionMutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

// connectionKickOperation removes one client session. Source-verified
// (handlers/connection/delete.cpp): the client_sess_id parameter is a JSON
// array of session ids; dsmctl sends exactly one and never sends the
// data_wipe companion (remote wipe stays out of scope). The handler answers
// an empty success, so callers verify by re-reading the connection list.
var connectionKickOperation = compatibility.Operation[ConnectionKickInput, ConnectionMutationResult]{
	Name: ConnectionKickCapabilityName,
	Variants: []compatibility.Variant[ConnectionKickInput, ConnectionMutationResult]{
		{
			Name: "drive-connection-v2", API: ConnectionAPIName, Version: 2, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(ConnectionAPIName, 2), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, input ConnectionKickInput) (ConnectionMutationResult, error) {
				_, err := executor.Execute(ctx, compatibility.Request{
					API: ConnectionAPIName, Version: 2, Method: "delete",
					JSONParameters: map[string]any{"client_sess_id": []string{input.SessionID}},
				})
				if err != nil {
					return ConnectionMutationResult{}, fmt.Errorf("call %s.delete v2: %w", ConnectionAPIName, err)
				}
				return ConnectionMutationResult{}, nil
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

var privilegeListOperation = compatibility.Operation[driveadmin.PrivilegeQuery, driveadmin.PrivilegeList]{
	Name: PrivilegeReadCapabilityName,
	Variants: []compatibility.Variant[driveadmin.PrivilegeQuery, driveadmin.PrivilegeList]{
		{
			Name: "drive-privilege-v1", API: PrivilegeAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(PrivilegeAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, query driveadmin.PrivilegeQuery) (driveadmin.PrivilegeList, error) {
				// Verified live on Drive 4.0.3: additional must be an array
				// (a bare boolean is rejected with 120) and unlocks the
				// enabled/status fields; limit -1 returns every account.
				parameters := map[string]any{
					"type": query.Type, "offset": 0, "limit": -1,
					"additional": []string{"enabled", "status"},
				}
				if query.DomainName != "" {
					parameters["domain_name"] = query.DomainName
				}
				data, err := executor.Execute(ctx, compatibility.Request{
					API: PrivilegeAPIName, Version: 1, Method: "list", JSONParameters: parameters,
				})
				if err != nil {
					return driveadmin.PrivilegeList{}, fmt.Errorf("call %s.list v1: %w", PrivilegeAPIName, err)
				}
				return decodePrivilegeList(data)
			},
		},
	},
}

var nodeListOperation = compatibility.Operation[driveadmin.NodeQuery, driveadmin.Nodes]{
	Name: NodesReadCapabilityName,
	Variants: []compatibility.Variant[driveadmin.NodeQuery, driveadmin.Nodes]{
		{
			Name: "drive-node-v1", API: NodeAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(NodeAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, query driveadmin.NodeQuery) (driveadmin.Nodes, error) {
				parameters := map[string]any{
					"target": NodeTarget(query.TeamFolder),
					"offset": query.Offset, "limit": query.Limit,
					"list_removed": !query.ExcludeRemoved,
				}
				if query.Pattern != "" {
					parameters["pattern"] = query.Pattern
				}
				if query.Recursive {
					parameters["recursive"] = true
				}
				data, err := executor.Execute(ctx, compatibility.Request{
					API: NodeAPIName, Version: 1, Method: "list", JSONParameters: parameters,
				})
				if err != nil {
					return driveadmin.Nodes{}, fmt.Errorf("call %s.list v1: %w", NodeAPIName, err)
				}
				return decodeNodes(data)
			},
		},
	},
}

var nodeVersionsOperation = compatibility.Operation[driveadmin.NodeVersionQuery, driveadmin.NodeVersions]{
	Name: NodeVersionsReadCapabilityName,
	Variants: []compatibility.Variant[driveadmin.NodeVersionQuery, driveadmin.NodeVersions]{
		{
			Name: "drive-node-v1", API: NodeAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(NodeAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, query driveadmin.NodeVersionQuery) (driveadmin.NodeVersions, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: NodeAPIName, Version: 1, Method: "list_version",
					JSONParameters: map[string]any{
						"target": NodeTarget(query.TeamFolder),
						"path":   query.Path,
					},
				})
				if err != nil {
					return driveadmin.NodeVersions{}, fmt.Errorf("call %s.list_version v1: %w", NodeAPIName, err)
				}
				versions, err := decodeNodeVersions(data)
				if err != nil {
					return driveadmin.NodeVersions{}, err
				}
				versions.Path = query.Path
				return versions, nil
			},
		},
	},
}

func SelectNodes(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := nodeListOperation.Select(target)
	return selection, err
}

func SelectNodeVersions(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := nodeVersionsOperation.Select(target)
	return selection, err
}

func ExecuteNodes(ctx context.Context, target compatibility.Target, executor compatibility.Executor, query driveadmin.NodeQuery) (driveadmin.Nodes, compatibility.Selection, error) {
	return nodeListOperation.Run(ctx, target, executor, query)
}

func ExecuteNodeVersions(ctx context.Context, target compatibility.Target, executor compatibility.Executor, query driveadmin.NodeVersionQuery) (driveadmin.NodeVersions, compatibility.Selection, error) {
	return nodeVersionsOperation.Run(ctx, target, executor, query)
}

// NodeRestoreItem is one node descriptor for the restore task, built from the
// files read (Node.list): the handler needs node_id, sync_id, file_type
// (1 = folder), path, and name.
type NodeRestoreItem struct {
	NodeID   string
	SyncID   string
	FileType int
	Path     string
	Name     string
}

// NodeRestoreInput starts one restore task. Target is Drive's target form
// (user or @share); IncludeRemoved recurses into removed folders and Override
// replaces present content. Verified against handlers/node/restore/start.cpp.
type NodeRestoreInput struct {
	Target         string
	CopyTo         string
	Override       bool
	IncludeRemoved bool
	Nodes          []NodeRestoreItem
}

// nodeRestoreStartOperation gates the restore capability. status/finish reuse
// the same backend selection and are driven by the facade's async loop.
var nodeRestoreStartOperation = compatibility.Operation[NodeRestoreInput, string]{
	Name: NodeRestoreCapabilityName,
	Variants: []compatibility.Variant[NodeRestoreInput, string]{
		{
			Name: "drive-node-restore-v1", API: NodeRestoreAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(NodeRestoreAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, input NodeRestoreInput) (string, error) {
				nodes := make([]map[string]any, 0, len(input.Nodes))
				for _, node := range input.Nodes {
					entry := map[string]any{
						"node_id":   node.NodeID,
						"file_type": node.FileType,
						"path":      node.Path,
						"name":      node.Name,
					}
					// sync_id is stringified by the handler (std::stoull); send
					// "0" when the read did not report one (removed nodes).
					if node.SyncID != "" {
						entry["sync_id"] = node.SyncID
					} else {
						entry["sync_id"] = "0"
					}
					nodes = append(nodes, entry)
				}
				parameters := map[string]any{
					"target":          input.Target,
					"nodes":           nodes,
					"override":        input.Override,
					"include_removed": input.IncludeRemoved,
				}
				if input.CopyTo != "" {
					parameters["copy_to"] = input.CopyTo
				}
				data, err := executor.Execute(ctx, compatibility.Request{
					API: NodeRestoreAPIName, Version: 1, Method: "start", JSONParameters: parameters,
				})
				if err != nil {
					return "", fmt.Errorf("call %s.start v1: %w", NodeRestoreAPIName, err)
				}
				return decodeRestoreTaskID(data)
			},
		},
	},
}

func SelectNodeRestore(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := nodeRestoreStartOperation.Select(target)
	return selection, err
}

// ExecuteNodeRestoreStart begins the restore task and returns its task id and
// the selected backend for evidence.
func ExecuteNodeRestoreStart(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input NodeRestoreInput) (string, compatibility.Selection, error) {
	return nodeRestoreStartOperation.Run(ctx, target, executor, input)
}

// NodeRestoreProgress is one status poll of the singleton restore task.
type NodeRestoreProgress struct {
	Current int
	Total   int
}

// ExecuteNodeRestoreStatus polls the singleton restore task. The task carries
// no id in the status request (it is a per-admin singleton).
func ExecuteNodeRestoreStatus(ctx context.Context, executor compatibility.Executor) (NodeRestoreProgress, error) {
	data, err := executor.Execute(ctx, compatibility.Request{API: NodeRestoreAPIName, Version: 1, Method: "status"})
	if err != nil {
		return NodeRestoreProgress{}, fmt.Errorf("call %s.status v1: %w", NodeRestoreAPIName, err)
	}
	return decodeRestoreProgress(data)
}

// ExecuteNodeRestoreFinish clears the singleton restore task.
func ExecuteNodeRestoreFinish(ctx context.Context, executor compatibility.Executor) error {
	if _, err := executor.Execute(ctx, compatibility.Request{API: NodeRestoreAPIName, Version: 1, Method: "finish"}); err != nil {
		return fmt.Errorf("call %s.finish v1: %w", NodeRestoreAPIName, err)
	}
	return nil
}

// Drive's own Privilege.set is deliberately not exposed. Live verification
// on Drive 4.0.3 showed the DSM application privilege
// (SYNO.SDS.Drive.Application, managed by the account module) is the real
// access control: the privilege view lists exactly the accounts the app
// privilege allows, and a Drive-side disable does not stick while the app
// privilege still allows the account (Drive re-materializes the user row).

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
	return []string{StatusAPIName, ConnectionAPIName, ShareAPIName, LogAPIName, ConfigAPIName, DBUsageAPIName, DashboardAPIName, ActivationAPIName, PrivilegeAPIName, NodeAPIName, NodeRestoreAPIName}
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

// logExportOperation gates the Drive log export. It shares the Log API v1
// backend with the log read; the export itself uses the raw file transport in
// the facade because it answers a file rather than the JSON envelope.
var logExportOperation = compatibility.Operation[Input, struct{}]{
	Name: LogExportCapabilityName,
	Variants: []compatibility.Variant[Input, struct{}]{
		{
			Name: "drive-log-v1", API: LogAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(LogAPIName, 1), baselinePackage),
			Execute: func(context.Context, compatibility.Executor, Input) (struct{}, error) {
				return struct{}{}, nil
			},
		},
	},
}

func SelectLogExport(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := logExportOperation.Select(target)
	return selection, err
}

func SelectConnectionSummary(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := connectionSummaryOperation.Select(target)
	return selection, err
}

func SelectConnectionKick(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := connectionKickOperation.Select(target)
	return selection, err
}

func ExecuteConnectionKick(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input ConnectionKickInput) (ConnectionMutationResult, compatibility.Selection, error) {
	result, selection, err := connectionKickOperation.Run(ctx, target, executor, input)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "delete"
	}
	return result, selection, err
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

func SelectPrivilegeList(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := privilegeListOperation.Select(target)
	return selection, err
}

func ExecutePrivilegeList(ctx context.Context, target compatibility.Target, executor compatibility.Executor, query driveadmin.PrivilegeQuery) (driveadmin.PrivilegeList, compatibility.Selection, error) {
	return privilegeListOperation.Run(ctx, target, executor, query)
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
