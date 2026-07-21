package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/firewall"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

// fakeFirewallClient implements firewallClient for the plan/apply and guard tests.
type fakeFirewallClient struct {
	status             synology.FirewallStatus
	profiles           map[string]synology.FirewallProfileRules
	sessions           []synology.FirewallSessionSource
	caps               synology.FirewallCapabilities
	port               int
	persist            bool
	breakPostcondition bool
	profileMutations   int
	enableMutations    int
}

func (c *fakeFirewallClient) FirewallStatus(context.Context) (synology.FirewallStatus, error) {
	return c.status, nil
}
func (c *fakeFirewallClient) FirewallProfiles(context.Context) ([]synology.FirewallProfile, error) {
	out := make([]synology.FirewallProfile, 0, len(c.profiles))
	for name := range c.profiles {
		out = append(out, synology.FirewallProfile{Name: name, IsActive: name == c.status.ActiveProfile})
	}
	return out, nil
}
func (c *fakeFirewallClient) FirewallRules(_ context.Context, profile string) (synology.FirewallRuleSet, error) {
	set := synology.FirewallRuleSet{ActiveProfile: c.status.ActiveProfile}
	if profile == "" {
		for _, p := range c.profiles {
			set.Profiles = append(set.Profiles, p)
		}
		return set, nil
	}
	if p, ok := c.profiles[profile]; ok {
		set.Profiles = append(set.Profiles, p)
	}
	return set, nil
}
func (c *fakeFirewallClient) FirewallCapabilities(context.Context) (synology.FirewallCapabilities, synology.CompatibilityReport, error) {
	return c.caps, synology.CompatibilityReport{}, nil
}
func (c *fakeFirewallClient) FirewallTransport() synology.FirewallConnection {
	return synology.FirewallConnection{Port: c.port, Protocol: "tcp"}
}
func (c *fakeFirewallClient) FirewallActiveSessions(context.Context) ([]synology.FirewallSessionSource, error) {
	return c.sessions, nil
}
func (c *fakeFirewallClient) ApplyFirewallProfileChange(_ context.Context, change synology.FirewallProfileChange) (synology.FirewallMutationResult, error) {
	c.profileMutations++
	if c.persist && !c.breakPostcondition {
		current := c.profiles[change.Profile]
		c.profiles[change.Profile] = firewall.MergeProfile(current, change.Adapters)
	}
	return synology.FirewallMutationResult{Backend: "firewall-profile-set-v1", API: "SYNO.Core.Security.Firewall.Profile", Version: 1, Method: "set"}, nil
}
func (c *fakeFirewallClient) ApplyFirewallEnableChange(_ context.Context, change synology.FirewallEnableChange) (synology.FirewallMutationResult, error) {
	c.enableMutations++
	if c.persist && !c.breakPostcondition {
		c.status.Enabled = change.Enabled
		if change.Enabled && change.Profile != "" {
			c.status.ActiveProfile = change.Profile
		}
	}
	return synology.FirewallMutationResult{Backend: "firewall-enable-v1", API: "SYNO.Core.Security.Firewall", Version: 1, Method: "set"}, nil
}

func adapter(name, policy string, rules ...synology.FirewallRule) synology.FirewallAdapterPolicy {
	return synology.FirewallAdapterPolicy{Adapter: name, Policy: policy, Rules: rules, Total: len(rules)}
}

func allowRule(ports string) synology.FirewallRule {
	return synology.FirewallRule{Enabled: true, Name: "allow-dsm", Policy: "allow", Protocol: "tcp", PortDirection: "destination", PortGroup: "ports", Ports: ports, SourceGroup: "all", Source: "all"}
}

func firewallTestClient() *fakeFirewallClient {
	return &fakeFirewallClient{
		status: synology.FirewallStatus{Enabled: false, ActiveProfile: "default", Adapters: []string{"eth0", "global"}},
		profiles: map[string]synology.FirewallProfileRules{
			"default": {Profile: "default", IsActive: true, Adapters: []synology.FirewallAdapterPolicy{adapter("global", "none")}},
			"custom":  {Profile: "custom", Adapters: []synology.FirewallAdapterPolicy{adapter("global", "none")}},
		},
		sessions: []synology.FirewallSessionSource{{From: "10.17.36.69", Who: "deryck", Current: true}},
		port:     5001,
		persist:  true,
		caps: synology.FirewallCapabilities{
			Module: firewall.ModuleName, StatusRead: true, ProfilesRead: true, AdaptersRead: true, RulesRead: true,
			ProfileWrite: true, EnableWrite: true, Mutations: true,
		},
	}
}

// ---- never-lockout guard: the required refuse-on-deny AND allow-on-permit proofs

// TestGuardRefusesEnablingLockout proves the guard refuses enabling the firewall
// with a deny-by-default active profile that lacks a rule allowing the session.
func TestGuardRefusesEnablingLockout(t *testing.T) {
	client := firewallTestClient()
	client.profiles["default"] = synology.FirewallProfileRules{Profile: "default", IsActive: true, Adapters: []synology.FirewallAdapterPolicy{adapter("global", "drop")}}

	_, err := planFirewallEnableWithClient(context.Background(), "lab", client, firewall.EnableChange{Enabled: true})
	if err == nil || !strings.Contains(err.Error(), "never-lockout guard refuses") {
		t.Fatalf("expected never-lockout refusal, got %v", err)
	}
}

// TestGuardAllowsEnablingWhenSessionPermitted proves the same enable is permitted
// once the active profile carries a rule allowing the session (and only then).
func TestGuardAllowsEnablingWhenSessionPermitted(t *testing.T) {
	client := firewallTestClient()
	client.profiles["default"] = synology.FirewallProfileRules{Profile: "default", IsActive: true, Adapters: []synology.FirewallAdapterPolicy{adapter("global", "drop", allowRule("5001"))}}

	plan, err := planFirewallEnableWithClient(context.Background(), "lab", client, firewall.EnableChange{Enabled: true})
	if err != nil {
		t.Fatalf("expected permit, got %v", err)
	}
	if !plan.Guard.Evaluated || !plan.Guard.Allowed || plan.Guard.Overridden {
		t.Fatalf("guard = %#v", plan.Guard)
	}
	if plan.Risk != "high" {
		t.Fatalf("enabling must be high risk, got %q", plan.Risk)
	}
}

// TestGuardOverrideProceeds proves allow_connectivity_break lets a lockout plan
// proceed with the guard flagged overridden.
func TestGuardOverrideProceeds(t *testing.T) {
	client := firewallTestClient()
	client.profiles["default"] = synology.FirewallProfileRules{Profile: "default", IsActive: true, Adapters: []synology.FirewallAdapterPolicy{adapter("global", "drop")}}

	plan, err := planFirewallEnableWithClient(context.Background(), "lab", client, firewall.EnableChange{Enabled: true, AllowConnectivityBreak: true})
	if err != nil {
		t.Fatalf("override should proceed, got %v", err)
	}
	if plan.Guard.Allowed || !plan.Guard.Overridden {
		t.Fatalf("guard = %#v", plan.Guard)
	}
}

// TestGuardFailsClosedWithoutSource proves the guard refuses an effect-taking
// change when the source cannot be determined and no keep_reachable is supplied.
func TestGuardFailsClosedWithoutSource(t *testing.T) {
	client := firewallTestClient()
	client.sessions = []synology.FirewallSessionSource{{From: "10.17.36.69", Current: false}} // none current
	client.profiles["default"] = synology.FirewallProfileRules{Profile: "default", IsActive: true, Adapters: []synology.FirewallAdapterPolicy{adapter("global", "drop", allowRule("5001"))}}

	_, err := planFirewallEnableWithClient(context.Background(), "lab", client, firewall.EnableChange{Enabled: true})
	if err == nil || !strings.Contains(err.Error(), "could not be determined") {
		t.Fatalf("expected fail-closed on undetermined source, got %v", err)
	}
}

// TestGuardKeepReachableSuppliesSource proves keep_reachable lets the guard verify
// the session when the live source cannot be read, and still refuses a lockout.
func TestGuardKeepReachableSuppliesSource(t *testing.T) {
	client := firewallTestClient()
	client.sessions = []synology.FirewallSessionSource{{From: "10.17.36.69", Current: false}}
	client.profiles["default"] = synology.FirewallProfileRules{Profile: "default", IsActive: true, Adapters: []synology.FirewallAdapterPolicy{adapter("global", "drop", allowRule("5001"))}}

	plan, err := planFirewallEnableWithClient(context.Background(), "lab", client, firewall.EnableChange{Enabled: true, KeepReachable: "10.17.36.69"})
	if err != nil {
		t.Fatalf("keep_reachable + allow rule should permit, got %v", err)
	}
	if !plan.Guard.Allowed || plan.Guard.Connection.Determined {
		t.Fatalf("guard = %#v", plan.Guard)
	}
	// The same keep_reachable but a drop profile with no allow rule must refuse.
	client.profiles["default"] = synology.FirewallProfileRules{Profile: "default", IsActive: true, Adapters: []synology.FirewallAdapterPolicy{adapter("global", "drop")}}
	if _, err := planFirewallEnableWithClient(context.Background(), "lab", client, firewall.EnableChange{Enabled: true, KeepReachable: "10.17.36.69"}); err == nil {
		t.Fatal("keep_reachable into a deny-all profile must refuse")
	}
}

// ---- enable/disable plan + apply + postcondition ----------------------------

func TestFirewallEnablePlanApply(t *testing.T) {
	client := firewallTestClient()
	client.profiles["default"] = synology.FirewallProfileRules{Profile: "default", IsActive: true, Adapters: []synology.FirewallAdapterPolicy{adapter("global", "drop", allowRule("5001"))}}

	plan, err := planFirewallEnableWithClient(context.Background(), "lab", client, firewall.EnableChange{Enabled: true})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	plan.Hash, err = firewallEnablePlanHash(plan)
	if err != nil {
		t.Fatalf("hash error = %v", err)
	}
	result, err := applyFirewallEnableWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if !result.Applied || client.enableMutations != 1 || !client.status.Enabled {
		t.Fatalf("result=%#v status=%#v", result, client.status)
	}
}

func TestFirewallDisableIsMediumNoGuard(t *testing.T) {
	client := firewallTestClient()
	client.status.Enabled = true
	plan, err := planFirewallEnableWithClient(context.Background(), "lab", client, firewall.EnableChange{Enabled: false})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" || plan.Guard.Evaluated {
		t.Fatalf("disable plan = %#v", plan)
	}
}

func TestFirewallEnableNoOpRejected(t *testing.T) {
	client := firewallTestClient() // already disabled
	if _, err := planFirewallEnableWithClient(context.Background(), "lab", client, firewall.EnableChange{Enabled: false}); err == nil {
		t.Fatal("disabling an already-disabled firewall should be a no-op error")
	}
}

// ---- profile change plan + apply --------------------------------------------

// TestProfileChangeNonActiveIsMedium proves a rule edit to a non-active profile
// with the firewall off does not take effect, is medium risk, and is not guarded.
func TestProfileChangeNonActiveIsMedium(t *testing.T) {
	client := firewallTestClient()
	change := firewall.ProfileChange{Profile: "custom", Adapters: []synology.FirewallAdapterPolicy{adapter("global", "none", allowRule("8080"))}}
	plan, err := planFirewallProfileWithClient(context.Background(), "lab", client, change)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.TakesEffect || plan.Guard.Evaluated || plan.Risk != "medium" {
		t.Fatalf("plan = %#v", plan)
	}
	plan.Hash, _ = firewallProfilePlanHash(plan)
	result, err := applyFirewallProfileWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if !result.Applied || client.profileMutations != 1 || len(client.profiles["custom"].Adapters[0].Rules) != 1 {
		t.Fatalf("result=%#v profile=%#v", result, client.profiles["custom"])
	}
}

// TestProfileChangeActiveEnabledGuarded proves editing the active profile while
// the firewall is enabled takes effect and is guarded: removing the allow rule
// that permits the session is refused.
func TestProfileChangeActiveEnabledGuarded(t *testing.T) {
	client := firewallTestClient()
	client.status.Enabled = true
	client.profiles["default"] = synology.FirewallProfileRules{Profile: "default", IsActive: true, Adapters: []synology.FirewallAdapterPolicy{adapter("global", "drop", allowRule("5001"))}}
	// Replace the global section with a drop-all (removing the allow rule).
	change := firewall.ProfileChange{Profile: "default", Adapters: []synology.FirewallAdapterPolicy{adapter("global", "drop")}}
	if _, err := planFirewallProfileWithClient(context.Background(), "lab", client, change); err == nil || !strings.Contains(err.Error(), "never-lockout guard refuses") {
		t.Fatalf("expected guard refusal, got %v", err)
	}
	// With the override it proceeds (high risk, guard overridden).
	change.AllowConnectivityBreak = true
	plan, err := planFirewallProfileWithClient(context.Background(), "lab", client, change)
	if err != nil {
		t.Fatalf("override plan error = %v", err)
	}
	if plan.Risk != "high" || !plan.TakesEffect || !plan.Guard.Overridden {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestProfileChangeNoOpRejected(t *testing.T) {
	client := firewallTestClient()
	// Writing the current (empty) global section back with no activation is a no-op.
	change := firewall.ProfileChange{Profile: "custom", Adapters: []synology.FirewallAdapterPolicy{adapter("global", "none")}}
	if _, err := planFirewallProfileWithClient(context.Background(), "lab", client, change); err == nil {
		t.Fatal("a no-op profile change should be rejected")
	}
}

// ---- staleness + postcondition ----------------------------------------------

func TestFirewallProfilePlanStale(t *testing.T) {
	client := firewallTestClient()
	change := firewall.ProfileChange{Profile: "custom", Adapters: []synology.FirewallAdapterPolicy{adapter("global", "none", allowRule("8080"))}}
	plan, err := planFirewallProfileWithClient(context.Background(), "lab", client, change)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	plan.Hash, _ = firewallProfilePlanHash(plan)
	// Mutate the observed profile out-of-band between plan and apply.
	client.profiles["custom"] = synology.FirewallProfileRules{Profile: "custom", Adapters: []synology.FirewallAdapterPolicy{adapter("global", "drop")}}
	if _, err := applyFirewallProfileWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale rejection, got %v", err)
	}
}

func TestFirewallProfilePostconditionFails(t *testing.T) {
	client := firewallTestClient()
	client.breakPostcondition = true // the write silently does not persist
	change := firewall.ProfileChange{Profile: "custom", Adapters: []synology.FirewallAdapterPolicy{adapter("global", "none", allowRule("8080"))}}
	plan, err := planFirewallProfileWithClient(context.Background(), "lab", client, change)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	plan.Hash, _ = firewallProfilePlanHash(plan)
	if _, err := applyFirewallProfileWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "verify firewall profile change") {
		t.Fatalf("expected postcondition failure, got %v", err)
	}
}

// TestFirewallProfilePlanHashDetectsTampering proves the approval hash covers the
// intent, so a mutated request invalidates the plan.
func TestFirewallProfilePlanHashDetectsTampering(t *testing.T) {
	client := firewallTestClient()
	change := firewall.ProfileChange{Profile: "custom", Adapters: []synology.FirewallAdapterPolicy{adapter("global", "none", allowRule("8080"))}}
	plan, err := planFirewallProfileWithClient(context.Background(), "lab", client, change)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if err := validateFirewallProfilePlan(plan, plan.Hash); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
	tampered := plan
	tampered.Request.Activate = true
	if err := validateFirewallProfilePlan(tampered, plan.Hash); err == nil {
		t.Fatal("tampered plan accepted")
	}
}
