// Package tftpservice implements the DSM TFTP service operations behind
// SYNO.Core.TFTP v1. Confirmed live on DSM 7.3.2, get returns the same field
// names the set uses (enable, enable_log, startip/endip, permission, root_path,
// timeout) and returns the complete configuration without needing an "additional"
// selector, so this package reads with a plain get.
package tftpservice

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	APIName = "SYNO.Core.TFTP"

	ReadCapabilityName = "controlpanel.fileservices.tftp.read"
	SetCapabilityName  = "controlpanel.fileservices.tftp.set"
)

type Input struct{}

// Settings is the observed TFTP configuration. AllowWrite mirrors DSM's
// permission ("rw" = true, "r" = false).
type Settings struct {
	Enabled      bool
	RootPath     string
	AllowWrite   bool
	LogEnabled   bool
	ClientIPLow  string
	ClientIPHigh string
	Timeout      int
}

// Patch is a partial TFTP update; a nil field is not sent, so DSM preserves it.
type Patch struct {
	Enabled    *bool
	RootPath   *string
	AllowWrite *bool
	LogEnabled *bool
	Timeout    *int
}

func (p Patch) empty() bool {
	return p.Enabled == nil && p.RootPath == nil && p.AllowWrite == nil && p.LogEnabled == nil && p.Timeout == nil
}

// MutationResult records the selected backend for one set.
type MutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

var readOperation = compatibility.Operation[Input, Settings]{
	Name: "controlpanel.fileservices.tftp.read",
	Variants: []compatibility.Variant[Input, Settings]{
		{
			Name: "core-tftp-v1", API: APIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (Settings, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: APIName, Version: 1, Method: "get"})
				if err != nil {
					return Settings{}, fmt.Errorf("call %s.get v1: %w", APIName, err)
				}
				return decodeSettings(data)
			},
		},
	},
}

var setOperation = compatibility.Operation[Patch, MutationResult]{
	Name: "controlpanel.fileservices.tftp.set",
	Variants: []compatibility.Variant[Patch, MutationResult]{
		{
			Name: "core-tftp-v1", API: APIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, patch Patch) (MutationResult, error) {
				parameters := map[string]any{}
				if patch.Enabled != nil {
					parameters["enable"] = *patch.Enabled
				}
				if patch.RootPath != nil {
					parameters["root_path"] = *patch.RootPath
				}
				if patch.AllowWrite != nil {
					if *patch.AllowWrite {
						parameters["permission"] = "rw"
					} else {
						parameters["permission"] = "r"
					}
				}
				if patch.LogEnabled != nil {
					parameters["enable_log"] = *patch.LogEnabled
				}
				if patch.Timeout != nil {
					parameters["timeout"] = *patch.Timeout
				}
				if len(parameters) == 0 {
					return MutationResult{}, fmt.Errorf("tftp set: empty patch")
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: APIName, Version: 1, Method: "set", JSONParameters: parameters}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v1: %w", APIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

func APINames() []string { return []string{APIName} }

func SelectRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := readOperation.Select(target)
	return selection, err
}

func SelectSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := setOperation.Select(target)
	return selection, err
}

func ExecuteRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (Settings, compatibility.Selection, error) {
	return readOperation.Run(ctx, target, executor, Input{})
}

func ExecuteSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, patch Patch) (MutationResult, compatibility.Selection, error) {
	if patch.empty() {
		return MutationResult{}, compatibility.Selection{}, fmt.Errorf("tftp set: empty patch")
	}
	result, selection, err := setOperation.Run(ctx, target, executor, patch)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}
