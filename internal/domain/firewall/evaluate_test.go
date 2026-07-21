package firewall

import "testing"

// conn is the management tuple used across the evaluator tests: the operator's
// session source as the NAS sees it, on the DSM port, over tcp.
var conn = Connection{Source: "192.0.2.69", Port: 5001, Protocol: "tcp"}

func global(policy string, rules ...Rule) ProfileRules {
	return ProfileRules{Profile: "default", Adapters: []AdapterPolicy{{Adapter: GlobalAdapter, Policy: policy, Rules: rules, Total: len(rules)}}}
}

func rule(policy, proto, portGroup, ports, srcGroup, src string) Rule {
	return Rule{Enabled: true, Policy: policy, Protocol: proto, PortDirection: PortDirDst, PortGroup: portGroup, Ports: ports, SourceGroup: srcGroup, Source: src}
}

// TestProfileAllowsSession is a table of DSM first-match + adapter-default
// verdicts the never-lockout guard depends on. Each row asserts whether the
// resulting active ruleset provably allows the management session.
func TestProfileAllowsSession(t *testing.T) {
	allowAll := rule(PolicyAllow, "tcp", PortAll, "", SourceAll, "")
	denyAll := rule(PolicyDeny, "tcp", PortAll, "", SourceAll, "")
	allowDSMPort := rule(PolicyAllow, "tcp", PortList, "5001", SourceAll, "")
	allowOtherPort := rule(PolicyAllow, "tcp", PortList, "5000", SourceAll, "")
	allowFromMe := rule(PolicyAllow, "tcp", PortList, "5001", SourceIP, "192.0.2.69")
	allowFromOther := rule(PolicyAllow, "tcp", PortList, "5001", SourceIP, "10.0.0.9")
	allowNetmask := rule(PolicyAllow, "tcp", PortList, "5001", SourceNetmask, "192.0.2.0/24")
	allowRange := rule(PolicyAllow, "tcp", PortAll, "", SourceIPRange, "192.0.2.1-192.0.2.200")
	allowServiceSet := rule(PolicyAllow, "tcp", PortService, "DSM", SourceAll, "")
	denyServiceSet := rule(PolicyDeny, "tcp", PortService, "DSM", SourceAll, "")
	allowRangeUDP := rule(PolicyAllow, "udp", PortAll, "", SourceAll, "")

	cases := []struct {
		name    string
		profile ProfileRules
		allowed bool
	}{
		{"default-allow no rules", global(PolicyAllow), true},
		{"default-drop no rules (lockout)", global(PolicyDrop), false},
		{"default-none no rules (ultimate allow)", global(PolicyNone), true},
		{"default-deny + allow dsm port", global(PolicyDrop, allowDSMPort), true},
		{"default-deny + allow-all rule", global(PolicyDrop, allowAll), true},
		{"default-allow + deny-all first", global(PolicyAllow, denyAll), false},
		{"default-deny + allow other port only", global(PolicyDrop, allowOtherPort), false},
		{"default-deny + allow from my ip", global(PolicyDrop, allowFromMe), true},
		{"default-deny + allow from other ip", global(PolicyDrop, allowFromOther), false},
		{"default-deny + allow from my subnet", global(PolicyDrop, allowNetmask), true},
		{"default-deny + allow from my range", global(PolicyDrop, allowRange), true},
		{"first-match deny beats later allow", global(PolicyAllow, denyAll, allowAll), false},
		{"first-match allow beats later deny", global(PolicyDrop, allowAll, denyAll), true},
		{"udp allow does not cover tcp session (default drop)", global(PolicyDrop, allowRangeUDP), false},
		{"maybe-allow service set not relied on (default drop)", global(PolicyDrop, allowServiceSet), false},
		{"maybe-deny service set blocks (default allow)", global(PolicyAllow, denyServiceSet), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ProfileAllowsSession(tc.profile, conn)
			if got.Allowed != tc.allowed {
				t.Fatalf("allowed = %t, want %t (reason: %s)", got.Allowed, tc.allowed, got.Reason)
			}
		})
	}
}

// TestProfileAllowsSessionSkipsDisabledRules proves a disabled deny rule does not
// block, and a disabled allow rule does not rescue a default-drop.
func TestProfileAllowsSessionSkipsDisabledRules(t *testing.T) {
	disabledDeny := rule(PolicyDeny, "tcp", PortAll, "", SourceAll, "")
	disabledDeny.Enabled = false
	if r := ProfileAllowsSession(global(PolicyAllow, disabledDeny), conn); !r.Allowed {
		t.Fatalf("disabled deny should not block: %s", r.Reason)
	}

	disabledAllow := rule(PolicyAllow, "tcp", PortAll, "", SourceAll, "")
	disabledAllow.Enabled = false
	if r := ProfileAllowsSession(global(PolicyDrop, disabledAllow), conn); r.Allowed {
		t.Fatalf("disabled allow should not rescue a default-drop: %s", r.Reason)
	}
}

// TestProfileAllowsSessionPhysicalAdapterFailsClosed proves that a configured
// physical adapter whose chain would block the session fails the whole verdict,
// even when the global chain would allow it, because dsmctl cannot know which NIC
// carries the management traffic.
func TestProfileAllowsSessionPhysicalAdapterFailsClosed(t *testing.T) {
	profile := ProfileRules{Profile: "default", Adapters: []AdapterPolicy{
		{Adapter: "eth0", Policy: PolicyDrop},                 // configured, no allow -> blocks
		{Adapter: GlobalAdapter, Policy: PolicyAllow},          // global would allow
	}}
	if r := ProfileAllowsSession(profile, conn); r.Allowed {
		t.Fatalf("a drop-by-default configured NIC must fail the verdict closed: %s", r.Reason)
	}
}

// TestProfileAllowsSessionPhysicalDefersToGlobal proves a physical adapter with a
// "none" default defers to the global section (which allows here).
func TestProfileAllowsSessionPhysicalDefersToGlobal(t *testing.T) {
	profile := ProfileRules{Profile: "default", Adapters: []AdapterPolicy{
		{Adapter: "eth0", Policy: PolicyNone, Rules: []Rule{rule(PolicyAllow, "tcp", PortList, "9999", SourceAll, "")}},
		{Adapter: GlobalAdapter, Policy: PolicyAllow},
	}}
	if r := ProfileAllowsSession(profile, conn); !r.Allowed {
		t.Fatalf("eth0 none default should defer to global allow: %s", r.Reason)
	}
}

func TestPortsCover(t *testing.T) {
	cases := []struct {
		spec string
		port int
		want matchState
	}{
		{"5001", 5001, matchYes},
		{"5000,5001,5002", 5001, matchYes},
		{"5000-5010", 5001, matchYes},
		{"5000:5010", 5001, matchYes},
		{"22,80,443", 5001, matchNo},
		{"5002-5010", 5001, matchNo},
		{"all", 5001, matchYes},
		{"DSM", 5001, matchMaybe}, // unparseable service token
	}
	for _, tc := range cases {
		if got := portsCover(tc.spec, tc.port); got != tc.want {
			t.Errorf("portsCover(%q,%d) = %v, want %v", tc.spec, tc.port, got, tc.want)
		}
	}
}
