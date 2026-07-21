package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/domain/terminalsnmp"
	"github.com/ychiu1211/dsmctl/internal/runtime"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type fakeTerminalSNMPClient struct {
	terminal     synology.TerminalState
	snmp         synology.SNMPState
	capabilities synology.TerminalSNMPCapabilities
	persist      bool

	terminalMutations int
	snmpMutations     int
	// receivedCommunity retains the SAME slice passed to ApplySNMPChange so the
	// test can assert it is zeroized by the caller after the wire call returns.
	receivedCommunity        []byte
	receivedCommunityString  string
	receivedCommunityPresent bool
}

func (c *fakeTerminalSNMPClient) TerminalState(context.Context) (synology.TerminalState, error) {
	return c.terminal, nil
}
func (c *fakeTerminalSNMPClient) SNMPState(context.Context) (synology.SNMPState, error) {
	return c.snmp, nil
}
func (c *fakeTerminalSNMPClient) TerminalSNMPCapabilities(context.Context) (synology.TerminalSNMPCapabilities, synology.CompatibilityReport, error) {
	return c.capabilities, synology.CompatibilityReport{}, nil
}

func (c *fakeTerminalSNMPClient) ApplyTerminalChange(_ context.Context, change synology.TerminalChange) (synology.TerminalSNMPMutationResult, error) {
	c.terminalMutations++
	if c.persist {
		if change.SSHEnabled != nil {
			c.terminal.SSHEnabled = *change.SSHEnabled
		}
		if change.SSHPort != nil {
			c.terminal.SSHPort = *change.SSHPort
		}
		if change.TelnetEnabled != nil {
			c.terminal.TelnetEnabled = *change.TelnetEnabled
		}
		if change.ConsoleForbidden != nil {
			c.terminal.ConsoleForbidden = *change.ConsoleForbidden
		}
	}
	return synology.TerminalSNMPMutationResult{Backend: "terminal-set-v3", API: "SYNO.Core.Terminal", Version: 3, Method: "set"}, nil
}

func (c *fakeTerminalSNMPClient) ApplySNMPChange(_ context.Context, change synology.SNMPChange, community []byte) (synology.TerminalSNMPMutationResult, error) {
	c.snmpMutations++
	c.receivedCommunity = community
	c.receivedCommunityPresent = community != nil
	c.receivedCommunityString = string(community)
	if c.persist {
		if change.Enabled != nil {
			c.snmp.Enabled = *change.Enabled
		}
		if change.V1V2cEnabled != nil {
			c.snmp.V1V2cEnabled = *change.V1V2cEnabled
		}
		if change.V3Enabled != nil {
			c.snmp.V3Enabled = *change.V3Enabled
		}
		if change.Location != nil {
			c.snmp.Location = *change.Location
		}
		if change.Contact != nil {
			c.snmp.Contact = *change.Contact
		}
		if community != nil {
			c.snmp.CommunityConfigured = true
		}
	}
	return synology.TerminalSNMPMutationResult{Backend: "snmp-set-v1", API: "SYNO.Core.SNMP", Version: 1, Method: "set"}, nil
}

var _ terminalSNMPClient = (*fakeTerminalSNMPClient)(nil)

func terminalSNMPTestClient() *fakeTerminalSNMPClient {
	return &fakeTerminalSNMPClient{
		terminal: synology.TerminalState{SSHEnabled: true, SSHPort: 22, TelnetEnabled: false, ConsoleForbidden: false},
		snmp:     synology.SNMPState{Enabled: false},
		capabilities: synology.TerminalSNMPCapabilities{
			Module: terminalsnmp.ModuleName, TerminalRead: true, SNMPRead: true, TerminalWrite: true, SNMPWrite: true,
		},
		persist: true,
	}
}

func terminalSNMPTestService() *Service {
	cfg := config.New()
	manager := runtime.NewManager(cfg, credentials.NewEnvironment())
	return NewService(cfg, manager)
}

// ---- Terminal ----

func TestTerminalPlanApplyPortChange(t *testing.T) {
	client := terminalSNMPTestClient()
	plan, err := planTerminalWithClient(context.Background(), "lab", client, terminalsnmp.TerminalChange{SSHPort: intPtr(2222)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" || plan.Hash == "" || plan.ObservedFingerprint == "" {
		t.Fatalf("plan = %#v", plan)
	}
	if !strings.Contains(strings.Join(plan.Summary, " "), "move SSH from port 22 to port 2222") {
		t.Fatalf("summary = %#v", plan.Summary)
	}
	if !strings.Contains(strings.Join(plan.Warnings, " "), "firewall") {
		t.Fatalf("port-change plan must warn about the firewall/port forward: %#v", plan.Warnings)
	}
	result, err := applyTerminalPlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if !result.Applied || client.terminalMutations != 1 || client.terminal.SSHPort != 2222 {
		t.Fatalf("result=%#v state=%#v", result, client.terminal)
	}
}

func TestTerminalEnablingIsHigh(t *testing.T) {
	cases := []struct {
		name  string
		start synology.TerminalState
		req   terminalsnmp.TerminalChange
		want  string
	}{
		{"enable ssh", synology.TerminalState{SSHEnabled: false, SSHPort: 22}, terminalsnmp.TerminalChange{SSHEnabled: boolPtr(true)}, "opens a remote shell"},
		{"enable telnet", synology.TerminalState{SSHEnabled: true, SSHPort: 22}, terminalsnmp.TerminalChange{TelnetEnabled: boolPtr(true)}, "cleartext"},
		{"disable ssh", synology.TerminalState{SSHEnabled: true, SSHPort: 22}, terminalsnmp.TerminalChange{SSHEnabled: boolPtr(false)}, "strand"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := terminalSNMPTestClient()
			client.terminal = tc.start
			plan, err := planTerminalWithClient(context.Background(), "lab", client, tc.req)
			if err != nil {
				t.Fatalf("plan error = %v", err)
			}
			if plan.Risk != "high" {
				t.Fatalf("risk = %q, want high", plan.Risk)
			}
			if !strings.Contains(strings.Join(plan.Warnings, "\n"), tc.want) {
				t.Fatalf("warnings = %#v, want substring %q", plan.Warnings, tc.want)
			}
		})
	}
}

func TestTerminalConsoleAndPortAreMedium(t *testing.T) {
	client := terminalSNMPTestClient()
	plan, err := planTerminalWithClient(context.Background(), "lab", client, terminalsnmp.TerminalChange{ConsoleForbidden: boolPtr(true), SSHPort: intPtr(2020)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" {
		t.Fatalf("risk = %q, want medium", plan.Risk)
	}
}

func TestTerminalPlanStaleRejection(t *testing.T) {
	client := terminalSNMPTestClient()
	plan, err := planTerminalWithClient(context.Background(), "lab", client, terminalsnmp.TerminalChange{SSHPort: intPtr(2222)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	// The observed Terminal state changes out from under the plan.
	client.terminal.SSHPort = 2000
	if _, err := applyTerminalPlanWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale rejection, got %v", err)
	}
}

func TestTerminalEmptyAndNoOpRejected(t *testing.T) {
	client := terminalSNMPTestClient()
	if _, err := planTerminalWithClient(context.Background(), "lab", client, terminalsnmp.TerminalChange{}); err == nil {
		t.Fatal("empty terminal patch must be rejected")
	}
	// A patch equal to the observed state is a no-op.
	if _, err := planTerminalWithClient(context.Background(), "lab", client, terminalsnmp.TerminalChange{SSHPort: intPtr(22)}); err == nil || !strings.Contains(err.Error(), "would not change") {
		t.Fatalf("no-op terminal patch must be rejected, got %v", err)
	}
}

// ---- SNMP ----

func TestSNMPValidationRejectsV3EnableAndMissingCommunity(t *testing.T) {
	client := terminalSNMPTestClient()
	// Enabling v3 is WIRE-UNVERIFIED and rejected.
	if _, err := planSNMPWithClient(context.Background(), "lab", client, terminalsnmp.SNMPChange{V3Enabled: boolPtr(true)}); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("enabling v3 must be rejected, got %v", err)
	}
	// Enabling v1/v2c with no community configured and none supplied is rejected.
	req := terminalsnmp.SNMPChange{Enabled: boolPtr(true), V1V2cEnabled: boolPtr(true)}
	if _, err := planSNMPWithClient(context.Background(), "lab", client, req); err == nil || !strings.Contains(err.Error(), "community") {
		t.Fatalf("enabling v1/v2c without a community must be rejected, got %v", err)
	}
	// A literal community (not env:NAME) is rejected at shape validation.
	if err := validateSNMPShape(terminalsnmp.SNMPChange{CommunityCredentialRef: "hunter2"}); err == nil || !strings.Contains(err.Error(), "env:NAME") {
		t.Fatalf("literal community must be rejected, got %v", err)
	}
}

// TestSNMPPlanApplyCommunitySecretHygiene is the mandatory credential_ref
// request-capture: the resolved community must appear ONLY in the wire request
// (the client's ApplySNMPChange community argument), never in the plan, the
// approval hash input, or the result, and must be zeroized after apply.
func TestSNMPPlanApplyCommunitySecretHygiene(t *testing.T) {
	const secret = "CANARY-COMMUNITY-do-not-leak-9f8e"
	t.Setenv("WI071_TEST_COMMUNITY", secret)
	service := terminalSNMPTestService()
	client := terminalSNMPTestClient()

	req := terminalsnmp.SNMPChange{
		Enabled:                boolPtr(true),
		V1V2cEnabled:           boolPtr(true),
		Location:               strPtr("MDF rack 3"),
		CommunityCredentialRef: "env:WI071_TEST_COMMUNITY",
	}
	plan, err := planSNMPWithClient(context.Background(), "lab", client, req)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" {
		t.Fatalf("risk = %q, want medium", plan.Risk)
	}
	// The plan (and thus the approval hash input) must never carry the secret; the
	// reference NAME is allowed.
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(planJSON), secret) {
		t.Fatalf("plan leaked the community secret: %s", planJSON)
	}
	if !strings.Contains(string(planJSON), "env:WI071_TEST_COMMUNITY") {
		t.Fatalf("plan must record the credential reference name: %s", planJSON)
	}

	result, err := service.applySNMPPlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if !result.Applied || client.snmpMutations != 1 {
		t.Fatalf("result = %#v mutations=%d", result, client.snmpMutations)
	}
	// The secret reached the wire request exactly once, via the community argument.
	if !client.receivedCommunityPresent || client.receivedCommunityString != secret {
		t.Fatalf("resolved community did not ride the wire request: present=%v", client.receivedCommunityPresent)
	}
	// ... and was zeroized by the caller right after the wire call returned.
	for i, b := range client.receivedCommunity {
		if b != 0 {
			t.Fatalf("community byte %d was not zeroized: %v", i, client.receivedCommunity)
		}
	}
	// The result carries no secret.
	resultJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(resultJSON), secret) {
		t.Fatalf("apply result leaked the community secret: %s", resultJSON)
	}
	// Postcondition-observable effect: community now configured, service enabled.
	if !client.snmp.CommunityConfigured || !client.snmp.Enabled || !client.snmp.V1V2cEnabled {
		t.Fatalf("post-apply state = %#v", client.snmp)
	}
}

// TestSNMPApplyWithoutCommunityOmitsSecret proves a non-community SNMP change
// resolves no secret and passes nil to the wire (DSM keeps the existing community).
func TestSNMPApplyWithoutCommunityOmitsSecret(t *testing.T) {
	service := terminalSNMPTestService()
	client := terminalSNMPTestClient()
	client.snmp = synology.SNMPState{Enabled: true, V1V2cEnabled: true, CommunityConfigured: true, Location: "old"}
	plan, err := planSNMPWithClient(context.Background(), "lab", client, terminalsnmp.SNMPChange{Location: strPtr("new-loc")})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	result, err := service.applySNMPPlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if !result.Applied || client.receivedCommunityPresent {
		t.Fatalf("no community should ride the wire: present=%v", client.receivedCommunityPresent)
	}
	if client.snmp.Location != "new-loc" {
		t.Fatalf("location not applied: %#v", client.snmp)
	}
}

func TestSNMPPlanStaleRejection(t *testing.T) {
	service := terminalSNMPTestService()
	client := terminalSNMPTestClient()
	client.snmp = synology.SNMPState{Enabled: true, V1V2cEnabled: true, CommunityConfigured: true, Location: "a"}
	plan, err := planSNMPWithClient(context.Background(), "lab", client, terminalsnmp.SNMPChange{Location: strPtr("b")})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	client.snmp.Location = "changed-externally"
	if _, err := service.applySNMPPlanWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale rejection, got %v", err)
	}
}

func strPtr(s string) *string { return &s }
