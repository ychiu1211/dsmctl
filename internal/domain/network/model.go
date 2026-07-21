// Package network contains stable, DSM-version-independent models for the
// Control Panel > Network surface: the general settings (hostname, default
// gateway, DNS, proxy), the per-NIC Ethernet configuration and link status,
// link-aggregation bonds, and the static-route table. WebAPI names and DSM
// field names stay behind the operation package.
//
// Every area is a separate DSM API and a separate compatibility/failure
// boundary, so a NAS missing one still reports the others. This is the Slice A
// (read-only) model of WI-069; the guarded, session-severing writes are the
// deferred Slice B follow-on and are intentionally absent here.
//
// Live-verified on DSM 7.3 (lab, build 81168):
//   - General: SYNO.Core.Network get (v1/v2) — hostname (server_name), IPv4/IPv6
//     default gateway, DHCP-vs-manual DNS, and the default-gateway interface.
//   - Interfaces: SYNO.Core.Network.Ethernet list (v1/v2) — the rich per-NIC
//     record (ip/mask/gateway/dns/mtu/dhcp/speed/duplex/link/vlan/ipv6).
//   - Bonds: SYNO.Core.Network.Bond list (v1/v2) — the envelope is verified
//     (empty array on the lab, which has no bond); the per-bond FIELD names
//     (mode, member NICs) could not be live-verified and are marked
//     wire-unverified below.
//   - Proxy: SYNO.Core.Network.Proxy get (v1) — the HTTP/HTTPS proxy. The DSM
//     response carries a masked proxy password; the decoder never surfaces it
//     (presence/secret hygiene), only the enable/host/port/username fields.
//   - Routes: SYNO.Core.Network.Router.Static.Route get (v1) — the method
//     EXISTS but returned code 4302 on the lab because no static-route/advanced-
//     routing feature is configured; the success FIELD shape could not be
//     live-verified and is marked wire-unverified.
//   - Traffic control: SYNO.Core.Network.TrafficControl.Rules load (v1) — the
//     method exists but requires a parameter that could not be discovered
//     (code 114); this area is capability-detected only (no decoder shipped),
//     per the WI's "do not ship a guessed decoder" rule.
package network

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "network"

// LinkConnected / LinkDisconnected are the observed DSM link-status tokens
// (SYNO.Core.Network.Ethernet list "status" field). Surfaced as raw strings.
const (
	LinkConnected    = "connected"
	LinkDisconnected = "disconnected"
)

// General is the DSM global network configuration (SYNO.Core.Network get).
// Volatile fields are omitted; only stable configuration is modeled.
type General struct {
	Hostname         string           `json:"hostname" jsonschema:"NAS server name / hostname (DSM field server_name)"`
	DefaultGatewayV4 string           `json:"default_gateway_v4" jsonschema:"IPv4 default gateway address (DSM field gateway)"`
	DefaultGatewayV6 string           `json:"default_gateway_v6,omitempty" jsonschema:"IPv6 default gateway address, empty when IPv6 routing is not configured (DSM field v6gateway)"`
	DNSPrimary       string           `json:"dns_primary,omitempty" jsonschema:"Primary DNS nameserver (DSM field dns_primary)"`
	DNSSecondary     string           `json:"dns_secondary,omitempty" jsonschema:"Secondary DNS nameserver (DSM field dns_secondary)"`
	DNSManual        bool             `json:"dns_manual" jsonschema:"True when DNS nameservers are configured manually; false when they are supplied by DHCP (DSM field dns_manual)"`
	UseDHCPDomain    bool             `json:"use_dhcp_domain" jsonschema:"Whether the DHCP-supplied search domain is used (DSM field use_dhcp_domain)"`
	IPv4First        bool             `json:"ipv4_first" jsonschema:"Whether IPv4 is preferred over IPv6 in name resolution (DSM field ipv4_first)"`
	MultiGateway     bool             `json:"multi_gateway" jsonschema:"Whether multiple gateways / advanced routing are enabled (DSM field multi_gateway)"`
	ARPIgnore        bool             `json:"arp_ignore" jsonschema:"Whether ARP-ignore is enabled (DSM field arp_ignore)"`
	IPConflictDetect bool             `json:"ip_conflict_detect" jsonschema:"Whether IP-conflict detection is enabled (DSM field enable_ip_conflict_detect; SYNO.Core.Network v2 only)"`
	DefaultGateway   GatewayInterface `json:"default_gateway" jsonschema:"The interface that carries the default gateway (DSM field gateway_info)"`
	Proxy            ProxySettings    `json:"proxy" jsonschema:"HTTP/HTTPS proxy configuration (SYNO.Core.Network.Proxy). The proxy password is never surfaced"`
}

// GatewayInterface identifies the interface DSM reports as carrying the default
// gateway (SYNO.Core.Network get "gateway_info").
type GatewayInterface struct {
	Interface string `json:"interface,omitempty" jsonschema:"Logical interface name carrying the default gateway (DSM field ifname)"`
	IP        string `json:"ip,omitempty" jsonschema:"IP address on that interface"`
	Netmask   string `json:"netmask,omitempty" jsonschema:"Subnet mask on that interface (DSM field mask)"`
	Status    string `json:"status,omitempty" jsonschema:"Link status of that interface"`
	Type      string `json:"type,omitempty" jsonschema:"Interface type, for example lan or pppoe"`
	UseDHCP   bool   `json:"use_dhcp" jsonschema:"Whether that interface obtains its address via DHCP"`
}

// ProxySettings is the DSM HTTP/HTTPS proxy configuration
// (SYNO.Core.Network.Proxy get). The proxy PASSWORD is a secret and is never
// modeled or surfaced — only presence/config metadata is exposed.
type ProxySettings struct {
	Supported      bool   `json:"supported" jsonschema:"Whether the proxy area could be read on this NAS"`
	Enabled        bool   `json:"enabled" jsonschema:"Whether an outbound HTTP/HTTPS proxy is enabled (DSM field enable)"`
	AuthEnabled    bool   `json:"auth_enabled" jsonschema:"Whether the proxy requires authentication (DSM field enable_auth). The password itself is never surfaced"`
	Username       string `json:"username,omitempty" jsonschema:"Proxy auth username, when authentication is enabled (DSM field username). Not a secret; the password is never included"`
	HTTPHost       string `json:"http_host,omitempty" jsonschema:"HTTP proxy host (DSM field http_host)"`
	HTTPPort       string `json:"http_port,omitempty" jsonschema:"HTTP proxy port (DSM field http_port)"`
	HTTPSHost      string `json:"https_host,omitempty" jsonschema:"HTTPS proxy host, when a distinct HTTPS proxy is configured (DSM field https_host)"`
	HTTPSPort      string `json:"https_port,omitempty" jsonschema:"HTTPS proxy port (DSM field https_port)"`
	BypassLocal    bool   `json:"bypass_local" jsonschema:"Whether local addresses bypass the proxy (DSM field enable_bypass)"`
	DifferentHTTPS bool   `json:"different_https" jsonschema:"Whether a separate HTTPS proxy host is used (DSM field enable_different_host)"`
}

// IPv6Address is one IPv6 assignment on an interface (an element of the DSM
// Ethernet "ipv6" array, or the SYNO.Core.Network.IPv6 get record). The array
// was empty on the lab (IPv6 off on every NIC), so the element FIELD names are
// wire-unverified; the decoder reads them tolerantly.
type IPv6Address struct {
	Address      string `json:"address,omitempty" jsonschema:"IPv6 address"`
	PrefixLength int    `json:"prefix_length,omitempty" jsonschema:"IPv6 prefix length"`
	Type         string `json:"type,omitempty" jsonschema:"IPv6 assignment mode, for example auto, manual, dhcpv6, or off"`
}

// Interface is one NIC's configuration and link status
// (SYNO.Core.Network.Ethernet list). Read-only.
type Interface struct {
	Name             string        `json:"name" jsonschema:"Logical interface name, for example eth0 or bond0 (DSM field ifname)"`
	Type             string        `json:"type,omitempty" jsonschema:"Interface type, for example lan, pppoe, or ovs (DSM field type)"`
	IPv4             string        `json:"ipv4,omitempty" jsonschema:"IPv4 address (DSM field ip)"`
	Netmask          string        `json:"netmask,omitempty" jsonschema:"IPv4 subnet mask (DSM field mask)"`
	GatewayV4        string        `json:"gateway_v4,omitempty" jsonschema:"Per-interface IPv4 gateway (DSM field gateway)"`
	DNS              string        `json:"dns,omitempty" jsonschema:"Per-interface DNS nameserver (DSM field dns)"`
	UseDHCP          bool          `json:"use_dhcp" jsonschema:"Whether the interface obtains its IPv4 address via DHCP (DSM field use_dhcp)"`
	MTU              int           `json:"mtu,omitempty" jsonschema:"Effective MTU in bytes; 9000 indicates jumbo frames (DSM field mtu)"`
	MTUConfig        int           `json:"mtu_config,omitempty" jsonschema:"Configured MTU in bytes (DSM field mtu_config)"`
	LinkStatus       string        `json:"link_status,omitempty" jsonschema:"Link status: connected or disconnected (DSM field status)"`
	SpeedMbps        int           `json:"speed_mbps,omitempty" jsonschema:"Negotiated link speed in Mbps; -1 when the link is down (DSM field speed)"`
	MaxSpeedMbps     int           `json:"max_speed_mbps,omitempty" jsonschema:"Maximum supported link speed in Mbps (DSM field max_supported_speed)"`
	FullDuplex       bool          `json:"full_duplex" jsonschema:"Whether the link negotiated full duplex (DSM field duplex)"`
	IsDefaultGateway bool          `json:"is_default_gateway" jsonschema:"Whether this interface carries the default gateway (DSM field is_default_gateway)"`
	VLANEnabled      bool          `json:"vlan_enabled" jsonschema:"Whether 802.1Q VLAN tagging is enabled on this interface (DSM field enable_vlan)"`
	VLANID           int           `json:"vlan_id,omitempty" jsonschema:"VLAN id when VLAN tagging is enabled (DSM field vlan_id)"`
	IPv6             []IPv6Address `json:"ipv6,omitempty" jsonschema:"IPv6 assignments on this interface (DSM field ipv6). Empty when IPv6 is off"`
}

// Bond is one link-aggregation interface (SYNO.Core.Network.Bond list). The
// list ENVELOPE is live-verified (empty on the lab); the per-bond FIELD names
// (Mode, Members) are wire-unverified because the lab has no bond, so the
// decoder reads them tolerantly and the capability report flags them.
type Bond struct {
	Name    string   `json:"name" jsonschema:"Bond interface name, for example bond0 (DSM field ifname)"`
	Type    string   `json:"type,omitempty" jsonschema:"Interface type (DSM field type)"`
	IPv4    string   `json:"ipv4,omitempty" jsonschema:"Bond IPv4 address (DSM field ip)"`
	Netmask string   `json:"netmask,omitempty" jsonschema:"Bond IPv4 subnet mask (DSM field mask)"`
	Status  string   `json:"status,omitempty" jsonschema:"Bond link status (DSM field status)"`
	UseDHCP bool     `json:"use_dhcp" jsonschema:"Whether the bond obtains its address via DHCP (DSM field use_dhcp)"`
	Mode    string   `json:"mode,omitempty" jsonschema:"Bonding mode, for example active-backup, balance-xor, 802.3ad/LACP, or ALB. WIRE-UNVERIFIED: no bond existed on the lab to confirm the DSM field name"`
	Members []string `json:"members,omitempty" jsonschema:"Member (slave) NIC names in the bond. WIRE-UNVERIFIED: no bond existed on the lab to confirm the DSM field name"`
}

// Route is one entry of the static-route table
// (SYNO.Core.Network.Router.Static.Route get). WIRE-UNVERIFIED: the method
// exists but returned code 4302 on the lab (no advanced routing configured),
// so the success FIELD shape could not be live-confirmed; the decoder reads it
// tolerantly and the capability report flags it.
type Route struct {
	Destination string `json:"destination,omitempty" jsonschema:"Destination network address"`
	Netmask     string `json:"netmask,omitempty" jsonschema:"Destination subnet mask or prefix"`
	Gateway     string `json:"gateway,omitempty" jsonschema:"Next-hop gateway address"`
	Interface   string `json:"interface,omitempty" jsonschema:"Egress interface name"`
	Family      string `json:"family,omitempty" jsonschema:"Address family: ipv4 or ipv6"`
}

// RouteTable is the static-route view. Configured reports whether DSM returned a
// route table at all (false when the advanced-routing feature is off, which is
// the lab's state).
type RouteTable struct {
	Configured bool    `json:"configured" jsonschema:"Whether DSM returned a static-route table; false when advanced routing / static routes are not configured on the NAS"`
	Routes     []Route `json:"routes" jsonschema:"The static routes, when configured"`
}

// Capabilities reports which network reads dsmctl exposes for the selected NAS
// and which areas' field decoding is still wire-unverified. Each area is gated
// on its own DSM API so a NAS missing one still reports the others. This slice
// exposes no mutations.
type Capabilities struct {
	Module                    string `json:"module" jsonschema:"Stable module name: network"`
	GeneralRead               bool   `json:"general_read" jsonschema:"Whether the general network settings can be read"`
	InterfacesRead            bool   `json:"interfaces_read" jsonschema:"Whether the per-interface configuration and link status can be read"`
	BondsRead                 bool   `json:"bonds_read" jsonschema:"Whether the link-aggregation bond list can be read"`
	RoutesRead                bool   `json:"routes_read" jsonschema:"Whether the static-route table API is present (the read is attempted; on a NAS without advanced routing it returns an empty, not-configured table)"`
	TrafficControlRead        bool   `json:"traffic_control_read" jsonschema:"Whether the traffic-control (bandwidth) rules API is present. Capability-detected only: the read parameter could not be live-verified, so no decoder is shipped this pass"`
	ProxyRead                 bool   `json:"proxy_read" jsonschema:"Whether the outbound proxy configuration can be read"`
	BondFieldsWireUnverified  bool   `json:"bond_fields_wire_unverified" jsonschema:"True while the per-bond mode/member field decoding is unverified (no bond existed on the lab to confirm the DSM field names)"`
	RouteFieldsWireUnverified bool   `json:"route_fields_wire_unverified" jsonschema:"True while the per-route field decoding is unverified (the lab has no static routes; the method returned code 4302)"`
	IPv6FieldsWireUnverified  bool   `json:"ipv6_fields_wire_unverified" jsonschema:"True while the per-interface IPv6 element decoding is unverified (IPv6 was off on every lab NIC)"`
	Mutations                 bool   `json:"mutations" jsonschema:"Whether any guarded write is available (false: this is the read-only slice)"`
}
