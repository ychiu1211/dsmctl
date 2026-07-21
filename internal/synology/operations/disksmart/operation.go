// Package disksmart implements independently selectable, read-only DSM
// operations for Storage Manager's per-disk health and S.M.A.R.T. surface.
//
// Three DSM API families are read, each gated independently so a NAS that
// advertises one but not the others fails closed only for the missing area:
//
//   - Per-disk health, SSD remaining-life, spare-block/bad-sector detail, and
//     coarse self-test state: SYNO.Core.Storage.Disk v1 `list`
//     (params {offset:0, limit:-1}).
//   - Per-disk S.M.A.R.T. attribute table plus a health/test summary:
//     SYNO.Storage.CGI.Smart v1 `get_health_info` (params {device:"/dev/sdX"}).
//     A disk that exposes no attribute table answers with DSM code 117, which
//     is reported as "no SMART data" rather than failing the whole read.
//   - Detailed self-test status/log: SYNO.Core.Storage.Disk v1
//     `get_smart_test_log` (params {device:"/dev/sdX"}).
//   - Global disk-health warning thresholds: SYNO.Storage.CGI.HddMan v1 `get`.
//
// Every shape below was live-verified against the lab (DS3018xs, DSM 7.3); see
// WI-077's evidence note. Serial numbers are read but treated as identity and
// must never enter committed fixtures or logs.
package disksmart

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/disksmart"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	CoreDiskAPI = "SYNO.Core.Storage.Disk"
	SmartAPI    = "SYNO.Storage.CGI.Smart"
	HddManAPI   = "SYNO.Storage.CGI.HddMan"

	HealthReadCapabilityName     = "disk.smart.health.read"
	AttributesReadCapabilityName = "disk.smart.attributes.read"
	ThresholdsReadCapabilityName = "disk.smart.thresholds.read"

	// NoSMARTDataCode is DSM's error code for get_health_info on a disk that
	// exposes no S.M.A.R.T. attribute table.
	NoSMARTDataCode = 117
)

// Input is the empty request the NAS-wide reads take.
type Input struct{}

// deviceInput carries the kernel device path (/dev/sdX) for a per-disk read.
type deviceInput struct {
	Device string
}

// healthOp enumerates every installed disk with its health-lifecycle detail.
var healthOp = compatibility.Operation[Input, []disksmart.DiskHealth]{
	Name: "disk.smart.health.read",
	Variants: []compatibility.Variant[Input, []disksmart.DiskHealth]{
		{
			Name: "core-storage-disk-list-v1", API: CoreDiskAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(CoreDiskAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]disksmart.DiskHealth, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: CoreDiskAPI, Version: 1, Method: "list",
					JSONParameters: map[string]any{"offset": 0, "limit": -1},
					ReadOnly:       true,
				})
				if err != nil {
					return nil, fmt.Errorf("call %s.list v1: %w", CoreDiskAPI, err)
				}
				return decodeDiskHealthList(data)
			},
		},
	},
}

// thresholdsOp reads the NAS-wide disk-health warning thresholds.
var thresholdsOp = compatibility.Operation[Input, disksmart.HealthThresholds]{
	Name: "disk.smart.thresholds.read",
	Variants: []compatibility.Variant[Input, disksmart.HealthThresholds]{
		{
			Name: "hddman-get-v1", API: HddManAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(HddManAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (disksmart.HealthThresholds, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: HddManAPI, Version: 1, Method: "get", ReadOnly: true,
				})
				if err != nil {
					return disksmart.HealthThresholds{}, fmt.Errorf("call %s.get v1: %w", HddManAPI, err)
				}
				return decodeThresholds(data)
			},
		},
	},
}

// attributeOp reads one disk's S.M.A.R.T. attribute table plus summary.
var attributeOp = compatibility.Operation[deviceInput, disksmart.DiskSMART]{
	Name: "disk.smart.attributes.read",
	Variants: []compatibility.Variant[deviceInput, disksmart.DiskSMART]{
		{
			Name: "storage-cgi-smart-get-health-info-v1", API: SmartAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(SmartAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input deviceInput) (disksmart.DiskSMART, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: SmartAPI, Version: 1, Method: "get_health_info",
					JSONParameters: map[string]any{"device": input.Device},
					ReadOnly:       true,
				})
				if err != nil {
					return disksmart.DiskSMART{}, fmt.Errorf("call %s.get_health_info v1: %w", SmartAPI, err)
				}
				return decodeDiskSMART(data)
			},
		},
	},
}

// testLogOp reads one disk's detailed self-test status/log.
var testLogOp = compatibility.Operation[deviceInput, disksmart.SMARTTestStatus]{
	Name: "disk.smart.testlog.read",
	Variants: []compatibility.Variant[deviceInput, disksmart.SMARTTestStatus]{
		{
			Name: "core-storage-disk-get-smart-test-log-v1", API: CoreDiskAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(CoreDiskAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input deviceInput) (disksmart.SMARTTestStatus, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: CoreDiskAPI, Version: 1, Method: "get_smart_test_log",
					JSONParameters: map[string]any{"device": input.Device},
					ReadOnly:       true,
				})
				if err != nil {
					return disksmart.SMARTTestStatus{}, fmt.Errorf("call %s.get_smart_test_log v1: %w", CoreDiskAPI, err)
				}
				return decodeTestStatus(data, input.Device)
			},
		},
	},
}

// APINames returns every DSM API this module may read, so the facade can
// discover them in a single query before selecting any area.
func APINames() []string {
	return []string{CoreDiskAPI, SmartAPI, HddManAPI}
}

// SelectHealth reports the per-disk health backend selection.
func SelectHealth(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := healthOp.Select(target)
	return selection, err
}

// SelectAttributes reports the per-disk SMART attribute backend selection.
func SelectAttributes(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := attributeOp.Select(target)
	return selection, err
}

// SelectThresholds reports the global-threshold backend selection.
func SelectThresholds(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := thresholdsOp.Select(target)
	return selection, err
}

// ReadHealth reads every disk's health-lifecycle detail (required) and enriches
// it with the NAS-wide warning thresholds when that independently versioned API
// is available.
func ReadHealth(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (disksmart.HealthState, compatibility.Selection, error) {
	disks, selection, err := healthOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return disksmart.HealthState{}, selection, err
	}
	if disks == nil {
		disks = []disksmart.DiskHealth{}
	}
	state := disksmart.HealthState{Disks: disks}
	if thresholds, ok, err := readOptionalThresholds(ctx, target, executor); err != nil {
		return disksmart.HealthState{}, selection, err
	} else if ok {
		state.Thresholds = &thresholds
	}
	return state, selection, nil
}

// ReadSMART reads every disk's S.M.A.R.T. attribute table. The attribute area
// (SYNO.Storage.CGI.Smart) is the primary selection; the disk list is used only
// to enumerate the devices to query. A disk that returns a DSM error for the
// attribute read is reported as "no SMART data" rather than failing the whole
// read, and the detailed self-test log is attached when its API is present.
func ReadSMART(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (disksmart.SMARTState, compatibility.Selection, error) {
	_, selection, err := attributeOp.Select(target)
	if err != nil {
		return disksmart.SMARTState{}, selection, err
	}

	state := disksmart.SMARTState{Disks: []disksmart.DiskSMART{}}
	// Enumerate disks through the health API. When that API is absent there is
	// nothing to key the per-disk attribute reads on, so return an empty but
	// successful state for the (supported) attribute area.
	disks, _, err := healthOp.Run(ctx, target, executor, Input{})
	if err != nil {
		if compatibility.IsUnsupported(err) {
			return state, selection, nil
		}
		return disksmart.SMARTState{}, selection, err
	}

	testLogSupported := false
	if _, testSelection, selErr := testLogOp.Select(target); selErr == nil {
		testLogSupported = testSelection.Supported
	}

	for _, disk := range disks {
		if disk.Device == "" {
			continue
		}
		smart, _, err := attributeOp.Run(ctx, target, executor, deviceInput{Device: disk.Device})
		if err != nil {
			// A DSM application error for a single disk (for example code 117
			// on an enterprise SSD with no attribute table) means that disk has
			// no SMART data — not that the read failed. Transport and session
			// failures are not application errors and are propagated.
			code, isAPIErr := compatibility.APIErrorCode(err)
			if !isAPIErr {
				return disksmart.SMARTState{}, selection, err
			}
			smart = disksmart.DiskSMART{NoSMARTData: true, AbsenceCode: code}
		}
		// The disk list is the identity authority; overlay it so a disk with no
		// SMART data is still identifiable.
		smart.ID = disk.ID
		smart.Device = disk.Device
		if smart.Name == "" {
			smart.Name = disk.Name
		}
		if smart.Model == "" {
			smart.Model = disk.Model
		}
		if smart.Serial == "" {
			smart.Serial = disk.Serial
		}
		if smart.Type == "" {
			smart.Type = disk.Type
		}
		smart.IsSSD = smart.IsSSD || disk.Type == "SSD"
		if smart.Attributes == nil {
			smart.Attributes = []disksmart.SMARTAttribute{}
		}
		if testLogSupported {
			if status, _, err := testLogOp.Run(ctx, target, executor, deviceInput{Device: disk.Device}); err == nil {
				smart.TestStatus = &status
			}
			// A failed test-log read is intentionally non-fatal: the attribute
			// table and summary above are still returned.
		}
		state.Disks = append(state.Disks, smart)
	}
	return state, selection, nil
}

// readOptionalThresholds reads the global thresholds only when the HddMan API is
// available. An unsupported area is a normal skip (ok=false, nil error).
func readOptionalThresholds(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (disksmart.HealthThresholds, bool, error) {
	if _, selection, err := thresholdsOp.Select(target); err != nil {
		if compatibility.IsUnsupported(err) {
			return disksmart.HealthThresholds{}, false, nil
		}
		return disksmart.HealthThresholds{}, false, err
	} else if !selection.Supported {
		return disksmart.HealthThresholds{}, false, nil
	}
	result, _, err := thresholdsOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return disksmart.HealthThresholds{}, false, err
	}
	return result, true, nil
}
