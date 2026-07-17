// Package utilizationread implements the read-only DSM Resource Monitor
// utilization operations. Both the current snapshot and the recorded history
// are served by SYNO.Core.System.Utilization.get; the request parameter picks
// the mode. This package never mutates DSM.
package utilizationread

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/resmon"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	APIName = "SYNO.Core.System.Utilization"
	// CapabilityName covers both current and history reads because DSM serves
	// them from the same API and version.
	CapabilityName = "resource.read"

	OperationCurrent = "resource.current"
	OperationHistory = "resource.history"
)

// HistoryInput selects the recorded-history window and which DSM resource
// groups to fetch. Period is a normalized resmon.Period* value; Resources are
// the DSM resource-group names (cpu, memory, network, disk, space) — DSM
// rejects a history request with no resource (error 1051, bad parameters).
// Interfaces maps a resource group to the device ids to read; DSM requires it
// for the per-device groups (disk, network, space) and rejects them without a
// valid interface list (error 1057, bad interface). cpu and memory need none.
type HistoryInput struct {
	Period     string
	Resources  []string
	Interfaces map[string][]string
}

var currentOperation = compatibility.Operation[struct{}, resmon.Utilization]{
	Name: OperationCurrent,
	Variants: []compatibility.Variant[struct{}, resmon.Utilization]{
		{
			Name: "core-system-utilization-current-v1", API: APIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ struct{}) (resmon.Utilization, error) {
				// The current snapshot is the default get with no mode parameter;
				// DSM returns every resource group.
				data, err := executor.Execute(ctx, compatibility.Request{API: APIName, Version: 1, Method: "get"})
				if err != nil {
					return resmon.Utilization{}, fmt.Errorf("call %s.get current v1: %w", APIName, err)
				}
				return decodeUtilization(data)
			},
		},
	},
}

var historyOperation = compatibility.Operation[HistoryInput, resmon.History]{
	Name: OperationHistory,
	Variants: []compatibility.Variant[HistoryInput, resmon.History]{
		{
			Name: "core-system-utilization-history-v1", API: APIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input HistoryInput) (resmon.History, error) {
				// DSM selects history mode with type=history, the window with
				// time_range, and the groups to fetch with the resource array.
				// A missing resource, or an unrecognized time_range token, yields
				// error 1051 (bad parameters).
				parameters := map[string]any{
					"type":       "history",
					"time_range": input.Period,
					"resource":   input.Resources,
				}
				if len(input.Interfaces) > 0 {
					parameters["interfaces"] = input.Interfaces
				}
				data, err := executor.Execute(ctx, compatibility.Request{API: APIName, Version: 1, Method: "get", JSONParameters: parameters})
				if err != nil {
					return resmon.History{}, fmt.Errorf("call %s.get history v1: %w", APIName, err)
				}
				return decodeHistory(data, input)
			},
		},
	},
}

func APINames() []string {
	return currentOperation.APINames()
}

func SelectCurrent(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := currentOperation.Select(target)
	return selection, err
}

func SelectHistory(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := historyOperation.Select(target)
	return selection, err
}

func ExecuteCurrent(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (resmon.Utilization, compatibility.Selection, error) {
	return currentOperation.Run(ctx, target, executor, struct{}{})
}

func ExecuteHistory(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input HistoryInput) (resmon.History, compatibility.Selection, error) {
	return historyOperation.Run(ctx, target, executor, input)
}
