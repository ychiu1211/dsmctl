// Package terminalsnmp implements the independently selectable, read-only DSM
// operations for the Control Panel → Terminal & SNMP surface. Terminal and SNMP
// are two distinct DSM API families (SYNO.Core.Terminal and SYNO.Core.SNMP)
// with distinct failure boundaries: selection is per operation, so one being
// absent reports (not supported) without disabling the other, and each fails
// closed (no silent empty-success decode) when its API is missing.
//
// Verified live on DSM 7.3 (DS3018xs):
//   - SYNO.Core.Terminal v1–v3 `get` → enable_ssh, enable_telnet, ssh_port,
//     forbid_console (plus ssh cipher/kex/mac menus, which this read ignores).
//   - SYNO.Core.SNMP v1 `get` → enable_snmp, enable_snmp_v1v2, enable_snmp_v3,
//     location, contact, rocommunity (SECRET — the community string), rouser
//     (the SNMPv3 username). The community string is never decoded.
//
// The guarded writes (Terminal set: SSH enable / port / Telnet; SNMP set:
// enable / versions / device info / community + v3 credentials via a credential
// reference / trap target) are a later, explicitly-authorized slice.
package terminalsnmp

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/terminalsnmp"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	TerminalAPIName = "SYNO.Core.Terminal"
	SNMPAPIName     = "SYNO.Core.SNMP"

	TerminalReadCapabilityName = "terminalsnmp.terminal.read"
	SNMPReadCapabilityName     = "terminalsnmp.snmp.read"
)

// Input is the empty input every read takes.
type Input struct{}

var terminalOperation = compatibility.Operation[Input, terminalsnmp.TerminalState]{
	Name: TerminalReadCapabilityName,
	Variants: []compatibility.Variant[Input, terminalsnmp.TerminalState]{
		terminalReadVariant(3, 30),
		terminalReadVariant(2, 20),
		terminalReadVariant(1, 10),
	},
}

func terminalReadVariant(version, priority int) compatibility.Variant[Input, terminalsnmp.TerminalState] {
	return compatibility.Variant[Input, terminalsnmp.TerminalState]{
		Name:     fmt.Sprintf("terminal-get-v%d", version),
		API:      TerminalAPIName,
		Version:  version,
		Priority: priority,
		Match:    compatibility.APIVersion(TerminalAPIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (terminalsnmp.TerminalState, error) {
			data, err := executor.Execute(ctx, compatibility.Request{API: TerminalAPIName, Version: version, Method: "get", ReadOnly: true})
			if err != nil {
				return terminalsnmp.TerminalState{}, fmt.Errorf("call %s.get v%d: %w", TerminalAPIName, version, err)
			}
			return decodeTerminal(data)
		},
	}
}

var snmpOperation = compatibility.Operation[Input, terminalsnmp.SNMPState]{
	Name: SNMPReadCapabilityName,
	Variants: []compatibility.Variant[Input, terminalsnmp.SNMPState]{
		{
			Name: "snmp-get-v1", API: SNMPAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(SNMPAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (terminalsnmp.SNMPState, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: SNMPAPIName, Version: 1, Method: "get", ReadOnly: true})
				if err != nil {
					return terminalsnmp.SNMPState{}, fmt.Errorf("call %s.get v1: %w", SNMPAPIName, err)
				}
				return decodeSNMP(data)
			},
		},
	},
}

// APINames lists every DSM API this module may use so the facade can discover
// them in one call before selecting variants.
func APINames() []string {
	return []string{TerminalAPIName, SNMPAPIName}
}

func SelectTerminal(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := terminalOperation.Select(target)
	return selection, err
}

func SelectSNMP(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := snmpOperation.Select(target)
	return selection, err
}

func ExecuteTerminal(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (terminalsnmp.TerminalState, compatibility.Selection, error) {
	return terminalOperation.Run(ctx, target, executor, Input{})
}

func ExecuteSNMP(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (terminalsnmp.SNMPState, compatibility.Selection, error) {
	return snmpOperation.Run(ctx, target, executor, Input{})
}
