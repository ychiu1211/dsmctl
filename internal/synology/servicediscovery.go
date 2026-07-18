package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/servicediscovery"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	servicediscoveryop "github.com/ychiu1211/dsmctl/internal/synology/operations/servicediscovery"
)

type ServiceDiscoveryState = servicediscovery.State
type ServiceDiscoveryCapabilities = servicediscovery.Capabilities
type ServiceDiscoveryChange = servicediscovery.Change
type ServiceDiscoveryMutationResult = servicediscoveryop.MutationResult

// ServiceDiscoveryState reads Time Machine advertising and, when its backend is
// available, WS-Discovery.
func (c *Client) ServiceDiscoveryState(ctx context.Context) (ServiceDiscoveryState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, servicediscoveryop.APINames()...); err != nil {
		return ServiceDiscoveryState{}, fmt.Errorf("prepare service discovery target: %w", err)
	}
	timeMachine, _, err := servicediscoveryop.ExecuteTimeMachineRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ServiceDiscoveryState{}, fmt.Errorf("get Time Machine advertising: %w", err)
	}
	c.target.AddCapability(servicediscoveryop.TimeMachineReadCapabilityName)
	state := ServiceDiscoveryState{SMBTimeMachine: timeMachine.SMB, AFPTimeMachine: timeMachine.AFP}

	wsSelection, selectionErr := servicediscoveryop.SelectWSDiscoveryRead(c.target)
	if selectionErr != nil && !compatibility.IsUnsupported(selectionErr) {
		return ServiceDiscoveryState{}, fmt.Errorf("select WS-Discovery read backend: %w", selectionErr)
	}
	if wsSelection.Supported {
		enabled, _, wsErr := servicediscoveryop.ExecuteWSDiscoveryRead(ctx, c.target, lockedExecutor{client: c})
		if wsErr != nil {
			return ServiceDiscoveryState{}, fmt.Errorf("get WS-Discovery: %w", wsErr)
		}
		state.WSDiscovery = &enabled
		c.target.AddCapability(servicediscoveryop.WSDiscoveryReadCapabilityName)
	}
	return state, nil
}

// ServiceDiscoveryCapabilities reports the independently selected Time Machine
// and WS-Discovery backends.
func (c *Client) ServiceDiscoveryCapabilities(ctx context.Context) (ServiceDiscoveryCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, servicediscoveryop.APINames()...); err != nil {
		return ServiceDiscoveryCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare service discovery capabilities target: %w", err)
	}
	selectors := []func(compatibility.Target) (compatibility.Selection, error){
		servicediscoveryop.SelectTimeMachineRead,
		servicediscoveryop.SelectTimeMachineSet,
		servicediscoveryop.SelectWSDiscoveryRead,
		servicediscoveryop.SelectWSDiscoverySet,
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selectOperation := range selectors {
		selection, err := selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return ServiceDiscoveryCapabilities{}, CompatibilityReport{}, fmt.Errorf("select service discovery backend: %w", err)
		}
		selections = append(selections, selection)
	}
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	capabilityNames := []string{
		servicediscoveryop.TimeMachineReadCapabilityName,
		servicediscoveryop.TimeMachineSetCapabilityName,
		servicediscoveryop.WSDiscoveryReadCapabilityName,
		servicediscoveryop.WSDiscoverySetCapabilityName,
	}
	for index, name := range capabilityNames {
		if supported(index) {
			c.target.AddCapability(name)
		}
	}
	capabilities := ServiceDiscoveryCapabilities{
		Read:        supported(0),
		Set:         supported(1),
		WSDiscovery: supported(2) && supported(3),
	}
	return capabilities, c.target.Report(selections...), nil
}

// ApplyServiceDiscoveryChange applies a patch: Time Machine fields are merged
// into a freshly read pair and submitted as one ServiceDiscovery set, and
// WS-Discovery is submitted to its own backend. Each touched area yields one
// mutation result.
func (c *Client) ApplyServiceDiscoveryChange(ctx context.Context, change ServiceDiscoveryChange) ([]ServiceDiscoveryMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, servicediscoveryop.APINames()...); err != nil {
		return nil, fmt.Errorf("prepare service discovery mutation target: %w", err)
	}
	results := make([]ServiceDiscoveryMutationResult, 0, 2)
	if change.SMBTimeMachine != nil || change.AFPTimeMachine != nil {
		current, _, err := servicediscoveryop.ExecuteTimeMachineRead(ctx, c.target, lockedExecutor{client: c})
		if err != nil {
			return nil, fmt.Errorf("refresh Time Machine advertising before apply: %w", err)
		}
		desired := current
		if change.SMBTimeMachine != nil {
			desired.SMB = *change.SMBTimeMachine
		}
		if change.AFPTimeMachine != nil {
			desired.AFP = *change.AFPTimeMachine
		}
		result, _, err := servicediscoveryop.ExecuteTimeMachineSet(ctx, c.target, lockedExecutor{client: c}, desired)
		if err != nil {
			return nil, fmt.Errorf("apply Time Machine advertising: %w", err)
		}
		results = append(results, result)
	}
	if change.WSDiscovery != nil {
		result, _, err := servicediscoveryop.ExecuteWSDiscoverySet(ctx, c.target, lockedExecutor{client: c}, *change.WSDiscovery)
		if err != nil {
			return nil, fmt.Errorf("apply WS-Discovery: %w", err)
		}
		results = append(results, result)
	}
	return results, nil
}
