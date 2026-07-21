package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/snapshotreplication"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	snapshotops "github.com/ychiu1211/dsmctl/internal/synology/operations/snapshotreplication"
)

type SnapshotReplicationShareSnapshots = snapshotreplication.ShareSnapshots
type SnapshotReplicationShareConfig = snapshotreplication.ShareConfig
type SnapshotReplicationRetentionPolicy = snapshotreplication.RetentionPolicy
type SnapshotReplicationLogPage = snapshotreplication.LogPage
type SnapshotReplicationNodeIdentity = snapshotreplication.NodeIdentity
type SnapshotReplicationPlans = snapshotreplication.ReplicationPlans
type SnapshotReplicationCapabilities = snapshotreplication.Capabilities
type SnapshotReplicationChange = snapshotreplication.Change
type SnapshotReplicationMutationResult = snapshotops.MutationResult

func (c *Client) snapshotReplicationEvidenceLocked() snapshotreplication.PackageEvidence {
	evidence := snapshotreplication.PackageEvidence{ID: snapshotops.PackageID}
	if installed, ok := c.target.InstalledPackage(snapshotops.PackageID); ok {
		evidence.Installed = true
		evidence.Version = installed.Version.Raw
		evidence.Running = installed.Running
	}
	return evidence
}

func (c *Client) prepareSnapshotReplicationTargetLocked(ctx context.Context) error {
	if err := c.preparePackageScopedTargetLocked(ctx, snapshotops.APINames()...); err != nil {
		return fmt.Errorf("prepare Snapshot Replication target: %w", err)
	}
	return nil
}

// SnapshotReplicationShareSnapshots lists one shared folder's btrfs snapshots.
func (c *Client) SnapshotReplicationShareSnapshots(ctx context.Context, share string) (SnapshotReplicationShareSnapshots, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return SnapshotReplicationShareSnapshots{}, err
	}
	snapshots, _, err := snapshotops.ExecuteSnapshots(ctx, c.target, lockedExecutor{client: c}, snapshotops.ShareInput{Share: share})
	if err != nil {
		return SnapshotReplicationShareSnapshots{}, fmt.Errorf("get snapshots of share %q: %w", share, err)
	}
	c.target.AddCapability(snapshotops.SnapshotsReadCapabilityName)
	return snapshots, nil
}

// SnapshotReplicationShareConfig reads one shared folder's snapshot settings.
func (c *Client) SnapshotReplicationShareConfig(ctx context.Context, share string) (SnapshotReplicationShareConfig, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return SnapshotReplicationShareConfig{}, err
	}
	config, _, err := snapshotops.ExecuteShareConfig(ctx, c.target, lockedExecutor{client: c}, snapshotops.ShareInput{Share: share})
	if err != nil {
		return SnapshotReplicationShareConfig{}, fmt.Errorf("get snapshot configuration of share %q: %w", share, err)
	}
	c.target.AddCapability(snapshotops.ShareConfigReadCapabilityName)
	return config, nil
}

// SnapshotReplicationRetention reads one shared folder's retention policy.
func (c *Client) SnapshotReplicationRetention(ctx context.Context, share string) (SnapshotReplicationRetentionPolicy, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return SnapshotReplicationRetentionPolicy{}, err
	}
	policy, _, err := snapshotops.ExecuteRetention(ctx, c.target, lockedExecutor{client: c}, snapshotops.ShareInput{Share: share})
	if err != nil {
		return SnapshotReplicationRetentionPolicy{}, fmt.Errorf("get retention policy of share %q: %w", share, err)
	}
	c.target.AddCapability(snapshotops.RetentionReadCapabilityName)
	return policy, nil
}

// SnapshotReplicationLog reads one page of the Snapshot Replication log feed.
func (c *Client) SnapshotReplicationLog(ctx context.Context, offset, limit int) (SnapshotReplicationLogPage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return SnapshotReplicationLogPage{}, err
	}
	page, _, err := snapshotops.ExecuteLog(ctx, c.target, lockedExecutor{client: c}, snapshotops.LogInput{Offset: offset, Limit: limit})
	if err != nil {
		return SnapshotReplicationLogPage{}, fmt.Errorf("get Snapshot Replication log: %w", err)
	}
	c.target.AddCapability(snapshotops.LogReadCapabilityName)
	return page, nil
}

// SnapshotReplicationNode reads the local replication node identity.
func (c *Client) SnapshotReplicationNode(ctx context.Context) (SnapshotReplicationNodeIdentity, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return SnapshotReplicationNodeIdentity{}, err
	}
	node, _, err := snapshotops.ExecuteNode(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return SnapshotReplicationNodeIdentity{}, fmt.Errorf("get replication node identity: %w", err)
	}
	c.target.AddCapability(snapshotops.NodeReadCapabilityName)
	return node, nil
}

// SnapshotReplicationPlans lists replication plans. It requires the installed
// SnapshotReplication package and fails closed without it.
func (c *Client) SnapshotReplicationPlans(ctx context.Context) (SnapshotReplicationPlans, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return SnapshotReplicationPlans{}, err
	}
	evidence := c.snapshotReplicationEvidenceLocked()
	plans, _, err := snapshotops.ExecutePlans(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		if evidence.Installed && !evidence.Running {
			return SnapshotReplicationPlans{}, fmt.Errorf("get replication plans: the SnapshotReplication package is installed but not running; start it with a package lifecycle plan and retry: %w", err)
		}
		return SnapshotReplicationPlans{}, fmt.Errorf("get replication plans: %w", err)
	}
	c.target.AddCapability(snapshotops.ReplicationReadCapabilityName)
	return plans, nil
}

// ApplySnapshotReplicationChange performs one validated snapshot mutation.
func (c *Client) ApplySnapshotReplicationChange(ctx context.Context, change SnapshotReplicationChange) (SnapshotReplicationMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return SnapshotReplicationMutationResult{}, err
	}
	executor := lockedExecutor{client: c}
	var result SnapshotReplicationMutationResult
	var err error
	switch change.Action {
	case snapshotreplication.ActionCreate:
		result, _, err = snapshotops.ExecuteSnapshotCreate(ctx, c.target, executor, snapshotops.CreateInput{
			Share: change.Share, Description: change.Description, Lock: change.Lock,
		})
	case snapshotreplication.ActionSetAttributes:
		result, _, err = snapshotops.ExecuteSnapshotSet(ctx, c.target, executor, snapshotops.SetInput{
			Share: change.Share, Snapshot: change.Snapshot, Description: change.Description, Lock: change.Lock,
		})
	case snapshotreplication.ActionDelete:
		result, _, err = snapshotops.ExecuteSnapshotDelete(ctx, c.target, executor, snapshotops.DeleteInput{
			Share: change.Share, Snapshots: change.Snapshots,
		})
	case snapshotreplication.ActionSetShareConfig:
		result, _, err = snapshotops.ExecuteShareConfigSet(ctx, c.target, executor, snapshotops.ShareConfigSetInput{
			Share: change.Share, SnapshotBrowsing: change.SnapshotBrowsing, LocalTimeFormat: change.LocalTimeFormat,
		})
	default:
		return SnapshotReplicationMutationResult{}, fmt.Errorf("unsupported snapshot change action %q", change.Action)
	}
	if err != nil {
		return SnapshotReplicationMutationResult{}, fmt.Errorf("apply snapshot %s: %w", change.Action, err)
	}
	return result, nil
}

// SnapshotReplicationModuleCapabilities reports the module's operations plus
// package evidence.
func (c *Client) SnapshotReplicationModuleCapabilities(ctx context.Context) (SnapshotReplicationCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return SnapshotReplicationCapabilities{}, CompatibilityReport{}, err
	}
	selectors := []func(compatibility.Target) (compatibility.Selection, error){
		snapshotops.SelectSnapshots,
		snapshotops.SelectShareConfig,
		snapshotops.SelectRetention,
		snapshotops.SelectLog,
		snapshotops.SelectNode,
		snapshotops.SelectPlans,
		snapshotops.SelectSnapshotCreate,
		snapshotops.SelectSnapshotSet,
		snapshotops.SelectSnapshotDelete,
		snapshotops.SelectShareConfigSet,
		snapshotops.SelectReplicationPair,
		snapshotops.SelectReplicationCreate,
		snapshotops.SelectReplicationManage,
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selectOperation := range selectors {
		selection, err := selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return SnapshotReplicationCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Snapshot Replication backend: %w", err)
		}
		selections = append(selections, selection)
	}
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	capabilityNames := []string{
		snapshotops.SnapshotsReadCapabilityName,
		snapshotops.ShareConfigReadCapabilityName,
		snapshotops.RetentionReadCapabilityName,
		snapshotops.LogReadCapabilityName,
		snapshotops.NodeReadCapabilityName,
		snapshotops.ReplicationReadCapabilityName,
		snapshotops.SnapshotCreateCapabilityName,
		snapshotops.SnapshotSetAttributesCapabilityName,
		snapshotops.SnapshotDeleteCapabilityName,
		snapshotops.ShareConfigSetCapabilityName,
		snapshotops.ReplicationPairCapabilityName,
		snapshotops.ReplicationCreateCapabilityName,
		snapshotops.ReplicationManageCapabilityName,
	}
	for index, name := range capabilityNames {
		if supported(index) {
			c.target.AddCapability(name)
		}
	}
	capabilities := SnapshotReplicationCapabilities{
		Module:                snapshotreplication.ModuleName,
		Package:               c.snapshotReplicationEvidenceLocked(),
		SnapshotsRead:         supported(0),
		ShareConfigRead:       supported(1),
		RetentionRead:         supported(2),
		LogRead:               supported(3),
		NodeRead:              supported(4),
		ReplicationRead:       supported(5),
		SnapshotCreate:        supported(6),
		SnapshotSetAttributes: supported(7),
		SnapshotDelete:        supported(8),
		ShareConfigSet:        supported(9),
		ReplicationPair:       supported(10),
		ReplicationCreate:     supported(11),
		ReplicationManage:     supported(12),
	}
	return capabilities, c.target.Report(selections...), nil
}
