package securityadvisor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/securityadvisor"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

func liveConfiguration() securityadvisor.Configuration {
	return securityadvisor.Configuration{
		Baseline: securityadvisor.BaselineCompany,
		Schedule: securityadvisor.Schedule{Enabled: true, Hour: 2, Minute: 21, Weekday: "4", TaskID: 2},
	}
}

// TestExecuteConfigurationSetRequestShape locks the live-verified Conf.set wire:
// the fields ride inside a single top-level `Input` object, the baseline is sent
// as argGroup, and the scalar types match what DSM accepts.
func TestExecuteConfigurationSetRequestShape(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{"scheduleTaskId":2,"success":true}`)}
	result, selection, err := ExecuteConfigurationSet(context.Background(), saTarget(), executor, liveConfiguration())
	if err != nil {
		t.Fatalf("ExecuteConfigurationSet() error = %v", err)
	}
	if !selection.Supported || selection.Backend != "securityscan-conf-set-v1" {
		t.Fatalf("selection = %#v", selection)
	}
	if result.API != ConfAPIName || result.Method != "set" || result.Version != 1 {
		t.Fatalf("result metadata = %#v", result)
	}
	if executor.request.API != ConfAPIName || executor.request.Method != "set" || executor.request.Version != 1 {
		t.Fatalf("request = %#v", executor.request)
	}
	if executor.request.ReadOnly {
		t.Fatal("Conf.set must not be marked read-only (never auto-retried)")
	}
	input, ok := executor.request.JSONParameters["Input"].(map[string]any)
	if !ok {
		t.Fatalf("Input is not an object: %#v", executor.request.JSONParameters)
	}
	if input["argGroup"] != securityadvisor.BaselineCompany {
		t.Errorf("argGroup = %v, want company", input["argGroup"])
	}
	if enabled, ok := input["enableSchedule"].(bool); !ok || !enabled {
		t.Errorf("enableSchedule = %v (%T), want bool true", input["enableSchedule"], input["enableSchedule"])
	}
	if weekday, ok := input["weekday"].(string); !ok || weekday != "4" {
		t.Errorf("weekday = %v (%T), want string \"4\"", input["weekday"], input["weekday"])
	}
	if hour, ok := input["hour"].(int); !ok || hour != 2 {
		t.Errorf("hour = %v (%T), want int 2", input["hour"], input["hour"])
	}
	if minute, ok := input["minute"].(int); !ok || minute != 21 {
		t.Errorf("minute = %v (%T), want int 21", input["minute"], input["minute"])
	}
	if taskID, ok := input["scheduleTaskId"].(int); !ok || taskID != 2 {
		t.Errorf("scheduleTaskId = %v (%T), want int 2", input["scheduleTaskId"], input["scheduleTaskId"])
	}
}

func TestEncodeConfInputRejectsUnmanagedBaselines(t *testing.T) {
	base := liveConfiguration()
	cases := map[string]string{
		"custom":  securityadvisor.BaselineCustom,
		"empty":   "",
		"unknown": "enterprise",
	}
	for name, baseline := range cases {
		desired := base
		desired.Baseline = baseline
		if _, err := encodeConfInput(desired); err == nil {
			t.Errorf("%s baseline %q was accepted, want rejection", name, baseline)
		}
	}
}

func TestEncodeConfInputRejectsOutOfRangeSchedule(t *testing.T) {
	for _, mutate := range []func(*securityadvisor.Configuration){
		func(c *securityadvisor.Configuration) { c.Schedule.Hour = 24 },
		func(c *securityadvisor.Configuration) { c.Schedule.Hour = -1 },
		func(c *securityadvisor.Configuration) { c.Schedule.Minute = 60 },
		func(c *securityadvisor.Configuration) { c.Schedule.Weekday = "sunday" },
	} {
		desired := liveConfiguration()
		mutate(&desired)
		if _, err := encodeConfInput(desired); err == nil {
			t.Errorf("out-of-range schedule %#v was accepted, want rejection", desired.Schedule)
		}
	}
}

// TestExecuteRunScanRequestShape locks the live-verified run-scan wire:
// Operation.start with items sent as the plain "ALL" string sentinel (never a
// JSON array, which crashes the DSM CGI worker).
func TestExecuteRunScanRequestShape(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{"success":true}`)}
	result, selection, err := ExecuteRunScan(context.Background(), saTarget(), executor)
	if err != nil {
		t.Fatalf("ExecuteRunScan() error = %v", err)
	}
	if !selection.Supported || selection.Backend != "securityscan-operation-start-v1" {
		t.Fatalf("selection = %#v", selection)
	}
	if !result.Started || result.API != OperationAPIName || result.Method != "start" || result.Version != 1 {
		t.Fatalf("result = %#v", result)
	}
	if executor.request.API != OperationAPIName || executor.request.Method != "start" {
		t.Fatalf("request = %#v", executor.request)
	}
	items, ok := executor.request.JSONParameters["items"].(string)
	if !ok || items != "ALL" {
		t.Fatalf("items = %v (%T), want string \"ALL\"", executor.request.JSONParameters["items"], executor.request.JSONParameters["items"])
	}
}

// TestDecodeStatusDuringScan proves a status poll taken while a scan is running
// does not error on the sysStatus "running" marker (which is a scan state, not a
// severity) and reports the scan as in progress.
func TestDecodeStatusDuringScan(t *testing.T) {
	const running = `{
	  "items": {"network":{"category":"network","fail":{"warning":0},"failSeverity":"safe","progress":40,"runningItem":"rule_lan_export","total":9,"waitNum":3}},
	  "lastScanTime":"1784139791","startTime":"1784568325","sysProgress":40,"sysStatus":"running","success":true
	}`
	status, _, err := ExecuteStatus(context.Background(), saTarget(), &capturingExecutor{response: json.RawMessage(running)})
	if err != nil {
		t.Fatalf("ExecuteStatus() during scan error = %v", err)
	}
	if !status.Running {
		t.Fatalf("status.Running = false, want true during a scan")
	}
	if status.OverallSeverity != "" {
		t.Fatalf("overall severity = %q, want empty while scanning", status.OverallSeverity)
	}
	// A genuinely unknown severity value still errors (the strict-taxonomy
	// contract is preserved for completed scans).
	const bogus = `{"items":{},"sysProgress":100,"sysStatus":"apocalyptic","success":true}`
	if _, _, err := ExecuteStatus(context.Background(), saTarget(), &capturingExecutor{response: json.RawMessage(bogus)}); err == nil {
		t.Fatal("unknown completed severity was accepted, want rejection")
	}
}

func TestSelectWriteAndRunFailClosed(t *testing.T) {
	statusOnly := compatibility.NewTarget()
	statusOnly.SetAPI(StatusAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	if selection, err := SelectConfigurationSet(statusOnly); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("schedule write must fail closed when Conf is absent: %#v %v", selection, err)
	}
	if selection, err := SelectRunScan(statusOnly); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("run scan must fail closed when Operation is absent: %#v %v", selection, err)
	}
	if selection, err := SelectConfigurationSet(saTarget()); err != nil || !selection.Supported {
		t.Fatalf("schedule write must select on the full target: %#v %v", selection, err)
	}
	if selection, err := SelectRunScan(saTarget()); err != nil || !selection.Supported {
		t.Fatalf("run scan must select on the full target: %#v %v", selection, err)
	}
}
