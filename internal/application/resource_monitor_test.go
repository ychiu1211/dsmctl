package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/resmon"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type fakeResourceRecordingClient struct {
	setting    synology.ResourceRecordingSetting
	caps       synology.ResourceMonitorCapabilities
	mutations  int
	persist    bool
	lastEnable *bool
}

func (client *fakeResourceRecordingClient) ResourceMonitorSetting(context.Context) (synology.ResourceRecordingSetting, error) {
	return client.setting, nil
}

func (client *fakeResourceRecordingClient) ResourceMonitorCapabilities(context.Context) (synology.ResourceMonitorCapabilities, synology.CompatibilityReport, error) {
	return client.caps, synology.CompatibilityReport{}, nil
}

func (client *fakeResourceRecordingClient) ApplyResourceRecordingChange(_ context.Context, change resmon.RecordingChange) (synology.ResourceRecordingMutationResult, error) {
	client.mutations++
	client.lastEnable = change.Enable
	if client.persist && change.Enable != nil {
		client.setting.Enabled = *change.Enable
	}
	return synology.ResourceRecordingMutationResult{Backend: "resourcemonitor-setting-set-v1", API: "SYNO.ResourceMonitor.Setting", Version: 1, Method: "set"}, nil
}

func recordingTestClient(enabled bool) *fakeResourceRecordingClient {
	return &fakeResourceRecordingClient{
		setting: synology.ResourceRecordingSetting{Enabled: enabled},
		caps:    synology.ResourceMonitorCapabilities{Read: true, RecordingRead: true, RecordingSet: true},
		persist: true,
	}
}

func boolPointer(value bool) *bool { return &value }

func TestResourceRecordingPlanAndApplyEnable(t *testing.T) {
	client := recordingTestClient(false)
	plan, err := planResourceRecordingChangeWithClient(context.Background(), "office", client, resmon.RecordingChange{Enable: boolPointer(true)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "low" || len(plan.Summary) != 1 || plan.ObservedFingerprint == "" || plan.Hash == "" {
		t.Fatalf("plan = %#v", plan)
	}
	result, err := applyResourceRecordingPlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if !result.Applied || result.PlanHash != plan.Hash || client.mutations != 1 || !client.setting.Enabled {
		t.Fatalf("apply result = %#v client = %#v", result, client)
	}
}

func TestResourceRecordingPlanDisableIsMediumRisk(t *testing.T) {
	client := recordingTestClient(true)
	plan, err := planResourceRecordingChangeWithClient(context.Background(), "office", client, resmon.RecordingChange{Enable: boolPointer(false)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" || len(plan.Warnings) != 1 {
		t.Fatalf("plan effects = %q %#v", plan.Risk, plan.Warnings)
	}
}

func TestResourceRecordingPlanRejectsNoOp(t *testing.T) {
	client := recordingTestClient(true)
	_, err := planResourceRecordingChangeWithClient(context.Background(), "office", client, resmon.RecordingChange{Enable: boolPointer(true)})
	if err == nil || !strings.Contains(err.Error(), "already enabled") {
		t.Fatalf("expected already-enabled error, got %v", err)
	}
}

func TestResourceRecordingPlanRequiresSetCapability(t *testing.T) {
	client := recordingTestClient(false)
	client.caps.RecordingSet = false
	if _, err := planResourceRecordingChangeWithClient(context.Background(), "office", client, resmon.RecordingChange{Enable: boolPointer(true)}); err == nil {
		t.Fatal("expected error when recording set is unsupported")
	}
}

func TestResourceRecordingApplyRejectsTamperedHash(t *testing.T) {
	client := recordingTestClient(false)
	plan, err := planResourceRecordingChangeWithClient(context.Background(), "office", client, resmon.RecordingChange{Enable: boolPointer(true)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if err := validateResourceRecordingPlan(plan, "not-the-hash"); err == nil {
		t.Fatal("expected hash mismatch error")
	}
	if client.mutations != 0 {
		t.Fatalf("mutation ran despite bad hash: %d", client.mutations)
	}
}

func TestResourceRecordingApplyRejectsStalePlan(t *testing.T) {
	client := recordingTestClient(false)
	plan, err := planResourceRecordingChangeWithClient(context.Background(), "office", client, resmon.RecordingChange{Enable: boolPointer(true)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	// Someone enabled recording out of band; the plan's precondition no longer holds.
	client.setting.Enabled = true
	if _, err := applyResourceRecordingPlanWithClient(context.Background(), client, plan); err == nil {
		t.Fatal("expected stale-plan error")
	}
	if client.mutations != 0 {
		t.Fatalf("mutation ran on stale plan: %d", client.mutations)
	}
}
