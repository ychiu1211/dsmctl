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

func TestSMBAdvancedFieldsReadAndSetContract(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(SMBAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 3})
	executor := &captureExecutor{responses: map[string]json.RawMessage{
		SMBAPIName + ".get": json.RawMessage(`{
			"enable_samba":true,"workgroup":"WORKGROUP","smb_min_protocol":1,
			"smb_max_protocol":3,"smb_encrypt_transport":1,"enable_server_signing":0,
			"enable_op_lock":true,"enable_smb2_leases":1,"enable_durable_handles":false,
			"enable_local_master_browser":0
		}`),
	}}

	state, _, err := ExecuteSMBRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteSMBRead() error = %v", err)
	}
	if !state.OpportunisticLocking || !state.SMB2Leases || state.DurableHandles || state.LocalMasterBrowser {
		t.Fatalf("SMB advanced state = %#v", state)
	}

	oplock := false
	master := true
	_, _, err = ExecuteSMBSet(context.Background(), target, executor, controlpanel.SMBChange{
		OpportunisticLocking: &oplock, LocalMasterBrowser: &master,
	})
	if err != nil {
		t.Fatalf("ExecuteSMBSet() error = %v", err)
	}
	want := map[string]any{"enable_op_lock": false, "enable_local_master_browser": true}
	request := executor.requests[len(executor.requests)-1]
	if request.Method != "set" || !reflect.DeepEqual(request.JSONParameters, want) {
		t.Fatalf("SMB advanced set request = %#v, want %#v", request, want)
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

func TestNFSAdvancedSetSelectionAndFailClosed(t *testing.T) {
	withAPI := compatibility.NewTarget()
	withAPI.SetAPI(NFSAdvancedAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	selection, err := SelectNFSAdvancedSet(withAPI)
	if err != nil || !selection.Supported || selection.Backend != "core-fileserv-nfs-advanced-v1" {
		t.Fatalf("SelectNFSAdvancedSet() with API = %#v, %v", selection, err)
	}

	withoutAPI := compatibility.NewTarget()
	selection, err = SelectNFSAdvancedSet(withoutAPI)
	if err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectNFSAdvancedSet() without API = %#v, %v", selection, err)
	}
}

func TestNFSAdvancedSnapshotReadAndDomainSetContract(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(NFSAdvancedAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	// The real DSM AdvancedSetting get response omits enable_nfs (a base-service
	// field) and returns custom_port_enable as an integer, not a boolean; the
	// write must supply enable_nfs and re-encode custom_port_enable as a bool.
	executor := &captureExecutor{responses: map[string]json.RawMessage{
		NFSAdvancedAPIName + ".get": json.RawMessage(`{
			"custom_port_enable":0,"read_size":8192,"write_size":16384,
			"unix_pri_enable":true,"statd_port":0,"nlm_port":0,"nfs_v4_domain":"old.example"
		}`),
	}}

	snapshot, selection, err := ExecuteNFSAdvancedSnapshotRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteNFSAdvancedSnapshotRead() error = %v", err)
	}
	if selection.Backend != "core-fileserv-nfs-advanced-v1" || snapshot.Domain != "old.example" || snapshot.CustomPortEnable || snapshot.ReadSize != 8192 || snapshot.WriteSize != 16384 || !snapshot.UnixPermissions {
		t.Fatalf("snapshot = %#v (selection %#v)", snapshot, selection)
	}

	// The AdvancedSetting set requires enable_nfs even though get omits it; the
	// facade supplies the current base service state so the write preserves it.
	snapshot.Domain = "new.example"
	snapshot.EnableNFS = true
	if _, _, err := ExecuteNFSAdvancedSet(context.Background(), target, executor, snapshot); err != nil {
		t.Fatalf("ExecuteNFSAdvancedSet() error = %v", err)
	}
	request := executor.requests[len(executor.requests)-1]
	if request.API != NFSAdvancedAPIName || request.Method != "set" {
		t.Fatalf("advanced set request = %#v", request)
	}
	// Normalize through JSON to compare regardless of the raw-passthrough types.
	encoded, err := json.Marshal(request.JSONParameters)
	if err != nil {
		t.Fatalf("marshal set parameters: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("unmarshal set parameters: %v", err)
	}
	want := map[string]any{
		"enable_nfs": true,
		"custom_port_enable": false, "read_size": float64(8192), "write_size": float64(16384),
		"unix_pri_enable": true, "statd_port": float64(0), "nlm_port": float64(0),
		"nfs_v4_domain": "new.example",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("advanced set parameters = %#v, want %#v", got, want)
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
