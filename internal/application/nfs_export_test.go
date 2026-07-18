package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/nfsexport"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type fakeNFSExportClient struct {
	rules     map[string][]nfsexport.Rule
	read      bool
	set       bool
	mutations int
}

func newFakeNFSExportClient(share string, rules []nfsexport.Rule) *fakeNFSExportClient {
	return &fakeNFSExportClient{rules: map[string][]nfsexport.Rule{share: append([]nfsexport.Rule(nil), rules...)}, read: true, set: true}
}

func (client *fakeNFSExportClient) NFSExportState(_ context.Context, share string) (synology.NFSShareExport, error) {
	return synology.NFSShareExport{Share: share, Rules: append([]nfsexport.Rule(nil), client.rules[share]...)}, nil
}

func (client *fakeNFSExportClient) NFSExportCapabilities(context.Context) (synology.NFSExportCapabilities, synology.CompatibilityReport, error) {
	return synology.NFSExportCapabilities{Read: client.read, Set: client.set}, synology.CompatibilityReport{}, nil
}

func (client *fakeNFSExportClient) ApplyNFSExportChange(_ context.Context, request nfsexport.ChangeRequest) (synology.NFSExportMutationResult, error) {
	client.mutations++
	client.rules[request.Share] = append([]nfsexport.Rule(nil), request.Rules...)
	return synology.NFSExportMutationResult{Share: request.Share, Backend: "fake", API: "fake", Version: 1, Method: "save"}, nil
}

func readOnlyRule(client string) nfsexport.Rule {
	return nfsexport.Rule{Client: client, Privilege: nfsexport.PrivilegeReadOnly, Squash: nfsexport.SquashNoMapping, Security: nfsexport.SecuritySys}
}

func TestNFSExportPlanApplyAndStaleState(t *testing.T) {
	original := []nfsexport.Rule{readOnlyRule("10.0.0.0/24")}
	client := newFakeNFSExportClient("backup", original)

	desired := nfsexport.ChangeRequest{Share: "backup", Rules: []nfsexport.Rule{
		{Client: "10.0.0.0/24", Privilege: nfsexport.PrivilegeReadWrite, Squash: nfsexport.SquashNoMapping, Security: nfsexport.SecuritySys},
	}}
	plan, err := planNFSExportChangeWithClient(context.Background(), "lab", client, desired)
	if err != nil {
		t.Fatalf("planNFSExportChangeWithClient() error = %v", err)
	}
	if plan.Hash == "" || plan.ObservedFingerprint == "" || plan.Risk != "high" {
		t.Fatalf("plan = %#v", plan)
	}
	if err := validateNFSExportPlan(plan, plan.Hash); err != nil {
		t.Fatalf("validateNFSExportPlan() error = %v", err)
	}

	stale := newFakeNFSExportClient("backup", []nfsexport.Rule{readOnlyRule("10.0.0.0/24"), readOnlyRule("192.168.0.0/16")})
	if _, err := applyNFSExportPlanWithClient(context.Background(), stale, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale apply error = %v", err)
	}
	if stale.mutations != 0 {
		t.Fatal("stale plan reached mutation")
	}

	result, err := applyNFSExportPlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("applyNFSExportPlanWithClient() error = %v", err)
	}
	if !result.Applied || client.mutations != 1 || client.rules["backup"][0].Privilege != nfsexport.PrivilegeReadWrite {
		t.Fatalf("apply result/client = %#v %#v", result, client.rules)
	}
}

func TestNFSExportPlanRejectsNoOpAndInvalid(t *testing.T) {
	client := newFakeNFSExportClient("backup", []nfsexport.Rule{readOnlyRule("10.0.0.0/24")})

	noop := nfsexport.ChangeRequest{Share: "backup", Rules: []nfsexport.Rule{readOnlyRule("10.0.0.0/24")}}
	if _, err := planNFSExportChangeWithClient(context.Background(), "lab", client, noop); err == nil || !strings.Contains(err.Error(), "would not change") {
		t.Fatalf("no-op plan error = %v", err)
	}

	if err := validateNFSExportRequest(nfsexport.ChangeRequest{Share: "backup", Rules: []nfsexport.Rule{readOnlyRule("a b")}}); err == nil {
		t.Fatal("validateNFSExportRequest accepted a client with a space")
	}
	if err := validateNFSExportRequest(nfsexport.ChangeRequest{Share: "backup", Rules: []nfsexport.Rule{readOnlyRule("dup"), readOnlyRule("dup")}}); err == nil {
		t.Fatal("validateNFSExportRequest accepted duplicate clients")
	}
	if err := validateNFSExportRequest(nfsexport.ChangeRequest{Share: "backup", Rules: []nfsexport.Rule{{Client: "x", Privilege: "maybe", Squash: nfsexport.SquashNoMapping, Security: nfsexport.SecuritySys}}}); err == nil {
		t.Fatal("validateNFSExportRequest accepted an invalid privilege")
	}
}

func TestNFSExportPlanFailsClosedWithoutSetBackend(t *testing.T) {
	client := newFakeNFSExportClient("backup", []nfsexport.Rule{readOnlyRule("10.0.0.0/24")})
	client.set = false
	desired := nfsexport.ChangeRequest{Share: "backup", Rules: []nfsexport.Rule{{Client: "10.0.0.0/24", Privilege: nfsexport.PrivilegeReadWrite, Squash: nfsexport.SquashNoMapping, Security: nfsexport.SecuritySys}}}
	if _, err := planNFSExportChangeWithClient(context.Background(), "lab", client, desired); err == nil || !strings.Contains(err.Error(), "verified NFS export") {
		t.Fatalf("no-set-backend plan error = %v", err)
	}
}

func TestNFSExportPlanEffectsFlagRemovalAndWildcard(t *testing.T) {
	observed := []nfsexport.Rule{readOnlyRule("10.0.0.5"), readOnlyRule("10.0.0.6")}
	desired := []nfsexport.Rule{
		readOnlyRule("10.0.0.5"),
		{Client: "*", Privilege: nfsexport.PrivilegeReadWrite, Squash: nfsexport.SquashNoMapping, Security: nfsexport.SecuritySys},
	}
	destructive, warnings, summary := nfsExportPlanEffects(observed, desired)
	if !destructive {
		t.Fatal("expected removal of 10.0.0.6 to be destructive")
	}
	joined := strings.Join(append(summary, warnings...), "\n")
	if !strings.Contains(joined, "remove NFS export rule for \"10.0.0.6\"") {
		t.Fatalf("missing removal summary: %q", joined)
	}
	if !strings.Contains(joined, "read-write NFS access to any matching host") {
		t.Fatalf("missing wildcard read-write warning: %q", joined)
	}
}

func TestNFSExportPlanHashRejectsTampering(t *testing.T) {
	client := newFakeNFSExportClient("backup", []nfsexport.Rule{readOnlyRule("10.0.0.0/24")})
	desired := nfsexport.ChangeRequest{Share: "backup", Rules: []nfsexport.Rule{{Client: "10.0.0.0/24", Privilege: nfsexport.PrivilegeReadWrite, Squash: nfsexport.SquashNoMapping, Security: nfsexport.SecuritySys}}}
	plan, err := planNFSExportChangeWithClient(context.Background(), "lab", client, desired)
	if err != nil {
		t.Fatalf("planNFSExportChangeWithClient() error = %v", err)
	}
	plan.Risk = "low"
	if err := validateNFSExportPlan(plan, plan.Hash); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("tampered plan error = %v", err)
	}
}
