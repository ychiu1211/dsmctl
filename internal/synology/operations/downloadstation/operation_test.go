package downloadstation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

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

func TestAPINamesCoverLegacySurface(t *testing.T) {
	got := APINames()
	want := []string{InfoAPIName, ScheduleAPIName, StatisticAPIName, TaskAPIName}
	if len(got) != len(want) {
		t.Fatalf("APINames() = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("APINames() = %#v, want %#v", got, want)
		}
	}
}
