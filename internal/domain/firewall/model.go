// Package firewall contains stable, DSM-version-independent models for the
// Control Panel > Security > Firewall surface: the global enable flag and active
// profile, the named firewall profiles, the network adapters (interfaces), and
// each profile's per-adapter default (no-match) policy plus its ordered rule
// list. WebAPI names and DSM field names stay behind the operation package.
//
// This is the read slice (WI-066 Slice A). Every area is a separate DSM API and
// a separate compatibility/failure boundary, so a NAS missing one still reports
// the others. The module reads only; the guarded, lockout-simulated writes (rule
// CRUD/reorder, default policy, firewall enable/disable) are a deferred
// follow-on and are out of scope here.
//
// Live-verified on DSM 7.3 (lab). Both shipped profiles ("default", "custom")
// carried zero rules, so the rule-list envelope (the ordered array plus the
// per-adapter policy) is confirmed, but the per-rule FIELD names in Rule are
// best-knowledge and could not be observed against a populated rule; they are
// marked WIRE-UNVERIFIED and decoded tolerantly. Creating a rule to confirm them
// is a mutation and is deliberately not done in the read slice.
package firewall

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "firewall"

// Adapter policy tokens observed / expected from DSM. Only PolicyNone was seen on
// the lab (firewall disabled, no rules); the rest are the DSM UI vocabulary and
// are surfaced as raw strings, not enforced.
const (
	PolicyNone  = "none"  // no explicit default (DSM default when unconfigured)
	PolicyAllow = "allow" // allow packets that match no rule
	PolicyDeny  = "deny"  // deny (reject) packets that match no rule
	PolicyDrop  = "drop"  // silently drop packets that match no rule
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

// Rule is one firewall rule inside an adapter's ordered rule list. The list order
// is the DSM first-match evaluation order.
//
// WIRE-UNVERIFIED: both lab profiles carried zero rules, so these per-rule field
// names are best-knowledge (from the DSM firewall UI / spec) and were not
// confirmed against a populated rule. The decoder reads them tolerantly and never
// fails on a missing field; a future pass with a live rule (or the guarded write
// slice) will confirm the exact tokens.
type Rule struct {
	Enabled    bool   `json:"enabled" jsonschema:"Whether the rule is active in evaluation"`
	Policy     string `json:"policy" jsonschema:"Action for a matching packet: allow, deny, or drop (raw DSM token)"`
	Protocol   string `json:"protocol,omitempty" jsonschema:"Transport protocol the rule matches: tcp, udp, or all"`
	IPVersion  string `json:"ip_version,omitempty" jsonschema:"IP version the rule matches: ipv4 or ipv6"`
	SourceType string `json:"source_type,omitempty" jsonschema:"How the source is expressed: all, ip, subnet, range, or geoip"`
	Source     string `json:"source,omitempty" jsonschema:"The source value (IP, subnet, range, or country code), when the source is not all"`
	PortType   string `json:"port_type,omitempty" jsonschema:"How the destination ports are expressed: all, a built-in service/application set, or custom"`
	Ports      string `json:"ports,omitempty" jsonschema:"Destination ports or the referenced service/application set name"`
	Direction  string `json:"direction,omitempty" jsonschema:"Traffic direction the rule matches (DSM firewall rules are inbound)"`
	Name       string `json:"name,omitempty" jsonschema:"Optional human label for the rule"`
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

// Capabilities reports which firewall reads dsmctl currently exposes for the
// selected NAS. Each read area is gated on its own DSM API so a NAS missing one
// still reports the others. Guarded writes are a deferred follow-on, so Mutations
// is always false in this slice.
type Capabilities struct {
	Module                   string `json:"module" jsonschema:"Stable module name: firewall"`
	StatusRead               bool   `json:"status_read" jsonschema:"Whether the global enable flag and active profile can be read"`
	ProfilesRead             bool   `json:"profiles_read" jsonschema:"Whether the firewall profile list can be read"`
	AdaptersRead             bool   `json:"adapters_read" jsonschema:"Whether the network adapter list can be read"`
	RulesRead                bool   `json:"rules_read" jsonschema:"Whether a profile's per-adapter policy and ordered rules can be read"`
	RuleFieldsWireUnverified bool   `json:"rule_fields_wire_unverified" jsonschema:"True while per-rule field decoding is unverified because the lab carried no rules to confirm the field names against"`
	Mutations                bool   `json:"mutations" jsonschema:"Whether any guarded write is available (always false in the read slice)"`
}
