package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/externalaccess"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	externalaccessops "github.com/ychiu1211/dsmctl/internal/synology/operations/externalaccess"
)

type ExternalAccessAccountState = externalaccess.AccountState
type ExternalAccessQuickConnectState = externalaccess.QuickConnectState
type ExternalAccessQuickConnectChange = externalaccess.QuickConnectChange
type ExternalAccessQuickConnectMutationResult = externalaccessops.QuickConnectMutationResult
type ExternalAccessDDNSState = externalaccess.DDNSState
type ExternalAccessPortForwardState = externalaccess.PortForwardState
type ExternalAccessCapabilities = externalaccess.Capabilities

// ExternalAccessAccountState reads the Synology Account binding without
// coupling to QuickConnect or DDNS. Only the non-secret account identity is
// returned; the account token is never decoded.
func (c *Client) ExternalAccessAccountState(ctx context.Context) (ExternalAccessAccountState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, externalaccessops.APINames()...); err != nil {
		return ExternalAccessAccountState{}, fmt.Errorf("prepare External Access account target: %w", err)
	}
	state, _, err := externalaccessops.ReadAccount(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ExternalAccessAccountState{}, fmt.Errorf("get Synology Account status: %w", err)
	}
	c.target.AddCapability(externalaccessops.AccountReadCapabilityName)
	return state, nil
}

// ExternalAccessQuickConnectState reads QuickConnect configuration, relay
// setting, live status, and per-service exposure.
func (c *Client) ExternalAccessQuickConnectState(ctx context.Context) (ExternalAccessQuickConnectState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, externalaccessops.APINames()...); err != nil {
		return ExternalAccessQuickConnectState{}, fmt.Errorf("prepare External Access QuickConnect target: %w", err)
	}
	state, _, err := externalaccessops.ReadQuickConnect(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ExternalAccessQuickConnectState{}, fmt.Errorf("get QuickConnect configuration: %w", err)
	}
	c.target.AddCapability(externalaccessops.QuickConnectReadCapabilityName)
	return state, nil
}

// ExternalAccessDDNSState reads the configured DDNS records and detected WAN
// addresses.
func (c *Client) ExternalAccessDDNSState(ctx context.Context) (ExternalAccessDDNSState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, externalaccessops.APINames()...); err != nil {
		return ExternalAccessDDNSState{}, fmt.Errorf("prepare External Access DDNS target: %w", err)
	}
	state, _, err := externalaccessops.ReadDDNS(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ExternalAccessDDNSState{}, fmt.Errorf("get DDNS configuration: %w", err)
	}
	c.target.AddCapability(externalaccessops.DDNSReadCapabilityName)
	return state, nil
}

// ExternalAccessPortForwardState reads the configured port-forwarding rules and
// paired router configuration (Control Panel → External Access → Router
// Configuration).
func (c *Client) ExternalAccessPortForwardState(ctx context.Context) (ExternalAccessPortForwardState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, externalaccessops.APINames()...); err != nil {
		return ExternalAccessPortForwardState{}, fmt.Errorf("prepare External Access port-forwarding target: %w", err)
	}
	state, _, err := externalaccessops.ReadPortForward(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ExternalAccessPortForwardState{}, fmt.Errorf("get port-forwarding configuration: %w", err)
	}
	c.target.AddCapability(externalaccessops.PortForwardReadCapabilityName)
	return state, nil
}

// ApplyExternalAccessQuickConnectChange writes the guarded QuickConnect relay
// toggle. Only the relay flag is writable; the caller (application plan/apply)
// has already confirmed the change differs from the current state.
func (c *Client) ApplyExternalAccessQuickConnectChange(ctx context.Context, change ExternalAccessQuickConnectChange) (ExternalAccessQuickConnectMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if change.RelayEnabled == nil {
		return ExternalAccessQuickConnectMutationResult{}, fmt.Errorf("QuickConnect change has no fields")
	}
	if err := c.prepareCompatibilityTargetLocked(ctx, externalaccessops.APINames()...); err != nil {
		return ExternalAccessQuickConnectMutationResult{}, fmt.Errorf("prepare External Access QuickConnect mutation target: %w", err)
	}
	result, _, err := externalaccessops.ExecuteQuickConnectRelaySet(ctx, c.target, lockedExecutor{client: c}, *change.RelayEnabled)
	if err != nil {
		return ExternalAccessQuickConnectMutationResult{}, fmt.Errorf("apply QuickConnect relay setting: %w", err)
	}
	return result, nil
}

// ExternalAccessCapabilities reports which of the read areas this NAS exposes,
// each selected independently so one missing API does not disable the others,
// plus whether the guarded QuickConnect relay write is available.
func (c *Client) ExternalAccessCapabilities(ctx context.Context) (ExternalAccessCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, externalaccessops.APINames()...); err != nil {
		return ExternalAccessCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare External Access capabilities target: %w", err)
	}
	selectors := []struct {
		selectArea func(compatibility.Target) (compatibility.Selection, error)
		capability string
	}{
		{externalaccessops.SelectAccount, externalaccessops.AccountReadCapabilityName},
		{externalaccessops.SelectQuickConnect, externalaccessops.QuickConnectReadCapabilityName},
		{externalaccessops.SelectDDNS, externalaccessops.DDNSReadCapabilityName},
		{externalaccessops.SelectPortForward, externalaccessops.PortForwardReadCapabilityName},
		{externalaccessops.SelectQuickConnectRelaySet, externalaccessops.QuickConnectRelaySetCapabilityName},
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selector := range selectors {
		selection, err := selector.selectArea(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return ExternalAccessCapabilities{}, CompatibilityReport{}, fmt.Errorf("select External Access backend: %w", err)
		}
		if selection.Supported {
			c.target.AddCapability(selector.capability)
		}
		selections = append(selections, selection)
	}
	capabilities := ExternalAccessCapabilities{
		Account:         selections[0].Supported,
		QuickConnect:    selections[1].Supported,
		DDNS:            selections[2].Supported,
		PortForward:     selections[3].Supported,
		QuickConnectSet: selections[4].Supported,
	}
	return capabilities, c.target.Report(selections...), nil
}
