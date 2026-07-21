// Package firewall contains stable, DSM-version-independent models for the
// Control Panel > Security > Firewall surface: the global enable flag and active
// profile, the named firewall profiles, the network adapters (interfaces), and
// each profile's per-adapter default (no-match) policy plus its ordered rule
// list. WebAPI names and DSM field names stay behind the operation package.
//
// Every area is a separate DSM API and a separate compatibility/failure boundary,
// so a NAS missing one still reports the others. Slice A reads; Slice B (WI-066)
// adds the guarded, lockout-simulated writes (rule create/delete/reorder, default
// policy, firewall enable/disable) through the hash-bound plan/apply contract.
//
// Live-verified on DSM 7.3 (lab). The rule-list envelope (the ordered array plus
// the per-adapter policy) and the per-rule FIELD names are both confirmed: Slice B
// wrote a throwaway rule to the non-active profile with the firewall disabled and
// read it back, resolving the field names the read slice originally left
// WIRE-UNVERIFIED. The write wire (Profile.set full-profile replacement,
// Profile.Apply.start to enable/activate, Firewall.set set_type=disable) was
// reverse-engineered from the DSM admin_center UI and confirmed live.
package firewall

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "firewall"

// Adapter policy tokens observed / expected from DSM. Only PolicyNone was seen on
// the lab (firewall disabled, no rules); the rest are the DSM UI vocabulary and
// are surfaced as raw strings, not enforced.
// The adapter default (no-match) policy. Live-verified vocabulary: DSM stores
// "allow" or "drop" when a default is set, and "none" when the adapter defers to
// the all-interfaces ("global") section. There is no adapter-level "deny" in the
// DSM UI (the no-match choice is allow vs drop), but "deny" is accepted defensively
// and treated as blocking.
const (
	PolicyNone  = "none"  // no adapter default set: defer to the global (all-interfaces) section
	PolicyAllow = "allow" // allow packets that match no rule
	PolicyDeny  = "deny"  // deny (reject) packets that match no rule
	PolicyDrop  = "drop"  // silently drop packets that match no rule

	// GlobalAdapter is the DSM "all interfaces" pseudo-adapter. A physical adapter
	// whose default policy is "none" defers to this section; when it too matches
	// nothing and carries no default, the firewall's ultimate default is allow.
	GlobalAdapter = "global"
)

// Status is the global firewall state (SYNO.Core.Security.Firewall get) plus the
// enumerated network adapters (SYNO.Core.Security.Firewall.Adapter list). In DSM
// 7.3 a single profile is active across all adapters; the per-adapter policy and
// rules live inside that profile (see ProfileRules).
type Status struct {
	Enabled       bool     `json:"enabled" jsonschema:"Whether the firewall is enabled (DSM field enable_firewall)"`
	ActiveProfile string   `json:"active_profile" jsonschema:"Name of the currently applied firewall profile (DSM field profile_name)"`
	Adapters      []string `json:"adapters" jsonschema:"Network adapter/interface names the firewall knows about, for example eth0 and the all-interfaces pseudo-adapter global"`
}

// Profile is one named firewall profile (a rule group). DSM ships a "default" and
// a "custom" profile; a profile is referenced by name.
type Profile struct {
	Name     string `json:"name" jsonschema:"Profile name"`
	IsActive bool   `json:"is_active" jsonschema:"Whether this profile is the one currently applied (matches Status.active_profile)"`
}

// Source-group and port-group tokens observed live from DSM. These are the exact
// DSM tokens for source_ip_group and port_group; the guard's rule evaluator keys
// off them.
const (
	SourceAll     = "all"     // matches any source
	SourceIP      = "ip"      // a single IP in source
	SourceIPRange = "iprange" // an inclusive range "a-b" in source
	SourceNetmask = "netmask" // a CIDR/netmask subnet in source

	PortAll       = "all"         // matches every port
	PortService   = "service"     // a built-in service/application set named in ports
	PortList      = "ports"       // an explicit port list in ports
	PortRange     = "ports_range" // an explicit port range in ports
	PortDirDst    = "destination" // rule matches on destination port (the listening/service port)
	PortDirSource = "src"         // rule matches on source port (the client's ephemeral port)
)

// Rule is one firewall rule inside an adapter's ordered rule list. The list order
// is the DSM first-match evaluation order.
//
// The field set is live-verified (WI-066 Slice B): a throwaway rule written to the
// non-active profile with the firewall disabled, then read back, confirmed the
// exact DSM tokens. DSM stores: enable, name, policy, protocol, port_direction,
// port_group, ports, source_ip_group, source_ip, log. (There is no ip_version at
// the rule level.) The decoder reads them tolerantly and never fails on a missing
// field.
type Rule struct {
	Enabled       bool   `json:"enabled" jsonschema:"Whether the rule is active in evaluation (DSM field enable)"`
	Policy        string `json:"policy" jsonschema:"Action for a matching packet: allow, deny, or drop"`
	Protocol      string `json:"protocol,omitempty" jsonschema:"Transport protocol the rule matches: tcp, udp, or all"`
	PortDirection string `json:"port_direction,omitempty" jsonschema:"Which side the port set applies to: destination (the service port) or src (the client port). DSM field port_direction"`
	PortGroup     string `json:"port_group,omitempty" jsonschema:"How the ports are expressed: all, service (a built-in service/application set), ports (a list), or ports_range. DSM field port_group"`
	Ports         string `json:"ports,omitempty" jsonschema:"Destination ports, a port range, or the referenced service/application set name (DSM field ports)"`
	SourceGroup   string `json:"source_group,omitempty" jsonschema:"How the source is expressed: all, ip, iprange, or netmask. DSM field source_ip_group"`
	Source        string `json:"source,omitempty" jsonschema:"The source value (IP, range, or subnet) when the source is not all. DSM field source_ip"`
	Log           bool   `json:"log" jsonschema:"Whether a match is logged (DSM field log)"`
	Name          string `json:"name,omitempty" jsonschema:"Optional human label for the rule"`
}

// AdapterPolicy is one network adapter's default (no-match) policy and its ordered
// rule list within a profile (SYNO.Core.Security.Firewall.Profile get returns one
// of these per configured adapter, keyed by adapter name).
type AdapterPolicy struct {
	Adapter string `json:"adapter" jsonschema:"Adapter/interface name, for example eth0 or the all-interfaces pseudo-adapter global"`
	Policy  string `json:"policy" jsonschema:"Default action for packets that match no rule on this adapter: allow, deny, drop, or none (raw DSM token)"`
	Total   int    `json:"total" jsonschema:"Number of rules on this adapter"`
	Rules   []Rule `json:"rules" jsonschema:"The adapter's ordered rule list (DSM first-match evaluation order)"`
}

// ProfileRules is a single profile's per-adapter default policy and ordered rules.
type ProfileRules struct {
	Profile  string          `json:"profile" jsonschema:"Profile name"`
	IsActive bool            `json:"is_active" jsonschema:"Whether this is the currently applied profile"`
	Adapters []AdapterPolicy `json:"adapters" jsonschema:"Per-adapter default policy and ordered rules configured in this profile"`
}

// RuleSet is the firewall rule view across one or more profiles.
type RuleSet struct {
	ActiveProfile string         `json:"active_profile" jsonschema:"Name of the currently applied profile"`
	Profiles      []ProfileRules `json:"profiles" jsonschema:"Each requested profile with its per-adapter policy and ordered rules"`
}

// Capabilities reports which firewall reads and guarded writes dsmctl currently
// exposes for the selected NAS. Each area is gated on its own DSM API so a NAS
// missing one still reports the others.
type Capabilities struct {
	Module                   string `json:"module" jsonschema:"Stable module name: firewall"`
	StatusRead               bool   `json:"status_read" jsonschema:"Whether the global enable flag and active profile can be read"`
	ProfilesRead             bool   `json:"profiles_read" jsonschema:"Whether the firewall profile list can be read"`
	AdaptersRead             bool   `json:"adapters_read" jsonschema:"Whether the network adapter list can be read"`
	RulesRead                bool   `json:"rules_read" jsonschema:"Whether a profile's per-adapter policy and ordered rules can be read"`
	RuleFieldsWireUnverified bool   `json:"rule_fields_wire_unverified" jsonschema:"True only if the per-rule field decoding is unverified; false once a live rule round-trip has confirmed the field names (WI-066 Slice B confirmed them)"`
	ProfileWrite             bool   `json:"profile_write" jsonschema:"Whether a profile's rules and default policy can be changed (Profile.set)"`
	EnableWrite              bool   `json:"enable_write" jsonschema:"Whether the firewall can be enabled/disabled and the active profile switched (Profile.Apply / Firewall.set)"`
	Mutations                bool   `json:"mutations" jsonschema:"Whether any guarded write is available"`
}

// Connection is the management tuple the self-lockout guard protects: the source
// address DSM sees for the operator's session and the DSM port dsmctl is connected
// on. Firewall rules are evaluated against this tuple, so if the resulting active
// ruleset would not ALLOW it the apply is refused.
type Connection struct {
	Source     string `json:"source" jsonschema:"Source IP the NAS sees for the operator's current session"`
	Port       int    `json:"port" jsonschema:"DSM management port dsmctl is connected over"`
	Protocol   string `json:"protocol" jsonschema:"Transport protocol of the management session (tcp)"`
	Determined bool   `json:"determined" jsonschema:"Whether the source was determined from an active connection; false means it was supplied via keep_reachable or is unknown"`
}

// SessionSource is one active client connection as reported by
// SYNO.Core.CurrentConnection, reduced to the guard-relevant fields. It carries no
// session secret. Current marks the connection DSM flags as the requesting one.
type SessionSource struct {
	From    string `json:"from" jsonschema:"Source IP the NAS sees for this connection"`
	Who     string `json:"who,omitempty" jsonschema:"Account name of the connection"`
	Current bool   `json:"current" jsonschema:"Whether DSM flags this as the current (requesting) connection"`
}

// ProfileChange is a full-desired-state change to one firewall profile's adapter
// sections (rule create/delete/reorder and default policy). Because DSM's Profile
// set replaces the whole profile, the request carries the complete desired ordered
// rule list and default policy for each touched adapter; untouched adapters are
// merged from the freshly-read profile so they are never rewritten.
type ProfileChange struct {
	Profile  string          `json:"profile" jsonschema:"Profile whose adapter sections to rewrite"`
	Adapters []AdapterPolicy `json:"adapters" jsonschema:"Desired per-adapter default policy and complete ordered rule list for each adapter being changed"`
	// Activate requests that DSM apply (activate) the profile as part of the write
	// (Profile.set profile_applying=true). When false the rules are saved but not
	// applied. Activating a profile while the firewall is enabled, or activating a
	// non-active profile, makes it take effect and triggers the self-lockout guard.
	Activate bool `json:"activate" jsonschema:"Whether to apply/activate the profile as part of the save (Profile.set profile_applying)"`
	// AllowConnectivityBreak overrides the self-lockout guard for an effect-taking
	// change whose result would not ALLOW the operator's current session.
	AllowConnectivityBreak bool `json:"allow_connectivity_break,omitempty" jsonschema:"Override the never-lockout guard; required when the resulting active ruleset would deny the operator's session"`
	// KeepReachable is the source IP/CIDR the guard treats as the management source
	// when it cannot be read from an active connection (NAT/relay). Fails closed if
	// the source is undeterminable and this is empty.
	KeepReachable string `json:"keep_reachable,omitempty" jsonschema:"Source IP or CIDR the never-lockout guard protects when the live source cannot be determined"`
}

// EnableChange is a change to the global firewall enable flag and/or the active
// profile. Enabling the firewall (or switching the active profile while enabled)
// makes an active ruleset take effect and triggers the self-lockout guard;
// disabling removes all filtering and cannot lock the operator out.
type EnableChange struct {
	Enabled                bool   `json:"enabled" jsonschema:"Desired global firewall enable state"`
	Profile                string `json:"profile,omitempty" jsonschema:"Profile to make active when enabling; defaults to the currently active profile"`
	AllowConnectivityBreak bool   `json:"allow_connectivity_break,omitempty" jsonschema:"Override the never-lockout guard; required when enabling with an active ruleset that would deny the operator's session"`
	KeepReachable          string `json:"keep_reachable,omitempty" jsonschema:"Source IP or CIDR the never-lockout guard protects when the live source cannot be determined"`
}
