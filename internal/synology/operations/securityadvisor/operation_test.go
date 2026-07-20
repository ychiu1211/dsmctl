package securityadvisor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/securityadvisor"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type capturingExecutor struct {
	request  compatibility.Request
	response json.RawMessage
}

func (e *capturingExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	e.request = request
	return e.response, nil
}

// saTarget advertises the full DSM 7.3 Security Advisor family: Status, Conf,
// and the deferred Operation API, all v1 JSON on entry.cgi.
func saTarget() compatibility.Target {
	target := compatibility.NewTarget()
	target.SetAPI(StatusAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1, RequestFormat: "JSON"})
	target.SetAPI(ConfAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1, RequestFormat: "JSON"})
	target.SetAPI(OperationAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1, RequestFormat: "JSON"})
	return target
}

// liveStatus is the exact SYNO.Core.SecurityScan.Status system_get response
// captured from the DSM 7.3 lab.
const liveStatus = `{
  "items": {
    "malware":     {"category":"malware","fail":{"danger":0,"info":0,"outOfDate":0,"risk":0,"warning":0},"failSeverity":"safe","progress":100,"runningItem":"","total":4,"waitNum":0},
    "network":     {"category":"network","fail":{"danger":0,"info":0,"outOfDate":0,"risk":1,"warning":3},"failSeverity":"risk","progress":100,"runningItem":"","total":9,"waitNum":0},
    "systemCheck": {"category":"systemCheck","fail":{"danger":0,"info":0,"outOfDate":0,"risk":0,"warning":2},"failSeverity":"warning","progress":100,"runningItem":"","total":19,"waitNum":0},
    "update":      {"category":"update","fail":{"danger":0,"info":1,"outOfDate":1,"risk":0,"warning":0},"failSeverity":"outOfDate","progress":100,"runningItem":"","total":4,"waitNum":0},
    "userInfo":    {"category":"userInfo","fail":{"danger":0,"info":0,"outOfDate":0,"risk":1,"warning":1},"failSeverity":"risk","progress":100,"runningItem":"","total":10,"waitNum":0}
  },
  "lastScanTime": "1784139791",
  "startTime": "",
  "success": true,
  "sysProgress": 100,
  "sysStatus": "risk"
}`

// liveConf is the exact SYNO.Core.SecurityScan.Conf get response captured from
// the DSM 7.3 lab.
const liveConf = `{"defaultGroup":"company","enableSchedule":true,"hour":2,"minute":21,"scheduleTaskId":2,"success":true,"weekday":"4"}`

func TestSelectStatusRequiresAPI(t *testing.T) {
	if selection, err := SelectStatus(saTarget()); err != nil || !selection.Supported || selection.Backend != "securityscan-status-system-get-v1" {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
	if selection, err := SelectStatus(compatibility.NewTarget()); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
}

func TestSelectConfigurationRequiresAPI(t *testing.T) {
	if selection, err := SelectConfiguration(saTarget()); err != nil || !selection.Supported || selection.Backend != "securityscan-conf-get-v1" {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
	if selection, err := SelectConfiguration(compatibility.NewTarget()); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
}

func TestExecuteStatusDecodesLiveShape(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(liveStatus)}
	status, selection, err := ExecuteStatus(context.Background(), saTarget(), executor)
	if err != nil {
		t.Fatalf("ExecuteStatus() error = %v", err)
	}
	if executor.request.API != StatusAPIName || executor.request.Version != 1 || executor.request.Method != "system_get" {
		t.Fatalf("request = %#v", executor.request)
	}
	if !executor.request.ReadOnly {
		t.Fatalf("status read must be marked ReadOnly")
	}
	if selection.Backend != "securityscan-status-system-get-v1" {
		t.Fatalf("selection = %#v", selection)
	}
	if status.OverallSeverity != securityadvisor.SeverityRisk {
		t.Fatalf("overall severity = %q", status.OverallSeverity)
	}
	if status.Running {
		t.Fatalf("scan should not be reported running at 100%% progress")
	}
	if status.Progress != 100 || status.LastScanTime != 1784139791 {
		t.Fatalf("progress/time = %d / %d", status.Progress, status.LastScanTime)
	}
	// 4 + 9 + 19 + 4 + 10 = 46 total checks.
	if status.TotalChecks != 46 {
		t.Fatalf("total checks = %d", status.TotalChecks)
	}
	// findings: network 4, systemCheck 2, update 2, userInfo 2 = 10.
	if status.TotalFindings != 10 {
		t.Fatalf("total findings = %d", status.TotalFindings)
	}
	want := securityadvisor.SeverityCounts{Danger: 0, Risk: 2, Warning: 6, OutOfDate: 1, Info: 1}
	if status.Counts != want {
		t.Fatalf("aggregate counts = %#v want %#v", status.Counts, want)
	}
	if len(status.Categories) != 5 {
		t.Fatalf("categories = %d", len(status.Categories))
	}
	// Sorted by descending severity then name: the two risk categories first,
	// ordered network before userInfo.
	if status.Categories[0].Category != "network" || status.Categories[0].FailSeverity != securityadvisor.SeverityRisk {
		t.Fatalf("first category = %#v", status.Categories[0])
	}
	if status.Categories[1].Category != "userInfo" {
		t.Fatalf("second category = %#v", status.Categories[1])
	}
	// The safe category (malware) sorts last.
	last := status.Categories[len(status.Categories)-1]
	if last.Category != "malware" || last.FailSeverity != securityadvisor.SeveritySafe || last.Findings != 0 || last.Passed != 4 {
		t.Fatalf("last category = %#v", last)
	}
	network := status.Categories[0]
	if network.Total != 9 || network.Findings != 4 || network.Passed != 5 || network.Counts.Warning != 3 || network.Counts.Risk != 1 {
		t.Fatalf("network category = %#v", network)
	}
}

func TestExecuteConfigurationDecodesLiveShape(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(liveConf)}
	configuration, selection, err := ExecuteConfiguration(context.Background(), saTarget(), executor)
	if err != nil {
		t.Fatalf("ExecuteConfiguration() error = %v", err)
	}
	if executor.request.API != ConfAPIName || executor.request.Method != "get" {
		t.Fatalf("request = %#v", executor.request)
	}
	if selection.Backend != "securityscan-conf-get-v1" {
		t.Fatalf("selection = %#v", selection)
	}
	if configuration.Baseline != "company" {
		t.Fatalf("baseline = %q", configuration.Baseline)
	}
	want := securityadvisor.Schedule{Enabled: true, Hour: 2, Minute: 21, Weekday: "4", TaskID: 2}
	if configuration.Schedule != want {
		t.Fatalf("schedule = %#v want %#v", configuration.Schedule, want)
	}
}

func TestDecodeStatusRejectsUnknownSeverity(t *testing.T) {
	// An unrecognized severity value must error rather than being coerced.
	const unknownOverall = `{"items":{},"sysStatus":"apocalyptic","sysProgress":100,"lastScanTime":"0"}`
	if _, _, err := ExecuteStatus(context.Background(), saTarget(), &capturingExecutor{response: json.RawMessage(unknownOverall)}); err == nil || !strings.Contains(err.Error(), "unrecognized security-advisor severity") {
		t.Fatalf("overall severity error = %v", err)
	}
	const unknownCategory = `{"items":{"network":{"category":"network","failSeverity":"spicy","total":1,"progress":100}},"sysStatus":"safe","sysProgress":100,"lastScanTime":"0"}`
	if _, _, err := ExecuteStatus(context.Background(), saTarget(), &capturingExecutor{response: json.RawMessage(unknownCategory)}); err == nil || !strings.Contains(err.Error(), "unrecognized security-advisor severity") {
		t.Fatalf("category severity error = %v", err)
	}
}

func TestDecodeStatusRejectsMissingItems(t *testing.T) {
	const noItems = `{"sysStatus":"safe","sysProgress":100}`
	if _, _, err := ExecuteStatus(context.Background(), saTarget(), &capturingExecutor{response: json.RawMessage(noItems)}); err == nil || !strings.Contains(err.Error(), "no items object") {
		t.Fatalf("missing-items error = %v", err)
	}
}

func TestDecodeConfigurationRejectsMissingBaseline(t *testing.T) {
	const noGroup = `{"enableSchedule":true,"hour":2,"minute":21}`
	if _, _, err := ExecuteConfiguration(context.Background(), saTarget(), &capturingExecutor{response: json.RawMessage(noGroup)}); err == nil || !strings.Contains(err.Error(), "no defaultGroup") {
		t.Fatalf("missing-baseline error = %v", err)
	}
}

func TestDecodeStatusDetectsRunningScan(t *testing.T) {
	// A scan mid-flight: overall progress below 100 and a running item.
	const running = `{
      "items": {"network":{"category":"network","fail":{"warning":0},"failSeverity":"safe","progress":40,"runningItem":"open_port","total":9,"waitNum":3}},
      "lastScanTime":"1784139791","startTime":"1784200000","sysProgress":40,"sysStatus":"safe"
    }`
	status, _, err := ExecuteStatus(context.Background(), saTarget(), &capturingExecutor{response: json.RawMessage(running)})
	if err != nil {
		t.Fatalf("ExecuteStatus() error = %v", err)
	}
	if !status.Running || status.Progress != 40 {
		t.Fatalf("running scan not detected: %#v", status)
	}
	if status.StartTime != "1784200000" {
		t.Fatalf("start time = %q", status.StartTime)
	}
}

// TestDecodeStatusDropsInjectedSessionIdentity enforces the standing invariant
// that no session identity (SID/SynoToken) can ride the read into the display
// model, even if a malicious or buggy DSM smuggles such fields into the payload.
func TestDecodeStatusDropsInjectedSessionIdentity(t *testing.T) {
	const canary = "SIDCANARY-must-not-survive-decode"
	response := `{
      "items": {"network":{"category":"network","fail":{"warning":1},"failSeverity":"warning","progress":100,"total":9,"waitNum":0,"_sid":"` + canary + `","SynoToken":"` + canary + `"}},
      "lastScanTime":"1784139791","sysProgress":100,"sysStatus":"warning","_sid":"` + canary + `","SynoToken":"` + canary + `"
    }`
	status, _, err := ExecuteStatus(context.Background(), saTarget(), &capturingExecutor{response: json.RawMessage(response)})
	if err != nil {
		t.Fatalf("ExecuteStatus() error = %v", err)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), canary) {
		t.Fatalf("decoded status carried injected session identity: %s", encoded)
	}
}

func TestCapabilityHelpers(t *testing.T) {
	if !SupportsRunScan(saTarget()) || !SupportsScheduleWrite(saTarget()) {
		t.Fatalf("expected deferred write/action APIs advertised on the full target")
	}
	// A target advertising only Status supports the status read but neither the
	// deferred run-scan action nor the schedule write.
	statusOnly := compatibility.NewTarget()
	statusOnly.SetAPI(StatusAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	if SupportsRunScan(statusOnly) || SupportsScheduleWrite(statusOnly) {
		t.Fatalf("status-only target must not advertise deferred write/action APIs")
	}
	if selection, err := SelectConfiguration(statusOnly); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("schedule read must fail closed when Conf is absent: %#v %v", selection, err)
	}
}
