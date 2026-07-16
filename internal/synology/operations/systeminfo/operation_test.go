package systeminfo

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type executorFunc func(context.Context, compatibility.Request) (json.RawMessage, error)

func (function executorFunc) Execute(ctx context.Context, request compatibility.Request) (json.RawMessage, error) {
	return function(ctx, request)
}

func TestCurrentVariantSelectsV3AndNormalizesResponse(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(APIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	info, selection, err := Execute(context.Background(), target, executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API != APIName || request.Version != 3 || request.Method != "info" {
			t.Fatalf("request = %#v", request)
		}
		return fixture(t, "testdata/current-v3.json"), nil
	}))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if selection.Backend != "core-system-v3" || selection.Version != 3 {
		t.Fatalf("selection = %#v", selection)
	}
	if info.Hostname != "office" || info.MemoryMiB != 4096 || info.CPU != "AMD R1600" {
		t.Fatalf("info = %#v", info)
	}
}

func TestLegacyVariantSelectsV1AndNormalizesAliases(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(APIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	info, selection, err := Execute(context.Background(), target, executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.Version != 1 {
			t.Fatalf("request version = %d", request.Version)
		}
		return fixture(t, "testdata/legacy-v1.json"), nil
	}))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if selection.Backend != "core-system-v1-legacy" || selection.Version != 1 {
		t.Fatalf("selection = %#v", selection)
	}
	if info.Hostname != "legacy" || info.DSMVersion != "DSM 6.2.4" || info.MemoryMiB != 2048 || info.TemperatureC == nil || *info.TemperatureC != 44 {
		t.Fatalf("info = %#v", info)
	}
}

func fixture(t *testing.T, path string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return data
}
