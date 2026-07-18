package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/rsyncservice"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const rsyncServiceAPIVersion = "dsmctl.io/v1alpha1"

type RsyncServiceStateResult struct {
	NAS          string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	RsyncService synology.RsyncServiceState `json:"rsync_service" jsonschema:"Normalized rsync-service configuration"`
}

type RsyncServiceCapabilitiesResult struct {
	NAS          string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.RsyncServiceCapabilities `json:"capabilities" jsonschema:"Selected rsync-service operations"`
	Report       synology.CompatibilityReport      `json:"report" jsonschema:"Discovered APIs and selected rsync-service backend"`
}

type RsyncServicePlan struct {
	APIVersion          string                     `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                     `json:"nas" jsonschema:"NAS profile selected during planning"`
	Request             rsyncservice.Change        `json:"request" jsonschema:"Patch-only rsync-service intent"`
	Observed            synology.RsyncServiceState `json:"observed" jsonschema:"Complete rsync-service state observed during planning"`
	ObservedFingerprint string                     `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Destructive         bool                       `json:"destructive" jsonschema:"Whether the plan disables the rsync service"`
	Risk                string                     `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                   `json:"warnings" jsonschema:"Service exposure and disruption warnings"`
	Summary             []string                   `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                     `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type RsyncServiceApplyResult struct {
	NAS        string                                `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash   string                                `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied    bool                                  `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operations []synology.RsyncServiceMutationResult `json:"operations" jsonschema:"Selected DSM mutation backend"`
}

type rsyncServiceClient interface {
	RsyncServiceState(context.Context) (synology.RsyncServiceState, error)
	RsyncServiceCapabilities(context.Context) (synology.RsyncServiceCapabilities, synology.CompatibilityReport, error)
	ApplyRsyncServiceChange(context.Context, rsyncservice.Change) ([]synology.RsyncServiceMutationResult, error)
}

func (s *Service) GetRsyncServiceState(ctx context.Context, requestedNAS string) (RsyncServiceStateResult, error) {
	name, client, err := s.rsyncServiceClient(ctx, requestedNAS)
	if err != nil {
		return RsyncServiceStateResult{}, err
	}
	state, err := client.RsyncServiceState(ctx)
	if err != nil {
		return RsyncServiceStateResult{}, authenticationError(name, err)
	}
	return RsyncServiceStateResult{NAS: name, RsyncService: state}, nil
}

func (s *Service) GetRsyncServiceCapabilities(ctx context.Context, requestedNAS string) (RsyncServiceCapabilitiesResult, error) {
	name, client, err := s.rsyncServiceClient(ctx, requestedNAS)
	if err != nil {
		return RsyncServiceCapabilitiesResult{}, err
	}
	capabilities, report, err := client.RsyncServiceCapabilities(ctx)
	if err != nil {
		return RsyncServiceCapabilitiesResult{}, authenticationError(name, err)
	}
	return RsyncServiceCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) PlanRsyncServiceChange(ctx context.Context, requestedNAS string, request rsyncservice.Change) (RsyncServicePlan, error) {
	if err := validateRsyncServiceChange(request); err != nil {
		return RsyncServicePlan{}, err
	}
	name, client, err := s.rsyncServiceClient(ctx, requestedNAS)
	if err != nil {
		return RsyncServicePlan{}, err
	}
	return planRsyncServiceChangeWithClient(ctx, name, client, request)
}

func (s *Service) ApplyRsyncServicePlan(ctx context.Context, plan RsyncServicePlan, approvalHash string) (RsyncServiceApplyResult, error) {
	if err := validateRsyncServicePlan(plan, approvalHash); err != nil {
		return RsyncServiceApplyResult{}, err
	}
	name, client, err := s.rsyncServiceClient(ctx, plan.NAS)
	if err != nil {
		return RsyncServiceApplyResult{}, err
	}
	if name != plan.NAS {
		return RsyncServiceApplyResult{}, fmt.Errorf("rsync service plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyRsyncServicePlanWithClient(ctx, client, plan)
}

func applyRsyncServicePlanWithClient(ctx context.Context, client rsyncServiceClient, plan RsyncServicePlan) (RsyncServiceApplyResult, error) {
	current, err := planRsyncServiceChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return RsyncServiceApplyResult{}, fmt.Errorf("rsync service plan precondition no longer holds: %w", err)
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return RsyncServiceApplyResult{}, fmt.Errorf("rsync service plan is stale; create a new plan")
	}
	operations, err := client.ApplyRsyncServiceChange(ctx, plan.Request)
	if err != nil {
		return RsyncServiceApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := client.RsyncServiceState(ctx)
	if err != nil {
		return RsyncServiceApplyResult{}, fmt.Errorf("verify rsync service change: %w", err)
	}
	if !rsyncServiceChangeMatches(after, plan.Request) {
		return RsyncServiceApplyResult{}, fmt.Errorf("rsync service state does not match the approved patch")
	}
	return RsyncServiceApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operations: operations}, nil
}

func (s *Service) rsyncServiceClient(ctx context.Context, requestedNAS string) (string, rsyncServiceClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(rsyncServiceClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement rsync service management")
	}
	return name, client, nil
}

func planRsyncServiceChangeWithClient(ctx context.Context, nas string, client rsyncServiceClient, request rsyncservice.Change) (RsyncServicePlan, error) {
	capabilities, _, err := client.RsyncServiceCapabilities(ctx)
	if err != nil {
		return RsyncServicePlan{}, authenticationError(nas, err)
	}
	if !capabilities.Read {
		return RsyncServicePlan{}, fmt.Errorf("NAS %q does not expose a verified rsync service read backend", nas)
	}
	if !capabilities.Set {
		return RsyncServicePlan{}, fmt.Errorf("NAS %q does not expose a verified rsync service set backend", nas)
	}
	observed, err := client.RsyncServiceState(ctx)
	if err != nil {
		return RsyncServicePlan{}, authenticationError(nas, err)
	}
	if rsyncServiceChangeMatches(observed, request) {
		return RsyncServicePlan{}, fmt.Errorf("rsync service patch would not change the current state")
	}
	plan := RsyncServicePlan{APIVersion: rsyncServiceAPIVersion, NAS: nas, Request: request, Observed: observed}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return RsyncServicePlan{}, err
	}
	plan.Destructive, plan.Warnings, plan.Summary = rsyncServicePlanEffects(observed, request)
	if plan.Destructive || len(plan.Warnings) > 0 {
		plan.Risk = "high"
	} else {
		plan.Risk = "medium"
	}
	plan.Hash, err = rsyncServicePlanHash(plan)
	if err != nil {
		return RsyncServicePlan{}, err
	}
	return plan, nil
}

func validateRsyncServiceChange(change rsyncservice.Change) error {
	if change.Enabled == nil && change.RsyncAccount == nil {
		return fmt.Errorf("rsync service patch has no fields")
	}
	return nil
}

func rsyncServicePlanEffects(observed synology.RsyncServiceState, change rsyncservice.Change) (bool, []string, []string) {
	warnings := []string{}
	summary := []string{}
	destructive := false
	if change.Enabled != nil {
		summary = append(summary, fmt.Sprintf("set rsync service to %t", *change.Enabled))
		if *change.Enabled && !observed.Enabled {
			warnings = append(warnings, "enabling the rsync service exposes an rsync network-backup endpoint on this NAS")
		} else if !*change.Enabled && observed.Enabled {
			destructive = true
			warnings = append(warnings, "disabling the rsync service stops incoming rsync network backups")
		}
	}
	if change.RsyncAccount != nil {
		summary = append(summary, fmt.Sprintf("set rsync account to %t", *change.RsyncAccount))
		if *change.RsyncAccount && !observed.RsyncAccount {
			warnings = append(warnings, "enabling the rsync account allows authentication as the dedicated rsync user")
		}
	}
	return destructive, warnings, summary
}

func rsyncServiceChangeMatches(state synology.RsyncServiceState, change rsyncservice.Change) bool {
	return (change.Enabled == nil || state.Enabled == *change.Enabled) &&
		(change.RsyncAccount == nil || state.RsyncAccount == *change.RsyncAccount)
}

func validateRsyncServicePlan(plan RsyncServicePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the rsync service plan")
	}
	if plan.APIVersion != rsyncServiceAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid rsync service plan metadata")
	}
	if err := validateRsyncServiceChange(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("rsync service plan observed state was modified")
	}
	expectedHash, err := rsyncServicePlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("rsync service plan contents were modified after planning")
	}
	return nil
}

func rsyncServicePlanHash(plan RsyncServicePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

var _ rsyncServiceClient = (*synology.Client)(nil)
