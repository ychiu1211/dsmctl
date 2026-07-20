package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/terminalsnmp"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	terminalsnmpops "github.com/ychiu1211/dsmctl/internal/synology/operations/terminalsnmp"
)

type TerminalState = terminalsnmp.TerminalState
type SNMPState = terminalsnmp.SNMPState
type TerminalSNMPCapabilities = terminalsnmp.Capabilities

// TerminalState reads the Control Panel → Terminal tab (SSH enable + port,
// Telnet enable) without coupling to SNMP: SNMP being absent never disables it.
func (c *Client) TerminalState(ctx context.Context) (TerminalState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, terminalsnmpops.TerminalAPIName); err != nil {
		return TerminalState{}, fmt.Errorf("prepare Terminal target: %w", err)
	}
	state, _, err := terminalsnmpops.ExecuteTerminal(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return TerminalState{}, fmt.Errorf("get Terminal configuration: %w", err)
	}
	c.target.AddCapability(terminalsnmpops.TerminalReadCapabilityName)
	return state, nil
}

// SNMPState reads the Control Panel → SNMP tab. Only non-secret configuration is
// returned: the community string and any SNMPv3 auth/privacy passwords or trap
// community are never decoded (see the operation package).
func (c *Client) SNMPState(ctx context.Context) (SNMPState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, terminalsnmpops.SNMPAPIName); err != nil {
		return SNMPState{}, fmt.Errorf("prepare SNMP target: %w", err)
	}
	state, _, err := terminalsnmpops.ExecuteSNMP(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return SNMPState{}, fmt.Errorf("get SNMP configuration: %w", err)
	}
	c.target.AddCapability(terminalsnmpops.SNMPReadCapabilityName)
	return state, nil
}

// TerminalSNMPCapabilities reports which of the two independent read areas this
// NAS exposes, each selected on its own API so one missing family does not
// disable the other, plus the discovered backends.
func (c *Client) TerminalSNMPCapabilities(ctx context.Context) (TerminalSNMPCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, terminalsnmpops.APINames()...); err != nil {
		return TerminalSNMPCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare Terminal & SNMP capabilities target: %w", err)
	}
	terminalSelection, err := terminalsnmpops.SelectTerminal(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return TerminalSNMPCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Terminal backend: %w", err)
	}
	snmpSelection, err := terminalsnmpops.SelectSNMP(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return TerminalSNMPCapabilities{}, CompatibilityReport{}, fmt.Errorf("select SNMP backend: %w", err)
	}
	if terminalSelection.Supported {
		c.target.AddCapability(terminalsnmpops.TerminalReadCapabilityName)
	}
	if snmpSelection.Supported {
		c.target.AddCapability(terminalsnmpops.SNMPReadCapabilityName)
	}
	capabilities := TerminalSNMPCapabilities{
		Module:       terminalsnmp.ModuleName,
		TerminalRead: terminalSelection.Supported,
		SNMPRead:     snmpSelection.Supported,
	}
	return capabilities, c.target.Report(terminalSelection, snmpSelection), nil
}
