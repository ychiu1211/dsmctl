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
type SecurityAdvisorScheduleChange = securityadvisor.ScheduleChange
type SecurityAdvisorMutationResult = securityadvisorops.MutationResult
type SecurityAdvisorScanResult = securityadvisorops.ScanResult

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
	scheduleWriteSelection, err := securityadvisorops.SelectConfigurationSet(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return SecurityAdvisorCapabilities{}, CompatibilityReport{}, fmt.Errorf("select security advisor schedule write backend: %w", err)
	}
	runScanSelection, err := securityadvisorops.SelectRunScan(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return SecurityAdvisorCapabilities{}, CompatibilityReport{}, fmt.Errorf("select security advisor run scan backend: %w", err)
	}
	if scheduleWriteSelection.Supported {
		c.target.AddCapability(securityadvisorops.ScheduleWriteCapabilityName)
	}
	if runScanSelection.Supported {
		c.target.AddCapability(securityadvisorops.RunScanCapabilityName)
	}
	capabilities := SecurityAdvisorCapabilities{
		Module:        securityadvisor.ModuleName,
		StatusRead:    statusSelection.Supported,
		ScheduleRead:  scheduleSelection.Supported,
		RunScan:       runScanSelection.Supported,
		ScheduleWrite: scheduleWriteSelection.Supported,
	}
	return capabilities, c.target.Report(statusSelection, scheduleSelection, scheduleWriteSelection, runScanSelection), nil
}

// ApplySecurityAdvisorScheduleChange merges the patch into a freshly read
// complete Conf state and submits it as one set, so a field the caller did not
// specify can never be silently reset. The baseline is restricted to the two
// managed groups; the write is rejected while a scan is running.
func (c *Client) ApplySecurityAdvisorScheduleChange(ctx context.Context, change SecurityAdvisorScheduleChange) (SecurityAdvisorMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, securityadvisorops.APINames()...); err != nil {
		return SecurityAdvisorMutationResult{}, fmt.Errorf("prepare security advisor mutation target: %w", err)
	}
	current, _, err := securityadvisorops.ExecuteConfiguration(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return SecurityAdvisorMutationResult{}, fmt.Errorf("refresh security advisor configuration before apply: %w", err)
	}
	desired := mergeSecurityAdvisorScheduleChange(current, change)
	result, _, err := securityadvisorops.ExecuteConfigurationSet(ctx, c.target, lockedExecutor{client: c}, desired)
	if err != nil {
		return SecurityAdvisorMutationResult{}, fmt.Errorf("apply security advisor configuration: %w", err)
	}
	return result, nil
}

// RunSecurityScan triggers a full Security Advisor scan. It changes no persisted
// configuration and is exposed as an explicit action, never invoked implicitly.
func (c *Client) RunSecurityScan(ctx context.Context) (SecurityAdvisorScanResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, securityadvisorops.APINames()...); err != nil {
		return SecurityAdvisorScanResult{}, fmt.Errorf("prepare security advisor scan target: %w", err)
	}
	result, _, err := securityadvisorops.ExecuteRunScan(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return SecurityAdvisorScanResult{}, fmt.Errorf("run security advisor scan: %w", err)
	}
	return result, nil
}

// mergeSecurityAdvisorScheduleChange applies a patch onto the observed
// configuration. A nil field preserves the current value.
func mergeSecurityAdvisorScheduleChange(current SecurityAdvisorConfiguration, change SecurityAdvisorScheduleChange) SecurityAdvisorConfiguration {
	desired := current
	if change.Baseline != nil {
		desired.Baseline = *change.Baseline
	}
	if change.ScheduleEnabled != nil {
		desired.Schedule.Enabled = *change.ScheduleEnabled
	}
	if change.Hour != nil {
		desired.Schedule.Hour = *change.Hour
	}
	if change.Minute != nil {
		desired.Schedule.Minute = *change.Minute
	}
	if change.Weekday != nil {
		desired.Schedule.Weekday = *change.Weekday
	}
	return desired
}
