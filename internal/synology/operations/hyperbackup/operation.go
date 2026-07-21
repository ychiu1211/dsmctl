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
	"strconv"
	"strings"

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
	RepositoryAPIName  = "SYNO.Backup.Repository"
	AppBackupAPIName   = "SYNO.Backup.App2.Backup"
	VersionAPIName     = "SYNO.Backup.Version"
	LogAPIName         = "SYNO.SDS.Backup.Client.Common.Log"
	VaultConfigAPIName = "SYNO.Backup.Service.VersionBackup.Config"
	VaultTargetAPIName = "SYNO.Backup.Service.VersionBackup.Target"
	LunAPIName         = "SYNO.Backup.Lunbackup"

	TaskReadCapabilityName        = "hyper_backup.task.read"
	DetailReadCapabilityName      = "hyper_backup.detail.read"
	VersionReadCapabilityName     = "hyper_backup.version.read"
	LogReadCapabilityName         = "hyper_backup.log.read"
	VaultReadCapabilityName       = "hyper_backup.vault.read"
	TaskRunCapabilityName         = "hyper_backup.task.run"
	TaskCreateCapabilityName      = "hyper_backup.task.create"
	AppReadCapabilityName         = "hyper_backup.application.read"
	LunReadCapabilityName         = "hyper_backup.lun.read"
	LunBackupCreateCapabilityName = "hyper_backup.lun.create"
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

// applicationsOperation lists the packages Hyper Backup can include in a
// backup task (SYNO.Backup.App2.Backup v2 list, live-verified on 4.2.2). The
// conservative support_app_share=false is sent so the answer never assumes a
// destination capability; per-destination eligibility is re-checked by DSM at
// create time.
var applicationsOperation = compatibility.Operation[Input, hyperbackup.Applications]{
	Name: AppReadCapabilityName,
	Variants: []compatibility.Variant[Input, hyperbackup.Applications]{
		{
			Name: "hyperbackup-application-list-v2", API: AppBackupAPIName, Version: 2, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(AppBackupAPIName, 2), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (hyperbackup.Applications, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: AppBackupAPIName, Version: 2, Method: "list", ReadOnly: true,
					JSONParameters: map[string]any{
						"app_config":        []any{},
						"support_app_share": false,
						"detailed_app_info": true,
					},
				})
				if err != nil {
					return hyperbackup.Applications{}, fmt.Errorf("call %s.list: %w", AppBackupAPIName, err)
				}
				return decodeApplications(data)
			},
		},
	},
}

func SelectApplications(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := applicationsOperation.Select(target)
	return selection, err
}

func ExecuteApplications(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (hyperbackup.Applications, compatibility.Selection, error) {
	return applicationsOperation.Run(ctx, target, executor, Input{})
}

// TaskCreateInput is the fully resolved input for the create operation. The
// application layer has already resolved the destination (profile or
// credential reference) into host/account/password; the password exists only
// in memory for the DSM calls and is never logged (the client's per-request
// log records names, not parameter values).
type TaskCreateInput struct {
	Spec hyperbackup.TaskCreate
	// Destination resolution. Host is empty for the local mode.
	Host     string
	Account  string
	Password string
	Share    string
	Port     int
	SSL      bool
}

// destinationParameters builds the destination half of every create-flow call
// (candidate-dir proposal, repository create, task create), live-verified on
// 4.2.2 for both image_local and image_remote.
func (input TaskCreateInput) destinationParameters() (map[string]any, []string) {
	if input.Host == "" {
		return map[string]any{
			"target_type":   "image",
			"transfer_type": "image_local",
			"share":         input.Share,
		}, nil
	}
	return map[string]any{
		"target_type":      "image",
		"transfer_type":    "image_remote",
		"dest":             input.Host,
		"port":             input.Port,
		"sslcheck":         input.SSL,
		"ssl_trust_mode":   "ignore",
		"verify_cert":      false,
		"is_webapi_authen": false,
		"account":          input.Account,
		"pwd":              input.Password,
		"share":            input.Share,
	}, []string{"pwd"}
}

var taskCreateOperation = compatibility.Operation[TaskCreateInput, hyperbackup.TaskMutationResult]{
	Name: TaskCreateCapabilityName,
	Variants: []compatibility.Variant[TaskCreateInput, hyperbackup.TaskMutationResult]{
		{
			Name: "hyperbackup-task-create-v1", API: TaskAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(
				compatibility.APIVersion(TaskAPIName, 1),
				compatibility.APIVersion(TargetAPIName, 1),
				baselinePackage,
			),
			Execute: func(ctx context.Context, executor compatibility.Executor, input TaskCreateInput) (hyperbackup.TaskMutationResult, error) {
				destination, secretNames := input.destinationParameters()
				withDestination := func(extra map[string]any) map[string]any {
					merged := make(map[string]any, len(destination)+len(extra))
					for k, v := range destination {
						merged[k] = v
					}
					for k, v := range extra {
						merged[k] = v
					}
					return merged
				}
				// The candidate-dir proposal doubles as the destination check:
				// it authenticates against a remote vault (or validates the
				// local share) before anything is created.
				candidateData, err := executor.Execute(ctx, compatibility.Request{
					API: TargetAPIName, Version: 1, Method: "get_candidate_dir",
					JSONParameters: destination, EncryptedParameters: secretNames,
				})
				if err != nil {
					return hyperbackup.TaskMutationResult{}, fmt.Errorf("call %s.get_candidate_dir (destination check): %w", TargetAPIName, err)
				}
				directory, err := decodeCandidateDir(candidateData)
				if err != nil {
					return hyperbackup.TaskMutationResult{}, err
				}
				if input.Spec.Directory != "" {
					directory = input.Spec.Directory
				}
				repositoryData, err := executor.Execute(ctx, compatibility.Request{
					API: RepositoryAPIName, Version: 1, Method: "create",
					JSONParameters: withDestination(map[string]any{
						"target_id": directory,
						"name":      input.Spec.TaskName,
					}),
					EncryptedParameters: secretNames,
				})
				if err != nil {
					return hyperbackup.TaskMutationResult{}, fmt.Errorf("call %s.create: %w", RepositoryAPIName, err)
				}
				repositoryID, err := decodeRepositoryCreate(repositoryData)
				if err != nil {
					return hyperbackup.TaskMutationResult{}, err
				}
				folders := input.Spec.SourceFolders
				if folders == nil {
					folders = []string{}
				}
				applications := input.Spec.Applications
				if applications == nil {
					applications = []string{}
				}
				source := map[string]any{
					"app_list":  applications,
					"file_list": folders,
				}
				if len(applications) > 0 {
					// app_config rides along only for application backups
					// (both source shapes live-verified on 4.2.2).
					source["app_config"] = []any{}
				}
				taskData, err := executor.Execute(ctx, compatibility.Request{
					API: TaskAPIName, Version: 1, Method: "create",
					JSONParameters: withDestination(map[string]any{
						"action":    "create",
						"name":      input.Spec.TaskName,
						"repo_id":   repositoryID,
						"target_id": directory,
						"source":    source,
						"backup_params": map[string]any{
							"enable_data_compress": input.Spec.Compression,
							"enable_notify":        input.Spec.Notify,
						},
					}),
					EncryptedParameters: secretNames,
				})
				if err != nil {
					return hyperbackup.TaskMutationResult{}, fmt.Errorf("call %s.create: %w", TaskAPIName, err)
				}
				taskID, err := decodeTaskCreate(taskData)
				if err != nil {
					return hyperbackup.TaskMutationResult{}, err
				}
				return hyperbackup.TaskMutationResult{
					API: TaskAPIName, Version: 1, Method: "create",
					TaskID: taskID, RepositoryID: repositoryID, Directory: directory,
				}, nil
			},
		},
	},
}

func SelectTaskCreate(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := taskCreateOperation.Select(target)
	return selection, err
}

func ExecuteTaskCreate(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input TaskCreateInput) (hyperbackup.TaskMutationResult, compatibility.Selection, error) {
	result, selection, err := taskCreateOperation.Run(ctx, target, executor, input)
	if err == nil {
		result.Backend = selection.Backend
	}
	return result, selection, err
}

// ---- legacy LUN backup (SYNO.Backup.Lunbackup) — file/regular LUNs ----

var lunsOperation = compatibility.Operation[Input, hyperbackup.Luns]{
	Name: LunReadCapabilityName,
	Variants: []compatibility.Variant[Input, hyperbackup.Luns]{{
		Name: "hyperbackup-lun-enum-v1", API: LunAPIName, Version: 1, Priority: 10,
		Match: compatibility.All(compatibility.APIVersion(LunAPIName, 1), baselinePackage),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (hyperbackup.Luns, error) {
			data, err := executor.Execute(ctx, compatibility.Request{API: LunAPIName, Version: 1, Method: "enum_lun", ReadOnly: true})
			if err != nil {
				return hyperbackup.Luns{}, fmt.Errorf("call %s.enum_lun: %w", LunAPIName, err)
			}
			return decodeLuns(data)
		},
	}},
}

// lunBackupTasksOperation lists the LUN backup tasks from the Task list,
// keeping only loclunbkp/netlunbkp entries (whose task_id is the name string).
var lunBackupTasksOperation = compatibility.Operation[Input, hyperbackup.LunBackupTasks]{
	Name: LunReadCapabilityName,
	Variants: []compatibility.Variant[Input, hyperbackup.LunBackupTasks]{{
		Name: "hyperbackup-lun-tasks-v1", API: TaskAPIName, Version: 1, Priority: 10,
		Match: compatibility.All(compatibility.APIVersion(TaskAPIName, 1), compatibility.APIVersion(LunAPIName, 1), baselinePackage),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (hyperbackup.LunBackupTasks, error) {
			data, err := executor.Execute(ctx, compatibility.Request{
				API: TaskAPIName, Version: 1, Method: "list", ReadOnly: true,
				JSONParameters: map[string]any{"additional": []string{"last_bkp_result"}},
			})
			if err != nil {
				return hyperbackup.LunBackupTasks{}, fmt.Errorf("call %s.list: %w", TaskAPIName, err)
			}
			return decodeLunBackupTasks(data)
		},
	}},
}

// lunBackupCreateOperation creates a local LUN backup (loclunbkp) via apply_lun.
// backup-now is apply_lun's own bkpnow flag (create-and-immediately-back-up);
// the standalone bkpnow method is a no-op in 4.2.2, live-verified.
var lunBackupCreateOperation = compatibility.Operation[hyperbackup.LunBackupCreate, hyperbackup.LunBackupMutationResult]{
	Name: LunBackupCreateCapabilityName,
	Variants: []compatibility.Variant[hyperbackup.LunBackupCreate, hyperbackup.LunBackupMutationResult]{{
		Name: "hyperbackup-lun-create-v1", API: LunAPIName, Version: 1, Priority: 10,
		Match: compatibility.All(compatibility.APIVersion(LunAPIName, 1), baselinePackage),
		Execute: func(ctx context.Context, executor compatibility.Executor, spec hyperbackup.LunBackupCreate) (hyperbackup.LunBackupMutationResult, error) {
			directory := strings.TrimSpace(spec.Directory)
			if directory == "" {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: LunAPIName, Version: 1, Method: "get_local_dest_dir",
					JSONParameters: map[string]any{"bkpShare": spec.DestinationShare},
				})
				if err != nil {
					return hyperbackup.LunBackupMutationResult{}, fmt.Errorf("call %s.get_local_dest_dir: %w", LunAPIName, err)
				}
				if directory, err = decodeDefaultDirectory(data); err != nil {
					return hyperbackup.LunBackupMutationResult{}, err
				}
			}
			params := map[string]any{
				"mode":           "create",
				"oldbkpset":      "",
				"newbkpset":      spec.TaskName,
				"bkptype":        "loclunbkp",
				"lunsource":      spec.LunSource,
				"lunsize":        strconv.FormatInt(spec.SizeBytes, 10),
				"scheduleEnable": false,
				"dest":           spec.DestinationShare + "/" + directory,
				"desttype":       "locallun",
				"bkpnow":         spec.BackupNow,
			}
			if _, err := executor.Execute(ctx, compatibility.Request{API: LunAPIName, Version: 1, Method: "apply_lun", JSONParameters: params}); err != nil {
				return hyperbackup.LunBackupMutationResult{}, fmt.Errorf("call %s.apply_lun: %w", LunAPIName, err)
			}
			return hyperbackup.LunBackupMutationResult{
				API: LunAPIName, Version: 1, Method: "apply_lun",
				TaskName: spec.TaskName, DestinationDir: directory, BackedUp: spec.BackupNow,
			}, nil
		},
	}},
}

func SelectLuns(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := lunsOperation.Select(target)
	return selection, err
}

func ExecuteLuns(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (hyperbackup.Luns, compatibility.Selection, error) {
	return lunsOperation.Run(ctx, target, executor, Input{})
}

func ExecuteLunBackupTasks(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (hyperbackup.LunBackupTasks, compatibility.Selection, error) {
	return lunBackupTasksOperation.Run(ctx, target, executor, Input{})
}

func SelectLunBackupCreate(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := lunBackupCreateOperation.Select(target)
	return selection, err
}

func ExecuteLunBackupCreate(ctx context.Context, target compatibility.Target, executor compatibility.Executor, spec hyperbackup.LunBackupCreate) (hyperbackup.LunBackupMutationResult, compatibility.Selection, error) {
	result, selection, err := lunBackupCreateOperation.Run(ctx, target, executor, spec)
	if err == nil {
		result.Backend = selection.Backend
	}
	return result, selection, err
}

// ExecuteLunBackupTaskStatus reads one LUN backup task's live status via the
// legacy load_task call (used to bind/verify a create plan; LUN tasks are a
// separate space from the image Task API).
func ExecuteLunBackupTaskStatus(ctx context.Context, target compatibility.Target, executor compatibility.Executor, taskName string) (hyperbackup.LunBackupTask, compatibility.Selection, error) {
	op := compatibility.Operation[Input, hyperbackup.LunBackupTask]{
		Name: LunReadCapabilityName,
		Variants: []compatibility.Variant[Input, hyperbackup.LunBackupTask]{{
			Name: "hyperbackup-lun-load-task-v1", API: LunAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(LunAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, ex compatibility.Executor, _ Input) (hyperbackup.LunBackupTask, error) {
				data, err := ex.Execute(ctx, compatibility.Request{
					API: LunAPIName, Version: 1, Method: "load_task", ReadOnly: true,
					JSONParameters: map[string]any{"taskName": taskName},
				})
				if err != nil {
					return hyperbackup.LunBackupTask{}, fmt.Errorf("call %s.load_task: %w", LunAPIName, err)
				}
				return decodeLunBackupTaskStatus(data)
			},
		}},
	}
	return op.Run(ctx, target, executor, Input{})
}

func APINames() []string {
	return []string{
		TaskAPIName, TargetAPIName, RepositoryAPIName, AppBackupAPIName,
		VersionAPIName, LogAPIName, VaultConfigAPIName, VaultTargetAPIName, LunAPIName,
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
