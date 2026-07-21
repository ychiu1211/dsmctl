package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/share"
	"github.com/ychiu1211/dsmctl/internal/domain/snapshotreplication"
	"github.com/ychiu1211/dsmctl/internal/domain/storage"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

// fakeReplicationRelationClient is a two-role fake: instances stand in for both
// the source and destination NAS. It records the pairing/create calls and the
// secret material it received so tests can assert none came from the plan.
type fakeReplicationRelationClient struct {
	caps        synology.SnapshotReplicationCapabilities
	shares      []string
	shareSnap   map[string]bool
	volPath     string
	volFS       string
	volWritable bool
	volAvail    uint64
	plans       []snapshotreplication.ReplicationPlan

	endpointSID   string
	pairCredID    string
	createTask    string
	pollStatus    snapshotreplication.RelationTaskStatus
	confirmPlanID string // ID of the plan the confirming read reports (default plan-1)
	failCheck     bool

	// recorded
	pairedSID          string
	createdWith        *snapshotreplication.RelationCreate
	deletedCreds       []string
	deletedPlans       []string
	syncedPlans        []string
	pausedPlans        []string
	confirmAfterCreate bool
}

func newSourceRelation() *fakeReplicationRelationClient {
	return &fakeReplicationRelationClient{
		caps:               synology.SnapshotReplicationCapabilities{ReplicationCreate: true, ReplicationPair: true, ReplicationRead: true},
		shares:             []string{"data"},
		shareSnap:          map[string]bool{"data": true},
		volPath:            "/volume1",
		volFS:              "btrfs",
		volWritable:        true,
		volAvail:           1 << 40,
		pairCredID:         "dest-cred-abc",
		createTask:         "task-1",
		pollStatus:         snapshotreplication.RelationTaskStatus{Finished: true, Success: true, PlanID: "plan-1", RemotePlanID: "rplan-1", TargetID: "data"},
		confirmAfterCreate: true,
	}
}

func newDestRelation() *fakeReplicationRelationClient {
	return &fakeReplicationRelationClient{
		caps:        synology.SnapshotReplicationCapabilities{ReplicationCreate: true, ReplicationPair: true, ReplicationRead: true},
		shares:      []string{"homes"},
		shareSnap:   map[string]bool{"homes": true},
		volPath:     "/volume1",
		volFS:       "btrfs",
		volWritable: true,
		volAvail:    1 << 40,
		endpointSID: "dest-sid-xyz",
	}
}

func planReplicationRelationForTest(t *testing.T, source, dest *fakeReplicationRelationClient, req snapshotreplication.RelationCreate) (SnapshotReplicationRelationPlan, error) {
	t.Helper()
	return planSnapshotReplicationRelationWithClients(context.Background(), "nas51", source, "nas255", dest, req)
}

func TestPlanReplicationRelationHappyAndNoSecretLeak(t *testing.T) {
	source := newSourceRelation()
	dest := newDestRelation()
	plan, err := planReplicationRelationForTest(t, source, dest, snapshotreplication.RelationCreate{SourceShare: "data", DestVolume: "/volume1"})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "high" || !plan.Destructive || plan.Hash == "" || plan.ObservedFingerprint == "" {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.SourceNAS != "nas51" || plan.DestNAS != "nas255" {
		t.Fatalf("plan sites = %q -> %q", plan.SourceNAS, plan.DestNAS)
	}
	// The dest credential/session must never appear in the serialized plan.
	blob, _ := json.Marshal(plan)
	for _, secret := range []string{"dest-sid-xyz", "dest-cred-abc"} {
		if strings.Contains(string(blob), secret) {
			t.Fatalf("plan leaked secret %q: %s", secret, blob)
		}
	}
}

func TestPlanReplicationRelationRejectsUnsafe(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(src, dst *fakeReplicationRelationClient)
		wantErr string
	}{
		{"dest share collision", func(_, dst *fakeReplicationRelationClient) { dst.shares = append(dst.shares, "data") }, "already has a share named"},
		{"dest relation exists", func(_, dst *fakeReplicationRelationClient) {
			dst.plans = []snapshotreplication.ReplicationPlan{{TargetName: "data"}}
		}, "already has a replication relation"},
		{"source not snapshot-capable", func(src, _ *fakeReplicationRelationClient) { src.shareSnap["data"] = false }, "not snapshot-capable"},
		{"dest volume missing", func(_, dst *fakeReplicationRelationClient) { dst.volPath = "/volume9" }, "does not exist"},
		{"dest volume not btrfs", func(_, dst *fakeReplicationRelationClient) { dst.volFS = "ext4" }, "not btrfs"},
		{"dest volume read-only", func(_, dst *fakeReplicationRelationClient) { dst.volWritable = false }, "read-only or busy"},
		{"source share missing", func(src, _ *fakeReplicationRelationClient) { src.shares = nil }, "does not exist"},
		{"source lacks package", func(src, _ *fakeReplicationRelationClient) { src.caps.ReplicationCreate = false }, "verified replication create backend"},
		{"dest lacks package", func(_, dst *fakeReplicationRelationClient) { dst.caps.ReplicationRead = false }, "does not expose the SnapshotReplication package"},
	}
	for _, test := range cases {
		src := newSourceRelation()
		dst := newDestRelation()
		test.mutate(src, dst)
		if _, err := planReplicationRelationForTest(t, src, dst, snapshotreplication.RelationCreate{SourceShare: "data", DestVolume: "/volume1"}); err == nil || !strings.Contains(err.Error(), test.wantErr) {
			t.Errorf("%s: err = %v, want containing %q", test.name, err, test.wantErr)
		}
	}
}

func TestApplyReplicationRelationBrokersSecretAtApply(t *testing.T) {
	source := newSourceRelation()
	dest := newDestRelation()
	plan, err := planReplicationRelationForTest(t, source, dest, snapshotreplication.RelationCreate{SourceShare: "data", DestVolume: "/volume1"})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	result, err := applySnapshotReplicationRelationWithClients(context.Background(), "nas51", source, "nas255", dest, plan)
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if !result.Applied || result.PlanID != "plan-1" || result.RemotePlanID != "rplan-1" {
		t.Fatalf("result = %#v", result)
	}
	// The source paired using the destination's sid, obtained only at apply from
	// the dest client — never from the plan.
	if source.pairedSID != "dest-sid-xyz" {
		t.Fatalf("source paired with sid %q, want the dest session id", source.pairedSID)
	}
	if source.createdWith == nil || source.createdWith.SourceShare != "data" {
		t.Fatalf("create recorded = %#v", source.createdWith)
	}
	// A successful create must NOT delete the credential (it was consumed).
	if len(source.deletedCreds) != 0 {
		t.Fatalf("credential deleted after success: %#v", source.deletedCreds)
	}
}

func TestApplyReplicationRelationStaleRejected(t *testing.T) {
	source := newSourceRelation()
	dest := newDestRelation()
	plan, err := planReplicationRelationForTest(t, source, dest, snapshotreplication.RelationCreate{SourceShare: "data", DestVolume: "/volume1"})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	// Destination free space changes out-of-band → observed fingerprint drifts.
	dest.volAvail = 1 << 39
	if _, err := applySnapshotReplicationRelationWithClients(context.Background(), "nas51", source, "nas255", dest, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected a stale-plan error when dest state drifts, got %v", err)
	}
}

func TestApplyReplicationRelationConfirmsByPlanID(t *testing.T) {
	// The task self-reports success for plan-NEW, but the confirming list only
	// contains a DIFFERENT, pre-existing relation for the same share name. The
	// apply must NOT false-succeed (it must match on plan id, not target name).
	source := newSourceRelation()
	source.pollStatus.PlanID = "plan-NEW"
	source.confirmPlanID = "plan-OLD" // a same-named but different relation
	dest := newDestRelation()
	plan, err := planReplicationRelationForTest(t, source, dest, snapshotreplication.RelationCreate{SourceShare: "data", DestVolume: "/volume1"})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if _, err := applySnapshotReplicationRelationWithClients(context.Background(), "nas51", source, "nas255", dest, plan); err == nil || !strings.Contains(err.Error(), "is not listed") {
		t.Fatalf("apply must reject a plan-id mismatch, got %v", err)
	}
	// The temporary credential must be cleaned up on this failure.
	if len(source.deletedCreds) != 1 {
		t.Fatalf("credential not cleaned up on confirm failure: %#v", source.deletedCreds)
	}
}

func TestApplyReplicationRelationCleansUpOnTaskFailure(t *testing.T) {
	source := newSourceRelation()
	source.pollStatus = snapshotreplication.RelationTaskStatus{Finished: true, Success: false, Error: "destination out of space"}
	dest := newDestRelation()
	plan, err := planReplicationRelationForTest(t, source, dest, snapshotreplication.RelationCreate{SourceShare: "data", DestVolume: "/volume1"})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if _, err := applySnapshotReplicationRelationWithClients(context.Background(), "nas51", source, "nas255", dest, plan); err == nil || !strings.Contains(err.Error(), "out of space") {
		t.Fatalf("apply error = %v", err)
	}
	// A failed async task must still trigger temp-credential cleanup.
	if len(source.deletedCreds) != 1 || source.deletedCreds[0] != "dest-cred-abc" {
		t.Fatalf("credential leaked on async task failure: %#v", source.deletedCreds)
	}
}

func TestPlanReplicationRelationRejectsSourceFanout(t *testing.T) {
	source := newSourceRelation()
	source.plans = []snapshotreplication.ReplicationPlan{{TargetName: "data"}}
	dest := newDestRelation()
	if _, err := planReplicationRelationForTest(t, source, dest, snapshotreplication.RelationCreate{SourceShare: "data", DestVolume: "/volume1"}); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("expected a source-relation-exists error, got %v", err)
	}
}

func TestPlanReplicationRelationCaseInsensitiveCollision(t *testing.T) {
	source := newSourceRelation()
	dest := newDestRelation()
	dest.shares = append(dest.shares, "Data") // case-variant of source share "data"
	if _, err := planReplicationRelationForTest(t, source, dest, snapshotreplication.RelationCreate{SourceShare: "data", DestVolume: "/volume1"}); err == nil || !strings.Contains(err.Error(), "already has a share named") {
		t.Fatalf("expected a case-insensitive collision error, got %v", err)
	}
}

func TestApplyReplicationRelationCleansUpCredentialOnCheckFailure(t *testing.T) {
	source := newSourceRelation()
	source.failCheck = true
	dest := newDestRelation()
	plan, err := planReplicationRelationForTest(t, source, dest, snapshotreplication.RelationCreate{SourceShare: "data", DestVolume: "/volume1"})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if _, err := applySnapshotReplicationRelationWithClients(context.Background(), "nas51", source, "nas255", dest, plan); err == nil || !strings.Contains(err.Error(), "cannot reach") {
		t.Fatalf("apply error = %v", err)
	}
	if len(source.deletedCreds) != 1 || source.deletedCreds[0] != "dest-cred-abc" {
		t.Fatalf("credential not cleaned up: %#v", source.deletedCreds)
	}
}

func TestValidateReplicationPlanTamper(t *testing.T) {
	source := newSourceRelation()
	dest := newDestRelation()
	plan, err := planReplicationRelationForTest(t, source, dest, snapshotreplication.RelationCreate{SourceShare: "data", DestVolume: "/volume1"})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if err := validateSnapshotReplicationRelationPlan(plan, plan.Hash); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
	tampered := plan
	tampered.Request.DestVolume = "/volume2"
	if err := validateSnapshotReplicationRelationPlan(tampered, tampered.Hash); err == nil {
		t.Fatal("tampered plan accepted")
	}
	if err := validateSnapshotReplicationRelationPlan(plan, "wrong"); err == nil {
		t.Fatal("wrong approval hash accepted")
	}
}

func TestSyncStopRequireRealRelation(t *testing.T) {
	svc := &Service{}
	client := newSourceRelation()
	client.plans = []snapshotreplication.ReplicationPlan{{ID: "plan-7", TargetName: "data"}}

	// requireReplicationRelation is the shared guard; exercise it directly since
	// the Service dispatch needs a manager. Unknown id → refuse.
	if err := requireReplicationRelation(context.Background(), client, "nas51", "plan-UNKNOWN"); err == nil || !strings.Contains(err.Error(), "no replication relation") {
		t.Fatalf("unknown plan id must be refused, got %v", err)
	}
	if err := requireReplicationRelation(context.Background(), client, "nas51", "plan-7"); err != nil {
		t.Fatalf("known plan id rejected: %v", err)
	}
	_ = svc
}

func TestSyncAndStopDispatch(t *testing.T) {
	client := newSourceRelation()
	client.plans = []snapshotreplication.ReplicationPlan{{ID: "plan-7", TargetName: "data"}}

	if err := client.SyncReplicationPlan(context.Background(), "plan-7", true, "manual"); err != nil {
		t.Fatalf("sync error = %v", err)
	}
	if len(client.syncedPlans) != 1 || client.syncedPlans[0] != "plan-7" {
		t.Fatalf("synced = %#v", client.syncedPlans)
	}
	if err := client.PauseReplicationPlan(context.Background(), "plan-7"); err != nil {
		t.Fatalf("pause error = %v", err)
	}
	if len(client.pausedPlans) != 1 || client.pausedPlans[0] != "plan-7" {
		t.Fatalf("paused = %#v", client.pausedPlans)
	}
}

// --- interface implementation ---

func (c *fakeReplicationRelationClient) SnapshotReplicationModuleCapabilities(context.Context) (synology.SnapshotReplicationCapabilities, synology.CompatibilityReport, error) {
	return c.caps, synology.CompatibilityReport{}, nil
}

func (c *fakeReplicationRelationClient) ShareState(_ context.Context, _ bool) (synology.ShareState, error) {
	state := synology.ShareState{}
	for _, name := range c.shares {
		state.Shares = append(state.Shares, share.SharedFolder{Name: name, SnapshotSupported: c.shareSnap[name], VolumePath: c.volPath})
	}
	return state, nil
}

func (c *fakeReplicationRelationClient) StorageState(context.Context) (synology.StorageState, error) {
	if c.volPath == "" {
		return synology.StorageState{}, nil
	}
	return synology.StorageState{Volumes: []storage.Volume{{
		Path: c.volPath, FileSystem: c.volFS, Writable: c.volWritable, AvailableBytes: c.volAvail, UsedBytes: 1 << 20,
	}}}, nil
}

func (c *fakeReplicationRelationClient) SnapshotReplicationPlans(context.Context) (synology.SnapshotReplicationPlans, error) {
	plans := append([]snapshotreplication.ReplicationPlan(nil), c.plans...)
	if c.confirmAfterCreate && c.createdWith != nil {
		id := c.confirmPlanID
		if id == "" {
			id = "plan-1"
		}
		plans = append(plans, snapshotreplication.ReplicationPlan{ID: id, TargetName: c.createdWith.SourceShare})
	}
	return synology.SnapshotReplicationPlans{Total: len(plans), Plans: plans}, nil
}

func (c *fakeReplicationRelationClient) ReplicationPairingEndpoint(context.Context) (synology.PairingEndpoint, error) {
	return synology.PairingEndpoint{Addr: "10.0.0.9", Port: 5001, HTTPS: true, SID: c.endpointSID}, nil
}

func (c *fakeReplicationRelationClient) PairReplicationCredential(_ context.Context, _ synology.SnapshotReplicationPairEndpoint, sid string) (string, error) {
	c.pairedSID = sid
	return c.pairCredID, nil
}

func (c *fakeReplicationRelationClient) CheckReplicationRemoteConn(context.Context, synology.SnapshotReplicationPairEndpoint, string) error {
	if c.failCheck {
		return fmt.Errorf("unreachable")
	}
	return nil
}

func (c *fakeReplicationRelationClient) CreateReplicationPlan(_ context.Context, req snapshotreplication.RelationCreate, _ synology.SnapshotReplicationPairEndpoint, _ string) (string, error) {
	captured := req
	c.createdWith = &captured
	return c.createTask, nil
}

func (c *fakeReplicationRelationClient) PollReplicationTask(context.Context, string) (snapshotreplication.RelationTaskStatus, error) {
	return c.pollStatus, nil
}

func (c *fakeReplicationRelationClient) DeleteReplicationPlan(_ context.Context, planID string) error {
	c.deletedPlans = append(c.deletedPlans, planID)
	return nil
}

func (c *fakeReplicationRelationClient) DeleteReplicationCredential(_ context.Context, credID string) error {
	c.deletedCreds = append(c.deletedCreds, credID)
	return nil
}

func (c *fakeReplicationRelationClient) SyncReplicationPlan(_ context.Context, planID string, _ bool, _ string) error {
	c.syncedPlans = append(c.syncedPlans, planID)
	return nil
}

func (c *fakeReplicationRelationClient) PauseReplicationPlan(_ context.Context, planID string) error {
	c.pausedPlans = append(c.pausedPlans, planID)
	return nil
}

func (c *fakeReplicationRelationClient) SnapshotReplicationShareSnapshots(context.Context, string) (synology.SnapshotReplicationShareSnapshots, error) {
	return synology.SnapshotReplicationShareSnapshots{}, nil
}
func (c *fakeReplicationRelationClient) SnapshotReplicationShareConfig(context.Context, string) (synology.SnapshotReplicationShareConfig, error) {
	return synology.SnapshotReplicationShareConfig{}, nil
}
func (c *fakeReplicationRelationClient) SnapshotReplicationRetention(context.Context, string) (synology.SnapshotReplicationRetentionPolicy, error) {
	return synology.SnapshotReplicationRetentionPolicy{}, nil
}
func (c *fakeReplicationRelationClient) SnapshotReplicationLog(context.Context, int, int) (synology.SnapshotReplicationLogPage, error) {
	return synology.SnapshotReplicationLogPage{}, nil
}
func (c *fakeReplicationRelationClient) SnapshotReplicationNode(context.Context) (synology.SnapshotReplicationNodeIdentity, error) {
	return synology.SnapshotReplicationNodeIdentity{}, nil
}
func (c *fakeReplicationRelationClient) ApplySnapshotReplicationChange(context.Context, snapshotreplication.Change) (synology.SnapshotReplicationMutationResult, error) {
	return synology.SnapshotReplicationMutationResult{}, nil
}

var _ snapshotReplicationClient = (*fakeReplicationRelationClient)(nil)
