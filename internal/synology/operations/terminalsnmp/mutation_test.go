package terminalsnmp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestExecuteTerminalSetRequestShape(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{}`)}
	desired := TerminalSetInput{SSHEnabled: true, SSHPort: 2222, TelnetEnabled: false, ConsoleForbidden: true}
	result, selection, err := ExecuteTerminalSet(context.Background(), target(), executor, desired)
	if err != nil {
		t.Fatalf("ExecuteTerminalSet() error = %v", err)
	}
	req := executor.request
	if req.API != TerminalAPIName || req.Version != 3 || req.Method != "set" {
		t.Fatalf("request = %#v", req)
	}
	if req.ReadOnly {
		t.Fatalf("a mutation must not be marked read-only: %#v", req)
	}
	if got := req.JSONParameters["enable_ssh"]; got != true {
		t.Fatalf("enable_ssh = %#v", got)
	}
	if got := req.JSONParameters["ssh_port"]; got != 2222 {
		t.Fatalf("ssh_port = %#v", got)
	}
	if got := req.JSONParameters["enable_telnet"]; got != false {
		t.Fatalf("enable_telnet = %#v", got)
	}
	if got := req.JSONParameters["forbid_console"]; got != true {
		t.Fatalf("forbid_console = %#v", got)
	}
	if selection.Backend != "terminal-set-v3" || result.Method != "set" || result.API != TerminalAPIName {
		t.Fatalf("selection=%#v result=%#v", selection, result)
	}
}

// TestExecuteSNMPSetCommunityRidesOnlyRequestBody is the mandatory secret-hygiene
// request-capture: the resolved read community must appear ONLY in the SNMP set
// request body (the rocommunity parameter) and nowhere in the returned result.
func TestExecuteSNMPSetCommunityRidesOnlyRequestBody(t *testing.T) {
	const secret = "SUPER-SECRET-COMMUNITY-abc123"
	executor := &capturingExecutor{response: json.RawMessage(`{}`)}
	desired := SNMPSetInput{
		Enabled: true, V1V2cEnabled: true, V3Enabled: false,
		Location: "MDF", Contact: "ops", V3User: "",
		Community: []byte(secret),
	}
	result, selection, err := ExecuteSNMPSet(context.Background(), target(), executor, desired)
	if err != nil {
		t.Fatalf("ExecuteSNMPSet() error = %v", err)
	}
	req := executor.request
	if req.API != SNMPAPIName || req.Version != 1 || req.Method != "set" {
		t.Fatalf("request = %#v", req)
	}
	// The secret must be exactly the rocommunity parameter value.
	if got, _ := req.JSONParameters["rocommunity"].(string); got != secret {
		t.Fatalf("rocommunity = %q, want the resolved secret", got)
	}
	// The secret must appear in NO other request parameter.
	for name, value := range req.JSONParameters {
		if name == "rocommunity" {
			continue
		}
		if s, ok := value.(string); ok && strings.Contains(s, secret) {
			t.Fatalf("secret leaked into request parameter %q: %q", name, s)
		}
	}
	// The secret must appear NOWHERE in the returned result.
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("mutation result carried the secret: %s", encoded)
	}
	if selection.Backend != "snmp-set-v1" {
		t.Fatalf("selection = %#v", selection)
	}
	// Non-secret device info rides as plain params.
	if req.JSONParameters["location"] != "MDF" || req.JSONParameters["contact"] != "ops" {
		t.Fatalf("device info params = %#v", req.JSONParameters)
	}
}

// TestExecuteSNMPSetOmitsCommunityWhenAbsent proves that when no community is
// supplied (nil), the set omits rocommunity entirely so DSM keeps the configured
// community (patch semantics) rather than receiving an empty string it ignores.
func TestExecuteSNMPSetOmitsCommunityWhenAbsent(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{}`)}
	desired := SNMPSetInput{Enabled: false, V1V2cEnabled: false, V3Enabled: false, Community: nil}
	if _, _, err := ExecuteSNMPSet(context.Background(), target(), executor, desired); err != nil {
		t.Fatalf("ExecuteSNMPSet() error = %v", err)
	}
	if _, present := executor.request.JSONParameters["rocommunity"]; present {
		t.Fatalf("rocommunity must be absent when no community is supplied: %#v", executor.request.JSONParameters)
	}
	// rouser must also be omitted when the v3 username is empty.
	if _, present := executor.request.JSONParameters["rouser"]; present {
		t.Fatalf("rouser must be absent when the v3 username is empty: %#v", executor.request.JSONParameters)
	}
}

func TestSelectTerminalSNMPSetFailsClosed(t *testing.T) {
	if selection, err := SelectTerminalSet(target()); err != nil || !selection.Supported || selection.Backend != "terminal-set-v3" {
		t.Fatalf("terminal set selection=%#v err=%v", selection, err)
	}
	if selection, err := SelectSNMPSet(target()); err != nil || !selection.Supported || selection.Backend != "snmp-set-v1" {
		t.Fatalf("snmp set selection=%#v err=%v", selection, err)
	}
}
