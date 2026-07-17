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
type DriveAdminLog = driveadmin.Log
type DriveAdminLogQuery = driveadmin.LogQuery
type DriveAdminCapabilities = driveadmin.Capabilities

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
