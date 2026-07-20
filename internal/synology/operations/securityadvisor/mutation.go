package securityadvisor

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ychiu1211/dsmctl/internal/domain/securityadvisor"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// The write wire is live-verified on the DSM 7.x lab and cross-checked against
// the Security Advisor admin JS (SYNO.SDS.SecurityScan):
//
//	SYNO.Core.SecurityScan.Conf      set   v1  params {Input:{argGroup, enableSchedule, weekday, hour, minute, scheduleTaskId}}
//	SYNO.Core.SecurityScan.Operation start v1  params {items:"ALL"}  (space-separated rule ids, or the "ALL" sentinel)
//
// Conf.set wraps the fields in a single JSON object under the top-level `Input`
// form field, and the baseline field is `argGroup` (values home|company), which
// mirrors the read's `defaultGroup`. Conf.set is rejected while a scan is
// running (errinfo key securityscan_error_is_scanning), so a run and a config
// write are strictly separated. Operation.start's items must be a plain string,
// never a JSON array — an array crashes the DSM CGI worker.
const (
	// ScheduleWriteCapabilityName is the guarded schedule + baseline set.
	ScheduleWriteCapabilityName = "securityadvisor.schedule.write"
	// RunScanCapabilityName is the load-heavy run-scan action.
	RunScanCapabilityName = "securityadvisor.scan.run"

	// runScanAllItems is the DSM sentinel that scans every rule in the baseline.
	runScanAllItems = "ALL"
)

// MutationResult records the DSM backend that accepted a Security Advisor write.
type MutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

// ScanResult records the backend that accepted a run-scan action.
type ScanResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the scan trigger"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the scan trigger"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the scan trigger"`
	Started bool   `json:"started" jsonschema:"Whether DSM accepted the scan trigger"`
}

var configurationSetOperation = compatibility.Operation[securityadvisor.Configuration, MutationResult]{
	Name: ScheduleWriteCapabilityName,
	Variants: []compatibility.Variant[securityadvisor.Configuration, MutationResult]{
		{
			Name: "securityscan-conf-set-v1", API: ConfAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(ConfAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired securityadvisor.Configuration) (MutationResult, error) {
				input, err := encodeConfInput(desired)
				if err != nil {
					return MutationResult{}, err
				}
				if _, err := executor.Execute(ctx, compatibility.Request{
					API:            ConfAPIName,
					Version:        1,
					Method:         "set",
					JSONParameters: map[string]any{"Input": input},
				}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v1: %w", ConfAPIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

var runScanOperation = compatibility.Operation[Input, ScanResult]{
	Name: RunScanCapabilityName,
	Variants: []compatibility.Variant[Input, ScanResult]{
		{
			Name: "securityscan-operation-start-v1", API: OperationAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(OperationAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (ScanResult, error) {
				if _, err := executor.Execute(ctx, compatibility.Request{
					API:            OperationAPIName,
					Version:        1,
					Method:         "start",
					JSONParameters: map[string]any{"items": runScanAllItems},
				}); err != nil {
					return ScanResult{}, fmt.Errorf("call %s.start v1: %w", OperationAPIName, err)
				}
				return ScanResult{Started: true}, nil
			},
		},
	},
}

// encodeConfInput builds the Conf.set Input object from the complete desired
// configuration. The caller must merge its patch into the freshly read state
// first, so every field here is required and a missing one is a contract
// violation rather than a field to skip. The baseline is restricted to the two
// managed groups; the custom checklist is out of this module's scope.
func encodeConfInput(desired securityadvisor.Configuration) (map[string]any, error) {
	switch desired.Baseline {
	case securityadvisor.BaselineHome, securityadvisor.BaselineCompany:
	case "":
		return nil, fmt.Errorf("security advisor set requires a baseline")
	case securityadvisor.BaselineCustom:
		return nil, fmt.Errorf("security advisor set does not manage the custom checklist baseline")
	default:
		return nil, fmt.Errorf("security advisor set rejects unknown baseline %q", desired.Baseline)
	}
	if desired.Schedule.Hour < 0 || desired.Schedule.Hour > 23 {
		return nil, fmt.Errorf("security advisor set hour %d out of range 0-23", desired.Schedule.Hour)
	}
	if desired.Schedule.Minute < 0 || desired.Schedule.Minute > 59 {
		return nil, fmt.Errorf("security advisor set minute %d out of range 0-59", desired.Schedule.Minute)
	}
	weekday := desired.Schedule.Weekday
	if weekday == "" {
		weekday = "0"
	}
	if _, err := strconv.Atoi(weekday); err != nil {
		return nil, fmt.Errorf("security advisor set weekday %q is not a DSM weekday selector", desired.Schedule.Weekday)
	}
	return map[string]any{
		"argGroup":       desired.Baseline,
		"enableSchedule": desired.Schedule.Enabled,
		"weekday":        weekday,
		"hour":           desired.Schedule.Hour,
		"minute":         desired.Schedule.Minute,
		"scheduleTaskId": desired.Schedule.TaskID,
	}, nil
}

// SelectConfigurationSet reports whether the guarded schedule + baseline write
// rides an advertised backend.
func SelectConfigurationSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := configurationSetOperation.Select(target)
	return selection, err
}

// ExecuteConfigurationSet submits the complete desired configuration. The caller
// merges its patch into the freshly read state first so an unspecified field can
// never be silently reset by DSM.
func ExecuteConfigurationSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired securityadvisor.Configuration) (MutationResult, compatibility.Selection, error) {
	result, selection, err := configurationSetOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

// SelectRunScan reports whether the run-scan action rides an advertised backend.
func SelectRunScan(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := runScanOperation.Select(target)
	return selection, err
}

// ExecuteRunScan triggers a full Security Advisor scan. It changes no persisted
// configuration and is never invoked implicitly by a read.
func ExecuteRunScan(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (ScanResult, compatibility.Selection, error) {
	result, selection, err := runScanOperation.Run(ctx, target, executor, Input{})
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "start"
	}
	return result, selection, err
}
