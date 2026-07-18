package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type fakeFileServiceClient struct {
	smb         synology.SMBState
	nfs         synology.NFSState
	setAdvanced bool
	mutations   int
}

func (client *fakeFileServiceClient) SMBState(context.Context) (synology.SMBState, error) {
	return client.smb, nil
}

func (client *fakeFileServiceClient) NFSState(context.Context) (synology.NFSState, error) {
	return client.nfs, nil
}

func (client *fakeFileServiceClient) FileServiceCapabilities(context.Context) (synology.FileServiceCapabilities, synology.CompatibilityReport, error) {
	return synology.FileServiceCapabilities{
		SMB: controlpanel.FileServiceModuleCapabilities{Module: controlpanel.ModuleSMB, Read: true, Set: true},
		NFS: controlpanel.FileServiceModuleCapabilities{Module: controlpanel.ModuleNFS, Read: true, Set: true, SetAdvanced: client.setAdvanced},
	}, synology.CompatibilityReport{}, nil
}

func (client *fakeFileServiceClient) ApplyFileServiceChange(_ context.Context, request synology.FileServiceChangeRequest) (synology.FileServiceMutationResult, error) {
	client.mutations++
	if change := request.SMB; change != nil {
		if change.Enabled != nil {
			client.smb.Enabled = *change.Enabled
		}
		if change.Workgroup != nil {
			client.smb.Workgroup = strings.TrimSpace(*change.Workgroup)
		}
		if change.MinimumProtocol != nil {
			client.smb.MinimumProtocol = *change.MinimumProtocol
		}
		if change.MaximumProtocol != nil {
			client.smb.MaximumProtocol = *change.MaximumProtocol
		}
		if change.TransportEncryption != nil {
			client.smb.TransportEncryption = *change.TransportEncryption
		}
		if change.ServerSigning != nil {
			client.smb.ServerSigning = *change.ServerSigning
		}
	}
	if change := request.NFS; change != nil {
		if change.Enabled != nil {
			client.nfs.Enabled = *change.Enabled
		}
		if change.MaximumProtocol != nil {
			client.nfs.MaximumProtocol = *change.MaximumProtocol
		}
		if change.NFSv4Domain != nil {
			client.nfs.NFSv4Domain = strings.TrimSpace(*change.NFSv4Domain)
		}
	}
	return synology.FileServiceMutationResult{Protocol: request.Protocol, Backend: "fake", API: "fake", Version: 1, Method: "set"}, nil
}

func TestFileServiceSMBPlanApplyAndStaleState(t *testing.T) {
	client := &fakeFileServiceClient{smb: testSMBState()}
	workgroup := "LAB"
	request := controlpanel.FileServiceChangeRequest{
		Protocol: controlpanel.FileProtocolSMB,
		SMB:      &controlpanel.SMBChange{Workgroup: &workgroup},
	}
	plan, err := planFileServiceChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("planFileServiceChangeWithClient() error = %v", err)
	}
	if plan.Hash == "" || plan.ObservedFingerprint == "" || plan.Observed.SMB == nil || plan.Risk != "medium" {
		t.Fatalf("plan = %#v", plan)
	}
	if err := validateFileServicePlan(plan, plan.Hash); err != nil {
		t.Fatalf("validateFileServicePlan() error = %v", err)
	}

	staleClient := &fakeFileServiceClient{smb: testSMBState()}
	staleClient.smb.ServerSigning = controlpanel.SMBSigningRequired
	if _, err := applyFileServicePlanWithClient(context.Background(), staleClient, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale apply error = %v", err)
	}
	if staleClient.mutations != 0 {
		t.Fatal("stale plan reached mutation")
	}

	result, err := applyFileServicePlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("applyFileServicePlanWithClient() error = %v", err)
	}
	if !result.Applied || client.smb.Workgroup != "LAB" || client.mutations != 1 {
		t.Fatalf("apply result/client = %#v %#v", result, client)
	}
}

func TestFileServicePlanRejectsUnsafeShapesAndNoOps(t *testing.T) {
	client := &fakeFileServiceClient{smb: testSMBState(), nfs: synology.NFSState{
		Enabled: true, MaximumProtocol: controlpanel.NFSProtocol3,
		SupportedProtocols: []controlpanel.NFSProtocol{controlpanel.NFSProtocol2, controlpanel.NFSProtocol3, controlpanel.NFSProtocol4},
	}}

	domain := "lab.example"
	if _, err := planFileServiceChangeWithClient(context.Background(), "lab", client, controlpanel.FileServiceChangeRequest{
		Protocol: controlpanel.FileProtocolNFS, NFS: &controlpanel.NFSChange{NFSv4Domain: &domain},
	}); err == nil || !strings.Contains(err.Error(), "verified NFS") {
		t.Fatalf("advanced NFS plan error = %v", err)
	}

	enabled := true
	if _, err := planFileServiceChangeWithClient(context.Background(), "lab", client, controlpanel.FileServiceChangeRequest{
		Protocol: controlpanel.FileProtocolNFS, NFS: &controlpanel.NFSChange{Enabled: &enabled},
	}); err == nil || !strings.Contains(err.Error(), "would not change") {
		t.Fatalf("NFS no-op plan error = %v", err)
	}

	maximum := controlpanel.NFSProtocol4_1
	if _, err := planFileServiceChangeWithClient(context.Background(), "lab", client, controlpanel.FileServiceChangeRequest{
		Protocol: controlpanel.FileProtocolNFS, NFS: &controlpanel.NFSChange{MaximumProtocol: &maximum},
	}); err == nil || !strings.Contains(err.Error(), "not advertised") {
		t.Fatalf("unsupported NFS protocol error = %v", err)
	}
}

func TestFileServiceNFSDomainPlanApply(t *testing.T) {
	client := &fakeFileServiceClient{
		smb: testSMBState(),
		nfs: synology.NFSState{
			Enabled: true, MaximumProtocol: controlpanel.NFSProtocol4_1,
			SupportedProtocols: []controlpanel.NFSProtocol{controlpanel.NFSProtocol2, controlpanel.NFSProtocol3, controlpanel.NFSProtocol4, controlpanel.NFSProtocol4_1},
			NFSv4Domain:        "old.example",
		},
		setAdvanced: true,
	}
	domain := "new.example"
	request := controlpanel.FileServiceChangeRequest{
		Protocol: controlpanel.FileProtocolNFS,
		NFS:      &controlpanel.NFSChange{NFSv4Domain: &domain},
	}
	plan, err := planFileServiceChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("planFileServiceChangeWithClient() error = %v", err)
	}
	if plan.Observed.NFS == nil || plan.Risk != "high" {
		t.Fatalf("domain plan = %#v", plan)
	}

	result, err := applyFileServicePlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("applyFileServicePlanWithClient() error = %v", err)
	}
	if !result.Applied || client.nfs.NFSv4Domain != "new.example" || client.mutations != 1 {
		t.Fatalf("apply result/client = %#v %#v", result, client.nfs)
	}
}

func TestFileServiceNFSDomainRequiresAdvancedBackend(t *testing.T) {
	client := &fakeFileServiceClient{
		nfs: synology.NFSState{
			Enabled: true, MaximumProtocol: controlpanel.NFSProtocol4,
			SupportedProtocols: []controlpanel.NFSProtocol{controlpanel.NFSProtocol2, controlpanel.NFSProtocol3, controlpanel.NFSProtocol4},
			NFSv4Domain:        "old.example",
		},
		setAdvanced: false,
	}
	domain := "new.example"
	if _, err := planFileServiceChangeWithClient(context.Background(), "lab", client, controlpanel.FileServiceChangeRequest{
		Protocol: controlpanel.FileProtocolNFS, NFS: &controlpanel.NFSChange{NFSv4Domain: &domain},
	}); err == nil || !strings.Contains(err.Error(), "verified NFS") {
		t.Fatalf("advanced gating error = %v", err)
	}
}

func TestFileServicePlanHashRejectsTampering(t *testing.T) {
	client := &fakeFileServiceClient{smb: testSMBState()}
	encryption := controlpanel.SMBPolicyRequired
	plan, err := planFileServiceChangeWithClient(context.Background(), "lab", client, controlpanel.FileServiceChangeRequest{
		Protocol: controlpanel.FileProtocolSMB, SMB: &controlpanel.SMBChange{TransportEncryption: &encryption},
	})
	if err != nil {
		t.Fatalf("planFileServiceChangeWithClient() error = %v", err)
	}
	plan.Risk = "low"
	if err := validateFileServicePlan(plan, plan.Hash); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("tampered plan error = %v", err)
	}
}

func testSMBState() synology.SMBState {
	return synology.SMBState{
		Enabled: true, Workgroup: "WORKGROUP",
		MinimumProtocol: controlpanel.SMBProtocol2, MaximumProtocol: controlpanel.SMBProtocol3,
		TransportEncryption: controlpanel.SMBPolicyAutomatic, ServerSigning: controlpanel.SMBSigningDisabledForSMB1,
	}
}
