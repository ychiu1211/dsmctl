package provision

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The JSON bodies below are the exact live shapes captured from DS918+ units:
// 192.0.2.51 (freshly reset, no internet) and 192.0.2.255 (crashed, online
// reinstall available), plus the install-progress shape observed during a live
// online reinstall.

const notInstallState = `{"success":true,"data":{"has_disk":true,"dsinfo":{
  "model":"DS918+","serial":"TESTSERIAL0001","mac_addr":"00:00:5E:00:53:01","ip_addr":"192.0.2.51","hostname":"DiskStation",
  "build_ver":"7.3.2-86009","disk_count":4,"disk_size_enough":true,"is_installing":false,"https_admin_port":"5001",
  "internet_ok":"false","internet_install_ok":false,"internet_migrate_ok":false,"internet_reinstall_ok":false,
  "internet_install_version":"","status":"not_install"}}}`

const sysCrashOnlineState = `{"success":true,"data":{"has_disk":true,"dsinfo":{
  "model":"DS918+","serial":"TESTSERIAL0002","mac_addr":"00:00:5E:00:53:02","ip_addr":"192.0.2.255","hostname":"DiskStation",
  "build_ver":"7.3-81067","disk_count":4,"disk_size_enough":true,"is_installing":false,"https_admin_port":"5001",
  "internet_ok":"true","internet_install_ok":true,"internet_migrate_ok":true,"internet_reinstall_ok":true,
  "internet_reinstall_version":"DSM 7.3.1-86003","status":"sys_crash"}}}`

func stateServer(t *testing.T, body string) (Target, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/webman/get_state.cgi" {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	return Target{BaseURL: server.URL, HTTPClient: server.Client()}, server.Close
}

func TestGetStateNotInstallNoInternet(t *testing.T) {
	target, closeFn := stateServer(t, notInstallState)
	defer closeFn()
	state, err := GetState(context.Background(), target)
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	if state.Status != "not_install" || state.Installed() || state.NeedsInstall() != "not_install" {
		t.Fatalf("state = %#v", state)
	}
	if state.Model != "DS918+" || state.Serial != "TESTSERIAL0001" || state.DiskCount != 4 {
		t.Fatalf("device fields = %#v", state)
	}
	plan := state.OnlineInstallPlan()
	if plan.Kind != "install" || plan.Available {
		t.Fatalf("plan = %#v; want install unavailable (no internet)", plan)
	}
}

func TestGetStateSysCrashOnlineAvailable(t *testing.T) {
	target, closeFn := stateServer(t, sysCrashOnlineState)
	defer closeFn()
	state, err := GetState(context.Background(), target)
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	if state.Status != "sys_crash" || state.NeedsInstall() != "sys_crash" {
		t.Fatalf("state = %#v", state)
	}
	plan := state.OnlineInstallPlan()
	if plan.Kind != "reinstall" || !plan.Available || plan.Status != "sys_crash" || plan.Version != "DSM 7.3.1-86003" {
		t.Fatalf("plan = %#v; want reinstall available DSM 7.3.1-86003", plan)
	}
}

func TestGetInstallProgressParsesStageAndStringPercent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success": true, "data": {"stage": "download", "progress": "37"}}`))
	}))
	defer server.Close()
	target := Target{BaseURL: server.URL, HTTPClient: server.Client()}
	progress, err := GetInstallProgress(context.Background(), target)
	if err != nil {
		t.Fatalf("GetInstallProgress() error = %v", err)
	}
	if progress.Stage != "download" || progress.Percent != 37 {
		t.Fatalf("progress = %#v; want stage=download percent=37", progress)
	}
}

func TestGetInstallProgressEmptyMeansIdle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	target := Target{BaseURL: server.URL, HTTPClient: server.Client()}
	progress, err := GetInstallProgress(context.Background(), target)
	if err != nil || progress.Raw != "" || progress.Percent != 0 {
		t.Fatalf("idle progress = %#v, %v", progress, err)
	}
}

func TestDefaultPatURL(t *testing.T) {
	url, name, err := DefaultPatURL(DeviceState{Model: "DS918+", BuildVersion: "7.3.2-86009"})
	if err != nil {
		t.Fatalf("DefaultPatURL() error = %v", err)
	}
	wantURL := "https://global.synologydownload.com/download/DSM/release/7.3.2/86009/DSM_DS918%2B_86009.pat"
	if url != wantURL {
		t.Fatalf("url = %q, want %q", url, wantURL)
	}
	if name != "DSM_DS918+_86009.pat" {
		t.Fatalf("filename = %q", name)
	}
	if _, _, err := DefaultPatURL(DeviceState{Model: "DS918+", BuildVersion: "garbage"}); err == nil {
		t.Fatal("DefaultPatURL accepted a malformed build")
	}
}

func TestInstallLocalPostsMultipartImage(t *testing.T) {
	var gotField, gotFilename, gotQuery, gotContentType string
	var gotBytes []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotContentType = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			for field, files := range r.MultipartForm.File {
				gotField = field
				gotFilename = files[0].Filename
				f, _ := files[0].Open()
				gotBytes, _ = io.ReadAll(f)
				_ = f.Close()
			}
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	target := Target{BaseURL: server.URL, HTTPClient: server.Client()}
	image := "PATBYTES"
	if err := InstallLocal(context.Background(), target, "not_install", "DSM_DS918+_86009.pat", strings.NewReader(image), int64(len(image))); err != nil {
		t.Fatalf("InstallLocal() error = %v", err)
	}
	if gotField != "filename" || gotFilename != "DSM_DS918+_86009.pat" || string(gotBytes) != "PATBYTES" {
		t.Fatalf("multipart field=%q filename=%q bytes=%q", gotField, gotFilename, gotBytes)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Fatalf("content-type = %q", gotContentType)
	}
	if !strings.Contains(gotQuery, "upload=true") || !strings.Contains(gotQuery, "status=not_install") || !strings.Contains(gotQuery, "localinstallreq=false") {
		t.Fatalf("query = %q", gotQuery)
	}
}

func TestOnlineInstallPlanStatusMapping(t *testing.T) {
	cases := map[string]string{"not_install": "not_install", "sys_crash": "sys_crash", "sys_migrat": "sys_migrat", "ready": ""}
	for status, wantParam := range cases {
		plan := DeviceState{Status: status}.OnlineInstallPlan()
		if plan.Status != wantParam {
			t.Fatalf("status %q -> install param %q, want %q", status, plan.Status, wantParam)
		}
	}
}
