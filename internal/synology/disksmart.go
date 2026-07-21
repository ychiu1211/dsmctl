package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/disksmart"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	disksmartops "github.com/ychiu1211/dsmctl/internal/synology/operations/disksmart"
)

type DiskHealthState = disksmart.HealthState
type DiskSMARTState = disksmart.SMARTState
type DiskSMARTCapabilities = disksmart.Capabilities

// DiskHealth reads every installed disk's health, SSD remaining-life,
// spare-block/bad-sector detail, and coarse self-test state, plus the NAS-wide
// disk-health warning thresholds when that configuration API is available. It
// complements the storage inventory, which carries no per-disk lifespan or
// self-test detail.
func (c *Client) DiskHealth(ctx context.Context) (DiskHealthState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, disksmartops.APINames()...); err != nil {
		return DiskHealthState{}, fmt.Errorf("prepare disk health target: %w", err)
	}
	state, selection, err := disksmartops.ReadHealth(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DiskHealthState{}, fmt.Errorf("read disk health: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(disksmartops.HealthReadCapabilityName)
	}
	if state.Thresholds != nil {
		c.target.AddCapability(disksmartops.ThresholdsReadCapabilityName)
	}
	return state, nil
}

// DiskSMARTAttributes reads every disk's S.M.A.R.T. attribute table plus a
// per-disk health/test summary and self-test status. A disk that exposes no
// attribute table (many enterprise SSDs, NVMe/SATADOM/M.2, and USB devices) is
// reported with no_smart_data set rather than failing the whole read.
func (c *Client) DiskSMARTAttributes(ctx context.Context) (DiskSMARTState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, disksmartops.APINames()...); err != nil {
		return DiskSMARTState{}, fmt.Errorf("prepare disk SMART target: %w", err)
	}
	state, selection, err := disksmartops.ReadSMART(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DiskSMARTState{}, fmt.Errorf("read disk SMART attributes: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(disksmartops.AttributesReadCapabilityName)
	}
	return state, nil
}

// DiskSMARTCapabilities reports which disk-SMART read areas this NAS exposes,
// each selected independently so one missing API family does not disable the
// others.
func (c *Client) DiskSMARTCapabilities(ctx context.Context) (DiskSMARTCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, disksmartops.APINames()...); err != nil {
		return DiskSMARTCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare disk SMART capabilities target: %w", err)
	}
	selectors := []struct {
		selectArea func(compatibility.Target) (compatibility.Selection, error)
		capability string
	}{
		{disksmartops.SelectHealth, disksmartops.HealthReadCapabilityName},
		{disksmartops.SelectAttributes, disksmartops.AttributesReadCapabilityName},
		{disksmartops.SelectThresholds, disksmartops.ThresholdsReadCapabilityName},
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selector := range selectors {
		selection, err := selector.selectArea(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return DiskSMARTCapabilities{}, CompatibilityReport{}, fmt.Errorf("select disk SMART backend: %w", err)
		}
		if selection.Supported {
			c.target.AddCapability(selector.capability)
		}
		selections = append(selections, selection)
	}
	capabilities := DiskSMARTCapabilities{
		Health:     selections[0].Supported,
		Attributes: selections[1].Supported,
		Thresholds: selections[2].Supported,
	}
	return capabilities, c.target.Report(selections...), nil
}
