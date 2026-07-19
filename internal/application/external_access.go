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
