package hyperbackup

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/hyperbackup"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type captureExecutor struct {
	requests  []compatibility.Request
	responses map[string]string
}

func (e *captureExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	e.requests = append(e.requests, request)
	if body, ok := e.responses[request.API+" "+request.Method]; ok {
		return json.RawMessage(body), nil
	}
	return json.RawMessage(`{}`), nil
}

type routeExecutor struct {
	t      *testing.T
	routes map[string]string
}

func (e routeExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	key := request.API + " " + request.Method
	body, ok := e.routes[key]
	if !ok {
		e.t.Fatalf("unexpected request %q", key)
	}
	return json.RawMessage(body), nil
}

func hbTarget(packageVersion, vaultVersion string) compatibility.Target {
	target := compatibility.NewTarget()
	for _, api := range []string{TaskAPIName, TargetAPIName} {
		target.SetAPI(api, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	}
	target.SetAPI(VersionAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(LogAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	target.SetAPI(VaultConfigAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(VaultTargetAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	installed := []compatibility.InstalledPackage{}
	if packageVersion != "" {
		installed = append(installed, compatibility.InstalledPackage{ID: PackageID, Version: compatibility.ParsePackageVersion(packageVersion), Running: true})
	}
	if vaultVersion != "" {
		installed = append(installed, compatibility.InstalledPackage{ID: VaultPackageID, Version: compatibility.ParsePackageVersion(vaultVersion), Running: true})
	}
	target.SetInstalledPackages(installed)
	return target
}

// Shapes below are the live payloads captured on HyperBackup 4.2.2-4262
// (2026-07-21), trimmed to the decoded fields plus representative extras.
const (
	tasksBody = `{"is_data_restoring":false,"is_restoring":false,"total":1,"task_list":[{
		"data_enc":false,"data_type":"data","is_modified":false,
		"last_bkp_end_time":"2026/07/21 01:21:37","last_bkp_result":"done","last_bkp_time":"2026/07/21 01:20:57",
		"name":"dsmctl-probe-task","repo_id":1,
		"source":{"app_config":[],"app_list":[],"app_name_list":[],
			"file_list":[{"folderPath":"/Share/dsmctl_probe_src","fullPath":"/volume1/Share/dsmctl_probe_src","isValidSource":true}],
			"share_list":{"Share":{"fileSystem":"BTRFS"}}},
		"state":"backupable","status":"none","target_id":"dsmctl_probe_1","target_type":"image",
		"task_id":1,"transfer_type":"image_local","type":"image:image_local"}]}`
	taskGetBody = `{
		"backup_params":{"enable_data_compress":false,"enable_data_encrypt":false,"enable_notify":false,"enable_version_file_log":false,"max_auto_resume_retry":5},
		"data_enc":false,"data_type":"data","name":"dsmctl-probe-task","repo_id":1,
		"repository":{"name":"dsmctl-probe-local","repo_id":1,"share":"Share","target_type":"image","transfer_type":"image_local"},
		"rotate_params":{},"state":"backupable","status":"none","target_id":"dsmctl_probe_1",
		"target_type":"image","task_id":1,"transfer_type":"image_local","type":"image:image_local"}`
	// Progress counters arrive as strings while progress/avg_speed are numbers.
	statusRunningBody = `{"is_modified":false,"last_bkp_error":"","last_bkp_error_code":4401,
		"last_bkp_result":"backingup","last_bkp_time":"","state":"backupable","status":"backup","task_id":1,
		"progress":{"avg_speed":1024,"can_cancel":true,"can_suspend":false,"counted_file_count":"3",
			"processed_size":"2048","progress":42,"scan_file_count":"3","show_progress":true,
			"step":"backup_data","title_type":"title_backuping","total_size":"4096","transmitted_size":"1024"}}`
	statusIdleBody = `{"is_modified":false,"last_bkp_end_time":"2026/07/21 01:21:37","last_bkp_error":"",
		"last_bkp_error_code":4401,"last_bkp_result":"done","last_bkp_success_time":"2026/07/21 01:21:37",
		"last_bkp_time":"2026/07/21 01:20:57","state":"backupable","status":"none","task_id":1}`
	targetBody = `{"capability":{"support_download":true},"data_comp":false,"data_enc":false,
		"format_type":"image","host_name":"Derek_3018xs","is_online":true,"last_detect_time":"",
		"owner_id":1026,"owner_name":"deryck","support_multi_version":true,"uni_key":"0011327DEB57_1_1784567984"}`
	versionsBody = `{"backup_data_type":"data","permit_delete":{"permitted":true},"total":1,
		"version_info_list":[{"complete_time":1784568084,"complete_time_local":"2026/07/21 01:21:24",
			"has_history":true,"locked":false,"modify":"0","name":"2026/07/21 01:20:58","permit_delete":true,
			"start_time_local":"2026/07/21 01:20:58","status":"success","timestamp":1784568058,"version_id":"1"}]}`
	logsBody = `{"error_count":0,"info_count":3,"offset":3,"total":3,"warn_count":0,"log_list":[
		{"event":"[Local][dsmctl-probe-task] Backup task finished successfully.","level":"info","time":"2026/07/21 01:21:37","user":"deryck"},
		{"event":"[Local][dsmctl-probe-task] Backup task started.","level":"info","time":"2026/07/21 01:20:57","user":"deryck"},
		{"event":"Setting of backup task [dsmctl-probe-task] was created","level":"info","time":"2026/07/21 01:19:57","user":"deryck"}]}`
	vaultConfigBody  = `{"parallel_backup_limit":2}`
	vaultTargetsBody = `{"target_list":[]}`
	// Live payload from a real inbound image_remote backup (nas255 -> nas51).
	vaultTargetsPopulatedBody = `{"target_list":[{"computing_size":false,"is_enc":false,"is_resumable":false,
		"last_backup_duration":15,"last_backup_start_time":1784602516,"share":"hb_vault","status":"idle",
		"target_id":1,"target_name":"DiskStation_1","target_path":"/volume1/hb_vault/DiskStation_1",
		"uni_key":"00113285995F_1_1784602486","used_size":729}]}`
	successBody = `{}`
)

func TestTasksDecodeLiveShape(t *testing.T) {
	target := hbTarget("4.2.2-4262", "")
	tasks, selection, err := ExecuteTasks(context.Background(), target, routeExecutor{t: t, routes: map[string]string{
		"SYNO.Backup.Task list": tasksBody,
	}})
	if err != nil {
		t.Fatalf("ExecuteTasks() error = %v", err)
	}
	if !selection.Supported {
		t.Fatalf("selection = %#v", selection)
	}
	if tasks.Total != 1 || len(tasks.Tasks) != 1 {
		t.Fatalf("tasks = %#v", tasks)
	}
	task := tasks.Tasks[0]
	if task.TaskID != 1 || task.Name != "dsmctl-probe-task" || task.Type != "image:image_local" ||
		task.State != "backupable" || task.Status != "none" || task.RepositoryID != 1 ||
		task.LastBackupResult != "done" || task.LastBackupTime != "2026/07/21 01:20:57" {
		t.Fatalf("task = %#v", task)
	}
	if len(task.SourceFolders) != 1 || task.SourceFolders[0] != "/Share/dsmctl_probe_src" {
		t.Fatalf("source folders = %#v", task.SourceFolders)
	}
	if task.Schedule != nil || task.NextBackupTime != "" {
		t.Fatalf("unscheduled task reported a schedule: %#v", task)
	}
}

func TestTasksRequestSendsJSONLiterals(t *testing.T) {
	target := hbTarget("4.2.2-4262", "")
	executor := &captureExecutor{responses: map[string]string{"SYNO.Backup.Task list": tasksBody}}
	if _, _, err := ExecuteTasks(context.Background(), target, executor); err != nil {
		t.Fatalf("ExecuteTasks() error = %v", err)
	}
	if len(executor.requests) != 1 {
		t.Fatalf("requests = %#v", executor.requests)
	}
	request := executor.requests[0]
	if request.JSONParameters == nil {
		t.Fatalf("task list must send JSONParameters (JSON-request API): %#v", request)
	}
	if sortBy, ok := request.JSONParameters["sort_by"].(string); !ok || sortBy != "name" {
		t.Fatalf("sort_by = %#v", request.JSONParameters["sort_by"])
	}
	if !request.ReadOnly {
		t.Fatalf("task list must be marked read-only for retry")
	}
}

func TestDetailComposesGetStatusTarget(t *testing.T) {
	target := hbTarget("4.2.2-4262", "")
	detail, _, err := ExecuteDetail(context.Background(), target, routeExecutor{t: t, routes: map[string]string{
		"SYNO.Backup.Task get":      taskGetBody,
		"SYNO.Backup.Task status":   statusRunningBody,
		"SYNO.Backup.Target get":    targetBody,
	}}, DetailInput{TaskID: 1})
	if err != nil {
		t.Fatalf("ExecuteDetail() error = %v", err)
	}
	if detail.Repository.Name != "dsmctl-probe-local" || detail.Repository.Share != "Share" {
		t.Fatalf("repository = %#v", detail.Repository)
	}
	if detail.BackupParams.MaxAutoResumeRetry != 5 || detail.BackupParams.CompressionEnabled {
		t.Fatalf("backup params = %#v", detail.BackupParams)
	}
	if detail.Status.Status != "backup" || detail.Status.LastBackupResult != "backingup" {
		t.Fatalf("status = %#v", detail.Status)
	}
	if detail.Status.Progress == nil {
		t.Fatalf("running task must expose progress")
	}
	progress := detail.Status.Progress
	if progress.ProcessedBytes != 2048 || progress.TotalBytes != 4096 || progress.AverageSpeedBps != 1024 ||
		progress.Percent != 42 || progress.Step != "backup_data" || !progress.CanCancel {
		t.Fatalf("progress = %#v (string counters must decode)", progress)
	}
	if !detail.Target.Online || detail.Target.HostName != "Derek_3018xs" || !detail.Target.MultiVersionSupport {
		t.Fatalf("target = %#v", detail.Target)
	}
	if detail.Task.Status != "backup" {
		t.Fatalf("task row must reflect live status: %#v", detail.Task)
	}
}

func TestVersionsDecodeLiveShape(t *testing.T) {
	target := hbTarget("4.2.2-4262", "")
	versions, _, err := ExecuteVersions(context.Background(), target, routeExecutor{t: t, routes: map[string]string{
		"SYNO.Backup.Version list": versionsBody,
	}}, VersionsInput{TaskID: 1, Limit: 20})
	if err != nil {
		t.Fatalf("ExecuteVersions() error = %v", err)
	}
	if versions.TaskID != 1 || versions.Total != 1 || len(versions.Entries) != 1 {
		t.Fatalf("versions = %#v", versions)
	}
	version := versions.Entries[0]
	if version.VersionID != "1" || version.Status != "success" || version.Locked ||
		version.StartTime != "2026/07/21 01:20:58" || version.CompleteTime != "2026/07/21 01:21:24" ||
		version.Timestamp != 1784568058 {
		t.Fatalf("version = %#v", version)
	}
}

func TestLogsDecodeLiveShape(t *testing.T) {
	target := hbTarget("4.2.2-4262", "")
	logs, _, err := ExecuteLogs(context.Background(), target, routeExecutor{t: t, routes: map[string]string{
		"SYNO.SDS.Backup.Client.Common.Log list": logsBody,
	}}, LogsInput{Limit: 10})
	if err != nil {
		t.Fatalf("ExecuteLogs() error = %v", err)
	}
	if logs.Total != 3 || logs.InfoCount != 3 || logs.ErrorCount != 0 || len(logs.Entries) != 3 {
		t.Fatalf("logs = %#v", logs)
	}
	if logs.Entries[0].Level != "info" || logs.Entries[0].User != "deryck" ||
		!strings.Contains(logs.Entries[0].Event, "finished successfully") {
		t.Fatalf("entry = %#v", logs.Entries[0])
	}
}

func TestVaultDecodeLiveShape(t *testing.T) {
	target := hbTarget("", "4.2.2-4262")
	vault, _, err := ExecuteVault(context.Background(), target, routeExecutor{t: t, routes: map[string]string{
		"SYNO.Backup.Service.VersionBackup.Config get":  vaultConfigBody,
		"SYNO.Backup.Service.VersionBackup.Target list": vaultTargetsBody,
	}})
	if err != nil {
		t.Fatalf("ExecuteVault() error = %v", err)
	}
	if vault.ParallelBackupLimit != 2 || len(vault.Targets) != 0 {
		t.Fatalf("vault = %#v", vault)
	}
}

func TestVaultDecodeInboundTarget(t *testing.T) {
	target := hbTarget("", "4.2.2-4262")
	vault, _, err := ExecuteVault(context.Background(), target, routeExecutor{t: t, routes: map[string]string{
		"SYNO.Backup.Service.VersionBackup.Config get":  vaultConfigBody,
		"SYNO.Backup.Service.VersionBackup.Target list": vaultTargetsPopulatedBody,
	}})
	if err != nil {
		t.Fatalf("ExecuteVault() error = %v", err)
	}
	if len(vault.Targets) != 1 {
		t.Fatalf("targets = %#v", vault.Targets)
	}
	inbound := vault.Targets[0]
	if inbound.TargetID != 1 || inbound.Share != "hb_vault" || inbound.TargetName != "DiskStation_1" ||
		inbound.TargetPath != "/volume1/hb_vault/DiskStation_1" || inbound.Status != "idle" ||
		inbound.Encrypted || inbound.UsedSizeBytes != 729 ||
		inbound.LastBackupStart != 1784602516 || inbound.LastBackupDurationSec != 15 {
		t.Fatalf("inbound target = %#v", inbound)
	}
}

func TestTaskRunSendsBackup(t *testing.T) {
	target := hbTarget("4.2.2-4262", "")
	executor := &captureExecutor{responses: map[string]string{"SYNO.Backup.Task backup": successBody}}
	result, _, err := ExecuteTaskRun(context.Background(), target, executor, hyperbackup.TaskChange{Action: hyperbackup.TaskActionBackup, TaskID: 1})
	if err != nil {
		t.Fatalf("ExecuteTaskRun() error = %v", err)
	}
	if result.Method != "backup" || result.TaskID != 1 || result.API != TaskAPIName {
		t.Fatalf("result = %#v", result)
	}
	if len(executor.requests) != 1 || executor.requests[0].Method != "backup" {
		t.Fatalf("requests = %#v", executor.requests)
	}
	if taskID, ok := executor.requests[0].JSONParameters["task_id"].(int); !ok || taskID != 1 {
		t.Fatalf("task_id = %#v", executor.requests[0].JSONParameters["task_id"])
	}
}

func TestTaskCancelSendsObservedTaskState(t *testing.T) {
	target := hbTarget("4.2.2-4262", "")
	executor := &captureExecutor{responses: map[string]string{
		"SYNO.Backup.Task status": statusRunningBody,
		"SYNO.Backup.Task cancel": successBody,
	}}
	result, _, err := ExecuteTaskRun(context.Background(), target, executor, hyperbackup.TaskChange{Action: hyperbackup.TaskActionCancel, TaskID: 1})
	if err != nil {
		t.Fatalf("ExecuteTaskRun() error = %v", err)
	}
	if result.Method != "cancel" {
		t.Fatalf("result = %#v", result)
	}
	if len(executor.requests) != 2 || executor.requests[0].Method != "status" || executor.requests[1].Method != "cancel" {
		t.Fatalf("cancel must read the live state first: %#v", executor.requests)
	}
	if state, ok := executor.requests[1].JSONParameters["task_state"].(string); !ok || state != "backupable" {
		t.Fatalf("task_state = %#v", executor.requests[1].JSONParameters["task_state"])
	}
}

func TestTaskCreateRemoteFlow(t *testing.T) {
	target := hbTarget("4.2.2-4262", "")
	executor := &captureExecutor{responses: map[string]string{
		"SYNO.Backup.Target get_candidate_dir": `{"candidate_dir":"DiskStation_1","deststatus":0}`,
		"SYNO.Backup.Repository create":        `{"repo_id":7}`,
		"SYNO.Backup.Task create":              `{"task_id":9,"reboot_is_needed_before_backup":false}`,
	}}
	input := TaskCreateInput{
		Spec: hyperbackup.TaskCreate{
			TaskName:      "nightly",
			SourceFolders: []string{"/homes"},
		},
		Host: "10.17.37.51", Account: "deryck", Password: "secret",
		Share: "hb_vault", Port: 6281, SSL: true,
	}
	result, _, err := ExecuteTaskCreate(context.Background(), target, executor, input)
	if err != nil {
		t.Fatalf("ExecuteTaskCreate() error = %v", err)
	}
	if result.TaskID != 9 || result.RepositoryID != 7 || result.Directory != "DiskStation_1" || result.Method != "create" {
		t.Fatalf("result = %#v", result)
	}
	if len(executor.requests) != 3 {
		t.Fatalf("requests = %d, want candidate->repository->task", len(executor.requests))
	}
	repo := executor.requests[1]
	if repo.API != RepositoryAPIName || repo.JSONParameters["transfer_type"] != "image_remote" ||
		repo.JSONParameters["dest"] != "10.17.37.51" || repo.JSONParameters["target_id"] != "DiskStation_1" ||
		repo.JSONParameters["is_webapi_authen"] != false || repo.JSONParameters["ssl_trust_mode"] != "ignore" {
		t.Fatalf("repository create params = %#v", repo.JSONParameters)
	}
	if len(repo.EncryptedParameters) != 1 || repo.EncryptedParameters[0] != "pwd" {
		t.Fatalf("pwd must be marked for transport protection: %#v", repo.EncryptedParameters)
	}
	task := executor.requests[2]
	if task.JSONParameters["action"] != "create" || task.JSONParameters["repo_id"] != 7 {
		t.Fatalf("task create params = %#v", task.JSONParameters)
	}
	source, ok := task.JSONParameters["source"].(map[string]any)
	if !ok {
		t.Fatalf("task create source = %#v", task.JSONParameters["source"])
	}
	folders, ok := source["file_list"].([]string)
	if !ok || len(folders) != 1 || folders[0] != "/homes" {
		t.Fatalf("file_list = %#v", source["file_list"])
	}
}

func TestTaskCreateLocalOmitsRemoteAuth(t *testing.T) {
	target := hbTarget("4.2.2-4262", "")
	executor := &captureExecutor{responses: map[string]string{
		"SYNO.Backup.Target get_candidate_dir": `{"candidate_dir":"Box_1","deststatus":0}`,
		"SYNO.Backup.Repository create":        `{"repo_id":2}`,
		// Task.create can answer an empty body on success (lab-observed).
		"SYNO.Backup.Task create": ``,
	}}
	input := TaskCreateInput{
		Spec:  hyperbackup.TaskCreate{TaskName: "local", SourceFolders: []string{"/Share/data"}, Directory: "custom_dir"},
		Share: "Backups",
	}
	result, _, err := ExecuteTaskCreate(context.Background(), target, executor, input)
	if err != nil {
		t.Fatalf("ExecuteTaskCreate() error = %v", err)
	}
	if result.TaskID != 0 || result.RepositoryID != 2 || result.Directory != "custom_dir" {
		t.Fatalf("result = %#v (empty create body must fall back to postcondition id recovery)", result)
	}
	repo := executor.requests[1]
	if repo.JSONParameters["transfer_type"] != "image_local" || repo.JSONParameters["share"] != "Backups" {
		t.Fatalf("repository create params = %#v", repo.JSONParameters)
	}
	if _, hasPwd := repo.JSONParameters["pwd"]; hasPwd {
		t.Fatalf("local create must not carry credentials: %#v", repo.JSONParameters)
	}
	if repo.JSONParameters["target_id"] != "custom_dir" {
		t.Fatalf("requested directory must override the candidate: %#v", repo.JSONParameters["target_id"])
	}
}

func TestFailsClosedWithoutPackage(t *testing.T) {
	target := hbTarget("", "")
	if _, _, err := ExecuteTasks(context.Background(), target, routeExecutor{t: t, routes: map[string]string{}}); !compatibility.IsUnsupported(err) {
		t.Fatalf("ExecuteTasks() without the package = %v, want unsupported", err)
	}
	if _, _, err := ExecuteTaskRun(context.Background(), target, &captureExecutor{}, hyperbackup.TaskChange{Action: hyperbackup.TaskActionBackup, TaskID: 1}); !compatibility.IsUnsupported(err) {
		t.Fatalf("ExecuteTaskRun() without the package = %v, want unsupported", err)
	}
}

func TestFailsClosedBelowBaselineVersion(t *testing.T) {
	target := hbTarget("3.0.2-0100", "")
	if _, _, err := ExecuteTasks(context.Background(), target, routeExecutor{t: t, routes: map[string]string{}}); !compatibility.IsUnsupported(err) {
		t.Fatalf("ExecuteTasks() below baseline = %v, want unsupported", err)
	}
}

func TestVaultFailsClosedWithoutVaultPackage(t *testing.T) {
	// The client package alone must not enable the vault view.
	target := hbTarget("4.2.2-4262", "")
	if _, _, err := ExecuteVault(context.Background(), target, routeExecutor{t: t, routes: map[string]string{}}); !compatibility.IsUnsupported(err) {
		t.Fatalf("ExecuteVault() without HyperBackupVault = %v, want unsupported", err)
	}
}

func TestDecodersRejectMalformedShapes(t *testing.T) {
	if _, err := decodeTasks(json.RawMessage(`{"total":0}`)); err == nil {
		t.Fatalf("decodeTasks must require task_list")
	}
	if _, err := decodeTasks(json.RawMessage(`[]`)); err == nil {
		t.Fatalf("decodeTasks must reject a non-object payload")
	}
	if _, err := decodeTasks(json.RawMessage(`{"task_list":[{"name":"x"}]}`)); err == nil {
		t.Fatalf("decodeTasks must require task_id")
	}
	if _, err := decodeTaskStatus(json.RawMessage(`{"state":"backupable"}`)); err == nil {
		t.Fatalf("decodeTaskStatus must require status")
	}
	if _, err := decodeTarget(json.RawMessage(`{"host_name":"x"}`)); err == nil {
		t.Fatalf("decodeTarget must require is_online")
	}
	if _, err := decodeVersions(json.RawMessage(`{"total":1}`)); err == nil {
		t.Fatalf("decodeVersions must require version_info_list")
	}
	if _, err := decodeLogs(json.RawMessage(`{"total":0}`)); err == nil {
		t.Fatalf("decodeLogs must require log_list")
	}
	if _, err := decodeVaultConfig(json.RawMessage(`{}`)); err == nil {
		t.Fatalf("decodeVaultConfig must require parallel_backup_limit")
	}
	if _, err := decodeVaultTargets(json.RawMessage(`{}`)); err == nil {
		t.Fatalf("decodeVaultTargets must require target_list")
	}
}
