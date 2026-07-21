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
type TerminalChange = terminalsnmp.TerminalChange
type SNMPChange = terminalsnmp.SNMPChange
type TerminalSNMPMutationResult = terminalsnmpops.MutationResult

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
	terminalSetSelection, err := selectSupported(terminalsnmpops.SelectTerminalSet, c.target)
	if err != nil {
		return TerminalSNMPCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Terminal write backend: %w", err)
	}
	snmpSetSelection, err := selectSupported(terminalsnmpops.SelectSNMPSet, c.target)
	if err != nil {
		return TerminalSNMPCapabilities{}, CompatibilityReport{}, fmt.Errorf("select SNMP write backend: %w", err)
	}
	if terminalSelection.Supported {
		c.target.AddCapability(terminalsnmpops.TerminalReadCapabilityName)
	}
	if snmpSelection.Supported {
		c.target.AddCapability(terminalsnmpops.SNMPReadCapabilityName)
	}
	if terminalSetSelection.Supported {
		c.target.AddCapability(terminalsnmpops.TerminalWriteCapabilityName)
	}
	if snmpSetSelection.Supported {
		c.target.AddCapability(terminalsnmpops.SNMPWriteCapabilityName)
	}
	capabilities := TerminalSNMPCapabilities{
		Module:        terminalsnmp.ModuleName,
		TerminalRead:  terminalSelection.Supported,
		SNMPRead:      snmpSelection.Supported,
		TerminalWrite: terminalSetSelection.Supported,
		SNMPWrite:     snmpSetSelection.Supported,
	}
	return capabilities, c.target.Report(terminalSelection, snmpSelection, terminalSetSelection, snmpSetSelection), nil
}

// ApplyTerminalChange merges the patch into a freshly read complete Terminal
// state and submits it as one set, so a switch the caller did not specify can
// never be silently reset. dsmctl drives DSM over the WebAPI session (not SSH),
// so its own connectivity survives an SSH/port/console change.
func (c *Client) ApplyTerminalChange(ctx context.Context, change TerminalChange) (TerminalSNMPMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, terminalsnmpops.TerminalAPIName); err != nil {
		return TerminalSNMPMutationResult{}, fmt.Errorf("prepare Terminal mutation target: %w", err)
	}
	current, _, err := terminalsnmpops.ExecuteTerminal(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return TerminalSNMPMutationResult{}, fmt.Errorf("refresh Terminal state before apply: %w", err)
	}
	desired := mergeTerminalChange(current, change)
	result, _, err := terminalsnmpops.ExecuteTerminalSet(ctx, c.target, lockedExecutor{client: c}, desired)
	if err != nil {
		return TerminalSNMPMutationResult{}, fmt.Errorf("apply Terminal configuration: %w", err)
	}
	c.target.AddCapability(terminalsnmpops.TerminalWriteCapabilityName)
	return result, nil
}

// ApplySNMPChange merges the non-secret patch into a freshly read complete SNMP
// state and submits it as one set. community holds the resolved read-community
// SECRET, or nil to keep the currently configured community; it rides only the
// set request body and is never read back, logged, or returned. The caller
// (application layer) resolves the credential reference and zeroizes community
// right after this returns.
func (c *Client) ApplySNMPChange(ctx context.Context, change SNMPChange, community []byte) (TerminalSNMPMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, terminalsnmpops.SNMPAPIName); err != nil {
		return TerminalSNMPMutationResult{}, fmt.Errorf("prepare SNMP mutation target: %w", err)
	}
	current, _, err := terminalsnmpops.ExecuteSNMP(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return TerminalSNMPMutationResult{}, fmt.Errorf("refresh SNMP state before apply: %w", err)
	}
	desired := mergeSNMPChange(current, change)
	desired.Community = community
	result, _, err := terminalsnmpops.ExecuteSNMPSet(ctx, c.target, lockedExecutor{client: c}, desired)
	if err != nil {
		return TerminalSNMPMutationResult{}, fmt.Errorf("apply SNMP configuration: %w", err)
	}
	c.target.AddCapability(terminalsnmpops.SNMPWriteCapabilityName)
	return result, nil
}

func mergeTerminalChange(current TerminalState, change TerminalChange) terminalsnmpops.TerminalSetInput {
	desired := terminalsnmpops.TerminalSetInput{
		SSHEnabled:       current.SSHEnabled,
		SSHPort:          current.SSHPort,
		TelnetEnabled:    current.TelnetEnabled,
		ConsoleForbidden: current.ConsoleForbidden,
	}
	if change.SSHEnabled != nil {
		desired.SSHEnabled = *change.SSHEnabled
	}
	if change.SSHPort != nil {
		desired.SSHPort = *change.SSHPort
	}
	if change.TelnetEnabled != nil {
		desired.TelnetEnabled = *change.TelnetEnabled
	}
	if change.ConsoleForbidden != nil {
		desired.ConsoleForbidden = *change.ConsoleForbidden
	}
	return desired
}

// mergeSNMPChange merges the non-secret patch fields into the freshly read state.
// The community secret is never read back, so it is applied separately by the
// caller (ApplySNMPChange) via the resolved bytes, not merged here.
func mergeSNMPChange(current SNMPState, change SNMPChange) terminalsnmpops.SNMPSetInput {
	desired := terminalsnmpops.SNMPSetInput{
		Enabled:      current.Enabled,
		V1V2cEnabled: current.V1V2cEnabled,
		V3Enabled:    current.V3Enabled,
		Location:     current.Location,
		Contact:      current.Contact,
		V3User:       current.V3User,
	}
	if change.Enabled != nil {
		desired.Enabled = *change.Enabled
	}
	if change.V1V2cEnabled != nil {
		desired.V1V2cEnabled = *change.V1V2cEnabled
	}
	if change.V3Enabled != nil {
		desired.V3Enabled = *change.V3Enabled
	}
	if change.Location != nil {
		desired.Location = *change.Location
	}
	if change.Contact != nil {
		desired.Contact = *change.Contact
	}
	return desired
}
