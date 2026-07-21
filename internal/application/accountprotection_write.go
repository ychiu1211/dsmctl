package application

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/accountprotection"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const accountProtectionAPIVersion = "dsmctl.io/v1alpha1"

// The account-protection guarded writes follow the module plan/apply contract:
// the plan records and hashes the complete observed state, apply re-reads and
// rejects a changed state, merges the patch into a freshly read config, performs
// the typed set, and re-reads to verify the requested fields took effect.
//
// Risk model (from the work item): loosening the posture is HIGH — disabling Auto
// Block or Account Protection, raising the block threshold or lengthening the
// window (weaker blocking), enabling or changing the org-wide enforced-2FA scope,
// or adding a broad allow rule. Tightening is medium. The self-lockout guardrail
// refuses, by default, any edit that could lock the operator or an active session
// out: blocking a currently active source (or a subnet containing it), removing
// an active source from the allow list, or enabling enforced 2FA (which can lock
// out an admin who has not enrolled). Each of those proceeds only with an explicit
// override recorded in the plan intent.

// ---- Auto Block settings ----------------------------------------------------

type AutoBlockSettingsPlan struct {
	APIVersion          string                        `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                        `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                        `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             accountprotection.AutoBlockChange `json:"request" jsonschema:"Validated patch-only Auto Block settings intent"`
	Observed            synology.AutoBlockSettings    `json:"observed" jsonschema:"Complete Auto Block settings observed during planning"`
	ObservedFingerprint string                        `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed settings"`
	Risk                string                        `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                      `json:"warnings" jsonschema:"Posture-weakening warnings"`
	Summary             []string                      `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                        `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type AccountProtectionApplyResult struct {
	NAS       string                                  `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                                  `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                                    `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operation synology.AccountProtectionMutationResult `json:"operation" jsonschema:"Selected DSM mutation backend"`
}

func (s *Service) PlanAutoBlockChange(ctx context.Context, requestedNAS string, request accountprotection.AutoBlockChange) (AutoBlockSettingsPlan, error) {
	if err := validateAutoBlockShape(request); err != nil {
		return AutoBlockSettingsPlan{}, err
	}
	name, client, err := s.accountProtectionClient(ctx, requestedNAS)
	if err != nil {
		return AutoBlockSettingsPlan{}, err
	}
	plan, err := planAutoBlockWithClient(ctx, name, client, request)
	if err != nil {
		return AutoBlockSettingsPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = autoBlockPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyAutoBlockPlan(ctx context.Context, plan AutoBlockSettingsPlan, approvalHash string) (AccountProtectionApplyResult, error) {
	if err := validateAutoBlockPlan(plan, approvalHash); err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return AccountProtectionApplyResult{}, err
	}
	name, client, err := s.accountProtectionClient(ctx, plan.NAS)
	if err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if name != plan.NAS {
		return AccountProtectionApplyResult{}, fmt.Errorf("auto block plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyAutoBlockWithClient(ctx, client, plan)
}

func planAutoBlockWithClient(ctx context.Context, nas string, client accountProtectionClient, request accountprotection.AutoBlockChange) (AutoBlockSettingsPlan, error) {
	capabilities, _, err := client.AccountProtectionCapabilities(ctx)
	if err != nil {
		return AutoBlockSettingsPlan{}, authenticationError(nas, err)
	}
	if !capabilities.AutoBlockRead || !capabilities.AutoBlockWrite {
		return AutoBlockSettingsPlan{}, fmt.Errorf("NAS %q does not expose a verified Auto Block read/write backend", nas)
	}
	state, err := client.AutoBlockSettings(ctx)
	if err != nil {
		return AutoBlockSettingsPlan{}, authenticationError(nas, err)
	}
	if autoBlockSatisfied(state, request) {
		return AutoBlockSettingsPlan{}, fmt.Errorf("auto block patch would not change the current configuration")
	}
	plan := AutoBlockSettingsPlan{APIVersion: accountProtectionAPIVersion, NAS: nas, Request: request, Observed: state}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return AutoBlockSettingsPlan{}, err
	}
	plan.Risk, plan.Warnings, plan.Summary = autoBlockEffects(state, request)
	plan.Hash, err = autoBlockPlanHash(plan)
	if err != nil {
		return AutoBlockSettingsPlan{}, err
	}
	return plan, nil
}

func applyAutoBlockWithClient(ctx context.Context, client accountProtectionClient, plan AutoBlockSettingsPlan) (AccountProtectionApplyResult, error) {
	current, err := planAutoBlockWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return AccountProtectionApplyResult{}, fmt.Errorf("auto block plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = autoBlockPlanHash(current)
	if err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return AccountProtectionApplyResult{}, fmt.Errorf("auto block plan is stale; create a new plan")
	}
	operation, err := client.ApplyAutoBlockChange(ctx, plan.Request)
	if err != nil {
		return AccountProtectionApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyAutoBlockPostcondition(ctx, client, plan.Request); err != nil {
		return AccountProtectionApplyResult{}, fmt.Errorf("verify auto block change: %w", err)
	}
	return AccountProtectionApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func validateAutoBlockShape(change accountprotection.AutoBlockChange) error {
	if change.IsEmpty() {
		return fmt.Errorf("auto block patch has no fields")
	}
	if change.Attempts != nil && *change.Attempts < 1 {
		return fmt.Errorf("attempts %d must be at least 1", *change.Attempts)
	}
	if change.WithinMinutes != nil && *change.WithinMinutes < 1 {
		return fmt.Errorf("within_minutes %d must be at least 1", *change.WithinMinutes)
	}
	if change.ExpireDays != nil && *change.ExpireDays < 0 {
		return fmt.Errorf("expire_days %d must not be negative", *change.ExpireDays)
	}
	if change.ExpireEnabled != nil && *change.ExpireEnabled {
		if change.ExpireDays != nil && *change.ExpireDays <= 0 {
			return fmt.Errorf("expire_days must be at least 1 when expiration is enabled")
		}
	}
	return nil
}

func autoBlockSatisfied(state synology.AutoBlockSettings, change accountprotection.AutoBlockChange) bool {
	return (change.Enabled == nil || state.Enabled == *change.Enabled) &&
		(change.Attempts == nil || state.Attempts == *change.Attempts) &&
		(change.WithinMinutes == nil || state.WithinMinutes == *change.WithinMinutes) &&
		(change.ExpireEnabled == nil || state.ExpireEnabled == *change.ExpireEnabled) &&
		(change.ExpireDays == nil || state.ExpireDays == *change.ExpireDays)
}

func autoBlockEffects(state synology.AutoBlockSettings, change accountprotection.AutoBlockChange) (string, []string, []string) {
	warnings := []string{}
	summary := []string{}
	high := false
	if change.Enabled != nil && *change.Enabled != state.Enabled {
		if *change.Enabled {
			summary = append(summary, "enable Auto Block")
		} else {
			summary = append(summary, "disable Auto Block")
			warnings = append(warnings, "disabling Auto Block stops DSM from blocking sources after repeated failed sign-ins, weakening the security posture")
			high = true
		}
	}
	if change.Attempts != nil && *change.Attempts != state.Attempts {
		summary = append(summary, fmt.Sprintf("change the block threshold from %d to %d attempts", state.Attempts, *change.Attempts))
		if *change.Attempts > state.Attempts {
			warnings = append(warnings, fmt.Sprintf("raising the block threshold to %d attempts weakens blocking (more failed sign-ins are tolerated before a source is blocked)", *change.Attempts))
			high = true
		}
	}
	if change.WithinMinutes != nil && *change.WithinMinutes != state.WithinMinutes {
		summary = append(summary, fmt.Sprintf("change the detection window from %d to %d minutes", state.WithinMinutes, *change.WithinMinutes))
		if *change.WithinMinutes > state.WithinMinutes {
			warnings = append(warnings, fmt.Sprintf("lengthening the detection window to %d minutes weakens blocking", *change.WithinMinutes))
			high = true
		}
	}
	if change.ExpireEnabled != nil && *change.ExpireEnabled != state.ExpireEnabled {
		if *change.ExpireEnabled {
			summary = append(summary, "enable block expiration")
		} else {
			summary = append(summary, "disable block expiration (blocks never expire)")
		}
	}
	if change.ExpireDays != nil && *change.ExpireDays != state.ExpireDays {
		summary = append(summary, fmt.Sprintf("set the block expiry to %d days", *change.ExpireDays))
	}
	risk := "medium"
	if high {
		risk = "high"
	}
	return risk, warnings, summary
}

func verifyAutoBlockPostcondition(ctx context.Context, client accountProtectionClient, change accountprotection.AutoBlockChange) error {
	state, err := client.AutoBlockSettings(ctx)
	if err != nil {
		return err
	}
	if change.Enabled != nil && state.Enabled != *change.Enabled {
		return fmt.Errorf("enabled is %t, want %t", state.Enabled, *change.Enabled)
	}
	if change.Attempts != nil && state.Attempts != *change.Attempts {
		return fmt.Errorf("attempts is %d, want %d (DSM binds Auto Block thresholds only when it is enabled)", state.Attempts, *change.Attempts)
	}
	if change.WithinMinutes != nil && state.WithinMinutes != *change.WithinMinutes {
		return fmt.Errorf("within_minutes is %d, want %d (DSM binds Auto Block thresholds only when it is enabled)", state.WithinMinutes, *change.WithinMinutes)
	}
	if change.ExpireEnabled != nil && state.ExpireEnabled != *change.ExpireEnabled {
		return fmt.Errorf("expire_enabled is %t, want %t", state.ExpireEnabled, *change.ExpireEnabled)
	}
	if change.ExpireDays != nil && state.ExpireDays != *change.ExpireDays {
		return fmt.Errorf("expire_days is %d, want %d", state.ExpireDays, *change.ExpireDays)
	}
	return nil
}

func validateAutoBlockPlan(plan AutoBlockSettingsPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the auto block plan")
	}
	if plan.APIVersion != accountProtectionAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid auto block plan metadata")
	}
	if err := validateAutoBlockShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("auto block plan observed state was modified")
	}
	expectedHash, err := autoBlockPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("auto block plan contents were modified after planning")
	}
	return nil
}

func autoBlockPlanHash(plan AutoBlockSettingsPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// ---- Account Protection (SmartBlock) thresholds -----------------------------

type AccountProtectionThresholdsPlan struct {
	APIVersion          string                                     `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                                     `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                                     `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             accountprotection.AccountProtectionChange `json:"request" jsonschema:"Validated patch-only Account Protection thresholds intent"`
	Observed            synology.AccountProtection                 `json:"observed" jsonschema:"Complete Account Protection thresholds observed during planning"`
	ObservedFingerprint string                                     `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed thresholds"`
	Risk                string                                     `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                                   `json:"warnings" jsonschema:"Posture-weakening warnings"`
	Summary             []string                                   `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                                     `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

func (s *Service) PlanAccountProtectionThresholdsChange(ctx context.Context, requestedNAS string, request accountprotection.AccountProtectionChange) (AccountProtectionThresholdsPlan, error) {
	if err := validateAccountProtectionShape(request); err != nil {
		return AccountProtectionThresholdsPlan{}, err
	}
	name, client, err := s.accountProtectionClient(ctx, requestedNAS)
	if err != nil {
		return AccountProtectionThresholdsPlan{}, err
	}
	plan, err := planAccountProtectionWithClient(ctx, name, client, request)
	if err != nil {
		return AccountProtectionThresholdsPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = accountProtectionPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyAccountProtectionThresholdsPlan(ctx context.Context, plan AccountProtectionThresholdsPlan, approvalHash string) (AccountProtectionApplyResult, error) {
	if err := validateAccountProtectionPlan(plan, approvalHash); err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return AccountProtectionApplyResult{}, err
	}
	name, client, err := s.accountProtectionClient(ctx, plan.NAS)
	if err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if name != plan.NAS {
		return AccountProtectionApplyResult{}, fmt.Errorf("account protection plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyAccountProtectionWithClient(ctx, client, plan)
}

func planAccountProtectionWithClient(ctx context.Context, nas string, client accountProtectionClient, request accountprotection.AccountProtectionChange) (AccountProtectionThresholdsPlan, error) {
	capabilities, _, err := client.AccountProtectionCapabilities(ctx)
	if err != nil {
		return AccountProtectionThresholdsPlan{}, authenticationError(nas, err)
	}
	if !capabilities.AccountProtectionRead || !capabilities.AccountProtectionWrite {
		return AccountProtectionThresholdsPlan{}, fmt.Errorf("NAS %q does not expose a verified Account Protection read/write backend", nas)
	}
	state, err := client.AccountProtection(ctx)
	if err != nil {
		return AccountProtectionThresholdsPlan{}, authenticationError(nas, err)
	}
	if accountProtectionSatisfied(state, request) {
		return AccountProtectionThresholdsPlan{}, fmt.Errorf("account protection patch would not change the current configuration")
	}
	plan := AccountProtectionThresholdsPlan{APIVersion: accountProtectionAPIVersion, NAS: nas, Request: request, Observed: state}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return AccountProtectionThresholdsPlan{}, err
	}
	plan.Risk, plan.Warnings, plan.Summary = accountProtectionEffects(state, request)
	plan.Hash, err = accountProtectionPlanHash(plan)
	if err != nil {
		return AccountProtectionThresholdsPlan{}, err
	}
	return plan, nil
}

func applyAccountProtectionWithClient(ctx context.Context, client accountProtectionClient, plan AccountProtectionThresholdsPlan) (AccountProtectionApplyResult, error) {
	current, err := planAccountProtectionWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return AccountProtectionApplyResult{}, fmt.Errorf("account protection plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = accountProtectionPlanHash(current)
	if err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return AccountProtectionApplyResult{}, fmt.Errorf("account protection plan is stale; create a new plan")
	}
	operation, err := client.ApplyAccountProtectionChange(ctx, plan.Request)
	if err != nil {
		return AccountProtectionApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyAccountProtectionPostcondition(ctx, client, plan.Request); err != nil {
		return AccountProtectionApplyResult{}, fmt.Errorf("verify account protection change: %w", err)
	}
	return AccountProtectionApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func validateAccountProtectionShape(change accountprotection.AccountProtectionChange) error {
	if change.IsEmpty() {
		return fmt.Errorf("account protection patch has no fields")
	}
	positives := []struct {
		value *int
		name  string
	}{
		{change.UntrustedAttempts, "untrusted_attempts"},
		{change.UntrustedWithinMinutes, "untrusted_within_minutes"},
		{change.UntrustedBlockMinutes, "untrusted_block_minutes"},
		{change.TrustedAttempts, "trusted_attempts"},
		{change.TrustedWithinMinutes, "trusted_within_minutes"},
		{change.TrustedBlockMinutes, "trusted_block_minutes"},
	}
	for _, field := range positives {
		if field.value != nil && *field.value < 1 {
			return fmt.Errorf("%s %d must be at least 1", field.name, *field.value)
		}
	}
	return nil
}

func accountProtectionSatisfied(state synology.AccountProtection, change accountprotection.AccountProtectionChange) bool {
	return (change.Enabled == nil || state.Enabled == *change.Enabled) &&
		(change.UntrustedAttempts == nil || state.UntrustedAttempts == *change.UntrustedAttempts) &&
		(change.UntrustedWithinMinutes == nil || state.UntrustedWithinMinutes == *change.UntrustedWithinMinutes) &&
		(change.UntrustedBlockMinutes == nil || state.UntrustedBlockMinutes == *change.UntrustedBlockMinutes) &&
		(change.TrustedAttempts == nil || state.TrustedAttempts == *change.TrustedAttempts) &&
		(change.TrustedWithinMinutes == nil || state.TrustedWithinMinutes == *change.TrustedWithinMinutes) &&
		(change.TrustedBlockMinutes == nil || state.TrustedBlockMinutes == *change.TrustedBlockMinutes)
}

func accountProtectionEffects(state synology.AccountProtection, change accountprotection.AccountProtectionChange) (string, []string, []string) {
	warnings := []string{}
	summary := []string{}
	high := false
	if change.Enabled != nil && *change.Enabled != state.Enabled {
		if *change.Enabled {
			summary = append(summary, "enable Account Protection")
		} else {
			summary = append(summary, "disable Account Protection")
			warnings = append(warnings, "disabling Account Protection stops DSM from blocking untrusted clients after repeated failed sign-ins, weakening the security posture")
			high = true
		}
	}
	high = accountProtectionThresholdEffect(&summary, &warnings, "untrusted", "attempt threshold", change.UntrustedAttempts, state.UntrustedAttempts, true) || high
	high = accountProtectionThresholdEffect(&summary, &warnings, "untrusted", "detection window (minutes)", change.UntrustedWithinMinutes, state.UntrustedWithinMinutes, true) || high
	high = accountProtectionThresholdEffect(&summary, &warnings, "untrusted", "block duration (minutes)", change.UntrustedBlockMinutes, state.UntrustedBlockMinutes, false) || high
	high = accountProtectionThresholdEffect(&summary, &warnings, "trusted", "attempt threshold", change.TrustedAttempts, state.TrustedAttempts, true) || high
	high = accountProtectionThresholdEffect(&summary, &warnings, "trusted", "detection window (minutes)", change.TrustedWithinMinutes, state.TrustedWithinMinutes, true) || high
	high = accountProtectionThresholdEffect(&summary, &warnings, "trusted", "block duration (minutes)", change.TrustedBlockMinutes, state.TrustedBlockMinutes, false) || high
	risk := "medium"
	if high {
		risk = "high"
	}
	return risk, warnings, summary
}

// accountProtectionThresholdEffect records a single threshold change and reports
// whether it weakens blocking. weakensWhenRaised marks the fields (attempt count,
// detection window) whose increase tolerates more abuse before blocking; the
// block-duration fields are not weakening in either direction and never raise risk.
func accountProtectionThresholdEffect(summary, warnings *[]string, client, label string, requested *int, current int, weakensWhenRaised bool) bool {
	if requested == nil || *requested == current {
		return false
	}
	*summary = append(*summary, fmt.Sprintf("change the %s %s from %d to %d", client, label, current, *requested))
	if weakensWhenRaised && *requested > current {
		*warnings = append(*warnings, fmt.Sprintf("raising the %s %s to %d weakens blocking for %s clients", client, label, *requested, client))
		return true
	}
	return false
}

func verifyAccountProtectionPostcondition(ctx context.Context, client accountProtectionClient, change accountprotection.AccountProtectionChange) error {
	state, err := client.AccountProtection(ctx)
	if err != nil {
		return err
	}
	checks := []struct {
		requested *int
		actual    int
		name      string
	}{
		{change.UntrustedAttempts, state.UntrustedAttempts, "untrusted_attempts"},
		{change.UntrustedWithinMinutes, state.UntrustedWithinMinutes, "untrusted_within_minutes"},
		{change.UntrustedBlockMinutes, state.UntrustedBlockMinutes, "untrusted_block_minutes"},
		{change.TrustedAttempts, state.TrustedAttempts, "trusted_attempts"},
		{change.TrustedWithinMinutes, state.TrustedWithinMinutes, "trusted_within_minutes"},
		{change.TrustedBlockMinutes, state.TrustedBlockMinutes, "trusted_block_minutes"},
	}
	if change.Enabled != nil && state.Enabled != *change.Enabled {
		return fmt.Errorf("enabled is %t, want %t", state.Enabled, *change.Enabled)
	}
	for _, check := range checks {
		if check.requested != nil && check.actual != *check.requested {
			return fmt.Errorf("%s is %d, want %d", check.name, check.actual, *check.requested)
		}
	}
	return nil
}

func validateAccountProtectionPlan(plan AccountProtectionThresholdsPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the account protection plan")
	}
	if plan.APIVersion != accountProtectionAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid account protection plan metadata")
	}
	if err := validateAccountProtectionShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("account protection plan observed state was modified")
	}
	expectedHash, err := accountProtectionPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("account protection plan contents were modified after planning")
	}
	return nil
}

func accountProtectionPlanHash(plan AccountProtectionThresholdsPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// ---- Enforced-2FA policy ----------------------------------------------------

type EnforceTwoFactorPlan struct {
	APIVersion          string                                    `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                                    `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                                    `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             accountprotection.EnforceTwoFactorChange `json:"request" jsonschema:"Validated enforced-2FA policy intent"`
	Observed            synology.EnforceTwoFactor                 `json:"observed" jsonschema:"Enforced-2FA policy scope observed during planning"`
	ObservedFingerprint string                                    `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the observed policy"`
	Risk                string                                    `json:"risk" jsonschema:"Plan risk level (always high for an enforced-2FA change)"`
	Warnings            []string                                  `json:"warnings" jsonschema:"Lockout and posture warnings"`
	Summary             []string                                  `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                                    `json:"hash" jsonschema:"SHA-256 approval hash covering intent and observed state"`
}

func (s *Service) PlanEnforceTwoFactorChange(ctx context.Context, requestedNAS string, request accountprotection.EnforceTwoFactorChange) (EnforceTwoFactorPlan, error) {
	if err := validateEnforceTwoFactorShape(request); err != nil {
		return EnforceTwoFactorPlan{}, err
	}
	name, client, err := s.accountProtectionClient(ctx, requestedNAS)
	if err != nil {
		return EnforceTwoFactorPlan{}, err
	}
	plan, err := planEnforceTwoFactorWithClient(ctx, name, client, request)
	if err != nil {
		return EnforceTwoFactorPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = enforceTwoFactorPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyEnforceTwoFactorPlan(ctx context.Context, plan EnforceTwoFactorPlan, approvalHash string) (AccountProtectionApplyResult, error) {
	if err := validateEnforceTwoFactorPlan(plan, approvalHash); err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return AccountProtectionApplyResult{}, err
	}
	name, client, err := s.accountProtectionClient(ctx, plan.NAS)
	if err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if name != plan.NAS {
		return AccountProtectionApplyResult{}, fmt.Errorf("enforce 2fa plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyEnforceTwoFactorWithClient(ctx, client, plan)
}

func planEnforceTwoFactorWithClient(ctx context.Context, nas string, client accountProtectionClient, request accountprotection.EnforceTwoFactorChange) (EnforceTwoFactorPlan, error) {
	capabilities, _, err := client.AccountProtectionCapabilities(ctx)
	if err != nil {
		return EnforceTwoFactorPlan{}, authenticationError(nas, err)
	}
	if !capabilities.EnforceTwoFactorRead || !capabilities.EnforceTwoFactorWrite {
		return EnforceTwoFactorPlan{}, fmt.Errorf("NAS %q does not expose a verified enforced-2FA read/write backend", nas)
	}
	state, err := client.EnforceTwoFactor(ctx)
	if err != nil {
		return EnforceTwoFactorPlan{}, authenticationError(nas, err)
	}
	desiredOption := strings.TrimSpace(*request.Option)
	if strings.EqualFold(desiredOption, state.Option) {
		return EnforceTwoFactorPlan{}, fmt.Errorf("enforce 2fa patch would not change the current policy")
	}
	enabling := !strings.EqualFold(desiredOption, "none")
	if enabling && !request.AllowLockoutOverride {
		return EnforceTwoFactorPlan{}, fmt.Errorf("setting the enforced-2FA scope to %q can lock out an administrator who has not enrolled 2FA; re-plan with allow_lockout_override set to proceed", desiredOption)
	}
	plan := EnforceTwoFactorPlan{APIVersion: accountProtectionAPIVersion, NAS: nas, Request: request, Observed: state}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return EnforceTwoFactorPlan{}, err
	}
	plan.Risk, plan.Warnings, plan.Summary = enforceTwoFactorEffects(state, desiredOption, enabling)
	plan.Hash, err = enforceTwoFactorPlanHash(plan)
	if err != nil {
		return EnforceTwoFactorPlan{}, err
	}
	return plan, nil
}

func applyEnforceTwoFactorWithClient(ctx context.Context, client accountProtectionClient, plan EnforceTwoFactorPlan) (AccountProtectionApplyResult, error) {
	current, err := planEnforceTwoFactorWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return AccountProtectionApplyResult{}, fmt.Errorf("enforce 2fa plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = enforceTwoFactorPlanHash(current)
	if err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return AccountProtectionApplyResult{}, fmt.Errorf("enforce 2fa plan is stale; create a new plan")
	}
	operation, err := client.ApplyEnforceTwoFactorChange(ctx, plan.Request)
	if err != nil {
		return AccountProtectionApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyEnforceTwoFactorPostcondition(ctx, client, strings.TrimSpace(*plan.Request.Option)); err != nil {
		return AccountProtectionApplyResult{}, fmt.Errorf("verify enforce 2fa change: %w", err)
	}
	return AccountProtectionApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func validateEnforceTwoFactorShape(change accountprotection.EnforceTwoFactorChange) error {
	if change.IsEmpty() {
		return fmt.Errorf("enforce 2fa patch has no option")
	}
	if strings.TrimSpace(*change.Option) == "" {
		return fmt.Errorf("enforce 2fa option must not be empty")
	}
	return nil
}

func enforceTwoFactorEffects(state synology.EnforceTwoFactor, desiredOption string, enabling bool) (string, []string, []string) {
	warnings := []string{}
	summary := []string{fmt.Sprintf("change the enforced-2FA scope from %q to %q", state.Option, desiredOption)}
	if enabling {
		warnings = append(warnings, fmt.Sprintf("enabling enforced 2FA (scope %q) forces two-factor sign-in and can lock out an administrator who has not enrolled 2FA", desiredOption))
	} else {
		warnings = append(warnings, "disabling enforced 2FA removes the two-factor requirement, weakening the security posture")
	}
	// Every enforced-2FA change is high risk: enabling carries lockout risk,
	// disabling weakens the posture.
	return "high", warnings, summary
}

func verifyEnforceTwoFactorPostcondition(ctx context.Context, client accountProtectionClient, desiredOption string) error {
	state, err := client.EnforceTwoFactor(ctx)
	if err != nil {
		return err
	}
	if !strings.EqualFold(state.Option, desiredOption) {
		return fmt.Errorf("otp_enforce_option is %q, want %q", state.Option, desiredOption)
	}
	return nil
}

func validateEnforceTwoFactorPlan(plan EnforceTwoFactorPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the enforce 2fa plan")
	}
	if plan.APIVersion != accountProtectionAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid enforce 2fa plan metadata")
	}
	if err := validateEnforceTwoFactorShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("enforce 2fa plan observed state was modified")
	}
	expectedHash, err := enforceTwoFactorPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("enforce 2fa plan contents were modified after planning")
	}
	return nil
}

func enforceTwoFactorPlanHash(plan EnforceTwoFactorPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// ---- Auto Block allow/block list edit ---------------------------------------

// AutoBlockListObserved is the complete list state (both sides) plus the source
// IPs of currently active connections. The protected sources feed the
// self-lockout guardrail; hashing them into the plan means a new active
// connection (a new source that could be locked out) invalidates a stale plan.
type AutoBlockListObserved struct {
	Lists            synology.AutoBlockLists `json:"lists" jsonschema:"Both Auto Block allow/block lists observed during planning"`
	ProtectedSources []string                `json:"protected_sources" jsonschema:"Source IPs of currently active connections that must not be locked out"`
}

type AutoBlockListPlan struct {
	APIVersion          string                        `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                        `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                        `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             accountprotection.IPListEdit `json:"request" jsonschema:"Validated single-entry allow/block list edit"`
	Observed            AutoBlockListObserved         `json:"observed" jsonschema:"Complete list state and active sources observed during planning"`
	ObservedFingerprint string                        `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Risk                string                        `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                      `json:"warnings" jsonschema:"Self-lockout and posture warnings"`
	Summary             []string                      `json:"summary" jsonschema:"Human-readable patch operation"`
	Hash                string                        `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

func (s *Service) PlanAutoBlockListChange(ctx context.Context, requestedNAS string, request accountprotection.IPListEdit) (AutoBlockListPlan, error) {
	if err := validateAutoBlockListShape(request); err != nil {
		return AutoBlockListPlan{}, err
	}
	name, client, err := s.accountProtectionClient(ctx, requestedNAS)
	if err != nil {
		return AutoBlockListPlan{}, err
	}
	plan, err := planAutoBlockListWithClient(ctx, name, client, request)
	if err != nil {
		return AutoBlockListPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = autoBlockListPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyAutoBlockListPlan(ctx context.Context, plan AutoBlockListPlan, approvalHash string) (AccountProtectionApplyResult, error) {
	if err := validateAutoBlockListPlan(plan, approvalHash); err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return AccountProtectionApplyResult{}, err
	}
	name, client, err := s.accountProtectionClient(ctx, plan.NAS)
	if err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if name != plan.NAS {
		return AccountProtectionApplyResult{}, fmt.Errorf("auto block list plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyAutoBlockListWithClient(ctx, client, plan)
}

func planAutoBlockListWithClient(ctx context.Context, nas string, client accountProtectionClient, request accountprotection.IPListEdit) (AutoBlockListPlan, error) {
	capabilities, _, err := client.AccountProtectionCapabilities(ctx)
	if err != nil {
		return AutoBlockListPlan{}, authenticationError(nas, err)
	}
	if !capabilities.AutoBlockListRead || !capabilities.AutoBlockListWrite {
		return AutoBlockListPlan{}, fmt.Errorf("NAS %q does not expose a verified Auto Block list read/write backend", nas)
	}
	lists, err := client.AutoBlockLists(ctx)
	if err != nil {
		return AutoBlockListPlan{}, authenticationError(nas, err)
	}
	connections, err := client.ActiveConnections(ctx)
	if err != nil {
		return AutoBlockListPlan{}, authenticationError(nas, err)
	}
	observed := AutoBlockListObserved{Lists: lists, ProtectedSources: protectedSources(connections)}
	risk, warnings, summary, err := autoBlockListEffects(observed, request)
	if err != nil {
		return AutoBlockListPlan{}, err
	}
	plan := AutoBlockListPlan{APIVersion: accountProtectionAPIVersion, NAS: nas, Request: request, Observed: observed, Risk: risk, Warnings: warnings, Summary: summary}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return AutoBlockListPlan{}, err
	}
	plan.Hash, err = autoBlockListPlanHash(plan)
	if err != nil {
		return AutoBlockListPlan{}, err
	}
	return plan, nil
}

func applyAutoBlockListWithClient(ctx context.Context, client accountProtectionClient, plan AutoBlockListPlan) (AccountProtectionApplyResult, error) {
	current, err := planAutoBlockListWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return AccountProtectionApplyResult{}, fmt.Errorf("auto block list plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = autoBlockListPlanHash(current)
	if err != nil {
		return AccountProtectionApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return AccountProtectionApplyResult{}, fmt.Errorf("auto block list plan is stale; create a new plan")
	}
	operation, err := client.ApplyAutoBlockListEdit(ctx, plan.Request)
	if err != nil {
		return AccountProtectionApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyAutoBlockListPostcondition(ctx, client, plan.Request); err != nil {
		return AccountProtectionApplyResult{}, fmt.Errorf("verify auto block list change: %w", err)
	}
	return AccountProtectionApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func validateAutoBlockListShape(edit accountprotection.IPListEdit) error {
	switch edit.Kind {
	case accountprotection.KindAllow, accountprotection.KindBlock:
	case "":
		return fmt.Errorf("auto block list edit requires a kind (allow or block)")
	default:
		return fmt.Errorf("unsupported list kind %q; expected %q or %q", edit.Kind, accountprotection.KindAllow, accountprotection.KindBlock)
	}
	if strings.TrimSpace(edit.IP) == "" {
		return fmt.Errorf("auto block list edit requires an ip or subnet")
	}
	if !validIPTarget(edit.IP) {
		return fmt.Errorf("auto block list edit ip %q is not a valid IP address or CIDR subnet", edit.IP)
	}
	return nil
}

func autoBlockListEffects(observed AutoBlockListObserved, edit accountprotection.IPListEdit) (string, []string, []string, error) {
	target := strings.TrimSpace(edit.IP)
	list := observed.Lists.Block
	if edit.Kind == accountprotection.KindAllow {
		list = observed.Lists.Allow
	}
	present := listContainsIP(list, target)
	if edit.Remove && !present {
		return "", nil, nil, fmt.Errorf("%s is not on the %s list; nothing to remove", target, edit.Kind)
	}
	if !edit.Remove && present {
		return "", nil, nil, fmt.Errorf("%s is already on the %s list; nothing to add", target, edit.Kind)
	}

	warnings := []string{}
	high := false
	action := "add"
	if edit.Remove {
		action = "remove"
	}
	summary := []string{fmt.Sprintf("%s %s %s the %s list", action, target, prepositionFor(action), edit.Kind)}

	switch {
	case edit.Kind == accountprotection.KindBlock && !edit.Remove:
		// Blocking a source can lock out an active session or the operator.
		if protected := matchingProtectedSource(observed.ProtectedSources, target); protected != "" {
			high = true
			if !edit.AllowLockoutOverride {
				return "", nil, nil, fmt.Errorf("blocking %s would lock out the active connection %s; re-plan with allow_lockout_override set to proceed", target, protected)
			}
			warnings = append(warnings, fmt.Sprintf("blocking %s locks out the active connection %s (override acknowledged)", target, protected))
		}
		if isBroadSubnet(target) {
			high = true
			if !edit.AllowLockoutOverride {
				return "", nil, nil, fmt.Errorf("blocking the broad subnet %s could lock out many hosts including the operator; re-plan with allow_lockout_override set to proceed", target)
			}
			warnings = append(warnings, fmt.Sprintf("blocking the broad subnet %s could lock out many hosts (override acknowledged)", target))
		}
	case edit.Kind == accountprotection.KindAllow && !edit.Remove:
		// Allow-listing a broad subnet exempts many hosts from Auto Block.
		if isBroadSubnet(target) {
			high = true
			warnings = append(warnings, fmt.Sprintf("allow-listing the broad subnet %s exempts many hosts from Auto Block, weakening the security posture", target))
		}
	case edit.Kind == accountprotection.KindAllow && edit.Remove:
		// Removing an allow entry can expose an active source to Auto Block.
		if protected := protectedWithinTarget(observed.ProtectedSources, target); protected != "" {
			high = true
			if !edit.AllowLockoutOverride {
				return "", nil, nil, fmt.Errorf("removing %s from the allow list would expose the active connection %s to Auto Block; re-plan with allow_lockout_override set to proceed", target, protected)
			}
			warnings = append(warnings, fmt.Sprintf("removing %s from the allow list exposes the active connection %s to Auto Block (override acknowledged)", target, protected))
		}
	}

	risk := "medium"
	if high {
		risk = "high"
	}
	return risk, warnings, summary, nil
}

func verifyAutoBlockListPostcondition(ctx context.Context, client accountProtectionClient, edit accountprotection.IPListEdit) error {
	lists, err := client.AutoBlockLists(ctx)
	if err != nil {
		return err
	}
	list := lists.Block
	if edit.Kind == accountprotection.KindAllow {
		list = lists.Allow
	}
	present := listContainsIP(list, strings.TrimSpace(edit.IP))
	if edit.Remove && present {
		return fmt.Errorf("%s is still on the %s list", edit.IP, edit.Kind)
	}
	if !edit.Remove && !present {
		return fmt.Errorf("%s is not on the %s list", edit.IP, edit.Kind)
	}
	return nil
}

func validateAutoBlockListPlan(plan AutoBlockListPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the auto block list plan")
	}
	if plan.APIVersion != accountProtectionAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid auto block list plan metadata")
	}
	if err := validateAutoBlockListShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("auto block list plan observed state was modified")
	}
	expectedHash, err := autoBlockListPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("auto block list plan contents were modified after planning")
	}
	return nil
}

func autoBlockListPlanHash(plan AutoBlockListPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// ---- shared list/IP helpers -------------------------------------------------

func prepositionFor(action string) string {
	if action == "remove" {
		return "from"
	}
	return "to"
}

func protectedSources(connections []synology.ActiveConnection) []string {
	sources := make([]string, 0, len(connections))
	seen := map[string]bool{}
	for _, connection := range connections {
		ip := strings.TrimSpace(connection.From)
		if ip == "" || seen[ip] {
			continue
		}
		seen[ip] = true
		sources = append(sources, ip)
	}
	return sources
}

// validIPTarget reports whether target is a valid single IP or CIDR subnet.
func validIPTarget(target string) bool {
	target = strings.TrimSpace(target)
	if net.ParseIP(target) != nil {
		return true
	}
	_, _, err := net.ParseCIDR(target)
	return err == nil
}

// listContainsIP reports whether the list has an entry with the exact target
// string (a single IP or a CIDR literal). List membership is exact-string keyed
// (DSM stores the literal entry), so this is used for no-op detection and the
// postcondition re-read.
func listContainsIP(list accountprotection.IPList, target string) bool {
	target = strings.TrimSpace(target)
	for _, entry := range list.Entries {
		if strings.TrimSpace(entry.IP) == target {
			return true
		}
	}
	return false
}

// matchingProtectedSource returns the first protected source that a block target
// (single IP or CIDR) would cover, or "" if none.
func matchingProtectedSource(protected []string, target string) string {
	for _, source := range protected {
		if ipWithinTarget(target, source) {
			return source
		}
	}
	return ""
}

// protectedWithinTarget returns the first protected source covered by an allow
// entry being removed, or "" if none.
func protectedWithinTarget(protected []string, target string) string {
	return matchingProtectedSource(protected, target)
}

// ipWithinTarget reports whether the single IP ip falls within target, where
// target is either an exact IP or a CIDR subnet.
func ipWithinTarget(target, ip string) bool {
	parsedIP := net.ParseIP(strings.TrimSpace(ip))
	if parsedIP == nil {
		return false
	}
	target = strings.TrimSpace(target)
	if targetIP := net.ParseIP(target); targetIP != nil {
		return targetIP.Equal(parsedIP)
	}
	_, subnet, err := net.ParseCIDR(target)
	if err != nil {
		return false
	}
	return subnet.Contains(parsedIP)
}

// isBroadSubnet reports whether target is a CIDR wide enough to plausibly cover
// the operator's own network: an IPv4 prefix shorter than /24 or an IPv6 prefix
// shorter than /64. A single IP is never broad.
func isBroadSubnet(target string) bool {
	target = strings.TrimSpace(target)
	_, subnet, err := net.ParseCIDR(target)
	if err != nil {
		return false
	}
	ones, bits := subnet.Mask.Size()
	if bits == 32 {
		return ones < 24
	}
	return ones < 64
}
