package utilizationread

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/resmon"
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

func TestCurrentRequestAndDecode(t *testing.T) {
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API != APIName || request.Version != 1 || request.Method != "get" {
			t.Fatalf("request = %#v", request)
		}
		if request.JSONParameters != nil {
			t.Fatalf("current snapshot must not send mode parameters: %#v", request.JSONParameters)
		}
		return fixture(t, "current-v1.json"), nil
	})
	state, selection, err := ExecuteCurrent(context.Background(), supportedTarget(), executor)
	if err != nil || selection.Operation != OperationCurrent || !selection.Supported {
		t.Fatalf("state=%#v selection=%#v err=%v", state, selection, err)
	}
	if !state.RecordingEnabled {
		t.Fatalf("RecordingEnabled = false, want true")
	}
	wantCPU := resmon.CPUUtilization{Device: "System", UserPercent: 15, SystemPercent: 7, OtherPercent: 3, LoadAverage1: 88, LoadAverage5: 60, LoadAverage15: 42}
	if state.CPU != wantCPU {
		t.Fatalf("cpu = %#v, want %#v", state.CPU, wantCPU)
	}
	if state.Memory.RealUsagePercent != 38 || state.Memory.TotalRealBytes != int64(8267520)*kbToBytes || state.Memory.CachedBytes != int64(2097152)*kbToBytes {
		t.Fatalf("memory = %#v", state.Memory)
	}
	if len(state.Network) != 2 || state.Network[0] != (resmon.NetworkInterface{Device: "total", TxBytesPerSec: 20480, RxBytesPerSec: 40960}) {
		t.Fatalf("network = %#v", state.Network)
	}
	if state.Disk.Total.WriteBytesPerSec != 1048576 || state.Disk.Total.UtilizationPercent != 22 || len(state.Disk.Disks) != 2 {
		t.Fatalf("disk = %#v", state.Disk)
	}
	if state.Disk.Disks[0].Device != "sda" || state.Disk.Disks[0].DisplayName != "Disk 1" || state.Disk.Disks[0].ReadOpsPerSec != 6 {
		t.Fatalf("disk[0] = %#v", state.Disk.Disks[0])
	}
	if len(state.Volumes) != 1 || state.Volumes[0].Device != "volume1" || state.Volumes[0].WriteBytesPerSec != 262144 {
		t.Fatalf("volumes = %#v", state.Volumes)
	}
}

func TestHistoryRequestAndDecode(t *testing.T) {
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		want := map[string]any{"type": "history", "time_range": resmon.PeriodWeek, "resource": []string{"cpu", "space"}}
		if request.API != APIName || request.Version != 1 || request.Method != "get" {
			t.Fatalf("request = %#v", request)
		}
		if !reflect.DeepEqual(request.JSONParameters, want) {
			t.Fatalf("parameters = %#v, want %#v", request.JSONParameters, want)
		}
		return fixture(t, "history-v1.json"), nil
	})
	history, selection, err := ExecuteHistory(context.Background(), supportedTarget(), executor, HistoryInput{Period: resmon.PeriodWeek, Resources: []string{"cpu", "space"}})
	if err != nil || selection.Operation != OperationHistory || !selection.Supported {
		t.Fatalf("history=%#v selection=%#v err=%v", history, selection, err)
	}
	if history.Period != resmon.PeriodWeek {
		t.Fatalf("period = %q, want %q", history.Period, resmon.PeriodWeek)
	}
	// cpu: system_load + user_load, memory: real_usage, network eth0: rx + tx,
	// space volume1: utilization = 6 series total.
	if len(history.Series) != 6 {
		t.Fatalf("series count = %d (%#v)", len(history.Series), history.Series)
	}
	var cpuUser resmon.HistorySeries
	for _, series := range history.Series {
		if series.Dimension == resmon.DimensionCPU && series.Metric == "user_load" {
			cpuUser = series
		}
	}
	want := resmon.HistorySeries{
		Dimension: resmon.DimensionCPU,
		Metric:    "user_load",
		Values:    []float64{10, 12, 15},
	}
	if !reflect.DeepEqual(cpuUser, want) {
		t.Fatalf("cpu user_load series = %#v, want %#v", cpuUser, want)
	}
	for _, series := range history.Series {
		if series.Dimension == resmon.DimensionNetwork && series.Device != "eth0" {
			t.Fatalf("network series device = %q, want eth0", series.Device)
		}
		if series.Dimension == resmon.DimensionVolume && series.Device != "volume1" {
			t.Fatalf("volume series device = %q, want volume1", series.Device)
		}
	}
}

func TestDecodeRejectsMalformed(t *testing.T) {
	if _, err := decodeUtilization(json.RawMessage(`[]`)); err == nil {
		t.Fatal("expected error for non-object utilization response")
	}
	if _, err := decodeUtilization(json.RawMessage(`{"network":[]}`)); err == nil {
		t.Fatal("expected error when neither cpu nor memory is present")
	}
	if _, err := decodeHistory(json.RawMessage(`{"foo":1}`), HistoryInput{}); err == nil {
		t.Fatal("expected error when history has no recognized series")
	}
}

func TestSelectUnsupportedWithoutAPI(t *testing.T) {
	if selection, err := SelectCurrent(compatibility.NewTarget()); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("expected unsupported current selection, got %#v err=%v", selection, err)
	}
	if selection, err := SelectHistory(supportedTarget()); err != nil || !selection.Supported || selection.Operation != OperationHistory {
		t.Fatalf("expected supported history selection, got %#v err=%v", selection, err)
	}
}
