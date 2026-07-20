package notification

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ychiu1211/dsmctl/internal/domain/notification"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type executorFunc func(context.Context, compatibility.Request) (json.RawMessage, error)

func (function executorFunc) Execute(ctx context.Context, request compatibility.Request) (json.RawMessage, error) {
	return function(ctx, request)
}

func fullTarget() compatibility.Target {
	target := compatibility.NewTarget()
	for name, versions := range map[string][2]int{
		MailConfAPI:        {1, 2},
		PushMailAPI:        {1, 2},
		PushConfAPI:        {1, 1},
		PushMobileAPI:      {1, 2},
		WebhookProviderAPI: {1, 2},
		SMSConfAPI:         {1, 2},
		SMSProviderAPI:     {1, 2},
		FilterSettingsAPI:  {1, 2},
		DSMNotifyAPI:       {1, 1},
		NotifyStringsAPI:   {1, 1},
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

// fixtureExecutor serves canned responses keyed by API name (and, for the
// multiplexed DSMNotify method, by its action parameter).
func fixtureExecutor(t *testing.T, files map[string]string) executorFunc {
	t.Helper()
	return func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		key := request.API
		if request.API == DSMNotifyAPI {
			action, _ := request.JSONParameters["action"].(string)
			key = request.API + ":" + action
		}
		name, ok := files[key]
		if !ok {
			t.Fatalf("unexpected request %s.%s v%d (params %v)", request.API, request.Method, request.Version, request.JSONParameters)
		}
		return fixture(t, name), nil
	}
}

func mustMarshal(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

func TestReadMailDecodesWithoutSecrets(t *testing.T) {
	executor := fixtureExecutor(t, map[string]string{
		MailConfAPI: "mail-conf-v2.json",
		PushMailAPI: "push-mail-v2.json",
	})
	state, selection, err := ReadMail(context.Background(), fullTarget(), executor)
	if err != nil || !selection.Supported || selection.Version != 2 {
		t.Fatalf("state=%#v selection=%#v err=%v", state, selection, err)
	}
	if !state.Enabled || state.OAuthEnabled || state.SenderMail != "nas@example.com" || state.SenderName != "Lab NAS" ||
		state.SubjectPrefix != "[NAS]" || !state.WelcomeMailEnabled {
		t.Fatalf("mail state = %#v", state)
	}
	wantSMTP := notification.SMTPInfo{Server: "smtp.example.com", Port: 587, SSL: true, VerifyCert: true, AuthEnabled: true, AuthUser: "smtp-user"}
	if state.SMTP != wantSMTP {
		t.Fatalf("smtp = %#v, want %#v", state.SMTP, wantSMTP)
	}
	wantRecipients := []notification.MailRecipient{
		{Address: "admin@example.com", Name: "Admins"},
		{Address: "ops@example.com"},
	}
	if !reflect.DeepEqual(state.Recipients, wantRecipients) {
		t.Fatalf("recipients = %#v, want %#v", state.Recipients, wantRecipients)
	}
	if state.Relay == nil || !state.Relay.Enabled || state.Relay.SubjectPrefix != "[relay]" ||
		!reflect.DeepEqual(state.Relay.Recipients, []notification.MailRecipient{{Address: "relay@example.com"}}) {
		t.Fatalf("relay = %#v", state.Relay)
	}
	if encoded := mustMarshal(t, state); strings.Contains(encoded, "passwd") || strings.Contains(encoded, "password") {
		t.Fatalf("mail state leaks a password field: %s", encoded)
	}
}

func TestReadMailWithoutRelayAPI(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(MailConfAPI, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1, RequestFormat: "JSON"})
	executor := fixtureExecutor(t, map[string]string{MailConfAPI: "mail-conf-v2.json"})
	state, selection, err := ReadMail(context.Background(), target, executor)
	if err != nil || !selection.Supported || selection.Version != 1 {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
	if state.Relay != nil {
		t.Fatalf("relay should be nil when the relay API is absent, got %#v", state.Relay)
	}
}

func TestReadMailRejectsMalformedShape(t *testing.T) {
	executor := executorFunc(func(context.Context, compatibility.Request) (json.RawMessage, error) {
		return json.RawMessage(`{"unexpected":true}`), nil
	})
	if _, _, err := ReadMail(context.Background(), fullTarget(), executor); err == nil || !strings.Contains(err.Error(), "enable_mail") {
		t.Fatalf("expected a missing-field decode error, got %v", err)
	}
}

func TestReadPushKindsAndTokenAbsence(t *testing.T) {
	executor := fixtureExecutor(t, map[string]string{
		PushConfAPI:   "push-conf-v1.json",
		PushMobileAPI: "push-mobile-v2.json",
	})
	state, selection, err := ReadPush(context.Background(), fullTarget(), executor)
	if err != nil || !selection.Supported {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
	want := notification.PushState{
		MobileEnabled: true,
		Devices: []notification.PushDevice{
			{TargetID: "11", Kind: "device", Name: "Pixel 9", Model: "Pixel 9", LastSeen: "1784000000"},
			{TargetID: "12", Kind: "browser", Name: "Chrome on desktop"},
		},
	}
	if !reflect.DeepEqual(state, want) {
		t.Fatalf("push state = %#v, want %#v", state, want)
	}
	if encoded := mustMarshal(t, state); strings.Contains(encoded, "SECRET-PUSH-TOKEN") || strings.Contains(encoded, "token") {
		t.Fatalf("push state leaks a device token: %s", encoded)
	}
}

func TestReadWebhookNeverDecodesSecrets(t *testing.T) {
	executor := fixtureExecutor(t, map[string]string{WebhookProviderAPI: "webhook-list-v2.json"})
	state, selection, err := ReadWebhook(context.Background(), fullTarget(), executor)
	if err != nil || !selection.Supported || selection.Version != 2 {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
	enabled := true
	want := notification.WebhookState{Providers: []notification.WebhookProvider{
		{ProfileID: "3", Name: "Ops chat", Kind: "custom", Enabled: &enabled},
	}}
	if !reflect.DeepEqual(state, want) {
		t.Fatalf("webhook state = %#v, want %#v", state, want)
	}
	encoded := mustMarshal(t, state)
	for _, secret := range []string{"WEBHOOK-SECRET", "hooks.example.com", "url", "token"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("webhook state leaks %q: %s", secret, encoded)
		}
	}
}

func TestReadSMSOmitsAuthMaterial(t *testing.T) {
	executor := fixtureExecutor(t, map[string]string{
		SMSConfAPI:     "sms-conf-v2.json",
		SMSProviderAPI: "sms-provider-v2.json",
	})
	state, selection, err := ReadSMS(context.Background(), fullTarget(), executor)
	if err != nil || !selection.Supported {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
	if !state.Enabled || state.Provider != "clickatell" || state.IntervalMinutes != 10 {
		t.Fatalf("sms state = %#v", state)
	}
	if !reflect.DeepEqual(state.Phones, []notification.SMSPhone{{CountryCode: "886", Number: "912345678", Prefix: "+"}}) {
		t.Fatalf("phones = %#v", state.Phones)
	}
	wantProviders := []notification.SMSProviderInfo{
		{ID: "clickatell", Name: "clickatell", Method: "get", RequiredParams: []string{"api_id", "user"}},
		{ID: "SendinBlue-v3", Name: "SendinBlue-v3", Method: "post", RequiredParams: []string{"api_key", "sender"}},
	}
	if !reflect.DeepEqual(state.Providers, wantProviders) {
		t.Fatalf("providers = %#v, want %#v", state.Providers, wantProviders)
	}
	encoded := mustMarshal(t, state)
	for _, secret := range []string{"sms-account", "3148203", "@@PASS@@", "api.example.com", "template"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("sms state leaks %q: %s", secret, encoded)
		}
	}
}

func TestReadRulesNormalizesLevels(t *testing.T) {
	executor := fixtureExecutor(t, map[string]string{FilterSettingsAPI: "filter-settings-v2.json"})
	state, selection, err := ReadRules(context.Background(), fullTarget(), executor)
	if err != nil || !selection.Supported || selection.Version != 2 {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
	if len(state.Profiles) != 1 || state.Profiles[0].Name != "All" || len(state.Profiles[0].Events) != 2 {
		t.Fatalf("rules state = %#v", state)
	}
	first := state.Profiles[0].Events[0]
	if first.Name != "AutoBlockAdd" || first.Level != notification.LevelInfo || first.Group != "System" ||
		first.Title != "IP address blocked" || first.AppID != "SYNO.SDS.AdminCenter.Application" || first.WarnPercent != 1 {
		t.Fatalf("first event = %#v", first)
	}
	if state.Profiles[0].Events[1].Level != notification.LevelError {
		t.Fatalf("second event level = %#v", state.Profiles[0].Events[1])
	}
}

func TestReadRulesRejectsProfilelessResponse(t *testing.T) {
	executor := executorFunc(func(context.Context, compatibility.Request) (json.RawMessage, error) {
		return json.RawMessage(`{"success":true}`), nil
	})
	if _, _, err := ReadRules(context.Background(), fullTarget(), executor); err == nil || !strings.Contains(err.Error(), "no profile") {
		t.Fatalf("expected a profileless decode error, got %v", err)
	}
}

func TestReadDesktopTogglesAndSort(t *testing.T) {
	executor := fixtureExecutor(t, map[string]string{DSMNotifyAPI + ":loadHaveNtAppList": "dsmnotify-applist.json"})
	state, selection, err := ReadDesktop(context.Background(), fullTarget(), executor)
	if err != nil || !selection.Supported {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
	want := notification.DesktopState{Categories: []notification.DesktopCategory{
		{Category: "Package Center", AppIDs: []string{"SYNO.SDS.PkgManApp.Instance"}, Enabled: false},
		{Category: "System", AppIDs: []string{"SYNO.SDS.AdminCenter.Application", "SYNO.SDS.App.FileStation3.Instance"}, Enabled: true},
	}}
	if !reflect.DeepEqual(state, want) {
		t.Fatalf("desktop state = %#v, want %#v", state, want)
	}
}

func TestReadHistoryRequestAndRendering(t *testing.T) {
	var loadParams, stringsParams map[string]any
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		switch request.API {
		case DSMNotifyAPI:
			if request.Method != "notify" || request.Version != 1 {
				t.Fatalf("history request = %#v", request)
			}
			loadParams = request.JSONParameters
			return fixture(t, "dsmnotify-load.json"), nil
		case NotifyStringsAPI:
			if request.Method != "get" || request.Version != 1 {
				t.Fatalf("strings request = %#v", request)
			}
			stringsParams = request.JSONParameters
			return fixture(t, "dsmnotify-strings.json"), nil
		default:
			t.Fatalf("unexpected API %s", request.API)
			return nil, nil
		}
	})
	state, selection, err := ReadHistory(context.Background(), fullTarget(), executor, HistoryInput{
		Offset: 5, Limit: 10, Level: "NOTIFICATION_ERROR", DateFrom: 1784000000, DateTo: 1785000000,
	})
	if err != nil || !selection.Supported {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
	wantLoad := map[string]any{
		"action": "load", "offset": 5, "limit": 10,
		"sortBy": "time", "sortDir": "DESC",
		"level": "NOTIFICATION_ERROR", "dateFrom": int64(1784000000), "dateTo": int64(1785000000),
	}
	if !reflect.DeepEqual(loadParams, wantLoad) {
		t.Fatalf("load parameters = %#v, want %#v", loadParams, wantLoad)
	}
	if !reflect.DeepEqual(stringsParams, map[string]any{"lang": "enu"}) {
		t.Fatalf("strings parameters = %#v", stringsParams)
	}
	if state.Total != 30 || state.NewestTime != 1784383435 || len(state.Entries) != 3 {
		t.Fatalf("history state = %#v", state)
	}

	first := state.Entries[0]
	if first.ID != 264 || first.Level != notification.LevelError || first.Key != "PkgMgr_OpFail_DependPkgs" ||
		first.Source != "SYNO.SDS.PkgManApp.Instance" || first.HasMail {
		t.Fatalf("first entry = %#v", first)
	}
	if first.Title != "Package installation failed" {
		t.Fatalf("first title = %q", first.Title)
	}
	if first.Message != "Failed to install Surveillance Station on LabNAS.\nCheck Package Center." {
		t.Fatalf("first message = %q", first.Message)
	}
	if first.Time != time.Unix(1784383435, 0).Format("2006/01/02 15:04:05") || first.TimeUnix != 1784383435 {
		t.Fatalf("first time = %q (%d)", first.Time, first.TimeUnix)
	}
	if first.Vars["%PKG_NAME%"] != "Surveillance Station" {
		t.Fatalf("first vars = %#v", first.Vars)
	}

	// The second entry needs a nested substitution: %VOLUME_NAME% resolves to
	// "%_VOLUME% 1", which itself contains a placeholder.
	second := state.Entries[1]
	if second.Message != "Failed to mount the SSD cache on Volume 1." || second.Title != "SSD cache mount failed" || !second.HasMail {
		t.Fatalf("second entry = %#v", second)
	}

	// The third entry has no template: the raw key stays as the title and the
	// literal msg element becomes the message.
	third := state.Entries[2]
	if third.Title != "CustomScriptNote" || third.Message != "A literal custom message" || third.Vars != nil {
		t.Fatalf("third entry = %#v", third)
	}
}

func TestReadHistoryOmitsUnsetFilters(t *testing.T) {
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API == NotifyStringsAPI {
			return fixture(t, "dsmnotify-strings.json"), nil
		}
		want := map[string]any{"action": "load", "offset": 0, "limit": 30, "sortBy": "time", "sortDir": "DESC"}
		if !reflect.DeepEqual(request.JSONParameters, want) {
			t.Fatalf("parameters = %#v, want %#v", request.JSONParameters, want)
		}
		return fixture(t, "dsmnotify-load.json"), nil
	})
	if _, _, err := ReadHistory(context.Background(), fullTarget(), executor, HistoryInput{Limit: 30}); err != nil {
		t.Fatalf("ReadHistory() error = %v", err)
	}
}

func TestReadHistorySurvivesMissingStringTable(t *testing.T) {
	// Without the Strings API the entries keep their raw keys; a failing
	// Strings call behaves the same.
	target := compatibility.NewTarget()
	target.SetAPI(DSMNotifyAPI, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1, RequestFormat: "JSON"})
	executor := fixtureExecutor(t, map[string]string{DSMNotifyAPI + ":load": "dsmnotify-load.json"})
	state, _, err := ReadHistory(context.Background(), target, executor, HistoryInput{Limit: 5})
	if err != nil {
		t.Fatalf("ReadHistory() error = %v", err)
	}
	if state.Entries[0].Title != "PkgMgr_OpFail_DependPkgs" || state.Entries[0].Message != "" {
		t.Fatalf("expected raw fallback, got %#v", state.Entries[0])
	}
}

func TestHistoryRejectsMalformedShape(t *testing.T) {
	executor := executorFunc(func(context.Context, compatibility.Request) (json.RawMessage, error) {
		return json.RawMessage(`{"items":[]}`), nil
	})
	if _, _, err := ReadHistory(context.Background(), fullTarget(), executor, HistoryInput{Limit: 5}); err == nil || !strings.Contains(err.Error(), "total") {
		t.Fatalf("expected a missing-total decode error, got %v", err)
	}
}

func TestIndependentAreaSelection(t *testing.T) {
	empty := compatibility.NewTarget()
	for name, selectArea := range map[string]func(compatibility.Target) (compatibility.Selection, error){
		"mail": SelectMail, "push": SelectPush, "webhook": SelectWebhook,
		"sms": SelectSMS, "rules": SelectRules, "desktop": SelectDesktop, "history": SelectHistory,
	} {
		if selection, err := selectArea(empty); !compatibility.IsUnsupported(err) || selection.Supported {
			t.Fatalf("%s: expected unsupported on an empty target, got %#v err=%v", name, selection, err)
		}
	}

	// A target that only serves DSMNotify supports history and desktop while
	// every settings channel stays independently unsupported.
	bellOnly := compatibility.NewTarget()
	bellOnly.SetAPI(DSMNotifyAPI, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1, RequestFormat: "JSON"})
	if selection, err := SelectHistory(bellOnly); err != nil || !selection.Supported {
		t.Fatalf("history should be supported, got %#v err=%v", selection, err)
	}
	if selection, err := SelectDesktop(bellOnly); err != nil || !selection.Supported {
		t.Fatalf("desktop should be supported, got %#v err=%v", selection, err)
	}
	if selection, err := SelectMail(bellOnly); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("mail should stay unsupported, got %#v err=%v", selection, err)
	}
}

func TestAPINamesCoverEveryArea(t *testing.T) {
	want := []string{
		DSMNotifyAPI, NotifyStringsAPI,
		FilterSettingsAPI,
		MailConfAPI,
		PushConfAPI, PushMailAPI, PushMobileAPI,
		WebhookProviderAPI,
		SMSConfAPI, SMSProviderAPI,
	}
	names := APINames()
	got := map[string]bool{}
	for _, name := range names {
		got[name] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Fatalf("APINames() = %v is missing %s", names, name)
		}
	}
}
