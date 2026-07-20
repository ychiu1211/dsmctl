package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/office"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type fakeOfficeClient struct {
	system       synology.OfficeSystemSettings
	preferences  synology.OfficePreferences
	fonts        []synology.OfficeFont
	systemErr    error
	systemSets   []office.SystemChange
	prefSets     []office.PreferencesChange
	fontSets     []office.FontChange
	applySystem  func(office.SystemChange)
	applyPrefs   func(office.PreferencesChange)
	applyFonts   func(office.FontChange)
	capabilities synology.OfficeCapabilities
}

func newFakeOfficeClient() *fakeOfficeClient {
	client := &fakeOfficeClient{
		system:      synology.OfficeSystemSettings{HistoryPrune: false},
		preferences: synology.OfficePreferences{Ruler: true, AIHelperLanguages: []string{}},
		fonts: []synology.OfficeFont{
			{Name: "Arial", Enabled: true},
			{Name: "dsmctl-e2e-font", Custom: true, Enabled: true},
		},
		capabilities: synology.OfficeCapabilities{
			Module: office.ModuleName, InfoRead: true,
			SystemRead: true, SystemSet: true,
			PreferencesRead: true, PreferencesSet: true, FontsRead: true, FontsSet: true,
			Package: office.PackageEvidence{ID: "Spreadsheet", Installed: true, Version: "3.7.2-22592", Running: true},
		},
	}
	client.applySystem = func(change office.SystemChange) {
		if change.HistoryPrune != nil {
			client.system.HistoryPrune = *change.HistoryPrune
		}
	}
	client.applyPrefs = func(change office.PreferencesChange) {
		if change.Ruler != nil {
			client.preferences.Ruler = *change.Ruler
		}
	}
	client.applyFonts = func(change office.FontChange) {
		for _, name := range change.Names {
			switch change.Action {
			case office.FontActionAdd:
				client.fonts = append(client.fonts, synology.OfficeFont{Name: name, Custom: true, Enabled: true})
			case office.FontActionEnable, office.FontActionDisable:
				for index := range client.fonts {
					if client.fonts[index].Name == name && client.fonts[index].Custom {
						client.fonts[index].Enabled = change.Action == office.FontActionEnable
					}
				}
			case office.FontActionDelete:
				kept := client.fonts[:0]
				for _, font := range client.fonts {
					if font.Name != name || !font.Custom {
						kept = append(kept, font)
					}
				}
				client.fonts = kept
			}
		}
	}
	return client
}

func (c *fakeOfficeClient) OfficeInfo(context.Context) (synology.OfficeInfo, error) {
	return synology.OfficeInfo{Version: "3.7.2-22592", IsManager: true}, nil
}

func (c *fakeOfficeClient) OfficeSystemSettings(context.Context) (synology.OfficeSystemSettings, error) {
	return c.system, c.systemErr
}

func (c *fakeOfficeClient) OfficePreferences(context.Context) (synology.OfficePreferences, error) {
	return c.preferences, nil
}

func (c *fakeOfficeClient) OfficeFonts(context.Context) ([]synology.OfficeFont, error) {
	fonts := make([]synology.OfficeFont, len(c.fonts))
	copy(fonts, c.fonts)
	return fonts, nil
}

func (c *fakeOfficeClient) OfficeCapabilities(context.Context) (synology.OfficeCapabilities, synology.CompatibilityReport, error) {
	return c.capabilities, synology.CompatibilityReport{}, nil
}

func (c *fakeOfficeClient) ApplyOfficeSystemChange(_ context.Context, change office.SystemChange) (synology.OfficeMutationResult, error) {
	c.systemSets = append(c.systemSets, change)
	c.applySystem(change)
	return synology.OfficeMutationResult{}, nil
}

func (c *fakeOfficeClient) ApplyOfficePreferencesChange(_ context.Context, change office.PreferencesChange) (synology.OfficeMutationResult, error) {
	c.prefSets = append(c.prefSets, change)
	c.applyPrefs(change)
	return synology.OfficeMutationResult{}, nil
}

func (c *fakeOfficeClient) ApplyOfficeFontChange(_ context.Context, change office.FontChange) (synology.OfficeMutationResult, error) {
	c.fontSets = append(c.fontSets, change)
	c.applyFonts(change)
	return synology.OfficeMutationResult{}, nil
}

func TestValidateOfficeChangeRequiresExactlyOneScope(t *testing.T) {
	if err := validateOfficeChange(office.Change{}); err == nil {
		t.Fatal("validateOfficeChange() accepted an empty change")
	}
	both := office.Change{
		System:      &office.SystemChange{HistoryPrune: boolPointer(true)},
		Preferences: &office.PreferencesChange{Ruler: boolPointer(true)},
	}
	if err := validateOfficeChange(both); err == nil {
		t.Fatal("validateOfficeChange() accepted a change with both scopes")
	}
	if err := validateOfficeChange(office.Change{System: &office.SystemChange{}}); err == nil {
		t.Fatal("validateOfficeChange() accepted an empty system patch")
	}
	if err := validateOfficeChange(office.Change{Preferences: &office.PreferencesChange{}}); err == nil {
		t.Fatal("validateOfficeChange() accepted an empty preferences patch")
	}
	if err := validateOfficeChange(office.Change{System: &office.SystemChange{HistoryPrune: boolPointer(true)}}); err != nil {
		t.Fatalf("validateOfficeChange() rejected a valid system patch: %v", err)
	}
}

func TestPlanOfficeChangeSystemScopeWarnsOnEnablingPrune(t *testing.T) {
	client := newFakeOfficeClient()
	request := office.Change{System: &office.SystemChange{HistoryPrune: boolPointer(true)}}

	plan, err := planOfficeChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("planOfficeChangeWithClient() error = %v", err)
	}
	if plan.Risk != "high" || len(plan.Warnings) != 1 {
		t.Fatalf("plan risk = %q, warnings = %v", plan.Risk, plan.Warnings)
	}
	if plan.Observed.System == nil || plan.Observed.Preferences != nil {
		t.Fatalf("plan observed the wrong scope: %#v", plan.Observed)
	}
}

func TestPlanOfficeChangeRejectsNoOpPatch(t *testing.T) {
	client := newFakeOfficeClient()
	request := office.Change{Preferences: &office.PreferencesChange{Ruler: boolPointer(true)}}

	if _, err := planOfficeChangeWithClient(context.Background(), "lab", client, request); err == nil ||
		!strings.Contains(err.Error(), "would not change") {
		t.Fatalf("planOfficeChangeWithClient() no-op error = %v", err)
	}
}

func TestApplyOfficePlanRejectsStaleState(t *testing.T) {
	client := newFakeOfficeClient()
	request := office.Change{System: &office.SystemChange{HistoryPrune: boolPointer(true)}}
	plan, err := planOfficeChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("planOfficeChangeWithClient() error = %v", err)
	}

	// Another manager changes an unrelated preference: still fresh. A system
	// change between plan and apply must be stale.
	client.system.HistoryPrune = true
	if _, err := applyOfficePlanWithClient(context.Background(), client, plan); err == nil {
		t.Fatal("applyOfficePlanWithClient() accepted a stale plan")
	}
	if len(client.systemSets) != 0 {
		t.Fatalf("stale apply still mutated DSM: %#v", client.systemSets)
	}
}

func TestApplyOfficePlanAppliesAndVerifiesPreferences(t *testing.T) {
	client := newFakeOfficeClient()
	request := office.Change{Preferences: &office.PreferencesChange{Ruler: boolPointer(false)}}
	plan, err := planOfficeChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("planOfficeChangeWithClient() error = %v", err)
	}

	result, err := applyOfficePlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("applyOfficePlanWithClient() error = %v", err)
	}
	if !result.Applied || len(client.prefSets) != 1 || client.preferences.Ruler {
		t.Fatalf("apply result = %#v, sets = %#v", result, client.prefSets)
	}
}

func TestPlanOfficeFontChangeValidatesTargets(t *testing.T) {
	client := newFakeOfficeClient()
	plan := func(action office.FontAction, names ...string) error {
		_, err := planOfficeChangeWithClient(context.Background(), "lab", client,
			office.Change{Fonts: &office.FontChange{Action: action, Names: names}})
		return err
	}
	if err := plan(office.FontActionDisable, "Arial"); err == nil ||
		!strings.Contains(err.Error(), "system font") {
		t.Fatalf("disable of a system font error = %v", err)
	}
	if err := plan(office.FontActionAdd, "Arial"); err == nil ||
		!strings.Contains(err.Error(), "system font") {
		t.Fatalf("re-add of a system font error = %v", err)
	}
	if err := plan(office.FontActionDelete, "missing-font"); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("delete of a missing font error = %v", err)
	}
	if err := plan(office.FontActionEnable, "dsmctl-e2e-font"); err == nil ||
		!strings.Contains(err.Error(), "would not change") {
		t.Fatalf("enable of an already-enabled font error = %v", err)
	}
	if err := plan(office.FontActionDisable, "dsmctl-e2e-font"); err != nil {
		t.Fatalf("valid disable plan error = %v", err)
	}
}

func TestApplyOfficeFontPlanAppliesAndVerifies(t *testing.T) {
	client := newFakeOfficeClient()
	request := office.Change{Fonts: &office.FontChange{Action: office.FontActionAdd, Names: []string{"dsmctl-e2e-new"}}}
	plan, err := planOfficeChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("planOfficeChangeWithClient() error = %v", err)
	}
	result, err := applyOfficePlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("applyOfficePlanWithClient() error = %v", err)
	}
	if !result.Applied || len(client.fontSets) != 1 {
		t.Fatalf("apply result = %#v, sets = %#v", result, client.fontSets)
	}
}

func TestApplyOfficeFontPlanFailsOnSilentSkip(t *testing.T) {
	client := newFakeOfficeClient()
	// DSM accepts the call but silently skips the change.
	client.applyFonts = func(office.FontChange) {}
	request := office.Change{Fonts: &office.FontChange{Action: office.FontActionDelete, Names: []string{"dsmctl-e2e-font"}}}
	plan, err := planOfficeChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("planOfficeChangeWithClient() error = %v", err)
	}
	if _, err := applyOfficePlanWithClient(context.Background(), client, plan); err == nil ||
		!strings.Contains(err.Error(), "do not match the approved patch") {
		t.Fatalf("silent-skip apply error = %v", err)
	}
}

func TestApplyOfficePlanFailsWhenPostconditionDoesNotHold(t *testing.T) {
	client := newFakeOfficeClient()
	// DSM accepts the set but silently does not change the value.
	client.applySystem = func(office.SystemChange) {}
	request := office.Change{System: &office.SystemChange{HistoryPrune: boolPointer(true)}}
	plan, err := planOfficeChangeWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("planOfficeChangeWithClient() error = %v", err)
	}

	if _, err := applyOfficePlanWithClient(context.Background(), client, plan); err == nil ||
		!strings.Contains(err.Error(), "do not match the approved patch") {
		t.Fatalf("applyOfficePlanWithClient() postcondition error = %v", err)
	}
}
