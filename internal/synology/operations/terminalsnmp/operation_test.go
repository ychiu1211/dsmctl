package terminalsnmp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type capturingExecutor struct {
	request  compatibility.Request
	response json.RawMessage
}

func (e *capturingExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	e.request = request
	return e.response, nil
}

func target() compatibility.Target {
	t := compatibility.NewTarget()
	t.SetAPI(TerminalAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	t.SetAPI(SNMPAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	return t
}

func TestSelectFailsClosedWithoutAPI(t *testing.T) {
	if selection, err := SelectTerminal(target()); err != nil || !selection.Supported || selection.Backend != "terminal-get-v3" {
		t.Fatalf("terminal selection=%#v err=%v", selection, err)
	}
	if selection, err := SelectSNMP(target()); err != nil || !selection.Supported || selection.Backend != "snmp-get-v1" {
		t.Fatalf("snmp selection=%#v err=%v", selection, err)
	}
	// Independent boundaries: only Terminal advertised → SNMP fails closed, Terminal stays supported.
	terminalOnly := compatibility.NewTarget()
	terminalOnly.SetAPI(TerminalAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	if selection, err := SelectTerminal(terminalOnly); err != nil || !selection.Supported {
		t.Fatalf("terminal-only terminal selection=%#v err=%v", selection, err)
	}
	if selection, err := SelectSNMP(terminalOnly); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("terminal-only snmp selection=%#v err=%v", selection, err)
	}
}

func TestExecuteTerminalDecodesLiveShape(t *testing.T) {
	// Captured live from DSM 7.3 SYNO.Core.Terminal.get (cipher/kex/mac menus trimmed).
	executor := &capturingExecutor{response: json.RawMessage(`{
		"enable_ssh": true,
		"enable_telnet": false,
		"forbid_console": false,
		"ssh_port": 22,
		"ssh_cipher": [{"name":"aes128-ctr","in_use":true,"security_level":2}]
	}`)}
	state, selection, err := ExecuteTerminal(context.Background(), target(), executor)
	if err != nil {
		t.Fatalf("ExecuteTerminal() error = %v", err)
	}
	if executor.request.API != TerminalAPIName || executor.request.Version != 3 || executor.request.Method != "get" || !executor.request.ReadOnly {
		t.Fatalf("request = %#v", executor.request)
	}
	if selection.Backend != "terminal-get-v3" {
		t.Fatalf("selection = %#v", selection)
	}
	if !state.SSHEnabled || state.SSHPort != 22 || state.TelnetEnabled || state.ConsoleForbidden {
		t.Fatalf("terminal state = %#v", state)
	}
}

func TestExecuteSNMPDecodesLiveShape(t *testing.T) {
	// Captured live from DSM 7.3 SYNO.Core.SNMP.get (service disabled).
	executor := &capturingExecutor{response: json.RawMessage(`{
		"contact": "",
		"enable_snmp": false,
		"enable_snmp_v1v2": false,
		"enable_snmp_v3": false,
		"location": "",
		"name": "",
		"node0_name": "",
		"node1_name": "",
		"rocommunity": "",
		"rouser": ""
	}`)}
	state, selection, err := ExecuteSNMP(context.Background(), target(), executor)
	if err != nil {
		t.Fatalf("ExecuteSNMP() error = %v", err)
	}
	if executor.request.API != SNMPAPIName || executor.request.Version != 1 || executor.request.Method != "get" {
		t.Fatalf("request = %#v", executor.request)
	}
	if selection.Backend != "snmp-get-v1" {
		t.Fatalf("selection = %#v", selection)
	}
	if state.Enabled || state.V1V2cEnabled || state.V3Enabled || state.CommunityConfigured || state.V3User != "" || state.TrapConfigured || state.TrapHostPresent {
		t.Fatalf("disabled snmp state = %#v", state)
	}
}

func TestDecodeRejectsUnknownShape(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{"ssh":"on"}`)}
	if _, _, err := ExecuteTerminal(context.Background(), target(), executor); err == nil || !strings.Contains(err.Error(), "no enable_ssh field") {
		t.Fatalf("terminal error = %v", err)
	}
	executor = &capturingExecutor{response: json.RawMessage(`{"snmp":"on"}`)}
	if _, _, err := ExecuteSNMP(context.Background(), target(), executor); err == nil || !strings.Contains(err.Error(), "no enable_snmp field") {
		t.Fatalf("snmp error = %v", err)
	}
	executor = &capturingExecutor{response: json.RawMessage(`[]`)}
	if _, _, err := ExecuteTerminal(context.Background(), target(), executor); err == nil {
		t.Fatalf("expected error for non-object terminal response")
	}
}

// TestSNMPDecodeDropsSecrets is the mandatory no-secret-leak guarantee: an SNMP
// get response carrying a community string, SNMPv3 auth/privacy passwords, and a
// trap community must have every secret value dropped by the decoder. The
// decoded model surfaces only presence flags and the non-secret v3 username, so
// re-encoding it shows no trace of any secret byte.
func TestSNMPDecodeDropsSecrets(t *testing.T) {
	const canary = "SECRETCANARY-must-not-survive-decode"
	executor := &capturingExecutor{response: json.RawMessage(`{
		"enable_snmp": true,
		"enable_snmp_v1v2": true,
		"enable_snmp_v3": true,
		"location": "MDF rack 3",
		"contact": "ops@example.com",
		"rocommunity": "` + canary + `",
		"community": "` + canary + `",
		"rouser": "snmpmonitor",
		"V3_auth_passwd": "` + canary + `",
		"V3_priv_passwd": "` + canary + `",
		"snmpv3_auth_passwd": "` + canary + `",
		"snmpv3_priv_passwd": "` + canary + `",
		"trap_host": "trap.example.com",
		"trap_community": "` + canary + `"
	}`)}
	state, _, err := ExecuteSNMP(context.Background(), target(), executor)
	if err != nil {
		t.Fatalf("ExecuteSNMP() error = %v", err)
	}
	// Presence and non-secret identity are surfaced.
	if !state.Enabled || !state.V1V2cEnabled || !state.V3Enabled {
		t.Fatalf("enabled flags not decoded: %#v", state)
	}
	if !state.CommunityConfigured {
		t.Fatalf("community presence not reported: %#v", state)
	}
	if state.V3User != "snmpmonitor" {
		t.Fatalf("v3 username = %q, want snmpmonitor", state.V3User)
	}
	if !state.TrapHostPresent || !state.TrapConfigured {
		t.Fatalf("trap presence not reported: %#v", state)
	}
	if state.Location != "MDF rack 3" || state.Contact != "ops@example.com" {
		t.Fatalf("non-secret device info = %#v", state)
	}
	// Re-encode the whole decoded model and assert not one byte of the injected
	// secret material survived into any field.
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), canary) {
		t.Fatalf("decoded model carried secret material: %s", encoded)
	}
	// Wire secret-key names that must never surface in the model. (The model's
	// own non-secret presence flag community_configured legitimately contains the
	// word "community", so the bare word is not forbidden — the canary check above
	// is the authoritative no-leak assertion.)
	for _, forbidden := range []string{"rocommunity", "passwd", "trap_community"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("decoded model exposes a secret-bearing field %q: %s", forbidden, encoded)
		}
	}
}
