package servicediscovery

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

func newTarget() compatibility.Target {
	target := compatibility.NewTarget()
	target.SetAPI(ServiceDiscoveryAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	target.SetAPI(WSTransferAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	return target
}

func TestReadDecodesTimeMachineAndWSDiscovery(t *testing.T) {
	target := newTarget()
	executor := &captureExecutor{responses: map[string]json.RawMessage{
		ServiceDiscoveryAPIName + ".get": json.RawMessage(`{"enable_smb_time_machine":true,"enable_afp_time_machine":false}`),
		WSTransferAPIName + ".get":       json.RawMessage(`{"enable_wstransfer":1}`),
	}}

	tm, selection, err := ExecuteTimeMachineRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteTimeMachineRead() error = %v", err)
	}
	if selection.Backend != "core-fileserv-servicediscovery-v1" || !tm.SMB || tm.AFP {
		t.Fatalf("time machine = %#v (selection %#v)", tm, selection)
	}
	ws, _, err := ExecuteWSDiscoveryRead(context.Background(), target, executor)
	if err != nil || !ws {
		t.Fatalf("ws discovery = %v, %v", ws, err)
	}
}

func TestTimeMachineSetRequestShape(t *testing.T) {
	target := newTarget()
	executor := &captureExecutor{}
	if _, _, err := ExecuteTimeMachineSet(context.Background(), target, executor, TimeMachine{SMB: true, AFP: false}); err != nil {
		t.Fatalf("ExecuteTimeMachineSet() error = %v", err)
	}
	request := executor.requests[len(executor.requests)-1]
	want := map[string]any{"enable_smb_time_machine": true, "enable_afp_time_machine": false}
	if request.API != ServiceDiscoveryAPIName || request.Method != "set" || !reflect.DeepEqual(request.JSONParameters, want) {
		t.Fatalf("time machine set request = %#v, want %#v", request, want)
	}
}

func TestWSDiscoverySetRequestShape(t *testing.T) {
	target := newTarget()
	executor := &captureExecutor{}
	if _, _, err := ExecuteWSDiscoverySet(context.Background(), target, executor, true); err != nil {
		t.Fatalf("ExecuteWSDiscoverySet() error = %v", err)
	}
	request := executor.requests[len(executor.requests)-1]
	want := map[string]any{"enable_wstransfer": true}
	if request.API != WSTransferAPIName || request.Method != "set" || !reflect.DeepEqual(request.JSONParameters, want) {
		t.Fatalf("ws discovery set request = %#v, want %#v", request, want)
	}
}

func TestDecodeRejectsMissingFields(t *testing.T) {
	if _, err := decodeTimeMachine(json.RawMessage(`{"enable_smb_time_machine":true}`)); err == nil {
		t.Fatal("decodeTimeMachine() accepted a response missing enable_afp_time_machine")
	}
	if _, err := decodeWSDiscovery(json.RawMessage(`{}`)); err == nil {
		t.Fatal("decodeWSDiscovery() accepted a response missing enable_wstransfer")
	}
}

func TestSelectionFailsClosedWithoutAPIs(t *testing.T) {
	empty := compatibility.NewTarget()
	if selection, err := SelectTimeMachineSet(empty); err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectTimeMachineSet() = %#v, %v", selection, err)
	}
	if selection, err := SelectWSDiscoverySet(empty); err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectWSDiscoverySet() = %#v, %v", selection, err)
	}
}
