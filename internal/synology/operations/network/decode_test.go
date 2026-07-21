package network

import (
	"encoding/json"
	"strings"
	"testing"
)

// generalLive is the exact SYNO.Core.Network get v2 body observed on the DSM 7.3
// lab (build 81168).
const generalLive = `{
  "arp_ignore": true,
  "dns_manual": false,
  "dns_primary": "10.17.250.253",
  "dns_secondary": "10.17.250.253",
  "enable_ip_conflict_detect": true,
  "enable_windomain": false,
  "gateway": "10.17.39.254",
  "gateway_info": {"ifname": "eth0", "ip": "10.17.36.235", "mask": "255.255.248.0", "status": "connected", "type": "lan", "use_dhcp": true},
  "ipv4_first": false,
  "multi_gateway": false,
  "server_name": "Derek_3018xs",
  "use_dhcp_domain": true,
  "v6gateway": ""
}`

func TestDecodeGeneralLive(t *testing.T) {
	g, err := decodeGeneral(json.RawMessage(generalLive))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if g.Hostname != "Derek_3018xs" {
		t.Errorf("hostname = %q", g.Hostname)
	}
	if g.DefaultGatewayV4 != "10.17.39.254" || g.DefaultGatewayV6 != "" {
		t.Errorf("gateway v4=%q v6=%q", g.DefaultGatewayV4, g.DefaultGatewayV6)
	}
	if g.DNSPrimary != "10.17.250.253" || g.DNSManual {
		t.Errorf("dns primary=%q manual=%v", g.DNSPrimary, g.DNSManual)
	}
	if !g.IPConflictDetect || g.MultiGateway {
		t.Errorf("ip_conflict=%v multi_gateway=%v", g.IPConflictDetect, g.MultiGateway)
	}
	if g.DefaultGateway.Interface != "eth0" || !g.DefaultGateway.UseDHCP || g.DefaultGateway.IP != "10.17.36.235" {
		t.Errorf("gateway interface = %#v", g.DefaultGateway)
	}
}

func TestDecodeGeneralRejectsUnknownShape(t *testing.T) {
	if _, err := decodeGeneral(json.RawMessage(`{"unexpected":1}`)); err == nil || !strings.Contains(err.Error(), "no recognized fields") {
		t.Fatalf("error = %v", err)
	}
	if _, err := decodeGeneral(json.RawMessage(`["eth0"]`)); err == nil || !strings.Contains(err.Error(), "not an object") {
		t.Fatalf("error = %v", err)
	}
	if _, err := decodeGeneral(json.RawMessage(`null`)); err == nil {
		t.Fatalf("expected error for null")
	}
}

// interfacesLive is the exact SYNO.Core.Network.Ethernet list body observed on
// the lab (two connected NICs, two disconnected).
const interfacesLive = `[
  {"block":0,"dns":"10.17.250.253","duplex":true,"enable_vlan":false,"gateway":"10.17.39.254","ifname":"eth0","ip":"10.17.36.235","ipv6":[],"is_default_gateway":false,"mask":"255.255.248.0","max_supported_speed":1000,"mtu":1500,"mtu_config":1500,"speed":1000,"status":"connected","type":"lan","use_dhcp":true,"vlan_id":0},
  {"block":0,"dns":"","duplex":true,"enable_vlan":false,"gateway":"","ifname":"eth2","ip":"169.254.148.8","ipv6":[],"is_default_gateway":false,"mask":"255.255.0.0","max_supported_speed":1000,"mtu":9000,"mtu_config":9000,"speed":-1,"status":"disconnected","type":"lan","use_dhcp":true,"vlan_id":0}
]`

func TestDecodeInterfacesLive(t *testing.T) {
	ifaces, err := decodeInterfaces(json.RawMessage(interfacesLive))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(ifaces) != 2 {
		t.Fatalf("count = %d", len(ifaces))
	}
	eth0 := ifaces[0]
	if eth0.Name != "eth0" || eth0.IPv4 != "10.17.36.235" || eth0.Netmask != "255.255.248.0" {
		t.Errorf("eth0 = %#v", eth0)
	}
	if eth0.MTU != 1500 || !eth0.UseDHCP || !eth0.FullDuplex || eth0.SpeedMbps != 1000 || eth0.MaxSpeedMbps != 1000 {
		t.Errorf("eth0 link = %#v", eth0)
	}
	if eth0.LinkStatus != "connected" || eth0.GatewayV4 != "10.17.39.254" {
		t.Errorf("eth0 status = %#v", eth0)
	}
	eth2 := ifaces[1]
	if eth2.MTU != 9000 { // jumbo
		t.Errorf("eth2 mtu = %d", eth2.MTU)
	}
	if eth2.SpeedMbps != -1 || eth2.LinkStatus != "disconnected" {
		t.Errorf("eth2 down = %#v", eth2)
	}
	if len(eth2.IPv6) != 0 {
		t.Errorf("eth2 ipv6 should be empty: %#v", eth2.IPv6)
	}
}

func TestDecodeInterfacesAcceptsWrappedArray(t *testing.T) {
	ifaces, err := decodeInterfaces(json.RawMessage(`{"interfaces":[{"ifname":"eth0","ip":"1.2.3.4"}]}`))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(ifaces) != 1 || ifaces[0].Name != "eth0" {
		t.Fatalf("ifaces = %#v", ifaces)
	}
}

func TestDecodeInterfacesSkipsNamelessAndRejectsScalar(t *testing.T) {
	// An entry without ifname is skipped, not surfaced as a nameless interface.
	ifaces, err := decodeInterfaces(json.RawMessage(`[{"ip":"1.2.3.4"},{"ifname":"eth1"}]`))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(ifaces) != 1 || ifaces[0].Name != "eth1" {
		t.Fatalf("ifaces = %#v", ifaces)
	}
	// A scalar response is not a valid interface list.
	if _, err := decodeInterfaces(json.RawMessage(`42`)); err == nil {
		t.Fatalf("expected error for scalar")
	}
	if _, err := decodeInterfaces(json.RawMessage(`{"nope":1}`)); err == nil || !strings.Contains(err.Error(), "no array") {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeInterfacesIPv6Elements(t *testing.T) {
	// The ipv6 element shape is wire-unverified; the tolerant decoder reads the
	// SYNO.Core.Network.IPv6 get field names plus common alternatives.
	ifaces, err := decodeInterfaces(json.RawMessage(`[{"ifname":"eth0","ipv6":[{"address":"fe80::1","prefix_length":64,"type":"auto"},{"ip":"2001:db8::2","prefix":48}]}]`))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	v6 := ifaces[0].IPv6
	if len(v6) != 2 {
		t.Fatalf("ipv6 = %#v", v6)
	}
	if v6[0].Address != "fe80::1" || v6[0].PrefixLength != 64 || v6[0].Type != "auto" {
		t.Errorf("v6[0] = %#v", v6[0])
	}
	if v6[1].Address != "2001:db8::2" || v6[1].PrefixLength != 48 {
		t.Errorf("v6[1] = %#v", v6[1])
	}
}

func TestDecodeBondsEmptyLive(t *testing.T) {
	// The lab returns a bare empty array (no bonds).
	bonds, err := decodeBonds(json.RawMessage(`[]`))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(bonds) != 0 {
		t.Fatalf("bonds = %#v", bonds)
	}
}

func TestDecodeBondsTolerantFields(t *testing.T) {
	// Per-bond mode/members are wire-unverified; the decoder reads several
	// spellings and a member array of either strings or {ifname} objects.
	bonds, err := decodeBonds(json.RawMessage(`[{"ifname":"bond0","ip":"10.0.0.9","status":"connected","bond_mode":"802.3ad","slaves":["eth0",{"ifname":"eth1"}]}]`))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(bonds) != 1 {
		t.Fatalf("bonds = %#v", bonds)
	}
	b := bonds[0]
	if b.Name != "bond0" || b.Mode != "802.3ad" || b.IPv4 != "10.0.0.9" {
		t.Errorf("bond = %#v", b)
	}
	if len(b.Members) != 2 || b.Members[0] != "eth0" || b.Members[1] != "eth1" {
		t.Errorf("members = %#v", b.Members)
	}
}

func TestDecodeRoutesShapes(t *testing.T) {
	// Bare array.
	routes, err := decodeRoutes(json.RawMessage(`[{"destination":"192.168.9.0","netmask":"255.255.255.0","gateway":"10.0.0.1","ifname":"eth0"}]`))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(routes) != 1 || routes[0].Destination != "192.168.9.0" || routes[0].Interface != "eth0" {
		t.Fatalf("routes = %#v", routes)
	}
	// ipv4/ipv6 sectioned envelope tags the family.
	routes, err = decodeRoutes(json.RawMessage(`{"ipv4":[{"dest":"10.1.0.0","mask":"255.255.0.0","gateway":"10.0.0.1"}],"ipv6":[{"dest":"2001:db8::","prefix":"32","gateway":"fe80::1"}]}`))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("routes = %#v", routes)
	}
	if routes[0].Family != "ipv4" || routes[1].Family != "ipv6" {
		t.Errorf("families = %q %q", routes[0].Family, routes[1].Family)
	}
	// Empty is not an error.
	routes, err = decodeRoutes(json.RawMessage(`[]`))
	if err != nil || len(routes) != 0 {
		t.Fatalf("empty routes err=%v routes=%#v", err, routes)
	}
	// A scalar is rejected.
	if _, err := decodeRoutes(json.RawMessage(`7`)); err == nil {
		t.Fatalf("expected error for scalar route response")
	}
}

// proxyLive is the exact SYNO.Core.Network.Proxy get body observed on the lab,
// including the masked password field (tabs) DSM returns.
const proxyLive = `{"enable":false,"enable_auth":false,"enable_bypass":true,"enable_different_host":false,"http_host":"","http_port":"80","https_host":"","https_port":"80","password":"\t\t\t\t\t\t\t\t","username":""}`

func TestDecodeProxyLive(t *testing.T) {
	p, err := decodeProxy(json.RawMessage(proxyLive))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !p.Supported || p.Enabled || p.AuthEnabled || !p.BypassLocal {
		t.Errorf("proxy flags = %#v", p)
	}
	if p.HTTPPort != "80" {
		t.Errorf("http_port = %q", p.HTTPPort)
	}
}

// TestDecodeProxyNeverSurfacesPassword is the mandatory no-secret-leak guard: the
// proxy password (a real secret in the network surface) must never appear in the
// decoded domain model, even when DSM returns a concrete password value.
func TestDecodeProxyNeverSurfacesPassword(t *testing.T) {
	const secret = "SUPER-SECRET-PROXY-PASSWORD"
	body := `{"enable":true,"enable_auth":true,"username":"bob","http_host":"proxy.example","http_port":"3128","password":"` + secret + `"}`
	p, err := decodeProxy(json.RawMessage(body))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if p.Username != "bob" || !p.AuthEnabled { // legitimate fields still decode
		t.Fatalf("proxy = %#v", p)
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("decoded proxy leaked the password: %s", encoded)
	}
}

func TestDecodeProxyRejectsUnknownShape(t *testing.T) {
	if _, err := decodeProxy(json.RawMessage(`{"nope":1}`)); err == nil || !strings.Contains(err.Error(), "no recognized fields") {
		t.Fatalf("error = %v", err)
	}
}

// TestDecodersDropUnexpectedFields proves the field-whitelist decoders never
// surface an unexpected top-level field (e.g. a smuggled session token).
func TestDecodersDropUnexpectedFields(t *testing.T) {
	const canary = "CANARY-must-not-survive-decode"
	general, err := decodeGeneral(json.RawMessage(`{"server_name":"nas","gateway":"1.1.1.1","_sid":"` + canary + `","SynoToken":"` + canary + `"}`))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	ifaces, err := decodeInterfaces(json.RawMessage(`[{"ifname":"eth0","ip":"1.2.3.4","secret":"` + canary + `"}]`))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	for _, model := range []any{general, ifaces} {
		encoded, err := json.Marshal(model)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), canary) {
			t.Fatalf("decoded model carried unexpected field material: %s", encoded)
		}
	}
	if general.Hostname != "nas" || ifaces[0].Name != "eth0" { // legitimate fields survive
		t.Fatalf("legitimate fields lost: %#v %#v", general, ifaces)
	}
}
