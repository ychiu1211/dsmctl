package application

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type TerminalStateResult struct {
	NAS      string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Terminal synology.TerminalState `json:"terminal" jsonschema:"Normalized Terminal (SSH/Telnet) state"`
}

type SNMPStateResult struct {
	NAS  string             `json:"nas" jsonschema:"NAS profile used for the request"`
	SNMP synology.SNMPState `json:"snmp" jsonschema:"Normalized SNMP state; carries no community string or SNMPv3 passwords"`
}

type TerminalSNMPCapabilitiesResult struct {
	NAS          string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.TerminalSNMPCapabilities `json:"capabilities" jsonschema:"Terminal and SNMP reads currently exposed by dsmctl"`
	Report       synology.CompatibilityReport      `json:"report" jsonschema:"Discovered APIs and selected Terminal/SNMP backends"`
}

type terminalSNMPClient interface {
	TerminalState(context.Context) (synology.TerminalState, error)
	SNMPState(context.Context) (synology.SNMPState, error)
	TerminalSNMPCapabilities(context.Context) (synology.TerminalSNMPCapabilities, synology.CompatibilityReport, error)
	ApplyTerminalChange(context.Context, synology.TerminalChange) (synology.TerminalSNMPMutationResult, error)
	ApplySNMPChange(context.Context, synology.SNMPChange, []byte) (synology.TerminalSNMPMutationResult, error)
}

func (s *Service) GetTerminalState(ctx context.Context, requestedNAS string) (TerminalStateResult, error) {
	name, client, err := s.terminalSNMPClient(ctx, requestedNAS)
	if err != nil {
		return TerminalStateResult{}, err
	}
	state, err := client.TerminalState(ctx)
	if err != nil {
		return TerminalStateResult{}, authenticationError(name, err)
	}
	return TerminalStateResult{NAS: name, Terminal: state}, nil
}

func (s *Service) GetSNMPState(ctx context.Context, requestedNAS string) (SNMPStateResult, error) {
	name, client, err := s.terminalSNMPClient(ctx, requestedNAS)
	if err != nil {
		return SNMPStateResult{}, err
	}
	state, err := client.SNMPState(ctx)
	if err != nil {
		return SNMPStateResult{}, authenticationError(name, err)
	}
	return SNMPStateResult{NAS: name, SNMP: state}, nil
}

func (s *Service) GetTerminalSNMPCapabilities(ctx context.Context, requestedNAS string) (TerminalSNMPCapabilitiesResult, error) {
	name, client, err := s.terminalSNMPClient(ctx, requestedNAS)
	if err != nil {
		return TerminalSNMPCapabilitiesResult{}, err
	}
	capabilities, report, err := client.TerminalSNMPCapabilities(ctx)
	if err != nil {
		return TerminalSNMPCapabilitiesResult{}, authenticationError(name, err)
	}
	return TerminalSNMPCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) terminalSNMPClient(ctx context.Context, requestedNAS string) (string, terminalSNMPClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(terminalSNMPClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement terminal-snmp management")
	}
	return name, client, nil
}

var _ terminalSNMPClient = (*synology.Client)(nil)
