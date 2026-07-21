package network

import (
	"fmt"
	"net"
	"strings"
)

// This file implements the Slice B (WI-069) write intents plus the
// never-sever-the-management-NIC guard. The guard is the defining safeguard of
// this module: changing the management interface's IP/mask/DHCP/MTU, disabling
// it, or changing the default gateway can sever dsmctl's only transport and lock
// out the whole shared box with no fallback.
//
// The guard is FAIL-CLOSED. dsmctl reaches DSM at a fixed transport address
// (the base-URL host and port). The management interface is the NIC whose IPv4
// address equals that host — the address the return path terminates on. If the
// connection arrived via a hostname/DDNS/QuickConnect/NATed path (the host is not
// an IP, or no NIC bears it), the on-NAS egress cannot be resolved, so EVERY
// interface and the default gateway are treated as protected. A protected-path
// change is refused unless the intent carries AllowConnectivityBreak.

// Transport is the immutable address and port dsmctl connects to. It is ground
// truth for the guard's management-interface identification.
type Transport struct {
	Host string `json:"host" jsonschema:"Base-URL host dsmctl connects to (an IP for a direct LAN NAS, or a hostname for a relayed one)"`
	Port int    `json:"port" jsonschema:"DSM management port dsmctl is connected over"`
}

// ManagementPath is the resolved protected path: the NIC carrying the transport
// address plus the default gateway. When Ambiguous, the guard protects every
// interface and the gateway.
type ManagementPath struct {
	Transport      Transport `json:"transport" jsonschema:"The transport dsmctl connects over"`
	Interface      string    `json:"interface,omitempty" jsonschema:"Management NIC name (the interface bearing the transport host); empty when the connection is ambiguous"`
	InterfaceIP    string    `json:"interface_ip,omitempty" jsonschema:"IPv4 address of the management NIC (equals the transport host)"`
	DefaultGateway string    `json:"default_gateway,omitempty" jsonschema:"IPv4 default gateway that serves the management path"`
	Ambiguous      bool      `json:"ambiguous" jsonschema:"True when the on-NAS egress could not be resolved (hostname/relay/NAT); every interface and the gateway are then protected"`
	Reason         string    `json:"reason" jsonschema:"How the management path was resolved"`
}

// ResolveManagementPath matches the transport host against the interface list to
// find the management NIC. A non-IP host, or an IP that no NIC bears, yields an
// ambiguous (fully protected) path.
func ResolveManagementPath(transport Transport, interfaces []Interface, defaultGateway string) ManagementPath {
	path := ManagementPath{Transport: transport, DefaultGateway: strings.TrimSpace(defaultGateway)}
	host := strings.TrimSpace(transport.Host)
	ip := net.ParseIP(host)
	if ip == nil {
		path.Ambiguous = true
		path.Reason = fmt.Sprintf("dsmctl connected to %q, which is not a literal IP (hostname/DDNS/QuickConnect/relay); the on-NAS egress cannot be resolved, so every interface and the default gateway are protected", host)
		return path
	}
	for _, iface := range interfaces {
		if strings.TrimSpace(iface.IPv4) != "" && net.ParseIP(strings.TrimSpace(iface.IPv4)).Equal(ip) {
			path.Interface = iface.Name
			path.InterfaceIP = strings.TrimSpace(iface.IPv4)
			path.Reason = fmt.Sprintf("interface %s bears the transport address %s, so it is the management NIC", iface.Name, path.InterfaceIP)
			return path
		}
	}
	path.Ambiguous = true
	path.Reason = fmt.Sprintf("no interface bears the transport address %s (the session is NATed/relayed or the address is off-box); every interface and the default gateway are protected", host)
	return path
}

// IsManagementInterface reports whether name is the resolved management NIC. An
// ambiguous path reports every interface as management (fail closed).
func (p ManagementPath) IsManagementInterface(name string) bool {
	if p.Ambiguous {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(p.Interface))
}

// GuardVerdict is the never-sever guard's decision for one change.
type GuardVerdict struct {
	Protected  bool   `json:"protected" jsonschema:"Whether the change touches the protected management path"`
	Severs     bool   `json:"severs" jsonschema:"Whether the change could sever the current management transport"`
	Allowed    bool   `json:"allowed" jsonschema:"Whether the change may proceed (not protected, or overridden)"`
	Overridden bool   `json:"overridden,omitempty" jsonschema:"Whether allow_connectivity_break was used to proceed past a sever verdict"`
	Reason     string `json:"reason" jsonschema:"Which rule decided the verdict"`
}

func decide(protected, severs, override bool, reason string) GuardVerdict {
	v := GuardVerdict{Protected: protected, Severs: severs, Reason: reason}
	if !protected {
		v.Allowed = true
		return v
	}
	if override {
		v.Allowed = true
		v.Overridden = true
	}
	return v
}

// GeneralChange is a patch-only change to the general network settings. A nil
// field is left unchanged (patch-only ownership).
type GeneralChange struct {
	Hostname               *string `json:"hostname,omitempty" jsonschema:"New NAS hostname (DSM server_name); omit to leave unchanged"`
	DefaultGatewayV4       *string `json:"default_gateway_v4,omitempty" jsonschema:"New IPv4 default gateway; omit to leave unchanged. Changing it is session-severing and refused without allow_connectivity_break"`
	DNSPrimary             *string `json:"dns_primary,omitempty" jsonschema:"New primary DNS nameserver; omit to leave unchanged. Setting a DNS server switches DNS to manual"`
	DNSSecondary           *string `json:"dns_secondary,omitempty" jsonschema:"New secondary DNS nameserver; omit to leave unchanged"`
	IPv4First              *bool   `json:"ipv4_first,omitempty" jsonschema:"Prefer IPv4 over IPv6 in name resolution; omit to leave unchanged"`
	AllowConnectivityBreak bool    `json:"allow_connectivity_break,omitempty" jsonschema:"Override the never-sever guard; required to change the default gateway"`
}

// MergeGeneral overlays the patch onto a freshly read general block. Omitted
// fields are preserved. Setting any DNS server flips dns_manual on.
func MergeGeneral(current General, change GeneralChange) General {
	merged := current
	if change.Hostname != nil {
		merged.Hostname = strings.TrimSpace(*change.Hostname)
	}
	if change.DefaultGatewayV4 != nil {
		merged.DefaultGatewayV4 = strings.TrimSpace(*change.DefaultGatewayV4)
	}
	if change.DNSPrimary != nil {
		merged.DNSPrimary = strings.TrimSpace(*change.DNSPrimary)
		merged.DNSManual = true
	}
	if change.DNSSecondary != nil {
		merged.DNSSecondary = strings.TrimSpace(*change.DNSSecondary)
		merged.DNSManual = true
	}
	if change.IPv4First != nil {
		merged.IPv4First = *change.IPv4First
	}
	return merged
}

// EvaluateGeneralChange runs the never-sever guard for a general change. Only a
// default-gateway change touches the management path (fail closed regardless of
// how the connection was resolved); hostname and DNS changes are medium risk.
func EvaluateGeneralChange(path ManagementPath, current General, change GeneralChange) GuardVerdict {
	merged := MergeGeneral(current, change)
	if change.DefaultGatewayV4 != nil && strings.TrimSpace(merged.DefaultGatewayV4) != strings.TrimSpace(current.DefaultGatewayV4) {
		return decide(true, true, change.AllowConnectivityBreak,
			fmt.Sprintf("changing the default gateway from %q to %q can sever the management path", current.DefaultGatewayV4, merged.DefaultGatewayV4))
	}
	return decide(false, false, change.AllowConnectivityBreak, "the change does not alter the default gateway or any management-path route")
}

// GeneralChangeIsNoop reports whether the merged general equals the current one.
func GeneralChangeIsNoop(current General, change GeneralChange) bool {
	merged := MergeGeneral(current, change)
	return merged.Hostname == current.Hostname &&
		merged.DefaultGatewayV4 == current.DefaultGatewayV4 &&
		merged.DNSPrimary == current.DNSPrimary &&
		merged.DNSSecondary == current.DNSSecondary &&
		merged.DNSManual == current.DNSManual &&
		merged.IPv4First == current.IPv4First
}

// InterfaceChange is a patch-only change to one NIC. A nil field is left
// unchanged.
type InterfaceChange struct {
	Name                   string  `json:"name" jsonschema:"Logical interface name to change, for example eth1"`
	IPv4                   *string `json:"ipv4,omitempty" jsonschema:"New static IPv4 address; omit to leave unchanged"`
	Netmask                *string `json:"netmask,omitempty" jsonschema:"New IPv4 subnet mask; omit to leave unchanged"`
	GatewayV4              *string `json:"gateway_v4,omitempty" jsonschema:"New per-interface IPv4 gateway; omit to leave unchanged"`
	UseDHCP                *bool   `json:"use_dhcp,omitempty" jsonschema:"Switch DHCP on/off; omit to leave unchanged. Switching the management NIC to DHCP is session-severing (its address may change)"`
	MTU                    *int    `json:"mtu,omitempty" jsonschema:"New MTU in bytes (9000 = jumbo frames); omit to leave unchanged"`
	AllowConnectivityBreak bool    `json:"allow_connectivity_break,omitempty" jsonschema:"Override the never-sever guard; required to change or disable the management NIC"`
}

// MergeInterface overlays the patch onto a freshly read interface. Omitted
// fields are preserved (patch-only ownership).
func MergeInterface(current Interface, change InterfaceChange) Interface {
	merged := current
	if change.IPv4 != nil {
		merged.IPv4 = strings.TrimSpace(*change.IPv4)
	}
	if change.Netmask != nil {
		merged.Netmask = strings.TrimSpace(*change.Netmask)
	}
	if change.GatewayV4 != nil {
		merged.GatewayV4 = strings.TrimSpace(*change.GatewayV4)
	}
	if change.UseDHCP != nil {
		merged.UseDHCP = *change.UseDHCP
	}
	if change.MTU != nil {
		merged.MTU = *change.MTU
	}
	return merged
}

// InterfaceChangeIsNoop reports whether the merged interface equals the current.
func InterfaceChangeIsNoop(current Interface, change InterfaceChange) bool {
	merged := MergeInterface(current, change)
	return merged.IPv4 == current.IPv4 &&
		merged.Netmask == current.Netmask &&
		merged.GatewayV4 == current.GatewayV4 &&
		merged.UseDHCP == current.UseDHCP &&
		merged.MTU == current.MTU
}

// EvaluateInterfaceChange runs the never-sever guard for an interface change. Any
// change to the management NIC (or any change at all when the path is ambiguous)
// is treated as session-severing; a change to a non-management NIC is permitted.
func EvaluateInterfaceChange(path ManagementPath, current Interface, change InterfaceChange) GuardVerdict {
	if InterfaceChangeIsNoop(current, change) {
		return decide(false, false, change.AllowConnectivityBreak, "the change does not alter any interface field")
	}
	if path.Ambiguous {
		return decide(true, true, change.AllowConnectivityBreak,
			fmt.Sprintf("the connection is ambiguous (%s); every interface is protected, so changing %s is treated as session-severing", path.Reason, change.Name))
	}
	if path.IsManagementInterface(change.Name) {
		return decide(true, true, change.AllowConnectivityBreak,
			fmt.Sprintf("interface %s carries the management transport (%s); changing its IP/mask/DHCP/MTU can sever the only connection", change.Name, path.InterfaceIP))
	}
	return decide(false, false, change.AllowConnectivityBreak,
		fmt.Sprintf("interface %s does not carry the management transport (that is %s); the change does not affect the current connection", change.Name, path.Interface))
}

// InterfaceChangeFields lists the interface fields the change actually alters,
// for the plan summary.
func InterfaceChangeFields(current Interface, change InterfaceChange) []string {
	merged := MergeInterface(current, change)
	fields := []string{}
	if merged.IPv4 != current.IPv4 {
		fields = append(fields, fmt.Sprintf("ipv4 %q->%q", current.IPv4, merged.IPv4))
	}
	if merged.Netmask != current.Netmask {
		fields = append(fields, fmt.Sprintf("netmask %q->%q", current.Netmask, merged.Netmask))
	}
	if merged.GatewayV4 != current.GatewayV4 {
		fields = append(fields, fmt.Sprintf("gateway %q->%q", current.GatewayV4, merged.GatewayV4))
	}
	if merged.UseDHCP != current.UseDHCP {
		fields = append(fields, fmt.Sprintf("use_dhcp %t->%t", current.UseDHCP, merged.UseDHCP))
	}
	if merged.MTU != current.MTU {
		fields = append(fields, fmt.Sprintf("mtu %d->%d", current.MTU, merged.MTU))
	}
	return fields
}

// GeneralChangeFields lists the general fields the change actually alters.
func GeneralChangeFields(current General, change GeneralChange) []string {
	merged := MergeGeneral(current, change)
	fields := []string{}
	if merged.Hostname != current.Hostname {
		fields = append(fields, fmt.Sprintf("hostname %q->%q", current.Hostname, merged.Hostname))
	}
	if merged.DefaultGatewayV4 != current.DefaultGatewayV4 {
		fields = append(fields, fmt.Sprintf("default_gateway %q->%q", current.DefaultGatewayV4, merged.DefaultGatewayV4))
	}
	if merged.DNSPrimary != current.DNSPrimary {
		fields = append(fields, fmt.Sprintf("dns_primary %q->%q", current.DNSPrimary, merged.DNSPrimary))
	}
	if merged.DNSSecondary != current.DNSSecondary {
		fields = append(fields, fmt.Sprintf("dns_secondary %q->%q", current.DNSSecondary, merged.DNSSecondary))
	}
	if merged.IPv4First != current.IPv4First {
		fields = append(fields, fmt.Sprintf("ipv4_first %t->%t", current.IPv4First, merged.IPv4First))
	}
	return fields
}
