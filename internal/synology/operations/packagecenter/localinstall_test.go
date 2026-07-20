package packagecenter

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

func TestDecodeUploadResult(t *testing.T) {
	// Shape mirrors DSM 7.3 SYNO.Core.Package.Installation.upload: a task_id and
	// filename temp reference plus the INFO parsed from the .spk.
	raw := json.RawMessage(`{"task_id":"@SYNOPKG_UPLOAD_abc","filename":"/tmp/pkg.spk",
		"id":"dsmctl-gateway","name":"dsmctl Gateway","version":"0.1.0-1",
		"install_type":"","install_reboot":false,"startable":true}`)
	result, err := DecodeUploadResult(raw)
	if err != nil {
		t.Fatalf("DecodeUploadResult() error = %v", err)
	}
	if result.TaskID != "@SYNOPKG_UPLOAD_abc" || result.FileName != "/tmp/pkg.spk" ||
		result.PackageID != "dsmctl-gateway" || result.Name != "dsmctl Gateway" ||
		result.Version != "0.1.0-1" || result.InstallReboot {
		t.Fatalf("upload decode = %#v", result)
	}

	if _, err := DecodeUploadResult(json.RawMessage(`[]`)); err == nil {
		t.Fatal("DecodeUploadResult() accepted a non-object response")
	}
}

func TestLocalInstallRequestContract(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(InstallationAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 2})
	executor := &captureExecutor{responses: map[string]json.RawMessage{
		InstallationAPIName + ".install": json.RawMessage(`{"task_id":"@SYNOPKG_INSTALL_abc"}`),
	}}

	// Fresh install with a task_id reference, code-signature enforced.
	task, selection, err := ExecuteLocalInstall(context.Background(), target, executor, LocalInstallInput{
		VolumePath: "/volume1", TaskID: "@SYNOPKG_UPLOAD_abc", Path: "/tmp/pkg.spk",
		CheckCodesign: true, RunAfterInstall: true, Upgrade: false,
	})
	if err != nil {
		t.Fatalf("ExecuteLocalInstall() error = %v", err)
	}
	if selection.Backend != "core-package-installation-local-v1" || task.TaskID != "@SYNOPKG_INSTALL_abc" {
		t.Fatalf("selection/task = %#v %#v", selection, task)
	}
	request := executor.requests[len(executor.requests)-1]
	if request.API != InstallationAPIName || request.Method != "install" {
		t.Fatalf("install request api/method = %s.%s", request.API, request.Method)
	}
	params := request.JSONParameters
	if params["volume_path"] != "/volume1" || params["task_id"] != "@SYNOPKG_UPLOAD_abc" ||
		params["check_codesign"] != true || params["installrunpackage"] != true || params["force"] != false {
		t.Fatalf("install params = %#v", params)
	}
	if _, hasPath := params["path"]; hasPath {
		t.Fatalf("install must reference task_id, not path, when a task id is present: %#v", params)
	}

	// Upgrade of an already-installed package with no task_id falls back to path.
	executor.responses[InstallationAPIName+".upgrade"] = json.RawMessage(`{"task_id":"@SYNOPKG_UPGRADE_abc"}`)
	if _, _, err := ExecuteLocalInstall(context.Background(), target, executor, LocalInstallInput{
		VolumePath: "/volume1", Path: "/tmp/pkg.spk", Upgrade: true,
	}); err != nil {
		t.Fatalf("ExecuteLocalInstall(upgrade) error = %v", err)
	}
	upgrade := executor.requests[len(executor.requests)-1]
	if upgrade.Method != "upgrade" || upgrade.JSONParameters["path"] != "/tmp/pkg.spk" {
		t.Fatalf("upgrade request = %#v", upgrade)
	}
	if _, hasTask := upgrade.JSONParameters["task_id"]; hasTask {
		t.Fatalf("upgrade without a task id must reference path only: %#v", upgrade.JSONParameters)
	}
}

func TestUploadCleanupContract(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(InstallationAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 2})
	executor := &captureExecutor{responses: map[string]json.RawMessage{}}

	// Path present -> delete by path.
	if err := ExecuteUploadCleanup(context.Background(), target, executor, "@SYNOPKG_UPLOAD_abc", "/tmp/pkg.spk"); err != nil {
		t.Fatalf("ExecuteUploadCleanup(path) error = %v", err)
	}
	del := executor.requests[len(executor.requests)-1]
	if del.Method != "delete" || del.JSONParameters["path"] != "/tmp/pkg.spk" {
		t.Fatalf("cleanup delete = %#v", del)
	}

	// No path -> clean by task id.
	if err := ExecuteUploadCleanup(context.Background(), target, executor, "@SYNOPKG_UPLOAD_abc", ""); err != nil {
		t.Fatalf("ExecuteUploadCleanup(task) error = %v", err)
	}
	clean := executor.requests[len(executor.requests)-1]
	if clean.Method != "clean" || clean.JSONParameters["task_id"] != "@SYNOPKG_UPLOAD_abc" {
		t.Fatalf("cleanup clean = %#v", clean)
	}
}
