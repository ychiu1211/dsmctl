package firewall

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// This file implements the self-lockout guard's rule evaluator. It reproduces
// DSM's firewall precedence — first enabled rule that matches wins, otherwise the
// adapter's default (no-match) policy, otherwise (a "none" default) the
// all-interfaces "global" section, otherwise the firewall's ultimate default of
// allow — evaluated against the management tuple {source IP, DSM port, tcp}.
//
// The evaluator is deliberately FAIL-CLOSED: a session is reported reachable only
// when the ruleset PROVABLY allows it. A rule whose match cannot be decided (an
// unresolved service-set port reference, a source-port rule, an unparseable
// value) never counts as allowing the session, but a deny/drop rule that MIGHT
// match is treated as blocking. This asymmetry means the guard errs toward
// refusing an apply rather than approving one that could sever the session.

// matchState is a rule's match confidence against the management tuple.
type matchState int

const (
	matchNo    matchState = iota // the rule definitely does not match the tuple
	matchMaybe                   // the rule may match; cannot be decided
	matchYes                     // the rule definitely matches the tuple
)

// Reachability is the guard verdict for a management tuple against a profile.
type Reachability struct {
	Allowed bool   // true only if the ruleset provably allows the session
	Reason  string // which rule or default decided the verdict, for the plan/warnings
}

// ProfileAllowsSession reports whether profile, once enforced, would provably
// allow conn. Because dsmctl cannot know which physical interface the management
// traffic ingresses on, it fails closed: the session must be allowed under EVERY
// candidate ingress chain — each configured physical adapter's chain and the
// all-interfaces chain that any unconfigured adapter defers to. If any candidate
// chain does not provably allow the session, the whole verdict is not-allowed.
func ProfileAllowsSession(profile ProfileRules, conn Connection) Reachability {
	global, physical := splitAdapters(profile.Adapters)

	var chains []reachChain
	for i := range physical {
		if adapterConfigured(physical[i]) {
			adapter := physical[i]
			chains = append(chains, reachChain{primary: &adapter, global: global})
		}
	}
	// The all-interfaces (global) chain is ALWAYS a candidate: DSM's Profile.get
	// returns only configured adapters, so any of the box's physical NICs that is
	// not configured in this profile ingresses management traffic straight into the
	// global section. dsmctl cannot know which NIC the operator is on, so it fails
	// closed by requiring the session to survive on every candidate chain,
	// including this one.
	chains = append(chains, reachChain{primary: nil, global: global})

	for _, chain := range chains {
		if r := chain.evaluate(conn); !r.Allowed {
			return r
		}
	}
	return Reachability{Allowed: true, Reason: "the resulting active ruleset allows the current session"}
}

type reachChain struct {
	primary *AdapterPolicy // the ingress adapter's own section, or nil to start at global
	global  *AdapterPolicy // the all-interfaces section, or nil if the profile has none
}

func (c reachChain) evaluate(conn Connection) Reachability {
	if c.primary != nil {
		if r, decided := evaluateSection(*c.primary, conn); decided {
			return r
		}
		// primary default is "none": fall through to the global section.
	}
	if c.global != nil {
		if r, decided := evaluateSection(*c.global, conn); decided {
			return r
		}
	}
	// No section decided: the firewall's ultimate default is allow.
	return Reachability{Allowed: true, Reason: "no rule or default policy matched; the firewall default allows the session"}
}

// evaluateSection walks one adapter section's enabled rules in order and applies
// its default policy. decided is false only when the section neither matches a
// rule nor carries an explicit default (policy "none"), so the caller falls
// through to the next section.
func evaluateSection(section AdapterPolicy, conn Connection) (r Reachability, decided bool) {
	for i, rule := range section.Rules {
		if !rule.Enabled {
			continue
		}
		state := ruleMatch(rule, conn)
		switch {
		case state == matchYes:
			return Reachability{
				Allowed: policyAllows(rule.Policy),
				Reason:  fmt.Sprintf("rule %d (%s) on %s matches and its action is %q", i+1, ruleLabel(rule), section.Adapter, rule.Policy),
			}, true
		case state == matchMaybe && !policyAllows(rule.Policy):
			// A deny/drop rule that might match blocks the session (fail closed).
			return Reachability{
				Allowed: false,
				Reason:  fmt.Sprintf("rule %d (%s) on %s may match and its action is %q; treated as blocking", i+1, ruleLabel(rule), section.Adapter, rule.Policy),
			}, true
		}
		// matchNo, or a maybe-allow (which cannot be relied on): keep scanning.
	}
	switch normalizePolicy(section.Policy) {
	case PolicyAllow:
		return Reachability{Allowed: true, Reason: fmt.Sprintf("no rule matched; %s default policy allows", section.Adapter)}, true
	case PolicyDeny, PolicyDrop:
		return Reachability{Allowed: false, Reason: fmt.Sprintf("no rule matched; %s default policy is %q", section.Adapter, section.Policy)}, true
	default:
		// "none" (or unset): defer to the next section.
		return Reachability{}, false
	}
}

// ruleMatch decides whether rule matches the management tuple.
func ruleMatch(rule Rule, conn Connection) matchState {
	return combineMatch(
		protocolMatch(rule.Protocol, conn.Protocol),
		portMatch(rule, conn.Port),
		sourceMatch(rule, conn.Source),
	)
}

func combineMatch(states ...matchState) matchState {
	result := matchYes
	for _, s := range states {
		if s == matchNo {
			return matchNo
		}
		if s == matchMaybe {
			result = matchMaybe
		}
	}
	return result
}

func protocolMatch(ruleProto, connProto string) matchState {
	ruleProto = strings.ToLower(strings.TrimSpace(ruleProto))
	connProto = strings.ToLower(strings.TrimSpace(connProto))
	if ruleProto == "" || ruleProto == "all" {
		return matchYes
	}
	if ruleProto == connProto {
		return matchYes
	}
	return matchNo
}

// portMatch decides whether the rule's port set covers the management port. A
// source-port rule (port_direction=src) constrains the client's ephemeral port,
// which is unknown, so it can neither be relied on nor definitively excluded.
func portMatch(rule Rule, port int) matchState {
	switch strings.TrimSpace(rule.PortGroup) {
	case "", PortAll:
		return matchYes
	case PortList, PortRange:
		if !isDestinationPorts(rule.PortDirection) {
			return matchMaybe // source-port constraint on an unknown ephemeral port
		}
		return portsCover(rule.Ports, port)
	case PortService:
		// A built-in service/application set: resolving exactly which ports it
		// spans needs the service-port catalog, which is not resolved here. Treat
		// as undecidable so a deny service rule fails closed and an allow service
		// rule is not relied on.
		return matchMaybe
	default:
		return matchMaybe
	}
}

func isDestinationPorts(direction string) bool {
	switch strings.TrimSpace(direction) {
	case "", PortDirDst, "dst":
		return true
	default:
		return false
	}
}

// portsCover parses a DSM port field ("80", "80,443", "8000:9000", "8000-9000")
// and reports whether it covers port. An unparseable field is undecidable.
func portsCover(spec string, port int) matchState {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == PortAll {
		return matchYes
	}
	parsedAny := false
	for _, token := range strings.FieldsFunc(spec, func(r rune) bool { return r == ',' || r == ' ' }) {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		lo, hi, ok := parsePortToken(token)
		if !ok {
			return matchMaybe
		}
		parsedAny = true
		if port >= lo && port <= hi {
			return matchYes
		}
	}
	if !parsedAny {
		return matchMaybe
	}
	return matchNo
}

func parsePortToken(token string) (lo, hi int, ok bool) {
	sep := strings.IndexAny(token, ":-")
	if sep < 0 {
		p, err := strconv.Atoi(token)
		if err != nil {
			return 0, 0, false
		}
		return p, p, true
	}
	loStr, hiStr := token[:sep], token[sep+1:]
	lo, err1 := strconv.Atoi(strings.TrimSpace(loStr))
	hi, err2 := strconv.Atoi(strings.TrimSpace(hiStr))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi, true
}

// sourceMatch decides whether the rule's source set covers the management source
// IP. An unparseable source value is undecidable.
func sourceMatch(rule Rule, source string) matchState {
	ip := net.ParseIP(strings.TrimSpace(source))
	switch strings.TrimSpace(rule.SourceGroup) {
	case "", SourceAll:
		return matchYes
	case SourceIP:
		target := net.ParseIP(strings.TrimSpace(rule.Source))
		if ip == nil || target == nil {
			return matchMaybe
		}
		if target.Equal(ip) {
			return matchYes
		}
		return matchNo
	case SourceNetmask:
		return cidrContains(rule.Source, ip)
	case SourceIPRange:
		return rangeContains(rule.Source, ip)
	default:
		return matchMaybe
	}
}

func cidrContains(spec string, ip net.IP) matchState {
	spec = strings.TrimSpace(spec)
	_, subnet, err := net.ParseCIDR(spec)
	if err != nil || ip == nil {
		// DSM may store netmask as "ip/mask" or "ip netmask"; if it is not a clean
		// CIDR it is undecidable.
		return matchMaybe
	}
	if subnet.Contains(ip) {
		return matchYes
	}
	return matchNo
}

func rangeContains(spec string, ip net.IP) matchState {
	spec = strings.TrimSpace(spec)
	sep := strings.IndexAny(spec, "-~")
	if sep < 0 || ip == nil {
		return matchMaybe
	}
	lo := net.ParseIP(strings.TrimSpace(spec[:sep]))
	hi := net.ParseIP(strings.TrimSpace(spec[sep+1:]))
	if lo == nil || hi == nil {
		return matchMaybe
	}
	if bytesCompareIP(ip, lo) >= 0 && bytesCompareIP(ip, hi) <= 0 {
		return matchYes
	}
	return matchNo
}

// bytesCompareIP compares two IPs of the same family. Mixed families compare as
// their 16-byte forms, which is sufficient for range containment of same-family
// endpoints.
func bytesCompareIP(a, b net.IP) int {
	a16, b16 := a.To16(), b.To16()
	for i := range a16 {
		if a16[i] != b16[i] {
			if a16[i] < b16[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func policyAllows(policy string) bool {
	return normalizePolicy(policy) == PolicyAllow
}

func normalizePolicy(policy string) string {
	return strings.ToLower(strings.TrimSpace(policy))
}

func adapterConfigured(adapter AdapterPolicy) bool {
	return len(adapter.Rules) > 0 || normalizePolicy(adapter.Policy) != PolicyNone && normalizePolicy(adapter.Policy) != ""
}

func splitAdapters(adapters []AdapterPolicy) (global *AdapterPolicy, physical []AdapterPolicy) {
	for i := range adapters {
		if adapters[i].Adapter == GlobalAdapter {
			g := adapters[i]
			global = &g
			continue
		}
		physical = append(physical, adapters[i])
	}
	return global, physical
}

// MergeProfile overlays the desired adapter sections onto current: each desired
// adapter fully replaces that adapter's default policy and ordered rule list
// (full-desired-state ownership), while adapters not named in desired are
// preserved verbatim. It is used identically by the write path and the guard, so
// the ruleset the guard evaluates is exactly the one that will be written.
func MergeProfile(current ProfileRules, desired []AdapterPolicy) ProfileRules {
	merged := ProfileRules{Profile: current.Profile, IsActive: current.IsActive}
	replaced := make(map[string]AdapterPolicy, len(desired))
	for _, adapter := range desired {
		replaced[adapter.Adapter] = adapter
	}
	seen := make(map[string]bool, len(current.Adapters))
	for _, adapter := range current.Adapters {
		seen[adapter.Adapter] = true
		if replacement, ok := replaced[adapter.Adapter]; ok {
			replacement.Total = len(replacement.Rules)
			merged.Adapters = append(merged.Adapters, replacement)
			continue
		}
		merged.Adapters = append(merged.Adapters, adapter)
	}
	for _, adapter := range desired {
		if !seen[adapter.Adapter] {
			adapter.Total = len(adapter.Rules)
			merged.Adapters = append(merged.Adapters, adapter)
		}
	}
	return merged
}

func ruleLabel(rule Rule) string {
	if strings.TrimSpace(rule.Name) != "" {
		return rule.Name
	}
	return "unnamed"
}
