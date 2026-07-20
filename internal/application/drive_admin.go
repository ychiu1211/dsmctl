package application

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	ApplyDriveTeamFolderChange(context.Context, driveadmin.TeamFolderChange) (synology.DriveTeamFolderMutationResult, error)
	DriveConnectionSummary(context.Context) (synology.DriveConnectionSummary, error)
	DriveDBUsage(context.Context) (synology.DriveDBUsage, error)
	DriveTopAccessFiles(context.Context, synology.DriveTopAccessQuery) (synology.DriveTopAccessFiles, error)
	DriveActivation(context.Context) (synology.DriveActivation, error)
}

type DriveConnectionSummaryResult struct {
	NAS     string                          `json:"nas" jsonschema:"NAS profile used for the request"`
	Summary synology.DriveConnectionSummary `json:"summary" jsonschema:"Active connection counts by client family"`
}

type DriveDBUsageResult struct {
	NAS   string               `json:"nas" jsonschema:"NAS profile used for the request"`
	Usage synology.DriveDBUsage `json:"usage" jsonschema:"Cached Drive database usage in bytes"`
}

type DriveTopAccessFilesResult struct {
	NAS   string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Files synology.DriveTopAccessFiles  `json:"files" jsonschema:"Top accessed files, most accessed first"`
	Query synology.DriveTopAccessQuery  `json:"query" jsonschema:"Ranking query after defaults were applied"`
}

type DriveActivationResult struct {
	NAS        string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Activation synology.DriveActivation `json:"activation" jsonschema:"Drive package activation state"`
}

func (s *Service) GetDriveConnectionSummary(ctx context.Context, requestedNAS string) (DriveConnectionSummaryResult, error) {
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveConnectionSummaryResult{}, err
	}
	summary, err := client.DriveConnectionSummary(ctx)
	if err != nil {
		return DriveConnectionSummaryResult{}, authenticationError(name, err)
	}
	return DriveConnectionSummaryResult{NAS: name, Summary: summary}, nil
}

func (s *Service) GetDriveDBUsage(ctx context.Context, requestedNAS string) (DriveDBUsageResult, error) {
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveDBUsageResult{}, err
	}
	usage, err := client.DriveDBUsage(ctx)
	if err != nil {
		return DriveDBUsageResult{}, authenticationError(name, err)
	}
	return DriveDBUsageResult{NAS: name, Usage: usage}, nil
}

const (
	driveTopAccessDefaultLimit = 50
	driveTopAccessMaxLimit     = 1000
	driveTopAccessDefaultDays  = 1
)

func (s *Service) GetDriveTopAccessFiles(ctx context.Context, requestedNAS string, query synology.DriveTopAccessQuery) (DriveTopAccessFilesResult, error) {
	if err := validateDriveTopAccessQuery(&query); err != nil {
		return DriveTopAccessFilesResult{}, err
	}
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveTopAccessFilesResult{}, err
	}
	files, err := client.DriveTopAccessFiles(ctx, query)
	if err != nil {
		return DriveTopAccessFilesResult{}, authenticationError(name, err)
	}
	return DriveTopAccessFilesResult{NAS: name, Files: files, Query: query}, nil
}

func validateDriveTopAccessQuery(query *synology.DriveTopAccessQuery) error {
	switch query.RankingBy {
	case "":
		query.RankingBy = "both"
	case "both", "preview", "download":
	default:
		return fmt.Errorf("ranking_by must be both, preview, or download")
	}
	if query.PeriodDays < 0 || query.Limit < 0 || query.Offset < 0 {
		return fmt.Errorf("top-access query values cannot be negative")
	}
	if query.PeriodDays == 0 {
		query.PeriodDays = driveTopAccessDefaultDays
	}
	if query.Limit == 0 {
		query.Limit = driveTopAccessDefaultLimit
	}
	if query.Limit > driveTopAccessMaxLimit {
		return fmt.Errorf("top-access limit %d exceeds the maximum %d", query.Limit, driveTopAccessMaxLimit)
	}
	return nil
}

func (s *Service) GetDriveActivation(ctx context.Context, requestedNAS string) (DriveActivationResult, error) {
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveActivationResult{}, err
	}
	activation, err := client.DriveActivation(ctx)
	if err != nil {
		return DriveActivationResult{}, authenticationError(name, err)
	}
	return DriveActivationResult{NAS: name, Activation: activation}, nil
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
	NAS    string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Config synology.DriveServerConfig `json:"config" jsonschema:"Normalized Drive server database configuration"`
}

type DriveConfigPlan struct {
	APIVersion          string                        `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                        `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                        `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             driveadmin.ServerConfigChange `json:"request" jsonschema:"Patch-only Drive server config intent"`
	Observed            synology.DriveServerConfig    `json:"observed" jsonschema:"Complete config observed during planning"`
	ObservedFingerprint string                        `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed config"`
	Risk                string                        `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                      `json:"warnings" jsonschema:"Resource-impact warnings"`
	Summary             []string                      `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                        `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed config"`
}

type DriveConfigApplyResult struct {
	NAS      string                             `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                             `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied  bool                               `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Result   synology.DriveConfigMutationResult `json:"result" jsonschema:"Selected DSM mutation backend"`
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
	plan, err := planDriveConfigChangeWithClient(ctx, name, client, request)
	if err != nil {
		return DriveConfigPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = driveConfigPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyDriveConfigPlan(ctx context.Context, plan DriveConfigPlan, approvalHash string) (DriveConfigApplyResult, error) {
	if err := validateDriveConfigPlan(plan, approvalHash); err != nil {
		return DriveConfigApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return DriveConfigApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
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
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = driveConfigPlanHash(current)
	if err != nil {
		return DriveConfigApplyResult{}, err
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

const driveTeamFolderAPIVersion = "dsmctl.io/v1alpha1"

// Postcondition polling for team-folder changes: Drive applies enable/disable
// through its user-control queue, so the list may converge shortly after the
// set call returns. The delay is a variable so tests run without sleeping.
var driveTeamFolderVerifyDelay = 2 * time.Second

const driveTeamFolderVerifyAttempts = 5

type DriveTeamFolderPlan struct {
	APIVersion          string                       `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                       `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                       `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             driveadmin.TeamFolderChange  `json:"request" jsonschema:"Validated team-folder intent"`
	Observed            driveadmin.TeamFolder        `json:"observed" jsonschema:"Target team-folder entry observed during planning"`
	ObservedFingerprint string                       `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the observed team-folder entry"`
	Destructive         bool                         `json:"destructive" jsonschema:"Whether the plan deletes Drive data (team-folder database or stored versions)"`
	Risk                string                       `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                     `json:"warnings" jsonschema:"Data-loss and eligibility warnings"`
	Summary             []string                     `json:"summary" jsonschema:"Human-readable operations the plan will perform"`
	Hash                string                       `json:"hash" jsonschema:"SHA-256 approval hash covering intent and observed entry"`
}

type DriveTeamFolderApplyResult struct {
	NAS        string                                 `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash   string                                 `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied    bool                                   `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Result     synology.DriveTeamFolderMutationResult `json:"result" jsonschema:"Selected DSM mutation backend"`
	TeamFolder driveadmin.TeamFolder                  `json:"team_folder" jsonschema:"Team-folder entry re-read after the change"`
}

func (s *Service) PlanDriveTeamFolderChange(ctx context.Context, requestedNAS string, request driveadmin.TeamFolderChange) (DriveTeamFolderPlan, error) {
	if err := validateDriveTeamFolderChange(request); err != nil {
		return DriveTeamFolderPlan{}, err
	}
	name, client, err := s.driveAdminClient(ctx, requestedNAS)
	if err != nil {
		return DriveTeamFolderPlan{}, err
	}
	plan, err := planDriveTeamFolderChangeWithClient(ctx, name, client, request)
	if err != nil {
		return DriveTeamFolderPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = driveTeamFolderPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyDriveTeamFolderPlan(ctx context.Context, plan DriveTeamFolderPlan, approvalHash string) (DriveTeamFolderApplyResult, error) {
	if err := validateDriveTeamFolderPlan(plan, approvalHash); err != nil {
		return DriveTeamFolderApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return DriveTeamFolderApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return DriveTeamFolderApplyResult{}, err
	}
	name, client, err := s.driveAdminClient(ctx, plan.NAS)
	if err != nil {
		return DriveTeamFolderApplyResult{}, err
	}
	if name != plan.NAS {
		return DriveTeamFolderApplyResult{}, fmt.Errorf("Drive team-folder plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyDriveTeamFolderPlanWithClient(ctx, client, plan)
}

func applyDriveTeamFolderPlanWithClient(ctx context.Context, client driveAdminClient, plan DriveTeamFolderPlan) (DriveTeamFolderApplyResult, error) {
	current, err := planDriveTeamFolderChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return DriveTeamFolderApplyResult{}, fmt.Errorf("Drive team-folder plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = driveTeamFolderPlanHash(current)
	if err != nil {
		return DriveTeamFolderApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return DriveTeamFolderApplyResult{}, fmt.Errorf("Drive team-folder plan is stale; create a new plan")
	}
	result, err := client.ApplyDriveTeamFolderChange(ctx, plan.Request)
	if err != nil {
		return DriveTeamFolderApplyResult{}, authenticationError(plan.NAS, err)
	}
	// Drive answers Share.set with an empty success even when it skips an
	// ineligible share, so the re-read below is the authority on whether the
	// change happened.
	folder, err := verifyDriveTeamFolderPostcondition(ctx, client, plan)
	if err != nil {
		return DriveTeamFolderApplyResult{}, err
	}
	return DriveTeamFolderApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Result: result, TeamFolder: folder}, nil
}

func verifyDriveTeamFolderPostcondition(ctx context.Context, client driveAdminClient, plan DriveTeamFolderPlan) (driveadmin.TeamFolder, error) {
	desiredCount, desiredPolicy, desiredDays := driveTeamFolderDesiredVersioning(plan.Request, plan.Observed)
	var lastState string
	for attempt := 0; attempt < driveTeamFolderVerifyAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return driveadmin.TeamFolder{}, ctx.Err()
			case <-time.After(driveTeamFolderVerifyDelay):
			}
		}
		folders, err := client.DriveAdminTeamFolders(ctx)
		if err != nil {
			return driveadmin.TeamFolder{}, fmt.Errorf("verify Drive team-folder change: %w", err)
		}
		folder, found := findDriveTeamFolder(folders, plan.Request.Name)
		if !found {
			lastState = "the folder is no longer listed"
			continue
		}
		switch plan.Request.Action {
		case driveadmin.TeamFolderActionDisable:
			if !folder.Enabled {
				return folder, nil
			}
			lastState = "the folder is still enabled"
		default:
			if driveTeamFolderVersioningMatches(folder, desiredCount, desiredPolicy, desiredDays) {
				return folder, nil
			}
			lastState = fmt.Sprintf("observed enabled=%t versioning=%s", folder.Enabled, describeDriveTeamFolderVersioning(folder))
		}
	}
	return driveadmin.TeamFolder{}, fmt.Errorf("Drive did not confirm the team-folder change (%s); Drive may still be applying it or the share may be ineligible — re-check with the team-folders read", lastState)
}

func driveTeamFolderVersioningMatches(folder driveadmin.TeamFolder, count int, policy string, days int) bool {
	if !folder.Enabled || folder.MaxVersions == nil || *folder.MaxVersions != count {
		return false
	}
	if folder.VersionPolicy != policy {
		return false
	}
	observedDays := 0
	if folder.RetentionDays != nil {
		observedDays = *folder.RetentionDays
	}
	return observedDays == days
}

// driveTeamFolderDesiredVersioning computes the end state a plan commits to.
// Enable starts from scratch; set_versioning starts from the observed entry
// because DSM merges omitted fields from the stored view settings. Whenever
// the resulting count is zero, Drive forces the policy and retention off.
func driveTeamFolderDesiredVersioning(request driveadmin.TeamFolderChange, observed driveadmin.TeamFolder) (int, string, int) {
	count, policy, days := 0, "", 0
	if request.Action == driveadmin.TeamFolderActionSetVersioning {
		if observed.MaxVersions != nil {
			count = *observed.MaxVersions
		}
		policy = observed.VersionPolicy
		if observed.RetentionDays != nil {
			days = *observed.RetentionDays
		}
	}
	if request.MaxVersions != nil {
		count = *request.MaxVersions
	}
	if request.VersionPolicy != "" {
		policy = request.VersionPolicy
	}
	if request.RetentionDays != nil {
		days = *request.RetentionDays
	}
	if count == 0 {
		policy, days = "", 0
	}
	return count, policy, days
}

func describeDriveTeamFolderVersioning(folder driveadmin.TeamFolder) string {
	if folder.MaxVersions == nil {
		return "-"
	}
	days := 0
	if folder.RetentionDays != nil {
		days = *folder.RetentionDays
	}
	if *folder.MaxVersions == 0 {
		return "off"
	}
	return fmt.Sprintf("%d versions, policy %s, retention %d days", *folder.MaxVersions, valueOrNone(folder.VersionPolicy), days)
}

func valueOrNone(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func planDriveTeamFolderChangeWithClient(ctx context.Context, nas string, client driveAdminClient, request driveadmin.TeamFolderChange) (DriveTeamFolderPlan, error) {
	capabilities, _, err := client.DriveAdminCapabilities(ctx)
	if err != nil {
		return DriveTeamFolderPlan{}, authenticationError(nas, err)
	}
	if !capabilities.TeamFoldersRead {
		return DriveTeamFolderPlan{}, fmt.Errorf("NAS %q does not expose a verified Drive team-folder read backend", nas)
	}
	if !capabilities.TeamFoldersSet {
		return DriveTeamFolderPlan{}, fmt.Errorf("NAS %q does not expose a verified Drive team-folder set backend", nas)
	}
	folders, err := client.DriveAdminTeamFolders(ctx)
	if err != nil {
		return DriveTeamFolderPlan{}, authenticationError(nas, err)
	}
	observed, found := findDriveTeamFolder(folders, request.Name)
	if !found {
		return DriveTeamFolderPlan{}, fmt.Errorf("shared folder %q is not in the Drive team-folder view; list it with the team-folders read first", request.Name)
	}
	switch request.Action {
	case driveadmin.TeamFolderActionEnable:
		if observed.Enabled {
			return DriveTeamFolderPlan{}, fmt.Errorf("shared folder %q is already enabled as a team folder", request.Name)
		}
	case driveadmin.TeamFolderActionDisable:
		if !observed.Enabled {
			return DriveTeamFolderPlan{}, fmt.Errorf("shared folder %q is not enabled as a team folder", request.Name)
		}
	case driveadmin.TeamFolderActionSetVersioning:
		if !observed.Enabled {
			return DriveTeamFolderPlan{}, fmt.Errorf("shared folder %q is not enabled as a team folder; enable it first", request.Name)
		}
	}
	plan := DriveTeamFolderPlan{APIVersion: driveTeamFolderAPIVersion, NAS: nas, Request: request, Observed: observed}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return DriveTeamFolderPlan{}, err
	}
	desiredCount, desiredPolicy, desiredDays := driveTeamFolderDesiredVersioning(request, observed)
	if request.Action == driveadmin.TeamFolderActionSetVersioning &&
		driveTeamFolderVersioningMatches(observed, desiredCount, desiredPolicy, desiredDays) {
		return DriveTeamFolderPlan{}, fmt.Errorf("versioning change would not change team folder %q", request.Name)
	}
	plan.Destructive, plan.Warnings, plan.Summary = driveTeamFolderPlanEffects(request, observed, desiredCount, desiredPolicy, desiredDays)
	if plan.Destructive {
		plan.Risk = "high"
	} else {
		plan.Risk = "medium"
	}
	plan.Hash, err = driveTeamFolderPlanHash(plan)
	if err != nil {
		return DriveTeamFolderPlan{}, err
	}
	return plan, nil
}

// driveTeamFolderPlanEffects classifies one change. Disabling a team folder
// deletes Drive's database for it, and any versioning reduction prunes stored
// versions, so both are destructive and high risk.
func driveTeamFolderPlanEffects(request driveadmin.TeamFolderChange, observed driveadmin.TeamFolder, desiredCount int, desiredPolicy string, desiredDays int) (bool, []string, []string) {
	destructive := false
	warnings := []string{}
	summary := []string{}
	describeDesired := func() string {
		if desiredCount == 0 {
			return "versioning off"
		}
		return fmt.Sprintf("%d versions, policy %s, retention %d days (0 keeps versions until rotated)", desiredCount, desiredPolicy, desiredDays)
	}
	switch request.Action {
	case driveadmin.TeamFolderActionEnable:
		summary = append(summary, fmt.Sprintf("enable %q as a Drive team folder with %s", request.Name, describeDesired()))
		warnings = append(warnings, "Drive will index the folder contents into its database; large folders take time and space")
	case driveadmin.TeamFolderActionDisable:
		destructive = true
		summary = append(summary, fmt.Sprintf("disable team folder %q", request.Name))
		warnings = append(warnings, fmt.Sprintf("disabling deletes Drive's team-folder database and stored file versions for %q; files in the shared folder are not removed", request.Name))
	case driveadmin.TeamFolderActionSetVersioning:
		summary = append(summary, fmt.Sprintf("set versioning on team folder %q to %s (currently %s)", request.Name, describeDesired(), describeDriveTeamFolderVersioning(observed)))
		observedCount, observedDays := 0, 0
		if observed.MaxVersions != nil {
			observedCount = *observed.MaxVersions
		}
		if observed.RetentionDays != nil {
			observedDays = *observed.RetentionDays
		}
		if desiredCount < observedCount {
			destructive = true
			if desiredCount == 0 {
				warnings = append(warnings, fmt.Sprintf("turning versioning off deletes the stored versions for %q", request.Name))
			} else {
				warnings = append(warnings, fmt.Sprintf("reducing kept versions from %d to %d prunes older stored versions", observedCount, desiredCount))
			}
		}
		tightened := (observedDays == 0 && desiredDays > 0) || (observedDays > 0 && desiredDays > 0 && desiredDays < observedDays)
		if desiredCount > 0 && tightened {
			destructive = true
			warnings = append(warnings, fmt.Sprintf("retention of %d days prunes versions older than that", desiredDays))
		}
	}
	if observed.Status != "" && observed.Status != "normal" {
		warnings = append(warnings, fmt.Sprintf("share status is %q; Drive silently skips ineligible shares, which apply would surface as a failed postcondition", observed.Status))
	}
	return destructive, warnings, summary
}

func validateDriveTeamFolderChange(change driveadmin.TeamFolderChange) error {
	name := strings.TrimSpace(change.Name)
	if name == "" {
		return fmt.Errorf("team-folder change requires the shared-folder name")
	}
	if name != change.Name {
		return fmt.Errorf("team-folder name must not carry surrounding whitespace")
	}
	if strings.HasPrefix(name, "homes") {
		return fmt.Errorf("the Drive home entry %q is managed by the DSM home service and is out of scope for a team-folder change", name)
	}
	if name == "surveillance" {
		return fmt.Errorf("Drive ignores the surveillance share; it cannot be managed as a team folder")
	}
	if change.VersionPolicy != "" && change.VersionPolicy != "fifo" && change.VersionPolicy != "smart" {
		return fmt.Errorf("version_policy must be fifo or smart")
	}
	if change.MaxVersions != nil && (*change.MaxVersions < 0 || *change.MaxVersions > 32) {
		return fmt.Errorf("max_versions must be within 0..32")
	}
	if change.RetentionDays != nil && (*change.RetentionDays < 0 || *change.RetentionDays > 120) {
		return fmt.Errorf("retention_days must be within 0..120")
	}
	switch change.Action {
	case driveadmin.TeamFolderActionEnable:
		if change.MaxVersions == nil {
			return fmt.Errorf("enable requires max_versions (0..32; 0 turns versioning off): Drive refuses to enable a team folder without it")
		}
		if *change.MaxVersions > 0 && change.VersionPolicy == "" {
			return fmt.Errorf("enable with versioning requires an explicit version_policy (fifo or smart)")
		}
		if *change.MaxVersions == 0 && (change.VersionPolicy != "" || (change.RetentionDays != nil && *change.RetentionDays != 0)) {
			return fmt.Errorf("max_versions 0 turns versioning off; version_policy and retention_days do not apply")
		}
	case driveadmin.TeamFolderActionDisable:
		if change.MaxVersions != nil || change.VersionPolicy != "" || change.RetentionDays != nil {
			return fmt.Errorf("disable takes no versioning fields")
		}
	case driveadmin.TeamFolderActionSetVersioning:
		if change.MaxVersions == nil && change.VersionPolicy == "" && change.RetentionDays == nil {
			return fmt.Errorf("set_versioning requires at least one of max_versions, version_policy, or retention_days")
		}
	default:
		return fmt.Errorf("team-folder action must be enable, disable, or set_versioning")
	}
	return nil
}

func validateDriveTeamFolderPlan(plan DriveTeamFolderPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the Drive team-folder plan")
	}
	if plan.APIVersion != driveTeamFolderAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid Drive team-folder plan metadata")
	}
	if err := validateDriveTeamFolderChange(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("Drive team-folder plan observed entry was modified")
	}
	expectedHash, err := driveTeamFolderPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("Drive team-folder plan contents were modified after planning")
	}
	return nil
}

func driveTeamFolderPlanHash(plan DriveTeamFolderPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func findDriveTeamFolder(folders synology.DriveAdminTeamFolders, name string) (driveadmin.TeamFolder, bool) {
	for _, folder := range folders.TeamFolders {
		if folder.Name == name {
			return folder, true
		}
	}
	return driveadmin.TeamFolder{}, false
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
