package fileservices

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type captureExecutor struct {
	responses map[string]json.RawMessage
	requests  []compatibility.Request
}

func (executor *captureExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	executor.requests = append(executor.requests, request)
	if response, ok := executor.responses[request.API+"."+request.Method]; ok {
		return response, nil
	}
	return json.RawMessage(`{}`), nil
}

func TestSMBV3ReadAndSetContract(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(SMBAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 3})
	executor := &captureExecutor{responses: map[string]json.RawMessage{
		SMBAPIName + ".get": json.RawMessage(`{
			"enable_samba":true,"workgroup":"WORKGROUP","smb_min_protocol":1,
			"smb_max_protocol":3,"smb_encrypt_transport":1,"enable_server_signing":0
		}`),
	}}

	state, selection, err := ExecuteSMBRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteSMBRead() error = %v", err)
	}
	if selection.Backend != "core-fileserv-smb-v3" || state.MinimumProtocol != controlpanel.SMBProtocol2 || state.ServerSigning != controlpanel.SMBSigningDisabledForSMB1 {
		t.Fatalf("SMB selection/state = %#v %#v", selection, state)
	}

	enabled := true
	workgroup := "LAB"
	minimum := controlpanel.SMBProtocol2
	maximum := controlpanel.SMBProtocol3
	encryption := controlpanel.SMBPolicyRequired
	signing := controlpanel.SMBSigningRequired
	_, selection, err = ExecuteSMBSet(context.Background(), target, executor, controlpanel.SMBChange{
		Enabled: &enabled, Workgroup: &workgroup, MinimumProtocol: &minimum,
		MaximumProtocol: &maximum, TransportEncryption: &encryption, ServerSigning: &signing,
	})
	if err != nil {
		t.Fatalf("ExecuteSMBSet() error = %v", err)
	}
	want := map[string]any{
		"enable_samba": true, "workgroup": "LAB", "smb_min_protocol": 1,
		"smb_max_protocol": 3, "smb_encrypt_transport": 2, "enable_server_signing": 2,
	}
	request := executor.requests[len(executor.requests)-1]
	if selection.Version != 3 || request.Method != "set" || !reflect.DeepEqual(request.JSONParameters, want) {
		t.Fatalf("SMB set request = %#v, want parameters %#v", request, want)
	}
}

func TestNFSV3ReadBaseSetAndAdvancedReadContract(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(NFSAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 3})
	target.SetAPI(NFSAdvancedAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	executor := &captureExecutor{responses: map[string]json.RawMessage{
		NFSAPIName + ".get": json.RawMessage(`{
			"enable_nfs":true,"enable_nfs_v4":true,"enabled_minor_ver":1,
			"support_major_ver":4,"support_minor_ver":1
		}`),
		NFSAdvancedAPIName + ".get": json.RawMessage(`{"nfs_v4_domain":"lab.example"}`),
	}}

	state, selection, err := ExecuteNFSRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteNFSRead() error = %v", err)
	}
	if selection.Version != 3 || state.MaximumProtocol != controlpanel.NFSProtocol4_1 || !reflect.DeepEqual(state.SupportedProtocols, []controlpanel.NFSProtocol{controlpanel.NFSProtocol2, controlpanel.NFSProtocol3, controlpanel.NFSProtocol4, controlpanel.NFSProtocol4_1}) {
		t.Fatalf("NFS selection/state = %#v %#v", selection, state)
	}
	advanced, _, err := ExecuteNFSAdvancedRead(context.Background(), target, executor)
	if err != nil || advanced.Domain != "lab.example" {
		t.Fatalf("ExecuteNFSAdvancedRead() = %#v, %v", advanced, err)
	}

	enabled := false
	maximum := controlpanel.NFSProtocol4
	_, _, err = ExecuteNFSSet(context.Background(), target, executor, controlpanel.NFSChange{Enabled: &enabled, MaximumProtocol: &maximum})
	if err != nil {
		t.Fatalf("ExecuteNFSSet() error = %v", err)
	}
	want := map[string]any{
		"enable_nfs": false, "nfs_max_protocol": 1,
		"enable_nfs_v4": true, "enabled_minor_ver": 0,
	}
	request := executor.requests[len(executor.requests)-1]
	if request.Method != "set" || !reflect.DeepEqual(request.JSONParameters, want) {
		t.Fatalf("NFS set request = %#v, want parameters %#v", request, want)
	}
}

func TestNFSAdvancedSetFailsClosed(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(NFSAdvancedAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	selection, err := SelectNFSAdvancedSet(target)
	if err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectNFSAdvancedSet() = %#v, %v", selection, err)
	}
}

func TestDecodersRejectMissingModernFields(t *testing.T) {
	if _, err := decodeSMB(json.RawMessage(`{"enable_samba":true,"workgroup":"LAB"}`), true); err == nil {
		t.Fatal("decodeSMB() accepted missing v3 fields")
	}
	if _, err := decodeNFS(json.RawMessage(`{"enable_nfs":true}`), true); err == nil {
		t.Fatal("decodeNFS() accepted missing v2+ fields")
	}
}
