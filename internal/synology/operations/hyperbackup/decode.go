package hyperbackup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/hyperbackup"
)

// flexInt64 / flexInt decode a JSON number that Hyper Backup returns as a
// quoted string in several places (progress counters such as processed_size
// and total_size are strings while progress and avg_speed are numbers,
// live-verified on 4.2.2). A missing or null value decodes to zero.
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(b []byte) error {
	n, ok := parseFlexInt(b)
	if !ok {
		return nil
	}
	*f = flexInt64(n)
	return nil
}

type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	n, ok := parseFlexInt(b)
	if !ok {
		return nil
	}
	*f = flexInt(n)
	return nil
}

func parseFlexInt(b []byte) (int64, bool) {
	s := strings.TrimSpace(strings.Trim(string(b), `"`))
	if s == "" || s == "null" {
		return 0, false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, true
	}
	if fl, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(fl), true
	}
	return 0, false
}

func unmarshalObject(data json.RawMessage, what string, destination any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return fmt.Errorf("decode %s: empty response", what)
	}
	if trimmed[0] != '{' {
		return fmt.Errorf("decode %s: expected an object", what)
	}
	if err := json.Unmarshal(trimmed, destination); err != nil {
		return fmt.Errorf("decode %s: %w", what, err)
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// wireSchedule is the schedule block the task list attaches for scheduled
// tasks. The lab fixture is unscheduled, so the field names mirror the shape
// the DSM UI round-trips (SimpleScheduleComponent) and decode best-effort.
type wireSchedule struct {
	Enabled  *bool   `json:"schedule_enable"`
	Hour     flexInt `json:"hour"`
	Minute   flexInt `json:"min"`
	WeekName *string `json:"week_name"`
}

func (w *wireSchedule) toDomain() *hyperbackup.Schedule {
	if w == nil {
		return nil
	}
	return &hyperbackup.Schedule{
		Enabled:  w.Enabled != nil && *w.Enabled,
		Hour:     int(w.Hour),
		Minute:   int(w.Minute),
		WeekName: deref(w.WeekName),
	}
}

// wireSource is the expanded per-task source (additional get_source).
type wireSource struct {
	FileList []struct {
		FolderPath *string `json:"folderPath"`
	} `json:"file_list"`
	AppNameList []string `json:"app_name_list"`
}

func (w wireSource) folders() []string {
	folders := make([]string, 0, len(w.FileList))
	for _, entry := range w.FileList {
		if path := deref(entry.FolderPath); path != "" {
			folders = append(folders, path)
		}
	}
	return folders
}

type wireTask struct {
	TaskID         *flexInt      `json:"task_id"`
	Name           *string       `json:"name"`
	Type           *string       `json:"type"`
	TargetType     *string       `json:"target_type"`
	TransferType   *string       `json:"transfer_type"`
	TargetID       *string       `json:"target_id"`
	RepoID         flexInt       `json:"repo_id"`
	DataType       *string       `json:"data_type"`
	State          *string       `json:"state"`
	Status         *string       `json:"status"`
	DataEnc        bool          `json:"data_enc"`
	IsModified     bool          `json:"is_modified"`
	LastBkpTime    *string       `json:"last_bkp_time"`
	LastBkpEndTime *string       `json:"last_bkp_end_time"`
	LastBkpResult  *string       `json:"last_bkp_result"`
	NextBkpTime    *string       `json:"next_bkp_time"`
	Schedule       *wireSchedule `json:"schedule"`
	Source         *wireSource   `json:"source"`
}

func (w wireTask) toDomain(what string) (hyperbackup.Task, error) {
	if w.TaskID == nil {
		return hyperbackup.Task{}, fmt.Errorf("decode %s: required field \"task_id\" is missing", what)
	}
	if w.Name == nil {
		return hyperbackup.Task{}, fmt.Errorf("decode %s: required field \"name\" is missing", what)
	}
	task := hyperbackup.Task{
		TaskID:           int(*w.TaskID),
		Name:             strings.TrimSpace(*w.Name),
		Type:             deref(w.Type),
		TargetType:       deref(w.TargetType),
		TransferType:     deref(w.TransferType),
		TargetID:         deref(w.TargetID),
		RepositoryID:     int(w.RepoID),
		DataType:         deref(w.DataType),
		State:            deref(w.State),
		Status:           deref(w.Status),
		DataEncrypted:    w.DataEnc,
		Modified:         w.IsModified,
		LastBackupTime:   deref(w.LastBkpTime),
		LastBackupEnd:    deref(w.LastBkpEndTime),
		LastBackupResult: deref(w.LastBkpResult),
		NextBackupTime:   deref(w.NextBkpTime),
		Schedule:         w.Schedule.toDomain(),
	}
	if w.Source != nil {
		task.SourceFolders = w.Source.folders()
		task.SourceApps = w.Source.AppNameList
	}
	return task, nil
}

func decodeTasks(data json.RawMessage) (hyperbackup.Tasks, error) {
	var resp struct {
		TaskList *[]wireTask `json:"task_list"`
		Total    flexInt     `json:"total"`
	}
	if err := unmarshalObject(data, "Hyper Backup task list", &resp); err != nil {
		return hyperbackup.Tasks{}, err
	}
	if resp.TaskList == nil {
		return hyperbackup.Tasks{}, errors.New("decode Hyper Backup task list: required field \"task_list\" is missing")
	}
	tasks := hyperbackup.Tasks{Total: int(resp.Total), Tasks: make([]hyperbackup.Task, 0, len(*resp.TaskList))}
	for _, wire := range *resp.TaskList {
		task, err := wire.toDomain("Hyper Backup task list entry")
		if err != nil {
			return hyperbackup.Tasks{}, err
		}
		tasks.Tasks = append(tasks.Tasks, task)
	}
	if tasks.Total == 0 {
		tasks.Total = len(tasks.Tasks)
	}
	return tasks, nil
}

func decodeTaskDetail(data json.RawMessage) (hyperbackup.TaskDetail, error) {
	var resp struct {
		wireTask
		BackupParams *struct {
			EnableDataCompress bool    `json:"enable_data_compress"`
			EnableDataEncrypt  bool    `json:"enable_data_encrypt"`
			EnableNotify       bool    `json:"enable_notify"`
			EnableVersionLog   bool    `json:"enable_version_file_log"`
			MaxAutoResumeRetry flexInt `json:"max_auto_resume_retry"`
		} `json:"backup_params"`
		Repository *struct {
			Name         *string `json:"name"`
			RepoID       flexInt `json:"repo_id"`
			Share        *string `json:"share"`
			TargetType   *string `json:"target_type"`
			TransferType *string `json:"transfer_type"`
		} `json:"repository"`
	}
	if err := unmarshalObject(data, "Hyper Backup task", &resp); err != nil {
		return hyperbackup.TaskDetail{}, err
	}
	task, err := resp.wireTask.toDomain("Hyper Backup task")
	if err != nil {
		return hyperbackup.TaskDetail{}, err
	}
	detail := hyperbackup.TaskDetail{Task: task}
	if resp.BackupParams == nil {
		return hyperbackup.TaskDetail{}, errors.New("decode Hyper Backup task: required field \"backup_params\" is missing")
	}
	detail.BackupParams = hyperbackup.BackupParams{
		CompressionEnabled: resp.BackupParams.EnableDataCompress,
		EncryptionEnabled:  resp.BackupParams.EnableDataEncrypt,
		NotifyEnabled:      resp.BackupParams.EnableNotify,
		VersionFileLog:     resp.BackupParams.EnableVersionLog,
		MaxAutoResumeRetry: int(resp.BackupParams.MaxAutoResumeRetry),
	}
	if resp.Repository != nil {
		detail.Repository = hyperbackup.Repository{
			RepositoryID: int(resp.Repository.RepoID),
			Name:         deref(resp.Repository.Name),
			Share:        deref(resp.Repository.Share),
			TargetType:   deref(resp.Repository.TargetType),
			TransferType: deref(resp.Repository.TransferType),
		}
	}
	return detail, nil
}

func decodeTaskStatus(data json.RawMessage) (hyperbackup.TaskStatus, error) {
	var resp struct {
		State           *string `json:"state"`
		Status          *string `json:"status"`
		LastBkpTime     *string `json:"last_bkp_time"`
		LastBkpEndTime  *string `json:"last_bkp_end_time"`
		LastSuccessTime *string `json:"last_bkp_success_time"`
		LastBkpResult   *string `json:"last_bkp_result"`
		LastBkpError    *string `json:"last_bkp_error"`
		NextBkpTime     *string `json:"next_bkp_time"`
		Progress        *struct {
			Step             *string   `json:"step"`
			Progress         flexInt   `json:"progress"`
			ProcessedSize    flexInt64 `json:"processed_size"`
			TotalSize        flexInt64 `json:"total_size"`
			TransmittedSize  flexInt64 `json:"transmitted_size"`
			AvgSpeed         flexInt64 `json:"avg_speed"`
			CanCancel        bool      `json:"can_cancel"`
		} `json:"progress"`
	}
	if err := unmarshalObject(data, "Hyper Backup task status", &resp); err != nil {
		return hyperbackup.TaskStatus{}, err
	}
	if resp.Status == nil {
		return hyperbackup.TaskStatus{}, errors.New("decode Hyper Backup task status: required field \"status\" is missing")
	}
	status := hyperbackup.TaskStatus{
		State:            deref(resp.State),
		Status:           deref(resp.Status),
		LastBackupTime:   deref(resp.LastBkpTime),
		LastBackupEnd:    deref(resp.LastBkpEndTime),
		LastSuccessTime:  deref(resp.LastSuccessTime),
		LastBackupResult: deref(resp.LastBkpResult),
		LastBackupError:  deref(resp.LastBkpError),
		NextBackupTime:   deref(resp.NextBkpTime),
	}
	if resp.Progress != nil {
		status.Progress = &hyperbackup.Progress{
			Step:             deref(resp.Progress.Step),
			Percent:          int(resp.Progress.Progress),
			ProcessedBytes:   int64(resp.Progress.ProcessedSize),
			TotalBytes:       int64(resp.Progress.TotalSize),
			TransmittedBytes: int64(resp.Progress.TransmittedSize),
			AverageSpeedBps:  int64(resp.Progress.AvgSpeed),
			CanCancel:        resp.Progress.CanCancel,
		}
	}
	return status, nil
}

func decodeTarget(data json.RawMessage) (hyperbackup.Target, error) {
	var resp struct {
		IsOnline     *bool   `json:"is_online"`
		HostName     *string `json:"host_name"`
		OwnerName    *string `json:"owner_name"`
		FormatType   *string `json:"format_type"`
		MultiVersion bool    `json:"support_multi_version"`
	}
	if err := unmarshalObject(data, "Hyper Backup target", &resp); err != nil {
		return hyperbackup.Target{}, err
	}
	if resp.IsOnline == nil {
		return hyperbackup.Target{}, errors.New("decode Hyper Backup target: required field \"is_online\" is missing")
	}
	return hyperbackup.Target{
		Online:              *resp.IsOnline,
		HostName:            deref(resp.HostName),
		OwnerName:           deref(resp.OwnerName),
		FormatType:          deref(resp.FormatType),
		MultiVersionSupport: resp.MultiVersion,
	}, nil
}

func decodeVersions(data json.RawMessage) (hyperbackup.Versions, error) {
	var resp struct {
		VersionInfoList *[]struct {
			VersionID         *string   `json:"version_id"`
			Name              *string   `json:"name"`
			Status            *string   `json:"status"`
			Locked            bool      `json:"locked"`
			StartTimeLocal    *string   `json:"start_time_local"`
			CompleteTime      flexInt64 `json:"complete_time"`
			CompleteTimeLocal *string   `json:"complete_time_local"`
			Timestamp         flexInt64 `json:"timestamp"`
		} `json:"version_info_list"`
		Total flexInt `json:"total"`
	}
	if err := unmarshalObject(data, "Hyper Backup version list", &resp); err != nil {
		return hyperbackup.Versions{}, err
	}
	if resp.VersionInfoList == nil {
		return hyperbackup.Versions{}, errors.New("decode Hyper Backup version list: required field \"version_info_list\" is missing")
	}
	versions := hyperbackup.Versions{Total: int(resp.Total), Entries: make([]hyperbackup.Version, 0, len(*resp.VersionInfoList))}
	for _, wire := range *resp.VersionInfoList {
		if wire.VersionID == nil {
			return hyperbackup.Versions{}, errors.New("decode Hyper Backup version list: required field \"version_id\" is missing")
		}
		// An incomplete version (for example a canceled run) has complete_time
		// 0, which DSM still renders as a 1970 local-time string; report it as
		// never completed instead.
		completeTime := deref(wire.CompleteTimeLocal)
		if int64(wire.CompleteTime) == 0 {
			completeTime = ""
		}
		versions.Entries = append(versions.Entries, hyperbackup.Version{
			VersionID:    strings.TrimSpace(*wire.VersionID),
			Name:         deref(wire.Name),
			Status:       deref(wire.Status),
			Locked:       wire.Locked,
			StartTime:    deref(wire.StartTimeLocal),
			CompleteTime: completeTime,
			Timestamp:    int64(wire.Timestamp),
		})
	}
	if versions.Total == 0 {
		versions.Total = len(versions.Entries)
	}
	return versions, nil
}

func decodeLogs(data json.RawMessage) (hyperbackup.Logs, error) {
	var resp struct {
		LogList *[]struct {
			Level *string `json:"level"`
			Time  *string `json:"time"`
			Event *string `json:"event"`
			User  *string `json:"user"`
		} `json:"log_list"`
		Total      flexInt `json:"total"`
		Offset     flexInt `json:"offset"`
		ErrorCount flexInt `json:"error_count"`
		WarnCount  flexInt `json:"warn_count"`
		InfoCount  flexInt `json:"info_count"`
	}
	if err := unmarshalObject(data, "Hyper Backup log list", &resp); err != nil {
		return hyperbackup.Logs{}, err
	}
	if resp.LogList == nil {
		return hyperbackup.Logs{}, errors.New("decode Hyper Backup log list: required field \"log_list\" is missing")
	}
	logs := hyperbackup.Logs{
		Total:      int(resp.Total),
		Offset:     int(resp.Offset),
		ErrorCount: int(resp.ErrorCount),
		WarnCount:  int(resp.WarnCount),
		InfoCount:  int(resp.InfoCount),
		Entries:    make([]hyperbackup.LogEntry, 0, len(*resp.LogList)),
	}
	for _, wire := range *resp.LogList {
		if wire.Level == nil || wire.Time == nil || wire.Event == nil {
			return hyperbackup.Logs{}, errors.New("decode Hyper Backup log list: a log entry is missing level, time, or event")
		}
		logs.Entries = append(logs.Entries, hyperbackup.LogEntry{
			Level: strings.TrimSpace(*wire.Level),
			Time:  strings.TrimSpace(*wire.Time),
			Event: strings.TrimSpace(*wire.Event),
			User:  deref(wire.User),
		})
	}
	return logs, nil
}

func decodeCandidateDir(data json.RawMessage) (string, error) {
	var resp struct {
		CandidateDir *string `json:"candidate_dir"`
	}
	if err := unmarshalObject(data, "Hyper Backup candidate directory", &resp); err != nil {
		return "", err
	}
	if resp.CandidateDir == nil || strings.TrimSpace(*resp.CandidateDir) == "" {
		return "", errors.New("decode Hyper Backup candidate directory: required field \"candidate_dir\" is missing")
	}
	return strings.TrimSpace(*resp.CandidateDir), nil
}

func decodeRepositoryCreate(data json.RawMessage) (int, error) {
	var resp struct {
		RepoID *flexInt `json:"repo_id"`
	}
	if err := unmarshalObject(data, "Hyper Backup repository create result", &resp); err != nil {
		return 0, err
	}
	if resp.RepoID == nil {
		return 0, errors.New("decode Hyper Backup repository create result: required field \"repo_id\" is missing")
	}
	return int(*resp.RepoID), nil
}

// decodeTaskCreate reads the created task id. Task.create has been observed to
// answer HTTP 200 with an empty body on success (lab, image_local), so an
// empty or id-less response decodes to 0 and the caller recovers the id from
// the postcondition re-read instead of failing a create that DSM accepted.
func decodeTaskCreate(data json.RawMessage) (int, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return 0, nil
	}
	var resp struct {
		TaskID *flexInt `json:"task_id"`
	}
	if err := unmarshalObject(data, "Hyper Backup task create result", &resp); err != nil {
		return 0, err
	}
	if resp.TaskID == nil {
		return 0, nil
	}
	return int(*resp.TaskID), nil
}

func decodeVaultConfig(data json.RawMessage) (hyperbackup.Vault, error) {
	var resp struct {
		ParallelBackupLimit *flexInt `json:"parallel_backup_limit"`
	}
	if err := unmarshalObject(data, "Hyper Backup Vault configuration", &resp); err != nil {
		return hyperbackup.Vault{}, err
	}
	if resp.ParallelBackupLimit == nil {
		return hyperbackup.Vault{}, errors.New("decode Hyper Backup Vault configuration: required field \"parallel_backup_limit\" is missing")
	}
	return hyperbackup.Vault{ParallelBackupLimit: int(*resp.ParallelBackupLimit)}, nil
}

func decodeVaultTargets(data json.RawMessage) ([]hyperbackup.VaultTarget, error) {
	var resp struct {
		TargetList *[]struct {
			TargetID       *flexInt  `json:"target_id"`
			Share          *string   `json:"share"`
			TargetName     *string   `json:"target_name"`
			TargetPath     *string   `json:"target_path"`
			Status         *string   `json:"status"`
			IsEnc          bool      `json:"is_enc"`
			IsResumable    bool      `json:"is_resumable"`
			UsedSize       flexInt64 `json:"used_size"`
			ComputingSize  bool      `json:"computing_size"`
			LastBackupTime flexInt64 `json:"last_backup_start_time"`
			LastDuration   flexInt64 `json:"last_backup_duration"`
		} `json:"target_list"`
	}
	if err := unmarshalObject(data, "Hyper Backup Vault target list", &resp); err != nil {
		return nil, err
	}
	if resp.TargetList == nil {
		return nil, errors.New("decode Hyper Backup Vault target list: required field \"target_list\" is missing")
	}
	targets := make([]hyperbackup.VaultTarget, 0, len(*resp.TargetList))
	for _, wire := range *resp.TargetList {
		if wire.TargetID == nil {
			return nil, errors.New("decode Hyper Backup Vault target list: required field \"target_id\" is missing")
		}
		targets = append(targets, hyperbackup.VaultTarget{
			TargetID:              int(*wire.TargetID),
			Share:                 deref(wire.Share),
			TargetName:            deref(wire.TargetName),
			TargetPath:            deref(wire.TargetPath),
			Status:                deref(wire.Status),
			Encrypted:             wire.IsEnc,
			Resumable:             wire.IsResumable,
			UsedSizeBytes:         int64(wire.UsedSize),
			ComputingSize:         wire.ComputingSize,
			LastBackupStart:       int64(wire.LastBackupTime),
			LastBackupDurationSec: int64(wire.LastDuration),
		})
	}
	return targets, nil
}
