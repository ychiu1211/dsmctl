package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/dsmupdate"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	dsmupdateops "github.com/ychiu1211/dsmctl/internal/synology/operations/dsmupdate"
)

type DSMUpdateStatus = dsmupdate.UpdateStatus
type DSMUpdateAvailable = dsmupdate.AvailableUpdate
type DSMUpdatePolicy = dsmupdate.AutoUpdatePolicy
type DSMUpdateConfigBackup = dsmupdate.ConfigBackup
type DSMUpdateCapabilities = dsmupdate.Capabilities

// DSMUpdateStatus reads the local DSM update state (installed version, whether
// an upgrade is allowed, and any in-progress download/install state). It is
// side-effect-free and does not contact the update server.
func (c *Client) DSMUpdateStatus(ctx context.Context) (DSMUpdateStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, dsmupdateops.APINames()...); err != nil {
		return DSMUpdateStatus{}, fmt.Errorf("prepare DSM update status target: %w", err)
	}
	state, _, err := dsmupdateops.ReadStatus(ctx, c.target, lockedExecutor{client: c}, c.target.DSM.String())
	if err != nil {
		return DSMUpdateStatus{}, fmt.Errorf("get DSM update status: %w", err)
	}
	c.target.AddCapability(dsmupdateops.StatusReadCapabilityName)
	return state, nil
}

// DSMUpdateAvailable checks the update server for an offered update. The check
// is a network egress to Synology's update server; an unreachable server is
// reported as Checked=false (availability unknown) rather than erroring the
// module, so a blocked update-server reach never breaks the surface.
func (c *Client) DSMUpdateAvailable(ctx context.Context) (DSMUpdateAvailable, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, dsmupdateops.APINames()...); err != nil {
		return DSMUpdateAvailable{}, fmt.Errorf("prepare DSM update availability target: %w", err)
	}
	state, _, err := dsmupdateops.ReadAvailable(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		// The update-server check is a network egress; a reachability failure
		// (or any DSM-side check failure) must not error the module. Report the
		// availability as unknown. The decoder's malformed-response rejection is
		// exercised directly in unit tests.
		return DSMUpdateAvailable{Checked: false}, nil
	}
	c.target.AddCapability(dsmupdateops.AvailableReadCapabilityName)
	return state, nil
}

// DSMUpdatePolicy reads the DSM auto-update policy (whether automatic update is
// enabled, which updates are auto-installed, and the maintenance window).
func (c *Client) DSMUpdatePolicy(ctx context.Context) (DSMUpdatePolicy, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, dsmupdateops.APINames()...); err != nil {
		return DSMUpdatePolicy{}, fmt.Errorf("prepare DSM auto-update policy target: %w", err)
	}
	state, _, err := dsmupdateops.ReadPolicy(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DSMUpdatePolicy{}, fmt.Errorf("get DSM auto-update policy: %w", err)
	}
	c.target.AddCapability(dsmupdateops.PolicyReadCapabilityName)
	return state, nil
}

// DSMUpdateConfigBackup reads the configuration-backup status: whether the
// scheduled backup to the Synology account is enabled, its destination account
// and encryption mode, the last-backup result, and the stored backup history.
// The destination account password is never decoded.
func (c *Client) DSMUpdateConfigBackup(ctx context.Context) (DSMUpdateConfigBackup, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, dsmupdateops.APINames()...); err != nil {
		return DSMUpdateConfigBackup{}, fmt.Errorf("prepare configuration backup target: %w", err)
	}
	state, _, err := dsmupdateops.ReadConfigBackup(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DSMUpdateConfigBackup{}, fmt.Errorf("get configuration backup status: %w", err)
	}
	c.target.AddCapability(dsmupdateops.ConfigBackupReadCapabilityName)
	return state, nil
}

// DSMUpdateCapabilities reports which Update & Restore read areas this NAS
// exposes, each selected independently so one missing API (an absent
// config-backup family, a blocked update server) does not disable the others.
func (c *Client) DSMUpdateCapabilities(ctx context.Context) (DSMUpdateCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, dsmupdateops.APINames()...); err != nil {
		return DSMUpdateCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare DSM update capabilities target: %w", err)
	}
	selectors := []struct {
		selectArea func(compatibility.Target) (compatibility.Selection, error)
		capability string
	}{
		{dsmupdateops.SelectStatus, dsmupdateops.StatusReadCapabilityName},
		{dsmupdateops.SelectAvailable, dsmupdateops.AvailableReadCapabilityName},
		{dsmupdateops.SelectPolicy, dsmupdateops.PolicyReadCapabilityName},
		{dsmupdateops.SelectConfigBackup, dsmupdateops.ConfigBackupReadCapabilityName},
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selector := range selectors {
		selection, err := selector.selectArea(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return DSMUpdateCapabilities{}, CompatibilityReport{}, fmt.Errorf("select DSM update backend: %w", err)
		}
		if selection.Supported {
			c.target.AddCapability(selector.capability)
		}
		selections = append(selections, selection)
	}
	capabilities := DSMUpdateCapabilities{
		Status:       selections[0].Supported,
		Available:    selections[1].Supported,
		Policy:       selections[2].Supported,
		ConfigBackup: selections[3].Supported,
	}
	return capabilities, c.target.Report(selections...), nil
}
