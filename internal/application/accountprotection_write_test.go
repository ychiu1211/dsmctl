package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/accountprotection"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type fakeAccountProtectionClient struct {
	autoBlock    synology.AutoBlockSettings
	protection   synology.AccountProtection
	enforce      synology.EnforceTwoFactor
	lists        synology.AutoBlockLists
	connections  []synology.ActiveConnection
	capabilities synology.AccountProtectionCapabilities
	persist      bool
	mutations    int
}

func (c *fakeAccountProtectionClient) AutoBlockSettings(context.Context) (synology.AutoBlockSettings, error) {
	return c.autoBlock, nil
}
func (c *fakeAccountProtectionClient) AutoBlockLists(context.Context) (synology.AutoBlockLists, error) {
	return c.lists, nil
}
func (c *fakeAccountProtectionClient) AccountProtection(context.Context) (synology.AccountProtection, error) {
	return c.protection, nil
}
func (c *fakeAccountProtectionClient) EnforceTwoFactor(context.Context) (synology.EnforceTwoFactor, error) {
	return c.enforce, nil
}
func (c *fakeAccountProtectionClient) AccountProtectionCapabilities(context.Context) (synology.AccountProtectionCapabilities, synology.CompatibilityReport, error) {
	return c.capabilities, synology.CompatibilityReport{}, nil
}
func (c *fakeAccountProtectionClient) ActiveConnections(context.Context) ([]synology.ActiveConnection, error) {
	return c.connections, nil
}

func (c *fakeAccountProtectionClient) ApplyAutoBlockChange(_ context.Context, change synology.AutoBlockChange) (synology.AccountProtectionMutationResult, error) {
	c.mutations++
	if c.persist {
		if change.Enabled != nil {
			c.autoBlock.Enabled = *change.Enabled
		}
		if change.Attempts != nil {
			c.autoBlock.Attempts = *change.Attempts
		}
		if change.WithinMinutes != nil {
			c.autoBlock.WithinMinutes = *change.WithinMinutes
		}
		if change.ExpireDays != nil {
			c.autoBlock.ExpireDays = *change.ExpireDays
		}
		if change.ExpireEnabled != nil {
			c.autoBlock.ExpireEnabled = *change.ExpireEnabled
		}
	}
	return synology.AccountProtectionMutationResult{Backend: "account-protection-autoblock-set-v1", API: "SYNO.Core.Security.AutoBlock", Version: 1, Method: "set"}, nil
}

func (c *fakeAccountProtectionClient) ApplyAccountProtectionChange(_ context.Context, change synology.AccountProtectionChange) (synology.AccountProtectionMutationResult, error) {
	c.mutations++
	if c.persist {
		if change.Enabled != nil {
			c.protection.Enabled = *change.Enabled
		}
		if change.UntrustedAttempts != nil {
			c.protection.UntrustedAttempts = *change.UntrustedAttempts
		}
		if change.UntrustedWithinMinutes != nil {
			c.protection.UntrustedWithinMinutes = *change.UntrustedWithinMinutes
		}
		if change.TrustedAttempts != nil {
			c.protection.TrustedAttempts = *change.TrustedAttempts
		}
	}
	return synology.AccountProtectionMutationResult{Backend: "account-protection-smartblock-set-v1", API: "SYNO.Core.SmartBlock", Version: 1, Method: "set"}, nil
}

func (c *fakeAccountProtectionClient) ApplyEnforceTwoFactorChange(_ context.Context, change synology.EnforceTwoFactorChange) (synology.AccountProtectionMutationResult, error) {
	c.mutations++
	if c.persist && change.Option != nil {
		c.enforce.Option = *change.Option
		c.enforce.Enabled = !strings.EqualFold(*change.Option, "none")
	}
	return synology.AccountProtectionMutationResult{Backend: "account-protection-enforce-2fa-set-v1", API: "SYNO.Core.OTP.EnforcePolicy", Version: 1, Method: "set"}, nil
}

func (c *fakeAccountProtectionClient) ApplyAutoBlockListEdit(_ context.Context, edit synology.IPListEdit) (synology.AccountProtectionMutationResult, error) {
	c.mutations++
	if c.persist {
		list := &c.lists.Block
		if edit.Kind == accountprotection.KindAllow {
			list = &c.lists.Allow
		}
		if edit.Remove {
			kept := list.Entries[:0]
			for _, entry := range list.Entries {
				if entry.IP != edit.IP {
					kept = append(kept, entry)
				}
			}
			list.Entries = kept
		} else {
			list.Entries = append(list.Entries, accountprotection.IPRule{IP: edit.IP})
		}
		list.Total = len(list.Entries)
	}
	return synology.AccountProtectionMutationResult{Backend: "account-protection-autoblock-rules-edit-v1", API: "SYNO.Core.Security.AutoBlock.Rules", Version: 1}, nil
}

func accountProtectionTestClient() *fakeAccountProtectionClient {
	return &fakeAccountProtectionClient{
		autoBlock:  synology.AutoBlockSettings{Enabled: false, Attempts: 10, WithinMinutes: 5},
		protection: synology.AccountProtection{Enabled: false, UntrustedAttempts: 5, UntrustedWithinMinutes: 1, UntrustedBlockMinutes: 30, TrustedAttempts: 10, TrustedWithinMinutes: 1, TrustedBlockMinutes: 30},
		enforce:    synology.EnforceTwoFactor{Option: "none", Enabled: false},
		lists: synology.AutoBlockLists{
			Allow: accountprotection.IPList{Kind: "allow"},
			Block: accountprotection.IPList{Kind: "block"},
		},
		connections: []synology.ActiveConnection{{From: "10.17.36.69", Who: "deryck"}},
		capabilities: synology.AccountProtectionCapabilities{
			Module: accountprotection.ModuleName, AutoBlockRead: true, AutoBlockListRead: true, AccountProtectionRead: true, EnforceTwoFactorRead: true,
			AutoBlockWrite: true, AutoBlockListWrite: true, AccountProtectionWrite: true, EnforceTwoFactorWrite: true, Mutations: true,
		},
		persist: true,
	}
}

// ---- Auto Block settings ----

func TestAutoBlockPlanApplyEnable(t *testing.T) {
	client := accountProtectionTestClient()
	plan, err := planAutoBlockWithClient(context.Background(), "lab", client, accountprotection.AutoBlockChange{Enabled: boolPtr(true)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" || len(plan.Warnings) != 0 || plan.Hash == "" || plan.ObservedFingerprint == "" {
		t.Fatalf("plan = %#v", plan)
	}
	if err := validateAutoBlockPlan(plan, plan.Hash); err != nil {
		t.Fatalf("validate plan error = %v", err)
	}
	result, err := applyAutoBlockWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if !result.Applied || client.mutations != 1 || !client.autoBlock.Enabled {
		t.Fatalf("result = %#v state = %#v", result, client.autoBlock)
	}
}

func TestAutoBlockLooseningIsHigh(t *testing.T) {
	enabled := accountProtectionTestClient()
	enabled.autoBlock = synology.AutoBlockSettings{Enabled: true, Attempts: 10, WithinMinutes: 5}
	cases := []struct {
		name  string
		req   accountprotection.AutoBlockChange
		wants string
	}{
		{"disable", accountprotection.AutoBlockChange{Enabled: boolPtr(false)}, "disabling Auto Block"},
		{"raise attempts", accountprotection.AutoBlockChange{Attempts: intPtr(20)}, "weakens blocking"},
		{"lengthen window", accountprotection.AutoBlockChange{WithinMinutes: intPtr(60)}, "weakens blocking"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planAutoBlockWithClient(context.Background(), "lab", enabled, tc.req)
			if err != nil {
				t.Fatalf("plan error = %v", err)
			}
			if plan.Risk != "high" || !strings.Contains(strings.Join(plan.Warnings, "\n"), tc.wants) {
				t.Fatalf("risk=%q warnings=%#v", plan.Risk, plan.Warnings)
			}
		})
	}
}

func TestAutoBlockTighteningIsMedium(t *testing.T) {
	enabled := accountProtectionTestClient()
	enabled.autoBlock = synology.AutoBlockSettings{Enabled: true, Attempts: 10, WithinMinutes: 5}
	plan, err := planAutoBlockWithClient(context.Background(), "lab", enabled, accountprotection.AutoBlockChange{Attempts: intPtr(5)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" || len(plan.Warnings) != 0 {
		t.Fatalf("risk=%q warnings=%#v", plan.Risk, plan.Warnings)
	}
}

func TestAutoBlockRejectsNoOpAndBadShape(t *testing.T) {
	client := accountProtectionTestClient()
	if _, err := planAutoBlockWithClient(context.Background(), "lab", client, accountprotection.AutoBlockChange{Attempts: intPtr(10)}); err == nil || !strings.Contains(err.Error(), "would not change") {
		t.Fatalf("no-op error = %v", err)
	}
	if err := validateAutoBlockShape(accountprotection.AutoBlockChange{}); err == nil || !strings.Contains(err.Error(), "no fields") {
		t.Fatalf("empty shape error = %v", err)
	}
	if err := validateAutoBlockShape(accountprotection.AutoBlockChange{Attempts: intPtr(0)}); err == nil || !strings.Contains(err.Error(), "at least 1") {
		t.Fatalf("attempts shape error = %v", err)
	}
}

func TestAutoBlockApplyRejectsStaleAndTampering(t *testing.T) {
	client := accountProtectionTestClient()
	plan, err := planAutoBlockWithClient(context.Background(), "lab", client, accountprotection.AutoBlockChange{Enabled: boolPtr(true)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	tampered := plan
	tampered.Risk = "low"
	if err := validateAutoBlockPlan(tampered, tampered.Hash); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("tamper error = %v", err)
	}
	client.autoBlock.Attempts = 99
	if _, err := applyAutoBlockWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale error = %v", err)
	}
	if client.mutations != 0 {
		t.Fatalf("mutations = %d, want 0", client.mutations)
	}
}

func TestAutoBlockPostconditionNamesIgnoredThreshold(t *testing.T) {
	// A threshold change requested while Auto Block stays disabled is silently
	// ignored by DSM; the postcondition must catch it and name the field.
	client := accountProtectionTestClient()
	client.persist = false
	plan, err := planAutoBlockWithClient(context.Background(), "lab", client, accountprotection.AutoBlockChange{Attempts: intPtr(6)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if _, err := applyAutoBlockWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "verify auto block change") || !strings.Contains(err.Error(), "attempts") {
		t.Fatalf("postcondition error = %v", err)
	}
}

func TestAutoBlockMissingWriteBackend(t *testing.T) {
	client := accountProtectionTestClient()
	client.capabilities.AutoBlockWrite = false
	if _, err := planAutoBlockWithClient(context.Background(), "lab", client, accountprotection.AutoBlockChange{Enabled: boolPtr(true)}); err == nil || !strings.Contains(err.Error(), "Auto Block read/write backend") {
		t.Fatalf("missing backend error = %v", err)
	}
}

// ---- Account Protection (SmartBlock) ----

func TestAccountProtectionLooseningIsHigh(t *testing.T) {
	client := accountProtectionTestClient()
	client.protection.Enabled = true
	cases := []struct {
		name  string
		req   accountprotection.AccountProtectionChange
		wants string
	}{
		{"disable", accountprotection.AccountProtectionChange{Enabled: boolPtr(false)}, "disabling Account Protection"},
		{"raise untrusted attempts", accountprotection.AccountProtectionChange{UntrustedAttempts: intPtr(20)}, "weakens blocking"},
		{"lengthen untrusted window", accountprotection.AccountProtectionChange{UntrustedWithinMinutes: intPtr(30)}, "weakens blocking"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planAccountProtectionWithClient(context.Background(), "lab", client, tc.req)
			if err != nil {
				t.Fatalf("plan error = %v", err)
			}
			if plan.Risk != "high" || !strings.Contains(strings.Join(plan.Warnings, "\n"), tc.wants) {
				t.Fatalf("risk=%q warnings=%#v", plan.Risk, plan.Warnings)
			}
		})
	}
}

func TestAccountProtectionPlanApplyAndBlockDurationIsMedium(t *testing.T) {
	client := accountProtectionTestClient()
	client.protection.Enabled = true
	// Raising the block DURATION is stricter, not weaker: medium.
	plan, err := planAccountProtectionWithClient(context.Background(), "lab", client, accountprotection.AccountProtectionChange{UntrustedAttempts: intPtr(3)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" {
		t.Fatalf("tightening risk = %q", plan.Risk)
	}
	result, err := applyAccountProtectionWithClient(context.Background(), client, plan)
	if err != nil || !result.Applied || client.protection.UntrustedAttempts != 3 {
		t.Fatalf("apply result = %#v state = %#v err = %v", result, client.protection, err)
	}
}

// ---- Enforce 2FA ----

func TestEnforceTwoFactorEnableRefusedWithoutOverride(t *testing.T) {
	client := accountProtectionTestClient()
	if _, err := planEnforceTwoFactorWithClient(context.Background(), "lab", client, accountprotection.EnforceTwoFactorChange{Option: stringPointer("all")}); err == nil || !strings.Contains(err.Error(), "allow_lockout_override") {
		t.Fatalf("enable without override error = %v", err)
	}
}

func TestEnforceTwoFactorEnableWithOverrideIsHigh(t *testing.T) {
	client := accountProtectionTestClient()
	plan, err := planEnforceTwoFactorWithClient(context.Background(), "lab", client, accountprotection.EnforceTwoFactorChange{Option: stringPointer("all"), AllowLockoutOverride: true})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "high" || !strings.Contains(strings.Join(plan.Warnings, "\n"), "lock out an administrator") {
		t.Fatalf("risk=%q warnings=%#v", plan.Risk, plan.Warnings)
	}
	result, err := applyEnforceTwoFactorWithClient(context.Background(), client, plan)
	if err != nil || !result.Applied || client.enforce.Option != "all" {
		t.Fatalf("apply result = %#v state = %#v err = %v", result, client.enforce, err)
	}
}

func TestEnforceTwoFactorDisableIsHighNoOverride(t *testing.T) {
	client := accountProtectionTestClient()
	client.enforce = synology.EnforceTwoFactor{Option: "all", Enabled: true}
	plan, err := planEnforceTwoFactorWithClient(context.Background(), "lab", client, accountprotection.EnforceTwoFactorChange{Option: stringPointer("none")})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "high" || !strings.Contains(strings.Join(plan.Warnings, "\n"), "weakening the security posture") {
		t.Fatalf("risk=%q warnings=%#v", plan.Risk, plan.Warnings)
	}
}

func TestEnforceTwoFactorRejectsNoOp(t *testing.T) {
	client := accountProtectionTestClient()
	if _, err := planEnforceTwoFactorWithClient(context.Background(), "lab", client, accountprotection.EnforceTwoFactorChange{Option: stringPointer("none")}); err == nil || !strings.Contains(err.Error(), "would not change") {
		t.Fatalf("no-op error = %v", err)
	}
}

// ---- List edits + self-lockout guardrail ----

func TestListBlockAddTestNetIsMediumAndPatchOnly(t *testing.T) {
	client := accountProtectionTestClient()
	plan, err := planAutoBlockListWithClient(context.Background(), "lab", client, accountprotection.IPListEdit{Kind: accountprotection.KindBlock, IP: "192.0.2.1"})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" {
		t.Fatalf("risk = %q, want medium", plan.Risk)
	}
	result, err := applyAutoBlockListWithClient(context.Background(), client, plan)
	if err != nil || !result.Applied || client.mutations != 1 {
		t.Fatalf("apply result = %#v mutations = %d err = %v", result, client.mutations, err)
	}
	if !listContainsIP(client.lists.Block, "192.0.2.1") {
		t.Fatalf("block list should contain the added entry: %#v", client.lists.Block)
	}
}

func TestListBlockAddActiveSourceRefusedWithoutOverride(t *testing.T) {
	client := accountProtectionTestClient() // active connection from 10.17.36.69
	if _, err := planAutoBlockListWithClient(context.Background(), "lab", client, accountprotection.IPListEdit{Kind: accountprotection.KindBlock, IP: "10.17.36.69"}); err == nil || !strings.Contains(err.Error(), "lock out the active connection") {
		t.Fatalf("self-block error = %v", err)
	}
	// A subnet containing the active source is likewise refused.
	if _, err := planAutoBlockListWithClient(context.Background(), "lab", client, accountprotection.IPListEdit{Kind: accountprotection.KindBlock, IP: "10.17.36.0/24"}); err == nil || !strings.Contains(err.Error(), "lock out") {
		t.Fatalf("self-block subnet error = %v", err)
	}
	// With the explicit override it proceeds and is high risk.
	plan, err := planAutoBlockListWithClient(context.Background(), "lab", client, accountprotection.IPListEdit{Kind: accountprotection.KindBlock, IP: "10.17.36.69", AllowLockoutOverride: true})
	if err != nil {
		t.Fatalf("override plan error = %v", err)
	}
	if plan.Risk != "high" {
		t.Fatalf("override risk = %q", plan.Risk)
	}
}

func TestListBlockAddBroadSubnetRefusedWithoutOverride(t *testing.T) {
	client := accountProtectionTestClient()
	client.connections = nil // no active connections, so only the broad-subnet rule applies
	if _, err := planAutoBlockListWithClient(context.Background(), "lab", client, accountprotection.IPListEdit{Kind: accountprotection.KindBlock, IP: "203.0.113.0/16"}); err == nil || !strings.Contains(err.Error(), "broad subnet") {
		t.Fatalf("broad-subnet block error = %v", err)
	}
}

func TestListAllowAddBroadSubnetIsHigh(t *testing.T) {
	client := accountProtectionTestClient()
	plan, err := planAutoBlockListWithClient(context.Background(), "lab", client, accountprotection.IPListEdit{Kind: accountprotection.KindAllow, IP: "203.0.113.0/16"})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "high" || !strings.Contains(strings.Join(plan.Warnings, "\n"), "exempts many hosts") {
		t.Fatalf("risk=%q warnings=%#v", plan.Risk, plan.Warnings)
	}
}

func TestListAllowRemoveActiveSourceRefusedWithoutOverride(t *testing.T) {
	client := accountProtectionTestClient()
	client.lists.Allow.Entries = []accountprotection.IPRule{{IP: "10.17.36.0/24"}}
	client.lists.Allow.Total = 1
	if _, err := planAutoBlockListWithClient(context.Background(), "lab", client, accountprotection.IPListEdit{Kind: accountprotection.KindAllow, IP: "10.17.36.0/24", Remove: true}); err == nil || !strings.Contains(err.Error(), "expose the active connection") {
		t.Fatalf("allow-remove self-lockout error = %v", err)
	}
}

func TestListRejectsNoOpAddAndRemove(t *testing.T) {
	client := accountProtectionTestClient()
	client.lists.Block.Entries = []accountprotection.IPRule{{IP: "192.0.2.1"}}
	client.lists.Block.Total = 1
	if _, err := planAutoBlockListWithClient(context.Background(), "lab", client, accountprotection.IPListEdit{Kind: accountprotection.KindBlock, IP: "192.0.2.1"}); err == nil || !strings.Contains(err.Error(), "already on the block list") {
		t.Fatalf("no-op add error = %v", err)
	}
	if _, err := planAutoBlockListWithClient(context.Background(), "lab", client, accountprotection.IPListEdit{Kind: accountprotection.KindBlock, IP: "203.0.113.9", Remove: true}); err == nil || !strings.Contains(err.Error(), "nothing to remove") {
		t.Fatalf("no-op remove error = %v", err)
	}
}

func TestListShapeValidation(t *testing.T) {
	cases := []struct {
		name string
		edit accountprotection.IPListEdit
		want string
	}{
		{"bad kind", accountprotection.IPListEdit{Kind: "firewall", IP: "192.0.2.1"}, "unsupported list kind"},
		{"empty ip", accountprotection.IPListEdit{Kind: accountprotection.KindBlock}, "requires an ip"},
		{"bad ip", accountprotection.IPListEdit{Kind: accountprotection.KindBlock, IP: "not-an-ip"}, "not a valid IP"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateAutoBlockListShape(tc.edit); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("shape error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestListPlanStaleWhenActiveSourceAppears(t *testing.T) {
	client := accountProtectionTestClient()
	client.connections = nil
	plan, err := planAutoBlockListWithClient(context.Background(), "lab", client, accountprotection.IPListEdit{Kind: accountprotection.KindBlock, IP: "192.0.2.1"})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	// A new active connection appears after planning: the plan's observed
	// fingerprint (which hashes the protected sources) no longer holds.
	client.connections = []synology.ActiveConnection{{From: "10.0.0.5"}}
	if _, err := applyAutoBlockListWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale-on-new-connection error = %v", err)
	}
}

// ---- secret hygiene ----

func TestAccountProtectionPlansCarryNoSecrets(t *testing.T) {
	client := accountProtectionTestClient()
	plan, err := planEnforceTwoFactorWithClient(context.Background(), "lab", client, accountprotection.EnforceTwoFactorChange{Option: stringPointer("all"), AllowLockoutOverride: true})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal error = %v", err)
	}
	for _, forbidden := range []string{"synotoken", "\"sid\"", "otp_secret", "recovery", "seed", "password"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("plan JSON leaked %q: %s", forbidden, encoded)
		}
	}
}
