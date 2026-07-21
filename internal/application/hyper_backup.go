package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/hyperbackup"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const hyperBackupAPIVersion = "dsmctl.io/v1alpha1"

type HyperBackupCapabilitiesResult struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.HyperBackupCapabilities `json:"capabilities" jsonschema:"Hyper Backup reads and actions currently exposed by dsmctl"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected Hyper Backup backends"`
}

type HyperBackupTasksResult struct {
	NAS   string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	Tasks synology.HyperBackupTasks `json:"tasks" jsonschema:"Backup task list"`
}

type HyperBackupTaskDetailResult struct {
	NAS  string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Task synology.HyperBackupTaskDetail `json:"task" jsonschema:"Full task view: repository, transfer options, live status, destination reachability"`
}

type HyperBackupVersionsResult struct {
	NAS      string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Versions synology.HyperBackupVersions `json:"versions" jsonschema:"Backup versions of the task"`
}

type HyperBackupLogsResult struct {
	NAS  string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Logs synology.HyperBackupLogs `json:"logs" jsonschema:"Hyper Backup log feed page"`
}

type HyperBackupVaultResult struct {
	NAS   string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	Vault synology.HyperBackupVault `json:"vault" jsonschema:"Hyper Backup Vault view of this NAS as a backup destination"`
}

func (s *Service) GetHyperBackupCapabilities(ctx context.Context, requestedNAS string) (HyperBackupCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return HyperBackupCapabilitiesResult{}, err
	}
	capabilities, report, err := client.HyperBackupCapabilities(ctx)
	if err != nil {
		return HyperBackupCapabilitiesResult{}, authenticationError(name, err)
	}
	return HyperBackupCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetHyperBackupTasks(ctx context.Context, requestedNAS string) (HyperBackupTasksResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return HyperBackupTasksResult{}, err
	}
	tasks, err := client.HyperBackupTasks(ctx)
	if err != nil {
		return HyperBackupTasksResult{}, authenticationError(name, err)
	}
	return HyperBackupTasksResult{NAS: name, Tasks: tasks}, nil
}

func (s *Service) GetHyperBackupTaskDetail(ctx context.Context, requestedNAS string, taskID int) (HyperBackupTaskDetailResult, error) {
	if taskID <= 0 {
		return HyperBackupTaskDetailResult{}, fmt.Errorf("task_id must be a positive task identifier")
	}
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return HyperBackupTaskDetailResult{}, err
	}
	detail, err := client.HyperBackupTaskDetail(ctx, taskID)
	if err != nil {
		return HyperBackupTaskDetailResult{}, authenticationError(name, err)
	}
	return HyperBackupTaskDetailResult{NAS: name, Task: detail}, nil
}

func (s *Service) GetHyperBackupVersions(ctx context.Context, requestedNAS string, taskID, offset, limit int) (HyperBackupVersionsResult, error) {
	if taskID <= 0 {
		return HyperBackupVersionsResult{}, fmt.Errorf("task_id must be a positive task identifier")
	}
	if offset < 0 || limit < 0 {
		return HyperBackupVersionsResult{}, fmt.Errorf("offset and limit must not be negative")
	}
	if limit == 0 {
		limit = 50
	}
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return HyperBackupVersionsResult{}, err
	}
	versions, err := client.HyperBackupVersions(ctx, taskID, offset, limit)
	if err != nil {
		return HyperBackupVersionsResult{}, authenticationError(name, err)
	}
	return HyperBackupVersionsResult{NAS: name, Versions: versions}, nil
}

func (s *Service) GetHyperBackupLogs(ctx context.Context, requestedNAS string, offset, limit int) (HyperBackupLogsResult, error) {
	if offset < 0 || limit < 0 {
		return HyperBackupLogsResult{}, fmt.Errorf("offset and limit must not be negative")
	}
	if limit == 0 {
		limit = 50
	}
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return HyperBackupLogsResult{}, err
	}
	logs, err := client.HyperBackupLogs(ctx, offset, limit)
	if err != nil {
		return HyperBackupLogsResult{}, authenticationError(name, err)
	}
	return HyperBackupLogsResult{NAS: name, Logs: logs}, nil
}

func (s *Service) GetHyperBackupVault(ctx context.Context, requestedNAS string) (HyperBackupVaultResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return HyperBackupVaultResult{}, err
	}
	vault, err := client.HyperBackupVault(ctx)
	if err != nil {
		return HyperBackupVaultResult{}, authenticationError(name, err)
	}
	return HyperBackupVaultResult{NAS: name, Vault: vault}, nil
}

// HyperBackupTaskSummary is a stable-field projection of the target task,
// bound into a run/cancel plan so an apply fails when the task has since
// changed state (it binds the live activity, not volatile progress counters).
type HyperBackupTaskSummary struct {
	TaskID           int    `json:"task_id" jsonschema:"Task identifier"`
	Name             string `json:"name,omitempty" jsonschema:"Task display name"`
	State            string `json:"state,omitempty" jsonschema:"Task lifecycle state observed during planning"`
	Status           string `json:"status,omitempty" jsonschema:"Live activity observed during planning"`
	LastBackupTime   string `json:"last_backup_time,omitempty" jsonschema:"Start time of the last run observed during planning"`
	LastBackupResult string `json:"last_backup_result,omitempty" jsonschema:"Result of the last run observed during planning"`
}

type HyperBackupTaskPlan struct {
	APIVersion          string                  `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                  `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                  `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             hyperbackup.TaskChange  `json:"request" jsonschema:"Validated task action intent"`
	Observed            HyperBackupTaskSummary  `json:"observed" jsonschema:"Target task observed during planning"`
	ObservedFingerprint string                  `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the observed target task"`
	Risk                string                  `json:"risk" jsonschema:"Plan risk level"`
	Warnings            []string                `json:"warnings" jsonschema:"Operational warnings"`
	Summary             []string                `json:"summary" jsonschema:"Human-readable operations"`
	Hash                string                  `json:"hash" jsonschema:"SHA-256 approval hash covering intent and observed task state"`
}

type HyperBackupTaskApplyResult struct {
	NAS      string                                 `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                                 `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied  bool                                   `json:"applied" jsonschema:"Whether DSM accepted the action and postcondition verification passed"`
	Result   synology.HyperBackupTaskMutationResult `json:"result" jsonschema:"Selected DSM mutation backend and target task"`
}

type hyperBackupTaskClient interface {
	HyperBackupTasks(context.Context) (synology.HyperBackupTasks, error)
	HyperBackupTaskStatus(context.Context, int) (synology.HyperBackupTaskStatus, error)
	HyperBackupCapabilities(context.Context) (synology.HyperBackupCapabilities, synology.CompatibilityReport, error)
	ApplyHyperBackupTaskChange(context.Context, synology.HyperBackupTaskChange) (synology.HyperBackupTaskMutationResult, error)
}

func (s *Service) hyperBackupTaskClient(ctx context.Context, requestedNAS string) (string, hyperBackupTaskClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(hyperBackupTaskClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement Hyper Backup task management")
	}
	return name, client, nil
}

func (s *Service) PlanHyperBackupTaskChange(ctx context.Context, requestedNAS string, request hyperbackup.TaskChange) (HyperBackupTaskPlan, error) {
	if err := validateHyperBackupTaskChangeShape(request); err != nil {
		return HyperBackupTaskPlan{}, err
	}
	name, client, err := s.hyperBackupTaskClient(ctx, requestedNAS)
	if err != nil {
		return HyperBackupTaskPlan{}, err
	}
	plan, err := planHyperBackupTaskWithClient(ctx, name, client, request)
	if err != nil {
		return HyperBackupTaskPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = hyperBackupTaskPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyHyperBackupTaskPlan(ctx context.Context, plan HyperBackupTaskPlan, approvalHash string) (HyperBackupTaskApplyResult, error) {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return HyperBackupTaskApplyResult{}, fmt.Errorf("approval hash does not match the task plan")
	}
	if plan.APIVersion != hyperBackupAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return HyperBackupTaskApplyResult{}, fmt.Errorf("invalid task plan metadata")
	}
	if err := validateHyperBackupTaskChangeShape(plan.Request); err != nil {
		return HyperBackupTaskApplyResult{}, err
	}
	expectedHash, err := hyperBackupTaskPlanHash(plan)
	if err != nil {
		return HyperBackupTaskApplyResult{}, err
	}
	if expectedHash != plan.Hash {
		return HyperBackupTaskApplyResult{}, fmt.Errorf("task plan contents were modified after planning")
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return HyperBackupTaskApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return HyperBackupTaskApplyResult{}, err
	}
	name, client, err := s.hyperBackupTaskClient(ctx, plan.NAS)
	if err != nil {
		return HyperBackupTaskApplyResult{}, err
	}
	if name != plan.NAS {
		return HyperBackupTaskApplyResult{}, fmt.Errorf("task plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	current, err := planHyperBackupTaskWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return HyperBackupTaskApplyResult{}, fmt.Errorf("task plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = hyperBackupTaskPlanHash(current)
	if err != nil {
		return HyperBackupTaskApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return HyperBackupTaskApplyResult{}, fmt.Errorf("task plan is stale; create a new plan")
	}
	result, err := client.ApplyHyperBackupTaskChange(ctx, plan.Request)
	if err != nil {
		return HyperBackupTaskApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyHyperBackupTaskPostcondition(ctx, client, plan.Request, plan.Observed); err != nil {
		return HyperBackupTaskApplyResult{}, fmt.Errorf("verify task action: %w", err)
	}
	return HyperBackupTaskApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Result: result}, nil
}

func planHyperBackupTaskWithClient(ctx context.Context, nas string, client hyperBackupTaskClient, request hyperbackup.TaskChange) (HyperBackupTaskPlan, error) {
	capabilities, _, err := client.HyperBackupCapabilities(ctx)
	if err != nil {
		return HyperBackupTaskPlan{}, authenticationError(nas, err)
	}
	if !capabilities.TaskRead || !capabilities.TaskRun {
		return HyperBackupTaskPlan{}, fmt.Errorf("NAS %q does not expose a verified Hyper Backup task read/run backend", nas)
	}
	tasks, err := client.HyperBackupTasks(ctx)
	if err != nil {
		return HyperBackupTaskPlan{}, authenticationError(nas, err)
	}
	var target *synology.HyperBackupTask
	for index := range tasks.Tasks {
		if tasks.Tasks[index].TaskID == request.TaskID {
			target = &tasks.Tasks[index]
			break
		}
	}
	if target == nil {
		return HyperBackupTaskPlan{}, fmt.Errorf("backup task %d was not found on NAS %q", request.TaskID, nas)
	}
	observed := HyperBackupTaskSummary{
		TaskID:           target.TaskID,
		Name:             target.Name,
		State:            target.State,
		Status:           target.Status,
		LastBackupTime:   target.LastBackupTime,
		LastBackupResult: target.LastBackupResult,
	}
	running := observed.Status != "" && observed.Status != "none"
	switch request.Action {
	case hyperbackup.TaskActionBackup:
		if running {
			return HyperBackupTaskPlan{}, fmt.Errorf("backup task %d (%s) is currently %s; wait for it to finish or cancel it first", observed.TaskID, observed.Name, observed.Status)
		}
		if observed.State != "backupable" {
			return HyperBackupTaskPlan{}, fmt.Errorf("backup task %d (%s) is in state %q and cannot run a backup", observed.TaskID, observed.Name, observed.State)
		}
	case hyperbackup.TaskActionCancel:
		if !running {
			return HyperBackupTaskPlan{}, fmt.Errorf("backup task %d (%s) has no running backup to cancel", observed.TaskID, observed.Name)
		}
	}
	plan := HyperBackupTaskPlan{APIVersion: hyperBackupAPIVersion, NAS: nas, Request: request, Observed: observed}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return HyperBackupTaskPlan{}, err
	}
	plan.Risk, plan.Warnings, plan.Summary = hyperBackupTaskEffects(request, observed)
	plan.Hash, err = hyperBackupTaskPlanHash(plan)
	if err != nil {
		return HyperBackupTaskPlan{}, err
	}
	return plan, nil
}

// validateHyperBackupTaskChangeShape rejects everything invalid regardless of
// NAS state.
func validateHyperBackupTaskChangeShape(change hyperbackup.TaskChange) error {
	switch change.Action {
	case hyperbackup.TaskActionBackup, hyperbackup.TaskActionCancel:
		if change.TaskID <= 0 {
			return fmt.Errorf("a %s action requires a positive task_id", change.Action)
		}
	default:
		return fmt.Errorf("unsupported task action %q; use backup or cancel", change.Action)
	}
	return nil
}

func hyperBackupTaskEffects(change hyperbackup.TaskChange, observed HyperBackupTaskSummary) (string, []string, []string) {
	switch change.Action {
	case hyperbackup.TaskActionBackup:
		return "medium",
			[]string{"running a backup reads the source data and writes a new version to the backup destination; transfer load depends on how much changed"},
			[]string{fmt.Sprintf("run backup task %d (%s) now", observed.TaskID, observed.Name)}
	case hyperbackup.TaskActionCancel:
		return "medium",
			[]string{"canceling stops the running backup; the interrupted run is recorded with result \"cancel\" and no new version is completed"},
			[]string{fmt.Sprintf("cancel the running backup of task %d (%s)", observed.TaskID, observed.Name)}
	default:
		return "high", []string{}, []string{}
	}
}

// verifyHyperBackupTaskPostcondition re-reads the task status after the
// action. A run is verified by the task actively backing up or, for a very
// fast run, by a fresh last-backup start time; a cancel by the task no longer
// actively backing up (canceling counts: DSM finishes the cancel async).
func verifyHyperBackupTaskPostcondition(ctx context.Context, client hyperBackupTaskClient, change hyperbackup.TaskChange, observed HyperBackupTaskSummary) error {
	status, err := client.HyperBackupTaskStatus(ctx, change.TaskID)
	if err != nil {
		return err
	}
	switch change.Action {
	case hyperbackup.TaskActionBackup:
		if status.Status == "backup" || status.Status == "waiting" {
			return nil
		}
		if status.LastBackupTime != "" && status.LastBackupTime != observed.LastBackupTime {
			return nil
		}
		return fmt.Errorf("task %d did not start backing up (status %q, last backup time unchanged)", change.TaskID, status.Status)
	case hyperbackup.TaskActionCancel:
		if status.Status == "backup" {
			return fmt.Errorf("task %d is still backing up after cancel", change.TaskID)
		}
		return nil
	default:
		return nil
	}
}

func hyperBackupTaskPlanHash(plan HyperBackupTaskPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

var _ hyperBackupTaskClient = (*synology.Client)(nil)
