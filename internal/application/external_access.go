package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/externalaccess"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const externalAccessAPIVersion = "dsmctl.io/v1alpha1"

type ExternalAccessCapabilitiesResult struct {
	NAS          string                              `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.ExternalAccessCapabilities `json:"capabilities" jsonschema:"External Access read areas currently exposed by dsmctl"`
	Report       synology.CompatibilityReport        `json:"report" jsonschema:"Discovered APIs and selected External Access compatibility backends"`
}

type ExternalAccessAccountResult struct {
	NAS     string                              `json:"nas" jsonschema:"NAS profile used for the request"`
	Account synology.ExternalAccessAccountState `json:"account" jsonschema:"Normalized Synology Account binding without any account token"`
}

type ExternalAccessQuickConnectResult struct {
	NAS          string                                   `json:"nas" jsonschema:"NAS profile used for the request"`
	QuickConnect synology.ExternalAccessQuickConnectState `json:"quickconnect" jsonschema:"Normalized QuickConnect configuration and live status"`
}

type ExternalAccessDDNSResult struct {
	NAS  string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	DDNS synology.ExternalAccessDDNSState `json:"ddns" jsonschema:"Normalized DDNS records and detected external addresses"`
}

type ExternalAccessPortForwardResult struct {
	NAS         string                                  `json:"nas" jsonschema:"NAS profile used for the request"`
	PortForward synology.ExternalAccessPortForwardState `json:"port_forward" jsonschema:"Normalized router configuration and port-forwarding rules"`
}

func (s *Service) GetExternalAccessCapabilities(ctx context.Context, requestedNAS string) (ExternalAccessCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ExternalAccessCapabilitiesResult{}, err
	}
	capabilities, report, err := client.ExternalAccessCapabilities(ctx)
	if err != nil {
		return ExternalAccessCapabilitiesResult{}, authenticationError(name, err)
	}
	return ExternalAccessCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetExternalAccessAccount(ctx context.Context, requestedNAS string) (ExternalAccessAccountResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ExternalAccessAccountResult{}, err
	}
	state, err := client.ExternalAccessAccountState(ctx)
	if err != nil {
		return ExternalAccessAccountResult{}, authenticationError(name, err)
	}
	return ExternalAccessAccountResult{NAS: name, Account: state}, nil
}

func (s *Service) GetExternalAccessQuickConnect(ctx context.Context, requestedNAS string) (ExternalAccessQuickConnectResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ExternalAccessQuickConnectResult{}, err
	}
	state, err := client.ExternalAccessQuickConnectState(ctx)
	if err != nil {
		return ExternalAccessQuickConnectResult{}, authenticationError(name, err)
	}
	return ExternalAccessQuickConnectResult{NAS: name, QuickConnect: state}, nil
}

func (s *Service) GetExternalAccessDDNS(ctx context.Context, requestedNAS string) (ExternalAccessDDNSResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ExternalAccessDDNSResult{}, err
	}
	state, err := client.ExternalAccessDDNSState(ctx)
	if err != nil {
		return ExternalAccessDDNSResult{}, authenticationError(name, err)
	}
	return ExternalAccessDDNSResult{NAS: name, DDNS: state}, nil
}

func (s *Service) GetExternalAccessPortForward(ctx context.Context, requestedNAS string) (ExternalAccessPortForwardResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ExternalAccessPortForwardResult{}, err
	}
	state, err := client.ExternalAccessPortForwardState(ctx)
	if err != nil {
		return ExternalAccessPortForwardResult{}, authenticationError(name, err)
	}
	return ExternalAccessPortForwardResult{NAS: name, PortForward: state}, nil
}

// ExternalAccessQuickConnectPlan binds a validated relay-toggle intent to the
// complete QuickConnect state observed while planning. Toggling relay changes
// external reachability, so there is no destructive-data flag; the risk label
// and warnings carry the operational consequences.
type ExternalAccessQuickConnectPlan struct {
	APIVersion          string                                   `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                                   `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                                   `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             externalaccess.QuickConnectChange        `json:"request" jsonschema:"Validated patch-only QuickConnect intent"`
	Observed            synology.ExternalAccessQuickConnectState `json:"observed" jsonschema:"Complete QuickConnect state observed during planning"`
	ObservedFingerprint string                                   `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Risk                string                                   `json:"risk" jsonschema:"Plan risk level"`
	Warnings            []string                                 `json:"warnings" jsonschema:"External-reachability warnings"`
	Summary             []string                                 `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                                   `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type ExternalAccessQuickConnectApplyResult struct {
	NAS      string                                            `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                                            `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied  bool                                              `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Result   synology.ExternalAccessQuickConnectMutationResult `json:"result" jsonschema:"Selected DSM mutation backend"`
}

type externalAccessQuickConnectClient interface {
	ExternalAccessQuickConnectState(context.Context) (synology.ExternalAccessQuickConnectState, error)
	ExternalAccessCapabilities(context.Context) (synology.ExternalAccessCapabilities, synology.CompatibilityReport, error)
	ApplyExternalAccessQuickConnectChange(context.Context, synology.ExternalAccessQuickConnectChange) (synology.ExternalAccessQuickConnectMutationResult, error)
}

func (s *Service) externalAccessQuickConnectClient(ctx context.Context, requestedNAS string) (string, externalAccessQuickConnectClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(externalAccessQuickConnectClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement External Access QuickConnect management")
	}
	return name, client, nil
}

func (s *Service) PlanExternalAccessQuickConnectChange(ctx context.Context, requestedNAS string, request externalaccess.QuickConnectChange) (ExternalAccessQuickConnectPlan, error) {
	if request.RelayEnabled == nil {
		return ExternalAccessQuickConnectPlan{}, fmt.Errorf("QuickConnect patch has no fields")
	}
	name, client, err := s.externalAccessQuickConnectClient(ctx, requestedNAS)
	if err != nil {
		return ExternalAccessQuickConnectPlan{}, err
	}
	plan, err := planExternalAccessQuickConnectWithClient(ctx, name, client, request)
	if err != nil {
		return ExternalAccessQuickConnectPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = externalAccessQuickConnectPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyExternalAccessQuickConnectPlan(ctx context.Context, plan ExternalAccessQuickConnectPlan, approvalHash string) (ExternalAccessQuickConnectApplyResult, error) {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return ExternalAccessQuickConnectApplyResult{}, fmt.Errorf("approval hash does not match the QuickConnect plan")
	}
	if plan.APIVersion != externalAccessAPIVersion || strings.TrimSpace(plan.NAS) == "" || plan.Request.RelayEnabled == nil {
		return ExternalAccessQuickConnectApplyResult{}, fmt.Errorf("invalid QuickConnect plan metadata")
	}
	expectedHash, err := externalAccessQuickConnectPlanHash(plan)
	if err != nil {
		return ExternalAccessQuickConnectApplyResult{}, err
	}
	if expectedHash != plan.Hash {
		return ExternalAccessQuickConnectApplyResult{}, fmt.Errorf("QuickConnect plan contents were modified after planning")
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return ExternalAccessQuickConnectApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return ExternalAccessQuickConnectApplyResult{}, err
	}
	name, client, err := s.externalAccessQuickConnectClient(ctx, plan.NAS)
	if err != nil {
		return ExternalAccessQuickConnectApplyResult{}, err
	}
	if name != plan.NAS {
		return ExternalAccessQuickConnectApplyResult{}, fmt.Errorf("QuickConnect plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	current, err := planExternalAccessQuickConnectWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return ExternalAccessQuickConnectApplyResult{}, fmt.Errorf("QuickConnect plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = externalAccessQuickConnectPlanHash(current)
	if err != nil {
		return ExternalAccessQuickConnectApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return ExternalAccessQuickConnectApplyResult{}, fmt.Errorf("QuickConnect plan is stale; create a new plan")
	}
	result, err := client.ApplyExternalAccessQuickConnectChange(ctx, plan.Request)
	if err != nil {
		return ExternalAccessQuickConnectApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := client.ExternalAccessQuickConnectState(ctx)
	if err != nil {
		return ExternalAccessQuickConnectApplyResult{}, fmt.Errorf("verify QuickConnect change: %w", err)
	}
	if after.RelayEnabled == nil || *after.RelayEnabled != *plan.Request.RelayEnabled {
		return ExternalAccessQuickConnectApplyResult{}, fmt.Errorf("QuickConnect relay state does not match the approved patch")
	}
	return ExternalAccessQuickConnectApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Result: result}, nil
}

func planExternalAccessQuickConnectWithClient(ctx context.Context, nas string, client externalAccessQuickConnectClient, request externalaccess.QuickConnectChange) (ExternalAccessQuickConnectPlan, error) {
	capabilities, _, err := client.ExternalAccessCapabilities(ctx)
	if err != nil {
		return ExternalAccessQuickConnectPlan{}, authenticationError(nas, err)
	}
	if !capabilities.QuickConnect {
		return ExternalAccessQuickConnectPlan{}, fmt.Errorf("NAS %q does not expose a verified QuickConnect read backend", nas)
	}
	if !capabilities.QuickConnectSet {
		return ExternalAccessQuickConnectPlan{}, fmt.Errorf("NAS %q does not expose the QuickConnect relay set backend (requires QuickConnect API v3)", nas)
	}
	observed, err := client.ExternalAccessQuickConnectState(ctx)
	if err != nil {
		return ExternalAccessQuickConnectPlan{}, authenticationError(nas, err)
	}
	if observed.RelayEnabled == nil {
		return ExternalAccessQuickConnectPlan{}, fmt.Errorf("NAS %q did not report the current relay setting", nas)
	}
	if *observed.RelayEnabled == *request.RelayEnabled {
		return ExternalAccessQuickConnectPlan{}, fmt.Errorf("QuickConnect patch would not change the current relay state")
	}
	plan := ExternalAccessQuickConnectPlan{APIVersion: externalAccessAPIVersion, NAS: nas, Request: request, Observed: observed, Risk: "high"}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return ExternalAccessQuickConnectPlan{}, err
	}
	plan.Summary = []string{fmt.Sprintf("set QuickConnect relay enabled to %t", *request.RelayEnabled)}
	if *request.RelayEnabled {
		plan.Warnings = []string{"enabling relay allows external clients to reach this NAS through Synology's relay servers"}
	} else {
		plan.Warnings = []string{"disabling relay stops relayed external connections; clients that depend on the relay lose access until it is re-enabled"}
	}
	plan.Hash, err = externalAccessQuickConnectPlanHash(plan)
	if err != nil {
		return ExternalAccessQuickConnectPlan{}, err
	}
	return plan, nil
}

func externalAccessQuickConnectPlanHash(plan ExternalAccessQuickConnectPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

var _ externalAccessQuickConnectClient = (*synology.Client)(nil)

// externalAccessWriteClient is the surface the config/permission/DDNS writes
// need beyond the relay-only client above.
type externalAccessWriteClient interface {
	ExternalAccessCapabilities(context.Context) (synology.ExternalAccessCapabilities, synology.CompatibilityReport, error)
	ExternalAccessQuickConnectState(context.Context) (synology.ExternalAccessQuickConnectState, error)
	ExternalAccessDDNSState(context.Context) (synology.ExternalAccessDDNSState, error)
	ApplyExternalAccessQuickConnectConfigChange(context.Context, synology.ExternalAccessQuickConnectConfigChange) (synology.ExternalAccessMutationResult, error)
	ApplyExternalAccessQuickConnectPermissionChange(context.Context, []externalaccess.QuickConnectService) (synology.ExternalAccessMutationResult, error)
	ApplyExternalAccessDDNSChange(context.Context, synology.ExternalAccessDDNSRecordChange, string) (synology.ExternalAccessMutationResult, error)
}

func (s *Service) externalAccessWriteClient(ctx context.Context, requestedNAS string) (string, externalAccessWriteClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(externalAccessWriteClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement External Access write management")
	}
	return name, client, nil
}

// ---- QuickConnect config (enabled / alias / region) ------------------------

type ExternalAccessQuickConnectConfigPlan struct {
	APIVersion          string                                     `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                                     `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                                     `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             externalaccess.QuickConnectConfigChange    `json:"request" jsonschema:"Validated patch-only QuickConnect config intent"`
	Observed            synology.ExternalAccessQuickConnectState   `json:"observed" jsonschema:"Complete QuickConnect state observed during planning"`
	ObservedFingerprint string                                     `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Risk                string                                     `json:"risk" jsonschema:"Plan risk level (always high: changes external exposure or the globally-unique alias)"`
	Warnings            []string                                   `json:"warnings" jsonschema:"External-reachability warnings"`
	Summary             []string                                   `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                                     `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type ExternalAccessQuickConnectConfigApplyResult struct {
	NAS      string                                `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                                `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied  bool                                  `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Result   synology.ExternalAccessMutationResult `json:"result" jsonschema:"Selected DSM mutation backend"`
}

func externalAccessQuickConnectConfigPlanHash(plan ExternalAccessQuickConnectConfigPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func validateQuickConnectConfigChange(change externalaccess.QuickConnectConfigChange) error {
	if change.Enabled == nil && change.ServerAlias == nil && change.Region == nil {
		return fmt.Errorf("QuickConnect config patch has no fields")
	}
	if change.ServerAlias != nil && strings.TrimSpace(*change.ServerAlias) == "" {
		return fmt.Errorf("server_alias must not be empty")
	}
	if change.Region != nil && strings.TrimSpace(*change.Region) == "" {
		return fmt.Errorf("region must not be empty")
	}
	return nil
}

func (s *Service) PlanExternalAccessQuickConnectConfigChange(ctx context.Context, requestedNAS string, request externalaccess.QuickConnectConfigChange) (ExternalAccessQuickConnectConfigPlan, error) {
	if err := validateQuickConnectConfigChange(request); err != nil {
		return ExternalAccessQuickConnectConfigPlan{}, err
	}
	name, client, err := s.externalAccessWriteClient(ctx, requestedNAS)
	if err != nil {
		return ExternalAccessQuickConnectConfigPlan{}, err
	}
	plan, err := planQuickConnectConfigWithClient(ctx, name, client, request)
	if err != nil {
		return ExternalAccessQuickConnectConfigPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = externalAccessQuickConnectConfigPlanHash(plan)
	}
	return plan, err
}

func planQuickConnectConfigWithClient(ctx context.Context, nas string, client externalAccessWriteClient, request externalaccess.QuickConnectConfigChange) (ExternalAccessQuickConnectConfigPlan, error) {
	capabilities, _, err := client.ExternalAccessCapabilities(ctx)
	if err != nil {
		return ExternalAccessQuickConnectConfigPlan{}, authenticationError(nas, err)
	}
	if !capabilities.QuickConnect || !capabilities.QuickConnectConfigSet {
		return ExternalAccessQuickConnectConfigPlan{}, fmt.Errorf("NAS %q does not expose a verified QuickConnect config set backend", nas)
	}
	observed, err := client.ExternalAccessQuickConnectState(ctx)
	if err != nil {
		return ExternalAccessQuickConnectConfigPlan{}, authenticationError(nas, err)
	}
	summary := []string{}
	changed := false
	if request.Enabled != nil && *request.Enabled != observed.Enabled {
		summary = append(summary, fmt.Sprintf("set QuickConnect enabled to %t", *request.Enabled))
		changed = true
	}
	if request.ServerAlias != nil && *request.ServerAlias != observed.ID {
		summary = append(summary, fmt.Sprintf("set QuickConnect alias to %q (currently %q)", *request.ServerAlias, observed.ID))
		changed = true
	}
	if request.Region != nil && *request.Region != observed.Region {
		summary = append(summary, fmt.Sprintf("set QuickConnect region to %q", *request.Region))
		changed = true
	}
	if !changed {
		return ExternalAccessQuickConnectConfigPlan{}, fmt.Errorf("QuickConnect config patch would not change the current configuration")
	}
	plan := ExternalAccessQuickConnectConfigPlan{APIVersion: externalAccessAPIVersion, NAS: nas, Request: request, Observed: observed, Risk: "high", Summary: summary}
	plan.Warnings = []string{"changing the QuickConnect alias re-registers a globally-unique external name and may drop clients using the old one; enabling/disabling QuickConnect changes public reachability"}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return ExternalAccessQuickConnectConfigPlan{}, err
	}
	plan.Hash, err = externalAccessQuickConnectConfigPlanHash(plan)
	if err != nil {
		return ExternalAccessQuickConnectConfigPlan{}, err
	}
	return plan, nil
}

func (s *Service) ApplyExternalAccessQuickConnectConfigPlan(ctx context.Context, plan ExternalAccessQuickConnectConfigPlan, approvalHash string) (ExternalAccessQuickConnectConfigApplyResult, error) {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return ExternalAccessQuickConnectConfigApplyResult{}, fmt.Errorf("approval hash does not match the QuickConnect config plan")
	}
	if plan.APIVersion != externalAccessAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return ExternalAccessQuickConnectConfigApplyResult{}, fmt.Errorf("invalid QuickConnect config plan metadata")
	}
	if err := validateQuickConnectConfigChange(plan.Request); err != nil {
		return ExternalAccessQuickConnectConfigApplyResult{}, err
	}
	if expected, err := externalAccessQuickConnectConfigPlanHash(plan); err != nil || expected != approvalHash {
		return ExternalAccessQuickConnectConfigApplyResult{}, fmt.Errorf("QuickConnect config plan contents were modified after planning")
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return ExternalAccessQuickConnectConfigApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return ExternalAccessQuickConnectConfigApplyResult{}, err
	}
	name, client, err := s.externalAccessWriteClient(ctx, plan.NAS)
	if err != nil {
		return ExternalAccessQuickConnectConfigApplyResult{}, err
	}
	if name != plan.NAS {
		return ExternalAccessQuickConnectConfigApplyResult{}, fmt.Errorf("QuickConnect config plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	current, err := planQuickConnectConfigWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return ExternalAccessQuickConnectConfigApplyResult{}, fmt.Errorf("QuickConnect config plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	if current.Hash, err = externalAccessQuickConnectConfigPlanHash(current); err != nil {
		return ExternalAccessQuickConnectConfigApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return ExternalAccessQuickConnectConfigApplyResult{}, fmt.Errorf("QuickConnect config plan is stale; create a new plan")
	}
	result, err := client.ApplyExternalAccessQuickConnectConfigChange(ctx, plan.Request)
	if err != nil {
		return ExternalAccessQuickConnectConfigApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := client.ExternalAccessQuickConnectState(ctx)
	if err != nil {
		return ExternalAccessQuickConnectConfigApplyResult{}, fmt.Errorf("verify QuickConnect config change: %w", err)
	}
	if plan.Request.Enabled != nil && after.Enabled != *plan.Request.Enabled {
		return ExternalAccessQuickConnectConfigApplyResult{}, fmt.Errorf("QuickConnect enabled state does not match the approved patch")
	}
	if plan.Request.ServerAlias != nil && after.ID != *plan.Request.ServerAlias {
		return ExternalAccessQuickConnectConfigApplyResult{}, fmt.Errorf("QuickConnect alias does not match the approved patch")
	}
	if plan.Request.Region != nil && after.Region != *plan.Request.Region {
		return ExternalAccessQuickConnectConfigApplyResult{}, fmt.Errorf("QuickConnect region does not match the approved patch")
	}
	return ExternalAccessQuickConnectConfigApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Result: result}, nil
}

// ---- QuickConnect per-service permission (live-verified) -------------------

type ExternalAccessQuickConnectPermissionPlan struct {
	APIVersion          string                                      `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                                      `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                                      `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             externalaccess.QuickConnectPermissionChange `json:"request" jsonschema:"Validated per-service exposure intent"`
	Observed            synology.ExternalAccessQuickConnectState    `json:"observed" jsonschema:"Complete QuickConnect state observed during planning"`
	ObservedFingerprint string                                      `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Risk                string                                      `json:"risk" jsonschema:"Plan risk level (always high: changes external reachability)"`
	Warnings            []string                                    `json:"warnings" jsonschema:"External-reachability warnings"`
	Summary             []string                                    `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                                      `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type ExternalAccessQuickConnectPermissionApplyResult struct {
	NAS      string                                `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                                `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied  bool                                  `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Result   synology.ExternalAccessMutationResult `json:"result" jsonschema:"Selected DSM mutation backend"`
}

func externalAccessQuickConnectPermissionPlanHash(plan ExternalAccessQuickConnectPermissionPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// mergeQuickConnectServices applies a per-service patch onto the full observed
// service list, so the complete list can be sent (DSM rejects a partial set).
func mergeQuickConnectServices(observed []externalaccess.QuickConnectService, patch []externalaccess.QuickConnectService) []externalaccess.QuickConnectService {
	want := map[string]bool{}
	for _, service := range patch {
		want[service.ID] = service.Enabled
	}
	desired := make([]externalaccess.QuickConnectService, 0, len(observed))
	for _, service := range observed {
		enabled := service.Enabled
		if override, ok := want[service.ID]; ok {
			enabled = override
		}
		desired = append(desired, externalaccess.QuickConnectService{ID: service.ID, Enabled: enabled})
	}
	return desired
}

func validateQuickConnectPermissionChange(change externalaccess.QuickConnectPermissionChange) error {
	if len(change.Services) == 0 {
		return fmt.Errorf("QuickConnect permission patch lists no services")
	}
	seen := map[string]bool{}
	for _, service := range change.Services {
		if strings.TrimSpace(service.ID) == "" {
			return fmt.Errorf("QuickConnect service id must not be empty")
		}
		if seen[service.ID] {
			return fmt.Errorf("QuickConnect service %q is listed twice", service.ID)
		}
		seen[service.ID] = true
	}
	return nil
}

func (s *Service) PlanExternalAccessQuickConnectPermissionChange(ctx context.Context, requestedNAS string, request externalaccess.QuickConnectPermissionChange) (ExternalAccessQuickConnectPermissionPlan, error) {
	if err := validateQuickConnectPermissionChange(request); err != nil {
		return ExternalAccessQuickConnectPermissionPlan{}, err
	}
	name, client, err := s.externalAccessWriteClient(ctx, requestedNAS)
	if err != nil {
		return ExternalAccessQuickConnectPermissionPlan{}, err
	}
	plan, err := planQuickConnectPermissionWithClient(ctx, name, client, request)
	if err != nil {
		return ExternalAccessQuickConnectPermissionPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = externalAccessQuickConnectPermissionPlanHash(plan)
	}
	return plan, err
}

func planQuickConnectPermissionWithClient(ctx context.Context, nas string, client externalAccessWriteClient, request externalaccess.QuickConnectPermissionChange) (ExternalAccessQuickConnectPermissionPlan, error) {
	capabilities, _, err := client.ExternalAccessCapabilities(ctx)
	if err != nil {
		return ExternalAccessQuickConnectPermissionPlan{}, authenticationError(nas, err)
	}
	if !capabilities.QuickConnect || !capabilities.QuickConnectPermissionSet {
		return ExternalAccessQuickConnectPermissionPlan{}, fmt.Errorf("NAS %q does not expose a verified QuickConnect permission set backend", nas)
	}
	observed, err := client.ExternalAccessQuickConnectState(ctx)
	if err != nil {
		return ExternalAccessQuickConnectPermissionPlan{}, authenticationError(nas, err)
	}
	current := map[string]bool{}
	for _, service := range observed.Services {
		current[service.ID] = service.Enabled
	}
	summary := []string{}
	changed := false
	for _, service := range request.Services {
		enabled, known := current[service.ID]
		if !known {
			return ExternalAccessQuickConnectPermissionPlan{}, fmt.Errorf("QuickConnect service %q is not offered by this NAS", service.ID)
		}
		if enabled != service.Enabled {
			summary = append(summary, fmt.Sprintf("set QuickConnect service %q exposure to %t", service.ID, service.Enabled))
			changed = true
		}
	}
	if !changed {
		return ExternalAccessQuickConnectPermissionPlan{}, fmt.Errorf("QuickConnect permission patch would not change the current exposure")
	}
	plan := ExternalAccessQuickConnectPermissionPlan{APIVersion: externalAccessAPIVersion, NAS: nas, Request: request, Observed: observed, Risk: "high", Summary: summary}
	plan.Warnings = []string{"changing per-service exposure alters which services are reachable from the public internet through QuickConnect"}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return ExternalAccessQuickConnectPermissionPlan{}, err
	}
	plan.Hash, err = externalAccessQuickConnectPermissionPlanHash(plan)
	if err != nil {
		return ExternalAccessQuickConnectPermissionPlan{}, err
	}
	return plan, nil
}

func (s *Service) ApplyExternalAccessQuickConnectPermissionPlan(ctx context.Context, plan ExternalAccessQuickConnectPermissionPlan, approvalHash string) (ExternalAccessQuickConnectPermissionApplyResult, error) {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return ExternalAccessQuickConnectPermissionApplyResult{}, fmt.Errorf("approval hash does not match the QuickConnect permission plan")
	}
	if plan.APIVersion != externalAccessAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return ExternalAccessQuickConnectPermissionApplyResult{}, fmt.Errorf("invalid QuickConnect permission plan metadata")
	}
	if err := validateQuickConnectPermissionChange(plan.Request); err != nil {
		return ExternalAccessQuickConnectPermissionApplyResult{}, err
	}
	if expected, err := externalAccessQuickConnectPermissionPlanHash(plan); err != nil || expected != approvalHash {
		return ExternalAccessQuickConnectPermissionApplyResult{}, fmt.Errorf("QuickConnect permission plan contents were modified after planning")
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return ExternalAccessQuickConnectPermissionApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return ExternalAccessQuickConnectPermissionApplyResult{}, err
	}
	name, client, err := s.externalAccessWriteClient(ctx, plan.NAS)
	if err != nil {
		return ExternalAccessQuickConnectPermissionApplyResult{}, err
	}
	if name != plan.NAS {
		return ExternalAccessQuickConnectPermissionApplyResult{}, fmt.Errorf("QuickConnect permission plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	current, err := planQuickConnectPermissionWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return ExternalAccessQuickConnectPermissionApplyResult{}, fmt.Errorf("QuickConnect permission plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	if current.Hash, err = externalAccessQuickConnectPermissionPlanHash(current); err != nil {
		return ExternalAccessQuickConnectPermissionApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return ExternalAccessQuickConnectPermissionApplyResult{}, fmt.Errorf("QuickConnect permission plan is stale; create a new plan")
	}
	desired := mergeQuickConnectServices(current.Observed.Services, plan.Request.Services)
	result, err := client.ApplyExternalAccessQuickConnectPermissionChange(ctx, desired)
	if err != nil {
		return ExternalAccessQuickConnectPermissionApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := client.ExternalAccessQuickConnectState(ctx)
	if err != nil {
		return ExternalAccessQuickConnectPermissionApplyResult{}, fmt.Errorf("verify QuickConnect permission change: %w", err)
	}
	got := map[string]bool{}
	for _, service := range after.Services {
		got[service.ID] = service.Enabled
	}
	for _, service := range plan.Request.Services {
		if got[service.ID] != service.Enabled {
			return ExternalAccessQuickConnectPermissionApplyResult{}, fmt.Errorf("QuickConnect service %q exposure does not match the approved patch", service.ID)
		}
	}
	return ExternalAccessQuickConnectPermissionApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Result: result}, nil
}

// ---- DDNS record create / update / delete ----------------------------------

type ExternalAccessDDNSPlan struct {
	APIVersion          string                           `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                           `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                           `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             externalaccess.DDNSRecordChange  `json:"request" jsonschema:"Validated DDNS record intent; the password is a credential reference, never a value"`
	Observed            synology.ExternalAccessDDNSState `json:"observed" jsonschema:"Complete DDNS state observed during planning"`
	ObservedFingerprint string                           `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Risk                string                           `json:"risk" jsonschema:"Plan risk level (always high: publishes/removes a public hostname)"`
	Warnings            []string                         `json:"warnings" jsonschema:"External-exposure warnings"`
	Summary             []string                         `json:"summary" jsonschema:"Human-readable operations"`
	Hash                string                           `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type ExternalAccessDDNSApplyResult struct {
	NAS      string                                `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                                `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied  bool                                  `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Result   synology.ExternalAccessMutationResult `json:"result" jsonschema:"Selected DSM mutation backend"`
}

func externalAccessDDNSPlanHash(plan ExternalAccessDDNSPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func validateDDNSRecordChange(change externalaccess.DDNSRecordChange) error {
	switch change.Action {
	case externalaccess.DDNSActionCreate, externalaccess.DDNSActionUpdate, externalaccess.DDNSActionDelete:
	default:
		return fmt.Errorf("DDNS action must be create, set, or delete")
	}
	if strings.TrimSpace(change.Provider) == "" {
		return fmt.Errorf("DDNS provider is required")
	}
	if strings.TrimSpace(change.Hostname) == "" {
		return fmt.Errorf("DDNS hostname is required")
	}
	if change.PasswordRef != "" && !validSecretReference(change.PasswordRef) {
		return fmt.Errorf("password_ref must use env:NAME or vault:<id>")
	}
	if change.Action == externalaccess.DDNSActionCreate && change.PasswordRef == "" {
		return fmt.Errorf("creating a DDNS record requires password_ref for the provider account")
	}
	return nil
}

func (s *Service) PlanExternalAccessDDNSChange(ctx context.Context, requestedNAS string, request externalaccess.DDNSRecordChange) (ExternalAccessDDNSPlan, error) {
	if err := validateDDNSRecordChange(request); err != nil {
		return ExternalAccessDDNSPlan{}, err
	}
	name, client, err := s.externalAccessWriteClient(ctx, requestedNAS)
	if err != nil {
		return ExternalAccessDDNSPlan{}, err
	}
	plan, err := planDDNSWithClient(ctx, name, client, request)
	if err != nil {
		return ExternalAccessDDNSPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = externalAccessDDNSPlanHash(plan)
	}
	return plan, err
}

func ddnsRecordPresent(state synology.ExternalAccessDDNSState, hostname string) bool {
	for _, record := range state.Records {
		if record.Hostname == hostname {
			return true
		}
	}
	return false
}

func planDDNSWithClient(ctx context.Context, nas string, client externalAccessWriteClient, request externalaccess.DDNSRecordChange) (ExternalAccessDDNSPlan, error) {
	capabilities, _, err := client.ExternalAccessCapabilities(ctx)
	if err != nil {
		return ExternalAccessDDNSPlan{}, authenticationError(nas, err)
	}
	if !capabilities.DDNS || !capabilities.DDNSSet {
		return ExternalAccessDDNSPlan{}, fmt.Errorf("NAS %q does not expose a verified DDNS record set backend", nas)
	}
	observed, err := client.ExternalAccessDDNSState(ctx)
	if err != nil {
		return ExternalAccessDDNSPlan{}, authenticationError(nas, err)
	}
	present := ddnsRecordPresent(observed, request.Hostname)
	summary := []string{}
	switch request.Action {
	case externalaccess.DDNSActionCreate:
		if present {
			return ExternalAccessDDNSPlan{}, fmt.Errorf("DDNS record %q already exists; use set to update it", request.Hostname)
		}
		summary = append(summary, fmt.Sprintf("create DDNS record %q via provider %q", request.Hostname, request.Provider))
	case externalaccess.DDNSActionUpdate:
		if !present {
			return ExternalAccessDDNSPlan{}, fmt.Errorf("DDNS record %q does not exist; use create to add it", request.Hostname)
		}
		summary = append(summary, fmt.Sprintf("update DDNS record %q", request.Hostname))
	case externalaccess.DDNSActionDelete:
		if !present {
			return ExternalAccessDDNSPlan{}, fmt.Errorf("DDNS record %q does not exist", request.Hostname)
		}
		summary = append(summary, fmt.Sprintf("delete DDNS record %q", request.Hostname))
	}
	plan := ExternalAccessDDNSPlan{APIVersion: externalAccessAPIVersion, NAS: nas, Request: request, Observed: observed, Risk: "high", Summary: summary}
	if request.Action == externalaccess.DDNSActionDelete {
		plan.Warnings = []string{"deleting a DDNS record removes a public hostname; clients using it lose name resolution"}
	} else {
		plan.Warnings = []string{"creating or updating a DDNS record publishes a public hostname pointing at this NAS's WAN address"}
	}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return ExternalAccessDDNSPlan{}, err
	}
	plan.Hash, err = externalAccessDDNSPlanHash(plan)
	if err != nil {
		return ExternalAccessDDNSPlan{}, err
	}
	return plan, nil
}

func (s *Service) ApplyExternalAccessDDNSPlan(ctx context.Context, plan ExternalAccessDDNSPlan, approvalHash string) (ExternalAccessDDNSApplyResult, error) {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return ExternalAccessDDNSApplyResult{}, fmt.Errorf("approval hash does not match the DDNS plan")
	}
	if plan.APIVersion != externalAccessAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return ExternalAccessDDNSApplyResult{}, fmt.Errorf("invalid DDNS plan metadata")
	}
	if err := validateDDNSRecordChange(plan.Request); err != nil {
		return ExternalAccessDDNSApplyResult{}, err
	}
	if expected, err := externalAccessDDNSPlanHash(plan); err != nil || expected != approvalHash {
		return ExternalAccessDDNSApplyResult{}, fmt.Errorf("DDNS plan contents were modified after planning")
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return ExternalAccessDDNSApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return ExternalAccessDDNSApplyResult{}, err
	}
	name, client, err := s.externalAccessWriteClient(ctx, plan.NAS)
	if err != nil {
		return ExternalAccessDDNSApplyResult{}, err
	}
	if name != plan.NAS {
		return ExternalAccessDDNSApplyResult{}, fmt.Errorf("DDNS plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	current, err := planDDNSWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return ExternalAccessDDNSApplyResult{}, fmt.Errorf("DDNS plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	if current.Hash, err = externalAccessDDNSPlanHash(current); err != nil {
		return ExternalAccessDDNSApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return ExternalAccessDDNSApplyResult{}, fmt.Errorf("DDNS plan is stale; create a new plan")
	}
	password := ""
	if plan.Request.PasswordRef != "" {
		password, err = s.secretReferences.ResolveSecret(ctx, plan.Request.PasswordRef)
		if err != nil {
			return ExternalAccessDDNSApplyResult{}, fmt.Errorf("resolve DDNS password_ref: %w", err)
		}
	}
	result, err := client.ApplyExternalAccessDDNSChange(ctx, plan.Request, password)
	if err != nil {
		return ExternalAccessDDNSApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := client.ExternalAccessDDNSState(ctx)
	if err != nil {
		return ExternalAccessDDNSApplyResult{}, fmt.Errorf("verify DDNS change: %w", err)
	}
	present := ddnsRecordPresent(after, plan.Request.Hostname)
	if plan.Request.Action == externalaccess.DDNSActionDelete && present {
		return ExternalAccessDDNSApplyResult{}, fmt.Errorf("DDNS record %q still present after delete", plan.Request.Hostname)
	}
	if plan.Request.Action != externalaccess.DDNSActionDelete && !present {
		return ExternalAccessDDNSApplyResult{}, fmt.Errorf("DDNS record %q not present after %s", plan.Request.Hostname, plan.Request.Action)
	}
	return ExternalAccessDDNSApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Result: result}, nil
}

var _ externalAccessWriteClient = (*synology.Client)(nil)
