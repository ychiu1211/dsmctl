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

func TestTeamFoldersSetSelectsVerifiedBackend(t *testing.T) {
	selection, err := SelectTeamFoldersSet(driveTarget("4.0.3-27892", true))
	if err != nil {
		t.Fatalf("SelectTeamFoldersSet() error = %v", err)
	}
	if !selection.Supported || selection.Backend != "drive-share-v1" || selection.API != ShareAPIName {
		t.Fatalf("selection = %#v", selection)
	}

	// Without the package (or below baseline) the write must fail closed.
	if selection, err := SelectTeamFoldersSet(driveTarget("", false)); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("team-folder set without the package must fail closed, selection=%#v err=%v", selection, err)
	}
	if selection, err := SelectTeamFoldersSet(driveTarget("2.0.4-11112", true)); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("team-folder set below baseline must fail closed, selection=%#v err=%v", selection, err)
	}
}

func TestExecuteTeamFoldersSetEnableRequestShape(t *testing.T) {
	// The set handler answers success with an empty data object.
	executor := &capturingExecutor{response: json.RawMessage(`{}`)}
	enable := true
	count, days := 8, 30
	input := TeamFolderSetInput{ShareName: "projects", Enable: &enable, MaxVersions: &count, VersionPolicy: "smart", RetentionDays: &days}
	result, _, err := ExecuteTeamFoldersSet(context.Background(), driveTarget("4.0.3-27892", true), executor, input)
	if err != nil {
		t.Fatalf("ExecuteTeamFoldersSet() error = %v", err)
	}
	if executor.request.API != ShareAPIName || executor.request.Version != 1 || executor.request.Method != "set" {
		t.Fatalf("request = %#v", executor.request)
	}
	// Source-verified shape (handlers/share/set.cpp): the share parameter is an
	// array of per-share objects; exactly one entry is sent per plan.
	entries, ok := executor.request.JSONParameters["share"].([]map[string]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("share parameter = %#v", executor.request.JSONParameters["share"])
	}
	entry := entries[0]
	if entry["share_name"] != "projects" || entry["share_enable"] != true ||
		entry["rotate_cnt"] != 8 || entry["rotate_policy"] != "smart" || entry["rotate_days"] != 30 {
		t.Fatalf("entry = %#v", entry)
	}
	if result.API != ShareAPIName || result.Method != "set" || result.Backend != "drive-share-v1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteTeamFoldersSetVersioningOnlyOmitsEnableFlag(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{}`)}
	count := 4
	input := TeamFolderSetInput{ShareName: "projects", MaxVersions: &count}
	if _, _, err := ExecuteTeamFoldersSet(context.Background(), driveTarget("4.0.3-27892", true), executor, input); err != nil {
		t.Fatalf("ExecuteTeamFoldersSet() error = %v", err)
	}
	entries := executor.request.JSONParameters["share"].([]map[string]any)
	entry := entries[0]
	// Presence of share_enable routes the entry to the enable/disable path in
	// the handler, so a versioning-only change must omit it entirely, along
	// with the versioning fields the caller did not send.
	for _, key := range []string{"share_enable", "rotate_policy", "rotate_days"} {
		if _, present := entry[key]; present {
			t.Fatalf("entry key %q should be omitted: %#v", key, entry)
		}
	}
	if entry["rotate_cnt"] != 4 || entry["share_name"] != "projects" {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestExecuteTeamFoldersSetDisableSendsOnlyEnableFlag(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{}`)}
	disable := false
	input := TeamFolderSetInput{ShareName: "projects", Enable: &disable}
	if _, _, err := ExecuteTeamFoldersSet(context.Background(), driveTarget("4.0.3-27892", true), executor, input); err != nil {
		t.Fatalf("ExecuteTeamFoldersSet() error = %v", err)
	}
	entries := executor.request.JSONParameters["share"].([]map[string]any)
	entry := entries[0]
	if entry["share_name"] != "projects" || entry["share_enable"] != false || len(entry) != 2 {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestExecuteStatusRequestShapeAndDecode(t *testing.T) {
	// Response shape captured live from Drive 4.0.3 (WI-022).
	executor := &capturingExecutor{response: json.RawMessage(`{
		"csrv_alias_err": "", "csrv_enable": true, "csrv_status": "connected success",
		"cstn_freeze": false, "enable_status": "Enabled", "no_folder_available": false
	}`)}
	status, selection, err := ExecuteStatus(context.Background(), driveTarget("4.0.3-27892", true), executor)
	if err != nil {
		t.Fatalf("ExecuteStatus() error = %v", err)
	}
	if executor.request.API != StatusAPIName || executor.request.Version != 1 || executor.request.Method != "get_status" {
		t.Fatalf("request = %#v", executor.request)
	}
	if status.Status != "enabled" || selection.Backend != "drive-status-v1" {
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

func TestExecuteTeamFoldersRequestShapeAndDecode(t *testing.T) {
	// Response shape captured live from Drive 4.0.3 (WI-022): share_enable is
	// the team-folder activation flag, and fields that do not apply to a
	// disabled share are reported as "-".
	executor := &capturingExecutor{response: json.RawMessage(`{
		"total": 3,
		"items": [
			{"share_name": "homes/mydrive_home", "share_enable": true, "share_status": "normal", "share_type": "", "rotate_cnt": 8, "rotate_policy": "smart", "rotate_days": 0},
			{"share_name": "projects", "share_enable": false, "share_status": "normal", "share_type": "normal", "rotate_cnt": "-", "rotate_policy": "-", "rotate_days": 0},
			{"share_name": "team-data", "share_enable": true, "share_status": "normal", "share_type": "normal", "rotate_cnt": 0, "rotate_policy": "-", "rotate_days": 0}
		]
	}`)}
	folders, _, err := ExecuteTeamFolders(context.Background(), driveTarget("4.0.3-27892", true), executor)
	if err != nil {
		t.Fatalf("ExecuteTeamFolders() error = %v", err)
	}
	if executor.request.API != ShareAPIName || executor.request.Method != "list" || executor.request.Version != 1 {
		t.Fatalf("request = %#v", executor.request)
	}
	// Verified live: list rejects the request without paging and a valid sort.
	parameters := executor.request.JSONParameters
	if parameters["offset"] != 0 || parameters["limit"] != teamFolderPageLimit || parameters["sort_by"] != "share_name" || parameters["sort_direction"] != "ASC" {
		t.Fatalf("parameters = %#v", parameters)
	}
	if folders.Total != 3 || len(folders.TeamFolders) != 3 {
		t.Fatalf("folders = %#v", folders)
	}
	home := folders.TeamFolders[0]
	if home.Name != "homes/mydrive_home" || !home.Enabled || home.Status != "normal" {
		t.Fatalf("home = %#v", home)
	}
	if home.MaxVersions == nil || *home.MaxVersions != 8 || home.VersionPolicy != "smart" || home.RetentionDays == nil || *home.RetentionDays != 0 {
		t.Fatalf("home versioning = %#v", home)
	}
	// Disabled shares report "-" for versioning fields, surfaced as absent.
	disabled := folders.TeamFolders[1]
	if disabled.Name != "projects" || disabled.Enabled || disabled.Type != "normal" {
		t.Fatalf("disabled = %#v", disabled)
	}
	if disabled.MaxVersions != nil || disabled.VersionPolicy != "" || disabled.RetentionDays != nil {
		t.Fatalf("disabled versioning should be absent: %#v", disabled)
	}
	// Enabled with versioning off: rotate_cnt 0 and policy "-".
	off := folders.TeamFolders[2]
	if off.MaxVersions == nil || *off.MaxVersions != 0 || off.VersionPolicy != "" {
		t.Fatalf("versioning-off entry = %#v", off)
	}
}

func TestExecuteTeamFoldersRequiresName(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{"items":[{"share_status":"normal"}]}`)}
	_, _, err := ExecuteTeamFolders(context.Background(), driveTarget("4.0.3-27892", true), executor)
	if err == nil || !strings.Contains(err.Error(), "no name field") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteObservabilityReads(t *testing.T) {
	// Response shapes captured live from Drive 4.0.3 (WI-052).
	executor := &capturingExecutor{response: json.RawMessage(`{"summary":{"desktop":2,"mobile":1,"sharesync":0,"total":3}}`)}
	summary, selection, err := ExecuteConnectionSummary(context.Background(), driveTarget("4.0.3-27892", true), executor)
	if err != nil {
		t.Fatalf("ExecuteConnectionSummary() error = %v", err)
	}
	// The summary method exists only at Connection v2 (v1 answers 103).
	if executor.request.API != ConnectionAPIName || executor.request.Version != 2 || executor.request.Method != "summary" {
		t.Fatalf("request = %#v", executor.request)
	}
	if summary.Desktop != 2 || summary.Mobile != 1 || summary.Total != 3 || selection.Backend != "drive-connection-v2" {
		t.Fatalf("summary = %#v selection = %#v", summary, selection)
	}

	executor = &capturingExecutor{response: json.RawMessage(`{"database_size":2243510,"office_size":26701800,"repo_size":857164,"update_time":1784495605}`)}
	usage, _, err := ExecuteDBUsage(context.Background(), driveTarget("4.0.3-27892", true), executor)
	if err != nil {
		t.Fatalf("ExecuteDBUsage() error = %v", err)
	}
	if executor.request.API != DBUsageAPIName || executor.request.Method != "get" {
		t.Fatalf("request = %#v", executor.request)
	}
	if usage.RepositorySize != 857164 || usage.DatabaseSize != 2243510 || usage.OfficeSize != 26701800 || usage.UpdatedUnix != 1784495605 {
		t.Fatalf("usage = %#v", usage)
	}

	executor = &capturingExecutor{response: json.RawMessage(`{"files":[{"path":"/projects/spec.md","name":"spec.md","access_count":12}]}`)}
	files, _, err := ExecuteDashboard(context.Background(), driveTarget("4.0.3-27892", true), executor,
		driveadmin.TopAccessQuery{RankingBy: "both", PeriodDays: 7, Limit: 5})
	if err != nil {
		t.Fatalf("ExecuteDashboard() error = %v", err)
	}
	parameters := executor.request.JSONParameters
	if executor.request.Method != "top_access_files" || parameters["ranking_by"] != "both" || parameters["period_days"] != 7 || parameters["limit"] != 5 {
		t.Fatalf("request = %#v", executor.request)
	}
	if len(files.Files) != 1 || files.Files[0].Path != "/projects/spec.md" || files.Files[0].AccessCount != 12 {
		t.Fatalf("files = %#v", files)
	}

	executor = &capturingExecutor{response: json.RawMessage(`{"activated":false,"activation_time":0,"serial_number":"1790PXN037200"}`)}
	activation, _, err := ExecuteActivation(context.Background(), driveTarget("4.0.3-27892", true), executor)
	if err != nil {
		t.Fatalf("ExecuteActivation() error = %v", err)
	}
	if executor.request.API != ActivationAPIName || executor.request.Method != "get" {
		t.Fatalf("request = %#v", executor.request)
	}
	if activation.Activated || activation.SerialNumber != "1790PXN037200" || activation.ActivationUnix != 0 {
		t.Fatalf("activation = %#v", activation)
	}
}

func TestObservabilityDecodersRejectUnknownShapes(t *testing.T) {
	target := driveTarget("4.0.3-27892", true)
	if _, _, err := ExecuteConnectionSummary(context.Background(), target, &capturingExecutor{response: json.RawMessage(`{"counts":{}}`)}); err == nil || !strings.Contains(err.Error(), "no summary object") {
		t.Fatalf("summary error = %v", err)
	}
	if _, _, err := ExecuteDBUsage(context.Background(), target, &capturingExecutor{response: json.RawMessage(`{"sizes":{}}`)}); err == nil || !strings.Contains(err.Error(), "no repo_size field") {
		t.Fatalf("db usage error = %v", err)
	}
	if _, _, err := ExecuteDashboard(context.Background(), target, &capturingExecutor{response: json.RawMessage(`{"ranking":1}`)}, driveadmin.TopAccessQuery{RankingBy: "both", PeriodDays: 1, Limit: 5}); err == nil || !strings.Contains(err.Error(), "no file array") {
		t.Fatalf("dashboard error = %v", err)
	}
	if _, _, err := ExecuteActivation(context.Background(), target, &capturingExecutor{response: json.RawMessage(`{"enabled":true}`)}); err == nil || !strings.Contains(err.Error(), "activated") {
		t.Fatalf("activation error = %v", err)
	}
}

func TestExecuteLogSendsFiltersAndDecodes(t *testing.T) {
	// Response shape captured live from Drive 4.0.3 (WI-022): entries carry a
	// numeric event type plus substitution slots instead of rendered text.
	executor := &capturingExecutor{response: json.RawMessage(`{
		"total": 1,
		"items": [
			{"time": 1779279309, "username": "alice", "client_type": "web_portal", "ip_address": "10.0.0.5",
			 "type": 24, "s1": "/projects/spec.md", "share_name": "projects", "target": "user", "p1": "1"}
		]
	}`)}
	query := driveadmin.LogQuery{Limit: 50, Offset: 10, Keyword: "spec", Username: "alice", From: 1700000000, To: 1800000000}
	log, _, err := ExecuteLog(context.Background(), driveTarget("4.0.3-27892", true), executor, query)
	if err != nil {
		t.Fatalf("ExecuteLog() error = %v", err)
	}
	if executor.request.API != LogAPIName || executor.request.Method != "list" || executor.request.Version != 1 {
		t.Fatalf("request = %#v", executor.request)
	}
	parameters := executor.request.JSONParameters
	// Verified live: share_type, target, log_type, and get_all are required;
	// the all-scopes view is share_type "all" with target "user".
	if parameters["share_type"] != "all" || parameters["target"] != "user" || parameters["get_all"] != false {
		t.Fatalf("scope parameters = %#v", parameters)
	}
	if types, ok := parameters["log_type"].([]int); !ok || len(types) != 0 {
		t.Fatalf("log_type = %#v", parameters["log_type"])
	}
	if parameters["limit"] != 50 || parameters["offset"] != 10 || parameters["keyword"] != "spec" || parameters["username"] != "alice" {
		t.Fatalf("parameters = %#v", parameters)
	}
	if parameters["datefrom"] != int64(1700000000) || parameters["dateto"] != int64(1800000000) {
		t.Fatalf("time parameters = %#v", parameters)
	}
	if log.Total != 1 || len(log.Entries) != 1 {
		t.Fatalf("log = %#v", log)
	}
	entry := log.Entries[0]
	if entry.TimeUnix != 1779279309 || entry.Username != "alice" || entry.ClientType != "web_portal" ||
		entry.IPAddress != "10.0.0.5" || entry.EventType != 24 || entry.Path != "/projects/spec.md" || entry.TeamFolder != "projects" {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestExecuteLogScopesToTeamFolder(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{"items":[]}`)}
	query := driveadmin.LogQuery{Limit: 100, TeamFolder: "projects"}
	if _, _, err := ExecuteLog(context.Background(), driveTarget("4.0.3-27892", true), executor, query); err != nil {
		t.Fatalf("ExecuteLog() error = %v", err)
	}
	parameters := executor.request.JSONParameters
	// Verified live: one team folder is share_type "share" with an @-prefixed
	// shared-folder name.
	if parameters["share_type"] != "share" || parameters["target"] != "@projects" {
		t.Fatalf("scope parameters = %#v", parameters)
	}
}

func TestExecuteLogOmitsUnsetFilters(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{"items":[]}`)}
	if _, _, err := ExecuteLog(context.Background(), driveTarget("4.0.3-27892", true), executor, driveadmin.LogQuery{Limit: 100}); err != nil {
		t.Fatalf("ExecuteLog() error = %v", err)
	}
	parameters := executor.request.JSONParameters
	for _, key := range []string{"keyword", "username", "datefrom", "dateto"} {
		if _, present := parameters[key]; present {
			t.Fatalf("parameter %q should be omitted when unset: %#v", key, parameters)
		}
	}
	for _, key := range []string{"share_type", "target", "log_type", "get_all", "offset", "limit"} {
		if _, present := parameters[key]; !present {
			t.Fatalf("required parameter %q missing: %#v", key, parameters)
		}
	}
}
