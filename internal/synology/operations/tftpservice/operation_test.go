package tftpservice

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

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

func TestReadDecodesLiveFieldNames(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(APIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	executor := &captureExecutor{responses: map[string]json.RawMessage{
		// Field names as returned live by DSM 7.3.2 (same as the set names).
		APIName + ".get": json.RawMessage(`{
			"enable":true,"root_path":"/volume1/tftp","permission":"rw",
			"enable_log":false,"startip":"10.0.0.1","endip":"10.0.0.9","timeout":10
		}`),
	}}

	settings, selection, err := ExecuteRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteRead() error = %v", err)
	}
	if selection.Backend != "core-tftp-v1" || !settings.Enabled || settings.RootPath != "/volume1/tftp" ||
		!settings.AllowWrite || settings.LogEnabled || settings.ClientIPLow != "10.0.0.1" ||
		settings.ClientIPHigh != "10.0.0.9" || settings.Timeout != 10 {
		t.Fatalf("TFTP settings = %#v", settings)
	}
	// The read is a plain get (the live API returns the full config without an
	// "additional" selector).
	getRequest := executor.requests[0]
	if getRequest.Method != "get" || getRequest.JSONParameters != nil {
		t.Fatalf("TFTP get request = %#v, want a plain get", getRequest)
	}
}

func TestPartialSetUsesSetFieldNames(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(APIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	executor := &captureExecutor{}

	logEnabled := true
	timeout := 30
	allowWrite := false
	_, _, err := ExecuteSet(context.Background(), target, executor, Patch{LogEnabled: &logEnabled, Timeout: &timeout, AllowWrite: &allowWrite})
	if err != nil {
		t.Fatalf("ExecuteSet() error = %v", err)
	}
	// Only changed fields are sent, using the set-side names (enable_log, permission).
	want := map[string]any{"enable_log": true, "timeout": 30, "permission": "r"}
	request := executor.requests[len(executor.requests)-1]
	if request.Method != "set" || !reflect.DeepEqual(request.JSONParameters, want) {
		t.Fatalf("TFTP set request = %#v, want %#v", request, want)
	}
}

func TestSetSelectionFailsClosedAndEmptyPatchRejected(t *testing.T) {
	withoutAPI := compatibility.NewTarget()
	selection, err := SelectSet(withoutAPI)
	if err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectSet() without API = %#v, %v", selection, err)
	}
	withAPI := compatibility.NewTarget()
	withAPI.SetAPI(APIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	if _, _, err := ExecuteSet(context.Background(), withAPI, &captureExecutor{}, Patch{}); err == nil {
		t.Fatal("ExecuteSet() accepted an empty patch")
	}
}

func TestDecodeRejectsMissingEnableAndBadPermission(t *testing.T) {
	if _, err := decodeSettings(json.RawMessage(`{"root_path":"/x"}`)); err == nil {
		t.Fatal("decodeSettings() accepted a response missing enable")
	}
	if _, err := decodeSettings(json.RawMessage(`{"enable":true,"permission":"weird"}`)); err == nil {
		t.Fatal("decodeSettings() accepted an unknown permission value")
	}
}
