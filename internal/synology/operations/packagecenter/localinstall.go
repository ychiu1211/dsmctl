package packagecenter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// Manual (local .spk) install. Unlike the online install in install.go — where
// DSM downloads the package from a catalog URL — the package bytes are uploaded
// from the client and DSM installs from the uploaded temp file.
//
// Verified against DSM 7.3's own Package Center client (PkgManApp.js):
//   - SYNO.Core.Package.Installation.upload (multipart, field "file") returns a
//     task_id/filename plus the INFO fields requested in "additional".
//   - SYNO.Core.Package.Installation.install references the uploaded file by
//     task_id (preferred) or path, with volume_path/type/check_codesign/force/
//     installrunpackage. This reuses the same install task + status polling as
//     the online install; only the upload and the file reference are new.
const (
	// UploadMethod uploads a local package file for a manual install.
	UploadMethod = "upload"
	// UploadFileField is the multipart field DSM expects the .spk bytes under.
	UploadFileField = "file"
	// UploadAdditionalField requests the INFO metadata keys be parsed and
	// returned alongside the temp reference.
	UploadAdditionalField = "additional"
	// UploadAdditionalValue mirrors the INFO keys DSM's own UI asks the upload to
	// parse, so the response carries the package identity (id/name/version).
	UploadAdditionalValue = `["description","maintainer","distributor","startable","dsm_apps","status","install_reboot","install_type","install_on_cold_storage","break_pkgs","replace_pkgs"]`
)

// UploadResult is the decoded upload response: the temp reference DSM assigns
// plus the package identity parsed from the .spk INFO.
type UploadResult struct {
	TaskID        string
	FileName      string
	PackageID     string
	Name          string
	Version       string
	InstallType   string
	InstallReboot bool
}

// DecodeUploadResult reads the upload response, tolerating field-name variance
// across DSM builds (the raw shape is available under DSMCTL_DUMP for live
// confirmation).
func DecodeUploadResult(data json.RawMessage) (UploadResult, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return UploadResult{}, fmt.Errorf("decode package upload: expected a non-empty object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return UploadResult{}, fmt.Errorf("decode package upload: %w", err)
	}
	return UploadResult{
		TaskID:        firstString(raw, "task_id", "taskid"),
		FileName:      firstString(raw, "filename", "filepath", "path"),
		PackageID:     firstString(raw, "id", "package"),
		Name:          firstString(raw, "name", "dname"),
		Version:       firstString(raw, "version"),
		InstallType:   firstString(raw, "install_type"),
		InstallReboot: firstBool(raw, "install_reboot"),
	}, nil
}

// LocalInstallInput carries the uploaded-file reference plus install options for
// a manual install or upgrade.
type LocalInstallInput struct {
	VolumePath      string
	TaskID          string
	Path            string
	CheckCodesign   bool
	RunAfterInstall bool
	Upgrade         bool
}

var localInstallOperation = compatibility.Operation[LocalInstallInput, DownloadTask]{
	Name: InstallCapabilityName,
	Variants: []compatibility.Variant[LocalInstallInput, DownloadTask]{
		{
			Name: "core-package-installation-local-v1", API: InstallationAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(InstallationAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input LocalInstallInput) (DownloadTask, error) {
				method := "install"
				if input.Upgrade {
					method = "upgrade"
				}
				parameters := map[string]any{
					"volume_path":       input.VolumePath,
					"extra_values":      "{}",
					"type":              0,
					"check_codesign":    input.CheckCodesign,
					"force":             false,
					"installrunpackage": input.RunAfterInstall,
				}
				// Reference the uploaded file: DSM's own client prefers task_id and
				// falls back to path when no task id is present.
				if input.TaskID != "" {
					parameters["task_id"] = input.TaskID
				} else {
					parameters["path"] = input.Path
				}
				data, err := executor.Execute(ctx, compatibility.Request{API: InstallationAPIName, Version: 1, Method: method, JSONParameters: parameters})
				if err != nil {
					return DownloadTask{}, fmt.Errorf("call %s.%s v1: %w", InstallationAPIName, method, err)
				}
				return decodeDownloadTask(data)
			},
		},
	},
}

// SelectLocalInstall reports whether a manual (local .spk) install is supported.
func SelectLocalInstall(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := localInstallOperation.Select(target)
	return selection, err
}

// ExecuteLocalInstall installs (or upgrades) from an uploaded package file.
func ExecuteLocalInstall(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input LocalInstallInput) (DownloadTask, compatibility.Selection, error) {
	return localInstallOperation.Run(ctx, target, executor, input)
}

// ExecuteUploadCleanup removes an uploaded-but-not-installed temp package. DSM
// deletes by path and cleans by task_id; the call is best-effort.
func ExecuteUploadCleanup(ctx context.Context, target compatibility.Target, executor compatibility.Executor, taskID, path string) error {
	if path != "" {
		_, err := executor.Execute(ctx, compatibility.Request{
			API: InstallationAPIName, Version: 1, Method: "delete",
			JSONParameters: map[string]any{"path": path},
		})
		return err
	}
	if taskID != "" {
		_, err := executor.Execute(ctx, compatibility.Request{
			API: InstallationAPIName, Version: 1, Method: "clean",
			JSONParameters: map[string]any{"task_id": taskID},
		})
		return err
	}
	return nil
}
