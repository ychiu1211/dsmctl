package rsyncservice

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

func TestReadAndSetContract(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(APIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	executor := &captureExecutor{responses: map[string]json.RawMessage{
		// Live DSM 7.3.2 get returns "enable" (same key as set) and the port as a string.
		APIName + ".get": json.RawMessage(`{"enable":true,"enable_rsync_account":false,"rsync_sshd_port":"22","enable_custom_config":false}`),
	}}

	settings, selection, err := ExecuteRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteRead() error = %v", err)
	}
	if selection.Backend != "backup-service-networkbackup-v1" || !settings.Enabled || settings.RsyncAccount || settings.SSHPort != 22 {
		t.Fatalf("rsync selection/settings = %#v %#v", selection, settings)
	}

	_, _, err = ExecuteSet(context.Background(), target, executor, Settings{Enabled: false, RsyncAccount: true})
	if err != nil {
		t.Fatalf("ExecuteSet() error = %v", err)
	}
	// set uses the "enable" key (never "enable_rsync") and always carries the account switch.
	want := map[string]any{"enable": false, "enable_rsync_account": true}
	request := executor.requests[len(executor.requests)-1]
	if request.Method != "set" || !reflect.DeepEqual(request.JSONParameters, want) {
		t.Fatalf("rsync set request = %#v, want parameters %#v", request, want)
	}
}

func TestSetSelectionFailsClosed(t *testing.T) {
	withoutAPI := compatibility.NewTarget()
	selection, err := SelectSet(withoutAPI)
	if err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectSet() without API = %#v, %v", selection, err)
	}
}

func TestDecodeRejectsMissingFields(t *testing.T) {
	if _, err := decodeSettings(json.RawMessage(`{"enable":true}`)); err == nil {
		t.Fatal("decodeSettings() accepted a response missing enable_rsync_account")
	}
}
