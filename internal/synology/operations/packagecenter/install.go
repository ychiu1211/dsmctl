package packagecenter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// InstallationAPIName drives the guarded online install/upgrade via a download
// task plus status polling.
const InstallationAPIName = "SYNO.Core.Package.Installation"

// InstallInput carries the catalog-resolved download metadata and install
// options for one online install or upgrade.
type InstallInput struct {
	Name            string
	URL             string
	Checksum        string
	Filesize        int64
	Beta            bool
	QuickInstall    bool
	VolumePath      string
	RunAfterInstall bool
	Upgrade         bool
}

// DownloadTask is the reference returned by the download method for polling.
type DownloadTask struct {
	TaskID string
}

// TaskProgress is the normalized status of an in-flight install/download task.
type TaskProgress struct {
	TaskID    string
	PackageID string
	Finished  bool
	Success   bool
	Error     string
	Progress  float64
	Beta      bool
}

var downloadOperation = compatibility.Operation[InstallInput, DownloadTask]{
	Name: InstallCapabilityName,
	Variants: []compatibility.Variant[InstallInput, DownloadTask]{
		{
			Name: "core-package-installation-download-v1", API: InstallationAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(InstallationAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input InstallInput) (DownloadTask, error) {
				parameters := map[string]any{
					"name":              input.Name,
					"url":               input.URL,
					"checksum":          input.Checksum,
					"filesize":          input.Filesize,
					"volume_path":       input.VolumePath,
					"beta":              input.Beta,
					"blqinst":           input.QuickInstall,
					"installrunpackage": input.RunAfterInstall,
					"is_syno":           true,
					"type":              0,
				}
				data, err := executor.Execute(ctx, compatibility.Request{API: InstallationAPIName, Version: 1, Method: "install", JSONParameters: parameters})
				if err != nil {
					return DownloadTask{}, fmt.Errorf("call %s.install v1: %w", InstallationAPIName, err)
				}
				return decodeDownloadTask(data)
			},
		},
	},
}

func SelectInstallDownload(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := downloadOperation.Select(target)
	return selection, err
}

// ExecuteInstallDownload starts the online download+install task.
func ExecuteInstallDownload(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input InstallInput) (DownloadTask, compatibility.Selection, error) {
	return downloadOperation.Run(ctx, target, executor, input)
}

// ExecuteInstallStatus polls one install/download task by id. It is read-only.
func ExecuteInstallStatus(ctx context.Context, target compatibility.Target, executor compatibility.Executor, taskID string) ([]TaskProgress, error) {
	data, err := executor.Execute(ctx, compatibility.Request{
		API: InstallationAPIName, Version: 1, Method: "status",
		JSONParameters: map[string]any{"taskid": taskID},
	})
	if err != nil {
		return nil, fmt.Errorf("call %s.status v1: %w", InstallationAPIName, err)
	}
	return decodeStatus(data)
}

func decodeDownloadTask(data json.RawMessage) (DownloadTask, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return DownloadTask{}, fmt.Errorf("decode package download: expected a non-empty object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return DownloadTask{}, fmt.Errorf("decode package download: %w", err)
	}
	task := DownloadTask{TaskID: firstString(raw, "task_id", "taskid", "id")}
	return task, nil
}

func decodeStatus(data json.RawMessage) ([]TaskProgress, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("decode install status: expected a non-empty object")
	}
	// The status response shape varies; accept a top-level "tasks" array or a
	// single task object.
	var envelope struct {
		Tasks []map[string]json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, fmt.Errorf("decode install status: %w", err)
	}
	entries := envelope.Tasks
	if len(entries) == 0 {
		var single map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &single); err == nil && len(single) > 0 {
			entries = []map[string]json.RawMessage{single}
		}
	}
	progress := make([]TaskProgress, 0, len(entries))
	for _, raw := range entries {
		progress = append(progress, TaskProgress{
			TaskID:    firstString(raw, "task_id", "taskid", "id"),
			PackageID: firstString(raw, "package", "id", "name"),
			Finished:  firstBool(raw, "finished", "is_finished"),
			Success:   firstBool(raw, "success"),
			Error:     firstString(raw, "error", "error_msg"),
			Progress:  firstFloat(raw, "progress"),
			Beta:      firstBool(raw, "beta"),
		})
	}
	return progress, nil
}

func firstFloat(raw map[string]json.RawMessage, names ...string) float64 {
	for _, name := range names {
		if value, ok := raw[name]; ok {
			var f float64
			if err := json.Unmarshal(value, &f); err == nil {
				return f
			}
		}
	}
	return 0
}
