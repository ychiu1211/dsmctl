package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/servicediscovery"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const serviceDiscoveryAPIVersion = "dsmctl.io/v1alpha1"

type ServiceDiscoveryStateResult struct {
	NAS              string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	ServiceDiscovery synology.ServiceDiscoveryState `json:"service_discovery" jsonschema:"Normalized service-discovery configuration"`
}

type ServiceDiscoveryCapabilitiesResult struct {
	NAS          string                                `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.ServiceDiscoveryCapabilities `json:"capabilities" jsonschema:"Selected Time Machine and WS-Discovery operations"`
	Report       synology.CompatibilityReport          `json:"report" jsonschema:"Discovered APIs and selected service-discovery backends"`
}

type ServiceDiscoveryPlan struct {
	APIVersion          string                         `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                         `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                         `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             servicediscovery.Change        `json:"request" jsonschema:"Patch-only service-discovery intent"`
	Observed            synology.ServiceDiscoveryState `json:"observed" jsonschema:"Complete service-discovery state observed during planning"`
	ObservedFingerprint string                         `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Destructive         bool                           `json:"destructive" jsonschema:"Whether the plan disables a discovery advertisement"`
	Risk                string                         `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                       `json:"warnings" jsonschema:"Discovery disruption and network-exposure warnings"`
	Summary             []string                       `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                         `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type ServiceDiscoveryApplyResult struct {
	NAS        string                                    `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash   string                                    `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied    bool                                      `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operations []synology.ServiceDiscoveryMutationResult `json:"operations" jsonschema:"Selected DSM mutation backends, one per changed area"`
}

type serviceDiscoveryClient interface {
	ServiceDiscoveryState(context.Context) (synology.ServiceDiscoveryState, error)
	ServiceDiscoveryCapabilities(context.Context) (synology.ServiceDiscoveryCapabilities, synology.CompatibilityReport, error)
	ApplyServiceDiscoveryChange(context.Context, servicediscovery.Change) ([]synology.ServiceDiscoveryMutationResult, error)
}

func (s *Service) GetServiceDiscoveryState(ctx context.Context, requestedNAS string) (ServiceDiscoveryStateResult, error) {
	name, client, err := s.serviceDiscoveryClient(ctx, requestedNAS)
	if err != nil {
		return ServiceDiscoveryStateResult{}, err
	}
	state, err := client.ServiceDiscoveryState(ctx)
	if err != nil {
		return ServiceDiscoveryStateResult{}, authenticationError(name, err)
	}
	return ServiceDiscoveryStateResult{NAS: name, ServiceDiscovery: state}, nil
}

func (s *Service) GetServiceDiscoveryCapabilities(ctx context.Context, requestedNAS string) (ServiceDiscoveryCapabilitiesResult, error) {
	name, client, err := s.serviceDiscoveryClient(ctx, requestedNAS)
	if err != nil {
		return ServiceDiscoveryCapabilitiesResult{}, err
	}
	capabilities, report, err := client.ServiceDiscoveryCapabilities(ctx)
	if err != nil {
		return ServiceDiscoveryCapabilitiesResult{}, authenticationError(name, err)
	}
	return ServiceDiscoveryCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) PlanServiceDiscoveryChange(ctx context.Context, requestedNAS string, request servicediscovery.Change) (ServiceDiscoveryPlan, error) {
	if err := validateServiceDiscoveryChange(request); err != nil {
		return ServiceDiscoveryPlan{}, err
	}
	name, client, err := s.serviceDiscoveryClient(ctx, requestedNAS)
	if err != nil {
		return ServiceDiscoveryPlan{}, err
	}
	plan, err := planServiceDiscoveryChangeWithClient(ctx, name, client, request)
	if err != nil {
		return ServiceDiscoveryPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = serviceDiscoveryPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyServiceDiscoveryPlan(ctx context.Context, plan ServiceDiscoveryPlan, approvalHash string) (ServiceDiscoveryApplyResult, error) {
	if err := validateServiceDiscoveryPlan(plan, approvalHash); err != nil {
		return ServiceDiscoveryApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return ServiceDiscoveryApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return ServiceDiscoveryApplyResult{}, err
	}
	name, client, err := s.serviceDiscoveryClient(ctx, plan.NAS)
	if err != nil {
		return ServiceDiscoveryApplyResult{}, err
	}
	if name != plan.NAS {
		return ServiceDiscoveryApplyResult{}, fmt.Errorf("service discovery plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyServiceDiscoveryPlanWithClient(ctx, client, plan)
}

func applyServiceDiscoveryPlanWithClient(ctx context.Context, client serviceDiscoveryClient, plan ServiceDiscoveryPlan) (ServiceDiscoveryApplyResult, error) {
	current, err := planServiceDiscoveryChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return ServiceDiscoveryApplyResult{}, fmt.Errorf("service discovery plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = serviceDiscoveryPlanHash(current)
	if err != nil {
		return ServiceDiscoveryApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return ServiceDiscoveryApplyResult{}, fmt.Errorf("service discovery plan is stale; create a new plan")
	}
	operations, err := client.ApplyServiceDiscoveryChange(ctx, plan.Request)
	if err != nil {
		return ServiceDiscoveryApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := client.ServiceDiscoveryState(ctx)
	if err != nil {
		return ServiceDiscoveryApplyResult{}, fmt.Errorf("verify service discovery change: %w", err)
	}
	if !serviceDiscoveryChangeMatches(after, plan.Request) {
		return ServiceDiscoveryApplyResult{}, fmt.Errorf("service discovery state does not match the approved patch")
	}
	return ServiceDiscoveryApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operations: operations}, nil
}

func (s *Service) serviceDiscoveryClient(ctx context.Context, requestedNAS string) (string, serviceDiscoveryClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(serviceDiscoveryClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement service discovery management")
	}
	return name, client, nil
}

func planServiceDiscoveryChangeWithClient(ctx context.Context, nas string, client serviceDiscoveryClient, request servicediscovery.Change) (ServiceDiscoveryPlan, error) {
	capabilities, _, err := client.ServiceDiscoveryCapabilities(ctx)
	if err != nil {
		return ServiceDiscoveryPlan{}, authenticationError(nas, err)
	}
	timeMachineChange := request.SMBTimeMachine != nil || request.AFPTimeMachine != nil
	wsChange := request.WSDiscovery != nil
	if !capabilities.Read {
		return ServiceDiscoveryPlan{}, fmt.Errorf("NAS %q does not expose a verified service discovery read backend", nas)
	}
	if timeMachineChange && !capabilities.Set {
		return ServiceDiscoveryPlan{}, fmt.Errorf("NAS %q does not expose a verified Time Machine advertising set backend", nas)
	}
	if wsChange && !capabilities.WSDiscovery {
		return ServiceDiscoveryPlan{}, fmt.Errorf("NAS %q does not expose a verified WS-Discovery set backend", nas)
	}
	observed, err := client.ServiceDiscoveryState(ctx)
	if err != nil {
		return ServiceDiscoveryPlan{}, authenticationError(nas, err)
	}
	if serviceDiscoveryChangeMatches(observed, request) {
		return ServiceDiscoveryPlan{}, fmt.Errorf("service discovery patch would not change the current state")
	}
	plan := ServiceDiscoveryPlan{APIVersion: serviceDiscoveryAPIVersion, NAS: nas, Request: request, Observed: observed}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return ServiceDiscoveryPlan{}, err
	}
	plan.Destructive, plan.Warnings, plan.Summary = serviceDiscoveryPlanEffects(observed, request)
	if plan.Destructive || len(plan.Warnings) > 0 {
		plan.Risk = "high"
	} else {
		plan.Risk = "medium"
	}
	plan.Hash, err = serviceDiscoveryPlanHash(plan)
	if err != nil {
		return ServiceDiscoveryPlan{}, err
	}
	return plan, nil
}

func validateServiceDiscoveryChange(change servicediscovery.Change) error {
	if change.SMBTimeMachine == nil && change.AFPTimeMachine == nil && change.WSDiscovery == nil {
		return fmt.Errorf("service discovery patch has no fields")
	}
	return nil
}

func serviceDiscoveryPlanEffects(observed synology.ServiceDiscoveryState, change servicediscovery.Change) (bool, []string, []string) {
	warnings := []string{}
	summary := []string{}
	if change.SMBTimeMachine != nil {
		summary = append(summary, fmt.Sprintf("set SMB Time Machine advertising to %t", *change.SMBTimeMachine))
		if observed.SMBTimeMachine && !*change.SMBTimeMachine {
			warnings = append(warnings, "disabling SMB Time Machine advertising stops Macs from discovering this NAS for Time Machine")
		}
	}
	if change.AFPTimeMachine != nil {
		summary = append(summary, fmt.Sprintf("set AFP Time Machine advertising to %t", *change.AFPTimeMachine))
		if observed.AFPTimeMachine && !*change.AFPTimeMachine {
			warnings = append(warnings, "disabling AFP Time Machine advertising stops Macs from discovering this NAS over AFP")
		}
	}
	if change.WSDiscovery != nil {
		summary = append(summary, fmt.Sprintf("set WS-Discovery to %t", *change.WSDiscovery))
		if *change.WSDiscovery {
			warnings = append(warnings, "enabling WS-Discovery advertises this NAS to Windows clients on the local network")
		} else if observed.WSDiscovery != nil && *observed.WSDiscovery {
			warnings = append(warnings, "disabling WS-Discovery hides this NAS from Windows network discovery")
		}
	}
	return false, warnings, summary
}

func serviceDiscoveryChangeMatches(state synology.ServiceDiscoveryState, change servicediscovery.Change) bool {
	return (change.SMBTimeMachine == nil || state.SMBTimeMachine == *change.SMBTimeMachine) &&
		(change.AFPTimeMachine == nil || state.AFPTimeMachine == *change.AFPTimeMachine) &&
		(change.WSDiscovery == nil || (state.WSDiscovery != nil && *state.WSDiscovery == *change.WSDiscovery))
}

func validateServiceDiscoveryPlan(plan ServiceDiscoveryPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the service discovery plan")
	}
	if plan.APIVersion != serviceDiscoveryAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid service discovery plan metadata")
	}
	if err := validateServiceDiscoveryChange(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("service discovery plan observed state was modified")
	}
	expectedHash, err := serviceDiscoveryPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("service discovery plan contents were modified after planning")
	}
	return nil
}

func serviceDiscoveryPlanHash(plan ServiceDiscoveryPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

var _ serviceDiscoveryClient = (*synology.Client)(nil)
