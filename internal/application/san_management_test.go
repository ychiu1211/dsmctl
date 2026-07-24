package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/derekvery666/dsmctl/internal/domain/san"
	"github.com/derekvery666/dsmctl/internal/domain/storage"
	"github.com/derekvery666/dsmctl/internal/synology"
)

func TestBuildSANPlanLUNCreateBindsStableVolumeAndUnmappedPostcondition(t *testing.T) {
	request := san.ChangeRequest{Action: san.ActionCreate, Resource: san.ResourceLUN, LUN: &san.LUNChange{
		Name: "dsmctl-e2e-lun-unit", BackingVolumeID: "volume_1", SizeBytes: 1 << 30, Provisioning: san.ProvisioningThin,
	}}
	plan, err := BuildSANPlan("lab", san.State{}, writableSANVolumeState(), request)
	if err != nil {
		t.Fatalf("BuildSANPlan() error = %v", err)
	}
	if plan.Hash == "" || plan.References.BackingVolumeID != "volume_1" || plan.References.BackingVolumePath != "/volume1" || plan.VolumeFingerprint == "" {
		t.Fatalf("plan stable volume binding = %#v, hash=%q", plan.References, plan.Hash)
	}
	created := san.State{LUNs: []san.LUN{{
		ID: "lun-uuid", Name: request.LUN.Name, Protocol: san.ProtocolISCSI, SizeBytes: 1 << 30,
		Provisioning: san.ProvisioningThin, BackingLocation: "/volume1", Mapped: false,
	}}}
	if err := verifySANPostcondition(created, plan, "lun-uuid"); err != nil {
		t.Fatalf("verifySANPostcondition() error = %v", err)
	}
	created.LUNs[0].BackingLocation = "/volume2"
	if err := verifySANPostcondition(created, plan, "lun-uuid"); err == nil {
		t.Fatal("verifySANPostcondition() accepted wrong backing volume")
	}
}

func TestBuildSANPlanLUNCreateRejectsUnsafeVolumes(t *testing.T) {
	request := san.ChangeRequest{Action: san.ActionCreate, Resource: san.ResourceLUN, LUN: &san.LUNChange{
		Name: "new-lun", BackingVolumeID: "volume_1", SizeBytes: 1 << 30, Provisioning: san.ProvisioningThick,
	}}
	tests := []struct {
		name   string
		volume storage.Volume
		want   string
	}{
		{name: "missing path", volume: storage.Volume{ID: "volume_1", FileSystem: "btrfs", Status: "normal", AvailableBytes: 2 << 30}, want: "no DSM path"},
		{name: "read only", volume: storage.Volume{ID: "volume_1", Path: "/volume1", FileSystem: "btrfs", Status: "normal", ReadOnly: true, AvailableBytes: 2 << 30}, want: "not normal and writable"},
		{name: "insufficient", volume: storage.Volume{ID: "volume_1", Path: "/volume1", FileSystem: "btrfs", Status: "normal", AvailableBytes: 1}, want: "available bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildSANPlan("lab", san.State{}, storage.State{Volumes: []storage.Volume{test.volume}}, request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildSANPlan() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestBuildSANPlanLUNCreateIgnoresAvailableByteDrift(t *testing.T) {
	request := san.ChangeRequest{Action: san.ActionCreate, Resource: san.ResourceLUN, LUN: &san.LUNChange{
		Name: "dsmctl-e2e-lun-unit", BackingVolumeID: "volume_1", SizeBytes: 1 << 30, Provisioning: san.ProvisioningThin,
	}}
	firstState := writableSANVolumeState()
	first, err := BuildSANPlan("lab", san.State{}, firstState, request)
	if err != nil {
		t.Fatal(err)
	}
	secondState := writableSANVolumeState()
	secondState.Volumes[0].AvailableBytes -= 1 << 20
	second, err := BuildSANPlan("lab", san.State{}, secondState, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.VolumeFingerprint != second.VolumeFingerprint || first.Hash != second.Hash {
		t.Fatalf("available-byte drift changed plan: first=%s/%s second=%s/%s",
			first.VolumeFingerprint, first.Hash, second.VolumeFingerprint, second.Hash)
	}
}

func TestBuildSANPlanLUNCreateBindsStableVolumeSafetyFields(t *testing.T) {
	request := san.ChangeRequest{Action: san.ActionCreate, Resource: san.ResourceLUN, LUN: &san.LUNChange{
		Name: "dsmctl-e2e-lun-unit", BackingVolumeID: "volume_1", SizeBytes: 1 << 30, Provisioning: san.ProvisioningThin,
	}}
	baseState := writableSANVolumeState()
	base, err := BuildSANPlan("lab", san.State{}, baseState, request)
	if err != nil {
		t.Fatal(err)
	}
	changedPath := writableSANVolumeState()
	changedPath.Volumes[0].Path = "/volume2"
	pathPlan, err := BuildSANPlan("lab", san.State{}, changedPath, request)
	if err != nil {
		t.Fatal(err)
	}
	if base.VolumeFingerprint == pathPlan.VolumeFingerprint || base.Hash == pathPlan.Hash {
		t.Fatal("backing-volume path change did not invalidate the plan")
	}
	changedFilesystem := writableSANVolumeState()
	changedFilesystem.Volumes[0].FileSystem = "ext4"
	filesystemPlan, err := BuildSANPlan("lab", san.State{}, changedFilesystem, request)
	if err != nil {
		t.Fatal(err)
	}
	if base.VolumeFingerprint == filesystemPlan.VolumeFingerprint || base.Hash == filesystemPlan.Hash {
		t.Fatal("backing-volume filesystem change did not invalidate the plan")
	}
}

func TestBuildSANPlanStableIDDeleteGuardsMappingsAndSessions(t *testing.T) {
	state := sampleSANState()
	state.Targets[0].ConnectedSessions = 1
	_, err := BuildSANPlan("lab", state, storage.State{}, san.ChangeRequest{
		Action: san.ActionDelete, Resource: san.ResourceTarget, Target: &san.TargetChange{ID: "target-1"},
	})
	if err == nil || !strings.Contains(err.Error(), "active session") {
		t.Fatalf("target delete error = %v", err)
	}

	state.Targets[0].ConnectedSessions = 0
	_, err = BuildSANPlan("lab", state, storage.State{}, san.ChangeRequest{
		Action: san.ActionDelete, Resource: san.ResourceLUN, LUN: &san.LUNChange{ID: "lun-1"},
	})
	if err == nil || !strings.Contains(err.Error(), "is mapped") {
		t.Fatalf("LUN delete error = %v", err)
	}

	state.Mappings = nil
	state.LUNs[0].Mapped = false
	plan, err := BuildSANPlan("lab", state, storage.State{}, san.ChangeRequest{
		Action: san.ActionDelete, Resource: san.ResourceLUN, LUN: &san.LUNChange{ID: "lun-1"},
	})
	if err != nil {
		t.Fatalf("unmapped LUN delete plan error = %v", err)
	}
	if !plan.Destructive || plan.Risk != "high" || plan.Precondition.ResourceID != "lun-1" || plan.Precondition.Fingerprint == "" {
		t.Fatalf("delete plan guard = %#v", plan)
	}
}

func TestBuildSANPlanMappingBindsBothEndpoints(t *testing.T) {
	state := sampleSANState()
	state.Mappings = nil
	state.LUNs[0].Mapped = false
	request := san.ChangeRequest{Action: san.ActionAttach, Resource: san.ResourceMapping, Mapping: &san.MappingChange{TargetID: "target-1", LUNID: "lun-1"}}
	plan, err := BuildSANPlan("lab", state, storage.State{}, request)
	if err != nil {
		t.Fatalf("BuildSANPlan() error = %v", err)
	}
	if plan.References.TargetID != "target-1" || plan.References.LUNID != "lun-1" || plan.Precondition.ExpectedExists {
		t.Fatalf("mapping plan = %#v", plan)
	}
	state.Mappings = []san.Mapping{{TargetID: "target-1", LUNID: "lun-1"}}
	state.LUNs[0].Mapped = true
	if err := verifySANPostcondition(state, plan, "target-1:lun-1"); err != nil {
		t.Fatalf("mapping postcondition error = %v", err)
	}
	state.Targets = nil
	if err := verifySANPostcondition(state, plan, "target-1:lun-1"); err == nil {
		t.Fatal("mapping postcondition accepted a missing endpoint")
	}
}

func TestBuildSANPlanExpansionRequiresResolvableCurrentVolume(t *testing.T) {
	newSize := uint64(2 << 30)
	state := san.State{LUNs: []san.LUN{{ID: "lun-1", Name: "lun", SizeBytes: 1 << 30, BackingLocation: "/missing"}}}
	_, err := BuildSANPlan("lab", state, writableSANVolumeState(), san.ChangeRequest{
		Action: san.ActionUpdate, Resource: san.ResourceLUN, LUN: &san.LUNChange{ID: "lun-1", NewSizeBytes: &newSize},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot resolve current LUN backing path") {
		t.Fatalf("BuildSANPlan() error = %v", err)
	}
}

func TestSANPlanHashRejectsTamperingAndKeepsSecretReferences(t *testing.T) {
	request := san.ChangeRequest{Action: san.ActionCreate, Resource: san.ResourceTarget, Target: &san.TargetChange{
		Name: "target", IQN: "iqn.2026-07.test:target", Authentication: san.AuthenticationCHAP,
		CHAPUser: "initiator", CHAPPasswordRef: "env:DSMCTL_TEST_CHAP",
	}}
	plan, err := BuildSANPlan("lab", san.State{}, storage.State{}, request)
	if err != nil {
		t.Fatalf("BuildSANPlan() error = %v", err)
	}
	if err := validateSANPlan(plan, plan.Hash); err != nil {
		t.Fatalf("validateSANPlan() error = %v", err)
	}
	if strings.Contains(strings.Join(plan.Summary, " "), "password") {
		t.Fatal("plan summary should not contain password material")
	}
	plan.Request.Target.Name = "tampered"
	if err := validateSANPlan(plan, plan.Hash); err == nil {
		t.Fatal("validateSANPlan() accepted tampered plan")
	}
}

func TestCanonicalSANRequestRejectsEnabledWithTargetPropertyPatch(t *testing.T) {
	enabled := false
	name := "renamed"
	_, err := canonicalSANRequest(san.ChangeRequest{
		Action: san.ActionUpdate, Resource: san.ResourceTarget,
		Target: &san.TargetChange{ID: "target-1", Enabled: &enabled, NewName: &name},
	})
	if err == nil || !strings.Contains(err.Error(), "must be planned separately") {
		t.Fatalf("canonicalSANRequest() error = %v", err)
	}
}

func TestSANFailureResultIncludesActionableCurrentState(t *testing.T) {
	request := san.ChangeRequest{Action: san.ActionCreate, Resource: san.ResourceLUN, LUN: &san.LUNChange{
		Name: "uncertain-lun", BackingVolumeID: "volume_1", SizeBytes: 1 << 30, Provisioning: san.ProvisioningThin,
	}}
	plan, err := BuildSANPlan("lab", san.State{}, writableSANVolumeState(), request)
	if err != nil {
		t.Fatal(err)
	}
	current := synology.SANState{LUNs: []san.LUN{{ID: "lun-uncertain", Name: "uncertain-lun"}}}
	result, applyErr := (&Service{}).sanFailureResult(context.Background(), "lab", &scriptedSANStateClient{states: []synology.SANState{current}}, plan, "san.lun.create", errors.New("transport response lost"))
	var typed *SANApplyError
	if !errors.As(applyErr, &typed) || !typed.Retryable || !typed.ResourceExists || typed.MappingExists {
		t.Fatalf("SANApplyError = %#v (%v)", typed, applyErr)
	}
	if result.Applied || !result.Retryable || result.StateFingerprint == "" || len(result.SAN.LUNs) != 1 {
		t.Fatalf("failure result = %#v", result)
	}
}

func TestSANDeletePlanHashBindsObservedNameAndUnmappedGraph(t *testing.T) {
	state := san.State{LUNs: []san.LUN{{ID: "lun-1", Name: "created-name", Mapped: false}}}
	request := san.ChangeRequest{Action: san.ActionDelete, Resource: san.ResourceLUN, LUN: &san.LUNChange{ID: "lun-1"}}
	first, err := BuildSANPlan("lab", state, storage.State{}, request)
	if err != nil {
		t.Fatal(err)
	}
	state.LUNs[0].Name = "unexpected-name"
	second, err := BuildSANPlan("lab", state, storage.State{}, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash == second.Hash || first.Precondition.Fingerprint == second.Precondition.Fingerprint {
		t.Fatal("delete plan did not bind the observed LUN name into its precondition and hash")
	}
}

func TestWaitForSANPostconditionPollsUntilStableIDAppears(t *testing.T) {
	request := san.ChangeRequest{Action: san.ActionCreate, Resource: san.ResourceLUN, LUN: &san.LUNChange{
		Name: "new-lun", BackingVolumeID: "volume_1", SizeBytes: 1 << 30, Provisioning: san.ProvisioningThin,
	}}
	plan, err := BuildSANPlan("lab", san.State{}, writableSANVolumeState(), request)
	if err != nil {
		t.Fatal(err)
	}
	client := &scriptedSANStateClient{states: []synology.SANState{
		{},
		{LUNs: []san.LUN{{ID: "lun-new", Name: "new-lun", SizeBytes: 1 << 30, Provisioning: san.ProvisioningThin, BackingLocation: "/volume1"}}},
	}}
	state, err := waitForSANPostcondition(context.Background(), client, plan, "lun-new")
	if err != nil {
		t.Fatalf("waitForSANPostcondition() error = %v", err)
	}
	if len(state.LUNs) != 1 || client.calls != 2 {
		t.Fatalf("state=%#v calls=%d", state, client.calls)
	}
}

type scriptedSANStateClient struct {
	states []synology.SANState
	calls  int
}

func (client *scriptedSANStateClient) SANState(context.Context) (synology.SANState, error) {
	index := client.calls
	client.calls++
	if index >= len(client.states) {
		index = len(client.states) - 1
	}
	return client.states[index], nil
}

func writableSANVolumeState() storage.State {
	return storage.State{Volumes: []storage.Volume{{
		ID: "volume_1", Path: "/volume1", FileSystem: "btrfs", Status: "normal", AvailableBytes: 10 << 30,
	}}}
}

func sampleSANState() san.State {
	return san.State{
		Targets:  []san.Target{{ID: "target-1", Name: "target", IQN: "iqn.2026-07.test:target", Authentication: san.AuthenticationNone}},
		LUNs:     []san.LUN{{ID: "lun-1", Name: "lun", SizeBytes: 1 << 30, Provisioning: san.ProvisioningThin, BackingLocation: "/volume1", Mapped: true}},
		Mappings: []san.Mapping{{TargetID: "target-1", LUNID: "lun-1"}},
	}
}
