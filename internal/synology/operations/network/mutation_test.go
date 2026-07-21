package network

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/network"
)

// TestExecuteGeneralSetRequestShapeV2 locks the confirmed SYNO.Core.Network set
// body (v2 includes enable_ip_conflict_detect). The wire was live-verified by a
// no-op round-trip on the DSM 7.3 lab.
func TestExecuteGeneralSetRequestShapeV2(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{}}
	input := GeneralSetInput{General: network.General{
		Hostname: "test-nas", DefaultGatewayV4: "198.51.100.254", DefaultGatewayV6: "",
		DNSManual: true, DNSPrimary: "8.8.8.8", DNSSecondary: "8.8.4.4",
		UseDHCPDomain: true, IPv4First: false, MultiGateway: false, ARPIgnore: true, IPConflictDetect: true,
	}}
	if _, _, err := ExecuteGeneralSet(context.Background(), netTarget(2), exec, input); err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(exec.requests) != 1 {
		t.Fatalf("requests = %d", len(exec.requests))
	}
	req := exec.requests[0]
	if req.API != NetworkAPIName || req.Method != "set" || req.Version != 2 {
		t.Fatalf("request = %#v", req)
	}
	if req.ReadOnly {
		t.Fatal("a network write must not be marked read-only")
	}
	want := map[string]any{
		"server_name": "test-nas", "gateway": "198.51.100.254", "v6gateway": "",
		"dns_manual": true, "dns_primary": "8.8.8.8", "dns_secondary": "8.8.4.4",
		"use_dhcp_domain": true, "ipv4_first": false, "multi_gateway": false, "arp_ignore": true,
		"enable_ip_conflict_detect": true,
	}
	for k, v := range want {
		if req.JSONParameters[k] != v {
			t.Fatalf("param %q = %v, want %v", k, req.JSONParameters[k], v)
		}
	}
}

// TestExecuteGeneralSetV1OmitsIPConflict asserts the v1 body omits the v2-only
// enable_ip_conflict_detect field.
func TestExecuteGeneralSetV1OmitsIPConflict(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{}}
	if _, _, err := ExecuteGeneralSet(context.Background(), netTarget(1), exec, GeneralSetInput{General: network.General{Hostname: "n"}}); err != nil {
		t.Fatalf("error = %v", err)
	}
	req := exec.requests[0]
	if req.Version != 1 {
		t.Fatalf("version = %d", req.Version)
	}
	if _, present := req.JSONParameters["enable_ip_conflict_detect"]; present {
		t.Fatalf("v1 body must not carry enable_ip_conflict_detect: %#v", req.JSONParameters)
	}
}

// TestExecuteInterfaceSetRequestShape locks the best-known (WIRE-UNVERIFIED)
// Ethernet.set body. This documents the shape; the live apply is refused while
// the wire is unverified.
func TestExecuteInterfaceSetRequestShape(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{}}
	input := InterfaceSetInput{Interface: network.Interface{Name: "eth1", IPv4: "192.0.2.35", Netmask: "255.255.248.0", GatewayV4: "198.51.100.254", UseDHCP: true, MTU: 1500}}
	if _, _, err := ExecuteInterfaceSet(context.Background(), netTarget(2), exec, input); err != nil {
		t.Fatalf("error = %v", err)
	}
	req := exec.requests[0]
	if req.API != EthernetAPIName || req.Method != "set" {
		t.Fatalf("request = %#v", req)
	}
	if req.JSONParameters["ifname"] != "eth1" || req.JSONParameters["ip"] != "192.0.2.35" || req.JSONParameters["mask"] != "255.255.248.0" {
		t.Fatalf("body = %#v", req.JSONParameters)
	}
	if req.JSONParameters["use_dhcp"] != true || req.JSONParameters["mtu"] != 1500 {
		t.Fatalf("body = %#v", req.JSONParameters)
	}
}

func TestDecodeCurrentSources(t *testing.T) {
	data := json.RawMessage(`{"items":[
		{"from":"192.0.2.69","who":"testuser","is_current_connected":false,"_sid":"SECRET"},
		{"from":"192.0.2.69","who":"testuser"},
		{"from":"192.0.2.70"}
	],"total":3}`)
	sources := decodeCurrentSources(data)
	if len(sources) != 2 {
		t.Fatalf("sources = %#v (want deduped 2)", sources)
	}
	// no secret survives
	encoded, _ := json.Marshal(sources)
	if string(encoded) == "" || containsStr(string(encoded), "SECRET") {
		t.Fatalf("leaked secret: %s", encoded)
	}
}

func TestDecodeCurrentSourcesEmpty(t *testing.T) {
	if s := decodeCurrentSources(json.RawMessage(`{"total":0}`)); s != nil {
		t.Fatalf("sources = %#v", s)
	}
}

func containsStr(h, n string) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return true
		}
	}
	return false
}
