package systeminfo

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	APIName        = "SYNO.Core.System"
	CapabilityName = "system.info"
	OperationName  = "system.info"
)

type Input struct{}

type Info struct {
	Hostname        string   `json:"hostname,omitempty" jsonschema:"NAS hostname"`
	Model           string   `json:"model,omitempty" jsonschema:"Synology model name"`
	Serial          string   `json:"serial,omitempty" jsonschema:"Device serial number"`
	DSMVersion      string   `json:"dsm_version,omitempty" jsonschema:"Installed DSM version"`
	CPU             string   `json:"cpu,omitempty" jsonschema:"CPU description"`
	CPUCores        int      `json:"cpu_cores,omitempty" jsonschema:"Number of CPU cores"`
	MemoryMiB       int64    `json:"memory_mib,omitempty" jsonschema:"Installed memory in MiB"`
	Uptime          string   `json:"uptime,omitempty" jsonschema:"NAS uptime reported by DSM"`
	TimeZone        string   `json:"time_zone,omitempty" jsonschema:"Configured time zone"`
	TemperatureC    *float64 `json:"temperature_c,omitempty" jsonschema:"System temperature in Celsius"`
	TemperatureWarn bool     `json:"temperature_warning,omitempty" jsonschema:"Whether DSM reports a temperature warning"`
}

var operation = compatibility.Operation[Input, Info]{
	Name: OperationName,
	Variants: []compatibility.Variant[Input, Info]{
		coreVariant("core-system-v3", 3, 30, currentFields),
		coreVariant("core-system-v2", 2, 20, currentFields),
		coreVariant("core-system-v1-legacy", 1, 10, legacyFields),
	},
}

func APINames() []string {
	return operation.APINames()
}

func Select(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := operation.Select(target)
	return selection, err
}

func Execute(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (Info, compatibility.Selection, error) {
	return operation.Run(ctx, target, executor, Input{})
}

func coreVariant(name string, version, priority int, fields fieldAliases) compatibility.Variant[Input, Info] {
	return compatibility.Variant[Input, Info]{
		Name:     name,
		API:      APIName,
		Version:  version,
		Priority: priority,
		Match:    compatibility.APIVersion(APIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (Info, error) {
			data, err := executor.Execute(ctx, compatibility.Request{
				API:     APIName,
				Version: version,
				Method:  "info",
			})
			if err != nil {
				return Info{}, fmt.Errorf("call %s.info v%d: %w", APIName, version, err)
			}
			return decode(data, fields)
		},
	}
}
