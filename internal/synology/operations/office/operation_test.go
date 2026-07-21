package office

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/office"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type capturingExecutor struct {
	request  compatibility.Request
	response json.RawMessage
}

func (executor *capturingExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	executor.request = request
	if executor.response != nil {
		return executor.response, nil
	}
	return json.RawMessage(`{}`), nil
}

func officeTarget(packageVersion string, running bool) compatibility.Target {
	target := compatibility.NewTarget()
	for _, api := range APINames() {
		target.SetAPI(api, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	}
	if packageVersion != "" {
		target.SetInstalledPackages([]compatibility.InstalledPackage{
			{ID: PackageID, Version: compatibility.ParsePackageVersion(packageVersion), Running: running},
		})
	} else {
		target.SetInstalledPackages(nil)
	}
	return target
}

func TestInfoReadDecodesLiveShape(t *testing.T) {
	target := officeTarget("3.7.2-22592", true)
	executor := &capturingExecutor{response: json.RawMessage(`{
		"ai_feedback_url":null,"gids":[100,101],"is_manager":true,
		"is_user_event_enabled":true,"mobile_compatibility":{"major":"1","minor":"3"},
		"schema_doc":13,"schema_mobile":2003,"schema_sheet":5,"schema_slide":13,
		"uid":1026,"username":"testuser",
		"version":{"build":"22592","hotfix":"2","major":"3","minor":"7"}
	}`)}

	info, selection, err := ExecuteInfoRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteInfoRead() error = %v", err)
	}
	if selection.Backend != "office-info-v1" || info.Version != "3.7.2-22592" || !info.IsManager ||
		info.SchemaDocument != 13 || info.SchemaSpreadsheet != 5 || info.SchemaSlides != 13 {
		t.Fatalf("info = %#v", info)
	}
}

func TestSystemReadDecodesLiveShape(t *testing.T) {
	target := officeTarget("3.7.2-22592", true)
	executor := &capturingExecutor{response: json.RawMessage(`{"history_prune":true}`)}

	settings, selection, err := ExecuteSystemRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteSystemRead() error = %v", err)
	}
	if selection.Backend != "office-setting-system-v1" || !settings.HistoryPrune {
		t.Fatalf("system settings = %#v", settings)
	}
}

func TestPreferencesReadDecodesLiveShape(t *testing.T) {
	target := officeTarget("3.7.2-22592", true)
	executor := &capturingExecutor{response: json.RawMessage(`{
		"ai_helper_languages":["English"],"ai_translator_language":"ja",
		"default_locale":"zh-TW","focus_mode":{},"formatting_marks":{},
		"formula_panel_expanded":true,"formula_panel_opened":false,
		"formula_preview":true,"hide_hint":{},"preference_settings":{},
		"ruler":true,"side_panel_width":{}
	}`)}

	preferences, selection, err := ExecutePreferencesRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecutePreferencesRead() error = %v", err)
	}
	want := office.Preferences{
		Ruler: true, FormulaPreview: true, FormulaPanelOpened: false, FormulaPanelExpanded: true,
		DefaultLocale: "zh-TW", AITranslatorLanguage: "ja", AIHelperLanguages: []string{"English"},
	}
	if selection.Backend != "office-setting-v1" || !reflect.DeepEqual(preferences, want) {
		t.Fatalf("preferences = %#v, want %#v", preferences, want)
	}
}

func TestFontsReadNormalizesMapToSortedSlice(t *testing.T) {
	target := officeTarget("3.7.2-22592", true)
	// Live shape from Office 3.7.2: system fonts are {} or {display}; custom
	// entries carry system:false and, when disabled, disable:true.
	executor := &capturingExecutor{response: json.RawMessage(`{
		"Verdana":{},
		"Arial":{},
		"標楷體":{},
		"Microsoft JhengHei":{"display":"微軟正黑體"},
		"dsmctl-e2e-font":{"system":false},
		"dsmctl-e2e-off":{"system":false,"disable":true}
	}`)}

	fonts, selection, err := ExecuteFontsRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteFontsRead() error = %v", err)
	}
	want := []office.Font{
		{Name: "Arial", Enabled: true},
		{Name: "Microsoft JhengHei", DisplayName: "微軟正黑體", Enabled: true},
		{Name: "Verdana", Enabled: true},
		{Name: "dsmctl-e2e-font", Custom: true, Enabled: true},
		{Name: "dsmctl-e2e-off", Custom: true, Enabled: false},
		{Name: "標楷體", Enabled: true},
	}
	if selection.Backend != "office-setting-font-v1" || !reflect.DeepEqual(fonts, want) {
		t.Fatalf("fonts = %#v, want %#v", fonts, want)
	}
}

func TestFontsSetSendsNamesArrayPerAction(t *testing.T) {
	target := officeTarget("3.7.2-22592", true)
	for _, action := range []office.FontAction{
		office.FontActionAdd, office.FontActionEnable, office.FontActionDisable, office.FontActionDelete,
	} {
		executor := &capturingExecutor{}
		change := office.FontChange{Action: action, Names: []string{"dsmctl-e2e-font"}}
		result, _, err := ExecuteFontsSet(context.Background(), target, executor, change)
		if err != nil {
			t.Fatalf("ExecuteFontsSet(%s) error = %v", action, err)
		}
		want := map[string]any{"fonts": []string{"dsmctl-e2e-font"}}
		if executor.request.API != FontAPIName || executor.request.Method != string(action) ||
			!reflect.DeepEqual(executor.request.JSONParameters, want) {
			t.Fatalf("fonts %s request = %#v, want method %q with %#v", action, executor.request, action, want)
		}
		if result.Method != string(action) {
			t.Fatalf("fonts %s result method = %q", action, result.Method)
		}
	}
}

func TestFontsSetRejectsEmptyNamesAndUnknownAction(t *testing.T) {
	target := officeTarget("3.7.2-22592", true)
	if _, _, err := ExecuteFontsSet(context.Background(), target, &capturingExecutor{}, office.FontChange{Action: office.FontActionAdd}); err == nil {
		t.Fatal("ExecuteFontsSet() accepted empty names")
	}
	if _, _, err := ExecuteFontsSet(context.Background(), target, &capturingExecutor{}, office.FontChange{Action: "rename", Names: []string{"x"}}); err == nil {
		t.Fatal("ExecuteFontsSet() accepted an unknown action")
	}
}

func TestSystemSetSendsOnlyPatchedFieldsWithDSMNames(t *testing.T) {
	target := officeTarget("3.7.2-22592", true)
	executor := &capturingExecutor{}
	prune := false
	if _, _, err := ExecuteSystemSet(context.Background(), target, executor, office.SystemChange{HistoryPrune: &prune}); err != nil {
		t.Fatalf("ExecuteSystemSet() error = %v", err)
	}
	want := map[string]any{"history_prune": false}
	if executor.request.API != SystemAPIName || executor.request.Method != "set" ||
		!reflect.DeepEqual(executor.request.JSONParameters, want) {
		t.Fatalf("system set request = %#v, want %#v", executor.request, want)
	}
}

func TestSystemSetRejectsEmptyPatch(t *testing.T) {
	target := officeTarget("3.7.2-22592", true)
	if _, _, err := ExecuteSystemSet(context.Background(), target, &capturingExecutor{}, office.SystemChange{}); err == nil {
		t.Fatal("ExecuteSystemSet() accepted an empty patch")
	}
}

func TestPreferencesSetSendsOnlyPatchedFieldsWithDSMNames(t *testing.T) {
	target := officeTarget("3.7.2-22592", true)
	executor := &capturingExecutor{}
	ruler := false
	languages := []string{"English", "日本語"}
	change := office.PreferencesChange{Ruler: &ruler, AIHelperLanguages: &languages}
	if _, _, err := ExecutePreferencesSet(context.Background(), target, executor, change); err != nil {
		t.Fatalf("ExecutePreferencesSet() error = %v", err)
	}
	want := map[string]any{"ruler": false, "ai_helper_languages": []string{"English", "日本語"}}
	if executor.request.API != SettingAPIName || executor.request.Method != "set" ||
		!reflect.DeepEqual(executor.request.JSONParameters, want) {
		t.Fatalf("preferences set request = %#v, want %#v", executor.request, want)
	}
}

func TestPreferencesSetRejectsEmptyPatch(t *testing.T) {
	target := officeTarget("3.7.2-22592", true)
	if _, _, err := ExecutePreferencesSet(context.Background(), target, &capturingExecutor{}, office.PreferencesChange{}); err == nil {
		t.Fatal("ExecutePreferencesSet() accepted an empty patch")
	}
}

func TestSelectFailsClosedWithoutPackage(t *testing.T) {
	selectors := map[string]func(compatibility.Target) (compatibility.Selection, error){
		"info read":        SelectInfoRead,
		"system read":      SelectSystemRead,
		"system set":       SelectSystemSet,
		"preferences read": SelectPreferencesRead,
		"preferences set":  SelectPreferencesSet,
		"fonts read":       SelectFontsRead,
		"fonts set":        SelectFontsSet,
	}
	for name, selectOperation := range selectors {
		if selection, err := selectOperation(officeTarget("", false)); err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
			t.Fatalf("%s without package = %#v, %v", name, selection, err)
		}
	}
}

func TestDecodeRejectsMissingCoreFields(t *testing.T) {
	if _, err := decodeSystemSettings(json.RawMessage(`{}`)); err == nil {
		t.Fatal("decodeSystemSettings() accepted a response missing history_prune")
	}
	if _, err := decodePreferences(json.RawMessage(`{"formula_preview":true}`)); err == nil {
		t.Fatal("decodePreferences() accepted a response missing ruler")
	}
	if _, err := decodeInfo(json.RawMessage(`{"is_manager":true}`)); err == nil {
		t.Fatal("decodeInfo() accepted a response missing version")
	}
}
