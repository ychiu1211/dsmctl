// Package notification implements independently selectable, read-only DSM
// operations for the Control Panel → Notification surface (email, push,
// webhook, SMS, event rule catalog) and the per-user desktop notification
// feed: per-category desktop toggles and the notification history behind the
// DSM bell. Each area reads a distinct DSM API family; an area whose API is
// absent is reported unsupported without affecting the others.
//
// Every read shape was live-verified on DSM 7.3 (see WI-072's wire map). The
// history and desktop reads share SYNO.Core.DSMNotify v1, whose single method
// "notify" is multiplexed by an "action" parameter (load /
// loadHaveNtAppList); the delete/clear actions of the same method are
// mutations and are deliberately not implemented here.
package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ychiu1211/dsmctl/internal/domain/notification"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	MailConfAPI        = "SYNO.Core.Notification.Mail.Conf"
	PushMailAPI        = "SYNO.Core.Notification.Push.Mail"
	PushConfAPI        = "SYNO.Core.Notification.Push.Conf"
	PushMobileAPI      = "SYNO.Core.Notification.Push.Mobile"
	WebhookProviderAPI = "SYNO.Core.Notification.Push.Webhook.Provider"
	SMSConfAPI         = "SYNO.Core.Notification.SMS.Conf"
	SMSProviderAPI     = "SYNO.Core.Notification.SMS.Provider"
	FilterSettingsAPI  = "SYNO.Core.Notification.Advance.FilterSettings"
	DSMNotifyAPI       = "SYNO.Core.DSMNotify"
	NotifyStringsAPI   = "SYNO.Core.DSMNotify.Strings"

	MailReadCapabilityName    = "notification.mail.read"
	PushReadCapabilityName    = "notification.push.read"
	WebhookReadCapabilityName = "notification.webhook.read"
	SMSReadCapabilityName     = "notification.sms.read"
	RulesReadCapabilityName   = "notification.rules.read"
	DesktopReadCapabilityName = "notification.desktop.read"
	HistoryReadCapabilityName = "notification.history.read"
)

// Input is the empty request the settings read operations take.
type Input struct{}

// HistoryInput carries the DSM-applied history filters. Level is DSM's wire
// value (NOTIFICATION_*), already validated by the domain query.
type HistoryInput struct {
	Offset   int
	Limit    int
	Level    string
	DateFrom int64
	DateTo   int64
	Lang     string
}

// Intermediate decoded partials, composed into domain state by the Read
// functions below.
type mailConf struct {
	Enabled            bool
	OAuthEnabled       bool
	SenderName         string
	SenderMail         string
	SubjectPrefix      string
	WelcomeMailEnabled bool
	SMTP               notification.SMTPInfo
	Recipients         []notification.MailRecipient
}

type relayMailConf struct {
	Enabled       bool
	Recipients    []notification.MailRecipient
	SubjectPrefix string
}

type pushConf struct {
	MobileEnabled bool
}

type smsConf struct {
	Enabled         bool
	Provider        string
	Phones          []notification.SMSPhone
	IntervalMinutes int
}

type historyPage struct {
	Total      int
	NewestTime int64
	Items      []historyItem
}

type historyItem struct {
	ID      int64
	Time    int64
	Level   string
	Key     string
	Source  string
	HasMail bool
	Vars    map[string]string
}

// stringTemplates is one entry of the DSM notification string table.
type stringTemplates struct {
	Title string `json:"title"`
	Msg   string `json:"msg"`
	Level string `json:"level"`
}

func readVariant[O any](name, api string, version, priority int, method string, decode func(json.RawMessage) (O, error)) compatibility.Variant[Input, O] {
	return compatibility.Variant[Input, O]{
		Name:     name,
		API:      api,
		Version:  version,
		Priority: priority,
		Match:    compatibility.APIVersion(api, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (O, error) {
			data, err := executor.Execute(ctx, compatibility.Request{API: api, Version: version, Method: method})
			if err != nil {
				var zero O
				return zero, fmt.Errorf("call %s.%s v%d: %w", api, method, version, err)
			}
			return decode(data)
		},
	}
}

var mailConfOp = compatibility.Operation[Input, mailConf]{
	Name: "notification.mail.conf.read",
	Variants: []compatibility.Variant[Input, mailConf]{
		readVariant("notification-mail-conf-get-v2", MailConfAPI, 2, 20, "get", decodeMailConf),
		readVariant("notification-mail-conf-get-v1", MailConfAPI, 1, 10, "get", decodeMailConf),
	},
}

var relayMailOp = compatibility.Operation[Input, relayMailConf]{
	Name: "notification.mail.relay.read",
	Variants: []compatibility.Variant[Input, relayMailConf]{
		readVariant("notification-push-mail-get-v2", PushMailAPI, 2, 20, "get", decodeRelayMailConf),
		readVariant("notification-push-mail-get-v1", PushMailAPI, 1, 10, "get", decodeRelayMailConf),
	},
}

var pushConfOp = compatibility.Operation[Input, pushConf]{
	Name: "notification.push.conf.read",
	Variants: []compatibility.Variant[Input, pushConf]{
		readVariant("notification-push-conf-get-v1", PushConfAPI, 1, 10, "get", decodePushConf),
	},
}

var pushMobileOp = compatibility.Operation[Input, []notification.PushDevice]{
	Name: "notification.push.mobile.read",
	Variants: []compatibility.Variant[Input, []notification.PushDevice]{
		readVariant("notification-push-mobile-list-v2", PushMobileAPI, 2, 20, "list", decodePushDevices),
		readVariant("notification-push-mobile-list-v1", PushMobileAPI, 1, 10, "list", decodePushDevices),
	},
}

var webhookOp = compatibility.Operation[Input, []notification.WebhookProvider]{
	Name: "notification.webhook.read",
	Variants: []compatibility.Variant[Input, []notification.WebhookProvider]{
		readVariant("notification-webhook-provider-list-v2", WebhookProviderAPI, 2, 20, "list", decodeWebhookProviders),
		readVariant("notification-webhook-provider-list-v1", WebhookProviderAPI, 1, 10, "list", decodeWebhookProviders),
	},
}

var smsConfOp = compatibility.Operation[Input, smsConf]{
	Name: "notification.sms.conf.read",
	Variants: []compatibility.Variant[Input, smsConf]{
		readVariant("notification-sms-conf-get-v2", SMSConfAPI, 2, 20, "get", decodeSMSConf),
		readVariant("notification-sms-conf-get-v1", SMSConfAPI, 1, 10, "get", decodeSMSConf),
	},
}

var smsProviderOp = compatibility.Operation[Input, []notification.SMSProviderInfo]{
	Name: "notification.sms.provider.read",
	Variants: []compatibility.Variant[Input, []notification.SMSProviderInfo]{
		readVariant("notification-sms-provider-list-v2", SMSProviderAPI, 2, 20, "list", decodeSMSProviders),
		readVariant("notification-sms-provider-list-v1", SMSProviderAPI, 1, 10, "list", decodeSMSProviders),
	},
}

var rulesOp = compatibility.Operation[Input, notification.RulesState]{
	Name: "notification.rules.read",
	Variants: []compatibility.Variant[Input, notification.RulesState]{
		readVariant("notification-filter-settings-list-v2", FilterSettingsAPI, 2, 20, "list", decodeRules),
		readVariant("notification-filter-settings-list-v1", FilterSettingsAPI, 1, 10, "list", decodeRules),
	},
}

var desktopOp = compatibility.Operation[Input, notification.DesktopState]{
	Name: "notification.desktop.read",
	Variants: []compatibility.Variant[Input, notification.DesktopState]{
		{
			Name: "dsmnotify-app-list-v1", API: DSMNotifyAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(DSMNotifyAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (notification.DesktopState, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: DSMNotifyAPI, Version: 1, Method: "notify",
					JSONParameters: map[string]any{"action": "loadHaveNtAppList"},
				})
				if err != nil {
					return notification.DesktopState{}, fmt.Errorf("call %s.notify v1 (loadHaveNtAppList): %w", DSMNotifyAPI, err)
				}
				return decodeDesktop(data)
			},
		},
	},
}

var historyOp = compatibility.Operation[HistoryInput, historyPage]{
	Name: "notification.history.read",
	Variants: []compatibility.Variant[HistoryInput, historyPage]{
		{
			Name: "dsmnotify-load-v1", API: DSMNotifyAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(DSMNotifyAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input HistoryInput) (historyPage, error) {
				parameters := map[string]any{
					"action": "load",
					"offset": input.Offset,
					"limit":  input.Limit,
					"sortBy": "time", "sortDir": "DESC",
				}
				if input.Level != "" {
					parameters["level"] = input.Level
				}
				if input.DateFrom > 0 {
					parameters["dateFrom"] = input.DateFrom
				}
				if input.DateTo > 0 {
					parameters["dateTo"] = input.DateTo
				}
				data, err := executor.Execute(ctx, compatibility.Request{
					API: DSMNotifyAPI, Version: 1, Method: "notify", JSONParameters: parameters,
				})
				if err != nil {
					return historyPage{}, fmt.Errorf("call %s.notify v1 (load): %w", DSMNotifyAPI, err)
				}
				return decodeHistoryPage(data)
			},
		},
	},
}

// stringsOp fetches the notification string-template table used to render
// history entries. It is an enrichment: history stays readable (raw keys and
// variables) when the table cannot be fetched.
var stringsOp = compatibility.Operation[HistoryInput, map[string]stringTemplates]{
	Name: "notification.history.strings.read",
	Variants: []compatibility.Variant[HistoryInput, map[string]stringTemplates]{
		{
			Name: "dsmnotify-strings-get-v1", API: NotifyStringsAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(NotifyStringsAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input HistoryInput) (map[string]stringTemplates, error) {
				lang := input.Lang
				if lang == "" {
					lang = "enu"
				}
				data, err := executor.Execute(ctx, compatibility.Request{
					API: NotifyStringsAPI, Version: 1, Method: "get",
					JSONParameters: map[string]any{"lang": lang},
				})
				if err != nil {
					return nil, fmt.Errorf("call %s.get v1: %w", NotifyStringsAPI, err)
				}
				return decodeStringTable(data)
			},
		},
	},
}

// APINames returns every DSM API this module may read, so the facade can
// discover them in a single query before selecting any area.
func APINames() []string {
	unique := map[string]struct{}{}
	for _, names := range [][]string{
		mailConfOp.APINames(), relayMailOp.APINames(),
		pushConfOp.APINames(), pushMobileOp.APINames(),
		webhookOp.APINames(),
		smsConfOp.APINames(), smsProviderOp.APINames(),
		rulesOp.APINames(), desktopOp.APINames(),
		historyOp.APINames(), stringsOp.APINames(),
	} {
		for _, name := range names {
			unique[name] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for name := range unique {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// Select* report each area's primary read selection, so capabilities can be
// described without a read.
func SelectMail(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := mailConfOp.Select(target)
	return selection, err
}

func SelectPush(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := pushConfOp.Select(target)
	return selection, err
}

func SelectWebhook(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := webhookOp.Select(target)
	return selection, err
}

func SelectSMS(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := smsConfOp.Select(target)
	return selection, err
}

func SelectRules(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := rulesOp.Select(target)
	return selection, err
}

func SelectDesktop(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := desktopOp.Select(target)
	return selection, err
}

func SelectHistory(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := historyOp.Select(target)
	return selection, err
}

// ReadMail reads the email channel (required) and enriches it with the
// Synology-relay mode when that independently versioned API is available.
func ReadMail(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (notification.MailState, compatibility.Selection, error) {
	conf, selection, err := mailConfOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return notification.MailState{}, selection, err
	}
	state := notification.MailState{
		Enabled:            conf.Enabled,
		OAuthEnabled:       conf.OAuthEnabled,
		SenderName:         conf.SenderName,
		SenderMail:         conf.SenderMail,
		SubjectPrefix:      conf.SubjectPrefix,
		WelcomeMailEnabled: conf.WelcomeMailEnabled,
		SMTP:               conf.SMTP,
		Recipients:         conf.Recipients,
	}
	if state.Recipients == nil {
		state.Recipients = []notification.MailRecipient{}
	}
	if relay, ok, err := runOptional(ctx, target, executor, relayMailOp); err != nil {
		return notification.MailState{}, selection, err
	} else if ok {
		state.Relay = &notification.RelayMailState{
			Enabled:       relay.Enabled,
			Recipients:    relay.Recipients,
			SubjectPrefix: relay.SubjectPrefix,
		}
	}
	return state, selection, nil
}

// ReadPush reads the push channel toggle (required) and the paired push
// targets (skipped when the mobile list API is absent).
func ReadPush(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (notification.PushState, compatibility.Selection, error) {
	conf, selection, err := pushConfOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return notification.PushState{}, selection, err
	}
	state := notification.PushState{MobileEnabled: conf.MobileEnabled, Devices: []notification.PushDevice{}}
	if devices, ok, err := runOptional(ctx, target, executor, pushMobileOp); err != nil {
		return notification.PushState{}, selection, err
	} else if ok {
		state.Devices = devices
	}
	return state, selection, nil
}

// ReadWebhook reads the configured webhook providers.
func ReadWebhook(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (notification.WebhookState, compatibility.Selection, error) {
	providers, selection, err := webhookOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return notification.WebhookState{}, selection, err
	}
	return notification.WebhookState{Providers: providers}, selection, nil
}

// ReadSMS reads the SMS channel configuration (required) and the provider
// catalog (skipped when its API is absent).
func ReadSMS(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (notification.SMSState, compatibility.Selection, error) {
	conf, selection, err := smsConfOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return notification.SMSState{}, selection, err
	}
	state := notification.SMSState{
		Enabled:         conf.Enabled,
		Provider:        conf.Provider,
		Phones:          conf.Phones,
		IntervalMinutes: conf.IntervalMinutes,
	}
	if state.Phones == nil {
		state.Phones = []notification.SMSPhone{}
	}
	if providers, ok, err := runOptional(ctx, target, executor, smsProviderOp); err != nil {
		return notification.SMSState{}, selection, err
	} else if ok {
		state.Providers = providers
	}
	return state, selection, nil
}

// ReadRules reads the notification event rule catalog.
func ReadRules(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (notification.RulesState, compatibility.Selection, error) {
	return rulesOp.Run(ctx, target, executor, Input{})
}

// ReadDesktop reads the per-category desktop notification toggles of the
// signed-in user.
func ReadDesktop(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (notification.DesktopState, compatibility.Selection, error) {
	return desktopOp.Run(ctx, target, executor, Input{})
}

// ReadHistory reads one page of the DSM notification history and renders each
// entry's title and message from the DSM string-template table. When the
// string table cannot be read the raw key and variables are still returned.
func ReadHistory(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input HistoryInput) (notification.HistoryState, compatibility.Selection, error) {
	page, selection, err := historyOp.Run(ctx, target, executor, input)
	if err != nil {
		return notification.HistoryState{}, selection, err
	}
	templates := map[string]stringTemplates{}
	if _, stringsSelection, err := stringsOp.Select(target); err == nil && stringsSelection.Supported {
		if table, _, err := stringsOp.Run(ctx, target, executor, input); err == nil {
			templates = table
		}
		// A failed template fetch is intentionally non-fatal: the history
		// entries below fall back to their raw keys and variables.
	}
	return renderHistory(page, templates), selection, nil
}

// runOptional runs an enrichment operation only when its API is available. An
// unsupported operation is a normal "skip" (ok=false, nil error); any other
// selection or execution failure is returned.
func runOptional[O any](ctx context.Context, target compatibility.Target, executor compatibility.Executor, operation compatibility.Operation[Input, O]) (O, bool, error) {
	var zero O
	if _, selection, err := operation.Select(target); err != nil {
		if compatibility.IsUnsupported(err) {
			return zero, false, nil
		}
		return zero, false, err
	} else if !selection.Supported {
		return zero, false, nil
	}
	result, _, err := operation.Run(ctx, target, executor, Input{})
	if err != nil {
		return zero, false, err
	}
	return result, true, nil
}
