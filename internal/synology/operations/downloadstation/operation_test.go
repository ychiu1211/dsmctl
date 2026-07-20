package downloadstation

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/downloadstation"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type captureExecutor struct {
	requests []compatibility.Request
	response string
}

func (e *captureExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	e.requests = append(e.requests, request)
	return json.RawMessage(e.response), nil
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

func dsTarget(packageVersion string) compatibility.Target {
	target := compatibility.NewTarget()
	target.SetAPI(InfoAPIName, compatibility.APIInfo{Path: "DownloadStation/info.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(ScheduleAPIName, compatibility.APIInfo{Path: "DownloadStation/schedule.cgi", MinVersion: 1, MaxVersion: 1})
	target.SetAPI(StatisticAPIName, compatibility.APIInfo{Path: "DownloadStation/statistic.cgi", MinVersion: 1, MaxVersion: 1})
	target.SetAPI(TaskAPIName, compatibility.APIInfo{Path: "DownloadStation/task.cgi", MinVersion: 1, MaxVersion: 3})
	for _, api := range []string{
		SettingsGlobalAPIName, SettingsBTAPIName, SettingsEmuleAPIName, SettingsEmuleLocationAPIName,
		SettingsFtpHttpAPIName, SettingsNzbAPIName, SettingsAutoExtractionAPIName, SettingsLocationAPIName,
		SettingsRssAPIName, SettingsSchedulerAPIName,
	} {
		target.SetAPI(api, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	}
	if packageVersion != "" {
		target.SetInstalledPackages([]compatibility.InstalledPackage{
			{ID: PackageID, Version: compatibility.ParsePackageVersion(packageVersion), Running: true},
		})
	}
	return target
}

// Shapes below are the live payloads captured on Download Station 4.1.2-5012.
const (
	infoBody      = `{"is_manager":true,"version":5012,"version_string":"4.1-5012"}`
	configBody    = `{"bt_max_download":0,"bt_max_upload":20,"default_destination":null,"emule_enabled":false,"emule_max_download":0,"emule_max_upload":20,"ftp_max_download":0,"http_max_download":0,"nzb_max_download":0,"unzip_service_enabled":false}`
	scheduleBody  = `{"emule_enabled":false,"enabled":false}`
	statisticBody = `{"speed_download":0,"speed_upload":0}`
	tasksEmpty    = `{"offset":0,"tasks":[],"total":0}`
	// A populated task with "size" as a quoted string exercises flexInt64.
	tasksBody = `{"offset":0,"total":1,"tasks":[{"id":"dbid_1","type":"http","username":"deryck","title":"file.iso","size":"1048576","status":"downloading","additional":{"detail":{"destination":"downloads"},"transfer":{"size_downloaded":"524288","size_uploaded":0,"speed_download":1000,"speed_upload":0}}}]}`
)

func TestServiceComposesInfoConfigSchedule(t *testing.T) {
	target := dsTarget("4.1.2-5012")
	state, selection, err := ExecuteService(context.Background(), target, routeExecutor{t: t, routes: map[string]string{
		"SYNO.DownloadStation.Info getinfo":       infoBody,
		"SYNO.DownloadStation.Info getconfig":     configBody,
		"SYNO.DownloadStation.Schedule getconfig": scheduleBody,
	}})
	if err != nil {
		t.Fatalf("ExecuteService() error = %v", err)
	}
	if !selection.Supported {
		t.Fatalf("selection = %#v", selection)
	}
	if state.Version != "4.1-5012" || !state.IsManager {
		t.Fatalf("info = %#v", state)
	}
	if state.Config.BTMaxUploadKBs != 20 || state.Config.EmuleEnabled || state.Config.UnzipServiceEnabled || state.Config.DefaultDestination != "" {
		t.Fatalf("config = %#v", state.Config)
	}
	if state.Schedule.Enabled || state.Schedule.EmuleEnabled {
		t.Fatalf("schedule = %#v", state.Schedule)
	}
}

func TestTasksDecodePopulatedWithStringNumbers(t *testing.T) {
	target := dsTarget("4.1.2-5012")
	tasks, _, err := ExecuteTask(context.Background(), target, routeExecutor{t: t, routes: map[string]string{
		"SYNO.DownloadStation.Task list": tasksBody,
	}})
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}
	if tasks.Total != 1 || len(tasks.Tasks) != 1 {
		t.Fatalf("tasks = %#v", tasks)
	}
	task := tasks.Tasks[0]
	if task.ID != "dbid_1" || task.Type != "http" || task.Title != "file.iso" || task.Size != 1048576 || task.Status != "downloading" || task.Destination != "downloads" {
		t.Fatalf("task = %#v", task)
	}
	if task.Transfer.SizeDownloaded != 524288 || task.Transfer.SpeedDownload != 1000 {
		t.Fatalf("transfer = %#v", task.Transfer)
	}
}

func TestTasksEmpty(t *testing.T) {
	target := dsTarget("4.1.2-5012")
	tasks, _, err := ExecuteTask(context.Background(), target, routeExecutor{t: t, routes: map[string]string{
		"SYNO.DownloadStation.Task list": tasksEmpty,
	}})
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}
	if tasks.Total != 0 || len(tasks.Tasks) != 0 {
		t.Fatalf("tasks = %#v", tasks)
	}
}

func TestStatisticsDecode(t *testing.T) {
	target := dsTarget("4.1.2-5012")
	stats, _, err := ExecuteStatistic(context.Background(), target, routeExecutor{t: t, routes: map[string]string{
		"SYNO.DownloadStation.Statistic getinfo": statisticBody,
	}})
	if err != nil {
		t.Fatalf("ExecuteStatistic() error = %v", err)
	}
	if stats.SpeedDownload != 0 || stats.SpeedUpload != 0 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestSettingsComposesAllGroups(t *testing.T) {
	// Live shapes captured on Download Station 4.1.2-5012.
	target := dsTarget("4.1.2-5012")
	settings, selection, err := ExecuteSettings(context.Background(), target, routeExecutor{t: t, routes: map[string]string{
		"SYNO.DownloadStation2.Settings.Global get":         `{"download_volume":"/volume1","enable_emule":false,"enable_unzip_service":false}`,
		"SYNO.DownloadStation2.Settings.BT get":             `{"dht_port":6881,"enable_dht":true,"enable_port_forwarding":false,"enable_preview":true,"enable_seeding_auto_remove":false,"encrypt":"auto","max_download_rate":0,"max_peer":50,"max_upload_rate":20,"seeding_interval":0,"seeding_ratio":0,"tcp_port":16881}`,
		"SYNO.DownloadStation2.Settings.Emule get":          `{"enable_emule":false}`,
		"SYNO.DownloadStation2.Settings.Emule.Location get": `{"default_destination":"emule/incoming"}`,
		"SYNO.DownloadStation2.Settings.FtpHttp get":        `{"enable_ftp_max_conn":false,"ftp_http_max_download_rate":0,"ftp_max_conn":3}`,
		"SYNO.DownloadStation2.Settings.Nzb get":            `{"conn_per_download":2,"enable_auth":false,"enable_encryption":false,"enable_parchive":true,"enable_remove_parfiles":false,"max_download_rate":0,"port":119,"server":"","username":""}`,
		"SYNO.DownloadStation2.Settings.AutoExtraction get": `{"create_subfolder":false,"delete_archive":false,"enable_unzip":false,"enable_unzip_service":false,"passwords":["secret"],"unzip_location":"current_folder","unzip_overwrite":false,"unzip_to_path":"","username":""}`,
		"SYNO.DownloadStation2.Settings.Location get":       `{"default_destination":"downloads","enable_delete_torrent_nzb_watch":false,"enable_torrent_nzb_watch":false,"torrent_nzb_watch_folder":""}`,
		"SYNO.DownloadStation2.Settings.Rss get":            `{"update_interval":1440}`,
		"SYNO.DownloadStation2.Settings.Scheduler get":      `{"download_rate":0,"enable_schedule":false,"max_tasks":10,"max_tasks_limit":80,"order":"request","schedule":"1111","upload_rate":0}`,
	}})
	if err != nil {
		t.Fatalf("ExecuteSettings() error = %v", err)
	}
	if !selection.Supported {
		t.Fatalf("selection = %#v", selection)
	}
	if settings.Global.DownloadVolume != "/volume1" {
		t.Fatalf("global = %#v", settings.Global)
	}
	if settings.BT.TCPPort != 16881 || settings.BT.DHTPort != 6881 || !settings.BT.EnableDHT || settings.BT.Encryption != "auto" || settings.BT.MaxUploadRate != 20 || settings.BT.MaxPeer != 50 {
		t.Fatalf("bt = %#v", settings.BT)
	}
	if settings.Emule.Enabled || settings.Emule.DefaultDestination != "emule/incoming" {
		t.Fatalf("emule = %#v", settings.Emule)
	}
	if settings.FtpHttp.MaxConn != 3 || settings.Nzb.Port != 119 || !settings.Nzb.EnableParchive {
		t.Fatalf("ftphttp/nzb = %#v %#v", settings.FtpHttp, settings.Nzb)
	}
	// The archive password value must never surface; only the boolean does.
	if !settings.AutoExtraction.PasswordConfigured {
		t.Fatalf("auto-extraction password flag = %#v", settings.AutoExtraction)
	}
	if got := fmt.Sprintf("%#v", settings.AutoExtraction); strings.Contains(got, "secret") {
		t.Fatalf("auto-extraction leaked the archive password: %s", got)
	}
	if settings.Location.DefaultDestination != "downloads" || settings.Rss.UpdateIntervalMinutes != 1440 {
		t.Fatalf("location/rss = %#v %#v", settings.Location, settings.Rss)
	}
	if settings.Scheduler.MaxTasks != 10 || settings.Scheduler.MaxTasksLimit != 80 || settings.Scheduler.Order != "request" || settings.Scheduler.ScheduleBitmap != "1111" {
		t.Fatalf("scheduler = %#v", settings.Scheduler)
	}
}

func TestSettingsFailsClosedWithoutPackage(t *testing.T) {
	target := dsTarget("")
	if selection, err := SelectSettings(target); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("SelectSettings without package = %#v, %v", selection, err)
	}
}

func TestTaskWriteCreateCapturesParams(t *testing.T) {
	target := dsTarget("4.1.2-5012")
	exec := &captureExecutor{response: `{}`}
	result, selection, err := ExecuteTaskWrite(context.Background(), target, exec, downloadstation.TaskChange{
		Action: downloadstation.TaskActionCreate, URIs: []string{"http://x/a.zip", "http://x/b.zip"}, Destination: "Share",
	})
	if err != nil {
		t.Fatalf("ExecuteTaskWrite(create) error = %v", err)
	}
	if !selection.Supported || result.Method != "create" || result.API != TaskAPIName {
		t.Fatalf("result = %#v", result)
	}
	if len(exec.requests) != 1 {
		t.Fatalf("requests = %#v", exec.requests)
	}
	req := exec.requests[0]
	if req.Method != "create" || req.Parameters.Get("uri") != "http://x/a.zip,http://x/b.zip" || req.Parameters.Get("destination") != "Share" {
		t.Fatalf("create request = %#v", req)
	}
}

func TestTaskWriteControlCapturesParamsAndAffectedIDs(t *testing.T) {
	target := dsTarget("4.1.2-5012")
	for _, action := range []downloadstation.TaskAction{downloadstation.TaskActionPause, downloadstation.TaskActionResume} {
		exec := &captureExecutor{response: `[{"id":"dbid_1","error":0},{"id":"dbid_2","error":0}]`}
		result, _, err := ExecuteTaskWrite(context.Background(), target, exec, downloadstation.TaskChange{
			Action: action, TaskIDs: []string{"dbid_1", "dbid_2"},
		})
		if err != nil {
			t.Fatalf("ExecuteTaskWrite(%s) error = %v", action, err)
		}
		req := exec.requests[0]
		if req.Method != string(action) || req.Parameters.Get("id") != "dbid_1,dbid_2" {
			t.Fatalf("%s request = %#v", action, req)
		}
		if !reflect.DeepEqual(result.AffectedIDs, []string{"dbid_1", "dbid_2"}) {
			t.Fatalf("%s affected = %#v", action, result.AffectedIDs)
		}
	}
}

func TestTaskWriteDeleteSendsForceComplete(t *testing.T) {
	target := dsTarget("4.1.2-5012")
	exec := &captureExecutor{response: `[{"id":"dbid_1","error":0}]`}
	_, _, err := ExecuteTaskWrite(context.Background(), target, exec, downloadstation.TaskChange{
		Action: downloadstation.TaskActionDelete, TaskIDs: []string{"dbid_1"}, ForceComplete: true,
	})
	if err != nil {
		t.Fatalf("ExecuteTaskWrite(delete) error = %v", err)
	}
	req := exec.requests[0]
	if req.Method != "delete" || req.Parameters.Get("id") != "dbid_1" || req.Parameters.Get("force_complete") != "true" {
		t.Fatalf("delete request = %#v", req)
	}
}

func TestTaskWriteControlSurfacesPerIDFailure(t *testing.T) {
	target := dsTarget("4.1.2-5012")
	exec := &captureExecutor{response: `[{"id":"dbid_1","error":0},{"id":"dbid_2","error":404}]`}
	_, _, err := ExecuteTaskWrite(context.Background(), target, exec, downloadstation.TaskChange{
		Action: downloadstation.TaskActionPause, TaskIDs: []string{"dbid_1", "dbid_2"},
	})
	if err == nil || !strings.Contains(err.Error(), "dbid_2 (error 404)") {
		t.Fatalf("expected per-id failure, got %v", err)
	}
}

func TestTaskEditCapturesParamsAndFallsBackToRequestedIDs(t *testing.T) {
	target := dsTarget("4.1.2-5012")
	executor := &captureExecutor{response: `{}`}
	change := downloadstation.TaskChange{
		Action: downloadstation.TaskActionEdit, TaskIDs: []string{"dbid_1", "dbid_2"}, Destination: "Share/sub",
	}
	result, _, err := ExecuteTaskEdit(context.Background(), target, executor, change)
	if err != nil {
		t.Fatalf("ExecuteTaskEdit() error = %v", err)
	}
	request := executor.requests[0]
	if request.API != TaskAPIName || request.Version != 2 || request.Method != "edit" ||
		request.Parameters.Get("id") != "dbid_1,dbid_2" || request.Parameters.Get("destination") != "Share/sub" {
		t.Fatalf("edit request = %#v", request)
	}
	if result.Method != "edit" || !reflect.DeepEqual(result.AffectedIDs, []string{"dbid_1", "dbid_2"}) {
		t.Fatalf("edit result = %#v", result)
	}
}

func TestTaskEditFailsClosedWithoutTaskV2(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(TaskAPIName, compatibility.APIInfo{Path: "DownloadStation/task.cgi", MinVersion: 1, MaxVersion: 1})
	target.SetInstalledPackages([]compatibility.InstalledPackage{
		{ID: PackageID, Version: compatibility.ParsePackageVersion("4.1.2-5012"), Running: true},
	})
	if selection, err := SelectTaskEdit(target); err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectTaskEdit() on Task v1 = %#v, %v", selection, err)
	}
}

func TestSettingsSetInputsCarryResolvedSecretsOnly(t *testing.T) {
	password := "secret-value"
	nzbValues := encodeNzbSetInput(NzbSetInput{
		Change:   downloadstation.NzbSettingsChange{PasswordRef: stringPointerForTest("env:NZB_PASSWORD")},
		Password: &password,
	})
	if nzbValues.Get("password") != "secret-value" {
		t.Fatalf("nzb password param = %q", nzbValues.Get("password"))
	}
	if len(nzbValues) != 1 {
		t.Fatalf("nzb values leaked the credential reference: %#v", nzbValues)
	}
	// Without a resolved secret, the reference must never reach DSM.
	nzbValues = encodeNzbSetInput(NzbSetInput{
		Change: downloadstation.NzbSettingsChange{PasswordRef: stringPointerForTest("env:NZB_PASSWORD")},
	})
	if len(nzbValues) != 0 {
		t.Fatalf("nzb values without a secret = %#v", nzbValues)
	}

	// Clearing sends the JSON empty-string literal: a bare empty form value is
	// dropped by DSM's JSON-request parser (live-verified with username).
	empty := ""
	nzbValues = encodeNzbSetInput(NzbSetInput{
		Change:   downloadstation.NzbSettingsChange{},
		Password: &empty,
	})
	if nzbValues.Get("password") != `""` {
		t.Fatalf("nzb empty password param = %q", nzbValues.Get("password"))
	}

	passwords := []string{"a", "b"}
	extractValues := encodeAutoExtractionSetInput(AutoExtractionSetInput{
		Change:    downloadstation.AutoExtractionSettingsChange{PasswordsRef: stringPointerForTest("env:EXTRACT_PASSWORDS")},
		Passwords: &passwords,
	})
	if extractValues.Get("passwords") != `["a","b"]` || len(extractValues) != 1 {
		t.Fatalf("extract values = %#v", extractValues)
	}
}

func stringPointerForTest(value string) *string { return &value }

func TestTaskWriteFailsClosedWithoutPackage(t *testing.T) {
	target := dsTarget("")
	if selection, err := SelectTaskWrite(target); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("SelectTaskWrite without package = %#v, %v", selection, err)
	}
}

func TestBTSetEncodesFullObject(t *testing.T) {
	target := dsTarget("4.1.2-5012")
	exec := &captureExecutor{response: `{}`}
	result, selection, err := ExecuteBTSet(context.Background(), target, exec, downloadstation.BTSettings{
		TCPPort: 16881, DHTPort: 6881, EnableDHT: true, EnablePortForwarding: false, EnablePreview: true,
		Encryption: "auto", MaxDownloadRate: 0, MaxUploadRate: 20, MaxPeer: 50,
		SeedingRatio: 0, SeedingInterval: 0, EnableSeedingAutoRemove: false,
	})
	if err != nil {
		t.Fatalf("ExecuteBTSet() error = %v", err)
	}
	if !selection.Supported || result.Method != "set" || result.Group != "bt" || result.API != SettingsBTAPIName {
		t.Fatalf("result = %#v", result)
	}
	req := exec.requests[0]
	want := map[string]string{
		"tcp_port": "16881", "dht_port": "6881", "enable_dht": "true", "enable_port_forwarding": "false",
		"enable_preview": "true", "encrypt": "auto", "max_download_rate": "0", "max_upload_rate": "20",
		"max_peer": "50", "seeding_ratio": "0", "seeding_interval": "0", "enable_seeding_auto_remove": "false",
	}
	for key, value := range want {
		if got := req.Parameters.Get(key); got != value {
			t.Fatalf("set param %q = %q, want %q", key, got, value)
		}
	}
}

func TestSettingsWriteFailsClosedWithoutPackage(t *testing.T) {
	target := dsTarget("")
	if selection, err := SelectSettingsWrite(target); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("SelectSettingsWrite without package = %#v, %v", selection, err)
	}
}

func TestFtpHttpSetEncodesFullObject(t *testing.T) {
	target := dsTarget("4.1.2-5012")
	exec := &captureExecutor{response: `{}`}
	result, _, err := ExecuteFtpHttpSet(context.Background(), target, exec, downloadstation.FtpHttpSettings{
		MaxDownloadRate: 100, EnableMaxConn: true, MaxConn: 5,
	})
	if err != nil {
		t.Fatalf("ExecuteFtpHttpSet() error = %v", err)
	}
	if result.Group != "ftp_http" || result.Method != "set" {
		t.Fatalf("result = %#v", result)
	}
	req := exec.requests[0]
	if req.Parameters.Get("ftp_http_max_download_rate") != "100" || req.Parameters.Get("enable_ftp_max_conn") != "true" || req.Parameters.Get("ftp_max_conn") != "5" {
		t.Fatalf("ftphttp set params = %#v", req.Parameters)
	}
}

func TestFailsClosedWithoutPackage(t *testing.T) {
	// APIs present but the package catalog does not contain DownloadStation.
	target := dsTarget("")
	for name, selectArea := range map[string]func(compatibility.Target) (compatibility.Selection, error){
		"service":   SelectService,
		"task":      SelectTask,
		"statistic": SelectStatistic,
	} {
		selection, err := selectArea(target)
		if !compatibility.IsUnsupported(err) || selection.Supported {
			t.Fatalf("%s selection = %#v, err = %v", name, selection, err)
		}
	}
}

func TestFailsClosedBelowBaselineVersion(t *testing.T) {
	target := dsTarget("2.5.0-1000")
	if selection, err := SelectService(target); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("SelectService below baseline = %#v, %v", selection, err)
	}
}

func TestDecodersRejectMalformedShapes(t *testing.T) {
	tests := []struct {
		name string
		fn   func(json.RawMessage) error
		data string
		want string
	}{
		{name: "info not object", fn: func(d json.RawMessage) error { _, e := decodeInfo(d); return e }, data: `[]`, want: "expected an object"},
		{name: "info missing is_manager", fn: func(d json.RawMessage) error { _, e := decodeInfo(d); return e }, data: `{"version_string":"x"}`, want: "is_manager"},
		{name: "config missing emule_enabled", fn: func(d json.RawMessage) error { _, e := decodeConfig(d); return e }, data: `{"bt_max_upload":1}`, want: "emule_enabled"},
		{name: "schedule missing enabled", fn: func(d json.RawMessage) error { _, e := decodeSchedule(d); return e }, data: `{"emule_enabled":false}`, want: "enabled"},
		{name: "statistics missing speed_download", fn: func(d json.RawMessage) error { _, e := decodeStatistics(d); return e }, data: `{"speed_upload":0}`, want: "speed_download"},
		{name: "tasks missing tasks", fn: func(d json.RawMessage) error { _, e := decodeTasks(d); return e }, data: `{"total":0}`, want: "tasks"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.fn(json.RawMessage(test.data)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestEncodeSchedulerSettingsQuotesBitmapAndOmitsMaxTasksLimit(t *testing.T) {
	// DSM's entry.cgi parses each form value as JSON, so the all-digit bitmap
	// must be a quoted JSON string or it parses as a number and fails the
	// Param.String check (code 120). max_tasks_limit is a get-only field and is
	// not a set param, so it must not be sent.
	v := encodeSchedulerSettings(downloadstation.SchedulerSettings{
		MaxTasks:       8,
		MaxTasksLimit:  80,
		Order:          "request",
		ScheduleBitmap: "1010",
	})
	if got := v.Get("schedule"); got != `"1010"` {
		t.Fatalf("schedule = %q, want %q", got, `"1010"`)
	}
	if v.Has("max_tasks_limit") {
		t.Fatalf("max_tasks_limit must not be sent on set, got %q", v.Get("max_tasks_limit"))
	}
	if got := v.Get("max_tasks"); got != "8" {
		t.Fatalf("max_tasks = %q, want %q", got, "8")
	}
}

func TestDecodeLocationSettingsNormalizesNullSentinel(t *testing.T) {
	// DSM returns "(null)" for an unset watch folder; echoing that literal back
	// on a set fails path validation (code 522), so the model must carry "".
	got, err := decodeLocationSettings(json.RawMessage(`{"default_destination":"(null)","enable_torrent_nzb_watch":false,"enable_delete_torrent_nzb_watch":false,"torrent_nzb_watch_folder":"(null)"}`))
	if err != nil {
		t.Fatalf("decodeLocationSettings: %v", err)
	}
	if got.TorrentNzbWatchFolder != "" {
		t.Fatalf("TorrentNzbWatchFolder = %q, want empty", got.TorrentNzbWatchFolder)
	}
	if got.DefaultDestination != "" {
		t.Fatalf("DefaultDestination = %q, want empty", got.DefaultDestination)
	}
}

func TestEncodeAutoExtractionChangeIsPartialAndNeverSendsPasswords(t *testing.T) {
	// A partial set must send only the patched fields; unspecified fields
	// (including passwords, which the read never returns) must be absent so the
	// handler leaves them untouched.
	local := true
	v := encodeAutoExtractionChange(downloadstation.AutoExtractionSettingsChange{
		DeleteArchive: boolPtr(true),
		UnzipToLocal:  &local,
	})
	if got := v.Get("delete_archive"); got != "true" {
		t.Fatalf("delete_archive = %q, want true", got)
	}
	if got := v.Get("unzip_location"); got != "true" {
		t.Fatalf("unzip_location = %q, want true", got)
	}
	for _, absent := range []string{"passwords", "enable_unzip", "create_subfolder", "unzip_overwrite", "unzip_to_path"} {
		if v.Has(absent) {
			t.Fatalf("unspecified field %q must not be sent, got %q", absent, v.Get(absent))
		}
	}
}

func boolPtr(b bool) *bool { return &b }

func intPtr(i int) *int { return &i }

func TestEncodeNzbChangeIsPartialAndNeverSendsPassword(t *testing.T) {
	v := encodeNzbChange(downloadstation.NzbSettingsChange{
		ConnPerDownload: intPtr(4),
		EnableAuth:      boolPtr(true),
	})
	if got := v.Get("conn_per_download"); got != "4" {
		t.Fatalf("conn_per_download = %q, want 4", got)
	}
	if got := v.Get("enable_auth"); got != "true" {
		t.Fatalf("enable_auth = %q, want true", got)
	}
	for _, absent := range []string{"password", "server", "port", "username", "max_download_rate", "enable_encryption"} {
		if v.Has(absent) {
			t.Fatalf("unspecified field %q must not be sent, got %q", absent, v.Get(absent))
		}
	}
}

func TestAPINamesCoverLegacyAndSettings(t *testing.T) {
	got := APINames()
	want := []string{
		InfoAPIName, ScheduleAPIName, StatisticAPIName, TaskAPIName,
		SettingsGlobalAPIName, SettingsBTAPIName, SettingsEmuleAPIName, SettingsEmuleLocationAPIName,
		SettingsFtpHttpAPIName, SettingsNzbAPIName, SettingsAutoExtractionAPIName, SettingsLocationAPIName,
		SettingsRssAPIName, SettingsSchedulerAPIName,
	}
	if len(got) != len(want) {
		t.Fatalf("APINames() = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("APINames() = %#v, want %#v", got, want)
		}
	}
}
