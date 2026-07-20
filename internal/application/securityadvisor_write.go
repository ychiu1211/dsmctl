package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/securityadvisor"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const securityAdvisorAPIVersion = "dsmctl.io/v1alpha1"

// SecurityAdvisorSchedulePlan binds a validated patch-only schedule + baseline
// intent to the complete Conf state observed while planning. Weakening the audit
// (loosening the baseline or disabling the schedule) is HIGH risk; tightening or
// a time-only change is medium.
type SecurityAdvisorSchedulePlan struct {
	APIVersion          string                                `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                                `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                                `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             securityadvisor.ScheduleChange        `json:"request" jsonschema:"Validated patch-only schedule and baseline intent"`
	Observed            synology.SecurityAdvisorConfiguration `json:"observed" jsonschema:"Complete Security Advisor configuration observed during planning"`
	ObservedFingerprint string                                `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed configuration"`
	Risk                string                                `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                              `json:"warnings" jsonschema:"Audit-weakening warnings"`
	Summary             []string                              `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                                `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type SecurityAdvisorScheduleApplyResult struct {
	NAS       string                                 `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                                 `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                                   `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operation synology.SecurityAdvisorMutationResult `json:"operation" jsonschema:"Selected DSM mutation backend"`
}

type SecurityAdvisorScanActionResult struct {
	NAS  string                             `json:"nas" jsonschema:"NAS profile used for the request"`
	Scan synology.SecurityAdvisorScanResult `json:"scan" jsonschema:"Result of the run-scan action"`
}

func (s *Service) PlanSecurityAdvisorScheduleChange(ctx context.Context, requestedNAS string, request securityadvisor.ScheduleChange) (SecurityAdvisorSchedulePlan, error) {
	if err := validateSecurityAdvisorScheduleShape(request); err != nil {
		return SecurityAdvisorSchedulePlan{}, err
	}
	name, client, err := s.securityAdvisorClient(ctx, requestedNAS)
	if err != nil {
		return SecurityAdvisorSchedulePlan{}, err
	}
	plan, err := planSecurityAdvisorScheduleWithClient(ctx, name, client, request)
	if err != nil {
		return SecurityAdvisorSchedulePlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = securityAdvisorSchedulePlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplySecurityAdvisorSchedulePlan(ctx context.Context, plan SecurityAdvisorSchedulePlan, approvalHash string) (SecurityAdvisorScheduleApplyResult, error) {
	if err := validateSecurityAdvisorSchedulePlan(plan, approvalHash); err != nil {
		return SecurityAdvisorScheduleApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return SecurityAdvisorScheduleApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return SecurityAdvisorScheduleApplyResult{}, err
	}
	name, client, err := s.securityAdvisorClient(ctx, plan.NAS)
	if err != nil {
		return SecurityAdvisorScheduleApplyResult{}, err
	}
	if name != plan.NAS {
		return SecurityAdvisorScheduleApplyResult{}, fmt.Errorf("security advisor plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applySecurityAdvisorScheduleWithClient(ctx, client, plan)
}

// RunSecurityScan triggers a full Security Advisor scan. It is an explicit,
// load-heavy action with no persisted-state fingerprint and no plan hash, and it
// is never invoked implicitly by a read.
func (s *Service) RunSecurityScan(ctx context.Context, requestedNAS string) (SecurityAdvisorScanActionResult, error) {
	name, client, err := s.securityAdvisorClient(ctx, requestedNAS)
	if err != nil {
		return SecurityAdvisorScanActionResult{}, err
	}
	return runSecurityScanWithClient(ctx, name, client)
}

func runSecurityScanWithClient(ctx context.Context, nas string, client securityAdvisorClient) (SecurityAdvisorScanActionResult, error) {
	capabilities, _, err := client.SecurityAdvisorCapabilities(ctx)
	if err != nil {
		return SecurityAdvisorScanActionResult{}, authenticationError(nas, err)
	}
	if !capabilities.RunScan {
		return SecurityAdvisorScanActionResult{}, fmt.Errorf("NAS %q does not expose a verified Security Advisor run-scan backend", nas)
	}
	scan, err := client.RunSecurityScan(ctx)
	if err != nil {
		return SecurityAdvisorScanActionResult{}, authenticationError(nas, err)
	}
	return SecurityAdvisorScanActionResult{NAS: nas, Scan: scan}, nil
}

func planSecurityAdvisorScheduleWithClient(ctx context.Context, nas string, client securityAdvisorClient, request securityadvisor.ScheduleChange) (SecurityAdvisorSchedulePlan, error) {
	capabilities, _, err := client.SecurityAdvisorCapabilities(ctx)
	if err != nil {
		return SecurityAdvisorSchedulePlan{}, authenticationError(nas, err)
	}
	if !capabilities.ScheduleRead || !capabilities.ScheduleWrite {
		return SecurityAdvisorSchedulePlan{}, fmt.Errorf("NAS %q does not expose a verified Security Advisor schedule read/write backend", nas)
	}
	state, err := client.SecurityAdvisorConfiguration(ctx)
	if err != nil {
		return SecurityAdvisorSchedulePlan{}, authenticationError(nas, err)
	}
	if err := validateSecurityAdvisorScheduleAgainstState(state, request); err != nil {
		return SecurityAdvisorSchedulePlan{}, err
	}
	plan := SecurityAdvisorSchedulePlan{APIVersion: securityAdvisorAPIVersion, NAS: nas, Request: request, Observed: state}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return SecurityAdvisorSchedulePlan{}, err
	}
	plan.Risk, plan.Warnings, plan.Summary = securityAdvisorScheduleEffects(state, request)
	plan.Hash, err = securityAdvisorSchedulePlanHash(plan)
	if err != nil {
		return SecurityAdvisorSchedulePlan{}, err
	}
	return plan, nil
}

func applySecurityAdvisorScheduleWithClient(ctx context.Context, client securityAdvisorClient, plan SecurityAdvisorSchedulePlan) (SecurityAdvisorScheduleApplyResult, error) {
	current, err := planSecurityAdvisorScheduleWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return SecurityAdvisorScheduleApplyResult{}, fmt.Errorf("security advisor plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = securityAdvisorSchedulePlanHash(current)
	if err != nil {
		return SecurityAdvisorScheduleApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return SecurityAdvisorScheduleApplyResult{}, fmt.Errorf("security advisor plan is stale; create a new plan")
	}
	operation, err := client.ApplySecurityAdvisorScheduleChange(ctx, plan.Request)
	if err != nil {
		return SecurityAdvisorScheduleApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifySecurityAdvisorSchedulePostcondition(ctx, client, plan.Request); err != nil {
		return SecurityAdvisorScheduleApplyResult{}, fmt.Errorf("verify security advisor change: %w", err)
	}
	return SecurityAdvisorScheduleApplyResult{
		NAS:       plan.NAS,
		PlanHash:  plan.Hash,
		Applied:   true,
		Operation: operation,
	}, nil
}

// validateSecurityAdvisorScheduleShape rejects everything invalid regardless of
// the observed configuration.
func validateSecurityAdvisorScheduleShape(change securityadvisor.ScheduleChange) error {
	if change.IsEmpty() {
		return fmt.Errorf("security advisor schedule patch has no fields")
	}
	if change.Baseline != nil {
		switch strings.TrimSpace(*change.Baseline) {
		case securityadvisor.BaselineHome, securityadvisor.BaselineCompany:
		case securityadvisor.BaselineCustom:
			return fmt.Errorf("baseline %q is not managed by this module; the custom checklist is per-check configuration owned by a separate work item", securityadvisor.BaselineCustom)
		default:
			return fmt.Errorf("unsupported baseline %q; expected %q or %q", *change.Baseline, securityadvisor.BaselineHome, securityadvisor.BaselineCompany)
		}
	}
	if change.Hour != nil && (*change.Hour < 0 || *change.Hour > 23) {
		return fmt.Errorf("hour %d out of range 0-23", *change.Hour)
	}
	if change.Minute != nil && (*change.Minute < 0 || *change.Minute > 59) {
		return fmt.Errorf("minute %d out of range 0-59", *change.Minute)
	}
	if change.Weekday != nil {
		day, err := strconv.Atoi(strings.TrimSpace(*change.Weekday))
		if err != nil || day < 0 || day > 6 {
			return fmt.Errorf("weekday %q must be a DSM weekday selector 0-6", *change.Weekday)
		}
	}
	return nil
}

// validateSecurityAdvisorScheduleAgainstState enforces the rules that depend on
// the freshly observed configuration.
func validateSecurityAdvisorScheduleAgainstState(state synology.SecurityAdvisorConfiguration, change securityadvisor.ScheduleChange) error {
	effectiveBaseline := state.Baseline
	if change.Baseline != nil {
		effectiveBaseline = strings.TrimSpace(*change.Baseline)
	}
	// The custom checklist cannot be preserved through this write (DSM's argGroup
	// accepts only the two managed groups), so a schedule-only patch on a
	// custom-baseline NAS must name an explicit managed baseline.
	if effectiveBaseline == securityadvisor.BaselineCustom {
		return fmt.Errorf("the NAS is on the custom checklist baseline; include an explicit baseline (%q or %q) because this module does not manage the custom checklist", securityadvisor.BaselineHome, securityadvisor.BaselineCompany)
	}
	if securityAdvisorScheduleSatisfied(state, change) {
		return fmt.Errorf("security advisor patch would not change the current configuration")
	}
	return nil
}

func securityAdvisorScheduleEffects(state synology.SecurityAdvisorConfiguration, change securityadvisor.ScheduleChange) (string, []string, []string) {
	warnings := []string{}
	summary := []string{}
	high := false
	if change.Baseline != nil && strings.TrimSpace(*change.Baseline) != state.Baseline {
		target := strings.TrimSpace(*change.Baseline)
		summary = append(summary, fmt.Sprintf("change the security baseline from %q to %q", state.Baseline, target))
		// company is the stricter business baseline; home is the lighter one.
		if state.Baseline == securityadvisor.BaselineCompany && target == securityadvisor.BaselineHome {
			warnings = append(warnings, "switching from the business baseline to the home baseline drops the stricter business checks and can hide genuine posture problems")
			high = true
		}
	}
	if change.ScheduleEnabled != nil && *change.ScheduleEnabled != state.Schedule.Enabled {
		if *change.ScheduleEnabled {
			summary = append(summary, "enable the scheduled scan")
		} else {
			summary = append(summary, "disable the scheduled scan")
			warnings = append(warnings, "disabling the scheduled scan stops the NAS from being audited automatically; posture drift will go unnoticed")
			high = true
		}
	}
	if change.Weekday != nil && strings.TrimSpace(*change.Weekday) != state.Schedule.Weekday {
		summary = append(summary, fmt.Sprintf("set the scheduled weekday to %q", strings.TrimSpace(*change.Weekday)))
	}
	if change.Hour != nil && *change.Hour != state.Schedule.Hour {
		summary = append(summary, fmt.Sprintf("set the scheduled hour to %02d", *change.Hour))
	}
	if change.Minute != nil && *change.Minute != state.Schedule.Minute {
		summary = append(summary, fmt.Sprintf("set the scheduled minute to %02d", *change.Minute))
	}
	risk := "medium"
	if high {
		risk = "high"
	}
	return risk, warnings, summary
}

func verifySecurityAdvisorSchedulePostcondition(ctx context.Context, client securityAdvisorClient, change securityadvisor.ScheduleChange) error {
	state, err := client.SecurityAdvisorConfiguration(ctx)
	if err != nil {
		return err
	}
	if change.Baseline != nil && state.Baseline != strings.TrimSpace(*change.Baseline) {
		return fmt.Errorf("baseline is %q, want %q", state.Baseline, strings.TrimSpace(*change.Baseline))
	}
	if change.ScheduleEnabled != nil && state.Schedule.Enabled != *change.ScheduleEnabled {
		return fmt.Errorf("scheduled scan enabled is %t, want %t", state.Schedule.Enabled, *change.ScheduleEnabled)
	}
	if change.Weekday != nil && state.Schedule.Weekday != strings.TrimSpace(*change.Weekday) {
		return fmt.Errorf("scheduled weekday is %q, want %q", state.Schedule.Weekday, strings.TrimSpace(*change.Weekday))
	}
	if change.Hour != nil && state.Schedule.Hour != *change.Hour {
		return fmt.Errorf("scheduled hour is %d, want %d", state.Schedule.Hour, *change.Hour)
	}
	if change.Minute != nil && state.Schedule.Minute != *change.Minute {
		return fmt.Errorf("scheduled minute is %d, want %d", state.Schedule.Minute, *change.Minute)
	}
	return nil
}

func validateSecurityAdvisorSchedulePlan(plan SecurityAdvisorSchedulePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the security advisor plan")
	}
	if plan.APIVersion != securityAdvisorAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid security advisor plan metadata")
	}
	if err := validateSecurityAdvisorScheduleShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("security advisor plan observed state was modified")
	}
	expectedHash, err := securityAdvisorSchedulePlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("security advisor plan contents were modified after planning")
	}
	return nil
}

func securityAdvisorSchedulePlanHash(plan SecurityAdvisorSchedulePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// securityAdvisorScheduleSatisfied reports whether the observed configuration
// already fulfills every requested field, which makes the patch a no-op.
func securityAdvisorScheduleSatisfied(state synology.SecurityAdvisorConfiguration, change securityadvisor.ScheduleChange) bool {
	return (change.Baseline == nil || state.Baseline == strings.TrimSpace(*change.Baseline)) &&
		(change.ScheduleEnabled == nil || state.Schedule.Enabled == *change.ScheduleEnabled) &&
		(change.Weekday == nil || state.Schedule.Weekday == strings.TrimSpace(*change.Weekday)) &&
		(change.Hour == nil || state.Schedule.Hour == *change.Hour) &&
		(change.Minute == nil || state.Schedule.Minute == *change.Minute)
}
