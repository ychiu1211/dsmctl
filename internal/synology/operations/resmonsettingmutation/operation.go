// Package resmonsettingmutation implements the guarded DSM Resource Monitor
// setting write. It calls SYNO.ResourceMonitor.Setting.set to toggle history
// recording. The caller submits the complete merged setting object so a patch
// to the recording field can never silently reset a co-located DSM setting.
package resmonsettingmutation

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/resmonsetting"
)

const (
	APIName           = "SYNO.ResourceMonitor.Setting"
	SetCapabilityName = "resource.recording_set"
	SetOperationName  = "resource.recording.set"
)

// RecordingField is the DSM setting field the mutation owns. Re-exported from
// the read package so the read and write halves target the same key.
const RecordingField = resmonsetting.RecordingField

// MutationResult records the DSM backend that accepted a recording-setting
// change.
type MutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

var setOperation = compatibility.Operation[map[string]any, MutationResult]{
	Name: SetOperationName,
	Variants: []compatibility.Variant[map[string]any, MutationResult]{
		{
			Name: "resourcemonitor-setting-set-v1", API: APIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired map[string]any) (MutationResult, error) {
				if len(desired) == 0 {
					return MutationResult{}, fmt.Errorf("recording set requires the merged setting object")
				}
				if _, ok := desired[RecordingField]; !ok {
					return MutationResult{}, fmt.Errorf("recording set requires the %q field", RecordingField)
				}
				if _, err := executor.Execute(ctx, compatibility.Request{
					API:            APIName,
					Version:        1,
					Method:         "set",
					JSONParameters: desired,
				}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v1: %w", APIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

func APINames() []string {
	return setOperation.APINames()
}

func SelectSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := setOperation.Select(target)
	return selection, err
}

// ExecuteSet submits the complete merged setting object. The caller must read
// the current setting, overlay the recording field, and pass the whole map so
// DSM never resets an unspecified field.
func ExecuteSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired map[string]any) (MutationResult, compatibility.Selection, error) {
	result, selection, err := setOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}
