package driveadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/driveadmin"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// ConfigAPIName is the Drive server database configuration API.
const ConfigAPIName = "SYNO.SynologyDrive.Config"

const (
	ConfigReadCapabilityName = "drive.admin.config.read"
	ConfigSetCapabilityName  = "drive.admin.config.set"
)

// ConfigSetInput is the merged vmtouch pair the set submits. DSM couples the
// enable flag and the reserved memory, so both are always sent.
type ConfigSetInput struct {
	VMTouchEnabled    bool
	VMTouchReserveMem int
}

// ConfigMutationResult records the selected backend for one config set.
type ConfigMutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

var configReadOperation = compatibility.Operation[Input, driveadmin.ServerConfig]{
	Name: ConfigReadCapabilityName,
	Variants: []compatibility.Variant[Input, driveadmin.ServerConfig]{
		{
			Name: "drive-config-v1", API: ConfigAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(ConfigAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (driveadmin.ServerConfig, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: ConfigAPIName, Version: 1, Method: "get"})
				if err != nil {
					return driveadmin.ServerConfig{}, fmt.Errorf("call %s.get v1: %w", ConfigAPIName, err)
				}
				return decodeServerConfig(data)
			},
		},
	},
}

var configSetOperation = compatibility.Operation[ConfigSetInput, ConfigMutationResult]{
	Name: ConfigSetCapabilityName,
	Variants: []compatibility.Variant[ConfigSetInput, ConfigMutationResult]{
		{
			Name: "drive-config-v1", API: ConfigAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(ConfigAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, input ConfigSetInput) (ConfigMutationResult, error) {
				parameters := map[string]any{
					"enable_vmtouch":      input.VMTouchEnabled,
					"vmtouch_reserve_mem": input.VMTouchReserveMem,
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: ConfigAPIName, Version: 1, Method: "set", JSONParameters: parameters}); err != nil {
					return ConfigMutationResult{}, fmt.Errorf("call %s.set v1: %w", ConfigAPIName, err)
				}
				return ConfigMutationResult{}, nil
			},
		},
	},
}

func SelectConfigRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := configReadOperation.Select(target)
	return selection, err
}

func SelectConfigSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := configSetOperation.Select(target)
	return selection, err
}

func ExecuteConfigRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (driveadmin.ServerConfig, compatibility.Selection, error) {
	return configReadOperation.Run(ctx, target, executor, Input{})
}

func ExecuteConfigSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input ConfigSetInput) (ConfigMutationResult, compatibility.Selection, error) {
	result, selection, err := configSetOperation.Run(ctx, target, executor, input)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

func decodeServerConfig(data json.RawMessage) (driveadmin.ServerConfig, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return driveadmin.ServerConfig{}, fmt.Errorf("decode Drive config: expected a non-empty object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return driveadmin.ServerConfig{}, fmt.Errorf("decode Drive config: %w", err)
	}
	enabled, ok := configBool(raw, "enable_vmtouch")
	if !ok {
		return driveadmin.ServerConfig{}, fmt.Errorf("decode Drive config: required field \"enable_vmtouch\" is missing or not boolean")
	}
	return driveadmin.ServerConfig{
		VolumePath:        configString(raw, "volume_select"),
		VMTouchEnabled:    enabled,
		VMTouchReserveMem: configInt(raw, "vmtouch_reserve_mem"),
	}, nil
}

func configBool(raw map[string]json.RawMessage, name string) (bool, bool) {
	value, ok := raw[name]
	if !ok {
		return false, false
	}
	var b bool
	if err := json.Unmarshal(value, &b); err == nil {
		return b, true
	}
	var integer int
	if err := json.Unmarshal(value, &integer); err == nil && (integer == 0 || integer == 1) {
		return integer == 1, true
	}
	return false, false
}

func configString(raw map[string]json.RawMessage, name string) string {
	if value, ok := raw[name]; ok {
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			return text
		}
	}
	return ""
}

func configInt(raw map[string]json.RawMessage, name string) int {
	if value, ok := raw[name]; ok {
		var n int
		if err := json.Unmarshal(value, &n); err == nil {
			return n
		}
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			if parsed, convErr := strconv.Atoi(strings.TrimSpace(text)); convErr == nil {
				return parsed
			}
		}
	}
	return 0
}
