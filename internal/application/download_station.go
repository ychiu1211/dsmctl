package application

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/downloadstation"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const downloadStationAPIVersion = "dsmctl.io/v1alpha1"

type DownloadStationCapabilitiesResult struct {
	NAS          string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.DownloadStationCapabilities `json:"capabilities" jsonschema:"Download Station reads currently exposed by dsmctl"`
	Report       synology.CompatibilityReport         `json:"report" jsonschema:"Discovered APIs and selected Download Station backends"`
}

type DownloadStationServiceResult struct {
	NAS     string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Service synology.DownloadStationServiceState `json:"service" jsonschema:"Normalized Download Station service configuration"`
}

type DownloadStationTasksResult struct {
	NAS   string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Tasks synology.DownloadStationTasks `json:"tasks" jsonschema:"Download task list"`
}

type DownloadStationStatisticsResult struct {
	NAS        string                             `json:"nas" jsonschema:"NAS profile used for the request"`
	Statistics synology.DownloadStationStatistics `json:"statistics" jsonschema:"Aggregate transfer statistics"`
}

type DownloadStationSettingsResult struct {
	NAS      string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Settings synology.DownloadStationSettings `json:"settings" jsonschema:"Full detailed Download Station configuration"`
}

func (s *Service) GetDownloadStationCapabilities(ctx context.Context, requestedNAS string) (DownloadStationCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DownloadStationCapabilitiesResult{}, err
	}
	capabilities, report, err := client.DownloadStationCapabilities(ctx)
	if err != nil {
		return DownloadStationCapabilitiesResult{}, authenticationError(name, err)
	}
	return DownloadStationCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetDownloadStationService(ctx context.Context, requestedNAS string) (DownloadStationServiceResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DownloadStationServiceResult{}, err
	}
	state, err := client.DownloadStationServiceState(ctx)
	if err != nil {
		return DownloadStationServiceResult{}, authenticationError(name, err)
	}
	return DownloadStationServiceResult{NAS: name, Service: state}, nil
}

func (s *Service) GetDownloadStationTasks(ctx context.Context, requestedNAS string) (DownloadStationTasksResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DownloadStationTasksResult{}, err
	}
	tasks, err := client.DownloadStationTasks(ctx)
	if err != nil {
		return DownloadStationTasksResult{}, authenticationError(name, err)
	}
	return DownloadStationTasksResult{NAS: name, Tasks: tasks}, nil
}

func (s *Service) GetDownloadStationStatistics(ctx context.Context, requestedNAS string) (DownloadStationStatisticsResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DownloadStationStatisticsResult{}, err
	}
	stats, err := client.DownloadStationStatistics(ctx)
	if err != nil {
		return DownloadStationStatisticsResult{}, authenticationError(name, err)
	}
	return DownloadStationStatisticsResult{NAS: name, Statistics: stats}, nil
}

func (s *Service) GetDownloadStationSettings(ctx context.Context, requestedNAS string) (DownloadStationSettingsResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DownloadStationSettingsResult{}, err
	}
	settings, err := client.DownloadStationSettings(ctx)
	if err != nil {
		return DownloadStationSettingsResult{}, authenticationError(name, err)
	}
	return DownloadStationSettingsResult{NAS: name, Settings: settings}, nil
}

// DownloadStationTaskSummary is a stable-field projection of a target task,
// bound into a task plan so an apply can detect a target that has since
// disappeared without binding to volatile transfer progress.
type DownloadStationTaskSummary struct {
	ID    string `json:"id" jsonschema:"Task identifier"`
	Title string `json:"title,omitempty" jsonschema:"Task title"`
	Type  string `json:"type,omitempty" jsonschema:"Download protocol"`
}

type DownloadStationTaskPlan struct {
	APIVersion          string                       `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                       `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                       `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             downloadstation.TaskChange   `json:"request" jsonschema:"Validated task mutation intent"`
	Observed            []DownloadStationTaskSummary `json:"observed" jsonschema:"Target tasks observed during planning (control actions); empty for create"`
	ObservedFingerprint string                       `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the observed target tasks"`
	Risk                string                       `json:"risk" jsonschema:"Plan risk level"`
	Warnings            []string                     `json:"warnings" jsonschema:"Operational warnings"`
	Summary             []string                     `json:"summary" jsonschema:"Human-readable operations"`
	Hash                string                       `json:"hash" jsonschema:"SHA-256 approval hash covering intent and observed targets"`
}

type DownloadStationTaskApplyResult struct {
	NAS      string                                     `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                                     `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied  bool                                       `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Result   synology.DownloadStationTaskMutationResult `json:"result" jsonschema:"Selected DSM mutation backend and affected task ids"`
}

type downloadStationTaskClient interface {
	DownloadStationTasks(context.Context) (synology.DownloadStationTasks, error)
	DownloadStationCapabilities(context.Context) (synology.DownloadStationCapabilities, synology.CompatibilityReport, error)
	ApplyDownloadStationTaskChange(context.Context, synology.DownloadStationTaskChange) (synology.DownloadStationTaskMutationResult, error)
}

func (s *Service) downloadStationTaskClient(ctx context.Context, requestedNAS string) (string, downloadStationTaskClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(downloadStationTaskClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement Download Station task management")
	}
	return name, client, nil
}

func (s *Service) PlanDownloadStationTaskChange(ctx context.Context, requestedNAS string, request downloadstation.TaskChange) (DownloadStationTaskPlan, error) {
	if err := validateTaskChangeShape(request); err != nil {
		return DownloadStationTaskPlan{}, err
	}
	name, client, err := s.downloadStationTaskClient(ctx, requestedNAS)
	if err != nil {
		return DownloadStationTaskPlan{}, err
	}
	plan, err := planDownloadStationTaskWithClient(ctx, name, client, request)
	if err != nil {
		return DownloadStationTaskPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = downloadStationTaskPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyDownloadStationTaskPlan(ctx context.Context, plan DownloadStationTaskPlan, approvalHash string) (DownloadStationTaskApplyResult, error) {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return DownloadStationTaskApplyResult{}, fmt.Errorf("approval hash does not match the task plan")
	}
	if plan.APIVersion != downloadStationAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return DownloadStationTaskApplyResult{}, fmt.Errorf("invalid task plan metadata")
	}
	if err := validateTaskChangeShape(plan.Request); err != nil {
		return DownloadStationTaskApplyResult{}, err
	}
	expectedHash, err := downloadStationTaskPlanHash(plan)
	if err != nil {
		return DownloadStationTaskApplyResult{}, err
	}
	if expectedHash != plan.Hash {
		return DownloadStationTaskApplyResult{}, fmt.Errorf("task plan contents were modified after planning")
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return DownloadStationTaskApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return DownloadStationTaskApplyResult{}, err
	}
	name, client, err := s.downloadStationTaskClient(ctx, plan.NAS)
	if err != nil {
		return DownloadStationTaskApplyResult{}, err
	}
	if name != plan.NAS {
		return DownloadStationTaskApplyResult{}, fmt.Errorf("task plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	current, err := planDownloadStationTaskWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return DownloadStationTaskApplyResult{}, fmt.Errorf("task plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = downloadStationTaskPlanHash(current)
	if err != nil {
		return DownloadStationTaskApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return DownloadStationTaskApplyResult{}, fmt.Errorf("task plan is stale; create a new plan")
	}
	result, err := client.ApplyDownloadStationTaskChange(ctx, plan.Request)
	if err != nil {
		return DownloadStationTaskApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyDownloadStationTaskPostcondition(ctx, client, plan.Request); err != nil {
		return DownloadStationTaskApplyResult{}, fmt.Errorf("verify task change: %w", err)
	}
	return DownloadStationTaskApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Result: result}, nil
}

func planDownloadStationTaskWithClient(ctx context.Context, nas string, client downloadStationTaskClient, request downloadstation.TaskChange) (DownloadStationTaskPlan, error) {
	capabilities, _, err := client.DownloadStationCapabilities(ctx)
	if err != nil {
		return DownloadStationTaskPlan{}, authenticationError(nas, err)
	}
	if !capabilities.TaskRead || !capabilities.TaskWrite {
		return DownloadStationTaskPlan{}, fmt.Errorf("NAS %q does not expose a verified Download Station task read/write backend", nas)
	}
	plan := DownloadStationTaskPlan{APIVersion: downloadStationAPIVersion, NAS: nas, Request: request, Observed: []DownloadStationTaskSummary{}}
	if request.Action != downloadstation.TaskActionCreate {
		tasks, err := client.DownloadStationTasks(ctx)
		if err != nil {
			return DownloadStationTaskPlan{}, authenticationError(nas, err)
		}
		byID := make(map[string]synology.DownloadStationTask, len(tasks.Tasks))
		for _, task := range tasks.Tasks {
			byID[task.ID] = task
		}
		observed := make([]DownloadStationTaskSummary, 0, len(request.TaskIDs))
		for _, id := range request.TaskIDs {
			task, ok := byID[id]
			if !ok {
				return DownloadStationTaskPlan{}, fmt.Errorf("task %q was not found on NAS %q", id, nas)
			}
			observed = append(observed, DownloadStationTaskSummary{ID: task.ID, Title: task.Title, Type: task.Type})
		}
		sort.Slice(observed, func(i, j int) bool { return observed[i].ID < observed[j].ID })
		plan.Observed = observed
	}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return DownloadStationTaskPlan{}, err
	}
	plan.Risk, plan.Warnings, plan.Summary = downloadStationTaskEffects(request)
	plan.Hash, err = downloadStationTaskPlanHash(plan)
	if err != nil {
		return DownloadStationTaskPlan{}, err
	}
	return plan, nil
}

// validateTaskChangeShape rejects everything invalid regardless of NAS state.
func validateTaskChangeShape(change downloadstation.TaskChange) error {
	switch change.Action {
	case downloadstation.TaskActionCreate:
		if len(change.URIs) == 0 {
			return fmt.Errorf("a create task requires at least one uri")
		}
		for _, uri := range change.URIs {
			if err := validateDownloadURI(strings.TrimSpace(uri)); err != nil {
				return err
			}
		}
	case downloadstation.TaskActionPause, downloadstation.TaskActionResume, downloadstation.TaskActionDelete:
		if len(change.TaskIDs) == 0 {
			return fmt.Errorf("a %s action requires at least one task_id", change.Action)
		}
		for _, id := range change.TaskIDs {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("task_id must not be empty")
			}
		}
	default:
		return fmt.Errorf("unsupported task action %q; use create, pause, resume, or delete", change.Action)
	}
	return nil
}

func validateDownloadURI(uri string) error {
	if uri == "" {
		return fmt.Errorf("uri must not be empty")
	}
	for _, scheme := range []string{"http://", "https://", "ftp://", "ftps://", "magnet:"} {
		if strings.HasPrefix(strings.ToLower(uri), scheme) {
			return nil
		}
	}
	return fmt.Errorf("unsupported download uri %q; expected an http(s), ftp(s), or magnet uri", uri)
}

func downloadStationTaskEffects(change downloadstation.TaskChange) (string, []string, []string) {
	switch change.Action {
	case downloadstation.TaskActionCreate:
		return "high",
			[]string{"creating a task makes the NAS fetch external content from the supplied uri(s)"},
			[]string{fmt.Sprintf("create %d download task(s) to %s", len(change.URIs), destinationOrDefault(change.Destination))}
	case downloadstation.TaskActionResume:
		return "high",
			[]string{"resuming restarts downloading, so the NAS fetches external content"},
			[]string{fmt.Sprintf("resume task(s) %s", strings.Join(change.TaskIDs, ", "))}
	case downloadstation.TaskActionDelete:
		warning := "deleting removes the task and its partial data"
		if change.ForceComplete {
			warning = "force_complete marks the task complete and keeps downloaded data instead of removing it"
		}
		return "high", []string{warning}, []string{fmt.Sprintf("delete task(s) %s", strings.Join(change.TaskIDs, ", "))}
	case downloadstation.TaskActionPause:
		return "medium",
			[]string{"pausing stops transfer for the task(s); it is reversible with resume"},
			[]string{fmt.Sprintf("pause task(s) %s", strings.Join(change.TaskIDs, ", "))}
	default:
		return "high", []string{}, []string{}
	}
}

func destinationOrDefault(destination string) string {
	if strings.TrimSpace(destination) == "" {
		return "the DSM default destination"
	}
	return strings.TrimSpace(destination)
}

func verifyDownloadStationTaskPostcondition(ctx context.Context, client downloadStationTaskClient, change downloadstation.TaskChange) error {
	tasks, err := client.DownloadStationTasks(ctx)
	if err != nil {
		return err
	}
	byID := make(map[string]synology.DownloadStationTask, len(tasks.Tasks))
	for _, task := range tasks.Tasks {
		byID[task.ID] = task
	}
	switch change.Action {
	case downloadstation.TaskActionCreate:
		wanted := make(map[string]struct{}, len(change.URIs))
		for _, uri := range change.URIs {
			wanted[strings.TrimSpace(uri)] = struct{}{}
		}
		for _, task := range tasks.Tasks {
			if _, ok := wanted[strings.TrimSpace(task.URI)]; ok {
				return nil
			}
		}
		return fmt.Errorf("no task matching the requested uri(s) is present after create")
	case downloadstation.TaskActionDelete:
		for _, id := range change.TaskIDs {
			if _, ok := byID[id]; ok {
				return fmt.Errorf("task %q is still present after delete", id)
			}
		}
		return nil
	case downloadstation.TaskActionPause:
		for _, id := range change.TaskIDs {
			task, ok := byID[id]
			if !ok {
				return fmt.Errorf("task %q is missing after pause", id)
			}
			if !strings.EqualFold(task.Status, "paused") {
				return fmt.Errorf("task %q is %q, want paused", id, task.Status)
			}
		}
		return nil
	case downloadstation.TaskActionResume:
		for _, id := range change.TaskIDs {
			task, ok := byID[id]
			if !ok {
				return fmt.Errorf("task %q is missing after resume", id)
			}
			if strings.EqualFold(task.Status, "paused") {
				return fmt.Errorf("task %q is still paused after resume", id)
			}
		}
		return nil
	default:
		return nil
	}
}

func downloadStationTaskPlanHash(plan DownloadStationTaskPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

var _ downloadStationTaskClient = (*synology.Client)(nil)

// --- Guarded settings write (BitTorrent group) ---

type DownloadStationSettingsPlan struct {
	APIVersion          string                         `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                         `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                         `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             downloadstation.SettingsChange `json:"request" jsonschema:"Validated patch-only settings intent"`
	Group               string                         `json:"group" jsonschema:"Settings group being changed, such as bt or ftp_http"`
	Observed            json.RawMessage                `json:"observed" jsonschema:"Complete observed state of the changed settings group"`
	ObservedFingerprint string                         `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed settings group"`
	Risk                string                         `json:"risk" jsonschema:"Plan risk level"`
	Warnings            []string                       `json:"warnings" jsonschema:"Operational warnings"`
	Summary             []string                       `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                         `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type DownloadStationSettingsApplyResult struct {
	NAS      string                                         `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                                         `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied  bool                                           `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Result   synology.DownloadStationSettingsMutationResult `json:"result" jsonschema:"Selected DSM mutation backend"`
}

type downloadStationSettingsClient interface {
	DownloadStationSettingsGroup(context.Context, string) (json.RawMessage, error)
	DownloadStationCapabilities(context.Context) (synology.DownloadStationCapabilities, synology.CompatibilityReport, error)
	ApplyDownloadStationSettingsChange(context.Context, synology.DownloadStationSettingsChange) (synology.DownloadStationSettingsMutationResult, error)
}

func (s *Service) downloadStationSettingsClient(ctx context.Context, requestedNAS string) (string, downloadStationSettingsClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(downloadStationSettingsClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement Download Station settings management")
	}
	return name, client, nil
}

func (s *Service) PlanDownloadStationSettingsChange(ctx context.Context, requestedNAS string, request downloadstation.SettingsChange) (DownloadStationSettingsPlan, error) {
	if err := validateSettingsChangeShape(request); err != nil {
		return DownloadStationSettingsPlan{}, err
	}
	name, client, err := s.downloadStationSettingsClient(ctx, requestedNAS)
	if err != nil {
		return DownloadStationSettingsPlan{}, err
	}
	plan, err := planDownloadStationSettingsWithClient(ctx, name, client, request)
	if err != nil {
		return DownloadStationSettingsPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = downloadStationSettingsPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyDownloadStationSettingsPlan(ctx context.Context, plan DownloadStationSettingsPlan, approvalHash string) (DownloadStationSettingsApplyResult, error) {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return DownloadStationSettingsApplyResult{}, fmt.Errorf("approval hash does not match the settings plan")
	}
	if plan.APIVersion != downloadStationAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return DownloadStationSettingsApplyResult{}, fmt.Errorf("invalid settings plan metadata")
	}
	if err := validateSettingsChangeShape(plan.Request); err != nil {
		return DownloadStationSettingsApplyResult{}, err
	}
	expectedHash, err := downloadStationSettingsPlanHash(plan)
	if err != nil {
		return DownloadStationSettingsApplyResult{}, err
	}
	if expectedHash != plan.Hash {
		return DownloadStationSettingsApplyResult{}, fmt.Errorf("settings plan contents were modified after planning")
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return DownloadStationSettingsApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return DownloadStationSettingsApplyResult{}, err
	}
	name, client, err := s.downloadStationSettingsClient(ctx, plan.NAS)
	if err != nil {
		return DownloadStationSettingsApplyResult{}, err
	}
	if name != plan.NAS {
		return DownloadStationSettingsApplyResult{}, fmt.Errorf("settings plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	current, err := planDownloadStationSettingsWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return DownloadStationSettingsApplyResult{}, fmt.Errorf("settings plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = downloadStationSettingsPlanHash(current)
	if err != nil {
		return DownloadStationSettingsApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return DownloadStationSettingsApplyResult{}, fmt.Errorf("settings plan is stale; create a new plan")
	}
	result, err := client.ApplyDownloadStationSettingsChange(ctx, plan.Request)
	if err != nil {
		return DownloadStationSettingsApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifySettingsGroupPostcondition(ctx, client, plan.Request); err != nil {
		return DownloadStationSettingsApplyResult{}, fmt.Errorf("verify settings change: %w", err)
	}
	return DownloadStationSettingsApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Result: result}, nil
}

// activeSettingsGroup returns the single group a change targets. Exactly one
// group patch must be present.
func activeSettingsGroup(change downloadstation.SettingsChange) (string, error) {
	groups := []string{}
	if change.BT != nil {
		groups = append(groups, "bt")
	}
	if change.FtpHttp != nil {
		groups = append(groups, "ftp_http")
	}
	switch len(groups) {
	case 0:
		return "", fmt.Errorf("settings change requires exactly one group patch (bt, ftp_http)")
	case 1:
		return groups[0], nil
	default:
		return "", fmt.Errorf("a settings change must target exactly one group, got %s", strings.Join(groups, ", "))
	}
}

func planDownloadStationSettingsWithClient(ctx context.Context, nas string, client downloadStationSettingsClient, request downloadstation.SettingsChange) (DownloadStationSettingsPlan, error) {
	group, err := activeSettingsGroup(request)
	if err != nil {
		return DownloadStationSettingsPlan{}, err
	}
	capabilities, _, err := client.DownloadStationCapabilities(ctx)
	if err != nil {
		return DownloadStationSettingsPlan{}, authenticationError(nas, err)
	}
	if !capabilities.SettingsRead || !capabilities.SettingsWrite {
		return DownloadStationSettingsPlan{}, fmt.Errorf("NAS %q does not expose a verified Download Station settings read/write backend", nas)
	}
	observed, err := client.DownloadStationSettingsGroup(ctx, group)
	if err != nil {
		return DownloadStationSettingsPlan{}, authenticationError(nas, err)
	}
	noop, risk, warnings, summary, err := settingsGroupEffects(group, request, observed)
	if err != nil {
		return DownloadStationSettingsPlan{}, err
	}
	if noop {
		return DownloadStationSettingsPlan{}, fmt.Errorf("settings patch would not change the current %s settings", group)
	}
	plan := DownloadStationSettingsPlan{APIVersion: downloadStationAPIVersion, NAS: nas, Request: request, Group: group, Observed: observed}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return DownloadStationSettingsPlan{}, err
	}
	plan.Risk, plan.Warnings, plan.Summary = risk, warnings, summary
	plan.Hash, err = downloadStationSettingsPlanHash(plan)
	if err != nil {
		return DownloadStationSettingsPlan{}, err
	}
	return plan, nil
}

// settingsGroupEffects unmarshals the observed group and returns the no-op flag,
// risk, warnings, and summary for the patch.
func settingsGroupEffects(group string, change downloadstation.SettingsChange, observed json.RawMessage) (bool, string, []string, []string, error) {
	switch group {
	case "bt":
		var current downloadstation.BTSettings
		if err := json.Unmarshal(observed, &current); err != nil {
			return false, "", nil, nil, fmt.Errorf("decode observed BT settings: %w", err)
		}
		risk, warnings, summary := btSettingsEffects(current, *change.BT)
		return btChangeIsNoOp(current, *change.BT), risk, warnings, summary, nil
	case "ftp_http":
		var current downloadstation.FtpHttpSettings
		if err := json.Unmarshal(observed, &current); err != nil {
			return false, "", nil, nil, fmt.Errorf("decode observed FTP/HTTP settings: %w", err)
		}
		risk, warnings, summary := ftpHttpSettingsEffects(current, *change.FtpHttp)
		return ftpHttpChangeIsNoOp(current, *change.FtpHttp), risk, warnings, summary, nil
	default:
		return false, "", nil, nil, fmt.Errorf("unsupported settings group %q", group)
	}
}

func verifySettingsGroupPostcondition(ctx context.Context, client downloadStationSettingsClient, change downloadstation.SettingsChange) error {
	group, err := activeSettingsGroup(change)
	if err != nil {
		return err
	}
	raw, err := client.DownloadStationSettingsGroup(ctx, group)
	if err != nil {
		return err
	}
	switch group {
	case "bt":
		var bt downloadstation.BTSettings
		if err := json.Unmarshal(raw, &bt); err != nil {
			return err
		}
		return verifyBTPostcondition(bt, *change.BT)
	case "ftp_http":
		var fh downloadstation.FtpHttpSettings
		if err := json.Unmarshal(raw, &fh); err != nil {
			return err
		}
		return verifyFtpHttpPostcondition(fh, *change.FtpHttp)
	default:
		return fmt.Errorf("unsupported settings group %q", group)
	}
}

func ftpHttpChangeIsNoOp(current downloadstation.FtpHttpSettings, patch downloadstation.FtpHttpSettingsChange) bool {
	return (patch.MaxDownloadRate == nil || *patch.MaxDownloadRate == current.MaxDownloadRate) &&
		(patch.EnableMaxConn == nil || *patch.EnableMaxConn == current.EnableMaxConn) &&
		(patch.MaxConn == nil || *patch.MaxConn == current.MaxConn)
}

func ftpHttpSettingsEffects(current downloadstation.FtpHttpSettings, patch downloadstation.FtpHttpSettingsChange) (string, []string, []string) {
	summary := []string{}
	if patch.MaxDownloadRate != nil && *patch.MaxDownloadRate != current.MaxDownloadRate {
		summary = append(summary, fmt.Sprintf("set the FTP/HTTP max download rate to %d KB/s", *patch.MaxDownloadRate))
	}
	if patch.EnableMaxConn != nil && *patch.EnableMaxConn != current.EnableMaxConn {
		summary = append(summary, fmt.Sprintf("set the per-task FTP connection limit to %t", *patch.EnableMaxConn))
	}
	if patch.MaxConn != nil && *patch.MaxConn != current.MaxConn {
		summary = append(summary, fmt.Sprintf("set max FTP connections per task to %d", *patch.MaxConn))
	}
	return "medium", []string{}, summary
}

func verifyFtpHttpPostcondition(current downloadstation.FtpHttpSettings, patch downloadstation.FtpHttpSettingsChange) error {
	if patch.MaxDownloadRate != nil && current.MaxDownloadRate != *patch.MaxDownloadRate {
		return fmt.Errorf("max_download_rate is %d, want %d", current.MaxDownloadRate, *patch.MaxDownloadRate)
	}
	if patch.EnableMaxConn != nil && current.EnableMaxConn != *patch.EnableMaxConn {
		return fmt.Errorf("enable_max_conn mismatch")
	}
	if patch.MaxConn != nil && current.MaxConn != *patch.MaxConn {
		return fmt.Errorf("max_conn is %d, want %d", current.MaxConn, *patch.MaxConn)
	}
	return nil
}

func validateSettingsChangeShape(change downloadstation.SettingsChange) error {
	group, err := activeSettingsGroup(change)
	if err != nil {
		return err
	}
	switch group {
	case "bt":
		return validateBTPatch(change.BT)
	case "ftp_http":
		return validateFtpHttpPatch(change.FtpHttp)
	default:
		return fmt.Errorf("unsupported settings group %q", group)
	}
}

func validateFtpHttpPatch(fh *downloadstation.FtpHttpSettingsChange) error {
	if fh.MaxDownloadRate == nil && fh.EnableMaxConn == nil && fh.MaxConn == nil {
		return fmt.Errorf("ftp_http settings patch has no fields")
	}
	if fh.MaxDownloadRate != nil && *fh.MaxDownloadRate < 0 {
		return fmt.Errorf("max_download_rate must not be negative")
	}
	if fh.MaxConn != nil && *fh.MaxConn < 1 {
		return fmt.Errorf("max_conn must be at least 1")
	}
	return nil
}

func validateBTPatch(bt *downloadstation.BTSettingsChange) error {
	if bt.TCPPort == nil && bt.DHTPort == nil && bt.EnableDHT == nil && bt.EnablePortForwarding == nil &&
		bt.EnablePreview == nil && bt.Encryption == nil && bt.MaxDownloadRate == nil && bt.MaxUploadRate == nil &&
		bt.MaxPeer == nil && bt.SeedingRatio == nil && bt.SeedingInterval == nil && bt.EnableSeedingAutoRemove == nil {
		return fmt.Errorf("bt settings patch has no fields")
	}
	for name, port := range map[string]*int{"tcp_port": bt.TCPPort, "dht_port": bt.DHTPort} {
		if port != nil && (*port < 1 || *port > 65535) {
			return fmt.Errorf("%s must be between 1 and 65535", name)
		}
	}
	if bt.Encryption != nil {
		switch strings.ToLower(strings.TrimSpace(*bt.Encryption)) {
		case "auto", "on", "off":
		default:
			return fmt.Errorf("encryption must be auto, on, or off")
		}
	}
	for name, rate := range map[string]*int{"max_download_rate": bt.MaxDownloadRate, "max_upload_rate": bt.MaxUploadRate, "seeding_ratio": bt.SeedingRatio, "seeding_interval": bt.SeedingInterval} {
		if rate != nil && *rate < 0 {
			return fmt.Errorf("%s must not be negative", name)
		}
	}
	if bt.MaxPeer != nil && *bt.MaxPeer < 1 {
		return fmt.Errorf("max_peer must be at least 1")
	}
	return nil
}

func btChangeIsNoOp(current synology.DownloadStationBTSettings, patch downloadstation.BTSettingsChange) bool {
	return (patch.TCPPort == nil || *patch.TCPPort == current.TCPPort) &&
		(patch.DHTPort == nil || *patch.DHTPort == current.DHTPort) &&
		(patch.EnableDHT == nil || *patch.EnableDHT == current.EnableDHT) &&
		(patch.EnablePortForwarding == nil || *patch.EnablePortForwarding == current.EnablePortForwarding) &&
		(patch.EnablePreview == nil || *patch.EnablePreview == current.EnablePreview) &&
		(patch.Encryption == nil || strings.EqualFold(*patch.Encryption, current.Encryption)) &&
		(patch.MaxDownloadRate == nil || *patch.MaxDownloadRate == current.MaxDownloadRate) &&
		(patch.MaxUploadRate == nil || *patch.MaxUploadRate == current.MaxUploadRate) &&
		(patch.MaxPeer == nil || *patch.MaxPeer == current.MaxPeer) &&
		(patch.SeedingRatio == nil || *patch.SeedingRatio == current.SeedingRatio) &&
		(patch.SeedingInterval == nil || *patch.SeedingInterval == current.SeedingInterval) &&
		(patch.EnableSeedingAutoRemove == nil || *patch.EnableSeedingAutoRemove == current.EnableSeedingAutoRemove)
}

func btSettingsEffects(current synology.DownloadStationBTSettings, patch downloadstation.BTSettingsChange) (string, []string, []string) {
	summary := []string{}
	warnings := []string{}
	high := false
	if patch.EnablePortForwarding != nil && *patch.EnablePortForwarding && !current.EnablePortForwarding {
		warnings = append(warnings, "enabling port forwarding opens the BitTorrent port on the router, increasing external exposure")
		high = true
	}
	if patch.TCPPort != nil && *patch.TCPPort != current.TCPPort {
		summary = append(summary, fmt.Sprintf("change the BitTorrent TCP port from %d to %d", current.TCPPort, *patch.TCPPort))
		warnings = append(warnings, "changing the listening port can interrupt active BitTorrent transfers")
	}
	if patch.DHTPort != nil && *patch.DHTPort != current.DHTPort {
		summary = append(summary, fmt.Sprintf("change the DHT port from %d to %d", current.DHTPort, *patch.DHTPort))
	}
	if patch.Encryption != nil && !strings.EqualFold(*patch.Encryption, current.Encryption) {
		summary = append(summary, fmt.Sprintf("set protocol encryption to %q", strings.ToLower(strings.TrimSpace(*patch.Encryption))))
	}
	if patch.MaxDownloadRate != nil && *patch.MaxDownloadRate != current.MaxDownloadRate {
		summary = append(summary, fmt.Sprintf("set the BT max download rate to %d KB/s", *patch.MaxDownloadRate))
	}
	if patch.MaxUploadRate != nil && *patch.MaxUploadRate != current.MaxUploadRate {
		summary = append(summary, fmt.Sprintf("set the BT max upload rate to %d KB/s", *patch.MaxUploadRate))
	}
	if patch.MaxPeer != nil && *patch.MaxPeer != current.MaxPeer {
		summary = append(summary, fmt.Sprintf("set max peers to %d", *patch.MaxPeer))
	}
	for label, cond := range map[string]bool{
		"toggle DHT":                    patch.EnableDHT != nil && *patch.EnableDHT != current.EnableDHT,
		"toggle download preview":       patch.EnablePreview != nil && *patch.EnablePreview != current.EnablePreview,
		"toggle seeding auto-remove":    patch.EnableSeedingAutoRemove != nil && *patch.EnableSeedingAutoRemove != current.EnableSeedingAutoRemove,
		"toggle port forwarding":        patch.EnablePortForwarding != nil && *patch.EnablePortForwarding != current.EnablePortForwarding,
		"change seeding ratio/interval": (patch.SeedingRatio != nil && *patch.SeedingRatio != current.SeedingRatio) || (patch.SeedingInterval != nil && *patch.SeedingInterval != current.SeedingInterval),
	} {
		if cond {
			summary = append(summary, label)
		}
	}
	risk := "medium"
	if high {
		risk = "high"
	}
	return risk, warnings, summary
}

func verifyBTPostcondition(bt synology.DownloadStationBTSettings, patch downloadstation.BTSettingsChange) error {
	if patch.TCPPort != nil && bt.TCPPort != *patch.TCPPort {
		return fmt.Errorf("tcp_port is %d, want %d", bt.TCPPort, *patch.TCPPort)
	}
	if patch.DHTPort != nil && bt.DHTPort != *patch.DHTPort {
		return fmt.Errorf("dht_port is %d, want %d", bt.DHTPort, *patch.DHTPort)
	}
	if patch.EnableDHT != nil && bt.EnableDHT != *patch.EnableDHT {
		return fmt.Errorf("enable_dht mismatch")
	}
	if patch.EnablePortForwarding != nil && bt.EnablePortForwarding != *patch.EnablePortForwarding {
		return fmt.Errorf("enable_port_forwarding mismatch")
	}
	if patch.EnablePreview != nil && bt.EnablePreview != *patch.EnablePreview {
		return fmt.Errorf("enable_preview mismatch")
	}
	if patch.Encryption != nil && !strings.EqualFold(bt.Encryption, strings.TrimSpace(*patch.Encryption)) {
		return fmt.Errorf("encryption is %q, want %q", bt.Encryption, *patch.Encryption)
	}
	if patch.MaxDownloadRate != nil && bt.MaxDownloadRate != *patch.MaxDownloadRate {
		return fmt.Errorf("max_download_rate is %d, want %d", bt.MaxDownloadRate, *patch.MaxDownloadRate)
	}
	if patch.MaxUploadRate != nil && bt.MaxUploadRate != *patch.MaxUploadRate {
		return fmt.Errorf("max_upload_rate is %d, want %d", bt.MaxUploadRate, *patch.MaxUploadRate)
	}
	if patch.MaxPeer != nil && bt.MaxPeer != *patch.MaxPeer {
		return fmt.Errorf("max_peer is %d, want %d", bt.MaxPeer, *patch.MaxPeer)
	}
	if patch.SeedingRatio != nil && bt.SeedingRatio != *patch.SeedingRatio {
		return fmt.Errorf("seeding_ratio mismatch")
	}
	if patch.SeedingInterval != nil && bt.SeedingInterval != *patch.SeedingInterval {
		return fmt.Errorf("seeding_interval mismatch")
	}
	if patch.EnableSeedingAutoRemove != nil && bt.EnableSeedingAutoRemove != *patch.EnableSeedingAutoRemove {
		return fmt.Errorf("enable_seeding_auto_remove mismatch")
	}
	return nil
}

func downloadStationSettingsPlanHash(plan DownloadStationSettingsPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

var _ downloadStationSettingsClient = (*synology.Client)(nil)
