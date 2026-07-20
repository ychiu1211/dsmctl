package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/driveadmin"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	driveops "github.com/ychiu1211/dsmctl/internal/synology/operations/driveadmin"
)

type DriveAdminStatus = driveadmin.ServiceStatus
type DriveAdminConnections = driveadmin.Connections
type DriveAdminTeamFolders = driveadmin.TeamFolders
type DriveAdminTeamFolder = driveadmin.TeamFolder
type DriveAdminLog = driveadmin.Log
type DriveAdminLogQuery = driveadmin.LogQuery
type DriveAdminCapabilities = driveadmin.Capabilities
type DriveServerConfig = driveadmin.ServerConfig
type DriveServerConfigChange = driveadmin.ServerConfigChange
type DriveConfigMutationResult = driveops.ConfigMutationResult
type DriveTeamFolderChange = driveadmin.TeamFolderChange
type DriveTeamFolderMutationResult = driveops.TeamFolderMutationResult
type DriveConnectionSummary = driveadmin.ConnectionSummary
type DriveConnection = driveadmin.Connection
type DriveConnectionKick = driveadmin.ConnectionKick
type DriveConnectionMutationResult = driveops.ConnectionMutationResult
type DriveDBUsage = driveadmin.DBUsage
type DriveTopAccessQuery = driveadmin.TopAccessQuery
type DriveTopAccessFiles = driveadmin.TopAccessFiles
type DriveActivation = driveadmin.Activation
type DrivePrivilegeList = driveadmin.PrivilegeList
type DrivePrivilegeQuery = driveadmin.PrivilegeQuery
type DriveNodeQuery = driveadmin.NodeQuery
type DriveNodes = driveadmin.Nodes
type DriveNodeVersionQuery = driveadmin.NodeVersionQuery
type DriveNodeVersions = driveadmin.NodeVersions

// driveAdminEvidenceLocked reports the installed SynologyDrive package as
// observed by the catalog refresh that ran in preparePackageScopedTargetLocked.
func (c *Client) driveAdminEvidenceLocked() driveadmin.PackageEvidence {
	evidence := driveadmin.PackageEvidence{ID: driveops.PackageID}
	if installed, ok := c.target.InstalledPackage(driveops.PackageID); ok {
		evidence.Installed = true
		evidence.Version = installed.Version.Raw
		evidence.Running = installed.Running
	}
	return evidence
}

// driveAdminReadError explains a failed Drive read using the package evidence:
// an installed-but-stopped Drive fails with guidance instead of a bare DSM
// error code.
func driveAdminReadError(what string, evidence driveadmin.PackageEvidence, err error) error {
	if evidence.Installed && !evidence.Running {
		return fmt.Errorf("get Drive %s: the SynologyDrive package is installed but not running; start it with a package lifecycle plan and retry: %w", what, err)
	}
	return fmt.Errorf("get Drive %s: %w", what, err)
}

// DriveAdminStatus reads the Drive service status. The installed-package
// catalog is refreshed first, so the returned evidence reflects this call.
func (c *Client) DriveAdminStatus(ctx context.Context) (DriveAdminStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveAdminStatus{}, fmt.Errorf("prepare Drive Admin target: %w", err)
	}
	evidence := c.driveAdminEvidenceLocked()
	status, _, err := driveops.ExecuteStatus(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DriveAdminStatus{}, driveAdminReadError("service status", evidence, err)
	}
	status.Package = evidence
	c.target.AddCapability(driveops.StatusCapabilityName)
	return status, nil
}

// DriveAdminConnections lists active Drive client connections.
func (c *Client) DriveAdminConnections(ctx context.Context) (DriveAdminConnections, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveAdminConnections{}, fmt.Errorf("prepare Drive Admin target: %w", err)
	}
	connections, _, err := driveops.ExecuteConnections(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DriveAdminConnections{}, driveAdminReadError("connections", c.driveAdminEvidenceLocked(), err)
	}
	c.target.AddCapability(driveops.ConnectionsCapabilityName)
	return connections, nil
}

// DriveAdminTeamFolders lists Drive team folders from the admin perspective.
func (c *Client) DriveAdminTeamFolders(ctx context.Context) (DriveAdminTeamFolders, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveAdminTeamFolders{}, fmt.Errorf("prepare Drive Admin target: %w", err)
	}
	folders, _, err := driveops.ExecuteTeamFolders(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DriveAdminTeamFolders{}, driveAdminReadError("team folders", c.driveAdminEvidenceLocked(), err)
	}
	c.target.AddCapability(driveops.TeamFoldersCapabilityName)
	return folders, nil
}

// DriveAdminLog reads Drive server log entries with Drive-applied filters.
func (c *Client) DriveAdminLog(ctx context.Context, query DriveAdminLogQuery) (DriveAdminLog, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveAdminLog{}, fmt.Errorf("prepare Drive Admin target: %w", err)
	}
	log, _, err := driveops.ExecuteLog(ctx, c.target, lockedExecutor{client: c}, query)
	if err != nil {
		return DriveAdminLog{}, driveAdminReadError("log", c.driveAdminEvidenceLocked(), err)
	}
	c.target.AddCapability(driveops.LogCapabilityName)
	return log, nil
}

// DriveNodes browses one Drive view (My Drive or a team folder), including
// removed entries — the Admin Console's rescue perspective.
func (c *Client) DriveNodes(ctx context.Context, query DriveNodeQuery) (DriveNodes, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveNodes{}, fmt.Errorf("prepare Drive Admin target: %w", err)
	}
	nodes, _, err := driveops.ExecuteNodes(ctx, c.target, lockedExecutor{client: c}, query)
	if err != nil {
		return DriveNodes{}, driveAdminReadError("nodes", c.driveAdminEvidenceLocked(), err)
	}
	c.target.AddCapability(driveops.NodesReadCapabilityName)
	return nodes, nil
}

// DriveNodeVersions lists one node's stored version history.
func (c *Client) DriveNodeVersions(ctx context.Context, query DriveNodeVersionQuery) (DriveNodeVersions, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveNodeVersions{}, fmt.Errorf("prepare Drive Admin target: %w", err)
	}
	versions, _, err := driveops.ExecuteNodeVersions(ctx, c.target, lockedExecutor{client: c}, query)
	if err != nil {
		return DriveNodeVersions{}, driveAdminReadError("node versions", c.driveAdminEvidenceLocked(), err)
	}
	c.target.AddCapability(driveops.NodeVersionsReadCapabilityName)
	return versions, nil
}

// DrivePrivileges lists accounts with their Drive privilege state.
func (c *Client) DrivePrivileges(ctx context.Context, query DrivePrivilegeQuery) (DrivePrivilegeList, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DrivePrivilegeList{}, fmt.Errorf("prepare Drive Admin target: %w", err)
	}
	list, _, err := driveops.ExecutePrivilegeList(ctx, c.target, lockedExecutor{client: c}, query)
	if err != nil {
		return DrivePrivilegeList{}, driveAdminReadError("privileges", c.driveAdminEvidenceLocked(), err)
	}
	c.target.AddCapability(driveops.PrivilegeReadCapabilityName)
	return list, nil
}

// ApplyDriveConnectionKick disconnects one client session by its session id.
// The delete call answers an empty success, so the caller verifies the
// postcondition by re-reading the connection list.
func (c *Client) ApplyDriveConnectionKick(ctx context.Context, kick DriveConnectionKick) (DriveConnectionMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveConnectionMutationResult{}, fmt.Errorf("prepare Drive Admin mutation target: %w", err)
	}
	result, _, err := driveops.ExecuteConnectionKick(ctx, c.target, lockedExecutor{client: c}, driveops.ConnectionKickInput{SessionID: kick.SessionID})
	if err != nil {
		return DriveConnectionMutationResult{}, fmt.Errorf("apply Drive connection kick: %w", err)
	}
	return result, nil
}

// DriveConnectionSummary reads the Admin Console overview connection counters.
func (c *Client) DriveConnectionSummary(ctx context.Context) (DriveConnectionSummary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveConnectionSummary{}, fmt.Errorf("prepare Drive Admin target: %w", err)
	}
	summary, _, err := driveops.ExecuteConnectionSummary(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DriveConnectionSummary{}, driveAdminReadError("connection summary", c.driveAdminEvidenceLocked(), err)
	}
	c.target.AddCapability(driveops.ConnectionSummaryCapabilityName)
	return summary, nil
}

// DriveDBUsage reads Drive's cached database usage breakdown.
func (c *Client) DriveDBUsage(ctx context.Context) (DriveDBUsage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveDBUsage{}, fmt.Errorf("prepare Drive Admin target: %w", err)
	}
	usage, _, err := driveops.ExecuteDBUsage(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DriveDBUsage{}, driveAdminReadError("database usage", c.driveAdminEvidenceLocked(), err)
	}
	c.target.AddCapability(driveops.DBUsageCapabilityName)
	return usage, nil
}

// DriveTopAccessFiles reads the Admin Console top-accessed-files ranking.
func (c *Client) DriveTopAccessFiles(ctx context.Context, query DriveTopAccessQuery) (DriveTopAccessFiles, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveTopAccessFiles{}, fmt.Errorf("prepare Drive Admin target: %w", err)
	}
	files, _, err := driveops.ExecuteDashboard(ctx, c.target, lockedExecutor{client: c}, query)
	if err != nil {
		return DriveTopAccessFiles{}, driveAdminReadError("top access files", c.driveAdminEvidenceLocked(), err)
	}
	c.target.AddCapability(driveops.DashboardCapabilityName)
	return files, nil
}

// DriveActivation reads the Drive package activation state.
func (c *Client) DriveActivation(ctx context.Context) (DriveActivation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveActivation{}, fmt.Errorf("prepare Drive Admin target: %w", err)
	}
	activation, _, err := driveops.ExecuteActivation(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DriveActivation{}, driveAdminReadError("activation", c.driveAdminEvidenceLocked(), err)
	}
	c.target.AddCapability(driveops.ActivationCapabilityName)
	return activation, nil
}

// DriveServerConfig reads the Drive server database configuration.
func (c *Client) DriveServerConfig(ctx context.Context) (DriveServerConfig, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveServerConfig{}, fmt.Errorf("prepare Drive Admin target: %w", err)
	}
	evidence := c.driveAdminEvidenceLocked()
	config, _, err := driveops.ExecuteConfigRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DriveServerConfig{}, driveAdminReadError("server config", evidence, err)
	}
	config.Package = evidence
	c.target.AddCapability(driveops.ConfigReadCapabilityName)
	return config, nil
}

// ApplyDriveServerConfigChange submits the coupled vmtouch pair, merged from the
// freshly read configuration so an unspecified half is preserved.
func (c *Client) ApplyDriveServerConfigChange(ctx context.Context, change DriveServerConfigChange) (DriveConfigMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveConfigMutationResult{}, fmt.Errorf("prepare Drive Admin mutation target: %w", err)
	}
	current, _, err := driveops.ExecuteConfigRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DriveConfigMutationResult{}, driveAdminReadError("server config", c.driveAdminEvidenceLocked(), err)
	}
	input := driveops.ConfigSetInput{VMTouchEnabled: current.VMTouchEnabled, VMTouchReserveMem: current.VMTouchReserveMem}
	if change.VMTouchEnabled != nil {
		input.VMTouchEnabled = *change.VMTouchEnabled
	}
	if change.VMTouchReserveMem != nil {
		input.VMTouchReserveMem = *change.VMTouchReserveMem
	}
	result, _, err := driveops.ExecuteConfigSet(ctx, c.target, lockedExecutor{client: c}, input)
	if err != nil {
		return DriveConfigMutationResult{}, fmt.Errorf("apply Drive server config: %w", err)
	}
	return result, nil
}

// ApplyDriveTeamFolderChange performs one validated team-folder mutation. The
// action-to-request mapping is mechanical: enable/disable set share_enable,
// and versioning fields are forwarded only when the intent carries them so
// DSM's own merge semantics apply to a versioning-only patch. Postcondition
// verification stays with the caller, which re-reads the team-folder list.
func (c *Client) ApplyDriveTeamFolderChange(ctx context.Context, change DriveTeamFolderChange) (DriveTeamFolderMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveTeamFolderMutationResult{}, fmt.Errorf("prepare Drive Admin mutation target: %w", err)
	}
	input := driveops.TeamFolderSetInput{ShareName: change.Name}
	switch change.Action {
	case driveadmin.TeamFolderActionEnable:
		enable := true
		input.Enable = &enable
		input.MaxVersions = change.MaxVersions
		input.VersionPolicy = change.VersionPolicy
		// Enable builds the view settings from scratch server-side, so the
		// retention default is sent explicitly instead of relying on struct
		// defaults inside the handler.
		retention := 0
		if change.RetentionDays != nil {
			retention = *change.RetentionDays
		}
		input.RetentionDays = &retention
	case driveadmin.TeamFolderActionDisable:
		disable := false
		input.Enable = &disable
	case driveadmin.TeamFolderActionSetVersioning:
		input.MaxVersions = change.MaxVersions
		input.VersionPolicy = change.VersionPolicy
		input.RetentionDays = change.RetentionDays
	default:
		return DriveTeamFolderMutationResult{}, fmt.Errorf("unsupported team-folder action %q", change.Action)
	}
	result, _, err := driveops.ExecuteTeamFoldersSet(ctx, c.target, lockedExecutor{client: c}, input)
	if err != nil {
		return DriveTeamFolderMutationResult{}, fmt.Errorf("apply Drive team-folder change: %w", err)
	}
	return result, nil
}

// DriveAdminCapabilities reports each Drive Admin operation's selection plus
// the installed-package evidence the selection used. A missing or too-old
// SynologyDrive package makes only this module unsupported.
func (c *Client) DriveAdminCapabilities(ctx context.Context) (DriveAdminCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, driveops.APINames()...); err != nil {
		return DriveAdminCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare Drive Admin capabilities target: %w", err)
	}
	selections, err := driveops.Select(c.target)
	if err != nil {
		return DriveAdminCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Drive Admin backends: %w", err)
	}
	c.addDriveAdminCapabilitiesLocked(selections)
	capabilities := driveAdminCapabilitiesFromSelections(selections)
	capabilities.Package = c.driveAdminEvidenceLocked()

	// Config read/set are selected separately so they do not disturb the stable
	// driveops.Select() order used by the Admin Console reads.
	configRead, err := driveops.SelectConfigRead(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return DriveAdminCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Drive config read backend: %w", err)
	}
	configSet, err := driveops.SelectConfigSet(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return DriveAdminCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Drive config set backend: %w", err)
	}
	capabilities.ConfigRead = configRead.Supported
	capabilities.ConfigSet = configSet.Supported
	if configRead.Supported {
		c.target.AddCapability(driveops.ConfigReadCapabilityName)
	}
	if configSet.Supported {
		c.target.AddCapability(driveops.ConfigSetCapabilityName)
	}
	selections = append(selections, configRead, configSet)

	// Observability reads (WI-053) are likewise selected after the stable
	// Admin Console order.
	extendedSelectors := []struct {
		selectOperation func(compatibility.Target) (compatibility.Selection, error)
		capability      string
		supported       *bool
	}{
		{driveops.SelectConnectionSummary, driveops.ConnectionSummaryCapabilityName, &capabilities.ConnectionSummaryRead},
		{driveops.SelectConnectionKick, driveops.ConnectionKickCapabilityName, &capabilities.ConnectionsKick},
		{driveops.SelectDBUsage, driveops.DBUsageCapabilityName, &capabilities.DBUsageRead},
		{driveops.SelectDashboard, driveops.DashboardCapabilityName, &capabilities.DashboardRead},
		{driveops.SelectActivation, driveops.ActivationCapabilityName, &capabilities.ActivationRead},
		{driveops.SelectPrivilegeList, driveops.PrivilegeReadCapabilityName, &capabilities.PrivilegeRead},
		{driveops.SelectNodes, driveops.NodesReadCapabilityName, &capabilities.NodesRead},
		{driveops.SelectNodeVersions, driveops.NodeVersionsReadCapabilityName, &capabilities.NodeVersionsRead},
	}
	for _, extended := range extendedSelectors {
		selection, err := extended.selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return DriveAdminCapabilities{}, CompatibilityReport{}, fmt.Errorf("select %s backend: %w", extended.capability, err)
		}
		*extended.supported = selection.Supported
		if selection.Supported {
			c.target.AddCapability(extended.capability)
		}
		selections = append(selections, selection)
	}
	return capabilities, c.target.Report(selections...), nil
}

// driveAdminCapabilitiesFromSelections maps the stable driveops.Select order:
// status, connections, team folders, log, team-folder set.
func driveAdminCapabilitiesFromSelections(selections []compatibility.Selection) DriveAdminCapabilities {
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	return DriveAdminCapabilities{
		Module:          driveadmin.ModuleName,
		StatusRead:      supported(0),
		ConnectionsRead: supported(1),
		TeamFoldersRead: supported(2),
		LogRead:         supported(3),
		TeamFoldersSet:  supported(4),
	}
}

func (c *Client) addDriveAdminCapabilitiesLocked(selections []compatibility.Selection) {
	names := []string{
		driveops.StatusCapabilityName,
		driveops.ConnectionsCapabilityName,
		driveops.TeamFoldersCapabilityName,
		driveops.LogCapabilityName,
		driveops.TeamFoldersSetCapabilityName,
	}
	for index, name := range names {
		if index < len(selections) && selections[index].Supported {
			c.target.AddCapability(name)
		}
	}
}
