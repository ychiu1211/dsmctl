// Package controlpaneltime implements the independently selectable DSM
// operation for reading Control Panel time and NTP configuration.
package controlpaneltime

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	APIName           = "SYNO.Core.Region.NTP"
	CapabilityName    = "controlpanel.time.read"
	OperationName     = "controlpanel.time.read"
	SetCapabilityName = "controlpanel.time.set"
	SetOperationName  = "controlpanel.time.set"
)

type Input struct{}

// MutationResult records the DSM backend that accepted a time-module change.
type MutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

var operation = compatibility.Operation[Input, controlpanel.TimeState]{
	Name: OperationName,
	Variants: []compatibility.Variant[Input, controlpanel.TimeState]{
		coreVariant("core-region-ntp-v3", 3, 30, true),
		coreVariant("core-region-ntp-v2", 2, 20, true),
		coreVariant("core-region-ntp-v1-legacy", 1, 10, false),
	},
}

// setOperation exposes only the v3 set method: it is the one write variant
// with primary DSM evidence. Older advertised versions stay unsupported and
// therefore fail closed before any request is built.
var setOperation = compatibility.Operation[controlpanel.TimeState, MutationResult]{
	Name: SetOperationName,
	Variants: []compatibility.Variant[controlpanel.TimeState, MutationResult]{
		setVariant("core-region-ntp-v3", 3, 30),
	},
}

func APINames() []string {
	return operation.APINames()
}

func Select(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := operation.Select(target)
	return selection, err
}

func Execute(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (controlpanel.TimeState, compatibility.Selection, error) {
	return operation.Run(ctx, target, executor, Input{})
}

func SelectSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := setOperation.Select(target)
	return selection, err
}

// ExecuteSet submits the complete desired configuration. The caller must
// merge its patch into the freshly read state first so an unspecified field
// can never be silently reset by DSM.
func ExecuteSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired controlpanel.TimeState) (MutationResult, compatibility.Selection, error) {
	result, selection, err := setOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

func setVariant(name string, version, priority int) compatibility.Variant[controlpanel.TimeState, MutationResult] {
	return compatibility.Variant[controlpanel.TimeState, MutationResult]{
		Name:     name,
		API:      APIName,
		Version:  version,
		Priority: priority,
		Match:    compatibility.APIVersion(APIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, desired controlpanel.TimeState) (MutationResult, error) {
			parameters, err := encodeTimeSet(desired)
			if err != nil {
				return MutationResult{}, err
			}
			if _, err := executor.Execute(ctx, compatibility.Request{
				API:            APIName,
				Version:        version,
				Method:         "set",
				JSONParameters: parameters,
			}); err != nil {
				return MutationResult{}, fmt.Errorf("call %s.set v%d: %w", APIName, version, err)
			}
			return MutationResult{}, nil
		},
	}
}

func coreVariant(name string, version, priority int, requireFormats bool) compatibility.Variant[Input, controlpanel.TimeState] {
	return compatibility.Variant[Input, controlpanel.TimeState]{
		Name:     name,
		API:      APIName,
		Version:  version,
		Priority: priority,
		Match:    compatibility.APIVersion(APIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (controlpanel.TimeState, error) {
			data, err := executor.Execute(ctx, compatibility.Request{
				API:     APIName,
				Version: version,
				Method:  "get",
			})
			if err != nil {
				return controlpanel.TimeState{}, fmt.Errorf("call %s.get v%d: %w", APIName, version, err)
			}
			return decode(data, requireFormats)
		},
	}
}
