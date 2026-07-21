package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/storage"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

func TestBuildStoragePlanSupportsCanonicalRAIDTypes(t *testing.T) {
	tests := []struct {
		raidType string
		disks    int
	}{
		{storage.RAIDSHR, 1},
		{storage.RAIDSHR2, 4},
		{storage.RAID0, 2},
		{storage.RAID1, 2},
		{storage.RAID5, 3},
		{storage.RAID6, 4},
		{storage.RAID10, 4},
		{storage.RAIDJBOD, 2},
		{storage.RAIDBasic, 1},
	}
	state := storageContractState(8)
	for _, test := range tests {
		t.Run(test.raidType, func(t *testing.T) {
			diskIDs := make([]string, test.disks)
			for index := range diskIDs {
				diskIDs[index] = state.Disks[index].ID
			}
			plan, err := BuildStoragePlan("lab", state, storage.ChangeRequest{
				Action: storage.ActionCreate, Resource: storage.ResourcePool,
				Pool: &storage.PoolChange{Name: "pool-" + test.raidType, RAIDType: test.raidType, DiskIDs: diskIDs},
			})
			if err != nil {
				t.Fatalf("BuildStoragePlan() error = %v", err)
			}
			if plan.Request.Pool.RAIDType != test.raidType {
				t.Fatalf("canonical RAID type = %q, want %q", plan.Request.Pool.RAIDType, test.raidType)
			}
			if plan.Hash == "" || plan.TopologyFingerprint == "" {
				t.Fatalf("plan is missing hashes: %#v", plan)
			}
			if plan.SafetyFingerprint == "" || !containsString(plan.ApplicableRAIDTypes, test.raidType) {
				t.Fatalf("plan safety/applicable RAID choices = %#v", plan)
			}
		})
	}
}

func TestBuildStoragePlanCalculatesModelAndDiskApplicableRAIDChoices(t *testing.T) {
	state := storageContractState(4)
	state.PoolCreation.SupportsSHR = false
	_, err := BuildStoragePlan("lab", state, storage.ChangeRequest{
		Action: storage.ActionCreate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAIDSHR, DiskIDs: []string{"disk-01", "disk-02"}},
	})
	if err == nil || !strings.Contains(err.Error(), "not applicable") {
		t.Fatalf("BuildStoragePlan() error = %v, want model capability rejection", err)
	}

	plan, err := BuildStoragePlan("lab", state, storage.ChangeRequest{
		Action: storage.ActionCreate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAID1, DiskIDs: []string{"disk-01", "disk-02"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(plan.ApplicableRAIDTypes, ","); got != "raid0,raid1,jbod" {
		t.Fatalf("applicable RAID choices = %q", got)
	}
}

func TestBuildStoragePlanRejectsUnsafeCandidateDisk(t *testing.T) {
	state := storageContractState(2)
	state.Disks[1].Health = "critical"
	_, err := BuildStoragePlan("lab", state, storage.ChangeRequest{
		Action: storage.ActionCreate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAID1, DiskIDs: []string{"disk-01", "disk-02"}},
	})
	if err == nil || !strings.Contains(err.Error(), "not eligible") {
		t.Fatalf("BuildStoragePlan() error = %v, want disk safety rejection", err)
	}
}

func TestBuildStoragePlanAcceptsCanonicalUnusedOwnerMarker(t *testing.T) {
	state := storageContractState(1)
	state.Disks[0].UsedBy = "unused"
	state.Disks[0].InUse = false
	if _, err := BuildStoragePlan("lab", state, storage.ChangeRequest{
		Action: storage.ActionCreate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAIDBasic, DiskIDs: []string{"disk-01"}},
	}); err != nil {
		t.Fatalf("BuildStoragePlan() rejected DSM unused marker: %v", err)
	}
}

func TestBuildStoragePlanAcceptsSysPartitionNormalDisks(t *testing.T) {
	// A freshly installed NAS reports its free disks as "sys_partition_normal"
	// because the DSM system partition is mirrored across every drive; such disks
	// must remain eligible pool candidates or a fresh box could never get a data
	// pool.
	state := storageContractState(3)
	for index := range state.Disks {
		state.Disks[index].Status = "sys_partition_normal"
	}
	if _, err := BuildStoragePlan("lab", state, storage.ChangeRequest{
		Action: storage.ActionCreate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAID5, DiskIDs: []string{"disk-01", "disk-02", "disk-03"}},
	}); err != nil {
		t.Fatalf("BuildStoragePlan() rejected sys_partition_normal disks: %v", err)
	}
}

func TestBuildStoragePlanGatesUnsupportedDrivesBehindOptIn(t *testing.T) {
	state := storageContractState(3)
	for index := range state.Disks {
		state.Disks[index].Compatibility = "not_in_support"
	}
	request := func(allow bool) storage.ChangeRequest {
		return storage.ChangeRequest{
			Action: storage.ActionCreate, Resource: storage.ResourcePool,
			Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAID5, DiskIDs: []string{"disk-01", "disk-02", "disk-03"}, AllowUnsupportedDisks: allow},
		}
	}
	if _, err := BuildStoragePlan("lab", state, request(false)); err == nil || !strings.Contains(err.Error(), "not eligible") {
		t.Fatalf("BuildStoragePlan() error = %v, want unsupported-drive rejection without opt-in", err)
	}
	plan, err := BuildStoragePlan("lab", state, request(true))
	if err != nil {
		t.Fatalf("BuildStoragePlan() with allow_unsupported_disks rejected the create: %v", err)
	}
	if !plan.Request.Pool.AllowUnsupportedDisks {
		t.Fatalf("plan did not preserve allow_unsupported_disks: %#v", plan.Request.Pool)
	}
	var warned bool
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "allow_unsupported_disks is set") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("plan warnings missing unsupported-drive advisory: %#v", plan.Warnings)
	}
}

func TestStoragePostStatusAcceptsBenignBackgroundOptimizing(t *testing.T) {
	// A freshly created RAID5 pool reports "background_optimizing" with
	// actioning=false while it is already writable and volume-ready; the pool
	// postcondition, the volume parent-pool check, and the volume postcondition
	// must all accept that state, but never a genuine failure state.
	pool := storage.Pool{ID: "reuse_1", Status: "background_optimizing", Health: "background_optimizing", Writable: true, Compatible: true, CanCreateVolume: true}
	if err := validatePoolPostStatus(pool); err != nil {
		t.Fatalf("validatePoolPostStatus rejected background_optimizing pool: %v", err)
	}
	if err := validateVolumePoolForMutation(pool, true); err != nil {
		t.Fatalf("validateVolumePoolForMutation rejected background_optimizing parent pool: %v", err)
	}
	if err := validatePoolPostStatus(storage.Pool{ID: "p", Status: "crashed", Health: "crashed"}); err == nil {
		t.Fatal("validatePoolPostStatus accepted a crashed pool")
	}
	volume := storage.Volume{ID: "volume_1", Status: "background_optimizing", Health: "normal"}
	if err := validateVolumePostStatus(volume); err != nil {
		t.Fatalf("validateVolumePostStatus rejected background_optimizing volume: %v", err)
	}
}

func TestVerifyStorageVolumePostconditionToleratesEmptyDSMName(t *testing.T) {
	// DSM often does not persist a display name for the first volume on a pool
	// (it reports an empty name), so the create postcondition must identify the
	// new volume by its new stable ID plus pool, filesystem, and approved
	// capacity — not by name. A freshly created volume is also still "creating"
	// (actioning=true), which must be accepted.
	plan := StoragePlan{
		Request:               storage.ChangeRequest{Action: storage.ActionCreate, Resource: storage.ResourceVolume, Volume: &storage.VolumeChange{Name: "volume1", PoolID: "reuse_1", FileSystem: "btrfs"}},
		References:            StorageStableReferences{PoolLayout: "multiple"},
		ResolvedCapacityBytes: 2966748659712,
	}
	before := storage.State{}
	after := storage.State{Volumes: []storage.Volume{{ID: "volume_1", Name: "", PoolID: "reuse_1", FileSystem: "btrfs", AllocatedBytes: 2966748659712, Status: "creating", Health: "creating", Writable: true, Actioning: true}}}
	id, err := verifyStorageVolumePostcondition(plan, before, after)
	if err != nil {
		t.Fatalf("verifyStorageVolumePostcondition rejected an empty-named creating volume: %v", err)
	}
	if id != "volume_1" {
		t.Fatalf("verifyStorageVolumePostcondition returned id %q, want volume_1", id)
	}
}

func TestBuildStoragePlanCanonicalizesDiskOrder(t *testing.T) {
	state := storageContractState(3)
	request := func(disks ...string) storage.ChangeRequest {
		return storage.ChangeRequest{
			Action: storage.ActionCreate, Resource: storage.ResourcePool,
			Pool: &storage.PoolChange{Name: "data", RAIDType: "RAID 1", DiskIDs: disks},
		}
	}
	first, err := BuildStoragePlan("lab", state, request("disk-02", "disk-01"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildStoragePlan("lab", state, request("disk-01", "disk-02"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash != second.Hash {
		t.Fatalf("equivalent disk sets produced different hashes: %s != %s", first.Hash, second.Hash)
	}
	if got := strings.Join(first.References.DiskIDs, ","); got != "disk-01,disk-02" {
		t.Fatalf("canonical disk references = %q", got)
	}
}

func TestBuildStoragePlanHashBindsIntentReferencesAndTopology(t *testing.T) {
	state := storageContractState(3)
	baseRequest := storage.ChangeRequest{
		Action: storage.ActionCreate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAID1, DiskIDs: []string{"disk-01", "disk-02"}},
	}
	base, err := BuildStoragePlan("lab", state, baseRequest)
	if err != nil {
		t.Fatal(err)
	}

	changedIntent := baseRequest
	changedPool := *baseRequest.Pool
	changedPool.Name = "archive"
	changedIntent.Pool = &changedPool
	intentPlan, err := BuildStoragePlan("lab", state, changedIntent)
	if err != nil {
		t.Fatal(err)
	}
	if base.Hash == intentPlan.Hash {
		t.Fatal("plan hash did not change when intent changed")
	}

	changedReferences := baseRequest
	changedPool = *baseRequest.Pool
	changedPool.DiskIDs = []string{"disk-01", "disk-03"}
	changedReferences.Pool = &changedPool
	referencePlan, err := BuildStoragePlan("lab", state, changedReferences)
	if err != nil {
		t.Fatal(err)
	}
	if base.Hash == referencePlan.Hash {
		t.Fatal("plan hash did not change when stable disk references changed")
	}

	changedTopology := state
	changedTopology.Disks = append([]storage.Disk(nil), state.Disks...)
	changedTopology.Disks[0].SizeBytes++
	topologyPlan, err := BuildStoragePlan("lab", changedTopology, baseRequest)
	if err != nil {
		t.Fatal(err)
	}
	if base.TopologyFingerprint == topologyPlan.TopologyFingerprint || base.Hash == topologyPlan.Hash {
		t.Fatal("topology change did not change topology fingerprint and plan hash")
	}
}

func TestBuildStoragePlanRejectsInvalidTopologyReferences(t *testing.T) {
	state := storageContractState(3)
	state.Pools = []storage.Pool{{ID: "pool-1", Name: "existing", RAIDType: storage.RAIDBasic, DiskIDs: []string{"disk-01"}}}
	tests := []struct {
		name    string
		request storage.ChangeRequest
		want    string
	}{
		{
			name: "duplicate disk",
			request: storage.ChangeRequest{Action: storage.ActionCreate, Resource: storage.ResourcePool,
				Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAID1, DiskIDs: []string{"disk-02", "disk-02"}}},
			want: "duplicate stable disk ID",
		},
		{
			name: "unknown disk",
			request: storage.ChangeRequest{Action: storage.ActionCreate, Resource: storage.ResourcePool,
				Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAID1, DiskIDs: []string{"disk-02", "missing"}}},
			want: "unknown stable disk ID",
		},
		{
			name: "occupied disk",
			request: storage.ChangeRequest{Action: storage.ActionCreate, Resource: storage.ResourcePool,
				Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAID1, DiskIDs: []string{"disk-01", "disk-02"}}},
			want: "already belongs to pool",
		},
		{
			name: "unknown parent pool",
			request: storage.ChangeRequest{Action: storage.ActionCreate, Resource: storage.ResourceVolume,
				Volume: &storage.VolumeChange{Name: "volume2", PoolID: "missing", FileSystem: storage.FileSystemBtrfs, Capacity: &storage.CapacityPolicy{Mode: storage.CapacityMaximum}}},
			want: "stable ID \"missing\" does not exist",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildStoragePlan("lab", state, test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildStoragePlan() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestBuildStoragePlanEnforcesPatchOnlyUpdates(t *testing.T) {
	state := storageContractState(3)
	state.Pools = []storage.Pool{{ID: "pool-1", Name: "existing", RAIDType: storage.RAID1, Status: "normal", Health: "normal", DiskIDs: []string{"disk-01", "disk-02"}, Writable: true, CanDelete: true}}
	state.Volumes = []storage.Volume{{ID: "volume-1", Name: "volume1", PoolID: "pool-1", FileSystem: storage.FileSystemBtrfs, SizeBytes: 100}}
	tests := []struct {
		name    string
		request storage.ChangeRequest
		want    string
	}{
		{
			name: "pool update cannot own initial disk set",
			request: storage.ChangeRequest{Action: storage.ActionUpdate, Resource: storage.ResourcePool,
				Pool: &storage.PoolChange{ID: "pool-1", DiskIDs: []string{"disk-01", "disk-02", "disk-03"}}},
			want: "patch-only",
		},
		{
			name: "volume update cannot change filesystem",
			request: storage.ChangeRequest{Action: storage.ActionUpdate, Resource: storage.ResourceVolume,
				Volume: &storage.VolumeChange{ID: "volume-1", FileSystem: storage.FileSystemExt4, ExpandTo: &storage.CapacityPolicy{Mode: storage.CapacityMaximum}}},
			want: "patch-only",
		},
		{
			name: "volume cannot shrink",
			request: storage.ChangeRequest{Action: storage.ActionUpdate, Resource: storage.ResourceVolume,
				Volume: &storage.VolumeChange{ID: "volume-1", ExpandTo: &storage.CapacityPolicy{Mode: storage.CapacityExact, SizeBytes: 99}}},
			want: "must exceed observed allocated size",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildStoragePlan("lab", state, test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildStoragePlan() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestBuildStoragePlanMarksDeletionConsequences(t *testing.T) {
	state := storageContractState(2)
	state.Pools = []storage.Pool{{ID: "pool-1", Name: "existing", RAIDType: storage.RAID1, Status: "normal", Health: "normal", DiskIDs: []string{"disk-01", "disk-02"}, Writable: true, CanExpand: true, CanDelete: true, MaxDiskCount: 24}}
	state.Volumes = []storage.Volume{
		{ID: "volume-2", Name: "volume2", PoolID: "pool-1", FileSystem: storage.FileSystemBtrfs},
		{ID: "volume-1", Name: "volume1", PoolID: "pool-1", FileSystem: storage.FileSystemBtrfs},
	}
	plan, err := BuildStoragePlan("lab", state, storage.ChangeRequest{
		Action: storage.ActionDelete, Resource: storage.ResourcePool, Pool: &storage.PoolChange{ID: "pool-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Destructive || plan.Risk != "high" || len(plan.DestructiveConsequences) != 1 {
		t.Fatalf("delete risk = destructive:%v risk:%q consequences:%#v", plan.Destructive, plan.Risk, plan.DestructiveConsequences)
	}
	if got := strings.Join(plan.DestructiveConsequences[0].ResourceIDs, ","); got != "pool-1,volume-1,volume-2" {
		t.Fatalf("destructive resource IDs = %q", got)
	}
}

func TestBuildStoragePlanDistinguishesAdditiveExpansionFromRAIDMigration(t *testing.T) {
	state := storageContractState(4)
	state.Pools = []storage.Pool{{ID: "pool-1", Name: "existing", RAIDType: storage.RAID1, Status: "normal", Health: "normal", DiskIDs: []string{"disk-01", "disk-02"}, Writable: true, CanExpand: true, CanDelete: true, MaxDiskCount: 24}}
	additive, err := BuildStoragePlan("lab", state, storage.ChangeRequest{
		Action: storage.ActionUpdate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{ID: "pool-1", AddDiskIDs: []string{"disk-03"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if additive.Destructive || additive.Risk != "medium" || len(additive.DestructiveConsequences) != 0 {
		t.Fatalf("additive expansion risk = destructive:%v risk:%q consequences:%#v", additive.Destructive, additive.Risk, additive.DestructiveConsequences)
	}
	if got := strings.Join(additive.References.DiskIDs, ","); got != "disk-01,disk-02,disk-03" {
		t.Fatalf("final topology disk references = %q", got)
	}

	target := storage.RAID5
	migration, err := BuildStoragePlan("lab", state, storage.ChangeRequest{
		Action: storage.ActionUpdate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{ID: "pool-1", AddDiskIDs: []string{"disk-03"}, TargetRAIDType: &target},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !migration.Destructive || migration.Risk != "high" || len(migration.DestructiveConsequences) != 1 {
		t.Fatalf("RAID migration risk = destructive:%v risk:%q consequences:%#v", migration.Destructive, migration.Risk, migration.DestructiveConsequences)
	}
}

func TestStorageServiceAdaptersFailClosedWithoutRuntime(t *testing.T) {
	service := &Service{}
	request := storage.ChangeRequest{Action: storage.ActionCreate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAID1, DiskIDs: []string{"disk-01", "disk-02"}}}
	if _, err := service.PlanStorageChange(context.Background(), "lab", request); !errors.Is(err, ErrStorageMutationBackendUnavailable) {
		t.Fatalf("PlanStorageChange() error = %v", err)
	}
	plan, err := BuildStoragePlan("lab", storageContractState(2), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyStoragePlan(context.Background(), plan, plan.Hash); !errors.Is(err, ErrStorageMutationBackendUnavailable) {
		t.Fatalf("ApplyStoragePlan() error = %v", err)
	}
}

func TestApplyStoragePlanRejectsTamperingBeforeUnsupported(t *testing.T) {
	request := storage.ChangeRequest{Action: storage.ActionCreate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAID1, DiskIDs: []string{"disk-01", "disk-02"}}}
	plan, err := BuildStoragePlan("lab", storageContractState(2), request)
	if err != nil {
		t.Fatal(err)
	}
	plan.Request.Pool.Name = "tampered"
	if _, err := (&Service{}).ApplyStoragePlan(context.Background(), plan, plan.Hash); err == nil || errors.Is(err, ErrStorageMutationBackendUnavailable) {
		t.Fatalf("ApplyStoragePlan() error = %v, want hash validation error", err)
	}
}

func TestApplyStoragePlanRejectsStaleDiskSafetyStateBeforeMutation(t *testing.T) {
	before := storageContractState(2)
	request := storage.ChangeRequest{Action: storage.ActionCreate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAID1, DiskIDs: []string{"disk-01", "disk-02"}}}
	plan, err := BuildStoragePlan("lab", before, request)
	if err != nil {
		t.Fatal(err)
	}
	changed := before
	changed.Disks = append([]storage.Disk(nil), before.Disks...)
	changed.Disks[1].Health = "critical"
	client := &fakeStorageManagementClient{states: []storage.State{changed}, capabilities: poolCapabilities()}
	_, err = applyStoragePlanWithClient(context.Background(), client, plan)
	if err == nil || client.applyCalls != 0 || !strings.Contains(err.Error(), "precondition failed") {
		t.Fatalf("result error=%v applyCalls=%d, want stale state before mutation", err, client.applyCalls)
	}
}

func TestApplyStoragePlanRejectsStalePoolStateBeforeMutation(t *testing.T) {
	before := poolExpansionState()
	request := storage.ChangeRequest{Action: storage.ActionUpdate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{ID: "pool-1", AddDiskIDs: []string{"disk-03"}}}
	plan, err := BuildStoragePlan("lab", before, request)
	if err != nil {
		t.Fatal(err)
	}
	changed := before
	changed.Pools = append([]storage.Pool(nil), before.Pools...)
	changed.Pools[0].Actioning = true
	client := &fakeStorageManagementClient{states: []storage.State{changed}, capabilities: poolCapabilities()}
	_, err = applyStoragePlanWithClient(context.Background(), client, plan)
	if err == nil || client.applyCalls != 0 || !strings.Contains(err.Error(), "precondition failed") {
		t.Fatalf("result error=%v applyCalls=%d, want stale pool state before mutation", err, client.applyCalls)
	}
}

func TestApplyStoragePlanVerifiesPoolPostconditions(t *testing.T) {
	tests := []struct {
		name    string
		before  storage.State
		request storage.ChangeRequest
		after   storage.State
		wantID  string
	}{
		{
			name:   "create resolves new stable ID",
			before: storageContractState(2),
			request: storage.ChangeRequest{Action: storage.ActionCreate, Resource: storage.ResourcePool,
				Pool: &storage.PoolChange{Name: "data", RAIDType: storage.RAID1, DiskIDs: []string{"disk-01", "disk-02"}}},
			after: func() storage.State {
				state := storageContractState(2)
				state.Pools = []storage.Pool{{ID: "pool-new", Name: "data", RAIDType: "raid_1", Status: "normal", Health: "normal", DiskIDs: []string{"disk-02", "disk-01"}, Writable: true, CanDelete: true}}
				return state
			}(),
			wantID: "pool-new",
		},
		{
			name:   "expand verifies complete member set and unchanged RAID",
			before: poolExpansionState(),
			request: storage.ChangeRequest{Action: storage.ActionUpdate, Resource: storage.ResourcePool,
				Pool: &storage.PoolChange{ID: "pool-1", AddDiskIDs: []string{"disk-03"}}},
			after: func() storage.State {
				state := poolExpansionState()
				state.Pools[0].DiskIDs = []string{"disk-03", "disk-01", "disk-02"}
				return state
			}(),
			wantID: "pool-1",
		},
		{
			name: "delete verifies stable ID absence",
			before: func() storage.State {
				state := storageContractState(2)
				state.Pools = []storage.Pool{{ID: "pool-1", Name: "data", RAIDType: storage.RAID1, Status: "normal", Health: "normal", DiskIDs: []string{"disk-01", "disk-02"}, Writable: true, CanDelete: true}}
				return state
			}(),
			request: storage.ChangeRequest{Action: storage.ActionDelete, Resource: storage.ResourcePool, Pool: &storage.PoolChange{ID: "pool-1"}},
			after:   storageContractState(2),
			wantID:  "pool-1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := BuildStoragePlan("lab", test.before, test.request)
			if err != nil {
				t.Fatal(err)
			}
			client := &fakeStorageManagementClient{states: []storage.State{test.before, test.after}, capabilities: poolCapabilities()}
			result, err := applyStoragePlanWithClient(context.Background(), client, plan)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Applied || result.Operation.ResourceID != test.wantID || client.applyCalls != 1 {
				t.Fatalf("result=%#v applyCalls=%d", result, client.applyCalls)
			}
		})
	}
}

func TestApplyStoragePlanRejectsIncorrectPoolPostcondition(t *testing.T) {
	before := poolExpansionState()
	request := storage.ChangeRequest{Action: storage.ActionUpdate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{ID: "pool-1", AddDiskIDs: []string{"disk-03"}}}
	plan, err := BuildStoragePlan("lab", before, request)
	if err != nil {
		t.Fatal(err)
	}
	after := poolExpansionState()
	after.Pools[0].DiskIDs = []string{"disk-01", "disk-02"}
	client := &fakeStorageManagementClient{states: []storage.State{before, after}, capabilities: poolCapabilities()}
	_, err = applyStoragePlanWithClient(context.Background(), client, plan)
	if err == nil || client.applyCalls != 1 || !strings.Contains(err.Error(), "postcondition failed") {
		t.Fatalf("result error=%v applyCalls=%d, want postcondition failure", err, client.applyCalls)
	}
}

func TestBuildStorageVolumePlansResolveCapacityAndStablePaths(t *testing.T) {
	state := volumeManagementState()
	create, err := BuildStoragePlan("lab", state, storage.ChangeRequest{
		Action: storage.ActionCreate, Resource: storage.ResourceVolume,
		Volume: &storage.VolumeChange{Name: "managed", PoolID: "pool-1", FileSystem: storage.FileSystemBtrfs,
			Capacity: &storage.CapacityPolicy{Mode: storage.CapacityExact, SizeBytes: 20 << 30}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if create.ResolvedCapacityBytes != 20<<30 || create.References.PoolPath != "reuse_1" || create.References.PoolLayout != "multiple" || create.Destructive {
		t.Fatalf("create plan = %#v", create)
	}

	maximum, err := BuildStoragePlan("lab", state, storage.ChangeRequest{
		Action: storage.ActionUpdate, Resource: storage.ResourceVolume,
		Volume: &storage.VolumeChange{ID: "volume_1", ExpandTo: &storage.CapacityPolicy{Mode: storage.CapacityMaximum}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if maximum.ResolvedCapacityBytes != 80<<30 || maximum.References.ResourceID != "volume_1" {
		t.Fatalf("maximum expansion plan = %#v", maximum)
	}
}

func TestBuildStorageVolumePlanRejectsUnsafeCapabilitiesAndCapacity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*storage.State)
		change storage.VolumeChange
		want   string
	}{
		{
			name: "filesystem not advertised",
			mutate: func(state *storage.State) {
				state.VolumeCreation.SupportedFileSystems = []string{storage.FileSystemExt4}
			},
			change: storage.VolumeChange{Name: "managed", PoolID: "pool-1", FileSystem: storage.FileSystemBtrfs, Capacity: &storage.CapacityPolicy{Mode: storage.CapacityMaximum}},
			want:   "not advertised",
		},
		{
			name:   "pool unavailable",
			mutate: func(state *storage.State) { state.Pools[0].CanCreateVolume = false },
			change: storage.VolumeChange{Name: "managed", PoolID: "pool-1", FileSystem: storage.FileSystemBtrfs, Capacity: &storage.CapacityPolicy{Mode: storage.CapacityMaximum}},
			want:   "creation available",
		},
		{
			name:   "not whole GiB",
			mutate: func(*storage.State) {},
			change: storage.VolumeChange{Name: "managed", PoolID: "pool-1", FileSystem: storage.FileSystemBtrfs, Capacity: &storage.CapacityPolicy{Mode: storage.CapacityExact, SizeBytes: (20 << 30) + 1}},
			want:   "whole GiB",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := volumeManagementState()
			test.mutate(&state)
			_, err := BuildStoragePlan("lab", state, storage.ChangeRequest{Action: storage.ActionCreate, Resource: storage.ResourceVolume, Volume: &test.change})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestBuildStorageVolumePlanRejectsCapacityOverflow(t *testing.T) {
	state := volumeManagementState()
	state.Volumes[0].AllocatedBytes = ^uint64(0) - 1
	state.Volumes[0].MaxFileSystemBytes = 0
	state.Pools[0].AvailableBytes = 2
	state.VolumeCreation.MaxFileSystemBytes = 0
	_, err := BuildStoragePlan("lab", state, storage.ChangeRequest{
		Action: storage.ActionUpdate, Resource: storage.ResourceVolume,
		Volume: &storage.VolumeChange{ID: "volume_1", ExpandTo: &storage.CapacityPolicy{Mode: storage.CapacityMaximum}},
	})
	if err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("error = %v, want overflow rejection", err)
	}
}

func TestApplyStorageVolumePlansRejectStaleCapacityAndVerifyPostconditions(t *testing.T) {
	t.Run("stale pool capacity blocks apply", func(t *testing.T) {
		before := volumeManagementState()
		request := storage.ChangeRequest{Action: storage.ActionCreate, Resource: storage.ResourceVolume,
			Volume: &storage.VolumeChange{Name: "managed", PoolID: "pool-1", FileSystem: storage.FileSystemBtrfs, Capacity: &storage.CapacityPolicy{Mode: storage.CapacityMaximum}}}
		plan, err := BuildStoragePlan("lab", before, request)
		if err != nil {
			t.Fatal(err)
		}
		changed := before
		changed.Pools = append([]storage.Pool(nil), before.Pools...)
		changed.Pools[0].AvailableBytes--
		client := &fakeStorageManagementClient{states: []storage.State{changed}, capabilities: volumeCapabilities()}
		_, err = applyStoragePlanWithClient(context.Background(), client, plan)
		if err == nil || client.applyCalls != 0 || !strings.Contains(err.Error(), "stale") {
			t.Fatalf("error=%v applyCalls=%d", err, client.applyCalls)
		}
	})

	tests := []struct {
		name    string
		before  storage.State
		request storage.ChangeRequest
		after   func(storage.State) storage.State
		wantID  string
	}{
		{
			name: "create resolves exactly one new stable ID", before: volumeManagementState(), wantID: "volume_2",
			request: storage.ChangeRequest{Action: storage.ActionCreate, Resource: storage.ResourceVolume,
				Volume: &storage.VolumeChange{Name: "managed", PoolID: "pool-1", FileSystem: storage.FileSystemBtrfs, Capacity: &storage.CapacityPolicy{Mode: storage.CapacityExact, SizeBytes: 20 << 30}}},
			after: func(state storage.State) storage.State {
				state.Volumes = append(state.Volumes, storage.Volume{ID: "volume_2", Name: "managed", PoolID: "pool-1", FileSystem: "btrfs", AllocatedBytes: 20 << 30, Status: "normal", Health: "normal", Writable: true})
				return state
			},
		},
		{
			name: "expand verifies approved target", before: volumeManagementState(), wantID: "volume_1",
			request: storage.ChangeRequest{Action: storage.ActionUpdate, Resource: storage.ResourceVolume,
				Volume: &storage.VolumeChange{ID: "volume_1", ExpandTo: &storage.CapacityPolicy{Mode: storage.CapacityExact, SizeBytes: 30 << 30}}},
			after: func(state storage.State) storage.State {
				state.Volumes = append([]storage.Volume(nil), state.Volumes...)
				state.Volumes[0].AllocatedBytes = 30 << 30
				return state
			},
		},
		{
			name: "delete verifies stable ID absence", before: volumeManagementState(), wantID: "volume_1",
			request: storage.ChangeRequest{Action: storage.ActionDelete, Resource: storage.ResourceVolume, Volume: &storage.VolumeChange{ID: "volume_1"}},
			after:   func(state storage.State) storage.State { state.Volumes = nil; return state },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := BuildStoragePlan("lab", test.before, test.request)
			if err != nil {
				t.Fatal(err)
			}
			client := &fakeStorageManagementClient{states: []storage.State{test.before, test.after(test.before)}, capabilities: volumeCapabilities()}
			result, err := applyStoragePlanWithClient(context.Background(), client, plan)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Applied || result.Operation.ResourceID != test.wantID || client.applyCalls != 1 {
				t.Fatalf("result=%#v applyCalls=%d", result, client.applyCalls)
			}
		})
	}
}

type fakeStorageManagementClient struct {
	states       []storage.State
	stateCalls   int
	applyCalls   int
	capabilities storage.Capabilities
}

func (client *fakeStorageManagementClient) StorageState(context.Context) (synology.StorageState, error) {
	if client.stateCalls >= len(client.states) {
		return storage.State{}, fmt.Errorf("unexpected storage state read %d", client.stateCalls+1)
	}
	state := client.states[client.stateCalls]
	client.stateCalls++
	return state, nil
}

func (client *fakeStorageManagementClient) StorageCapabilities(context.Context) (synology.StorageCapabilities, synology.CompatibilityReport, error) {
	return client.capabilities, synology.CompatibilityReport{}, nil
}

func (client *fakeStorageManagementClient) ApplyStorageChange(_ context.Context, input synology.StorageMutationInput) (synology.StorageMutationResult, error) {
	client.applyCalls++
	return synology.StorageMutationResult{Operation: "storage." + input.Request.Resource + "." + input.Request.Action}, nil
}

func poolCapabilities() storage.Capabilities {
	return storage.Capabilities{InventoryRead: true, PoolCreate: true, PoolUpdate: true, PoolDelete: true, Mutations: true}
}

func volumeCapabilities() storage.Capabilities {
	return storage.Capabilities{InventoryRead: true, VolumeCreate: true, VolumeUpdate: true, VolumeDelete: true, Mutations: true}
}

func volumeManagementState() storage.State {
	return storage.State{
		VolumeCreation: storage.VolumeCreationConstraints{
			SupportedFileSystems: []string{storage.FileSystemBtrfs, storage.FileSystemExt4},
			MinimumSizeBytes:     10 << 30, MaxFileSystemBytes: 100 << 30,
		},
		Pools: []storage.Pool{{
			ID: "pool-1", Path: "reuse_1", SpacePath: "/dev/vg1", RAIDType: storage.RAID5, Layout: "multiple",
			Status: "normal", Health: "normal", SizeBytes: 100 << 30, UsedBytes: 40 << 30, AvailableBytes: 60 << 30,
			Writable: true, Compatible: true, CanCreateVolume: true,
		}},
		Volumes: []storage.Volume{{
			ID: "volume_1", Name: "existing", PoolID: "pool-1", FileSystem: storage.FileSystemBtrfs,
			Status: "normal", Health: "normal", SizeBytes: 19 << 30, AllocatedBytes: 20 << 30,
			MaxFileSystemBytes: 100 << 30, Writable: true, CanExpand: true, CanDelete: true,
		}},
	}
}

func poolExpansionState() storage.State {
	state := storageContractState(3)
	state.Pools = []storage.Pool{{ID: "pool-1", Name: "data", RAIDType: storage.RAID1, Status: "normal", Health: "normal", DiskIDs: []string{"disk-01", "disk-02"}, Writable: true, CanExpand: true, CanDelete: true, MaxDiskCount: 24}}
	return state
}

func storageContractState(diskCount int) storage.State {
	state := storage.State{
		Disks: make([]storage.Disk, diskCount), PoolCreation: storage.PoolCreationConstraints{SupportsSHR: true, MaxDisks: diskCount},
		VolumeCreation: storage.VolumeCreationConstraints{SupportedFileSystems: []string{storage.FileSystemBtrfs, storage.FileSystemExt4}, MinimumSizeBytes: 10 << 30, MaxFileSystemBytes: 100 << 30},
	}
	for index := range state.Disks {
		state.Disks[index] = storage.Disk{
			ID: "disk-0" + string(rune('1'+index)), Serial: "serial-0" + string(rune('1'+index)), SizeBytes: 1_000_000,
			Status: "normal", Health: "normal", SMARTStatus: "normal", Selectable: true, Compatibility: "support",
		}
	}
	return state
}
