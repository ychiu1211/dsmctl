// Package securityadvisor implements the read-only DSM operations for the
// Control Panel → Security → Security Advisor surface. Security Advisor is DSM
// core (not a package), so selection uses the advertised API/version alone and
// each API is its own independent compatibility boundary.
//
// Verified live on DSM 7.3 (SYNO.API.Info.query): three v1 APIs make up the
// family, all JSON-request on entry.cgi —
//
//	SYNO.Core.SecurityScan.Status    system_get → last-scan status + per-category findings
//	SYNO.Core.SecurityScan.Conf      get        → scan schedule + security baseline
//	SYNO.Core.SecurityScan.Operation (scan trigger / cancel) — DEFERRED write/action, never called here
//
// The detailed per-rule lookup (Status.rule_get) exists but requires a specific
// rule id and is not an enumerable findings list on this release, so the read
// slice normalizes findings at DSM's per-category granularity, which is what
// Status.system_get exposes.
package securityadvisor

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/domain/securityadvisor"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	// StatusAPIName reads the last-scan status and per-category findings.
	StatusAPIName = "SYNO.Core.SecurityScan.Status"
	// ConfAPIName reads (and, in a deferred slice, writes) the schedule + baseline.
	ConfAPIName = "SYNO.Core.SecurityScan.Conf"
	// OperationAPIName triggers/cancels a scan. It is the deferred run-scan
	// action API; this read slice discovers it for capability reporting only and
	// never invokes it, because a full scan is CPU/IO-heavy on the NAS.
	OperationAPIName = "SYNO.Core.SecurityScan.Operation"

	StatusReadCapabilityName   = "securityadvisor.status.read"
	ScheduleReadCapabilityName = "securityadvisor.schedule.read"
)

// Input is the empty input for the parameterless reads.
type Input struct{}

var statusOperation = compatibility.Operation[Input, securityadvisor.Status]{
	Name: StatusReadCapabilityName,
	Variants: []compatibility.Variant[Input, securityadvisor.Status]{
		{
			Name: "securityscan-status-system-get-v1", API: StatusAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(StatusAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (securityadvisor.Status, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: StatusAPIName, Version: 1, Method: "system_get", ReadOnly: true})
				if err != nil {
					return securityadvisor.Status{}, err
				}
				return decodeStatus(data)
			},
		},
	},
}

var configurationOperation = compatibility.Operation[Input, securityadvisor.Configuration]{
	Name: ScheduleReadCapabilityName,
	Variants: []compatibility.Variant[Input, securityadvisor.Configuration]{
		{
			Name: "securityscan-conf-get-v1", API: ConfAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(ConfAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (securityadvisor.Configuration, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: ConfAPIName, Version: 1, Method: "get", ReadOnly: true})
				if err != nil {
					return securityadvisor.Configuration{}, err
				}
				return decodeConfiguration(data)
			},
		},
	},
}

// APINames lists every DSM API this module may discover so the facade can
// resolve them in one SYNO.API.Info call before selecting variants. It includes
// the deferred Operation API so capabilities can report run-scan availability.
func APINames() []string {
	return []string{StatusAPIName, ConfAPIName, OperationAPIName}
}

func SelectStatus(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := statusOperation.Select(target)
	return selection, err
}

func ExecuteStatus(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (securityadvisor.Status, compatibility.Selection, error) {
	return statusOperation.Run(ctx, target, executor, Input{})
}

func SelectConfiguration(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := configurationOperation.Select(target)
	return selection, err
}

func ExecuteConfiguration(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (securityadvisor.Configuration, compatibility.Selection, error) {
	return configurationOperation.Run(ctx, target, executor, Input{})
}

// SupportsRunScan reports whether the deferred run-scan action API is advertised.
func SupportsRunScan(target compatibility.Target) bool {
	return target.SupportsAPI(OperationAPIName, 1)
}

// SupportsScheduleWrite reports whether the deferred schedule/baseline write
// rides an advertised API. The write is Conf `set`, the same API/version as the
// schedule read.
func SupportsScheduleWrite(target compatibility.Target) bool {
	return target.SupportsAPI(ConfAPIName, 1)
}
