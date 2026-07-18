package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/tftpservice"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const tftpServiceAPIVersion = "dsmctl.io/v1alpha1"

type TFTPServiceStateResult struct {
	NAS         string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	TFTPService synology.TFTPServiceState `json:"tftp_service" jsonschema:"Normalized TFTP configuration"`
}

type TFTPServiceCapabilitiesResult struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.TFTPServiceCapabilities `json:"capabilities" jsonschema:"Selected TFTP operations"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected TFTP backend"`
}

type TFTPServicePlan struct {
	APIVersion          string                    `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                    `json:"nas" jsonschema:"NAS profile selected during planning"`
	Request             tftpservice.Change        `json:"request" jsonschema:"Patch-only TFTP intent"`
	Observed            synology.TFTPServiceState `json:"observed" jsonschema:"Complete TFTP state observed during planning"`
	ObservedFingerprint string                    `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Destructive         bool                      `json:"destructive" jsonschema:"Whether the plan disables TFTP"`
	Risk                string                    `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                  `json:"warnings" jsonschema:"Service exposure and disruption warnings"`
	Summary             []string                  `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                    `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type TFTPServiceApplyResult struct {
	NAS        string                               `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash   string                               `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied    bool                                 `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operations []synology.TFTPServiceMutationResult `json:"operations" jsonschema:"Selected DSM mutation backend"`
}

type tftpServiceClient interface {
	TFTPServiceState(context.Context) (synology.TFTPServiceState, error)
	TFTPServiceCapabilities(context.Context) (synology.TFTPServiceCapabilities, synology.CompatibilityReport, error)
	ApplyTFTPServiceChange(context.Context, tftpservice.Change) ([]synology.TFTPServiceMutationResult, error)
}

func (s *Service) GetTFTPServiceState(ctx context.Context, requestedNAS string) (TFTPServiceStateResult, error) {
	name, client, err := s.tftpServiceClient(ctx, requestedNAS)
	if err != nil {
		return TFTPServiceStateResult{}, err
	}
	state, err := client.TFTPServiceState(ctx)
	if err != nil {
		return TFTPServiceStateResult{}, authenticationError(name, err)
	}
	return TFTPServiceStateResult{NAS: name, TFTPService: state}, nil
}

func (s *Service) GetTFTPServiceCapabilities(ctx context.Context, requestedNAS string) (TFTPServiceCapabilitiesResult, error) {
	name, client, err := s.tftpServiceClient(ctx, requestedNAS)
	if err != nil {
		return TFTPServiceCapabilitiesResult{}, err
	}
	capabilities, report, err := client.TFTPServiceCapabilities(ctx)
	if err != nil {
		return TFTPServiceCapabilitiesResult{}, authenticationError(name, err)
	}
	return TFTPServiceCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) PlanTFTPServiceChange(ctx context.Context, requestedNAS string, request tftpservice.Change) (TFTPServicePlan, error) {
	if err := validateTFTPServiceChange(request); err != nil {
		return TFTPServicePlan{}, err
	}
	name, client, err := s.tftpServiceClient(ctx, requestedNAS)
	if err != nil {
		return TFTPServicePlan{}, err
	}
	return planTFTPServiceChangeWithClient(ctx, name, client, request)
}

func (s *Service) ApplyTFTPServicePlan(ctx context.Context, plan TFTPServicePlan, approvalHash string) (TFTPServiceApplyResult, error) {
	if err := validateTFTPServicePlan(plan, approvalHash); err != nil {
		return TFTPServiceApplyResult{}, err
	}
	name, client, err := s.tftpServiceClient(ctx, plan.NAS)
	if err != nil {
		return TFTPServiceApplyResult{}, err
	}
	if name != plan.NAS {
		return TFTPServiceApplyResult{}, fmt.Errorf("TFTP service plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyTFTPServicePlanWithClient(ctx, client, plan)
}

func applyTFTPServicePlanWithClient(ctx context.Context, client tftpServiceClient, plan TFTPServicePlan) (TFTPServiceApplyResult, error) {
	current, err := planTFTPServiceChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return TFTPServiceApplyResult{}, fmt.Errorf("TFTP service plan precondition no longer holds: %w", err)
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return TFTPServiceApplyResult{}, fmt.Errorf("TFTP service plan is stale; create a new plan")
	}
	operations, err := client.ApplyTFTPServiceChange(ctx, plan.Request)
	if err != nil {
		return TFTPServiceApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := client.TFTPServiceState(ctx)
	if err != nil {
		return TFTPServiceApplyResult{}, fmt.Errorf("verify TFTP service change: %w", err)
	}
	if !tftpServiceChangeMatches(after, plan.Request) {
		return TFTPServiceApplyResult{}, fmt.Errorf("TFTP service state does not match the approved patch")
	}
	return TFTPServiceApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operations: operations}, nil
}

func (s *Service) tftpServiceClient(ctx context.Context, requestedNAS string) (string, tftpServiceClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(tftpServiceClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement TFTP service management")
	}
	return name, client, nil
}

func planTFTPServiceChangeWithClient(ctx context.Context, nas string, client tftpServiceClient, request tftpservice.Change) (TFTPServicePlan, error) {
	capabilities, _, err := client.TFTPServiceCapabilities(ctx)
	if err != nil {
		return TFTPServicePlan{}, authenticationError(nas, err)
	}
	if !capabilities.Read {
		return TFTPServicePlan{}, fmt.Errorf("NAS %q does not expose a verified TFTP read backend", nas)
	}
	if !capabilities.Set {
		return TFTPServicePlan{}, fmt.Errorf("NAS %q does not expose a verified TFTP set backend", nas)
	}
	observed, err := client.TFTPServiceState(ctx)
	if err != nil {
		return TFTPServicePlan{}, authenticationError(nas, err)
	}
	if tftpServiceChangeMatches(observed, request) {
		return TFTPServicePlan{}, fmt.Errorf("TFTP patch would not change the current state")
	}
	// DSM rejects enabling TFTP without a valid root folder; catch it early.
	if request.Enabled != nil && *request.Enabled {
		effectiveRoot := observed.RootPath
		if request.RootPath != nil {
			effectiveRoot = *request.RootPath
		}
		if strings.TrimSpace(effectiveRoot) == "" {
			return TFTPServicePlan{}, fmt.Errorf("enabling TFTP requires a root folder; set root_path in the same patch")
		}
	}
	plan := TFTPServicePlan{APIVersion: tftpServiceAPIVersion, NAS: nas, Request: request, Observed: observed}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return TFTPServicePlan{}, err
	}
	plan.Destructive, plan.Warnings, plan.Summary = tftpServicePlanEffects(observed, request)
	if plan.Destructive || len(plan.Warnings) > 0 {
		plan.Risk = "high"
	} else {
		plan.Risk = "medium"
	}
	plan.Hash, err = tftpServicePlanHash(plan)
	if err != nil {
		return TFTPServicePlan{}, err
	}
	return plan, nil
}

func validateTFTPServiceChange(change tftpservice.Change) error {
	if change.Enabled == nil && change.RootPath == nil && change.Permission == nil && change.LogEnabled == nil && change.Timeout == nil {
		return fmt.Errorf("TFTP patch has no fields")
	}
	if change.Permission != nil {
		switch *change.Permission {
		case tftpservice.PermissionReadOnly, tftpservice.PermissionReadWrite:
		default:
			return fmt.Errorf("TFTP permission %q is not read_only or read_write", *change.Permission)
		}
	}
	if change.RootPath != nil && strings.TrimSpace(*change.RootPath) == "" {
		return fmt.Errorf("TFTP root_path cannot be empty")
	}
	if change.Timeout != nil && (*change.Timeout < 1 || *change.Timeout > 3600) {
		return fmt.Errorf("TFTP timeout %d is out of range 1-3600", *change.Timeout)
	}
	return nil
}

func tftpServicePlanEffects(observed synology.TFTPServiceState, change tftpservice.Change) (bool, []string, []string) {
	warnings := []string{}
	summary := []string{}
	destructive := false
	if change.Enabled != nil {
		summary = append(summary, fmt.Sprintf("set TFTP service to %t", *change.Enabled))
		if *change.Enabled && !observed.Enabled {
			warnings = append(warnings, "enabling TFTP exposes an unauthenticated file-transfer service on the local network")
		} else if !*change.Enabled && observed.Enabled {
			destructive = true
			warnings = append(warnings, "disabling TFTP stops all TFTP clients")
		}
	}
	if change.RootPath != nil && *change.RootPath != observed.RootPath {
		summary = append(summary, fmt.Sprintf("set TFTP root folder to %q", *change.RootPath))
	}
	if change.Permission != nil {
		summary = append(summary, fmt.Sprintf("set TFTP permission to %s", *change.Permission))
		if *change.Permission == tftpservice.PermissionReadWrite && observed.Permission != tftpservice.PermissionReadWrite {
			warnings = append(warnings, "granting TFTP write access lets unauthenticated clients upload files")
		}
	}
	if change.LogEnabled != nil {
		summary = append(summary, fmt.Sprintf("set TFTP logging to %t", *change.LogEnabled))
	}
	if change.Timeout != nil {
		summary = append(summary, fmt.Sprintf("set TFTP timeout to %d", *change.Timeout))
	}
	return destructive, warnings, summary
}

func tftpServiceChangeMatches(state synology.TFTPServiceState, change tftpservice.Change) bool {
	return (change.Enabled == nil || state.Enabled == *change.Enabled) &&
		(change.RootPath == nil || state.RootPath == *change.RootPath) &&
		(change.Permission == nil || state.Permission == *change.Permission) &&
		(change.LogEnabled == nil || state.LogEnabled == *change.LogEnabled) &&
		(change.Timeout == nil || state.Timeout == *change.Timeout)
}

func validateTFTPServicePlan(plan TFTPServicePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the TFTP service plan")
	}
	if plan.APIVersion != tftpServiceAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid TFTP service plan metadata")
	}
	if err := validateTFTPServiceChange(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("TFTP service plan observed state was modified")
	}
	expectedHash, err := tftpServicePlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("TFTP service plan contents were modified after planning")
	}
	return nil
}

func tftpServicePlanHash(plan TFTPServicePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

var _ tftpServiceClient = (*synology.Client)(nil)
