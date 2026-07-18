package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/rsyncservice"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	rsyncserviceop "github.com/ychiu1211/dsmctl/internal/synology/operations/rsyncservice"
)

type RsyncServiceState = rsyncservice.State
type RsyncServiceCapabilities = rsyncservice.Capabilities
type RsyncServiceChange = rsyncservice.Change
type RsyncServiceMutationResult = rsyncserviceop.MutationResult

// RsyncServiceState reads the rsync network-backup service switches.
func (c *Client) RsyncServiceState(ctx context.Context) (RsyncServiceState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, rsyncserviceop.APINames()...); err != nil {
		return RsyncServiceState{}, fmt.Errorf("prepare rsync service target: %w", err)
	}
	settings, _, err := rsyncserviceop.ExecuteRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return RsyncServiceState{}, fmt.Errorf("get rsync service settings: %w", err)
	}
	c.target.AddCapability(rsyncserviceop.ReadCapabilityName)
	return RsyncServiceState{Enabled: settings.Enabled, RsyncAccount: settings.RsyncAccount, SSHPort: settings.SSHPort}, nil
}

// RsyncServiceCapabilities reports the selected rsync-service backend.
func (c *Client) RsyncServiceCapabilities(ctx context.Context) (RsyncServiceCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, rsyncserviceop.APINames()...); err != nil {
		return RsyncServiceCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare rsync service capabilities target: %w", err)
	}
	selectors := []func(compatibility.Target) (compatibility.Selection, error){
		rsyncserviceop.SelectRead,
		rsyncserviceop.SelectSet,
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selectOperation := range selectors {
		selection, err := selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return RsyncServiceCapabilities{}, CompatibilityReport{}, fmt.Errorf("select rsync service backend: %w", err)
		}
		selections = append(selections, selection)
	}
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	if supported(0) {
		c.target.AddCapability(rsyncserviceop.ReadCapabilityName)
	}
	if supported(1) {
		c.target.AddCapability(rsyncserviceop.SetCapabilityName)
	}
	capabilities := RsyncServiceCapabilities{Read: supported(0), Set: supported(1)}
	return capabilities, c.target.Report(selections...), nil
}

// ApplyRsyncServiceChange applies a patch: the current state is read, the patch
// merged, and the whole set (service switch as the required anchor plus the
// account switch) submitted in one call.
func (c *Client) ApplyRsyncServiceChange(ctx context.Context, change RsyncServiceChange) ([]RsyncServiceMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, rsyncserviceop.APINames()...); err != nil {
		return nil, fmt.Errorf("prepare rsync service mutation target: %w", err)
	}
	current, _, err := rsyncserviceop.ExecuteRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return nil, fmt.Errorf("refresh rsync service settings before apply: %w", err)
	}
	desired := current
	if change.Enabled != nil {
		desired.Enabled = *change.Enabled
	}
	if change.RsyncAccount != nil {
		desired.RsyncAccount = *change.RsyncAccount
	}
	result, _, err := rsyncserviceop.ExecuteSet(ctx, c.target, lockedExecutor{client: c}, desired)
	if err != nil {
		return nil, fmt.Errorf("apply rsync service settings: %w", err)
	}
	return []RsyncServiceMutationResult{result}, nil
}
