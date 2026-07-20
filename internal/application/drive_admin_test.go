package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/driveadmin"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

// fakeDriveAdminClient implements driveAdminClient for plan/apply tests. A
// mutation is staged as pending and takes effect on a later team-folder read,
// so postcondition polling and DSM's silent-skip behavior are testable.
type fakeDriveAdminClient struct {
	caps      synology.DriveAdminCapabilities
	folders   []driveadmin.TeamFolder
	mutations int
	// silentSkip mimics the Share.set handler ignoring an ineligible share:
	// the call succeeds but nothing changes.
	silentSkip bool
	// convergeAfterReads delays the pending mutation by that many reads.
	convergeAfterReads int
	pending            *driveadmin.TeamFolderChange
}

func (c *fakeDriveAdminClient) DriveAdminStatus(context.Context) (synology.DriveAdminStatus, error) {
	return synology.DriveAdminStatus{}, nil
}

func (c *fakeDriveAdminClient) DriveAdminConnections(context.Context) (synology.DriveAdminConnections, error) {
	return synology.DriveAdminConnections{}, nil
}

func (c *fakeDriveAdminClient) DriveAdminLog(context.Context, synology.DriveAdminLogQuery) (synology.DriveAdminLog, error) {
	return synology.DriveAdminLog{}, nil
}

func (c *fakeDriveAdminClient) DriveAdminCapabilities(context.Context) (synology.DriveAdminCapabilities, synology.CompatibilityReport, error) {
	return c.caps, synology.CompatibilityReport{}, nil
}

func (c *fakeDriveAdminClient) DriveServerConfig(context.Context) (synology.DriveServerConfig, error) {
	return synology.DriveServerConfig{}, nil
}

func (c *fakeDriveAdminClient) DriveConnectionSummary(context.Context) (synology.DriveConnectionSummary, error) {
	return synology.DriveConnectionSummary{}, nil
}

func (c *fakeDriveAdminClient) DriveDBUsage(context.Context) (synology.DriveDBUsage, error) {
	return synology.DriveDBUsage{}, nil
}

func (c *fakeDriveAdminClient) DriveTopAccessFiles(context.Context, synology.DriveTopAccessQuery) (synology.DriveTopAccessFiles, error) {
	return synology.DriveTopAccessFiles{}, nil
}

func (c *fakeDriveAdminClient) DriveActivation(context.Context) (synology.DriveActivation, error) {
	return synology.DriveActivation{}, nil
}

func (c *fakeDriveAdminClient) ApplyDriveServerConfigChange(context.Context, driveadmin.ServerConfigChange) (synology.DriveConfigMutationResult, error) {
	return synology.DriveConfigMutationResult{}, nil
}

func (c *fakeDriveAdminClient) DriveAdminTeamFolders(context.Context) (synology.DriveAdminTeamFolders, error) {
	if c.pending != nil {
		if c.convergeAfterReads <= 0 {
			c.applyPending()
		} else {
			c.convergeAfterReads--
		}
	}
	return synology.DriveAdminTeamFolders{
		Total:       len(c.folders),
		TeamFolders: append([]driveadmin.TeamFolder(nil), c.folders...),
	}, nil
}

func (c *fakeDriveAdminClient) ApplyDriveTeamFolderChange(_ context.Context, change driveadmin.TeamFolderChange) (synology.DriveTeamFolderMutationResult, error) {
	c.mutations++
	if !c.silentSkip {
		staged := change
		c.pending = &staged
	}
	return synology.DriveTeamFolderMutationResult{Backend: "fake", API: "fake", Version: 1, Method: "set"}, nil
}

func (c *fakeDriveAdminClient) applyPending() {
	change := *c.pending
	c.pending = nil
	for index := range c.folders {
		if c.folders[index].Name != change.Name {
			continue
		}
		folder := &c.folders[index]
		count, policy, days := driveTeamFolderDesiredVersioning(change, *folder)
		switch change.Action {
		case driveadmin.TeamFolderActionEnable, driveadmin.TeamFolderActionSetVersioning:
			folder.Enabled = true
			folder.MaxVersions = &count
			folder.VersionPolicy = policy
			folder.RetentionDays = &days
		case driveadmin.TeamFolderActionDisable:
			folder.Enabled = false
			folder.MaxVersions = nil
			folder.VersionPolicy = ""
			folder.RetentionDays = nil
		}
		return
	}
}

func driveTeamFolderTestClient() *fakeDriveAdminClient {
	eight, zero := 8, 0
	return &fakeDriveAdminClient{
		caps: synology.DriveAdminCapabilities{TeamFoldersRead: true, TeamFoldersSet: true},
		folders: []driveadmin.TeamFolder{
			{Name: "projects", Enabled: false, Status: "normal", Type: "normal"},
			{Name: "team-data", Enabled: true, Status: "normal", Type: "normal", MaxVersions: &eight, VersionPolicy: "fifo", RetentionDays: &zero},
		},
	}
}

func intPtr(value int) *int { return &value }

func withoutTeamFolderVerifyDelay(t *testing.T) {
	t.Helper()
	previous := driveTeamFolderVerifyDelay
	driveTeamFolderVerifyDelay = 0
	t.Cleanup(func() { driveTeamFolderVerifyDelay = previous })
}

func TestDriveTeamFolderEnablePlanApply(t *testing.T) {
	withoutTeamFolderVerifyDelay(t)
	client := driveTeamFolderTestClient()
	request := driveadmin.TeamFolderChange{
		Action: driveadmin.TeamFolderActionEnable, Name: "projects",
		MaxVersions: intPtr(8), VersionPolicy: "smart", RetentionDays: intPtr(30),
	}
	if err := validateDriveTeamFolderChange(request); err != nil {
		t.Fatalf("validateDriveTeamFolderChange() error = %v", err)
	}
	plan, err := planDriveTeamFolderChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("planDriveTeamFolderChangeWithClient() error = %v", err)
	}
	if plan.Risk != "medium" || plan.Destructive || plan.Hash == "" || plan.Observed.Name != "projects" {
		t.Fatalf("plan = %#v", plan)
	}
	if err := validateDriveTeamFolderPlan(plan, plan.Hash); err != nil {
		t.Fatalf("validateDriveTeamFolderPlan() error = %v", err)
	}
	result, err := applyDriveTeamFolderPlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("applyDriveTeamFolderPlanWithClient() error = %v", err)
	}
	if !result.Applied || client.mutations != 1 {
		t.Fatalf("result = %#v mutations = %d", result, client.mutations)
	}
	if !result.TeamFolder.Enabled || result.TeamFolder.MaxVersions == nil || *result.TeamFolder.MaxVersions != 8 ||
		result.TeamFolder.VersionPolicy != "smart" || result.TeamFolder.RetentionDays == nil || *result.TeamFolder.RetentionDays != 30 {
		t.Fatalf("verified folder = %#v", result.TeamFolder)
	}
}

func TestDriveTeamFolderApplyPollsUntilConverged(t *testing.T) {
	withoutTeamFolderVerifyDelay(t)
	client := driveTeamFolderTestClient()
	request := driveadmin.TeamFolderChange{
		Action: driveadmin.TeamFolderActionEnable, Name: "projects", MaxVersions: intPtr(0),
	}
	plan, err := planDriveTeamFolderChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	client.convergeAfterReads = 2
	result, err := applyDriveTeamFolderPlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("apply should keep polling until Drive converges: %v", err)
	}
	// Versioning off: Drive forces fifo/0 and the list reports policy "-".
	if !result.TeamFolder.Enabled || *result.TeamFolder.MaxVersions != 0 || result.TeamFolder.VersionPolicy != "" {
		t.Fatalf("verified folder = %#v", result.TeamFolder)
	}
}

func TestDriveTeamFolderApplySurfacesSilentSkip(t *testing.T) {
	withoutTeamFolderVerifyDelay(t)
	client := driveTeamFolderTestClient()
	request := driveadmin.TeamFolderChange{
		Action: driveadmin.TeamFolderActionEnable, Name: "projects", MaxVersions: intPtr(0),
	}
	plan, err := planDriveTeamFolderChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	client.silentSkip = true
	if _, err := applyDriveTeamFolderPlanWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "did not confirm") {
		t.Fatalf("silent skip must surface as not-yet-confirmed, got %v", err)
	}
}

func TestDriveTeamFolderDisablePlanIsDestructive(t *testing.T) {
	withoutTeamFolderVerifyDelay(t)
	client := driveTeamFolderTestClient()
	request := driveadmin.TeamFolderChange{Action: driveadmin.TeamFolderActionDisable, Name: "team-data"}
	plan, err := planDriveTeamFolderChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "high" || !plan.Destructive {
		t.Fatalf("disable must be high risk and destructive: %#v", plan)
	}
	found := false
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "version") {
			found = true
		}
	}
	if !found {
		t.Fatalf("disable warnings must mention stored versions: %#v", plan.Warnings)
	}
	result, err := applyDriveTeamFolderPlanWithClient(context.Background(), client, plan)
	if err != nil || result.TeamFolder.Enabled {
		t.Fatalf("apply result = %#v err = %v", result, err)
	}
}

func TestDriveTeamFolderSetVersioningMergesAndClassifies(t *testing.T) {
	withoutTeamFolderVerifyDelay(t)
	client := driveTeamFolderTestClient()

	// Reducing kept versions prunes stored versions: high risk.
	request := driveadmin.TeamFolderChange{
		Action: driveadmin.TeamFolderActionSetVersioning, Name: "team-data", MaxVersions: intPtr(4),
	}
	plan, err := planDriveTeamFolderChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "high" || !plan.Destructive {
		t.Fatalf("version reduction must be high risk: %#v", plan)
	}
	result, err := applyDriveTeamFolderPlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	// Policy fifo merged from the observed entry, retention untouched.
	if *result.TeamFolder.MaxVersions != 4 || result.TeamFolder.VersionPolicy != "fifo" || *result.TeamFolder.RetentionDays != 0 {
		t.Fatalf("merged folder = %#v", result.TeamFolder)
	}

	// Raising the count is a plain medium-risk change.
	raise := driveadmin.TeamFolderChange{
		Action: driveadmin.TeamFolderActionSetVersioning, Name: "team-data", MaxVersions: intPtr(16),
	}
	raisePlan, err := planDriveTeamFolderChangeWithClient(context.Background(), "lab", client, raise)
	if err != nil {
		t.Fatalf("raise plan error = %v", err)
	}
	if raisePlan.Risk != "medium" || raisePlan.Destructive {
		t.Fatalf("raising versions should be medium risk: %#v", raisePlan)
	}

	// A change that matches the current state is rejected during planning.
	noop := driveadmin.TeamFolderChange{
		Action: driveadmin.TeamFolderActionSetVersioning, Name: "team-data", MaxVersions: intPtr(4),
	}
	if _, err := planDriveTeamFolderChangeWithClient(context.Background(), "lab", client, noop); err == nil || !strings.Contains(err.Error(), "would not change") {
		t.Fatalf("no-op error = %v", err)
	}
}

func TestDriveTeamFolderApplyRejectsStalePlan(t *testing.T) {
	withoutTeamFolderVerifyDelay(t)
	client := driveTeamFolderTestClient()
	request := driveadmin.TeamFolderChange{
		Action: driveadmin.TeamFolderActionEnable, Name: "projects", MaxVersions: intPtr(0),
	}
	plan, err := planDriveTeamFolderChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}

	// The same folder observed with a different status invalidates the plan.
	stale := driveTeamFolderTestClient()
	stale.folders[0].Status = "not_available"
	if _, err := applyDriveTeamFolderPlanWithClient(context.Background(), stale, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale apply error = %v", err)
	}
	if stale.mutations != 0 {
		t.Fatal("stale plan reached mutation")
	}

	// A state change that breaks the action precondition surfaces as such.
	enabled := driveTeamFolderTestClient()
	enabled.folders[0].Enabled = true
	if _, err := applyDriveTeamFolderPlanWithClient(context.Background(), enabled, plan); err == nil || !strings.Contains(err.Error(), "precondition") {
		t.Fatalf("precondition apply error = %v", err)
	}
}

func TestValidateDriveTeamFolderChangeRejectsUnsafeIntents(t *testing.T) {
	cases := []struct {
		name   string
		change driveadmin.TeamFolderChange
		want   string
	}{
		{"missing name", driveadmin.TeamFolderChange{Action: driveadmin.TeamFolderActionDisable}, "requires the shared-folder name"},
		{"home entry", driveadmin.TeamFolderChange{Action: driveadmin.TeamFolderActionEnable, Name: "homes/mydrive_home", MaxVersions: intPtr(8), VersionPolicy: "fifo"}, "home service"},
		{"surveillance", driveadmin.TeamFolderChange{Action: driveadmin.TeamFolderActionSetVersioning, Name: "surveillance", MaxVersions: intPtr(8)}, "surveillance"},
		{"unknown action", driveadmin.TeamFolderChange{Action: "toggle", Name: "projects"}, "action must be"},
		{"enable without versions", driveadmin.TeamFolderChange{Action: driveadmin.TeamFolderActionEnable, Name: "projects"}, "requires max_versions"},
		{"enable without policy", driveadmin.TeamFolderChange{Action: driveadmin.TeamFolderActionEnable, Name: "projects", MaxVersions: intPtr(8)}, "version_policy"},
		{"enable off with policy", driveadmin.TeamFolderChange{Action: driveadmin.TeamFolderActionEnable, Name: "projects", MaxVersions: intPtr(0), VersionPolicy: "fifo"}, "do not apply"},
		{"count out of range", driveadmin.TeamFolderChange{Action: driveadmin.TeamFolderActionEnable, Name: "projects", MaxVersions: intPtr(64), VersionPolicy: "fifo"}, "0..32"},
		{"days out of range", driveadmin.TeamFolderChange{Action: driveadmin.TeamFolderActionSetVersioning, Name: "projects", RetentionDays: intPtr(365)}, "0..120"},
		{"bad policy", driveadmin.TeamFolderChange{Action: driveadmin.TeamFolderActionSetVersioning, Name: "projects", VersionPolicy: "lifo"}, "fifo or smart"},
		{"disable with fields", driveadmin.TeamFolderChange{Action: driveadmin.TeamFolderActionDisable, Name: "projects", MaxVersions: intPtr(8)}, "no versioning fields"},
		{"empty set_versioning", driveadmin.TeamFolderChange{Action: driveadmin.TeamFolderActionSetVersioning, Name: "projects"}, "at least one"},
	}
	for _, test := range cases {
		if err := validateDriveTeamFolderChange(test.change); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: error = %v, want %q", test.name, err, test.want)
		}
	}
}

func TestValidateDriveAdminLogQueryDefaultsAndBounds(t *testing.T) {
	query := driveadmin.LogQuery{}
	if err := validateDriveAdminLogQuery(&query); err != nil {
		t.Fatalf("validateDriveAdminLogQuery(zero) error = %v", err)
	}
	if query.Limit != driveAdminDefaultLogLimit {
		t.Fatalf("default limit = %d", query.Limit)
	}

	valid := driveadmin.LogQuery{Limit: 25, From: 1700000000, To: 1700003600}
	if err := validateDriveAdminLogQuery(&valid); err != nil || valid.Limit != 25 {
		t.Fatalf("valid query error=%v limit=%d", err, valid.Limit)
	}

	cases := []struct {
		name  string
		query driveadmin.LogQuery
		want  string
	}{
		{"negative limit", driveadmin.LogQuery{Limit: -1}, "cannot be negative"},
		{"excessive limit", driveadmin.LogQuery{Limit: driveAdminMaxLogLimit + 1}, "exceeds the maximum"},
		{"negative bound", driveadmin.LogQuery{From: -5}, "Unix seconds"},
		{"inverted range", driveadmin.LogQuery{From: 200, To: 100}, "before the lower bound"},
	}
	for _, test := range cases {
		query := test.query
		if err := validateDriveAdminLogQuery(&query); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: error=%v, want %q", test.name, err, test.want)
		}
	}
}
