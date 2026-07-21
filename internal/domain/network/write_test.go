package network

import "testing"

func labInterfaces() []Interface {
	return []Interface{
		{Name: "eth0", IPv4: "192.0.2.235", Netmask: "255.255.248.0", GatewayV4: "198.51.100.254", UseDHCP: true, MTU: 1500, LinkStatus: LinkConnected},
		{Name: "eth1", IPv4: "192.0.2.35", Netmask: "255.255.248.0", GatewayV4: "198.51.100.254", UseDHCP: true, MTU: 1500, LinkStatus: LinkConnected},
		{Name: "eth2", IPv4: "169.254.148.8", Netmask: "255.255.0.0", UseDHCP: true, MTU: 1500, LinkStatus: LinkDisconnected},
	}
}

func strptr(s string) *string { return &s }
func intptr(i int) *int       { return &i }
func boolptr(b bool) *bool    { return &b }

func TestResolveManagementPathMatchesTransportNIC(t *testing.T) {
	path := ResolveManagementPath(Transport{Host: "192.0.2.235", Port: 5001}, labInterfaces(), "198.51.100.254")
	if path.Ambiguous {
		t.Fatalf("path should not be ambiguous: %+v", path)
	}
	if path.Interface != "eth0" || path.InterfaceIP != "192.0.2.235" {
		t.Fatalf("management NIC = %q (%q), want eth0 (192.0.2.235)", path.Interface, path.InterfaceIP)
	}
	if path.DefaultGateway != "198.51.100.254" {
		t.Fatalf("default gateway = %q", path.DefaultGateway)
	}
	if !path.IsManagementInterface("eth0") || path.IsManagementInterface("eth1") {
		t.Fatalf("management-interface classification wrong: %+v", path)
	}
}

func TestResolveManagementPathHostnameIsAmbiguous(t *testing.T) {
	path := ResolveManagementPath(Transport{Host: "mynas.example.com", Port: 5001}, labInterfaces(), "198.51.100.254")
	if !path.Ambiguous {
		t.Fatalf("a hostname connection must be ambiguous: %+v", path)
	}
	// Every interface is treated as management (fail closed).
	if !path.IsManagementInterface("eth1") {
		t.Fatalf("ambiguous path must protect every interface")
	}
}

func TestResolveManagementPathUnmatchedIPIsAmbiguous(t *testing.T) {
	// A NATed/relayed address that no NIC bears.
	path := ResolveManagementPath(Transport{Host: "192.168.99.99", Port: 5001}, labInterfaces(), "198.51.100.254")
	if !path.Ambiguous {
		t.Fatalf("an unmatched transport IP must be ambiguous: %+v", path)
	}
}

func TestGuardRefusesManagementNICChange(t *testing.T) {
	path := ResolveManagementPath(Transport{Host: "192.0.2.235", Port: 5001}, labInterfaces(), "198.51.100.254")
	current := labInterfaces()[0] // eth0
	change := InterfaceChange{Name: "eth0", IPv4: strptr("192.0.2.240")}
	v := EvaluateInterfaceChange(path, current, change)
	if !v.Protected || !v.Severs {
		t.Fatalf("changing the management NIC must be protected+severing: %+v", v)
	}
	if v.Allowed {
		t.Fatalf("changing the management NIC must NOT be allowed without override: %+v", v)
	}
	// With override it is allowed but flagged.
	change.AllowConnectivityBreak = true
	v = EvaluateInterfaceChange(path, current, change)
	if !v.Allowed || !v.Overridden {
		t.Fatalf("override must allow but flag the change: %+v", v)
	}
}

func TestGuardRefusesManagementNICToDHCP(t *testing.T) {
	path := ResolveManagementPath(Transport{Host: "192.0.2.235", Port: 5001}, labInterfaces(), "198.51.100.254")
	current := Interface{Name: "eth0", IPv4: "192.0.2.235", Netmask: "255.255.248.0", UseDHCP: false, MTU: 1500}
	// Switching the management NIC to DHCP (address may change) must be refused.
	v := EvaluateInterfaceChange(path, current, InterfaceChange{Name: "eth0", UseDHCP: boolptr(true)})
	if v.Allowed {
		t.Fatalf("switching the management NIC to DHCP must be refused: %+v", v)
	}
}

func TestGuardRefusesManagementNICMTU(t *testing.T) {
	path := ResolveManagementPath(Transport{Host: "192.0.2.235", Port: 5001}, labInterfaces(), "198.51.100.254")
	current := labInterfaces()[0]
	v := EvaluateInterfaceChange(path, current, InterfaceChange{Name: "eth0", MTU: intptr(9000)})
	if v.Allowed {
		t.Fatalf("an MTU change on the management NIC must be refused: %+v", v)
	}
}

func TestGuardAllowsNonManagementNICChange(t *testing.T) {
	path := ResolveManagementPath(Transport{Host: "192.0.2.235", Port: 5001}, labInterfaces(), "198.51.100.254")
	current := labInterfaces()[1] // eth1
	v := EvaluateInterfaceChange(path, current, InterfaceChange{Name: "eth1", MTU: intptr(1400)})
	if v.Protected || !v.Allowed {
		t.Fatalf("changing a non-management NIC must be permitted: %+v", v)
	}
}

func TestGuardAmbiguousRefusesEveryInterface(t *testing.T) {
	path := ResolveManagementPath(Transport{Host: "nas.ddns.net", Port: 5001}, labInterfaces(), "198.51.100.254")
	current := labInterfaces()[1] // eth1, normally non-management
	v := EvaluateInterfaceChange(path, current, InterfaceChange{Name: "eth1", MTU: intptr(1400)})
	if v.Allowed {
		t.Fatalf("an ambiguous connection must refuse even a non-management NIC change: %+v", v)
	}
}

func TestGuardRefusesDefaultGatewayChange(t *testing.T) {
	path := ResolveManagementPath(Transport{Host: "192.0.2.235", Port: 5001}, labInterfaces(), "198.51.100.254")
	current := General{DefaultGatewayV4: "198.51.100.254", Hostname: "test-nas"}
	v := EvaluateGeneralChange(path, current, GeneralChange{DefaultGatewayV4: strptr("198.51.100.1")})
	if !v.Protected || !v.Severs || v.Allowed {
		t.Fatalf("a default-gateway change must be protected, severing, and refused: %+v", v)
	}
	v = EvaluateGeneralChange(path, current, GeneralChange{DefaultGatewayV4: strptr("198.51.100.1"), AllowConnectivityBreak: true})
	if !v.Allowed || !v.Overridden {
		t.Fatalf("override must allow the gateway change but flag it: %+v", v)
	}
}

func TestGuardAllowsHostnameAndDNSChange(t *testing.T) {
	path := ResolveManagementPath(Transport{Host: "192.0.2.235", Port: 5001}, labInterfaces(), "198.51.100.254")
	current := General{DefaultGatewayV4: "198.51.100.254", Hostname: "test-nas", DNSPrimary: "203.0.113.253"}
	v := EvaluateGeneralChange(path, current, GeneralChange{Hostname: strptr("renamed"), DNSPrimary: strptr("8.8.8.8")})
	if v.Protected || !v.Allowed {
		t.Fatalf("a hostname/DNS change must be permitted (medium risk): %+v", v)
	}
}

func TestMergeGeneralPatchOnlyPreservesUntouched(t *testing.T) {
	current := General{Hostname: "old", DefaultGatewayV4: "10.0.0.1", DNSPrimary: "1.1.1.1", DNSSecondary: "2.2.2.2", IPv4First: true}
	merged := MergeGeneral(current, GeneralChange{Hostname: strptr("new")})
	if merged.Hostname != "new" {
		t.Fatalf("hostname not applied: %+v", merged)
	}
	if merged.DefaultGatewayV4 != "10.0.0.1" || merged.DNSPrimary != "1.1.1.1" || merged.DNSSecondary != "2.2.2.2" || !merged.IPv4First {
		t.Fatalf("patch-only preservation failed: %+v", merged)
	}
}

func TestMergeInterfacePatchOnlyPreservesUntouched(t *testing.T) {
	current := Interface{Name: "eth1", IPv4: "10.0.0.5", Netmask: "255.255.255.0", GatewayV4: "10.0.0.1", UseDHCP: false, MTU: 1500}
	merged := MergeInterface(current, InterfaceChange{Name: "eth1", MTU: intptr(9000)})
	if merged.MTU != 9000 {
		t.Fatalf("mtu not applied: %+v", merged)
	}
	if merged.IPv4 != "10.0.0.5" || merged.Netmask != "255.255.255.0" || merged.GatewayV4 != "10.0.0.1" || merged.UseDHCP {
		t.Fatalf("patch-only preservation failed: %+v", merged)
	}
}
