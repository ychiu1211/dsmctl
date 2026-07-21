package firewall

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/firewall"
)

// TestExecuteProfileSetRequestShape asserts the full-profile Profile.set body: a
// single "profile" object carrying every adapter section plus the profile name,
// the profile_applying flag, and the confirmed per-rule field names.
func TestExecuteProfileSetRequestShape(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{}}
	input := ProfileSetInput{
		Activate: true,
		Profile: firewall.ProfileRules{
			Profile: "custom",
			Adapters: []firewall.AdapterPolicy{{
				Adapter: "global", Policy: "drop",
				Rules: []firewall.Rule{{
					Enabled: true, Name: "allow-dsm", Policy: "allow", Protocol: "tcp",
					PortDirection: "destination", PortGroup: "ports", Ports: "5001",
					SourceGroup: "all", Source: "all", Log: false,
				}},
			}},
		},
	}
	if _, _, err := ExecuteProfileSet(context.Background(), fwTarget(), exec, input); err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(exec.requests) != 1 {
		t.Fatalf("requests = %d", len(exec.requests))
	}
	req := exec.requests[0]
	if req.API != FirewallProfileAPIName || req.Method != "set" || req.Version != 1 {
		t.Fatalf("request = %#v", req)
	}
	if req.ReadOnly {
		t.Fatal("a firewall write must not be marked read-only")
	}
	if applying, _ := req.JSONParameters["profile_applying"].(bool); !applying {
		t.Fatalf("profile_applying = %v", req.JSONParameters["profile_applying"])
	}
	profile, ok := req.JSONParameters["profile"].(map[string]any)
	if !ok {
		t.Fatalf("profile param is %T", req.JSONParameters["profile"])
	}
	if profile["name"] != "custom" {
		t.Fatalf("profile name = %v", profile["name"])
	}
	section, ok := profile["global"].(map[string]any)
	if !ok || section["policy"] != "drop" {
		t.Fatalf("global section = %#v", profile["global"])
	}
	rules, ok := section["rules"].([]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("rules = %#v", section["rules"])
	}
	got := rules[0].(map[string]any)
	want := map[string]any{
		"enable": true, "name": "allow-dsm", "policy": "allow", "protocol": "tcp",
		"port_direction": "destination", "port_group": "ports", "ports": "5001",
		"source_ip_group": "all", "source_ip": "all", "log": false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rule body = %#v, want %#v", got, want)
	}
}

// TestExecuteEnableDisableRequestShape asserts disabling uses Firewall.set with
// set_type=disable.
func TestExecuteEnableDisableRequestShape(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{}}
	if _, _, err := ExecuteEnable(context.Background(), fwTarget(), exec, EnableInput{Enabled: false}); err != nil {
		t.Fatalf("error = %v", err)
	}
	req := exec.requests[0]
	if req.API != FirewallAPIName || req.Method != "set" {
		t.Fatalf("disable request = %#v", req)
	}
	if req.JSONParameters["set_type"] != "disable" {
		t.Fatalf("set_type = %v", req.JSONParameters["set_type"])
	}
}

// TestExecuteEnableStartRequestShape asserts enabling uses Profile.Apply.start with
// the target profile name.
func TestExecuteEnableStartRequestShape(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{}}
	if _, _, err := ExecuteEnable(context.Background(), fwTarget(), exec, EnableInput{Enabled: true, Profile: "default"}); err != nil {
		t.Fatalf("error = %v", err)
	}
	req := exec.requests[0]
	if req.API != FirewallProfileApplyAPIName || req.Method != "start" {
		t.Fatalf("enable request = %#v", req)
	}
	if req.JSONParameters["name"] != "default" {
		t.Fatalf("name = %v", req.JSONParameters["name"])
	}
}

func TestExecuteEnableRequiresProfileName(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{}}
	if _, _, err := ExecuteEnable(context.Background(), fwTarget(), exec, EnableInput{Enabled: true}); err == nil {
		t.Fatal("enabling without a profile name should error")
	}
}

// TestEncodeDecodeRuleRoundTrip proves the encode/decode pair for a rule is
// symmetric on the live-confirmed field names.
func TestEncodeDecodeRuleRoundTrip(t *testing.T) {
	original := firewall.Rule{
		Enabled: true, Name: "web", Policy: "allow", Protocol: "tcp",
		PortDirection: "destination", PortGroup: "ports", Ports: "5000,5001",
		SourceGroup: "netmask", Source: "10.17.36.0/24", Log: true,
	}
	encoded := encodeRule(original)
	section := map[string]any{"rules": []any{encoded}}
	decoded := decodeRules(section)
	if len(decoded) != 1 || decoded[0] != original {
		t.Fatalf("round trip = %#v, want %#v", decoded, original)
	}
}

func TestDecodeCurrentConnection(t *testing.T) {
	data := json.RawMessage(`{"items":[
		{"from":"10.17.36.69","who":"deryck","is_current_connected":true,"_sid":"SECRET"},
		{"from":"10.17.36.70","who":"other","is_current_connected":false}
	],"total":2}`)
	sources, err := decodeCurrentConnection(data)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %#v", sources)
	}
	if sources[0].From != "10.17.36.69" || !sources[0].Current || sources[0].Who != "deryck" {
		t.Fatalf("first source = %#v", sources[0])
	}
	if sources[1].Current {
		t.Fatalf("second source should not be current: %#v", sources[1])
	}
	// The session secret must never survive decoding.
	encoded, _ := json.Marshal(sources)
	if string(encoded) == "" || contains(string(encoded), "SECRET") {
		t.Fatalf("decoded sources leaked a secret: %s", encoded)
	}
}

func TestDecodeCurrentConnectionEmpty(t *testing.T) {
	sources, err := decodeCurrentConnection(json.RawMessage(`{"total":0}`))
	if err != nil || sources != nil {
		t.Fatalf("sources=%#v err=%v", sources, err)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
