package resmonsettingmutation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type executorFunc func(context.Context, compatibility.Request) (json.RawMessage, error)

func (function executorFunc) Execute(ctx context.Context, request compatibility.Request) (json.RawMessage, error) {
	return function(ctx, request)
}

func supportedTarget() compatibility.Target {
	target := compatibility.NewTarget()
	target.SetAPI(APIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1, RequestFormat: "JSON"})
	return target
}

func TestSetSubmitsMergedObject(t *testing.T) {
	desired := map[string]any{RecordingField: true, "enable_event": false, "event_cpu": 90}
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API != APIName || request.Version != 1 || request.Method != "set" {
			t.Fatalf("request = %#v", request)
		}
		if !reflect.DeepEqual(request.JSONParameters, desired) {
			t.Fatalf("parameters = %#v, want %#v", request.JSONParameters, desired)
		}
		return json.RawMessage(`{}`), nil
	})
	result, selection, err := ExecuteSet(context.Background(), supportedTarget(), executor, desired)
	if err != nil || !selection.Supported {
		t.Fatalf("result=%#v selection=%#v err=%v", result, selection, err)
	}
	if result.Method != "set" || result.API != APIName || result.Version != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestSetRejectsMissingRecordingField(t *testing.T) {
	executor := executorFunc(func(_ context.Context, _ compatibility.Request) (json.RawMessage, error) {
		t.Fatal("executor must not be called when the recording field is absent")
		return nil, nil
	})
	if _, _, err := ExecuteSet(context.Background(), supportedTarget(), executor, map[string]any{"enable_event": true}); err == nil {
		t.Fatal("expected error when enable_history is absent")
	}
}

func TestSetPropagatesAPIError(t *testing.T) {
	sentinel := errors.New("dsm rejected")
	executor := executorFunc(func(_ context.Context, _ compatibility.Request) (json.RawMessage, error) {
		return nil, sentinel
	})
	if _, _, err := ExecuteSet(context.Background(), supportedTarget(), executor, map[string]any{RecordingField: false}); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want wrapped sentinel", err)
	}
}

func TestSelectSetUnsupportedWithoutAPI(t *testing.T) {
	if selection, err := SelectSet(compatibility.NewTarget()); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("expected unsupported selection, got %#v err=%v", selection, err)
	}
	if selection, err := SelectSet(supportedTarget()); err != nil || !selection.Supported || selection.Operation != SetOperationName {
		t.Fatalf("expected supported selection, got %#v err=%v", selection, err)
	}
}
