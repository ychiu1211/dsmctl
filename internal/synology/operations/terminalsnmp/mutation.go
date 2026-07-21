package terminalsnmp

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// The write wire is live-verified on the DSM 7.3 lab (DS3018xs, throwaway raw
// probes, every mutation reverted to baseline). The confirmed set shapes are:
//
//	SYNO.Core.Terminal  set  v3  {enable_ssh(bool), enable_telnet(bool), ssh_port(int), forbid_console(bool)}
//	SYNO.Core.SNMP      set  v1  {enable_snmp(bool), enable_snmp_v1v2(bool), enable_snmp_v3(bool),
//	                              location(string), contact(string), rocommunity(string SECRET), rouser(string)}
//
// Both setters mirror their get field names exactly (get/set symmetry), confirmed
// by a no-op set of the observed values (nothing changed) followed by a real
// round-trip (Terminal ssh_port 22->2222->22; SNMP enable v1/v2c with a throwaway
// community, then disable). The caller merges its patch into the freshly read
// state first, so an unspecified switch is never silently reset.
//
// DSM quirks confirmed live:
//   - SNMP.set silently ignores an EMPTY-string field while the service is
//     disabled (location/contact/rocommunity keep their old value); an empty
//     string clears location/contact only while SNMP is enabled. The postcondition
//     re-read is what catches an ignored write.
//   - SNMP.set returns code 2202 when a required secret is missing: enabling
//     v1/v2c with no community configured, or enabling v3 with no auth passphrase.
//
// WIRE-UNVERIFIED (deliberately NOT written by this module):
//   - The SNMPv3 auth/privacy password set-field names. Enabling v3 returns 2202
//     for every candidate field name tried (auth_passwd/priv_passwd,
//     authpass/privpass, V3_auth_passwd/V3_privacy_passwd, snmpv3_auth_passwd,
//     ...), and the module admin JS was not fetchable to confirm them. The write
//     layer therefore refuses enabling v3; only disabling v3 is supported.
//   - The SNMP trap target. No trap field appears in the SNMP get response even
//     while the service is enabled, so a trap write cannot be confirmed by a
//     postcondition re-read and is not exposed.
const (
	TerminalWriteCapabilityName = "terminalsnmp.terminal.write"
	SNMPWriteCapabilityName     = "terminalsnmp.snmp.write"
)

// MutationResult records the DSM backend that accepted a terminal-snmp write.
type MutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

// TerminalSetInput is the complete desired Terminal configuration submitted to
// SYNO.Core.Terminal.set. The caller merges its patch into the freshly read
// state so every field here reflects the intended end state.
type TerminalSetInput struct {
	SSHEnabled       bool
	SSHPort          int
	TelnetEnabled    bool
	ConsoleForbidden bool
}

// SNMPSetInput is the complete desired SNMP configuration submitted to
// SYNO.Core.SNMP.set. Community holds the resolved read-community SECRET; it is
// sent as the rocommunity field ONLY when non-nil and NOWHERE else, and the
// caller zeroizes it right after this returns. V3User is the non-secret v3
// username preserved from the fresh read.
type SNMPSetInput struct {
	Enabled      bool
	V1V2cEnabled bool
	V3Enabled    bool
	Location     string
	Contact      string
	V3User       string
	Community    []byte // rocommunity — SECRET; sent only when non-nil
}

var terminalSetOperation = compatibility.Operation[TerminalSetInput, MutationResult]{
	Name: TerminalWriteCapabilityName,
	Variants: []compatibility.Variant[TerminalSetInput, MutationResult]{
		terminalSetVariant(3, 30),
		terminalSetVariant(2, 20),
		terminalSetVariant(1, 10),
	},
}

func terminalSetVariant(version, priority int) compatibility.Variant[TerminalSetInput, MutationResult] {
	return compatibility.Variant[TerminalSetInput, MutationResult]{
		Name:     fmt.Sprintf("terminal-set-v%d", version),
		API:      TerminalAPIName,
		Version:  version,
		Priority: priority,
		Match:    compatibility.APIVersion(TerminalAPIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, desired TerminalSetInput) (MutationResult, error) {
			params := map[string]any{
				"enable_ssh":     desired.SSHEnabled,
				"enable_telnet":  desired.TelnetEnabled,
				"ssh_port":       desired.SSHPort,
				"forbid_console": desired.ConsoleForbidden,
			}
			if _, err := executor.Execute(ctx, compatibility.Request{API: TerminalAPIName, Version: version, Method: "set", JSONParameters: params}); err != nil {
				return MutationResult{}, fmt.Errorf("call %s.set v%d: %w", TerminalAPIName, version, err)
			}
			return MutationResult{}, nil
		},
	}
}

var snmpSetOperation = compatibility.Operation[SNMPSetInput, MutationResult]{
	Name: SNMPWriteCapabilityName,
	Variants: []compatibility.Variant[SNMPSetInput, MutationResult]{
		{
			Name: "snmp-set-v1", API: SNMPAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(SNMPAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired SNMPSetInput) (MutationResult, error) {
				params := map[string]any{
					"enable_snmp":      desired.Enabled,
					"enable_snmp_v1v2": desired.V1V2cEnabled,
					"enable_snmp_v3":   desired.V3Enabled,
					"location":         desired.Location,
					"contact":          desired.Contact,
				}
				// rouser is the non-secret v3 username; preserve it only when present.
				if desired.V3User != "" {
					params["rouser"] = desired.V3User
				}
				// rocommunity is the SECRET read community. Send it ONLY when the
				// caller supplied a fresh value; otherwise omit the key so DSM keeps
				// the configured community (patch semantics) rather than receiving an
				// empty string it would reject/ignore.
				if desired.Community != nil {
					params["rocommunity"] = string(desired.Community)
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: SNMPAPIName, Version: 1, Method: "set", JSONParameters: params}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v1: %w", SNMPAPIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

// SelectTerminalSet reports whether the guarded Terminal write rides an
// advertised backend.
func SelectTerminalSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := terminalSetOperation.Select(target)
	return selection, err
}

// SelectSNMPSet reports whether the guarded SNMP write rides an advertised backend.
func SelectSNMPSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := snmpSetOperation.Select(target)
	return selection, err
}

// ExecuteTerminalSet submits the complete desired Terminal configuration.
func ExecuteTerminalSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired TerminalSetInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := terminalSetOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

// ExecuteSNMPSet submits the complete desired SNMP configuration. The community
// SECRET, when present, rides only the set request body.
func ExecuteSNMPSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired SNMPSetInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := snmpSetOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}
