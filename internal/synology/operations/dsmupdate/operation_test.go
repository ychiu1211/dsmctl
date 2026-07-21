package dsmupdate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type executorFunc func(context.Context, compatibility.Request) (json.RawMessage, error)

func (function executorFunc) Execute(ctx context.Context, request compatibility.Request) (json.RawMessage, error) {
	return function(ctx, request)
}

// fullTarget advertises every API this module reads, at the versions observed
// live on DSM 7.3.
func fullTarget() compatibility.Target {
	target := compatibility.NewTarget()
	for name, versions := range map[string][2]int{
		UpgradeAPI:       {1, 2},
		UpgradeServerAPI: {1, 4},
		UpgradeSetAPI:    {1, 4},
		AutoBackupAPI:    {1, 2},
	} {
		target.SetAPI(name, compatibility.APIInfo{Path: "entry.cgi", MinVersion: versions[0], MaxVersion: versions[1], RequestFormat: "JSON"})
	}
	return target
}

func fixture(t *testing.T, name string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// fixtureExecutor serves canned responses keyed by "API:method".
func fixtureExecutor(t *testing.T, files map[string]string) executorFunc {
	t.Helper()
	return func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		key := request.API + ":" + request.Method
		name, ok := files[key]
		if !ok {
			t.Fatalf("unexpected call %s v%d", key, request.Version)
		}
		return fixture(t, name), nil
	}
}

func TestDecodeLocalStatus(t *testing.T) {
	status, err := decodeLocalStatus(fixture(t, "status.json"))
	if err != nil {
		t.Fatalf("decodeLocalStatus: %v", err)
	}
	if !status.AllowUpgrade {
		t.Errorf("AllowUpgrade = false, want true")
	}
	if status.State != "none" {
		t.Errorf("State = %q, want none", status.State)
	}
}

func TestDecodeLocalStatusRejectsMalformed(t *testing.T) {
	for name, data := range map[string]string{
		"empty":          ``,
		"array":          `[]`,
		"missing allow":  `{"status":"none"}`,
		"missing status": `{"allow_upgrade":true}`,
	} {
		if _, err := decodeLocalStatus(json.RawMessage(data)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestDecodeAvailableFlat(t *testing.T) {
	got, err := decodeAvailable(fixture(t, "available_v1.json"))
	if err != nil {
		t.Fatalf("decodeAvailable v1: %v", err)
	}
	if !got.Checked || got.Available {
		t.Errorf("got %+v, want checked=true available=false", got)
	}
}

func TestDecodeAvailableWrapped(t *testing.T) {
	got, err := decodeAvailable(fixture(t, "available_v3.json"))
	if err != nil {
		t.Fatalf("decodeAvailable v3: %v", err)
	}
	if !got.Checked || got.Available {
		t.Errorf("got %+v, want checked=true available=false", got)
	}
	if got.RSSResult != "success" {
		t.Errorf("RSSResult = %q, want success", got.RSSResult)
	}
}

func TestDecodeAvailablePresentSurfacesDetails(t *testing.T) {
	got, err := decodeAvailable(fixture(t, "available_present.json"))
	if err != nil {
		t.Fatalf("decodeAvailable present: %v", err)
	}
	if !got.Checked || !got.Available {
		t.Fatalf("got %+v, want checked=true available=true", got)
	}
	// The offered-version and restart/criticality fields are surfaced verbatim
	// under Details by their raw DSM key (no guessed typed decoder).
	for key, want := range map[string]string{
		"version": "7.3.2-90000",
		"reboot":  "true",
		"type":    "important",
		"nano":    "false",
	} {
		if got.Details[key] != want {
			t.Errorf("Details[%q] = %q, want %q", key, got.Details[key], want)
		}
	}
	if _, ok := got.Details["available"]; ok {
		t.Errorf("Details must not duplicate the available field")
	}
}

func TestDecodeAvailableRejectsMalformed(t *testing.T) {
	for name, data := range map[string]string{
		"empty":             ``,
		"array":             `[]`,
		"missing available": `{"update":{"rss_result":"success"}}`,
		"update not object": `{"update":true}`,
	} {
		if _, err := decodeAvailable(json.RawMessage(data)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestDecodePolicyV2(t *testing.T) {
	policy, err := decodePolicyV2(fixture(t, "policy_v2.json"))
	if err != nil {
		t.Fatalf("decodePolicyV2: %v", err)
	}
	if policy.AutoUpdateEnabled == nil || !*policy.AutoUpdateEnabled {
		t.Errorf("AutoUpdateEnabled = %v, want true", policy.AutoUpdateEnabled)
	}
	if policy.AutoUpdateType != "hotfix-security" {
		t.Errorf("AutoUpdateType = %q", policy.AutoUpdateType)
	}
	if policy.UpgradeType != "hotfix" {
		t.Errorf("UpgradeType = %q", policy.UpgradeType)
	}
	if policy.SmartNanoEnabled == nil || !*policy.SmartNanoEnabled {
		t.Errorf("SmartNanoEnabled = %v, want true", policy.SmartNanoEnabled)
	}
	if policy.Schedule == nil || policy.Schedule.Hour != 5 || policy.Schedule.Minute != 5 || policy.Schedule.WeekDay != "0" {
		t.Errorf("Schedule = %+v, want hour=5 minute=5 week_day=0", policy.Schedule)
	}
	// V2 does not report the older download-only field.
	if policy.AutoDownload != nil {
		t.Errorf("AutoDownload = %v, want nil for v2", policy.AutoDownload)
	}
}

func TestDecodePolicyV1(t *testing.T) {
	policy, err := decodePolicyV1(fixture(t, "policy_v1.json"))
	if err != nil {
		t.Fatalf("decodePolicyV1: %v", err)
	}
	if policy.AutoDownload == nil || !*policy.AutoDownload {
		t.Errorf("AutoDownload = %v, want true", policy.AutoDownload)
	}
	if policy.UpgradeType != "hotfix" {
		t.Errorf("UpgradeType = %q", policy.UpgradeType)
	}
	// V1 does not report the newer enable/type fields; they must stay unset.
	if policy.AutoUpdateEnabled != nil {
		t.Errorf("AutoUpdateEnabled = %v, want nil for v1", policy.AutoUpdateEnabled)
	}
}

func TestDecodePolicyRejectsMalformed(t *testing.T) {
	if _, err := decodePolicyV2(json.RawMessage(`{"upgrade_type":"hotfix"}`)); err == nil {
		t.Error("v2 without autoupdate_enable: expected error")
	}
	if _, err := decodePolicyV1(json.RawMessage(`{"upgrade_type":"hotfix"}`)); err == nil {
		t.Error("v1 without auto_download: expected error")
	}
}

func TestDecodeConfigSettingsNeverLeaksPassword(t *testing.T) {
	settings, err := decodeConfigSettings(fixture(t, "config_get.json"))
	if err != nil {
		t.Fatalf("decodeConfigSettings: %v", err)
	}
	if settings.Enabled {
		t.Errorf("Enabled = true, want false")
	}
	if settings.Account != "testuser@example.com" {
		t.Errorf("Account = %q", settings.Account)
	}
	if settings.EncryptionMethod != "manual" {
		t.Errorf("EncryptionMethod = %q", settings.EncryptionMethod)
	}
	// The destination password must never be decoded or surfaced anywhere.
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if strings.Contains(string(encoded), "s3cr3t") || strings.Contains(strings.ToLower(string(encoded)), "pwd") {
		t.Errorf("decoded config settings leaked password material: %s", encoded)
	}
}

func TestDecodeConfigVersions(t *testing.T) {
	versions, err := decodeConfigVersions(fixture(t, "config_list.json"))
	if err != nil {
		t.Fatalf("decodeConfigVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("len = %d, want 1", len(versions))
	}
	entry := versions[0]
	if entry.DSMVersion != "7.3.2-86009" || entry.Host != "test-nas" || entry.Model != "DS1621+" || entry.Serial != "0000TEST0000" {
		t.Errorf("entry = %+v", entry)
	}
}

func TestReadFunctionsFullTarget(t *testing.T) {
	target := fullTarget()
	executor := fixtureExecutor(t, map[string]string{
		UpgradeAPI + ":status":      "status.json",
		UpgradeServerAPI + ":check": "available_v3.json",
		UpgradeSetAPI + ":get":      "policy_v2.json",
		AutoBackupAPI + ":get":      "config_get.json",
		AutoBackupAPI + ":list":     "config_list.json",
	})
	ctx := context.Background()

	status, _, err := ReadStatus(ctx, target, executor, "DSM 7.3.2-86009")
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if status.InstalledVersion != "DSM 7.3.2-86009" || status.State != "none" {
		t.Errorf("status = %+v", status)
	}

	available, _, err := ReadAvailable(ctx, target, executor)
	if err != nil {
		t.Fatalf("ReadAvailable: %v", err)
	}
	if !available.Checked || available.RSSResult != "success" {
		t.Errorf("available = %+v", available)
	}

	policy, _, err := ReadPolicy(ctx, target, executor)
	if err != nil {
		t.Fatalf("ReadPolicy: %v", err)
	}
	if policy.AutoUpdateType != "hotfix-security" {
		t.Errorf("policy = %+v", policy)
	}

	backup, _, err := ReadConfigBackup(ctx, target, executor)
	if err != nil {
		t.Fatalf("ReadConfigBackup: %v", err)
	}
	if backup.Account != "testuser@example.com" || len(backup.Versions) != 1 {
		t.Errorf("backup = %+v", backup)
	}
}

// TestIndependentBoundaries confirms a NAS missing the config-backup API still
// exposes the update-family reads: the areas select their own backends.
func TestIndependentBoundaries(t *testing.T) {
	target := compatibility.NewTarget()
	// Update family only; no SYNO.Backup.Config.AutoBackup.
	target.SetAPI(UpgradeAPI, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(UpgradeServerAPI, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 4})
	target.SetAPI(UpgradeSetAPI, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 4})

	for name, selectArea := range map[string]func(compatibility.Target) (compatibility.Selection, error){
		"status":    SelectStatus,
		"available": SelectAvailable,
		"policy":    SelectPolicy,
	} {
		selection, err := selectArea(target)
		if err != nil {
			t.Errorf("%s select error: %v", name, err)
		}
		if !selection.Supported {
			t.Errorf("%s should be supported", name)
		}
	}
	selection, err := SelectConfigBackup(target)
	if err == nil || !compatibility.IsUnsupported(err) {
		t.Errorf("config backup select err = %v, want unsupported", err)
	}
	if selection.Supported {
		t.Errorf("config backup should be unsupported without its API")
	}
}

// TestConfigBackupHistoryFailureIsNonFatal confirms the config-backup settings
// are still returned when the history "list" method fails (history is a
// secondary enrichment).
func TestConfigBackupHistoryFailureIsNonFatal(t *testing.T) {
	target := fullTarget()
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		switch request.Method {
		case "get":
			return fixture(t, "config_get.json"), nil
		case "list":
			return nil, errors.New("list unavailable on this NAS")
		default:
			t.Fatalf("unexpected call %s.%s", request.API, request.Method)
			return nil, nil
		}
	})
	backup, _, err := ReadConfigBackup(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ReadConfigBackup with failing list: %v", err)
	}
	if backup.Account != "testuser@example.com" {
		t.Errorf("settings not returned: %+v", backup)
	}
	if len(backup.Versions) != 0 {
		t.Errorf("Versions = %v, want empty when list fails", backup.Versions)
	}
}
