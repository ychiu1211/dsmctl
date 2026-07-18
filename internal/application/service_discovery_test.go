package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/servicediscovery"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type fakeServiceDiscoveryClient struct {
	state synology.ServiceDiscoveryState
	caps  synology.ServiceDiscoveryCapabilities
	ops   int
}

func (client *fakeServiceDiscoveryClient) ServiceDiscoveryState(context.Context) (synology.ServiceDiscoveryState, error) {
	return client.state, nil
}

func (client *fakeServiceDiscoveryClient) ServiceDiscoveryCapabilities(context.Context) (synology.ServiceDiscoveryCapabilities, synology.CompatibilityReport, error) {
	return client.caps, synology.CompatibilityReport{}, nil
}

func (client *fakeServiceDiscoveryClient) ApplyServiceDiscoveryChange(_ context.Context, change servicediscovery.Change) ([]synology.ServiceDiscoveryMutationResult, error) {
	client.ops++
	results := []synology.ServiceDiscoveryMutationResult{}
	if change.SMBTimeMachine != nil {
		client.state.SMBTimeMachine = *change.SMBTimeMachine
	}
	if change.AFPTimeMachine != nil {
		client.state.AFPTimeMachine = *change.AFPTimeMachine
	}
	if change.SMBTimeMachine != nil || change.AFPTimeMachine != nil {
		results = append(results, synology.ServiceDiscoveryMutationResult{Area: "time_machine", Backend: "fake"})
	}
	if change.WSDiscovery != nil {
		enabled := *change.WSDiscovery
		client.state.WSDiscovery = &enabled
		results = append(results, synology.ServiceDiscoveryMutationResult{Area: "ws_discovery", Backend: "fake"})
	}
	return results, nil
}

func fullCapsServiceDiscovery() synology.ServiceDiscoveryCapabilities {
	return synology.ServiceDiscoveryCapabilities{Read: true, Set: true, WSDiscovery: true}
}

func TestServiceDiscoveryPlanApplyAndStale(t *testing.T) {
	client := &fakeServiceDiscoveryClient{
		state: synology.ServiceDiscoveryState{SMBTimeMachine: false, AFPTimeMachine: false, WSDiscovery: boolPtr(false)},
		caps:  fullCapsServiceDiscovery(),
	}
	request := servicediscovery.Change{SMBTimeMachine: boolPtr(true)}
	plan, err := planServiceDiscoveryChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("planServiceDiscoveryChangeWithClient() error = %v", err)
	}
	if plan.Hash == "" || plan.Risk != "medium" {
		t.Fatalf("plan = %#v", plan)
	}

	stale := &fakeServiceDiscoveryClient{
		state: synology.ServiceDiscoveryState{SMBTimeMachine: false, AFPTimeMachine: true, WSDiscovery: boolPtr(false)},
		caps:  fullCapsServiceDiscovery(),
	}
	if _, err := applyServiceDiscoveryPlanWithClient(context.Background(), stale, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale apply error = %v", err)
	}

	result, err := applyServiceDiscoveryPlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("applyServiceDiscoveryPlanWithClient() error = %v", err)
	}
	if !result.Applied || !client.state.SMBTimeMachine || client.ops != 1 {
		t.Fatalf("apply result/client = %#v %#v", result, client.state)
	}
}

func TestServiceDiscoveryWSEnableIsHighRisk(t *testing.T) {
	client := &fakeServiceDiscoveryClient{
		state: synology.ServiceDiscoveryState{WSDiscovery: boolPtr(false)},
		caps:  fullCapsServiceDiscovery(),
	}
	plan, err := planServiceDiscoveryChangeWithClient(context.Background(), "lab", client, servicediscovery.Change{WSDiscovery: boolPtr(true)})
	if err != nil {
		t.Fatalf("planServiceDiscoveryChangeWithClient() error = %v", err)
	}
	if plan.Risk != "high" || len(plan.Warnings) == 0 {
		t.Fatalf("WS enable plan = %#v", plan)
	}
}

func TestServiceDiscoveryRejectsNoOpAndMissingBackends(t *testing.T) {
	client := &fakeServiceDiscoveryClient{
		state: synology.ServiceDiscoveryState{SMBTimeMachine: true, WSDiscovery: boolPtr(false)},
		caps:  fullCapsServiceDiscovery(),
	}
	if _, err := planServiceDiscoveryChangeWithClient(context.Background(), "lab", client, servicediscovery.Change{SMBTimeMachine: boolPtr(true)}); err == nil || !strings.Contains(err.Error(), "would not change") {
		t.Fatalf("no-op plan error = %v", err)
	}

	noWS := &fakeServiceDiscoveryClient{
		state: synology.ServiceDiscoveryState{SMBTimeMachine: false},
		caps:  synology.ServiceDiscoveryCapabilities{Read: true, Set: true, WSDiscovery: false},
	}
	if _, err := planServiceDiscoveryChangeWithClient(context.Background(), "lab", noWS, servicediscovery.Change{WSDiscovery: boolPtr(true)}); err == nil || !strings.Contains(err.Error(), "WS-Discovery") {
		t.Fatalf("ws gating error = %v", err)
	}

	if err := validateServiceDiscoveryChange(servicediscovery.Change{}); err == nil {
		t.Fatal("validateServiceDiscoveryChange accepted an empty patch")
	}
}
