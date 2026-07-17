package driveadmin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/driveadmin"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type capturingExecutor struct {
	request  compatibility.Request
	response json.RawMessage
}

func (executor *capturingExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	executor.request = request
	return executor.response, nil
}

func driveTarget(packageVersion string, running bool) compatibility.Target {
	target := compatibility.NewTarget()
	for _, name := range APINames() {
		maxVersion := 1
		switch name {
		case ConnectionAPIName, ShareAPIName:
			maxVersion = 2
		}
		target.SetAPI(name, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: maxVersion})
	}
	if packageVersion != "" {
		target.SetInstalledPackages([]compatibility.InstalledPackage{
			{ID: PackageID, Version: compatibility.ParsePackageVersion(packageVersion), Running: running},
		})
	} else {
		target.SetInstalledPackages(nil)
	}
	return target
}

func TestSelectRequiresInstalledBaselinePackage(t *testing.T) {
	// APIs discovered but the package catalog reports Drive absent: every
	// operation must fail closed with package evidence in the reason.
	selections, err := Select(driveTarget("", false))
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if len(selections) != 5 {
		t.Fatalf("selection count = %d", len(selections))
	}
	for _, selection := range selections[:4] {
		if selection.Supported {
			t.Fatalf("selection %q should be unsupported without the package", selection.Operation)
		}
	}

	// Installed but below the verified baseline also fails closed.
	if _, _, err := ExecuteStatus(context.Background(), driveTarget("2.0.4-11112", true), &capturingExecutor{}); !compatibility.IsUnsupported(err) {
		t.Fatalf("Drive 2.x should be unsupported, got %v", err)
	}

	// A catalog that was never loaded must not look like evidence of absence.
	target := compatibility.NewTarget()
	for _, name := range APINames() {
		target.SetAPI(name, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	}
	selection, err := SelectStatus(target)
	if !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
	if !strings.Contains(selection.Reason, "catalog was not loaded") {
		t.Fatalf("reason should explain the missing catalog, got %q", selection.Reason)
	}
}

func TestSelectCarriesPackageVersionEvidence(t *testing.T) {
	selection, err := SelectStatus(driveTarget("4.0.3-27892", true))
	if err != nil {
		t.Fatalf("SelectStatus() error = %v", err)
	}
	if !selection.Supported || selection.Backend != "drive-status-v1" {
		t.Fatalf("selection = %#v", selection)
	}
	if !strings.Contains(selection.Reason, "package SynologyDrive 4.0.3-27892") {
		t.Fatalf("selection reason lacks package evidence: %q", selection.Reason)
	}
}

func TestTeamFoldersSetFailsClosed(t *testing.T) {
	selection, err := SelectTeamFoldersSet(driveTarget("4.0.3-27892", true))
	if !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("team-folder set must fail closed, selection=%#v err=%v", selection, err)
	}
}

func TestExecuteStatusRequestShapeAndDecode(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{"status":"Active"}`)}
	status, selection, err := ExecuteStatus(context.Background(), driveTarget("4.0.3-27892", true), executor)
	if err != nil {
		t.Fatalf("ExecuteStatus() error = %v", err)
	}
	if executor.request.API != StatusAPIName || executor.request.Version != 1 || executor.request.Method != "get_status" {
		t.Fatalf("request = %#v", executor.request)
	}
	if status.Status != "active" || selection.Backend != "drive-status-v1" {
		t.Fatalf("status=%#v selection=%#v", status, selection)
	}
}

func TestExecuteStatusRejectsUnknownShape(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{"unexpected":true}`)}
	_, _, err := ExecuteStatus(context.Background(), driveTarget("4.0.3-27892", true), executor)
	if err == nil || !strings.Contains(err.Error(), "no status field among [unexpected]") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteConnectionsDecodesItems(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{
		"total": 2,
		"items": [
			{"username": "alice", "device_name": "ALICE-NB", "client_type": "Desktop", "address": "10.0.0.5"},
			{"user": "bob", "ip": "10.0.0.9"}
		]
	}`)}
	connections, _, err := ExecuteConnections(context.Background(), driveTarget("4.0.3-27892", true), executor)
	if err != nil {
		t.Fatalf("ExecuteConnections() error = %v", err)
	}
	if executor.request.API != ConnectionAPIName || executor.request.Method != "list" || executor.request.Version != 1 {
		t.Fatalf("request = %#v", executor.request)
	}
	if connections.Total != 2 || len(connections.Connections) != 2 {
		t.Fatalf("connections = %#v", connections)
	}
	first, second := connections.Connections[0], connections.Connections[1]
	if first.User != "alice" || first.DeviceName != "ALICE-NB" || first.ClientType != "desktop" || first.Address != "10.0.0.5" {
		t.Fatalf("first = %#v", first)
	}
	if second.User != "bob" || second.Address != "10.0.0.9" {
		t.Fatalf("second = %#v", second)
	}
}

func TestExecuteConnectionsRejectsMissingListContainer(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{"sessions": 3}`)}
	_, _, err := ExecuteConnections(context.Background(), driveTarget("4.0.3-27892", true), executor)
	if err == nil || !strings.Contains(err.Error(), "no connection array among [sessions]") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteTeamFoldersDecodesItems(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{
		"items": [
			{"id": "5", "name": "projects", "status": "Enabled"},
			{"name": "media", "status": "disabled"}
		]
	}`)}
	folders, _, err := ExecuteTeamFolders(context.Background(), driveTarget("4.0.3-27892", true), executor)
	if err != nil {
		t.Fatalf("ExecuteTeamFolders() error = %v", err)
	}
	if executor.request.API != ShareAPIName || executor.request.Method != "list" || executor.request.Version != 1 {
		t.Fatalf("request = %#v", executor.request)
	}
	if folders.Total != 2 || len(folders.TeamFolders) != 2 {
		t.Fatalf("folders = %#v", folders)
	}
	if folders.TeamFolders[0].ID != "5" || folders.TeamFolders[0].Name != "projects" || folders.TeamFolders[0].Status != "enabled" {
		t.Fatalf("first = %#v", folders.TeamFolders[0])
	}
}

func TestExecuteTeamFoldersRequiresName(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{"items":[{"status":"enabled"}]}`)}
	_, _, err := ExecuteTeamFolders(context.Background(), driveTarget("4.0.3-27892", true), executor)
	if err == nil || !strings.Contains(err.Error(), "no name field") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteLogSendsFiltersAndDecodes(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{
		"total": 1,
		"items": [
			{"time": "2026-07-17 10:00:03", "username": "alice", "action": "Upload", "filename": "/projects/spec.md", "descr": "Uploaded spec.md"}
		]
	}`)}
	query := driveadmin.LogQuery{Limit: 50, Keyword: "spec", Username: "alice", Target: "/projects", From: 1700000000, To: 1800000000}
	log, _, err := ExecuteLog(context.Background(), driveTarget("4.0.3-27892", true), executor, query)
	if err != nil {
		t.Fatalf("ExecuteLog() error = %v", err)
	}
	if executor.request.API != LogAPIName || executor.request.Method != "list" || executor.request.Version != 1 {
		t.Fatalf("request = %#v", executor.request)
	}
	parameters := executor.request.JSONParameters
	if parameters["limit"] != 50 || parameters["keyword"] != "spec" || parameters["username"] != "alice" || parameters["target"] != "/projects" {
		t.Fatalf("parameters = %#v", parameters)
	}
	if parameters["datefrom"] != int64(1700000000) || parameters["dateto"] != int64(1800000000) {
		t.Fatalf("time parameters = %#v", parameters)
	}
	if log.Total != 1 || len(log.Entries) != 1 {
		t.Fatalf("log = %#v", log)
	}
	entry := log.Entries[0]
	if entry.Username != "alice" || entry.Action != "upload" || entry.Target != "/projects/spec.md" || entry.Description != "Uploaded spec.md" {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestExecuteLogOmitsUnsetFilters(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{"items":[]}`)}
	if _, _, err := ExecuteLog(context.Background(), driveTarget("4.0.3-27892", true), executor, driveadmin.LogQuery{Limit: 100}); err != nil {
		t.Fatalf("ExecuteLog() error = %v", err)
	}
	parameters := executor.request.JSONParameters
	for _, key := range []string{"keyword", "username", "target", "datefrom", "dateto"} {
		if _, present := parameters[key]; present {
			t.Fatalf("parameter %q should be omitted when unset: %#v", key, parameters)
		}
	}
}
