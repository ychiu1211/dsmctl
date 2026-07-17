// Package resmonsetting implements the read-only DSM Resource Monitor setting
// operation. It calls SYNO.ResourceMonitor.Setting.get and never mutates DSM.
// The setting write path lives in the resmonsettingmutation package.
package resmonsetting

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/resmon"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	APIName        = "SYNO.ResourceMonitor.Setting"
	CapabilityName = "resource.recording_read"
	OperationName  = "resource.recording.read"

	// RecordingField is the DSM setting field that turns history recording on
	// or off. It is shared with the mutation package so the patch targets the
	// exact key DSM's Resource Monitor UI uses.
	RecordingField = "enable_history"
)

// Settings is the decoded recording setting plus the raw DSM setting object.
// The raw map lets the mutation re-send every field DSM reported so a patch to
// RecordingField never resets settings dsmctl does not own.
type Settings struct {
	Recording resmon.RecordingSetting
	Raw       map[string]any
}

var operation = compatibility.Operation[struct{}, Settings]{
	Name: OperationName,
	Variants: []compatibility.Variant[struct{}, Settings]{
		{
			Name: "resourcemonitor-setting-get-v1", API: APIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ struct{}) (Settings, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: APIName, Version: 1, Method: "get"})
				if err != nil {
					return Settings{}, fmt.Errorf("call %s.get v1: %w", APIName, err)
				}
				return decode(data)
			},
		},
	},
}

func APINames() []string {
	return operation.APINames()
}

func Select(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := operation.Select(target)
	return selection, err
}

func Execute(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (Settings, compatibility.Selection, error) {
	return operation.Run(ctx, target, executor, struct{}{})
}
