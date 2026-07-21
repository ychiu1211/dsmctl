package snapshotreplication

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type capturingExecutor struct {
	responses map[string]json.RawMessage
	requests  []compatibility.Request
}

func (executor *capturingExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	executor.requests = append(executor.requests, request)
	if r, ok := executor.responses[request.API]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}

// coreTarget advertises the core snapshot APIs (present on DSM 7.3 without the
// SnapshotReplication package) with an empty installed-package catalog.
func coreTarget() compatibility.Target {
	target := compatibility.NewTarget()
	target.SetAPI(ShareSnapshotAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(RetentionAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	target.SetAPI(LogAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	target.SetAPI(NodeAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	target.SetInstalledPackages(nil)
	return target
}

func packageTarget() compatibility.Target {
	target := coreTarget()
	target.SetAPI(PlanAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	target.SetInstalledPackages([]compatibility.InstalledPackage{
		{ID: PackageID, Version: compatibility.ParsePackageVersion("7.4.7-1859"), Running: true},
	})
	return target
}

func TestSnapshotsDecodeLiveShape(t *testing.T) {
	// Shape confirmed live on DSM 7.3-81168 (list v2 with additional attributes).
	target := coreTarget()
	executor := &capturingExecutor{responses: map[string]json.RawMessage{
		ShareSnapshotAPIName: json.RawMessage(`{"snapshots":[
			{"desc":"wi087 probe","lock":false,"schedule_snapshot":false,"time":"GMT+08-2026.07.21-02.11.04","worm_lock":false}
		],"total":1}`),
	}}
	snapshots, selection, err := ExecuteSnapshots(context.Background(), target, executor, ShareInput{Share: "data"})
	if err != nil {
		t.Fatalf("ExecuteSnapshots() error = %v", err)
	}
	if selection.Backend != "core-share-snapshot-list-v2" || snapshots.Share != "data" || snapshots.Total != 1 {
		t.Fatalf("snapshots = %#v (selection %#v)", snapshots, selection)
	}
	snapshot := snapshots.Snapshots[0]
	if snapshot.Time != "GMT+08-2026.07.21-02.11.04" || snapshot.Description != "wi087 probe" || snapshot.Locked || snapshot.ScheduleCreated || snapshot.WormLocked {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	request := executor.requests[0]
	if request.Version != 2 || request.Method != "list" || request.JSONParameters["name"] != "data" || request.JSONParameters["limit"] != -1 {
		t.Fatalf("list request = %#v", request)
	}
	additional, ok := request.JSONParameters["additional"].([]string)
	if !ok || !reflect.DeepEqual(additional, []string{"desc", "lock", "schedule_snapshot", "worm_lock"}) {
		t.Fatalf("list additional = %#v", request.JSONParameters["additional"])
	}
}

func TestSnapshotsRejectMalformedShapes(t *testing.T) {
	target := coreTarget()
	for name, response := range map[string]string{
		"missing list":  `{"total":0}`,
		"unnamed entry": `{"snapshots":[{"desc":"x"}],"total":1}`,
		"not an object": `[]`,
	} {
		executor := &capturingExecutor{responses: map[string]json.RawMessage{ShareSnapshotAPIName: json.RawMessage(response)}}
		if _, _, err := ExecuteSnapshots(context.Background(), target, executor, ShareInput{Share: "data"}); err == nil {
			t.Fatalf("%s: expected a decode error", name)
		}
	}
}

func TestShareConfigDecodeLiveShape(t *testing.T) {
	// Shape confirmed live on DSM 7.3-81168 (get_share_conf v1).
	target := coreTarget()
	executor := &capturingExecutor{responses: map[string]json.RawMessage{
		ShareSnapshotAPIName: json.RawMessage(`{"enable_snapshot_browsing":false,"snapshot_local_time_format":true}`),
	}}
	config, _, err := ExecuteShareConfig(context.Background(), target, executor, ShareInput{Share: "data"})
	if err != nil {
		t.Fatalf("ExecuteShareConfig() error = %v", err)
	}
	if config.Share != "data" || config.SnapshotBrowsing || !config.LocalTimeFormat {
		t.Fatalf("config = %#v", config)
	}
	request := executor.requests[0]
	if request.Version != 1 || request.Method != "get_share_conf" || request.JSONParameters["name"] != "data" {
		t.Fatalf("get_share_conf request = %#v", request)
	}
	keys, ok := request.JSONParameters["sharesnapinfo"].([]string)
	if !ok || !reflect.DeepEqual(keys, []string{"snapshot_local_time_format", "enable_snapshot_browsing"}) {
		t.Fatalf("sharesnapinfo = %#v", request.JSONParameters["sharesnapinfo"])
	}
}

func TestRetentionDecodeLiveShape(t *testing.T) {
	// Shape confirmed live on DSM 7.3-81168 (Retention get, no task configured).
	target := coreTarget()
	executor := &capturingExecutor{responses: map[string]json.RawMessage{
		RetentionAPIName: json.RawMessage(`{"advDaily":7,"advHourly":24,"advMinimum":5,"advPolicyType":95,"advRetainDay":1,"advWeekly":2,"advYearly":0,"daily":7,"hourly":24,"monthly":1,"name":"data","policyType":0,"prefix":"share#","recently":0,"retainDay":7,"schedule":null,"tid":-1,"weekly":2,"yearly":0}`),
	}}
	policy, _, err := ExecuteRetention(context.Background(), target, executor, ShareInput{Share: "data"})
	if err != nil {
		t.Fatalf("ExecuteRetention() error = %v", err)
	}
	if policy.Share != "data" || policy.TaskID != -1 || policy.PolicyType != 0 || policy.RetainDays != 7 ||
		policy.Hourly != 24 || policy.Daily != 7 || policy.Weekly != 2 || policy.Monthly != 1 || policy.Yearly != 0 || policy.Scheduled {
		t.Fatalf("policy = %#v", policy)
	}
	request := executor.requests[0]
	if request.Method != "get" || request.JSONParameters["type"] != "share" || request.JSONParameters["name"] != "data" {
		t.Fatalf("retention request = %#v", request)
	}
	// A missing tid must be rejected, not defaulted.
	executor = &capturingExecutor{responses: map[string]json.RawMessage{RetentionAPIName: json.RawMessage(`{"policyType":0}`)}}
	if _, _, err := ExecuteRetention(context.Background(), target, executor, ShareInput{Share: "data"}); err == nil {
		t.Fatal("expected a decode error for a missing tid")
	}
}

func TestLogDecodeLiveShape(t *testing.T) {
	// Entry shape confirmed live on DSM 7.3-81168: string time, text in "event".
	target := coreTarget()
	executor := &capturingExecutor{responses: map[string]json.RawMessage{
		LogAPIName: json.RawMessage(`{"error_count":0,"info_count":1,"log_list":[{"event":"Took a shared folder snapshot [GMT+08-2026.07.21-02.11.04] from share [data] by [user].","level":"info","log_type":"drlog","time":"2026/07/21 02:11:05","user":"deryck"}],"offset":1,"total":1,"warn_count":0}`),
	}}
	page, _, err := ExecuteLog(context.Background(), target, executor, LogInput{Offset: 0, Limit: 50})
	if err != nil {
		t.Fatalf("ExecuteLog() error = %v", err)
	}
	if page.Total != 1 || page.ErrorCount != 0 || page.InfoCount != 1 || len(page.Entries) != 1 {
		t.Fatalf("page = %#v", page)
	}
	entry := page.Entries[0]
	if entry.Time != "2026/07/21 02:11:05" || entry.Level != "info" || entry.User != "deryck" || !strings.Contains(entry.Message, "Took a shared folder snapshot") {
		t.Fatalf("entry = %#v", entry)
	}
	if executor.requests[0].JSONParameters["limit"] != 50 {
		t.Fatalf("log request = %#v", executor.requests[0])
	}
}

func TestNodeDecodeLiveShape(t *testing.T) {
	// Shape confirmed live on DSM 7.3-81168 (DR.Node info).
	target := coreTarget()
	executor := &capturingExecutor{responses: map[string]json.RawMessage{
		NodeAPIName: json.RawMessage(`{"hostname":"Derek_3018xs","node_id":"82709d49-d8e2-4f83-90cf-8cfd9cae79d7","serial":"1790PXN037200"}`),
	}}
	node, _, err := ExecuteNode(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteNode() error = %v", err)
	}
	if node.Hostname != "Derek_3018xs" || node.NodeID != "82709d49-d8e2-4f83-90cf-8cfd9cae79d7" || node.Serial != "1790PXN037200" {
		t.Fatalf("node = %#v", node)
	}
	executor = &capturingExecutor{responses: map[string]json.RawMessage{NodeAPIName: json.RawMessage(`{"other":1}`)}}
	if _, _, err := ExecuteNode(context.Background(), target, executor); err == nil {
		t.Fatal("expected a decode error for an identity-free response")
	}
}

func TestPlansFailClosedWithoutPackage(t *testing.T) {
	// The plan API can linger advertised while the package is absent; the
	// package gate must still fail closed.
	target := coreTarget()
	target.SetAPI(PlanAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	if selection, err := SelectPlans(target); err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectPlans() without package = %#v, %v", selection, err)
	}
	// Core snapshot reads stay supported without the package.
	if selection, err := SelectSnapshots(target); err != nil || !selection.Supported {
		t.Fatalf("SelectSnapshots() without package = %#v, %v", selection, err)
	}
	if selection, err := SelectSnapshotCreate(target); err != nil || !selection.Supported {
		t.Fatalf("SelectSnapshotCreate() without package = %#v, %v", selection, err)
	}
}

func TestPlansDecodeWithPackage(t *testing.T) {
	target := packageTarget()
	// Real per-plan shape, live-verified against a nas51→nas255 relation on DSM
	// 7.4.7: base identity fields on the plan, enrichment blocks nested under
	// "additional"; readable_begin_time carries a trailing newline.
	executor := &capturingExecutor{responses: map[string]json.RawMessage{
		PlanAPIName: json.RawMessage(`{"plans":[{
			"plan_id":"plan-1","remote_plan_id":"rplan-9","role":1,"target_type":2,"target_name":"data",
			"additional":{
				"snapshot_count":3,
				"main_site_info":{"hostname":"nas51","node_id":"n-51","target_name":"data"},
				"dr_site_info":{"hostname":"nas255","node_id":"n-255","target_name":"data","status":"normal"},
				"sync_report":{"recent_records":[{"readable_begin_time":"Mon Jul 20 23:38:45 2026\n","is_success":true,"sync_size_byte":8990}]},
				"can_do":{"can_sync":true,"can_delete":true,"can_failover":false,"can_switchover":true,"can_testfailover":true}
			}
		}],"total":1}`),
	}}
	plans, selection, err := ExecutePlans(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecutePlans() error = %v", err)
	}
	if selection.Backend != "dr-plan-list-v1" || plans.Total != 1 || len(plans.Plans) != 1 {
		t.Fatalf("plans = %#v (selection %#v)", plans, selection)
	}
	plan := plans.Plans[0]
	if plan.ID != "plan-1" || plan.RemoteID != "rplan-9" || plan.Role != "main" || plan.TargetType != "share" ||
		plan.TargetName != "data" || plan.SnapshotCount != 3 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.MainSite.Hostname != "nas51" || plan.DRSite.Hostname != "nas255" || plan.DRSite.Status != "normal" {
		t.Fatalf("site info = %#v / %#v", plan.MainSite, plan.DRSite)
	}
	// The trailing newline on readable_begin_time must be trimmed.
	if plan.LastSyncTime != "Mon Jul 20 23:38:45 2026" || plan.LastSyncBytes != 8990 {
		t.Fatalf("sync report = %q %d", plan.LastSyncTime, plan.LastSyncBytes)
	}
	if !plan.Can.CanSync || !plan.Can.CanDelete || plan.Can.CanFailover || !plan.Can.CanSwitchover || !plan.Can.CanTestFailover {
		t.Fatalf("can = %#v", plan.Can)
	}
	if executor.requests[0].Method != "list" {
		t.Fatalf("plan request = %#v", executor.requests[0])
	}
}

func TestSnapshotCreateSendsSnapinfoAndDecodesTime(t *testing.T) {
	// The create response is the bare snapshot time string (live-verified).
	target := coreTarget()
	executor := &capturingExecutor{responses: map[string]json.RawMessage{
		ShareSnapshotAPIName: json.RawMessage(`"GMT+08-2026.07.21-02.11.04"`),
	}}
	description := "before upgrade"
	lock := true
	result, _, err := ExecuteSnapshotCreate(context.Background(), target, executor, CreateInput{Share: "data", Description: &description, Lock: &lock})
	if err != nil {
		t.Fatalf("ExecuteSnapshotCreate() error = %v", err)
	}
	if result.Snapshot != "GMT+08-2026.07.21-02.11.04" || result.Method != "create" || result.API != ShareSnapshotAPIName || result.Version != 1 {
		t.Fatalf("result = %#v", result)
	}
	request := executor.requests[0]
	info, ok := request.JSONParameters["snapinfo"].(map[string]any)
	if !ok || info["desc"] != "before upgrade" || info["lock"] != true {
		t.Fatalf("create snapinfo = %#v", request.JSONParameters)
	}

	// Without attributes the snapinfo envelope is omitted so DSM applies its
	// defaults.
	executor = &capturingExecutor{responses: map[string]json.RawMessage{ShareSnapshotAPIName: json.RawMessage(`"GMT+08-2026.07.21-03.00.00"`)}}
	if _, _, err := ExecuteSnapshotCreate(context.Background(), target, executor, CreateInput{Share: "data"}); err != nil {
		t.Fatalf("ExecuteSnapshotCreate() bare error = %v", err)
	}
	if _, present := executor.requests[0].JSONParameters["snapinfo"]; present {
		t.Fatalf("bare create request = %#v", executor.requests[0].JSONParameters)
	}
}

func TestSnapshotSetSendsPatchEnvelope(t *testing.T) {
	target := coreTarget()
	executor := &capturingExecutor{responses: map[string]json.RawMessage{ShareSnapshotAPIName: json.RawMessage(`{}`)}}
	lock := false
	result, _, err := ExecuteSnapshotSet(context.Background(), target, executor, SetInput{Share: "data", Snapshot: "GMT+08-2026.07.21-02.11.04", Lock: &lock})
	if err != nil {
		t.Fatalf("ExecuteSnapshotSet() error = %v", err)
	}
	if result.Method != "set" {
		t.Fatalf("result = %#v", result)
	}
	request := executor.requests[0]
	if request.JSONParameters["name"] != "data" || request.JSONParameters["snapshot"] != "GMT+08-2026.07.21-02.11.04" {
		t.Fatalf("set request = %#v", request.JSONParameters)
	}
	info, ok := request.JSONParameters["snapinfo"].(map[string]any)
	if !ok || info["lock"] != false {
		t.Fatalf("set snapinfo = %#v", request.JSONParameters["snapinfo"])
	}
	if _, present := info["desc"]; present {
		t.Fatalf("set snapinfo must omit an unpatched description: %#v", info)
	}
}

func TestSnapshotDeleteSendsTargets(t *testing.T) {
	target := coreTarget()
	executor := &capturingExecutor{responses: map[string]json.RawMessage{ShareSnapshotAPIName: json.RawMessage(`{}`)}}
	result, _, err := ExecuteSnapshotDelete(context.Background(), target, executor, DeleteInput{Share: "data", Snapshots: []string{"GMT+08-2026.07.21-02.11.04"}})
	if err != nil {
		t.Fatalf("ExecuteSnapshotDelete() error = %v", err)
	}
	if result.Method != "delete" {
		t.Fatalf("result = %#v", result)
	}
	request := executor.requests[0]
	targets, ok := request.JSONParameters["snapshots"].([]string)
	if !ok || !reflect.DeepEqual(targets, []string{"GMT+08-2026.07.21-02.11.04"}) {
		t.Fatalf("delete request = %#v", request.JSONParameters)
	}
}

func TestShareConfigSetSendsPatchEnvelope(t *testing.T) {
	target := coreTarget()
	executor := &capturingExecutor{responses: map[string]json.RawMessage{ShareSnapshotAPIName: json.RawMessage(`{}`)}}
	browsing := true
	result, _, err := ExecuteShareConfigSet(context.Background(), target, executor, ShareConfigSetInput{Share: "data", SnapshotBrowsing: &browsing})
	if err != nil {
		t.Fatalf("ExecuteShareConfigSet() error = %v", err)
	}
	if result.Method != "set_share_conf" {
		t.Fatalf("result = %#v", result)
	}
	request := executor.requests[0]
	if request.Method != "set_share_conf" {
		t.Fatalf("request = %#v", request)
	}
	info, ok := request.JSONParameters["sharesnapinfo"].(map[string]any)
	if !ok || info["enable_snapshot_browsing"] != true {
		t.Fatalf("sharesnapinfo = %#v", request.JSONParameters["sharesnapinfo"])
	}
	if _, present := info["snapshot_local_time_format"]; present {
		t.Fatalf("sharesnapinfo must omit an unpatched field: %#v", info)
	}
}
