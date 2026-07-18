package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/resmon"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const resourceRecordingAPIVersion = "dsmctl.io/v1alpha1"

// ResourceRecordingPlan binds a validated history-recording toggle to the
// setting observed while planning. The change is reversible and never destroys
// recorded history, so the risk ceiling is medium (disabling loses future
// samples) rather than high.
type ResourceRecordingPlan struct {
	APIVersion          string                            `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                            `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                            `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             resmon.RecordingChange            `json:"request" jsonschema:"Validated history-recording toggle intent"`
	Observed            synology.ResourceRecordingSetting `json:"observed" jsonschema:"History-recording setting observed during planning"`
	ObservedFingerprint string                            `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the observed setting"`
	Risk                string                            `json:"risk" jsonschema:"Plan risk level: low or medium"`
	Warnings            []string                          `json:"warnings" jsonschema:"Consequences of the toggle"`
	Summary             []string                          `json:"summary" jsonschema:"Human-readable operations"`
	Hash                string                            `json:"hash" jsonschema:"SHA-256 approval hash covering intent and observed state"`
}

type ResourceRecordingApplyResult struct {
	NAS       string                                   `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                                   `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                                     `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operation synology.ResourceRecordingMutationResult `json:"operation" jsonschema:"Selected DSM mutation backend"`
}

type resourceRecordingClient interface {
	ResourceMonitorSetting(context.Context) (synology.ResourceRecordingSetting, error)
	ResourceMonitorCapabilities(context.Context) (synology.ResourceMonitorCapabilities, synology.CompatibilityReport, error)
	ApplyResourceRecordingChange(context.Context, resmon.RecordingChange) (synology.ResourceRecordingMutationResult, error)
}

func (s *Service) PlanResourceRecordingChange(ctx context.Context, requestedNAS string, change resmon.RecordingChange) (ResourceRecordingPlan, error) {
	if err := validateResourceRecordingChangeShape(change); err != nil {
		return ResourceRecordingPlan{}, err
	}
	name, client, err := s.resourceRecordingClient(ctx, requestedNAS)
	if err != nil {
		return ResourceRecordingPlan{}, err
	}
	plan, err := planResourceRecordingChangeWithClient(ctx, name, client, change)
	if err != nil {
		return ResourceRecordingPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = resourceRecordingPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyResourceRecordingPlan(ctx context.Context, plan ResourceRecordingPlan, approvalHash string) (ResourceRecordingApplyResult, error) {
	if err := validateResourceRecordingPlan(plan, approvalHash); err != nil {
		return ResourceRecordingApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return ResourceRecordingApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return ResourceRecordingApplyResult{}, err
	}
	name, client, err := s.resourceRecordingClient(ctx, plan.NAS)
	if err != nil {
		return ResourceRecordingApplyResult{}, err
	}
	if name != plan.NAS {
		return ResourceRecordingApplyResult{}, fmt.Errorf("recording plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyResourceRecordingPlanWithClient(ctx, client, plan)
}

func (s *Service) resourceRecordingClient(ctx context.Context, requestedNAS string) (string, resourceRecordingClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(resourceRecordingClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement Resource Monitor recording management")
	}
	return name, client, nil
}

func planResourceRecordingChangeWithClient(ctx context.Context, nas string, client resourceRecordingClient, change resmon.RecordingChange) (ResourceRecordingPlan, error) {
	capabilities, _, err := client.ResourceMonitorCapabilities(ctx)
	if err != nil {
		return ResourceRecordingPlan{}, authenticationError(nas, err)
	}
	if !capabilities.RecordingRead || !capabilities.RecordingSet {
		return ResourceRecordingPlan{}, fmt.Errorf("NAS %q does not expose a verified recording read/set backend", nas)
	}
	setting, err := client.ResourceMonitorSetting(ctx)
	if err != nil {
		return ResourceRecordingPlan{}, authenticationError(nas, err)
	}
	if *change.Enable == setting.Enabled {
		return ResourceRecordingPlan{}, fmt.Errorf("history recording is already %s", enabledWord(setting.Enabled))
	}
	plan := ResourceRecordingPlan{APIVersion: resourceRecordingAPIVersion, NAS: nas, Request: change, Observed: setting}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return ResourceRecordingPlan{}, err
	}
	plan.Risk, plan.Warnings, plan.Summary = recordingPlanEffects(*change.Enable)
	plan.Hash, err = resourceRecordingPlanHash(plan)
	if err != nil {
		return ResourceRecordingPlan{}, err
	}
	return plan, nil
}

func applyResourceRecordingPlanWithClient(ctx context.Context, client resourceRecordingClient, plan ResourceRecordingPlan) (ResourceRecordingApplyResult, error) {
	current, err := planResourceRecordingChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return ResourceRecordingApplyResult{}, fmt.Errorf("recording plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = resourceRecordingPlanHash(current)
	if err != nil {
		return ResourceRecordingApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return ResourceRecordingApplyResult{}, fmt.Errorf("recording plan is stale; create a new plan")
	}
	operation, err := client.ApplyResourceRecordingChange(ctx, plan.Request)
	if err != nil {
		return ResourceRecordingApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyResourceRecordingPostcondition(ctx, client, *plan.Request.Enable); err != nil {
		return ResourceRecordingApplyResult{}, fmt.Errorf("verify recording change: %w", err)
	}
	return ResourceRecordingApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func validateResourceRecordingChangeShape(change resmon.RecordingChange) error {
	if change.Enable == nil {
		return fmt.Errorf("recording change requires an enable value")
	}
	return nil
}

func recordingPlanEffects(enable bool) (string, []string, []string) {
	if enable {
		return "low",
			[]string{"history begins recording now; earlier periods stay empty until enough samples accumulate"},
			[]string{"enable resource history recording"}
	}
	return "medium",
		[]string{"disabling stops collecting new history; DSM retains already-recorded samples but records nothing further until re-enabled"},
		[]string{"disable resource history recording"}
}

func verifyResourceRecordingPostcondition(ctx context.Context, client resourceRecordingClient, enable bool) error {
	setting, err := client.ResourceMonitorSetting(ctx)
	if err != nil {
		return err
	}
	if setting.Enabled != enable {
		return fmt.Errorf("history recording is %s, want %s", enabledWord(setting.Enabled), enabledWord(enable))
	}
	return nil
}

func validateResourceRecordingPlan(plan ResourceRecordingPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the recording plan")
	}
	if plan.APIVersion != resourceRecordingAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid recording plan metadata")
	}
	if err := validateResourceRecordingChangeShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("recording plan observed state was modified")
	}
	expectedHash, err := resourceRecordingPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("recording plan contents were modified after planning")
	}
	return nil
}

func resourceRecordingPlanHash(plan ResourceRecordingPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func enabledWord(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

var _ resourceRecordingClient = (*synology.Client)(nil)
