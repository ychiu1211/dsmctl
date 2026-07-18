package ftpservices

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

func TestFTPReadAndSetContract(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(FTPAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 3})
	executor := &captureExecutor{responses: map[string]json.RawMessage{
		// DSM returns both switches as booleans on get.
		FTPAPIName + ".get": json.RawMessage(`{"enable_ftp":true,"enable_ftps":false}`),
	}}

	settings, selection, err := ExecuteFTPRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteFTPRead() error = %v", err)
	}
	if selection.Backend != "core-fileserv-ftp-v3" || !settings.Plain || settings.FTPS {
		t.Fatalf("FTP selection/settings = %#v %#v", selection, settings)
	}

	_, selection, err = ExecuteFTPSet(context.Background(), target, executor, FTPSettings{Plain: false, FTPS: true})
	if err != nil {
		t.Fatalf("ExecuteFTPSet() error = %v", err)
	}
	// DSM requires both switches on every set, so both are always sent.
	want := map[string]any{"enable_ftp": false, "enable_ftps": true}
	request := executor.requests[len(executor.requests)-1]
	if selection.Version != 3 || request.Method != "set" || !reflect.DeepEqual(request.JSONParameters, want) {
		t.Fatalf("FTP set request = %#v, want parameters %#v", request, want)
	}
}

func TestSFTPReadAndSetContract(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(SFTPAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	executor := &captureExecutor{responses: map[string]json.RawMessage{
		FTPAPIName + ".get":  json.RawMessage(`{"enable_ftp":false,"enable_ftps":false}`),
		SFTPAPIName + ".get": json.RawMessage(`{"enable":true,"portnum":22}`),
	}}

	settings, selection, err := ExecuteSFTPRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteSFTPRead() error = %v", err)
	}
	if selection.Backend != "core-fileserv-sftp-v1" || !settings.Enabled || settings.Port != 22 {
		t.Fatalf("SFTP selection/settings = %#v %#v", selection, settings)
	}

	_, selection, err = ExecuteSFTPSet(context.Background(), target, executor, SFTPSettings{Enabled: false, Port: 2222})
	if err != nil {
		t.Fatalf("ExecuteSFTPSet() error = %v", err)
	}
	// The enable switch is required and the port is always sent to preserve it.
	want := map[string]any{"enable": false, "portnum": 2222}
	request := executor.requests[len(executor.requests)-1]
	if selection.Version != 1 || request.Method != "set" || !reflect.DeepEqual(request.JSONParameters, want) {
		t.Fatalf("SFTP set request = %#v, want parameters %#v", request, want)
	}
}

func TestFTPSetSelectionFailsClosed(t *testing.T) {
	withoutAPI := compatibility.NewTarget()
	selection, err := SelectFTPSet(withoutAPI)
	if err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectFTPSet() without API = %#v, %v", selection, err)
	}
	selection, err = SelectSFTPSet(withoutAPI)
	if err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectSFTPSet() without API = %#v, %v", selection, err)
	}
}

func TestDecodersRejectMissingFields(t *testing.T) {
	if _, err := decodeFTP(json.RawMessage(`{"enable_ftp":true}`)); err == nil {
		t.Fatal("decodeFTP() accepted a response missing enable_ftps")
	}
	if _, err := decodeSFTP(json.RawMessage(`{"enable":true}`)); err == nil {
		t.Fatal("decodeSFTP() accepted a response missing portnum")
	}
}
