package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/surveillance"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const surveillanceAPIVersion = "dsmctl.io/v1alpha1"

type SurveillanceCapabilitiesResult struct {
	NAS          string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.SurveillanceCapabilities `json:"capabilities" jsonschema:"Surveillance operations exposed by dsmctl, with installed-package evidence"`
	Report       synology.CompatibilityReport      `json:"report" jsonschema:"Discovered APIs, installed packages, and selected Surveillance backends"`
}

type SurveillanceInfoResult struct {
	NAS  string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	Info synology.SurveillanceInfo `json:"info" jsonschema:"Normalized Surveillance Station system information"`
}

type SurveillanceCamerasResult struct {
	NAS     string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Cameras synology.SurveillanceCameras `json:"cameras" jsonschema:"Configured cameras reported by Surveillance Station"`
}

type surveillanceClient interface {
	SurveillanceInfo(context.Context) (synology.SurveillanceInfo, error)
	SurveillanceCameras(context.Context) (synology.SurveillanceCameras, error)
	SurveillanceCapabilities(context.Context) (synology.SurveillanceCapabilities, synology.CompatibilityReport, error)
	SurveillanceHomeMode(context.Context) (synology.SurveillanceHomeMode, error)
	ApplySurveillanceHomeModeChange(context.Context, surveillance.HomeModeChange) (synology.SurveillanceHomeModeMutationResult, error)
}

type SurveillanceHomeModeResult struct {
	NAS      string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	HomeMode synology.SurveillanceHomeMode `json:"home_mode" jsonschema:"Surveillance Station Home Mode state"`
}

type SurveillanceHomeModePlan struct {
	APIVersion          string                        `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                        `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                        `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             surveillance.HomeModeChange   `json:"request" jsonschema:"Patch-only Home Mode intent"`
	Observed            synology.SurveillanceHomeMode `json:"observed" jsonschema:"Home Mode state observed during planning"`
	ObservedFingerprint string                        `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the observed state"`
	Risk                string                        `json:"risk" jsonschema:"Plan risk level"`
	Warnings            []string                      `json:"warnings" jsonschema:"Recording/notification-profile warnings"`
	Summary             []string                      `json:"summary" jsonschema:"Human-readable operations"`
	Hash                string                        `json:"hash" jsonschema:"SHA-256 approval hash covering intent and observed state"`
}

type SurveillanceHomeModeApplyResult struct {
	NAS      string                                      `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                                      `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied  bool                                        `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Result   synology.SurveillanceHomeModeMutationResult `json:"result" jsonschema:"Selected DSM mutation backend"`
}

func (s *Service) GetSurveillanceCapabilities(ctx context.Context, requestedNAS string) (SurveillanceCapabilitiesResult, error) {
	name, client, err := s.surveillanceClient(ctx, requestedNAS)
	if err != nil {
		return SurveillanceCapabilitiesResult{}, err
	}
	capabilities, report, err := client.SurveillanceCapabilities(ctx)
	if err != nil {
		return SurveillanceCapabilitiesResult{}, authenticationError(name, err)
	}
	return SurveillanceCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetSurveillanceInfo(ctx context.Context, requestedNAS string) (SurveillanceInfoResult, error) {
	name, client, err := s.surveillanceClient(ctx, requestedNAS)
	if err != nil {
		return SurveillanceInfoResult{}, err
	}
	info, err := client.SurveillanceInfo(ctx)
	if err != nil {
		return SurveillanceInfoResult{}, authenticationError(name, err)
	}
	return SurveillanceInfoResult{NAS: name, Info: info}, nil
}

func (s *Service) GetSurveillanceCameras(ctx context.Context, requestedNAS string) (SurveillanceCamerasResult, error) {
	name, client, err := s.surveillanceClient(ctx, requestedNAS)
	if err != nil {
		return SurveillanceCamerasResult{}, err
	}
	cameras, err := client.SurveillanceCameras(ctx)
	if err != nil {
		return SurveillanceCamerasResult{}, authenticationError(name, err)
	}
	return SurveillanceCamerasResult{NAS: name, Cameras: cameras}, nil
}

func (s *Service) GetSurveillanceHomeMode(ctx context.Context, requestedNAS string) (SurveillanceHomeModeResult, error) {
	name, client, err := s.surveillanceClient(ctx, requestedNAS)
	if err != nil {
		return SurveillanceHomeModeResult{}, err
	}
	mode, err := client.SurveillanceHomeMode(ctx)
	if err != nil {
		return SurveillanceHomeModeResult{}, authenticationError(name, err)
	}
	return SurveillanceHomeModeResult{NAS: name, HomeMode: mode}, nil
}

func (s *Service) PlanSurveillanceHomeModeChange(ctx context.Context, requestedNAS string, request surveillance.HomeModeChange) (SurveillanceHomeModePlan, error) {
	if request.On == nil {
		return SurveillanceHomeModePlan{}, fmt.Errorf("home mode patch has no fields")
	}
	name, client, err := s.surveillanceClient(ctx, requestedNAS)
	if err != nil {
		return SurveillanceHomeModePlan{}, err
	}
	plan, err := planSurveillanceHomeModeWithClient(ctx, name, client, request)
	if err != nil {
		return SurveillanceHomeModePlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = surveillanceHomeModePlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplySurveillanceHomeModePlan(ctx context.Context, plan SurveillanceHomeModePlan, approvalHash string) (SurveillanceHomeModeApplyResult, error) {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return SurveillanceHomeModeApplyResult{}, fmt.Errorf("approval hash does not match the home mode plan")
	}
	if plan.APIVersion != surveillanceAPIVersion || strings.TrimSpace(plan.NAS) == "" || plan.Request.On == nil {
		return SurveillanceHomeModeApplyResult{}, fmt.Errorf("invalid home mode plan metadata")
	}
	expectedHash, err := surveillanceHomeModePlanHash(plan)
	if err != nil {
		return SurveillanceHomeModeApplyResult{}, err
	}
	if expectedHash != plan.Hash {
		return SurveillanceHomeModeApplyResult{}, fmt.Errorf("home mode plan contents were modified after planning")
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return SurveillanceHomeModeApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return SurveillanceHomeModeApplyResult{}, err
	}
	name, client, err := s.surveillanceClient(ctx, plan.NAS)
	if err != nil {
		return SurveillanceHomeModeApplyResult{}, err
	}
	if name != plan.NAS {
		return SurveillanceHomeModeApplyResult{}, fmt.Errorf("home mode plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	current, err := planSurveillanceHomeModeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return SurveillanceHomeModeApplyResult{}, fmt.Errorf("home mode plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = surveillanceHomeModePlanHash(current)
	if err != nil {
		return SurveillanceHomeModeApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return SurveillanceHomeModeApplyResult{}, fmt.Errorf("home mode plan is stale; create a new plan")
	}
	result, err := client.ApplySurveillanceHomeModeChange(ctx, plan.Request)
	if err != nil {
		return SurveillanceHomeModeApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := client.SurveillanceHomeMode(ctx)
	if err != nil {
		return SurveillanceHomeModeApplyResult{}, fmt.Errorf("verify home mode change: %w", err)
	}
	if plan.Request.On != nil && after.On != *plan.Request.On {
		return SurveillanceHomeModeApplyResult{}, fmt.Errorf("home mode state does not match the approved patch")
	}
	return SurveillanceHomeModeApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Result: result}, nil
}

func planSurveillanceHomeModeWithClient(ctx context.Context, nas string, client surveillanceClient, request surveillance.HomeModeChange) (SurveillanceHomeModePlan, error) {
	capabilities, _, err := client.SurveillanceCapabilities(ctx)
	if err != nil {
		return SurveillanceHomeModePlan{}, authenticationError(nas, err)
	}
	if !capabilities.HomeModeRead {
		return SurveillanceHomeModePlan{}, fmt.Errorf("NAS %q does not expose a verified Surveillance home mode read backend", nas)
	}
	if !capabilities.HomeModeSet {
		return SurveillanceHomeModePlan{}, fmt.Errorf("NAS %q does not expose a verified Surveillance home mode set backend", nas)
	}
	observed, err := client.SurveillanceHomeMode(ctx)
	if err != nil {
		return SurveillanceHomeModePlan{}, authenticationError(nas, err)
	}
	if request.On != nil && observed.On == *request.On {
		return SurveillanceHomeModePlan{}, fmt.Errorf("home mode patch would not change the current state")
	}
	plan := SurveillanceHomeModePlan{APIVersion: surveillanceAPIVersion, NAS: nas, Request: request, Observed: observed, Risk: "medium"}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return SurveillanceHomeModePlan{}, err
	}
	plan.Summary = []string{fmt.Sprintf("switch Home Mode to %t", *request.On)}
	plan.Warnings = []string{"switching Home Mode changes the active recording and notification profile"}
	plan.Hash, err = surveillanceHomeModePlanHash(plan)
	if err != nil {
		return SurveillanceHomeModePlan{}, err
	}
	return plan, nil
}

func surveillanceHomeModePlanHash(plan SurveillanceHomeModePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func (s *Service) surveillanceClient(ctx context.Context, requestedNAS string) (string, surveillanceClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(surveillanceClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement Surveillance management")
	}
	return name, client, nil
}

var _ surveillanceClient = (*synology.Client)(nil)
