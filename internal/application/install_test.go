package application

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const installSysCrashState = `{"success":true,"data":{"has_disk":true,"dsinfo":{
  "model":"DS918+","serial":"17C0PDN818400","build_ver":"7.3.1-86003","disk_count":4,
  "internet_reinstall_ok":true,"internet_reinstall_version":"DSM 7.3.1-86003","status":"sys_crash"}}}`

func installStateAndTriggerServer(t *testing.T, installed *bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webman/get_state.cgi":
			_, _ = w.Write([]byte(installSysCrashState))
		case "/webman/install.cgi":
			*installed = true
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestInstallDiscoveredNASDetectsWithoutTriggering(t *testing.T) {
	triggered := false
	server := installStateAndTriggerServer(t, &triggered)
	defer server.Close()
	service := NewService(nil, nil)
	status, err := service.InstallDiscoveredNAS(context.Background(), InstallRequest{URL: server.URL})
	if err != nil {
		t.Fatalf("InstallDiscoveredNAS() error = %v", err)
	}
	if status.State != "sys_crash" || status.InstallKind != "reinstall" || !status.OnlineAvailable || status.OnlineVersion != "DSM 7.3.1-86003" {
		t.Fatalf("status = %#v", status)
	}
	if status.InstallStarted || triggered {
		t.Fatal("detect-only must not trigger an install")
	}
}

func TestInstallDiscoveredNASTriggersOnlineInstall(t *testing.T) {
	triggered := false
	server := installStateAndTriggerServer(t, &triggered)
	defer server.Close()
	service := NewService(nil, nil)
	status, err := service.InstallDiscoveredNAS(context.Background(), InstallRequest{URL: server.URL, Trigger: true})
	if err != nil {
		t.Fatalf("InstallDiscoveredNAS(trigger) error = %v", err)
	}
	if !status.InstallStarted || !triggered {
		t.Fatalf("install was not triggered: status=%#v triggered=%v", status, triggered)
	}
}

func TestInstallDiscoveredNASRejectsPublicHost(t *testing.T) {
	service := NewService(nil, nil)
	if _, err := service.InstallDiscoveredNAS(context.Background(), InstallRequest{URL: "http://8.8.8.8:5000"}); err == nil || !strings.Contains(err.Error(), "LAN/VPN") {
		t.Fatalf("public-host error = %v", err)
	}
}
