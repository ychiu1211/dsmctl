package firewall

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// recordingExecutor returns a canned response per (api, method) and records the
// last request it saw, so tests can assert both decode results and the exact
// request contract (method, version, params) sent to DSM.
type recordingExecutor struct {
	responses map[string]json.RawMessage
	requests  []compatibility.Request
}

func (e *recordingExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	e.requests = append(e.requests, request)
	key := request.API + "." + request.Method
	if request.JSONParameters != nil {
		if name, ok := request.JSONParameters["name"].(string); ok {
			key += "." + name
		}
	}
	if resp, ok := e.responses[key]; ok {
		return resp, nil
	}
	return json.RawMessage(`{}`), nil
}

func (e *recordingExecutor) ExecuteScript(context.Context, compatibility.Request) ([]byte, error) {
	return nil, nil
}

func fwTarget() compatibility.Target {
	target := compatibility.NewTarget()
	for _, name := range APINames() {
		target.SetAPI(name, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	}
	return target
}

func TestSelectorsRequireTheirAPI(t *testing.T) {
	full := fwTarget()
	empty := compatibility.NewTarget()
	cases := []struct {
		name    string
		backend string
		selectF func(compatibility.Target) (compatibility.Selection, error)
	}{
		{"status", "firewall-status-get-v1", SelectStatus},
		{"profiles", "firewall-profiles-list-v1", SelectProfiles},
		{"adapters", "firewall-adapters-list-v1", SelectAdapters},
		{"rules", "firewall-profile-rules-get-v1", SelectRules},
	}
	for _, tc := range cases {
		selection, err := tc.selectF(full)
		if err != nil || !selection.Supported || selection.Backend != tc.backend {
			t.Fatalf("%s: selection=%#v err=%v", tc.name, selection, err)
		}
		selection, err = tc.selectF(empty)
		if !compatibility.IsUnsupported(err) || selection.Supported {
			t.Fatalf("%s: expected unsupported, got selection=%#v err=%v", tc.name, selection, err)
		}
	}
}

// TestIndependentBoundaries proves one area being absent never disables another:
// a target with only the Profile API reports profiles + rules supported and the
// status/adapters areas unsupported.
func TestIndependentBoundaries(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(FirewallProfileAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	got := map[string]bool{}
	for _, s := range Select(target) {
		got[s.Operation] = s.Supported
	}
	if !got[ProfilesReadCapabilityName] || !got[RulesReadCapabilityName] {
		t.Fatalf("profiles and rules should be supported with only the Profile API: %#v", got)
	}
	if got[StatusReadCapabilityName] || got[AdaptersReadCapabilityName] {
		t.Fatalf("status and adapters should be unsupported without their APIs: %#v", got)
	}
	if SupportsMutationAPIs(target) {
		t.Fatalf("mutation APIs should be absent")
	}
}

func TestExecuteStatusDecodesLiveShape(t *testing.T) {
	// Captured live from DSM 7.3 (SYNO.Core.Security.Firewall get).
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		FirewallAPIName + ".get": json.RawMessage(`{"enable_firewall":false,"profile_name":"default"}`),
	}}
	status, selection, err := ExecuteStatus(context.Background(), fwTarget(), exec)
	if err != nil {
		t.Fatalf("ExecuteStatus() error = %v", err)
	}
	if selection.Backend != "firewall-status-get-v1" {
		t.Fatalf("backend = %q", selection.Backend)
	}
	if status.Enabled || status.ActiveProfile != "default" {
		t.Fatalf("status = %#v", status)
	}
	req := exec.requests[0]
	if req.API != FirewallAPIName || req.Version != 1 || req.Method != "get" {
		t.Fatalf("request = %#v", req)
	}
}

func TestExecuteProfilesDecodesLiveShape(t *testing.T) {
	// Captured live (SYNO.Core.Security.Firewall.Profile list).
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		FirewallProfileAPIName + ".list": json.RawMessage(`{"profile_names":["custom","default"]}`),
	}}
	names, selection, err := ExecuteProfiles(context.Background(), fwTarget(), exec)
	if err != nil {
		t.Fatalf("ExecuteProfiles() error = %v", err)
	}
	if selection.Backend != "firewall-profiles-list-v1" {
		t.Fatalf("backend = %q", selection.Backend)
	}
	if len(names) != 2 || names[0] != "custom" || names[1] != "default" {
		t.Fatalf("names = %#v", names)
	}
}

func TestExecuteAdaptersDecodesLiveShape(t *testing.T) {
	// Captured live (SYNO.Core.Security.Firewall.Adapter list).
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		FirewallAdapterAPIName + ".list": json.RawMessage(`{"adapter_names":["eth0","eth1","global","vpn"]}`),
	}}
	names, _, err := ExecuteAdapters(context.Background(), fwTarget(), exec)
	if err != nil {
		t.Fatalf("ExecuteAdapters() error = %v", err)
	}
	if len(names) != 4 || names[2] != "global" {
		t.Fatalf("names = %#v", names)
	}
}

func TestExecuteProfileRulesDecodesLiveEmptyShape(t *testing.T) {
	// Captured live (SYNO.Core.Security.Firewall.Profile get name=default) — the
	// lab default profile carried only the all-interfaces "global" section with an
	// empty rule list and a "none" default policy.
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		FirewallProfileAPIName + ".get.default": json.RawMessage(`{"global":{"policy":"none","rules":[]},"name":"default"}`),
	}}
	rules, selection, err := ExecuteProfileRules(context.Background(), fwTarget(), exec, "default")
	if err != nil {
		t.Fatalf("ExecuteProfileRules() error = %v", err)
	}
	if selection.Backend != "firewall-profile-rules-get-v1" {
		t.Fatalf("backend = %q", selection.Backend)
	}
	if rules.Profile != "default" || len(rules.Adapters) != 1 {
		t.Fatalf("rules = %#v", rules)
	}
	adapter := rules.Adapters[0]
	if adapter.Adapter != "global" || adapter.Policy != "none" || adapter.Total != 0 || len(adapter.Rules) != 0 {
		t.Fatalf("adapter = %#v", adapter)
	}
	// The required name parameter must be sent.
	if got := exec.requests[0].JSONParameters["name"]; got != "default" {
		t.Fatalf("name parameter = %v", got)
	}
}

// TestExecuteProfileRulesDecodesPopulatedRule exercises the WIRE-UNVERIFIED
// per-rule decoder against a synthetic populated rule (the lab had none). It
// documents the best-knowledge field mapping and proves order is preserved.
func TestExecuteProfileRulesDecodesPopulatedRule(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		FirewallProfileAPIName + ".get.custom": json.RawMessage(`{
			"eth0":{"policy":"deny","total":2,"rules":[
				{"enable":true,"policy":"allow","proto":"tcp","ip_ver":"ipv4","src_type":"all","port_type":"custom","port":"5000,5001","name":"web"},
				{"enable":false,"policy":"drop","proto":"udp","src_type":"ip","src":"203.0.113.7","port_type":"all"}
			]},
			"name":"custom"
		}`),
	}}
	rules, _, err := ExecuteProfileRules(context.Background(), fwTarget(), exec, "custom")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(rules.Adapters) != 1 {
		t.Fatalf("adapters = %#v", rules.Adapters)
	}
	adapter := rules.Adapters[0]
	if adapter.Adapter != "eth0" || adapter.Policy != "deny" || adapter.Total != 2 || len(adapter.Rules) != 2 {
		t.Fatalf("adapter = %#v", adapter)
	}
	first := adapter.Rules[0]
	if !first.Enabled || first.Policy != "allow" || first.Protocol != "tcp" || first.IPVersion != "ipv4" ||
		first.SourceType != "all" || first.PortType != "custom" || first.Ports != "5000,5001" || first.Name != "web" {
		t.Fatalf("first rule = %#v", first)
	}
	second := adapter.Rules[1]
	if second.Enabled || second.Policy != "drop" || second.Source != "203.0.113.7" || second.SourceType != "ip" {
		t.Fatalf("second rule = %#v", second)
	}
}
