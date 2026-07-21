package synology

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/firewall"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	fwops "github.com/ychiu1211/dsmctl/internal/synology/operations/firewall"
)

type FirewallStatus = firewall.Status
type FirewallProfile = firewall.Profile
type FirewallProfileRules = firewall.ProfileRules
type FirewallAdapterPolicy = firewall.AdapterPolicy
type FirewallRule = firewall.Rule
type FirewallRuleSet = firewall.RuleSet
type FirewallCapabilities = firewall.Capabilities
type FirewallProfileChange = firewall.ProfileChange
type FirewallEnableChange = firewall.EnableChange
type FirewallSessionSource = firewall.SessionSource
type FirewallConnection = firewall.Connection
type FirewallMutationResult = fwops.MutationResult

// FirewallTransport reports the DSM management port dsmctl is currently connected
// on. It is ground truth for the never-lockout guard's port dimension: the guard
// evaluates the resulting ruleset against {source, this port, tcp}. The base URL
// is immutable after construction, so no lock is needed.
func (c *Client) FirewallTransport() FirewallConnection {
	scheme := strings.ToLower(c.baseURL.Scheme)
	port := 0
	if p := c.baseURL.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	if port == 0 {
		if scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}
	return FirewallConnection{Port: port, Protocol: "tcp"}
}

// FirewallStatus reads the global firewall state (Control Panel > Security >
// Firewall): whether the firewall is enabled and which profile is active, plus
// the enumerated network adapters. Firewall is DSM core, so the plain
// compatibility target is used. The adapter list is best-effort: if that area is
// unsupported the status still returns with the enable flag and active profile.
func (c *Client) FirewallStatus(ctx context.Context) (FirewallStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, fwops.APINames()...); err != nil {
		return FirewallStatus{}, fmt.Errorf("prepare firewall target: %w", err)
	}
	status, _, err := fwops.ExecuteStatus(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return FirewallStatus{}, fmt.Errorf("get firewall status: %w", err)
	}
	c.target.AddCapability(fwops.StatusReadCapabilityName)
	if adapters, _, err := fwops.ExecuteAdapters(ctx, c.target, lockedExecutor{client: c}); err == nil {
		status.Adapters = adapters
		c.target.AddCapability(fwops.AdaptersReadCapabilityName)
	} else if !compatibility.IsUnsupported(err) {
		return FirewallStatus{}, fmt.Errorf("get firewall adapters: %w", err)
	}
	return status, nil
}

// FirewallProfiles reads the firewall profile list and marks the active one.
func (c *Client) FirewallProfiles(ctx context.Context) ([]FirewallProfile, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, fwops.APINames()...); err != nil {
		return nil, fmt.Errorf("prepare firewall target: %w", err)
	}
	names, _, err := fwops.ExecuteProfiles(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return nil, fmt.Errorf("get firewall profiles: %w", err)
	}
	c.target.AddCapability(fwops.ProfilesReadCapabilityName)
	active := c.firewallActiveProfileLocked(ctx)
	profiles := make([]FirewallProfile, 0, len(names))
	for _, name := range names {
		profiles = append(profiles, FirewallProfile{Name: name, IsActive: name == active && active != ""})
	}
	return profiles, nil
}

// FirewallRules reads the per-adapter default policy and ordered rule list for the
// requested profile, or for every profile when requestedProfile is empty.
func (c *Client) FirewallRules(ctx context.Context, requestedProfile string) (FirewallRuleSet, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, fwops.APINames()...); err != nil {
		return FirewallRuleSet{}, fmt.Errorf("prepare firewall target: %w", err)
	}

	active := c.firewallActiveProfileLocked(ctx)

	var names []string
	if requestedProfile != "" {
		names = []string{requestedProfile}
	} else {
		listed, _, err := fwops.ExecuteProfiles(ctx, c.target, lockedExecutor{client: c})
		if err != nil {
			return FirewallRuleSet{}, fmt.Errorf("list firewall profiles: %w", err)
		}
		c.target.AddCapability(fwops.ProfilesReadCapabilityName)
		names = listed
	}

	result := FirewallRuleSet{ActiveProfile: active, Profiles: make([]FirewallProfileRules, 0, len(names))}
	for _, name := range names {
		profileRules, _, err := fwops.ExecuteProfileRules(ctx, c.target, lockedExecutor{client: c}, name)
		if err != nil {
			return FirewallRuleSet{}, fmt.Errorf("get firewall rules for profile %q: %w", name, err)
		}
		profileRules.IsActive = name == active && active != ""
		result.Profiles = append(result.Profiles, profileRules)
	}
	c.target.AddCapability(fwops.RulesReadCapabilityName)
	return result, nil
}

// firewallActiveProfileLocked reads the active profile name, best-effort. A
// failure (or an unsupported status API) yields an empty name rather than an
// error, so the active-profile annotation degrades gracefully.
func (c *Client) firewallActiveProfileLocked(ctx context.Context) string {
	status, _, err := fwops.ExecuteStatus(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ""
	}
	c.target.AddCapability(fwops.StatusReadCapabilityName)
	return status.ActiveProfile
}

// FirewallCapabilities reports which firewall reads dsmctl exposes for the
// selected NAS, plus the discovered backends. Each area is an independent
// boundary: one being absent leaves the others usable.
func (c *Client) FirewallCapabilities(ctx context.Context) (FirewallCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, fwops.APINames()...); err != nil {
		return FirewallCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare firewall capabilities target: %w", err)
	}

	status, err := selectSupported(fwops.SelectStatus, c.target)
	if err != nil {
		return FirewallCapabilities{}, CompatibilityReport{}, fmt.Errorf("select firewall status backend: %w", err)
	}
	profiles, err := selectSupported(fwops.SelectProfiles, c.target)
	if err != nil {
		return FirewallCapabilities{}, CompatibilityReport{}, fmt.Errorf("select firewall profiles backend: %w", err)
	}
	adapters, err := selectSupported(fwops.SelectAdapters, c.target)
	if err != nil {
		return FirewallCapabilities{}, CompatibilityReport{}, fmt.Errorf("select firewall adapters backend: %w", err)
	}
	rules, err := selectSupported(fwops.SelectRules, c.target)
	if err != nil {
		return FirewallCapabilities{}, CompatibilityReport{}, fmt.Errorf("select firewall rules backend: %w", err)
	}
	profileWrite, err := selectSupported(fwops.SelectProfileSet, c.target)
	if err != nil {
		return FirewallCapabilities{}, CompatibilityReport{}, fmt.Errorf("select firewall profile write backend: %w", err)
	}
	enableWrite, err := selectSupported(fwops.SelectEnable, c.target)
	if err != nil {
		return FirewallCapabilities{}, CompatibilityReport{}, fmt.Errorf("select firewall enable write backend: %w", err)
	}

	if status.Supported {
		c.target.AddCapability(fwops.StatusReadCapabilityName)
	}
	if profiles.Supported {
		c.target.AddCapability(fwops.ProfilesReadCapabilityName)
	}
	if adapters.Supported {
		c.target.AddCapability(fwops.AdaptersReadCapabilityName)
	}
	if rules.Supported {
		c.target.AddCapability(fwops.RulesReadCapabilityName)
	}
	if profileWrite.Supported {
		c.target.AddCapability(fwops.ProfileWriteCapabilityName)
	}
	if enableWrite.Supported {
		c.target.AddCapability(fwops.EnableWriteCapabilityName)
	}

	capabilities := FirewallCapabilities{
		Module:       firewall.ModuleName,
		StatusRead:   status.Supported,
		ProfilesRead: profiles.Supported,
		AdaptersRead: adapters.Supported,
		RulesRead:    rules.Supported,
		// The per-rule field names were live-confirmed by the Slice B rule
		// round-trip, so the read is no longer wire-unverified.
		RuleFieldsWireUnverified: false,
		ProfileWrite:             profileWrite.Supported,
		EnableWrite:              enableWrite.Supported,
		Mutations:                profileWrite.Supported || enableWrite.Supported,
	}
	return capabilities, c.target.Report(status, profiles, adapters, rules, profileWrite, enableWrite), nil
}

// FirewallActiveSessions lists active client connections so the self-lockout guard
// can determine the operator's management source IP. Best-effort: no API or a
// transient failure yields no sources rather than an error, and no session secret
// is read.
func (c *Client) FirewallActiveSessions(ctx context.Context) ([]FirewallSessionSource, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, fwops.APINames()...); err != nil {
		return nil, fmt.Errorf("prepare firewall target: %w", err)
	}
	return fwops.ExecuteCurrentConnection(ctx, c.target, lockedExecutor{client: c})
}

// ApplyFirewallProfileChange merges the desired adapter sections into a freshly
// read profile (so untouched adapters are never rewritten) and submits the whole
// profile via Profile.set. The application-layer self-lockout guard runs before
// this and is the authority on connectivity safety; this method performs no guard
// of its own.
func (c *Client) ApplyFirewallProfileChange(ctx context.Context, change FirewallProfileChange) (FirewallMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, fwops.APINames()...); err != nil {
		return FirewallMutationResult{}, fmt.Errorf("prepare firewall mutation target: %w", err)
	}
	current, _, err := fwops.ExecuteProfileRules(ctx, c.target, lockedExecutor{client: c}, change.Profile)
	if err != nil {
		return FirewallMutationResult{}, fmt.Errorf("refresh firewall profile before apply: %w", err)
	}
	merged := firewall.MergeProfile(current, change.Adapters)
	result, _, err := fwops.ExecuteProfileSet(ctx, c.target, lockedExecutor{client: c}, fwops.ProfileSetInput{Profile: merged, Activate: change.Activate})
	if err != nil {
		return FirewallMutationResult{}, fmt.Errorf("apply firewall profile: %w", err)
	}
	return result, nil
}

// ApplyFirewallEnableChange enables (Profile.Apply.start) or disables (Firewall.set
// set_type=disable) the firewall. The guard runs in the application layer first.
func (c *Client) ApplyFirewallEnableChange(ctx context.Context, change FirewallEnableChange) (FirewallMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, fwops.APINames()...); err != nil {
		return FirewallMutationResult{}, fmt.Errorf("prepare firewall mutation target: %w", err)
	}
	result, _, err := fwops.ExecuteEnable(ctx, c.target, lockedExecutor{client: c}, fwops.EnableInput{Enabled: change.Enabled, Profile: change.Profile})
	if err != nil {
		return FirewallMutationResult{}, fmt.Errorf("apply firewall enable change: %w", err)
	}
	return result, nil
}

