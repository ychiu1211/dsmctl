package application

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/firewall"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const firewallAPIVersion = "dsmctl.io/v1alpha1"

// The firewall guarded writes follow the module plan/apply contract: the plan
// records and hashes the complete observed state (the full target profile, the
// global enable flag and active profile, AND the management connection tuple),
// apply re-reads and rejects a changed state, merges the intent into a freshly
// read profile, performs the typed write, and re-reads to verify.
//
// The defining safeguard is the never-lockout guard. dsmctl reaches DSM over a
// management tuple {source IP the NAS sees, DSM port, tcp}. Before any apply that
// would TAKE EFFECT — enabling the firewall, or changing the active profile / its
// default policy / its rules while the firewall is (or becomes) enabled — the
// guard evaluates the RESULTING active ruleset against that tuple with a local
// first-match + adapter-default evaluator (firewall.ProfileAllowsSession). If the
// result would not provably ALLOW the session, the apply is REFUSED unless the
// intent carries allow_connectivity_break. The tuple (source + port) is hashed
// into the plan, so a reconnection from a different source invalidates a stale
// plan. When the source cannot be read from an active connection and no
// keep_reachable is supplied, the guard fails closed.
//
// Risk: every effect-taking firewall write is HIGH (enabling, changing the active
// profile, a default-policy change, deleting/reordering a rule that could permit
// the management port). Adding an allow rule, or any edit to a non-active profile
// that does not take effect, is medium.

// ---- shared observed state + guard ------------------------------------------

// FirewallGuardResult is the guard's decision recorded in a plan.
type FirewallGuardResult struct {
	Evaluated  bool               `json:"evaluated" jsonschema:"Whether the never-lockout guard evaluated this change (true when it would take effect)"`
	Connection firewall.Connection `json:"connection" jsonschema:"The management tuple the guard protected"`
	Allowed    bool               `json:"allowed" jsonschema:"Whether the resulting active ruleset provably allows the current session"`
	Reason     string             `json:"reason" jsonschema:"Which rule or default policy decided the verdict"`
	Overridden bool               `json:"overridden,omitempty" jsonschema:"Whether allow_connectivity_break was used to proceed past a deny verdict"`
}

// FirewallApplyResult is returned by both firewall applies.
type FirewallApplyResult struct {
	NAS       string                          `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                          `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                            `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operation synology.FirewallMutationResult `json:"operation" jsonschema:"Selected DSM mutation backend"`
}

// determineConnection resolves the management tuple. keep_reachable (an IP or
// CIDR) is authoritative when supplied; otherwise the source is taken from the
// single active connection DSM flags as current. When neither yields a source the
// tuple is undetermined and the guard fails closed for an effect-taking change.
func determineConnection(transport firewall.Connection, sessions []synology.FirewallSessionSource, keepReachable string) firewall.Connection {
	conn := firewall.Connection{Port: transport.Port, Protocol: "tcp"}
	if trimmed := strings.TrimSpace(keepReachable); trimmed != "" {
		conn.Source = trimmed
		conn.Determined = false
		return conn
	}
	currents := map[string]bool{}
	for _, session := range sessions {
		if session.Current {
			if ip := strings.TrimSpace(session.From); ip != "" {
				currents[ip] = true
			}
		}
	}
	if len(currents) == 1 {
		for ip := range currents {
			conn.Source = ip
		}
		conn.Determined = true
	}
	return conn
}

// evaluateGuard runs the never-lockout guard against the resulting active profile.
// It returns the guard result, or an error that refuses the apply.
func evaluateGuard(conn firewall.Connection, resulting synology.FirewallProfileRules, allowBreak bool) (FirewallGuardResult, error) {
	if strings.TrimSpace(conn.Source) == "" {
		return FirewallGuardResult{Evaluated: true, Connection: conn}, fmt.Errorf(
			"the source of the management session could not be determined from an active connection; re-plan with keep_reachable set to the IP or CIDR dsmctl connects from so the never-lockout guard can verify the session survives")
	}
	probes, err := guardProbeIPs(conn.Source)
	if err != nil {
		return FirewallGuardResult{Evaluated: true, Connection: conn}, err
	}
	verdict := firewall.Reachability{Allowed: true, Reason: "the resulting active ruleset allows the current session"}
	for _, ip := range probes {
		r := firewall.ProfileAllowsSession(resulting, firewall.Connection{Source: ip, Port: conn.Port, Protocol: "tcp"})
		if !r.Allowed {
			verdict = r
			break
		}
	}
	result := FirewallGuardResult{Evaluated: true, Connection: conn, Allowed: verdict.Allowed, Reason: verdict.Reason}
	if !verdict.Allowed {
		if !allowBreak {
			return result, fmt.Errorf(
				"the never-lockout guard refuses this change: the resulting active firewall ruleset would not allow the current session (%s port %d): %s; re-plan with allow_connectivity_break set (and an out-of-band recovery path ready) to proceed",
				conn.Source, conn.Port, verdict.Reason)
		}
		result.Overridden = true
	}
	return result, nil
}

// guardProbeIPs turns a keep_reachable IP or CIDR (or a determined source IP) into
// the concrete IPs the guard checks. A single IP yields itself; a CIDR yields its
// first and last host addresses, and every one must be allowed for the change to
// pass (conservative for a source range).
func guardProbeIPs(source string) ([]string, error) {
	source = strings.TrimSpace(source)
	if ip := net.ParseIP(source); ip != nil {
		return []string{source}, nil
	}
	if _, subnet, err := net.ParseCIDR(source); err == nil {
		first, last := cidrHostBounds(subnet)
		return []string{first, last}, nil
	}
	return nil, fmt.Errorf("keep_reachable %q is not a valid IP address or CIDR subnet", source)
}

func cidrHostBounds(subnet *net.IPNet) (string, string) {
	first := make(net.IP, len(subnet.IP))
	copy(first, subnet.IP)
	last := make(net.IP, len(subnet.IP))
	for i := range last {
		last[i] = subnet.IP[i] | ^subnet.Mask[i]
	}
	return first.String(), last.String()
}

func protectedSourceList(sessions []synology.FirewallSessionSource) []string {
	seen := map[string]bool{}
	sources := make([]string, 0, len(sessions))
	for _, session := range sessions {
		ip := strings.TrimSpace(session.From)
		if ip == "" || seen[ip] {
			continue
		}
		seen[ip] = true
		sources = append(sources, ip)
	}
	return sources
}

// ---- profile change (rules + default policy) --------------------------------

type FirewallProfileObserved struct {
	Status           synology.FirewallStatus       `json:"status" jsonschema:"Global enable flag and active profile observed during planning"`
	Profile          synology.FirewallProfileRules `json:"profile" jsonschema:"Complete observed target profile (all adapter sections and ordered rules)"`
	Connection       firewall.Connection           `json:"connection" jsonschema:"Management tuple observed during planning"`
	ProtectedSources []string                      `json:"protected_sources" jsonschema:"Source IPs of all active connections at plan time"`
}

type FirewallProfilePlan struct {
	APIVersion          string                    `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                    `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                    `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             firewall.ProfileChange    `json:"request" jsonschema:"Validated full-desired-state profile change intent"`
	Observed            FirewallProfileObserved   `json:"observed" jsonschema:"Complete observed state hashed into the plan"`
	ObservedFingerprint string                    `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Resulting           synology.FirewallProfileRules `json:"resulting" jsonschema:"The merged profile that will be written (the guard evaluated this)"`
	TakesEffect         bool                      `json:"takes_effect" jsonschema:"Whether the change would take effect (and so was guarded)"`
	Guard               FirewallGuardResult       `json:"guard" jsonschema:"Never-lockout guard decision"`
	Risk                string                    `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                  `json:"warnings" jsonschema:"Connectivity and posture warnings"`
	Summary             []string                  `json:"summary" jsonschema:"Human-readable operations"`
	Hash                string                    `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

func (s *Service) PlanFirewallProfileChange(ctx context.Context, requestedNAS string, request firewall.ProfileChange) (FirewallProfilePlan, error) {
	if err := validateFirewallProfileShape(request); err != nil {
		return FirewallProfilePlan{}, err
	}
	name, client, err := s.firewallClient(ctx, requestedNAS)
	if err != nil {
		return FirewallProfilePlan{}, err
	}
	plan, err := planFirewallProfileWithClient(ctx, name, client, request)
	if err != nil {
		return FirewallProfilePlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = firewallProfilePlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyFirewallProfilePlan(ctx context.Context, plan FirewallProfilePlan, approvalHash string) (FirewallApplyResult, error) {
	if err := validateFirewallProfilePlan(plan, approvalHash); err != nil {
		return FirewallApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return FirewallApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return FirewallApplyResult{}, err
	}
	name, client, err := s.firewallClient(ctx, plan.NAS)
	if err != nil {
		return FirewallApplyResult{}, err
	}
	if name != plan.NAS {
		return FirewallApplyResult{}, fmt.Errorf("firewall profile plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyFirewallProfileWithClient(ctx, client, plan)
}

func planFirewallProfileWithClient(ctx context.Context, nas string, client firewallClient, request firewall.ProfileChange) (FirewallProfilePlan, error) {
	capabilities, _, err := client.FirewallCapabilities(ctx)
	if err != nil {
		return FirewallProfilePlan{}, authenticationError(nas, err)
	}
	if !capabilities.RulesRead || !capabilities.ProfileWrite {
		return FirewallProfilePlan{}, fmt.Errorf("NAS %q does not expose a verified firewall profile read/write backend", nas)
	}
	status, err := client.FirewallStatus(ctx)
	if err != nil {
		return FirewallProfilePlan{}, authenticationError(nas, err)
	}
	ruleSet, err := client.FirewallRules(ctx, request.Profile)
	if err != nil {
		return FirewallProfilePlan{}, authenticationError(nas, err)
	}
	if len(ruleSet.Profiles) != 1 {
		return FirewallProfilePlan{}, fmt.Errorf("firewall profile %q was not found on NAS %q", request.Profile, nas)
	}
	observedProfile := ruleSet.Profiles[0]
	resulting := firewall.MergeProfile(observedProfile, request.Adapters)
	if firewallProfilesEqual(observedProfile, resulting) && !request.Activate {
		return FirewallProfilePlan{}, fmt.Errorf("firewall profile change would not change the current configuration")
	}

	sessions, err := client.FirewallActiveSessions(ctx)
	if err != nil {
		return FirewallProfilePlan{}, authenticationError(nas, err)
	}
	conn := determineConnection(client.FirewallTransport(), sessions, request.KeepReachable)
	takesEffect := request.Activate || (status.Enabled && strings.EqualFold(status.ActiveProfile, request.Profile))

	plan := FirewallProfilePlan{
		APIVersion:  firewallAPIVersion,
		NAS:         nas,
		Request:     request,
		Observed:    FirewallProfileObserved{Status: status, Profile: observedProfile, Connection: conn, ProtectedSources: protectedSourceList(sessions)},
		Resulting:   resulting,
		TakesEffect: takesEffect,
	}
	if takesEffect {
		guard, err := evaluateGuard(conn, resulting, request.AllowConnectivityBreak)
		if err != nil {
			return FirewallProfilePlan{}, err
		}
		plan.Guard = guard
	}
	plan.Risk, plan.Warnings, plan.Summary = firewallProfileEffects(observedProfile, resulting, request, takesEffect, plan.Guard)
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return FirewallProfilePlan{}, err
	}
	plan.Hash, err = firewallProfilePlanHash(plan)
	if err != nil {
		return FirewallProfilePlan{}, err
	}
	return plan, nil
}

func applyFirewallProfileWithClient(ctx context.Context, client firewallClient, plan FirewallProfilePlan) (FirewallApplyResult, error) {
	current, err := planFirewallProfileWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return FirewallApplyResult{}, fmt.Errorf("firewall profile plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = firewallProfilePlanHash(current)
	if err != nil {
		return FirewallApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return FirewallApplyResult{}, fmt.Errorf("firewall profile plan is stale; create a new plan")
	}
	operation, err := client.ApplyFirewallProfileChange(ctx, plan.Request)
	if err != nil {
		return FirewallApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyFirewallProfilePostcondition(ctx, client, plan); err != nil {
		return FirewallApplyResult{}, fmt.Errorf("verify firewall profile change: %w", err)
	}
	return FirewallApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func verifyFirewallProfilePostcondition(ctx context.Context, client firewallClient, plan FirewallProfilePlan) error {
	ruleSet, err := client.FirewallRules(ctx, plan.Request.Profile)
	if err != nil {
		return err
	}
	if len(ruleSet.Profiles) != 1 {
		return fmt.Errorf("profile %q missing after write", plan.Request.Profile)
	}
	if !firewallProfilesEqual(ruleSet.Profiles[0], plan.Resulting) {
		return fmt.Errorf("the written profile does not match the desired state (DSM may have rejected or normalized a field); re-read and re-plan")
	}
	return nil
}

func validateFirewallProfileShape(change firewall.ProfileChange) error {
	if strings.TrimSpace(change.Profile) == "" {
		return fmt.Errorf("firewall profile change requires a profile name")
	}
	if len(change.Adapters) == 0 {
		return fmt.Errorf("firewall profile change requires at least one adapter section")
	}
	for _, adapter := range change.Adapters {
		if strings.TrimSpace(adapter.Adapter) == "" {
			return fmt.Errorf("firewall adapter section requires an adapter name")
		}
		if !validAdapterPolicy(adapter.Policy) {
			return fmt.Errorf("firewall adapter %q default policy %q is invalid (want allow, deny, drop, or none)", adapter.Adapter, adapter.Policy)
		}
		for i, rule := range adapter.Rules {
			if !validRulePolicy(rule.Policy) {
				return fmt.Errorf("firewall rule %d on adapter %q has invalid action %q (want allow, deny, or drop)", i+1, adapter.Adapter, rule.Policy)
			}
		}
	}
	if strings.TrimSpace(change.KeepReachable) != "" {
		if _, err := guardProbeIPs(change.KeepReachable); err != nil {
			return err
		}
	}
	return nil
}

func firewallProfileEffects(observed, resulting synology.FirewallProfileRules, request firewall.ProfileChange, takesEffect bool, guard FirewallGuardResult) (string, []string, []string) {
	summary := []string{}
	warnings := []string{}
	before := adaptersByName(observed.Adapters)
	high := false
	for _, adapter := range resulting.Adapters {
		prior, existed := before[adapter.Adapter]
		if !existed {
			summary = append(summary, fmt.Sprintf("configure adapter %s (default policy %s, %d rules)", adapter.Adapter, adapter.Policy, len(adapter.Rules)))
			continue
		}
		if !strings.EqualFold(prior.Policy, adapter.Policy) {
			summary = append(summary, fmt.Sprintf("change adapter %s default policy from %q to %q", adapter.Adapter, prior.Policy, adapter.Policy))
			if isBlockingPolicy(adapter.Policy) && !isBlockingPolicy(prior.Policy) {
				high = true
			}
		}
		if len(prior.Rules) != len(adapter.Rules) {
			summary = append(summary, fmt.Sprintf("change adapter %s rule count from %d to %d", adapter.Adapter, len(prior.Rules), len(adapter.Rules)))
		} else if !rulesEqual(prior.Rules, adapter.Rules) {
			summary = append(summary, fmt.Sprintf("rewrite adapter %s rules (order or fields changed)", adapter.Adapter))
		}
	}
	if request.Activate {
		summary = append(summary, "apply/activate the profile")
	}
	if len(summary) == 0 {
		summary = append(summary, "no adapter changes")
	}
	if takesEffect {
		high = true // any effect-taking firewall change is high risk
		if guard.Overridden {
			warnings = append(warnings, fmt.Sprintf("allow_connectivity_break acknowledged: the resulting ruleset would deny the current session (%s); an out-of-band recovery path is required", guard.Reason))
		}
	}
	risk := "medium"
	if high {
		risk = "high"
	}
	return risk, warnings, summary
}

func validateFirewallProfilePlan(plan FirewallProfilePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the firewall profile plan")
	}
	if plan.APIVersion != firewallAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid firewall profile plan metadata")
	}
	if err := validateFirewallProfileShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("firewall profile plan observed state was modified")
	}
	expectedHash, err := firewallProfilePlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("firewall profile plan contents were modified after planning")
	}
	return nil
}

func firewallProfilePlanHash(plan FirewallProfilePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// ---- firewall enable / disable ----------------------------------------------

type FirewallEnableObserved struct {
	Status           synology.FirewallStatus       `json:"status" jsonschema:"Global enable flag and active profile observed during planning"`
	ActiveProfile    synology.FirewallProfileRules `json:"active_profile" jsonschema:"The profile that would be active after the change, with its rules"`
	Connection       firewall.Connection           `json:"connection" jsonschema:"Management tuple observed during planning"`
	ProtectedSources []string                      `json:"protected_sources" jsonschema:"Source IPs of all active connections at plan time"`
}

type FirewallEnablePlan struct {
	APIVersion          string                  `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                  `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                  `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             firewall.EnableChange   `json:"request" jsonschema:"Validated firewall enable/disable intent"`
	Observed            FirewallEnableObserved  `json:"observed" jsonschema:"Complete observed state hashed into the plan"`
	ObservedFingerprint string                  `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Guard               FirewallGuardResult     `json:"guard" jsonschema:"Never-lockout guard decision (evaluated when enabling)"`
	Risk                string                  `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                `json:"warnings" jsonschema:"Connectivity and posture warnings"`
	Summary             []string                `json:"summary" jsonschema:"Human-readable operations"`
	Hash                string                  `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

func (s *Service) PlanFirewallEnableChange(ctx context.Context, requestedNAS string, request firewall.EnableChange) (FirewallEnablePlan, error) {
	name, client, err := s.firewallClient(ctx, requestedNAS)
	if err != nil {
		return FirewallEnablePlan{}, err
	}
	plan, err := planFirewallEnableWithClient(ctx, name, client, request)
	if err != nil {
		return FirewallEnablePlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = firewallEnablePlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyFirewallEnablePlan(ctx context.Context, plan FirewallEnablePlan, approvalHash string) (FirewallApplyResult, error) {
	if err := validateFirewallEnablePlan(plan, approvalHash); err != nil {
		return FirewallApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return FirewallApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return FirewallApplyResult{}, err
	}
	name, client, err := s.firewallClient(ctx, plan.NAS)
	if err != nil {
		return FirewallApplyResult{}, err
	}
	if name != plan.NAS {
		return FirewallApplyResult{}, fmt.Errorf("firewall enable plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyFirewallEnableWithClient(ctx, client, plan)
}

func planFirewallEnableWithClient(ctx context.Context, nas string, client firewallClient, request firewall.EnableChange) (FirewallEnablePlan, error) {
	capabilities, _, err := client.FirewallCapabilities(ctx)
	if err != nil {
		return FirewallEnablePlan{}, authenticationError(nas, err)
	}
	if !capabilities.EnableWrite {
		return FirewallEnablePlan{}, fmt.Errorf("NAS %q does not expose a verified firewall enable/disable backend", nas)
	}
	status, err := client.FirewallStatus(ctx)
	if err != nil {
		return FirewallEnablePlan{}, authenticationError(nas, err)
	}
	targetProfile := strings.TrimSpace(request.Profile)
	if targetProfile == "" {
		targetProfile = status.ActiveProfile
	}
	// No-op detection: disabling an already-disabled firewall, or enabling with the
	// same active profile already enabled.
	if request.Enabled && status.Enabled && strings.EqualFold(status.ActiveProfile, targetProfile) {
		return FirewallEnablePlan{}, fmt.Errorf("firewall is already enabled with profile %q; nothing to change", targetProfile)
	}
	if !request.Enabled && !status.Enabled {
		return FirewallEnablePlan{}, fmt.Errorf("firewall is already disabled; nothing to change")
	}

	sessions, err := client.FirewallActiveSessions(ctx)
	if err != nil {
		return FirewallEnablePlan{}, authenticationError(nas, err)
	}
	conn := determineConnection(client.FirewallTransport(), sessions, request.KeepReachable)

	var activeProfile synology.FirewallProfileRules
	plan := FirewallEnablePlan{APIVersion: firewallAPIVersion, NAS: nas, Request: firewall.EnableChange{Enabled: request.Enabled, Profile: targetProfile, AllowConnectivityBreak: request.AllowConnectivityBreak, KeepReachable: request.KeepReachable}}
	if request.Enabled {
		ruleSet, err := client.FirewallRules(ctx, targetProfile)
		if err != nil {
			return FirewallEnablePlan{}, authenticationError(nas, err)
		}
		if len(ruleSet.Profiles) != 1 {
			return FirewallEnablePlan{}, fmt.Errorf("firewall profile %q was not found on NAS %q", targetProfile, nas)
		}
		activeProfile = ruleSet.Profiles[0]
		guard, gerr := evaluateGuard(conn, activeProfile, request.AllowConnectivityBreak)
		if gerr != nil {
			return FirewallEnablePlan{}, gerr
		}
		plan.Guard = guard
	}
	plan.Observed = FirewallEnableObserved{Status: status, ActiveProfile: activeProfile, Connection: conn, ProtectedSources: protectedSourceList(sessions)}
	plan.Risk, plan.Warnings, plan.Summary = firewallEnableEffects(status, request.Enabled, targetProfile, plan.Guard)
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return FirewallEnablePlan{}, err
	}
	plan.Hash, err = firewallEnablePlanHash(plan)
	if err != nil {
		return FirewallEnablePlan{}, err
	}
	return plan, nil
}

func applyFirewallEnableWithClient(ctx context.Context, client firewallClient, plan FirewallEnablePlan) (FirewallApplyResult, error) {
	current, err := planFirewallEnableWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return FirewallApplyResult{}, fmt.Errorf("firewall enable plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = firewallEnablePlanHash(current)
	if err != nil {
		return FirewallApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return FirewallApplyResult{}, fmt.Errorf("firewall enable plan is stale; create a new plan")
	}
	operation, err := client.ApplyFirewallEnableChange(ctx, plan.Request)
	if err != nil {
		return FirewallApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyFirewallEnablePostcondition(ctx, client, plan.Request); err != nil {
		return FirewallApplyResult{}, fmt.Errorf("verify firewall enable change: %w", err)
	}
	return FirewallApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func verifyFirewallEnablePostcondition(ctx context.Context, client firewallClient, change firewall.EnableChange) error {
	status, err := client.FirewallStatus(ctx)
	if err != nil {
		return err
	}
	if status.Enabled != change.Enabled {
		return fmt.Errorf("enable_firewall is %t, want %t", status.Enabled, change.Enabled)
	}
	if change.Enabled && strings.TrimSpace(change.Profile) != "" && !strings.EqualFold(status.ActiveProfile, change.Profile) {
		return fmt.Errorf("active profile is %q, want %q", status.ActiveProfile, change.Profile)
	}
	return nil
}

func firewallEnableEffects(status synology.FirewallStatus, enabled bool, profile string, guard FirewallGuardResult) (string, []string, []string) {
	warnings := []string{}
	summary := []string{}
	if enabled {
		if !status.Enabled {
			summary = append(summary, fmt.Sprintf("enable the firewall with profile %q", profile))
		} else {
			summary = append(summary, fmt.Sprintf("switch the active profile to %q", profile))
		}
		if guard.Overridden {
			warnings = append(warnings, fmt.Sprintf("allow_connectivity_break acknowledged: enabling would deny the current session (%s); an out-of-band recovery path is required", guard.Reason))
		}
		return "high", warnings, summary
	}
	summary = append(summary, "disable the firewall")
	warnings = append(warnings, "disabling the firewall removes all packet filtering, weakening the security posture")
	return "medium", warnings, summary
}

func validateFirewallEnablePlan(plan FirewallEnablePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the firewall enable plan")
	}
	if plan.APIVersion != firewallAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid firewall enable plan metadata")
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("firewall enable plan observed state was modified")
	}
	expectedHash, err := firewallEnablePlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("firewall enable plan contents were modified after planning")
	}
	return nil
}

func firewallEnablePlanHash(plan FirewallEnablePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// ---- small helpers ----------------------------------------------------------

func validAdapterPolicy(policy string) bool {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case firewall.PolicyAllow, firewall.PolicyDeny, firewall.PolicyDrop, firewall.PolicyNone, "":
		return true
	default:
		return false
	}
}

func validRulePolicy(policy string) bool {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case firewall.PolicyAllow, firewall.PolicyDeny, firewall.PolicyDrop:
		return true
	default:
		return false
	}
}

func isBlockingPolicy(policy string) bool {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case firewall.PolicyDeny, firewall.PolicyDrop:
		return true
	default:
		return false
	}
}

func adaptersByName(adapters []synology.FirewallAdapterPolicy) map[string]synology.FirewallAdapterPolicy {
	out := make(map[string]synology.FirewallAdapterPolicy, len(adapters))
	for _, adapter := range adapters {
		out[adapter.Adapter] = adapter
	}
	return out
}

func firewallProfilesEqual(a, b synology.FirewallProfileRules) bool {
	if len(a.Adapters) != len(b.Adapters) {
		return false
	}
	aByName := adaptersByName(a.Adapters)
	for _, adapter := range b.Adapters {
		prior, ok := aByName[adapter.Adapter]
		if !ok || !strings.EqualFold(prior.Policy, adapter.Policy) || !rulesEqual(prior.Rules, adapter.Rules) {
			return false
		}
	}
	return true
}

func rulesEqual(a, b []synology.FirewallRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
