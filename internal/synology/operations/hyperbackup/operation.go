// Package hyperbackup implements the Hyper Backup module: the backup task
// list, task detail (status, repository, destination reachability), version
// list, and the log feed from the HyperBackup client package, the Vault view
// from the HyperBackupVault package, and the guarded run/cancel task actions.
// Client-side operations are gated on the installed HyperBackup package and
// the vault view on HyperBackupVault, so a NAS without the package fails
// closed. Every SYNO.Backup.* API is an entry.cgi JSON-request API, so all
// parameters are sent as JSON literals (JSONParameters), never bare form
// strings. Wire shapes live-verified on HyperBackup 4.2.2-4262 (2026-07-21).
package hyperbackup

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/hyperbackup"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// PackageID is the DSM package that owns the Hyper Backup client APIs.
const PackageID = "HyperBackup"

// VaultPackageID is the DSM package that owns the Hyper Backup Vault APIs.
const VaultPackageID = "HyperBackupVault"

const (
	TaskAPIName        = "SYNO.Backup.Task"
	TargetAPIName      = "SYNO.Backup.Target"
	VersionAPIName     = "SYNO.Backup.Version"
	LogAPIName         = "SYNO.SDS.Backup.Client.Common.Log"
	VaultConfigAPIName = "SYNO.Backup.Service.VersionBackup.Config"
	VaultTargetAPIName = "SYNO.Backup.Service.VersionBackup.Target"

	TaskReadCapabilityName    = "hyper_backup.task.read"
	DetailReadCapabilityName  = "hyper_backup.detail.read"
	VersionReadCapabilityName = "hyper_backup.version.read"
	LogReadCapabilityName     = "hyper_backup.log.read"
	VaultReadCapabilityName   = "hyper_backup.vault.read"
	TaskRunCapabilityName     = "hyper_backup.task.run"
)

// baselinePackage gates the client-side variants on Hyper Backup 4.x, the
// generation whose wire shapes are live-verified (4.2.2). Older DSM 6 era
// packages fail closed rather than receiving untested requests.
var baselinePackage = compatibility.PackageVersionRange(
	PackageID, compatibility.ParsePackageVersion("4.0"), compatibility.PackageVersion{},
)

// baselineVaultPackage gates the vault view on Hyper Backup Vault 4.x.
var baselineVaultPackage = compatibility.PackageVersionRange(
	VaultPackageID, compatibility.ParsePackageVersion("4.0"), compatibility.PackageVersion{},
)

type Input struct{}

// taskListAdditional is the live-verified additional set for the task list.
// get_source expands the per-task source (folders and applications).
var taskListAdditional = []string{
	"last_bkp_time", "next_bkp_time", "last_bkp_result", "is_modified", "schedule", "get_source",
}

var tasksOperation = compatibility.Operation[Input, hyperbackup.Tasks]{
	Name: TaskReadCapabilityName,
	Variants: []compatibility.Variant[Input, hyperbackup.Tasks]{
		{
			Name: "hyperbackup-task-list-v1", API: TaskAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(TaskAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (hyperbackup.Tasks, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: TaskAPIName, Version: 1, Method: "list", ReadOnly: true,
					JSONParameters: map[string]any{"sort_by": "name", "additional": taskListAdditional},
				})
				if err != nil {
					return hyperbackup.Tasks{}, fmt.Errorf("call %s.list: %w", TaskAPIName, err)
				}
				return decodeTasks(data)
			},
		},
	},
}

// DetailInput selects the task whose detail is read.
type DetailInput struct {
	TaskID int
}

var detailOperation = compatibility.Operation[DetailInput, hyperbackup.TaskDetail]{
	Name: DetailReadCapabilityName,
	Variants: []compatibility.Variant[DetailInput, hyperbackup.TaskDetail]{
		{
			Name: "hyperbackup-task-detail-v1", API: TaskAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(
				compatibility.APIVersion(TaskAPIName, 1),
				compatibility.APIVersion(TargetAPIName, 1),
				baselinePackage,
			),
			Execute: func(ctx context.Context, executor compatibility.Executor, input DetailInput) (hyperbackup.TaskDetail, error) {
				getData, err := executor.Execute(ctx, compatibility.Request{
					API: TaskAPIName, Version: 1, Method: "get", ReadOnly: true,
					JSONParameters: map[string]any{
						"task_id":    input.TaskID,
						"additional": []string{"backup_params", "rotate_params", "schedule", "repository"},
					},
				})
				if err != nil {
					return hyperbackup.TaskDetail{}, fmt.Errorf("call %s.get: %w", TaskAPIName, err)
				}
				detail, err := decodeTaskDetail(getData)
				if err != nil {
					return hyperbackup.TaskDetail{}, err
				}
				status, err := executeStatus(ctx, executor, input.TaskID)
				if err != nil {
					return hyperbackup.TaskDetail{}, err
				}
				detail.Status = status
				detail.Task.State = status.State
				detail.Task.Status = status.Status
				detail.Task.LastBackupTime = status.LastBackupTime
				detail.Task.LastBackupEnd = status.LastBackupEnd
				detail.Task.LastBackupResult = status.LastBackupResult
				detail.Task.NextBackupTime = status.NextBackupTime
				targetData, err := executor.Execute(ctx, compatibility.Request{
					API: TargetAPIName, Version: 1, Method: "get", ReadOnly: true,
					JSONParameters: map[string]any{
						"task_id":    input.TaskID,
						"additional": []string{"is_online", "check_task_key", "check_auth"},
					},
				})
				if err != nil {
					return hyperbackup.TaskDetail{}, fmt.Errorf("call %s.get: %w", TargetAPIName, err)
				}
				target, err := decodeTarget(targetData)
				if err != nil {
					return hyperbackup.TaskDetail{}, err
				}
				detail.Target = target
				return detail, nil
			},
		},
	},
}

// executeStatus reads one task's live status (shared by the detail read and
// the run/cancel action, which needs the current state and a postcondition).
func executeStatus(ctx context.Context, executor compatibility.Executor, taskID int) (hyperbackup.TaskStatus, error) {
	data, err := executor.Execute(ctx, compatibility.Request{
		API: TaskAPIName, Version: 1, Method: "status", ReadOnly: true,
		JSONParameters: map[string]any{
			"task_id":    taskID,
			"blOnline":   true,
			"additional": []string{"last_bkp_time", "next_bkp_time", "last_bkp_result", "is_modified"},
		},
	})
	if err != nil {
		return hyperbackup.TaskStatus{}, fmt.Errorf("call %s.status: %w", TaskAPIName, err)
	}
	return decodeTaskStatus(data)
}

// VersionsInput selects the task and page of versions to list.
type VersionsInput struct {
	TaskID int
	Offset int
	Limit  int
}

var versionsOperation = compatibility.Operation[VersionsInput, hyperbackup.Versions]{
	Name: VersionReadCapabilityName,
	Variants: []compatibility.Variant[VersionsInput, hyperbackup.Versions]{
		{
			// Version list exists from API version 2; v1 has no list method
			// (live-verified error 103), so the operation requires v2.
			Name: "hyperbackup-version-list-v2", API: VersionAPIName, Version: 2, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(VersionAPIName, 2), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, input VersionsInput) (hyperbackup.Versions, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: VersionAPIName, Version: 2, Method: "list", ReadOnly: true,
					JSONParameters: map[string]any{
						"task_id": input.TaskID,
						"offset":  input.Offset,
						"limit":   input.Limit,
					},
				})
				if err != nil {
					return hyperbackup.Versions{}, fmt.Errorf("call %s.list: %w", VersionAPIName, err)
				}
				versions, err := decodeVersions(data)
				if err != nil {
					return hyperbackup.Versions{}, err
				}
				versions.TaskID = input.TaskID
				return versions, nil
			},
		},
	},
}

// LogsInput selects the page of the log feed to read.
type LogsInput struct {
	Offset int
	Limit  int
}

var logsOperation = compatibility.Operation[LogsInput, hyperbackup.Logs]{
	Name: LogReadCapabilityName,
	Variants: []compatibility.Variant[LogsInput, hyperbackup.Logs]{
		{
			Name: "hyperbackup-log-list-v1", API: LogAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(LogAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, input LogsInput) (hyperbackup.Logs, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: LogAPIName, Version: 1, Method: "list", ReadOnly: true,
					JSONParameters: map[string]any{"offset": input.Offset, "limit": input.Limit},
				})
				if err != nil {
					return hyperbackup.Logs{}, fmt.Errorf("call %s.list: %w", LogAPIName, err)
				}
				return decodeLogs(data)
			},
		},
	},
}

var vaultOperation = compatibility.Operation[Input, hyperbackup.Vault]{
	Name: VaultReadCapabilityName,
	Variants: []compatibility.Variant[Input, hyperbackup.Vault]{
		{
			Name: "hyperbackupvault-view-v1", API: VaultTargetAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(
				compatibility.APIVersion(VaultTargetAPIName, 1),
				compatibility.APIVersion(VaultConfigAPIName, 1),
				baselineVaultPackage,
			),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (hyperbackup.Vault, error) {
				configData, err := executor.Execute(ctx, compatibility.Request{
					API: VaultConfigAPIName, Version: 1, Method: "get", ReadOnly: true,
				})
				if err != nil {
					return hyperbackup.Vault{}, fmt.Errorf("call %s.get: %w", VaultConfigAPIName, err)
				}
				vault, err := decodeVaultConfig(configData)
				if err != nil {
					return hyperbackup.Vault{}, err
				}
				targetData, err := executor.Execute(ctx, compatibility.Request{
					API: VaultTargetAPIName, Version: 1, Method: "list", ReadOnly: true,
				})
				if err != nil {
					return hyperbackup.Vault{}, fmt.Errorf("call %s.list: %w", VaultTargetAPIName, err)
				}
				vault.Targets, err = decodeVaultTargets(targetData)
				if err != nil {
					return hyperbackup.Vault{}, err
				}
				return vault, nil
			},
		},
	},
}

// taskRunOperation performs a guarded task action: backup (run now) or cancel.
// Cancel reads the task's current state first because the cancel method wants
// the task_state alongside the id (live-verified on 4.2.2).
var taskRunOperation = compatibility.Operation[hyperbackup.TaskChange, hyperbackup.TaskMutationResult]{
	Name: TaskRunCapabilityName,
	Variants: []compatibility.Variant[hyperbackup.TaskChange, hyperbackup.TaskMutationResult]{
		{
			Name: "hyperbackup-task-run-v1", API: TaskAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(TaskAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, change hyperbackup.TaskChange) (hyperbackup.TaskMutationResult, error) {
				result := hyperbackup.TaskMutationResult{API: TaskAPIName, Version: 1, TaskID: change.TaskID}
				switch change.Action {
				case hyperbackup.TaskActionBackup:
					if _, err := executor.Execute(ctx, compatibility.Request{
						API: TaskAPIName, Version: 1, Method: "backup",
						JSONParameters: map[string]any{"task_id": change.TaskID},
					}); err != nil {
						return hyperbackup.TaskMutationResult{}, fmt.Errorf("call %s.backup: %w", TaskAPIName, err)
					}
					result.Method = "backup"
					return result, nil
				case hyperbackup.TaskActionCancel:
					status, err := executeStatus(ctx, executor, change.TaskID)
					if err != nil {
						return hyperbackup.TaskMutationResult{}, err
					}
					if _, err := executor.Execute(ctx, compatibility.Request{
						API: TaskAPIName, Version: 1, Method: "cancel",
						JSONParameters: map[string]any{"task_id": change.TaskID, "task_state": status.State},
					}); err != nil {
						return hyperbackup.TaskMutationResult{}, fmt.Errorf("call %s.cancel: %w", TaskAPIName, err)
					}
					result.Method = "cancel"
					return result, nil
				default:
					return hyperbackup.TaskMutationResult{}, fmt.Errorf("unsupported task action %q", change.Action)
				}
			},
		},
	},
}

func APINames() []string {
	return []string{
		TaskAPIName, TargetAPIName, VersionAPIName, LogAPIName,
		VaultConfigAPIName, VaultTargetAPIName,
	}
}

func SelectTasks(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := tasksOperation.Select(target)
	return selection, err
}

func ExecuteTasks(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (hyperbackup.Tasks, compatibility.Selection, error) {
	return tasksOperation.Run(ctx, target, executor, Input{})
}

func SelectDetail(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := detailOperation.Select(target)
	return selection, err
}

func ExecuteDetail(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input DetailInput) (hyperbackup.TaskDetail, compatibility.Selection, error) {
	return detailOperation.Run(ctx, target, executor, input)
}

func SelectVersions(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := versionsOperation.Select(target)
	return selection, err
}

func ExecuteVersions(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input VersionsInput) (hyperbackup.Versions, compatibility.Selection, error) {
	return versionsOperation.Run(ctx, target, executor, input)
}

func SelectLogs(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := logsOperation.Select(target)
	return selection, err
}

func ExecuteLogs(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input LogsInput) (hyperbackup.Logs, compatibility.Selection, error) {
	return logsOperation.Run(ctx, target, executor, input)
}

func SelectVault(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := vaultOperation.Select(target)
	return selection, err
}

func ExecuteVault(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (hyperbackup.Vault, compatibility.Selection, error) {
	return vaultOperation.Run(ctx, target, executor, Input{})
}

func SelectTaskRun(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := taskRunOperation.Select(target)
	return selection, err
}

func ExecuteTaskRun(ctx context.Context, target compatibility.Target, executor compatibility.Executor, change hyperbackup.TaskChange) (hyperbackup.TaskMutationResult, compatibility.Selection, error) {
	result, selection, err := taskRunOperation.Run(ctx, target, executor, change)
	if err == nil {
		result.Backend = selection.Backend
	}
	return result, selection, err
}

// ExecuteTaskStatus reads one task's live status without the rest of the
// detail view. The application layer uses it to bind and verify run/cancel
// plans against the live task state.
func ExecuteTaskStatus(ctx context.Context, target compatibility.Target, executor compatibility.Executor, taskID int) (hyperbackup.TaskStatus, compatibility.Selection, error) {
	statusOperation := compatibility.Operation[DetailInput, hyperbackup.TaskStatus]{
		Name: DetailReadCapabilityName,
		Variants: []compatibility.Variant[DetailInput, hyperbackup.TaskStatus]{{
			Name: "hyperbackup-task-status-v1", API: TaskAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(TaskAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, input DetailInput) (hyperbackup.TaskStatus, error) {
				return executeStatus(ctx, executor, input.TaskID)
			},
		}},
	}
	return statusOperation.Run(ctx, target, executor, DetailInput{TaskID: taskID})
}
