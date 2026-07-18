package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/driveadmin"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

// Drive Admin log paging bounds mirror the DSM system log module.
const (
	driveAdminDefaultLogLimit = 100
	driveAdminMaxLogLimit     = 1000
)

type DriveAdminCapabilitiesResult struct {
	NAS          string                          `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.DriveAdminCapabilities `json:"capabilities" jsonschema:"Drive Admin operations currently exposed by dsmctl, with installed-package evidence"`
	Report       synology.CompatibilityReport    `json:"report" jsonschema:"Discovered APIs, installed packages, and selected Drive Admin backends"`
}

type DriveAdminStatusResult struct {
	NAS    string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	Status synology.DriveAdminStatus `json:"status" jsonschema:"Normalized Drive service status with installed-package evidence"`
}

type DriveAdminConnectionsResult struct {
	NAS         string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Connections synology.DriveAdminConnections `json:"connections" jsonschema:"Active Drive client connections"`
}

type DriveAdminTeamFoldersResult struct {
	NAS         string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	TeamFolders synology.DriveAdminTeamFolders `json:"team_folders" jsonschema:"Drive team folders from the admin perspective"`
}

type DriveAdminLogResult struct {
	NAS string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Log synology.DriveAdminLog `json:"log" jsonschema:"Drive server log entries for the requested page"`
}

type driveAdminClient interface {
	DriveAdminStatus(context.Context) (synology.DriveAdminStatus, error)
	DriveAdminConnections(context.Context) (synology.DriveAdminConnections, error)
	DriveAdminTeamFolders(context.Context) (synology.DriveAdminTeamFolders, error)
	DriveAdminLog(context.Context, synology.DriveAdminLogQuery) (synology.DriveAdminLog, error)
	DriveAdminCapabilities(context.Context) (synology.DriveAdminCapabilities, synology.CompatibilityReport, error)
	DriveServerConfig(context.Context) (synology.DriveServerConfig, error)
	ApplyDriveServerConfigChange(context.Context, driveadmin.ServerConfigChange) (synology.DriveConfigMutationResult, error)
}

func (s *Service) GetDriveAdminCapabilities(ctx context.Context, requestedNAS string) (DriveAdminCapabilitiesResult, error) {
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveAdminCapabilitiesResult{}, err
	}
	capabilities, report, err := client.DriveAdminCapabilities(ctx)
	if err != nil {
		return DriveAdminCapabilitiesResult{}, authenticationError(name, err)
	}
	return DriveAdminCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetDriveAdminStatus(ctx context.Context, requestedNAS string) (DriveAdminStatusResult, error) {
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveAdminStatusResult{}, err
	}
	status, err := client.DriveAdminStatus(ctx)
	if err != nil {
		return DriveAdminStatusResult{}, authenticationError(name, err)
	}
	return DriveAdminStatusResult{NAS: name, Status: status}, nil
}

func (s *Service) GetDriveAdminConnections(ctx context.Context, requestedNAS string) (DriveAdminConnectionsResult, error) {
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveAdminConnectionsResult{}, err
	}
	connections, err := client.DriveAdminConnections(ctx)
	if err != nil {
		return DriveAdminConnectionsResult{}, authenticationError(name, err)
	}
	return DriveAdminConnectionsResult{NAS: name, Connections: connections}, nil
}

func (s *Service) GetDriveAdminTeamFolders(ctx context.Context, requestedNAS string) (DriveAdminTeamFoldersResult, error) {
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveAdminTeamFoldersResult{}, err
	}
	folders, err := client.DriveAdminTeamFolders(ctx)
	if err != nil {
		return DriveAdminTeamFoldersResult{}, authenticationError(name, err)
	}
	return DriveAdminTeamFoldersResult{NAS: name, TeamFolders: folders}, nil
}

func (s *Service) GetDriveAdminLog(ctx context.Context, requestedNAS string, query driveadmin.LogQuery) (DriveAdminLogResult, error) {
	if err := validateDriveAdminLogQuery(&query); err != nil {
		return DriveAdminLogResult{}, err
	}
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveAdminLogResult{}, err
	}
	log, err := client.DriveAdminLog(ctx, query)
	if err != nil {
		return DriveAdminLogResult{}, authenticationError(name, err)
	}
	return DriveAdminLogResult{NAS: name, Log: log}, nil
}

func validateDriveAdminLogQuery(query *driveadmin.LogQuery) error {
	if query.Limit < 0 {
		return fmt.Errorf("log limit cannot be negative")
	}
	if query.Offset < 0 {
		return fmt.Errorf("log offset cannot be negative")
	}
	if query.Limit == 0 {
		query.Limit = driveAdminDefaultLogLimit
	}
	if query.Limit > driveAdminMaxLogLimit {
		return fmt.Errorf("log limit %d exceeds the maximum %d", query.Limit, driveAdminMaxLogLimit)
	}
	if query.From < 0 || query.To < 0 {
		return fmt.Errorf("log time bounds must be Unix seconds at or after 0")
	}
	if query.From > 0 && query.To > 0 && query.To < query.From {
		return fmt.Errorf("log time upper bound is before the lower bound")
	}
	return nil
}

const driveConfigAPIVersion = "dsmctl.io/v1alpha1"

type DriveServerConfigResult struct {
	NAS    string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	Config synology.DriveServerConfig `json:"config" jsonschema:"Normalized Drive server database configuration"`
}

type DriveConfigPlan struct {
	APIVersion          string                         `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                         `json:"nas" jsonschema:"NAS profile selected during planning"`
	Request             driveadmin.ServerConfigChange  `json:"request" jsonschema:"Patch-only Drive server config intent"`
	Observed            synology.DriveServerConfig     `json:"observed" jsonschema:"Complete config observed during planning"`
	ObservedFingerprint string                         `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed config"`
	Risk                string                         `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                       `json:"warnings" jsonschema:"Resource-impact warnings"`
	Summary             []string                       `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                         `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed config"`
}

type DriveConfigApplyResult struct {
	NAS      string                              `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                              `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied  bool                                `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Result   synology.DriveConfigMutationResult  `json:"result" jsonschema:"Selected DSM mutation backend"`
}

func (s *Service) GetDriveServerConfig(ctx context.Context, requestedNAS string) (DriveServerConfigResult, error) {
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveServerConfigResult{}, err
	}
	config, err := client.DriveServerConfig(ctx)
	if err != nil {
		return DriveServerConfigResult{}, authenticationError(name, err)
	}
	return DriveServerConfigResult{NAS: name, Config: config}, nil
}

func (s *Service) PlanDriveConfigChange(ctx context.Context, requestedNAS string, request driveadmin.ServerConfigChange) (DriveConfigPlan, error) {
	if err := validateDriveConfigChange(request); err != nil {
		return DriveConfigPlan{}, err
	}
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveConfigPlan{}, err
	}
	return planDriveConfigChangeWithClient(ctx, name, client, request)
}

func (s *Service) ApplyDriveConfigPlan(ctx context.Context, plan DriveConfigPlan, approvalHash string) (DriveConfigApplyResult, error) {
	if err := validateDriveConfigPlan(plan, approvalHash); err != nil {
		return DriveConfigApplyResult{}, err
	}
	name, client, err := s.driveAdminClient(ctx, plan.NAS)
	if err != nil {
		return DriveConfigApplyResult{}, err
	}
	if name != plan.NAS {
		return DriveConfigApplyResult{}, fmt.Errorf("Drive config plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	current, err := planDriveConfigChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return DriveConfigApplyResult{}, fmt.Errorf("Drive config plan precondition no longer holds: %w", err)
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return DriveConfigApplyResult{}, fmt.Errorf("Drive config plan is stale; create a new plan")
	}
	result, err := client.ApplyDriveServerConfigChange(ctx, plan.Request)
	if err != nil {
		return DriveConfigApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := client.DriveServerConfig(ctx)
	if err != nil {
		return DriveConfigApplyResult{}, fmt.Errorf("verify Drive config change: %w", err)
	}
	if !driveConfigChangeMatches(after, plan.Request) {
		return DriveConfigApplyResult{}, fmt.Errorf("Drive config does not match the approved patch")
	}
	return DriveConfigApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Result: result}, nil
}

func planDriveConfigChangeWithClient(ctx context.Context, nas string, client driveAdminClient, request driveadmin.ServerConfigChange) (DriveConfigPlan, error) {
	capabilities, _, err := client.DriveAdminCapabilities(ctx)
	if err != nil {
		return DriveConfigPlan{}, authenticationError(nas, err)
	}
	if !capabilities.ConfigRead {
		return DriveConfigPlan{}, fmt.Errorf("NAS %q does not expose a verified Drive config read backend", nas)
	}
	if !capabilities.ConfigSet {
		return DriveConfigPlan{}, fmt.Errorf("NAS %q does not expose a verified Drive config set backend", nas)
	}
	observed, err := client.DriveServerConfig(ctx)
	if err != nil {
		return DriveConfigPlan{}, authenticationError(nas, err)
	}
	if driveConfigChangeMatches(observed, request) {
		return DriveConfigPlan{}, fmt.Errorf("Drive config patch would not change the current config")
	}
	plan := DriveConfigPlan{APIVersion: driveConfigAPIVersion, NAS: nas, Request: request, Observed: observed}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return DriveConfigPlan{}, err
	}
	plan.Warnings, plan.Summary = driveConfigPlanEffects(observed, request)
	if len(plan.Warnings) > 0 {
		plan.Risk = "high"
	} else {
		plan.Risk = "medium"
	}
	plan.Hash, err = driveConfigPlanHash(plan)
	if err != nil {
		return DriveConfigPlan{}, err
	}
	return plan, nil
}

func validateDriveConfigChange(change driveadmin.ServerConfigChange) error {
	if change.VMTouchEnabled == nil && change.VMTouchReserveMem == nil {
		return fmt.Errorf("Drive config patch has no fields")
	}
	if change.VMTouchReserveMem != nil && *change.VMTouchReserveMem < 0 {
		return fmt.Errorf("vmtouch_reserve_mem cannot be negative")
	}
	return nil
}

func driveConfigPlanEffects(observed synology.DriveServerConfig, change driveadmin.ServerConfigChange) ([]string, []string) {
	warnings := []string{}
	summary := []string{}
	if change.VMTouchEnabled != nil {
		summary = append(summary, fmt.Sprintf("set vmtouch_enabled to %t", *change.VMTouchEnabled))
		if *change.VMTouchEnabled && !observed.VMTouchEnabled {
			warnings = append(warnings, "enabling vmtouch reserves memory to pin the Drive database")
		}
	}
	if change.VMTouchReserveMem != nil {
		summary = append(summary, fmt.Sprintf("set vmtouch_reserve_mem to %d MB", *change.VMTouchReserveMem))
	}
	return warnings, summary
}

func driveConfigChangeMatches(state synology.DriveServerConfig, change driveadmin.ServerConfigChange) bool {
	return (change.VMTouchEnabled == nil || state.VMTouchEnabled == *change.VMTouchEnabled) &&
		(change.VMTouchReserveMem == nil || state.VMTouchReserveMem == *change.VMTouchReserveMem)
}

func validateDriveConfigPlan(plan DriveConfigPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the Drive config plan")
	}
	if plan.APIVersion != driveConfigAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid Drive config plan metadata")
	}
	if err := validateDriveConfigChange(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("Drive config plan observed config was modified")
	}
	expectedHash, err := driveConfigPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("Drive config plan contents were modified after planning")
	}
	return nil
}

func driveConfigPlanHash(plan DriveConfigPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func (s *Service) driveAdminClient(ctx context.Context, requestedNAS string) (string, driveAdminClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(driveAdminClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement Drive Admin management")
	}
	return name, client, nil
}

var _ driveAdminClient = (*synology.Client)(nil)
