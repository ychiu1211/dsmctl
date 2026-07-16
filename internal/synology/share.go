package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/share"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/identityinventory"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/shareinventory"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/sharemutation"
)

type ShareState = share.State
type ShareCapabilities = share.Capabilities
type ShareChangeRequest = share.ChangeRequest
type ShareMutationResult = sharemutation.Result

func (c *Client) ShareState(ctx context.Context, includePermissions bool) (ShareState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	apiNames := append([]string(nil), shareinventory.APINames()...)
	if includePermissions {
		apiNames = append(apiNames, identityinventory.APINames()...)
	}
	if err := c.prepareCompatibilityTargetLocked(ctx, apiNames...); err != nil {
		return ShareState{}, fmt.Errorf("prepare shared-folder inventory target: %w", err)
	}

	input := shareinventory.Input{IncludePermissions: includePermissions}
	if includePermissions {
		identities, _, err := c.identityStateLocked(ctx)
		if err != nil {
			return ShareState{}, fmt.Errorf("load permission principals: %w", err)
		}
		input.Users = identities.Users
		input.Groups = identities.Groups
	}
	state, _, err := shareinventory.Execute(ctx, c.target, lockedExecutor{client: c}, input)
	if err != nil {
		return ShareState{}, fmt.Errorf("get shared-folder inventory: %w", err)
	}
	c.target.AddCapability(shareinventory.InventoryCapabilityName)
	if includePermissions {
		c.target.AddCapability(shareinventory.PermissionCapabilityName)
	}
	return state, nil
}

func (c *Client) ShareCapabilities(ctx context.Context) (ShareCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	apiNames := append(shareinventory.APINames(), identityinventory.APINames()...)
	apiNames = append(apiNames, sharemutation.APINames()...)
	if err := c.prepareCompatibilityTargetLocked(ctx, apiNames...); err != nil {
		return ShareCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare shared-folder capabilities target: %w", err)
	}
	shareSelections, err := shareinventory.Select(c.target)
	if err != nil {
		return ShareCapabilities{}, CompatibilityReport{}, fmt.Errorf("select shared-folder backends: %w", err)
	}
	identitySelections, err := identityinventory.Select(c.target)
	if err != nil {
		return ShareCapabilities{}, CompatibilityReport{}, fmt.Errorf("select permission-principal backends: %w", err)
	}
	inventorySupported := shareinventory.InventorySupported(shareSelections)
	permissionSupported := shareinventory.PermissionsSupported(shareSelections) && identityinventory.Supported(identitySelections)
	if inventorySupported {
		c.target.AddCapability(shareinventory.InventoryCapabilityName)
	}
	if permissionSupported {
		c.target.AddCapability(shareinventory.PermissionCapabilityName)
	}
	mutationSelections, err := sharemutation.Select(c.target)
	if err != nil {
		return ShareCapabilities{}, CompatibilityReport{}, fmt.Errorf("select shared-folder mutation backends: %w", err)
	}
	shareMutations := len(mutationSelections) > 0 && mutationSelections[0].Supported
	permissionMutations := len(mutationSelections) > 1 && mutationSelections[1].Supported
	if shareMutations {
		c.target.AddCapability(sharemutation.ShareCapabilityName)
	}
	if permissionMutations {
		c.target.AddCapability(sharemutation.PermissionCapabilityName)
	}
	capabilities := ShareCapabilities{
		InventoryRead:   inventorySupported,
		PermissionRead:  permissionSupported,
		ShareCreate:     shareMutations,
		ShareUpdate:     shareMutations,
		ShareDelete:     shareMutations,
		PermissionWrite: permissionMutations,
		Mutations:       shareMutations || permissionMutations,
	}
	c.updateDerivedCapabilitiesLocked()
	selections := append(shareSelections, identitySelections...)
	selections = append(selections, mutationSelections...)
	return capabilities, c.target.Report(selections...), nil
}

func (c *Client) ApplyShareChange(ctx context.Context, request share.ChangeRequest) (ShareMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, sharemutation.APINames()...); err != nil {
		return ShareMutationResult{}, fmt.Errorf("prepare shared-folder mutation target: %w", err)
	}
	switch request.Resource {
	case share.ResourceShare:
		if request.Share == nil {
			return ShareMutationResult{}, fmt.Errorf("shared-folder change is required")
		}
		result, _, err := sharemutation.ExecuteShare(ctx, c.target, lockedExecutor{client: c}, sharemutation.ShareInput{Action: request.Action, Change: *request.Share})
		if err != nil {
			return ShareMutationResult{}, fmt.Errorf("apply shared-folder change: %w", err)
		}
		return result, nil
	case share.ResourcePermission:
		if request.Permission == nil {
			return ShareMutationResult{}, fmt.Errorf("permission change is required")
		}
		result, _, err := sharemutation.ExecutePermission(ctx, c.target, lockedExecutor{client: c}, sharemutation.PermissionInput{Change: *request.Permission})
		if err != nil {
			return ShareMutationResult{}, fmt.Errorf("apply shared-folder permission change: %w", err)
		}
		return result, nil
	default:
		return ShareMutationResult{}, fmt.Errorf("unsupported share resource %q", request.Resource)
	}
}
