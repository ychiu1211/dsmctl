// Package servicediscovery implements the DSM File Services service-discovery
// operations. Time Machine advertising lives on
// SYNO.Core.FileServ.ServiceDiscovery and WS-Discovery on
// SYNO.Core.FileServ.ServiceDiscovery.WSTransfer; each is an independent
// compatibility boundary behind this package.
package servicediscovery

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	ServiceDiscoveryAPIName = "SYNO.Core.FileServ.ServiceDiscovery"
	WSTransferAPIName       = "SYNO.Core.FileServ.ServiceDiscovery.WSTransfer"

	TimeMachineReadCapabilityName = "controlpanel.fileservices.servicediscovery.timemachine.read"
	TimeMachineSetCapabilityName  = "controlpanel.fileservices.servicediscovery.timemachine.set"
	WSDiscoveryReadCapabilityName = "controlpanel.fileservices.servicediscovery.wsdiscovery.read"
	WSDiscoverySetCapabilityName  = "controlpanel.fileservices.servicediscovery.wsdiscovery.set"
)

type Input struct{}

// TimeMachine is the Time Machine advertising pair carried by the
// ServiceDiscovery API.
type TimeMachine struct {
	SMB bool
	AFP bool
}

// MutationResult records the selected backend for one set.
type MutationResult struct {
	Area    string `json:"area" jsonschema:"Changed service-discovery area: time_machine or ws_discovery"`
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

const (
	areaTimeMachine = "time_machine"
	areaWSDiscovery = "ws_discovery"
)

var timeMachineReadOperation = compatibility.Operation[Input, TimeMachine]{
	Name: "controlpanel.fileservices.servicediscovery.timemachine.read",
	Variants: []compatibility.Variant[Input, TimeMachine]{
		{
			Name: "core-fileserv-servicediscovery-v1", API: ServiceDiscoveryAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(ServiceDiscoveryAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (TimeMachine, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: ServiceDiscoveryAPIName, Version: 1, Method: "get"})
				if err != nil {
					return TimeMachine{}, fmt.Errorf("call %s.get v1: %w", ServiceDiscoveryAPIName, err)
				}
				return decodeTimeMachine(data)
			},
		},
	},
}

var timeMachineSetOperation = compatibility.Operation[TimeMachine, MutationResult]{
	Name: "controlpanel.fileservices.servicediscovery.timemachine.set",
	Variants: []compatibility.Variant[TimeMachine, MutationResult]{
		{
			Name: "core-fileserv-servicediscovery-v1", API: ServiceDiscoveryAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(ServiceDiscoveryAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired TimeMachine) (MutationResult, error) {
				parameters := map[string]any{
					"enable_smb_time_machine": desired.SMB,
					"enable_afp_time_machine": desired.AFP,
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: ServiceDiscoveryAPIName, Version: 1, Method: "set", JSONParameters: parameters}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v1: %w", ServiceDiscoveryAPIName, err)
				}
				return MutationResult{Area: areaTimeMachine}, nil
			},
		},
	},
}

var wsDiscoveryReadOperation = compatibility.Operation[Input, bool]{
	Name: "controlpanel.fileservices.servicediscovery.wsdiscovery.read",
	Variants: []compatibility.Variant[Input, bool]{
		{
			Name: "core-fileserv-wstransfer-v1", API: WSTransferAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(WSTransferAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (bool, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: WSTransferAPIName, Version: 1, Method: "get"})
				if err != nil {
					return false, fmt.Errorf("call %s.get v1: %w", WSTransferAPIName, err)
				}
				return decodeWSDiscovery(data)
			},
		},
	},
}

var wsDiscoverySetOperation = compatibility.Operation[bool, MutationResult]{
	Name: "controlpanel.fileservices.servicediscovery.wsdiscovery.set",
	Variants: []compatibility.Variant[bool, MutationResult]{
		{
			Name: "core-fileserv-wstransfer-v1", API: WSTransferAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(WSTransferAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, enabled bool) (MutationResult, error) {
				parameters := map[string]any{"enable_wstransfer": enabled}
				if _, err := executor.Execute(ctx, compatibility.Request{API: WSTransferAPIName, Version: 1, Method: "set", JSONParameters: parameters}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v1: %w", WSTransferAPIName, err)
				}
				return MutationResult{Area: areaWSDiscovery}, nil
			},
		},
	},
}

func APINames() []string {
	return []string{ServiceDiscoveryAPIName, WSTransferAPIName}
}

func SelectTimeMachineRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := timeMachineReadOperation.Select(target)
	return selection, err
}

func SelectTimeMachineSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := timeMachineSetOperation.Select(target)
	return selection, err
}

func SelectWSDiscoveryRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := wsDiscoveryReadOperation.Select(target)
	return selection, err
}

func SelectWSDiscoverySet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := wsDiscoverySetOperation.Select(target)
	return selection, err
}

func ExecuteTimeMachineRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (TimeMachine, compatibility.Selection, error) {
	return timeMachineReadOperation.Run(ctx, target, executor, Input{})
}

func ExecuteTimeMachineSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired TimeMachine) (MutationResult, compatibility.Selection, error) {
	result, selection, err := timeMachineSetOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

func ExecuteWSDiscoveryRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (bool, compatibility.Selection, error) {
	return wsDiscoveryReadOperation.Run(ctx, target, executor, Input{})
}

func ExecuteWSDiscoverySet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, enabled bool) (MutationResult, compatibility.Selection, error) {
	result, selection, err := wsDiscoverySetOperation.Run(ctx, target, executor, enabled)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}
