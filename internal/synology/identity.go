package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/identityinventory"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/identitymutation"
)

type IdentityState = identity.State
type IdentityCapabilities = identity.Capabilities
type IdentityChangeRequest = identity.ChangeRequest
type IdentityMutationResult = identitymutation.Result

func (c *Client) IdentityState(ctx context.Context) (IdentityState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	apiNames := append(identityinventory.APINames(), identitymutation.APINames()...)
	if err := c.prepareCompatibilityTargetLocked(ctx, apiNames...); err != nil {
		return IdentityState{}, fmt.Errorf("prepare account inventory target: %w", err)
	}
	state, _, err := c.identityStateLocked(ctx)
	if err != nil {
		return IdentityState{}, err
	}
	return state, nil
}

func (c *Client) identityStateLocked(ctx context.Context) (IdentityState, []compatibility.Selection, error) {
	state, selections, err := identityinventory.Execute(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return IdentityState{}, selections, fmt.Errorf("get account inventory: %w", err)
	}
	c.target.AddCapability(identityinventory.CapabilityName)
	return state, selections, nil
}

func (c *Client) IdentityCapabilities(ctx context.Context) (IdentityCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, identityinventory.APINames()...); err != nil {
		return IdentityCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare account capabilities target: %w", err)
	}
	selections, err := identityinventory.Select(c.target)
	if err != nil {
		return IdentityCapabilities{}, CompatibilityReport{}, fmt.Errorf("select account inventory backends: %w", err)
	}
	supported := identityinventory.Supported(selections)
	if supported {
		c.target.AddCapability(identityinventory.CapabilityName)
	}
	mutationSelections, err := identitymutation.Select(c.target)
	if err != nil {
		return IdentityCapabilities{}, CompatibilityReport{}, fmt.Errorf("select account mutation backends: %w", err)
	}
	userMutations := len(mutationSelections) > 0 && mutationSelections[0].Supported
	groupMutations := len(mutationSelections) > 1 && mutationSelections[1].Supported
	if userMutations {
		c.target.AddCapability(identitymutation.UserCapabilityName)
	}
	if groupMutations {
		c.target.AddCapability(identitymutation.GroupCapabilityName)
	}
	capabilities := IdentityCapabilities{
		InventoryRead: supported,
		UserCreate:    userMutations,
		UserUpdate:    userMutations,
		UserDelete:    userMutations,
		GroupCreate:   groupMutations,
		GroupUpdate:   groupMutations,
		GroupDelete:   groupMutations,
		Mutations:     userMutations || groupMutations,
	}
	c.updateDerivedCapabilitiesLocked()
	selections = append(selections, mutationSelections...)
	return capabilities, c.target.Report(selections...), nil
}

func (c *Client) ApplyIdentityChange(ctx context.Context, request identity.ChangeRequest, password string) (IdentityMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, identitymutation.APINames()...); err != nil {
		return IdentityMutationResult{}, fmt.Errorf("prepare account mutation target: %w", err)
	}
	switch request.Resource {
	case identity.ResourceUser:
		if request.User == nil {
			return IdentityMutationResult{}, fmt.Errorf("user change is required")
		}
		result, _, err := identitymutation.ExecuteUser(ctx, c.target, lockedExecutor{client: c}, identitymutation.UserInput{Action: request.Action, Change: *request.User, Password: password})
		if err != nil {
			return IdentityMutationResult{}, fmt.Errorf("apply user change: %w", err)
		}
		return result, nil
	case identity.ResourceGroup:
		if request.Group == nil {
			return IdentityMutationResult{}, fmt.Errorf("group change is required")
		}
		result, _, err := identitymutation.ExecuteGroup(ctx, c.target, lockedExecutor{client: c}, identitymutation.GroupInput{Action: request.Action, Change: *request.Group})
		if err != nil {
			return IdentityMutationResult{}, fmt.Errorf("apply group change: %w", err)
		}
		return result, nil
	default:
		return IdentityMutationResult{}, fmt.Errorf("unsupported identity resource %q", request.Resource)
	}
}
