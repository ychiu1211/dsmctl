package synology

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newDriveAdminTestServer fakes the DSM surface a package-scoped Drive read
// touches: discovery, login, the System Info bootstrap, the Package Center
// inventory used for the catalog refresh, and the Drive Admin APIs.
func newDriveAdminTestServer(t *testing.T, driveVersion string, running bool, inventoryCalls *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		api := r.Form.Get("api")
		method := r.Form.Get("method")
		w.Header().Set("Content-Type", "application/json")
		switch api + "." + method {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{
				"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7},
				"SYNO.Core.System":{"path":"entry.cgi","minVersion":1,"maxVersion":3},
				"SYNO.Core.Package":{"path":"entry.cgi","minVersion":1,"maxVersion":2},
				"SYNO.SynologyDrive":{"path":"entry.cgi","minVersion":1,"maxVersion":1},
				"SYNO.SynologyDrive.Connection":{"path":"entry.cgi","minVersion":1,"maxVersion":2},
				"SYNO.SynologyDrive.Share":{"path":"entry.cgi","minVersion":1,"maxVersion":2},
				"SYNO.SynologyDrive.Log":{"path":"entry.cgi","minVersion":1,"maxVersion":1}
			}}`)
		case "SYNO.API.Auth.login":
			fmt.Fprint(w, `{"success":true,"data":{"sid":"drive-sid","synotoken":"drive-token"}}`)
		case "SYNO.Core.System.info":
			fmt.Fprint(w, `{"success":true,"data":{"hostname":"lab-nas","model":"DS925+","firmware_ver":"DSM 7.3-81168"}}`)
		case "SYNO.Core.Package.list":
			*inventoryCalls++
			runningStatus := "stop"
			if running {
				runningStatus = "running"
			}
			fmt.Fprintf(w, `{"success":true,"data":{"packages":[
				{"id":"SynologyDrive","name":"Synology Drive Server","version":%q,"additional":{"status":%q,"startable":true,"install_type":""}}
			]}}`, driveVersion, runningStatus)
		case "SYNO.SynologyDrive.get_status":
			// Shape captured live from Drive 4.0.3 (WI-022).
			fmt.Fprint(w, `{"success":true,"data":{"cstn_freeze":false,"enable_status":"enabled","no_folder_available":false}}`)
		case "SYNO.SynologyDrive.Connection.list":
			// A stopped Drive package rejects its own WebAPI calls; mirror that
			// so the stopped-package guidance path is exercised end to end.
			if !running {
				fmt.Fprint(w, `{"success":false,"error":{"code":4000}}`)
				return
			}
			fmt.Fprint(w, `{"success":true,"data":{"total":1,"items":[{"username":"alice","ip_address":"10.0.0.5"}]}}`)
		default:
			t.Errorf("unexpected request %s.%s", api, method)
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
}

func TestDriveAdminReadsRefreshPackageCatalogPerCommand(t *testing.T) {
	inventoryCalls := 0
	server := newDriveAdminTestServer(t, "4.0.3-27892", true, &inventoryCalls)
	defer server.Close()

	client, err := NewClient(Options{BaseURL: server.URL, Username: "automation", Password: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	status, err := client.DriveAdminStatus(context.Background())
	if err != nil {
		t.Fatalf("DriveAdminStatus() error = %v", err)
	}
	if status.Status != "enabled" {
		t.Fatalf("status = %#v", status)
	}
	if !status.Package.Installed || status.Package.Version != "4.0.3-27892" || !status.Package.Running || status.Package.ID != "SynologyDrive" {
		t.Fatalf("package evidence = %#v", status.Package)
	}
	if inventoryCalls != 1 {
		t.Fatalf("inventory calls after first read = %d", inventoryCalls)
	}

	// The catalog must be refreshed again before the next package-scoped
	// command, not reused from the session cache.
	if _, err := client.DriveAdminConnections(context.Background()); err != nil {
		t.Fatalf("DriveAdminConnections() error = %v", err)
	}
	if inventoryCalls != 2 {
		t.Fatalf("inventory calls after second read = %d", inventoryCalls)
	}
}

func TestDriveAdminCapabilitiesReportPackageEvidence(t *testing.T) {
	inventoryCalls := 0
	server := newDriveAdminTestServer(t, "4.0.3-27892", true, &inventoryCalls)
	defer server.Close()

	client, err := NewClient(Options{BaseURL: server.URL, Username: "automation", Password: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	capabilities, report, err := client.DriveAdminCapabilities(context.Background())
	if err != nil {
		t.Fatalf("DriveAdminCapabilities() error = %v", err)
	}
	if !capabilities.StatusRead || !capabilities.ConnectionsRead || !capabilities.TeamFoldersRead || !capabilities.LogRead {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	if capabilities.TeamFoldersSet {
		t.Fatal("team-folder set must fail closed in this slice")
	}
	if !capabilities.Package.Installed || capabilities.Package.Version != "4.0.3-27892" {
		t.Fatalf("package evidence = %#v", capabilities.Package)
	}
	if len(report.Packages) != 1 || report.Packages[0].ID != "SynologyDrive" || report.Packages[0].Version != "4.0.3-27892" {
		t.Fatalf("report packages = %#v", report.Packages)
	}
	foundStatus := false
	for _, selection := range report.Operations {
		if selection.Operation == "drive.admin.status.read" {
			foundStatus = true
			if !selection.Supported || !strings.Contains(selection.Reason, "package SynologyDrive 4.0.3-27892") {
				t.Fatalf("status selection = %#v", selection)
			}
		}
	}
	if !foundStatus {
		t.Fatal("report lacks drive.admin.status.read selection")
	}
}

func TestDriveAdminUnsupportedBelowBaselineVersion(t *testing.T) {
	inventoryCalls := 0
	server := newDriveAdminTestServer(t, "2.0.4-11112", true, &inventoryCalls)
	defer server.Close()

	client, err := NewClient(Options{BaseURL: server.URL, Username: "automation", Password: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	capabilities, _, err := client.DriveAdminCapabilities(context.Background())
	if err != nil {
		t.Fatalf("DriveAdminCapabilities() error = %v", err)
	}
	if capabilities.StatusRead || capabilities.ConnectionsRead || capabilities.TeamFoldersRead || capabilities.LogRead {
		t.Fatalf("Drive 2.x should be unsupported: %#v", capabilities)
	}
	if !capabilities.Package.Installed || capabilities.Package.Version != "2.0.4-11112" {
		t.Fatalf("package evidence = %#v", capabilities.Package)
	}
	if _, err := client.DriveAdminStatus(context.Background()); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("DriveAdminStatus() error = %v", err)
	}
}

func TestDriveAdminStoppedPackageGuidance(t *testing.T) {
	inventoryCalls := 0
	server := newDriveAdminTestServer(t, "4.0.3-27892", false, &inventoryCalls)
	defer server.Close()

	client, err := NewClient(Options{BaseURL: server.URL, Username: "automation", Password: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	// The fake still answers get_status (some Drive versions answer while the
	// package is stopping); its evidence must report the stopped package.
	status, err := client.DriveAdminStatus(context.Background())
	if err != nil {
		t.Fatalf("DriveAdminStatus() error = %v", err)
	}
	if status.Package.Running {
		t.Fatalf("package evidence should report stopped: %#v", status.Package)
	}
	// A Drive API failure while the package is stopped is explained with
	// actionable guidance instead of a bare DSM error code.
	_, err = client.DriveAdminConnections(context.Background())
	if err == nil || !strings.Contains(err.Error(), "installed but not running") {
		t.Fatalf("DriveAdminConnections() error = %v", err)
	}
}
