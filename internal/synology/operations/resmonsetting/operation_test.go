package resmonsetting

import (
	"context"
	"encoding/json"
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

func TestGetRequestAndDecode(t *testing.T) {
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API != APIName || request.Version != 1 || request.Method != "get" {
			t.Fatalf("request = %#v", request)
		}
		return json.RawMessage(`{"enable_history": true, "enable_event": false, "event_cpu": 90}`), nil
	})
	settings, selection, err := Execute(context.Background(), supportedTarget(), executor)
	if err != nil || selection.Operation != OperationName || !selection.Supported {
		t.Fatalf("settings=%#v selection=%#v err=%v", settings, selection, err)
	}
	if !settings.Recording.Enabled {
		t.Fatalf("Enabled = false, want true")
	}
	// Raw must preserve co-located fields so a patch can round-trip them.
	if _, ok := settings.Raw["enable_event"]; !ok {
		t.Fatalf("Raw dropped enable_event: %#v", settings.Raw)
	}
}

func TestGetDecodeNumericBool(t *testing.T) {
	executor := executorFunc(func(_ context.Context, _ compatibility.Request) (json.RawMessage, error) {
		return json.RawMessage(`{"enable_history": 0}`), nil
	})
	settings, _, err := Execute(context.Background(), supportedTarget(), executor)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if settings.Recording.Enabled {
		t.Fatalf("Enabled = true, want false for enable_history=0")
	}
}

func TestGetRejectsMissingField(t *testing.T) {
	executor := executorFunc(func(_ context.Context, _ compatibility.Request) (json.RawMessage, error) {
		return json.RawMessage(`{"enable_event": true}`), nil
	})
	if _, _, err := Execute(context.Background(), supportedTarget(), executor); err == nil {
		t.Fatal("expected error when enable_history is absent")
	}
}

func TestSelectUnsupportedWithoutAPI(t *testing.T) {
	if selection, err := Select(compatibility.NewTarget()); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("expected unsupported selection, got %#v err=%v", selection, err)
	}
	if selection, err := Select(supportedTarget()); err != nil || !selection.Supported || selection.Operation != OperationName {
		t.Fatalf("expected supported selection, got %#v err=%v", selection, err)
	}
}
