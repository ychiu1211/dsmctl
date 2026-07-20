package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/securityadvisor"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	securityadvisorops "github.com/ychiu1211/dsmctl/internal/synology/operations/securityadvisor"
)

type SecurityAdvisorStatus = securityadvisor.Status
type SecurityAdvisorConfiguration = securityadvisor.Configuration
type SecurityAdvisorCapabilities = securityadvisor.Capabilities

// SecurityAdvisorStatus reads the last-scan status and per-category findings.
// Security Advisor is DSM core, so the plain compatibility target (not the
// package-scoped one) is used.
func (c *Client) SecurityAdvisorStatus(ctx context.Context) (SecurityAdvisorStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, securityadvisorops.APINames()...); err != nil {
		return SecurityAdvisorStatus{}, fmt.Errorf("prepare security advisor target: %w", err)
	}
	status, selection, err := securityadvisorops.ExecuteStatus(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return SecurityAdvisorStatus{}, fmt.Errorf("read security advisor status: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(securityadvisorops.StatusReadCapabilityName)
	}
	return status, nil
}

// SecurityAdvisorConfiguration reads the scan schedule and security baseline.
func (c *Client) SecurityAdvisorConfiguration(ctx context.Context) (SecurityAdvisorConfiguration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, securityadvisorops.APINames()...); err != nil {
		return SecurityAdvisorConfiguration{}, fmt.Errorf("prepare security advisor target: %w", err)
	}
	configuration, selection, err := securityadvisorops.ExecuteConfiguration(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return SecurityAdvisorConfiguration{}, fmt.Errorf("read security advisor configuration: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(securityadvisorops.ScheduleReadCapabilityName)
	}
	return configuration, nil
}

// SecurityAdvisorCapabilities reports the Security Advisor operations dsmctl
// exposes for the selected NAS, plus the discovered backends. Each API is its
// own compatibility boundary, so a NAS advertising Status but not Conf reports
// one read available and the other (not supported), never erroring the module.
func (c *Client) SecurityAdvisorCapabilities(ctx context.Context) (SecurityAdvisorCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, securityadvisorops.APINames()...); err != nil {
		return SecurityAdvisorCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare security advisor capabilities target: %w", err)
	}
	statusSelection, err := securityadvisorops.SelectStatus(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return SecurityAdvisorCapabilities{}, CompatibilityReport{}, fmt.Errorf("select security advisor status backend: %w", err)
	}
	scheduleSelection, err := securityadvisorops.SelectConfiguration(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return SecurityAdvisorCapabilities{}, CompatibilityReport{}, fmt.Errorf("select security advisor schedule backend: %w", err)
	}
	if statusSelection.Supported {
		c.target.AddCapability(securityadvisorops.StatusReadCapabilityName)
	}
	if scheduleSelection.Supported {
		c.target.AddCapability(securityadvisorops.ScheduleReadCapabilityName)
	}
	capabilities := SecurityAdvisorCapabilities{
		Module:       securityadvisor.ModuleName,
		StatusRead:   statusSelection.Supported,
		ScheduleRead: scheduleSelection.Supported,
		// The run-scan action and the schedule/baseline write are deferred
		// slices; their availability is reported from the advertised APIs, but
		// this module never executes them.
		RunScan:       securityadvisorops.SupportsRunScan(c.target),
		ScheduleWrite: securityadvisorops.SupportsScheduleWrite(c.target),
	}
	return capabilities, c.target.Report(statusSelection, scheduleSelection), nil
}
