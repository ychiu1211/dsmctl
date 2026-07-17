package synology

import (
	"context"
	"errors"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/resmon"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/resmonsetting"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/resmonsettingmutation"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/utilizationread"
)

type ResourceUtilization = resmon.Utilization
type ResourceHistory = resmon.History
type ResourceRecordingSetting = resmon.RecordingSetting
type ResourceMonitorCapabilities = resmon.Capabilities
type ResourceRecordingChange = resmon.RecordingChange
type ResourceRecordingMutationResult = resmonsettingmutation.MutationResult

// errNotEnableHistory is DSM's WEBAPI_CORE_SYSTEM_ERR_NOT_ENABLE_HISTORY. DSM
// returns it from SYNO.Core.System.Utilization.get with type=history when
// resource history recording has never been enabled.
const errNotEnableHistory = 1008

// resourceMonitorAPINames is every DSM API the Resource Monitor module touches.
func resourceMonitorAPINames() []string {
	names := utilizationread.APINames()
	names = append(names, resmonsetting.APINames()...)
	names = append(names, resmonsettingmutation.APINames()...)
	return names
}

// ResourceMonitorState reads a normalized current-utilization snapshot.
func (c *Client) ResourceMonitorState(ctx context.Context) (ResourceUtilization, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, resourceMonitorAPINames()...); err != nil {
		return ResourceUtilization{}, fmt.Errorf("prepare resource monitor target: %w", err)
	}
	state, selection, err := utilizationread.ExecuteCurrent(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ResourceUtilization{}, fmt.Errorf("read resource utilization: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(utilizationread.CapabilityName)
	}
	return state, nil
}

// ResourceMonitorHistory reads recorded utilization history. When DSM reports
// that recording is disabled, it returns resmon.ErrHistoryRecordingDisabled so
// callers can prompt the user to enable recording first. Dimensions, when
// provided, filter the returned series client-side.
func (c *Client) ResourceMonitorHistory(ctx context.Context, query resmon.HistoryQuery) (ResourceHistory, error) {
	period, err := resmon.NormalizePeriod(query.Period)
	if err != nil {
		return ResourceHistory{}, err
	}
	dimensions, err := normalizeDimensions(query.Dimensions)
	if err != nil {
		return ResourceHistory{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, resourceMonitorAPINames()...); err != nil {
		return ResourceHistory{}, fmt.Errorf("prepare resource monitor target: %w", err)
	}
	resources := historyResources(dimensions)
	interfaces, err := c.historyInterfacesLocked(ctx, resources)
	if err != nil {
		return ResourceHistory{}, err
	}
	history, selection, err := utilizationread.ExecuteHistory(ctx, c.target, lockedExecutor{client: c}, utilizationread.HistoryInput{Period: period, Resources: resources, Interfaces: interfaces})
	if err != nil {
		if isHistoryRecordingDisabled(err) {
			return ResourceHistory{}, resmon.ErrHistoryRecordingDisabled
		}
		return ResourceHistory{}, fmt.Errorf("read resource history: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(utilizationread.CapabilityName)
	}
	if len(dimensions) > 0 {
		history.Series = filterSeriesByDimension(history.Series, dimensions)
	}
	return history, nil
}

// ResourceMonitorSetting reads the history-recording setting.
func (c *Client) ResourceMonitorSetting(ctx context.Context) (ResourceRecordingSetting, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, resourceMonitorAPINames()...); err != nil {
		return ResourceRecordingSetting{}, fmt.Errorf("prepare resource monitor target: %w", err)
	}
	settings, selection, err := resmonsetting.Execute(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ResourceRecordingSetting{}, fmt.Errorf("read resource recording setting: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(resmonsetting.CapabilityName)
	}
	return settings.Recording, nil
}

// ResourceMonitorCapabilities reports which Resource Monitor operations the
// target supports and the selected backends.
func (c *Client) ResourceMonitorCapabilities(ctx context.Context) (ResourceMonitorCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, resourceMonitorAPINames()...); err != nil {
		return ResourceMonitorCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare resource monitor capabilities target: %w", err)
	}
	currentSelection, err := utilizationread.SelectCurrent(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return ResourceMonitorCapabilities{}, CompatibilityReport{}, fmt.Errorf("select utilization backend: %w", err)
	}
	historySelection, err := utilizationread.SelectHistory(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return ResourceMonitorCapabilities{}, CompatibilityReport{}, fmt.Errorf("select utilization history backend: %w", err)
	}
	settingSelection, err := resmonsetting.Select(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return ResourceMonitorCapabilities{}, CompatibilityReport{}, fmt.Errorf("select recording setting backend: %w", err)
	}
	setSelection, err := resmonsettingmutation.SelectSet(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return ResourceMonitorCapabilities{}, CompatibilityReport{}, fmt.Errorf("select recording set backend: %w", err)
	}
	if currentSelection.Supported {
		c.target.AddCapability(utilizationread.CapabilityName)
	}
	if settingSelection.Supported {
		c.target.AddCapability(resmonsetting.CapabilityName)
	}
	if setSelection.Supported {
		c.target.AddCapability(resmonsettingmutation.SetCapabilityName)
	}
	capabilities := ResourceMonitorCapabilities{
		Read:          currentSelection.Supported,
		RecordingRead: settingSelection.Supported,
		RecordingSet:  setSelection.Supported,
	}
	return capabilities, c.target.Report(currentSelection, historySelection, settingSelection, setSelection), nil
}

// ApplyResourceRecordingChange toggles history recording. It reads the current
// setting, overlays only the recording field, and submits the complete merged
// object so DSM never resets a co-located setting dsmctl does not own.
func (c *Client) ApplyResourceRecordingChange(ctx context.Context, change ResourceRecordingChange) (ResourceRecordingMutationResult, error) {
	if change.Enable == nil {
		return ResourceRecordingMutationResult{}, fmt.Errorf("recording change requires a desired enable value")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, resourceMonitorAPINames()...); err != nil {
		return ResourceRecordingMutationResult{}, fmt.Errorf("prepare resource monitor target: %w", err)
	}
	settings, _, err := resmonsetting.Execute(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ResourceRecordingMutationResult{}, fmt.Errorf("read resource recording setting: %w", err)
	}
	merged := make(map[string]any, len(settings.Raw))
	for key, value := range settings.Raw {
		merged[key] = value
	}
	merged[resmonsettingmutation.RecordingField] = *change.Enable
	result, selection, err := resmonsettingmutation.ExecuteSet(ctx, c.target, lockedExecutor{client: c}, merged)
	if err != nil {
		return ResourceRecordingMutationResult{}, fmt.Errorf("apply resource recording change: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(resmonsettingmutation.SetCapabilityName)
	}
	return result, nil
}

func isHistoryRecordingDisabled(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == errNotEnableHistory
}

func normalizeDimensions(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		dimension, err := resmon.NormalizeDimension(value)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[dimension]; duplicate {
			continue
		}
		seen[dimension] = struct{}{}
		normalized = append(normalized, dimension)
	}
	return normalized, nil
}

// historyInterfacesLocked builds the per-device interface lists DSM requires
// for the disk, network, and space history groups (error 1057 without them).
// It reads the current snapshot once to enumerate the live device ids. cpu and
// memory need no interfaces, so an all-cpu/memory request skips the extra read.
func (c *Client) historyInterfacesLocked(ctx context.Context, resources []string) (map[string][]string, error) {
	needed := map[string]bool{}
	for _, resource := range resources {
		switch resource {
		case "disk", "network", "space":
			needed[resource] = true
		}
	}
	if len(needed) == 0 {
		return nil, nil
	}
	current, _, err := utilizationread.ExecuteCurrent(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return nil, fmt.Errorf("read devices for history interfaces: %w", err)
	}
	interfaces := make(map[string][]string, len(needed))
	if needed["disk"] {
		devices := make([]string, 0, len(current.Disk.Disks))
		for _, disk := range current.Disk.Disks {
			if disk.Device != "" {
				devices = append(devices, disk.Device)
			}
		}
		interfaces["disk"] = devices
	}
	if needed["network"] {
		devices := make([]string, 0, len(current.Network))
		for _, iface := range current.Network {
			// DSM reports a synthetic "total" aggregate that is not a real
			// interface and is rejected as a history interface id.
			if iface.Device != "" && iface.Device != "total" {
				devices = append(devices, iface.Device)
			}
		}
		interfaces["network"] = devices
	}
	if needed["space"] {
		devices := make([]string, 0, len(current.Volumes))
		for _, volume := range current.Volumes {
			if volume.Device != "" {
				devices = append(devices, volume.Device)
			}
		}
		interfaces["space"] = devices
	}
	return interfaces, nil
}

// historyResources maps canonical dimensions to DSM's history resource-group
// names. DSM calls the per-volume group "space"; the rest match. An empty
// selection defaults to every supported group.
func historyResources(dimensions []string) []string {
	if len(dimensions) == 0 {
		dimensions = []string{resmon.DimensionCPU, resmon.DimensionMemory, resmon.DimensionNetwork, resmon.DimensionDisk, resmon.DimensionVolume}
	}
	resources := make([]string, 0, len(dimensions))
	for _, dimension := range dimensions {
		if dimension == resmon.DimensionVolume {
			resources = append(resources, "space")
			continue
		}
		resources = append(resources, dimension)
	}
	return resources
}

func filterSeriesByDimension(series []resmon.HistorySeries, dimensions []string) []resmon.HistorySeries {
	keep := make(map[string]struct{}, len(dimensions))
	for _, dimension := range dimensions {
		keep[dimension] = struct{}{}
	}
	filtered := make([]resmon.HistorySeries, 0, len(series))
	for _, entry := range series {
		if _, ok := keep[entry.Dimension]; ok {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
