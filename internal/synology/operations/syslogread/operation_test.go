package syslogread

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/syslog"
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

func fixture(t *testing.T, name string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestListRequestAndDecode(t *testing.T) {
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		want := map[string]any{"start": 20, "limit": 50, "keyword": "cache", "logtype": "system", "date_from": int64(1700000000), "date_to": int64(1700086400)}
		if request.API != APIName || request.Version != 1 || request.Method != "list" {
			t.Fatalf("request = %#v", request)
		}
		if !reflect.DeepEqual(request.JSONParameters, want) {
			t.Fatalf("parameters = %#v, want %#v", request.JSONParameters, want)
		}
		return fixture(t, "list-v1.json"), nil
	})
	state, selection, err := Execute(context.Background(), supportedTarget(), executor, Input{Limit: 50, Offset: 20, Keyword: "cache", LogType: "system", DateFrom: 1700000000, DateTo: 1700086400})
	if err != nil || selection.Operation != OperationName || !selection.Supported {
		t.Fatalf("state=%#v selection=%#v err=%v", state, selection, err)
	}
	if state.Total != 357 || state.InfoCount != 300 || state.WarnCount != 56 || state.ErrorCount != 1 {
		t.Fatalf("counts = %#v", state)
	}
	want := []syslog.Entry{
		{Time: "2026/07/17 13:35:55", Level: syslog.LevelInfo, Type: "system", Who: "deryck", Message: "System successfully removed the SSD cache from [Volume 1]."},
		{Time: "2026/07/17 13:00:12", Level: syslog.LevelInfo, Type: "connection", Who: "deryck", Message: "User [deryck] from [10.17.36.69] signed in to DSM successfully via [DSM]."},
		{Time: "2026/07/16 09:14:03", Level: syslog.LevelWarning, Type: "system", Who: "system", Message: "Volume [1] has entered read-only mode."},
	}
	if !reflect.DeepEqual(state.Entries, want) {
		t.Fatalf("entries = %#v, want %#v", state.Entries, want)
	}
}

func TestListOmitsEmptyFilters(t *testing.T) {
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		want := map[string]any{"start": 0, "limit": 10}
		if !reflect.DeepEqual(request.JSONParameters, want) {
			t.Fatalf("parameters = %#v, want %#v", request.JSONParameters, want)
		}
		return json.RawMessage(`{"total":0,"items":[]}`), nil
	})
	if _, _, err := Execute(context.Background(), supportedTarget(), executor, Input{Limit: 10}); err != nil {
		t.Fatalf("Execute() error = %v", err)
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
