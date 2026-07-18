package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/packagecenter"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type fakePackageClient struct {
	settings  synology.PackageSettings
	packages  []packagecenter.Package
	caps      synology.PackageCapabilities
	mutations int
}

func (client *fakePackageClient) PackageState(context.Context) (synology.PackageState, error) {
	return synology.PackageState{Packages: client.packages}, nil
}

func (client *fakePackageClient) PackageSettings(context.Context) (synology.PackageSettings, error) {
	return client.settings, nil
}

func (client *fakePackageClient) PackageCatalog(context.Context) (synology.PackageCatalog, error) {
	return synology.PackageCatalog{}, nil
}

func (client *fakePackageClient) PackageInstall(_ context.Context, input synology.PackageInstallInput) (synology.PackageInstallResult, error) {
	return synology.PackageInstallResult{PackageID: input.Name, Installed: true}, nil
}

func (client *fakePackageClient) PackageCapabilities(context.Context) (synology.PackageCapabilities, synology.CompatibilityReport, error) {
	return client.caps, synology.CompatibilityReport{}, nil
}

func (client *fakePackageClient) ApplyPackageSettingsChange(_ context.Context, desired synology.PackageSettings) (synology.PackageMutationResult, error) {
	client.mutations++
	client.settings = desired
	return synology.PackageMutationResult{Action: packagecenter.KindSettings, Backend: "fake", API: "fake", Version: 1, Method: "set"}, nil
}

func (client *fakePackageClient) ApplyPackageLifecycleChange(_ context.Context, change synology.PackageLifecycleChange) (synology.PackageMutationResult, error) {
	client.mutations++
	for index := range client.packages {
		if client.packages[index].ID != change.PackageID {
			continue
		}
		switch change.Action {
		case packagecenter.ActionStart:
			client.packages[index].Status = packagecenter.StatusRunning
			client.packages[index].Running = true
		case packagecenter.ActionStop:
			client.packages[index].Status = packagecenter.StatusStopped
			client.packages[index].Running = false
		case packagecenter.ActionUninstall:
			client.packages = append(client.packages[:index], client.packages[index+1:]...)
		}
		break
	}
	return synology.PackageMutationResult{Action: change.Action, PackageID: change.PackageID, Backend: "fake", API: "fake", Version: 1, Method: change.Action}, nil
}

func testPackageCaps() synology.PackageCapabilities {
	return synology.PackageCapabilities{
		Module: packagecenter.ModuleName, InventoryRead: true, SettingsRead: true,
		SettingsSet: true, Start: true, Stop: true, Uninstall: true,
	}
}

func testPackageClient() *fakePackageClient {
	return &fakePackageClient{
		settings: synology.PackageSettings{TrustLevel: packagecenter.TrustSynology, AutoUpdateEnabled: true},
		caps:     testPackageCaps(),
		packages: []packagecenter.Package{
			{ID: "SynologyDrive", Name: "Synology Drive Server", Version: "3.5.1", Status: packagecenter.StatusRunning, Running: true, CanStart: false, CanStop: true, CanUninstall: true},
			{ID: "WebStation", Name: "Web Station", Status: packagecenter.StatusStopped, Running: false, CanStart: true, CanStop: false, CanUninstall: true},
			{ID: "ActiveInsight", Name: "Active Insight", Status: packagecenter.StatusRunning, Running: true, CanStart: false, CanStop: true, CanUninstall: false},
		},
	}
}

func boolPtr(value bool) *bool { return &value }

func TestPackageSettingsPlanApplyAndStale(t *testing.T) {
	client := testPackageClient()
	request := packagecenter.ChangeRequest{
		Kind:     packagecenter.KindSettings,
		Settings: &packagecenter.SettingsChange{AutoUpdateEnabled: boolPtr(false)},
	}
	plan, err := planPackageChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("planPackageChangeWithClient() error = %v", err)
	}
	if plan.Hash == "" || plan.ObservedFingerprint == "" || plan.Observed.Settings == nil || plan.Risk != "medium" {
		t.Fatalf("plan = %#v", plan)
	}
	if err := validatePackagePlan(plan, plan.Hash); err != nil {
		t.Fatalf("validatePackagePlan() error = %v", err)
	}

	// A profile that differs in an unrelated field must invalidate the plan.
	stale := testPackageClient()
	stale.settings.AutoUpdateImportantOnly = true
	if _, err := applyPackagePlanWithClient(context.Background(), stale, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale apply error = %v", err)
	}
	if stale.mutations != 0 {
		t.Fatal("stale plan reached mutation")
	}

	result, err := applyPackagePlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("applyPackagePlanWithClient() error = %v", err)
	}
	if !result.Applied || client.settings.AutoUpdateEnabled || client.mutations != 1 {
		t.Fatalf("apply result/client = %#v %#v", result, client.settings)
	}
}

func TestPackageLifecycleStopPlanApply(t *testing.T) {
	client := testPackageClient()
	request := packagecenter.ChangeRequest{
		Kind:      packagecenter.KindLifecycle,
		Lifecycle: &packagecenter.LifecycleChange{Action: packagecenter.ActionStop, PackageID: "SynologyDrive"},
	}
	plan, err := planPackageChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("planPackageChangeWithClient() error = %v", err)
	}
	if plan.Observed.Package == nil || plan.Risk != "high" || len(plan.Summary) == 0 {
		t.Fatalf("plan = %#v", plan)
	}
	result, err := applyPackagePlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("applyPackagePlanWithClient() error = %v", err)
	}
	pkg, _ := findPackage(synology.PackageState{Packages: client.packages}, "SynologyDrive")
	if !result.Applied || pkg.Status != packagecenter.StatusStopped || client.mutations != 1 {
		t.Fatalf("apply result/package = %#v %#v", result, pkg)
	}
}

func TestPackageUninstallPlanApplyRemovesPackage(t *testing.T) {
	client := testPackageClient()
	request := packagecenter.ChangeRequest{
		Kind:      packagecenter.KindLifecycle,
		Lifecycle: &packagecenter.LifecycleChange{Action: packagecenter.ActionUninstall, PackageID: "WebStation"},
	}
	plan, err := planPackageChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("planPackageChangeWithClient() error = %v", err)
	}
	if !plan.Destructive || plan.Risk != "high" {
		t.Fatalf("uninstall plan = %#v", plan)
	}
	if _, err := applyPackagePlanWithClient(context.Background(), client, plan); err != nil {
		t.Fatalf("applyPackagePlanWithClient() error = %v", err)
	}
	if _, ok := findPackage(synology.PackageState{Packages: client.packages}, "WebStation"); ok {
		t.Fatal("package still present after uninstall")
	}
}

func TestPackagePlanRejectsUnsafeIntents(t *testing.T) {
	client := testPackageClient()

	// uninstall of a package DSM marks non-removable
	if _, err := planPackageChangeWithClient(context.Background(), "lab", client, packagecenter.ChangeRequest{
		Kind: packagecenter.KindLifecycle, Lifecycle: &packagecenter.LifecycleChange{Action: packagecenter.ActionUninstall, PackageID: "ActiveInsight"},
	}); err == nil || !strings.Contains(err.Error(), "cannot be uninstalled") {
		t.Fatalf("non-removable uninstall error = %v", err)
	}

	// start of an already-running package
	if _, err := planPackageChangeWithClient(context.Background(), "lab", client, packagecenter.ChangeRequest{
		Kind: packagecenter.KindLifecycle, Lifecycle: &packagecenter.LifecycleChange{Action: packagecenter.ActionStart, PackageID: "SynologyDrive"},
	}); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("already-running start error = %v", err)
	}

	// no-op settings patch
	if _, err := planPackageChangeWithClient(context.Background(), "lab", client, packagecenter.ChangeRequest{
		Kind: packagecenter.KindSettings, Settings: &packagecenter.SettingsChange{AutoUpdateEnabled: boolPtr(true)},
	}); err == nil || !strings.Contains(err.Error(), "would not change") {
		t.Fatalf("no-op settings error = %v", err)
	}

	// uninstall when the backend is unsupported
	noUninstall := testPackageClient()
	noUninstall.caps.Uninstall = false
	if _, err := planPackageChangeWithClient(context.Background(), "lab", noUninstall, packagecenter.ChangeRequest{
		Kind: packagecenter.KindLifecycle, Lifecycle: &packagecenter.LifecycleChange{Action: packagecenter.ActionUninstall, PackageID: "WebStation"},
	}); err == nil || !strings.Contains(err.Error(), "uninstall backend") {
		t.Fatalf("unsupported uninstall error = %v", err)
	}
}

func TestPackageRequestShapeRejectsDeferredAndMalformed(t *testing.T) {
	cases := []struct {
		name    string
		request packagecenter.ChangeRequest
		wantErr string
	}{
		{"install deferred", packagecenter.ChangeRequest{Kind: packagecenter.KindLifecycle, Lifecycle: &packagecenter.LifecycleChange{Action: packagecenter.ActionInstall, PackageID: "x"}}, "deferred"},
		{"update deferred", packagecenter.ChangeRequest{Kind: packagecenter.KindLifecycle, Lifecycle: &packagecenter.LifecycleChange{Action: packagecenter.ActionUpdate, PackageID: "x"}}, "deferred"},
		{"empty settings", packagecenter.ChangeRequest{Kind: packagecenter.KindSettings, Settings: &packagecenter.SettingsChange{}}, "no fields"},
		{"mixed payload", packagecenter.ChangeRequest{Kind: packagecenter.KindSettings, Settings: &packagecenter.SettingsChange{AutoUpdateEnabled: boolPtr(true)}, Lifecycle: &packagecenter.LifecycleChange{}}, "only the settings"},
		{"unknown kind", packagecenter.ChangeRequest{Kind: "other"}, "unsupported change kind"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validatePackageRequestShape(testCase.request); err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("validatePackageRequestShape() error = %v, want %q", err, testCase.wantErr)
			}
		})
	}
}

func TestPackagePlanHashRejectsTampering(t *testing.T) {
	client := testPackageClient()
	plan, err := planPackageChangeWithClient(context.Background(), "lab", client, packagecenter.ChangeRequest{
		Kind: packagecenter.KindSettings, Settings: &packagecenter.SettingsChange{AutoUpdateEnabled: boolPtr(false)},
	})
	if err != nil {
		t.Fatalf("planPackageChangeWithClient() error = %v", err)
	}
	plan.Risk = "low"
	if err := validatePackagePlan(plan, plan.Hash); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("tampered plan error = %v", err)
	}
}
