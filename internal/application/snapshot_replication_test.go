package application

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/snapshotreplication"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type fakeSnapshotReplicationClient struct {
	snapshots    []snapshotreplication.Snapshot
	config       synology.SnapshotReplicationShareConfig
	capabilities synology.SnapshotReplicationCapabilities
	// applyChanges makes ApplySnapshotReplicationChange mutate the fake state;
	// set false to simulate a DSM write that silently does nothing so the
	// postcondition re-read must catch it.
	applyChanges bool
	applied      []snapshotreplication.Change
	nextCreate   string
}

func newFakeSnapshotReplicationClient() *fakeSnapshotReplicationClient {
	return &fakeSnapshotReplicationClient{
		snapshots: []snapshotreplication.Snapshot{
			{Time: "GMT+08-2026.07.21-01.00.00", Description: "old", Locked: true},
			{Time: "GMT+08-2026.07.21-02.00.00", Locked: false},
		},
		config: synology.SnapshotReplicationShareConfig{Share: "data", SnapshotBrowsing: false, LocalTimeFormat: true},
		capabilities: synology.SnapshotReplicationCapabilities{
			Module:        snapshotreplication.ModuleName,
			SnapshotsRead: true, ShareConfigRead: true, RetentionRead: true,
			LogRead: true, NodeRead: true,
			SnapshotCreate: true, SnapshotSetAttributes: true, SnapshotDelete: true, ShareConfigSet: true,
			Package: snapshotreplication.PackageEvidence{ID: "SnapshotReplication"},
		},
		applyChanges: true,
		nextCreate:   "GMT+08-2026.07.21-03.00.00",
	}
}

func (c *fakeSnapshotReplicationClient) ShareState(context.Context, bool) (synology.ShareState, error) {
	return synology.ShareState{}, nil
}

func (c *fakeSnapshotReplicationClient) SnapshotReplicationShareSnapshots(_ context.Context, share string) (synology.SnapshotReplicationShareSnapshots, error) {
	snapshots := make([]snapshotreplication.Snapshot, len(c.snapshots))
	copy(snapshots, c.snapshots)
	return synology.SnapshotReplicationShareSnapshots{Share: share, Total: len(snapshots), Snapshots: snapshots}, nil
}

func (c *fakeSnapshotReplicationClient) SnapshotReplicationShareConfig(context.Context, string) (synology.SnapshotReplicationShareConfig, error) {
	return c.config, nil
}

func (c *fakeSnapshotReplicationClient) SnapshotReplicationRetention(_ context.Context, share string) (synology.SnapshotReplicationRetentionPolicy, error) {
	return synology.SnapshotReplicationRetentionPolicy{Share: share, TaskID: -1}, nil
}

func (c *fakeSnapshotReplicationClient) SnapshotReplicationLog(context.Context, int, int) (synology.SnapshotReplicationLogPage, error) {
	return synology.SnapshotReplicationLogPage{Entries: []snapshotreplication.LogEntry{}}, nil
}

func (c *fakeSnapshotReplicationClient) SnapshotReplicationNode(context.Context) (synology.SnapshotReplicationNodeIdentity, error) {
	return synology.SnapshotReplicationNodeIdentity{Hostname: "fake"}, nil
}

func (c *fakeSnapshotReplicationClient) SnapshotReplicationPlans(context.Context) (synology.SnapshotReplicationPlans, error) {
	return synology.SnapshotReplicationPlans{}, fmt.Errorf("package not installed")
}

func (c *fakeSnapshotReplicationClient) SnapshotReplicationModuleCapabilities(context.Context) (synology.SnapshotReplicationCapabilities, synology.CompatibilityReport, error) {
	return c.capabilities, synology.CompatibilityReport{}, nil
}

func (c *fakeSnapshotReplicationClient) ApplySnapshotReplicationChange(_ context.Context, change snapshotreplication.Change) (synology.SnapshotReplicationMutationResult, error) {
	c.applied = append(c.applied, change)
	result := synology.SnapshotReplicationMutationResult{Method: change.Action}
	if change.Action == snapshotreplication.ActionCreate {
		result.Snapshot = c.nextCreate
	}
	if !c.applyChanges {
		return result, nil
	}
	switch change.Action {
	case snapshotreplication.ActionCreate:
		locked := true
		if change.Lock != nil {
			locked = *change.Lock
		}
		description := ""
		if change.Description != nil {
			description = *change.Description
		}
		c.snapshots = append(c.snapshots, snapshotreplication.Snapshot{Time: c.nextCreate, Description: description, Locked: locked})
	case snapshotreplication.ActionSetAttributes:
		for index := range c.snapshots {
			if c.snapshots[index].Time != change.Snapshot {
				continue
			}
			if change.Description != nil {
				c.snapshots[index].Description = *change.Description
			}
			if change.Lock != nil {
				c.snapshots[index].Locked = *change.Lock
			}
		}
	case snapshotreplication.ActionDelete:
		targets := make(map[string]struct{}, len(change.Snapshots))
		for _, target := range change.Snapshots {
			targets[target] = struct{}{}
		}
		kept := c.snapshots[:0]
		for _, snapshot := range c.snapshots {
			if _, doomed := targets[snapshot.Time]; !doomed {
				kept = append(kept, snapshot)
			}
		}
		c.snapshots = kept
	case snapshotreplication.ActionSetShareConfig:
		if change.SnapshotBrowsing != nil {
			c.config.SnapshotBrowsing = *change.SnapshotBrowsing
		}
		if change.LocalTimeFormat != nil {
			c.config.LocalTimeFormat = *change.LocalTimeFormat
		}
	}
	return result, nil
}

// Relation-create interface stubs: the single-NAS snapshot tests never call
// these; the relation flow is exercised by fakeReplicationRelationClient.
func (c *fakeSnapshotReplicationClient) StorageState(context.Context) (synology.StorageState, error) {
	return synology.StorageState{}, nil
}
func (c *fakeSnapshotReplicationClient) ReplicationPairingEndpoint(context.Context) (synology.PairingEndpoint, error) {
	return synology.PairingEndpoint{}, fmt.Errorf("not implemented")
}
func (c *fakeSnapshotReplicationClient) PairReplicationCredential(context.Context, synology.SnapshotReplicationPairEndpoint, string) (string, error) {
	return "", fmt.Errorf("not implemented")
}
func (c *fakeSnapshotReplicationClient) CheckReplicationRemoteConn(context.Context, synology.SnapshotReplicationPairEndpoint, string) error {
	return fmt.Errorf("not implemented")
}
func (c *fakeSnapshotReplicationClient) CreateReplicationPlan(context.Context, snapshotreplication.RelationCreate, synology.SnapshotReplicationPairEndpoint, string) (string, error) {
	return "", fmt.Errorf("not implemented")
}
func (c *fakeSnapshotReplicationClient) PollReplicationTask(context.Context, string) (snapshotreplication.RelationTaskStatus, error) {
	return snapshotreplication.RelationTaskStatus{}, fmt.Errorf("not implemented")
}
func (c *fakeSnapshotReplicationClient) DeleteReplicationPlan(context.Context, string) error {
	return fmt.Errorf("not implemented")
}
func (c *fakeSnapshotReplicationClient) DeleteReplicationCredential(context.Context, string) error {
	return fmt.Errorf("not implemented")
}
func (c *fakeSnapshotReplicationClient) SyncReplicationPlan(context.Context, string, bool, string) error {
	return fmt.Errorf("not implemented")
}
func (c *fakeSnapshotReplicationClient) PauseReplicationPlan(context.Context, string) error {
	return fmt.Errorf("not implemented")
}

func TestValidateSnapshotReplicationChange(t *testing.T) {
	lock := true
	cases := []struct {
		name    string
		change  snapshotreplication.Change
		wantErr string
	}{
		{"missing share", snapshotreplication.Change{Action: snapshotreplication.ActionCreate}, "share is required"},
		{"unknown action", snapshotreplication.Change{Action: "restore", Share: "data"}, "unsupported action"},
		{"set without target", snapshotreplication.Change{Action: snapshotreplication.ActionSetAttributes, Share: "data", Lock: &lock}, "snapshot time name"},
		{"set without fields", snapshotreplication.Change{Action: snapshotreplication.ActionSetAttributes, Share: "data", Snapshot: "GMT+08-x"}, "description or lock"},
		{"delete without targets", snapshotreplication.Change{Action: snapshotreplication.ActionDelete, Share: "data"}, "at least one snapshot"},
		{"delete duplicate", snapshotreplication.Change{Action: snapshotreplication.ActionDelete, Share: "data", Snapshots: []string{"a", "a"}}, "more than once"},
		{"share config empty", snapshotreplication.Change{Action: snapshotreplication.ActionSetShareConfig, Share: "data"}, "snapshot_browsing or local_time_format"},
		{"create with target", snapshotreplication.Change{Action: snapshotreplication.ActionCreate, Share: "data", Snapshot: "GMT+08-x"}, "does not take snapshot targets"},
	}
	for _, test := range cases {
		if err := validateSnapshotReplicationChange(test.change); err == nil || !strings.Contains(err.Error(), test.wantErr) {
			t.Errorf("%s: error = %v, want containing %q", test.name, err, test.wantErr)
		}
	}
	if err := validateSnapshotReplicationChange(snapshotreplication.Change{Action: snapshotreplication.ActionCreate, Share: "data", Lock: &lock}); err != nil {
		t.Fatalf("valid create rejected: %v", err)
	}
}

func TestPlanSnapshotCreateAndApply(t *testing.T) {
	client := newFakeSnapshotReplicationClient()
	description := "before upgrade"
	request := snapshotreplication.Change{Action: snapshotreplication.ActionCreate, Share: "data", Description: &description}
	plan, err := planSnapshotReplicationChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" || plan.Hash == "" || plan.ObservedFingerprint == "" || len(plan.Observed.Snapshots.Snapshots) != 2 {
		t.Fatalf("plan = %#v", plan)
	}
	result, err := applySnapshotReplicationPlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if !result.Applied || result.Result.Snapshot != "GMT+08-2026.07.21-03.00.00" {
		t.Fatalf("apply result = %#v", result)
	}
	if len(client.applied) != 1 || client.applied[0].Action != snapshotreplication.ActionCreate {
		t.Fatalf("applied = %#v", client.applied)
	}
}

func TestPlanSnapshotDeleteWarnsAndAppliesHighRisk(t *testing.T) {
	client := newFakeSnapshotReplicationClient()
	request := snapshotreplication.Change{Action: snapshotreplication.ActionDelete, Share: "data", Snapshots: []string{"GMT+08-2026.07.21-01.00.00"}}
	plan, err := planSnapshotReplicationChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "high" {
		t.Fatalf("delete risk = %q", plan.Risk)
	}
	lockedWarning := false
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "locked") {
			lockedWarning = true
		}
	}
	if !lockedWarning {
		t.Fatalf("delete warnings = %#v", plan.Warnings)
	}
	if _, err := applySnapshotReplicationPlanWithClient(context.Background(), client, plan); err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if len(client.snapshots) != 1 || client.snapshots[0].Time != "GMT+08-2026.07.21-02.00.00" {
		t.Fatalf("snapshots after delete = %#v", client.snapshots)
	}
}

func TestPlanSnapshotDeleteRejectsUnknownTarget(t *testing.T) {
	client := newFakeSnapshotReplicationClient()
	request := snapshotreplication.Change{Action: snapshotreplication.ActionDelete, Share: "data", Snapshots: []string{"GMT+08-2099.01.01-00.00.00"}}
	if _, err := planSnapshotReplicationChangeWithClient(context.Background(), "lab", client, request); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("plan error = %v", err)
	}
}

func TestPlanSnapshotSetRejectsNoOp(t *testing.T) {
	client := newFakeSnapshotReplicationClient()
	lock := true
	request := snapshotreplication.Change{Action: snapshotreplication.ActionSetAttributes, Share: "data", Snapshot: "GMT+08-2026.07.21-01.00.00", Lock: &lock}
	if _, err := planSnapshotReplicationChangeWithClient(context.Background(), "lab", client, request); err == nil || !strings.Contains(err.Error(), "would not change") {
		t.Fatalf("plan error = %v", err)
	}
}

func TestApplySnapshotPlanRejectsStaleState(t *testing.T) {
	client := newFakeSnapshotReplicationClient()
	lock := false
	request := snapshotreplication.Change{Action: snapshotreplication.ActionSetAttributes, Share: "data", Snapshot: "GMT+08-2026.07.21-01.00.00", Lock: &lock}
	plan, err := planSnapshotReplicationChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	// A snapshot taken out-of-band invalidates the observed set.
	client.snapshots = append(client.snapshots, snapshotreplication.Snapshot{Time: "GMT+08-2026.07.21-02.30.00"})
	if _, err := applySnapshotReplicationPlanWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("apply error = %v", err)
	}
}

func TestApplySnapshotPlanpostconditionCatchesSilentNoop(t *testing.T) {
	client := newFakeSnapshotReplicationClient()
	browsing := true
	request := snapshotreplication.Change{Action: snapshotreplication.ActionSetShareConfig, Share: "data", SnapshotBrowsing: &browsing}
	plan, err := planSnapshotReplicationChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	client.applyChanges = false
	if _, err := applySnapshotReplicationPlanWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "does not match the approved change") {
		t.Fatalf("apply error = %v", err)
	}
}

func TestValidateSnapshotPlanRejectsTampering(t *testing.T) {
	client := newFakeSnapshotReplicationClient()
	description := "x"
	request := snapshotreplication.Change{Action: snapshotreplication.ActionCreate, Share: "data", Description: &description}
	plan, err := planSnapshotReplicationChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if err := validateSnapshotReplicationPlan(plan, plan.Hash); err != nil {
		t.Fatalf("validate error = %v", err)
	}
	tampered := plan
	tampered.Request.Share = "homes"
	if err := validateSnapshotReplicationPlan(tampered, tampered.Hash); err == nil {
		t.Fatal("tampered plan accepted")
	}
	if err := validateSnapshotReplicationPlan(plan, "wrong"); err == nil {
		t.Fatal("wrong approval hash accepted")
	}
}
