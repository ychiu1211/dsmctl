package hyperbackup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/derekvery666/dsmctl/internal/domain/hyperbackup"
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
			Step            *string   `json:"step"`
			Progress        flexInt   `json:"progress"`
			ProcessedSize   flexInt64 `json:"processed_size"`
			TotalSize       flexInt64 `json:"total_size"`
			TransmittedSize flexInt64 `json:"transmitted_size"`
			AvgSpeed        flexInt64 `json:"avg_speed"`
			CanCancel       bool      `json:"can_cancel"`
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

// decodeApplications reads the App2.Backup list. Unusually for DSM, the data
// element itself is the ARRAY of applications (live-verified on 4.2.2).
type applicationFolderList []string

func (folders *applicationFolderList) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*folders = nil
		return nil
	}

	var entries []json.RawMessage
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return fmt.Errorf("expected an array: %w", err)
	}

	decoded := make(applicationFolderList, 0, len(entries))
	for index, entry := range entries {
		entry = bytes.TrimSpace(entry)
		if len(entry) == 0 {
			return fmt.Errorf("entry %d is empty", index)
		}
		switch entry[0] {
		case '"':
			var folder string
			if err := json.Unmarshal(entry, &folder); err != nil {
				return fmt.Errorf("decode string entry %d: %w", index, err)
			}
			decoded = append(decoded, strings.TrimSpace(folder))
		case '{':
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(entry, &fields); err != nil {
				return fmt.Errorf("decode object entry %d: %w", index, err)
			}
			var descriptor struct {
				FolderPath string `json:"folderPath"`
				FullPath   string `json:"fullPath"`
				Folder     string `json:"folder"`
			}
			if err := json.Unmarshal(entry, &descriptor); err != nil {
				return fmt.Errorf("decode object entry %d: %w", index, err)
			}
			folder := strings.TrimSpace(descriptor.FolderPath)
			if folder == "" {
				folder = strings.TrimSpace(descriptor.FullPath)
			}
			if folder == "" {
				folder = strings.TrimSpace(descriptor.Folder)
			}
			if folder == "" {
				keys := make([]string, 0, len(fields))
				for key := range fields {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				return fmt.Errorf("object entry %d is missing folderPath, fullPath, and folder (fields: %s)", index, strings.Join(keys, ", "))
			}
			decoded = append(decoded, folder)
		default:
			return fmt.Errorf("entry %d must be a string or object", index)
		}
	}
	*folders = decoded
	return nil
}

func decodeApplications(data json.RawMessage) (hyperbackup.Applications, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return hyperbackup.Applications{}, errors.New("decode Hyper Backup application list: expected an array")
	}
	var wire []struct {
		ID           *string `json:"id"`
		Name         *string `json:"name"`
		Version      *string `json:"version"`
		IsRunning    bool    `json:"is_running"`
		OnlineBackup bool    `json:"online_backup"`
		SummaryDisp  *string `json:"summary_disp"`
		ErrorKey     *string `json:"error_key"`
		Depend       *struct {
			FolderList applicationFolderList `json:"folder_list"`
		} `json:"depend"`
	}
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return hyperbackup.Applications{}, fmt.Errorf("decode Hyper Backup application list: %w", err)
	}
	applications := hyperbackup.Applications{Entries: make([]hyperbackup.Application, 0, len(wire))}
	for _, entry := range wire {
		if entry.ID == nil {
			return hyperbackup.Applications{}, errors.New("decode Hyper Backup application list: required field \"id\" is missing")
		}
		application := hyperbackup.Application{
			ID:           strings.TrimSpace(*entry.ID),
			Name:         deref(entry.Name),
			Version:      deref(entry.Version),
			Running:      entry.IsRunning,
			OnlineBackup: entry.OnlineBackup,
			Summary:      deref(entry.SummaryDisp),
			Reason:       deref(entry.ErrorKey),
		}
		application.Backupable = application.Reason == ""
		if entry.Depend != nil {
			application.RequiredFolders = []string(entry.Depend.FolderList)
		}
		applications.Entries = append(applications.Entries, application)
	}
	return applications, nil
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

// ---- legacy LUN backup decoders (SYNO.Backup.Lunbackup, live-verified) ----

func decodeLuns(data json.RawMessage) (hyperbackup.Luns, error) {
	var resp struct {
		Items *[]struct {
			Name *string   `json:"name"`
			Size flexInt64 `json:"size"`
			Type *string   `json:"type"`
			UUID *string   `json:"uuid"`
		} `json:"items"`
	}
	if err := unmarshalObject(data, "Hyper Backup LUN list", &resp); err != nil {
		return hyperbackup.Luns{}, err
	}
	// DSM omits the items key entirely when no LUN is available (already backed
	// up or none), returning just {total:0} — treat that as an empty list.
	if resp.Items == nil {
		return hyperbackup.Luns{Entries: []hyperbackup.Lun{}}, nil
	}
	luns := hyperbackup.Luns{Entries: make([]hyperbackup.Lun, 0, len(*resp.Items))}
	for _, item := range *resp.Items {
		if item.Name == nil {
			return hyperbackup.Luns{}, errors.New("decode Hyper Backup LUN list: a LUN is missing \"name\"")
		}
		luns.Entries = append(luns.Entries, hyperbackup.Lun{
			Name:      strings.TrimSpace(*item.Name),
			Type:      deref(item.Type),
			UUID:      deref(item.UUID),
			SizeBytes: int64(item.Size),
		})
	}
	return luns, nil
}

// decodeLunBackupTasks reads the Task list and keeps only LUN backup entries
// (type loclunbkp/netlunbkp); their task_id is the name string, so they are a
// separate space from the image tasks.
func decodeLunBackupTasks(data json.RawMessage) (hyperbackup.LunBackupTasks, error) {
	var resp struct {
		TaskList *[]struct {
			Name          *string `json:"name"`
			Type          *string `json:"type"`
			Status        *string `json:"status"`
			LastBkpResult *string `json:"last_bkp_result"`
			Progress      *struct {
				Progress flexInt `json:"progress"`
				Step     *string `json:"step"`
			} `json:"progress"`
		} `json:"task_list"`
	}
	if err := unmarshalObject(data, "Hyper Backup LUN task list", &resp); err != nil {
		return hyperbackup.LunBackupTasks{}, err
	}
	if resp.TaskList == nil {
		return hyperbackup.LunBackupTasks{}, errors.New("decode Hyper Backup LUN task list: required field \"task_list\" is missing")
	}
	tasks := hyperbackup.LunBackupTasks{Entries: make([]hyperbackup.LunBackupTask, 0)}
	for _, w := range *resp.TaskList {
		typ := deref(w.Type)
		if typ != "loclunbkp" && typ != "netlunbkp" {
			continue
		}
		task := hyperbackup.LunBackupTask{
			TaskName:         deref(w.Name),
			Type:             typ,
			Status:           deref(w.Status),
			LastBackupResult: deref(w.LastBkpResult),
		}
		if w.Progress != nil {
			task.Percent = int(w.Progress.Progress)
			task.Step = deref(w.Progress.Step)
		}
		tasks.Entries = append(tasks.Entries, task)
	}
	return tasks, nil
}

func decodeLunBackupTaskStatus(data json.RawMessage) (hyperbackup.LunBackupTask, error) {
	var resp struct {
		TaskName      *string `json:"taskName"`
		Type          *string `json:"type"`
		BkpData       *string `json:"bkpdata"`
		DestShare     *string `json:"dest_share"`
		DestDir       *string `json:"dest_dir"`
		Status        *string `json:"status"`
		LastBkpResult *string `json:"last_bkp_result"`
		LastBkpTime   *string `json:"last_bkp_time"`
		UUID          *string `json:"uuid"`
		Progress      *struct {
			Progress flexInt `json:"progress"`
			Step     *string `json:"step"`
		} `json:"progress"`
	}
	if err := unmarshalObject(data, "Hyper Backup LUN task status", &resp); err != nil {
		return hyperbackup.LunBackupTask{}, err
	}
	if resp.TaskName == nil {
		return hyperbackup.LunBackupTask{}, errors.New("decode Hyper Backup LUN task status: required field \"taskName\" is missing")
	}
	task := hyperbackup.LunBackupTask{
		TaskName:         strings.TrimSpace(*resp.TaskName),
		Type:             deref(resp.Type),
		LunSource:        deref(resp.BkpData),
		DestinationShare: deref(resp.DestShare),
		DestinationDir:   deref(resp.DestDir),
		Status:           deref(resp.Status),
		LastBackupResult: deref(resp.LastBkpResult),
		LastBackupTime:   deref(resp.LastBkpTime),
		UUID:             deref(resp.UUID),
	}
	if resp.Progress != nil {
		task.Percent = int(resp.Progress.Progress)
		task.Step = deref(resp.Progress.Step)
	}
	return task, nil
}

func decodeDefaultDirectory(data json.RawMessage) (string, error) {
	var resp struct {
		DefaultDirectory *string `json:"defaultDirectory"`
	}
	if err := unmarshalObject(data, "Hyper Backup LUN destination directory", &resp); err != nil {
		return "", err
	}
	if resp.DefaultDirectory == nil || strings.TrimSpace(*resp.DefaultDirectory) == "" {
		return "", errors.New("decode Hyper Backup LUN destination directory: required field \"defaultDirectory\" is missing")
	}
	return strings.TrimSpace(*resp.DefaultDirectory), nil
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
