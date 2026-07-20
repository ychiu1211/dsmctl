// Package notification contains stable, read-only models for the DSM
// Control Panel → Notification surface (email, push, webhook, SMS channels and
// the event rule catalog) plus the per-user desktop notification feed: the
// per-category desktop toggles and the notification history the DSM bell shows.
// Each area owns a separate state type and a separate DSM API family, so one
// area being unavailable never disables the others.
//
// These models are deliberately free of any authentication material. The SMTP
// password, SMS provider account/token (and credential-bearing send-URL
// templates), webhook secrets, and per-device push tokens are never decoded, so
// a display or MCP path cannot leak them.
package notification

import (
	"fmt"
	"strings"
)

const (
	// Level* are the normalized severities of notification events and history
	// entries, matching the vocabulary of the DSM log module.
	LevelInfo    = "info"
	LevelWarning = "warn"
	LevelError   = "error"
)

// SMTPInfo is the non-secret SMTP transport configuration of the email channel.
type SMTPInfo struct {
	Server      string `json:"server,omitempty" jsonschema:"SMTP server hostname; empty when not configured"`
	Port        int    `json:"port,omitempty" jsonschema:"SMTP server port"`
	SSL         bool   `json:"ssl" jsonschema:"Whether the SMTP connection uses SSL/TLS"`
	VerifyCert  bool   `json:"verify_cert" jsonschema:"Whether DSM verifies the SMTP server certificate"`
	AuthEnabled bool   `json:"auth_enabled" jsonschema:"Whether SMTP authentication is enabled"`
	AuthUser    string `json:"auth_user,omitempty" jsonschema:"SMTP authentication username; the password is never read"`
}

// MailRecipient is one configured notification email recipient, decoded
// tolerantly (the lab used to model this type has none configured).
type MailRecipient struct {
	Address string `json:"address,omitempty" jsonschema:"Recipient email address"`
	Name    string `json:"name,omitempty" jsonschema:"Recipient display or profile name, when DSM reports one"`
}

// RelayMailState is the Synology-relay email mode (send notification mail
// through Synology's servers instead of a custom SMTP server).
type RelayMailState struct {
	Enabled       bool            `json:"enabled" jsonschema:"Whether relay-based notification email is enabled"`
	Recipients    []MailRecipient `json:"recipients,omitempty" jsonschema:"Recipients configured for relay email"`
	SubjectPrefix string          `json:"subject_prefix,omitempty" jsonschema:"Subject prefix applied to relay email"`
}

// MailState is the normalized email notification channel.
type MailState struct {
	Enabled            bool            `json:"enabled" jsonschema:"Whether custom-SMTP notification email is enabled"`
	OAuthEnabled       bool            `json:"oauth_enabled" jsonschema:"Whether the SMTP account authenticates via OAuth instead of a password"`
	SenderName         string          `json:"sender_name,omitempty" jsonschema:"Sender display name"`
	SenderMail         string          `json:"sender_mail,omitempty" jsonschema:"Sender email address"`
	SubjectPrefix      string          `json:"subject_prefix,omitempty" jsonschema:"Subject prefix applied to notification email"`
	WelcomeMailEnabled bool            `json:"welcome_mail_enabled" jsonschema:"Whether DSM sends welcome mail to new users"`
	SMTP               SMTPInfo        `json:"smtp" jsonschema:"Non-secret SMTP transport configuration; the password is never read"`
	Recipients         []MailRecipient `json:"recipients" jsonschema:"Configured recipients; empty when none"`
	Relay              *RelayMailState `json:"relay,omitempty" jsonschema:"Synology-relay email mode; null when the relay API is unavailable"`
}

// PushDevice is one paired push target (a mobile app installation or a browser
// registration), decoded tolerantly. Push tokens are never decoded.
type PushDevice struct {
	TargetID string `json:"target_id,omitempty" jsonschema:"Stable DSM identifier of the paired push target"`
	Kind     string `json:"kind,omitempty" jsonschema:"Target kind: device (mobile app) or browser"`
	Name     string `json:"name,omitempty" jsonschema:"Device or browser name reported by DSM"`
	Model    string `json:"model,omitempty" jsonschema:"Device model, when reported"`
	LastSeen string `json:"last_seen,omitempty" jsonschema:"Last activity time reported by DSM, when present"`
}

// PushState is the normalized push notification channel: the global mobile
// toggle plus the paired targets.
type PushState struct {
	MobileEnabled bool         `json:"mobile_enabled" jsonschema:"Whether push notifications to paired mobile devices are enabled"`
	Devices       []PushDevice `json:"devices" jsonschema:"Paired push targets; empty when none"`
}

// WebhookProvider is one configured webhook notification provider, decoded
// tolerantly. The webhook URL and any token or signing secret are never
// decoded.
type WebhookProvider struct {
	ProfileID string `json:"profile_id,omitempty" jsonschema:"Stable DSM identifier of the webhook provider"`
	Name      string `json:"name,omitempty" jsonschema:"Provider display name"`
	Kind      string `json:"kind,omitempty" jsonschema:"Provider type or service, when DSM reports one"`
	Enabled   *bool  `json:"enabled,omitempty" jsonschema:"Whether the provider is active; null when DSM does not report it"`
}

// WebhookState is the normalized webhook notification channel.
type WebhookState struct {
	Providers []WebhookProvider `json:"providers" jsonschema:"Configured webhook providers; empty when none"`
}

// SMSPhone is one configured SMS recipient phone number.
type SMSPhone struct {
	CountryCode string `json:"country_code,omitempty" jsonschema:"Phone country code"`
	Number      string `json:"number,omitempty" jsonschema:"Phone number"`
	Prefix      string `json:"prefix,omitempty" jsonschema:"Dialing prefix, when DSM reports one"`
}

// SMSProviderInfo is one entry of the SMS provider catalog: the provider
// identity and which parameters it requires. The credential-bearing send-URL
// template is never decoded.
type SMSProviderInfo struct {
	ID             string   `json:"id,omitempty" jsonschema:"Provider identifier"`
	Name           string   `json:"name,omitempty" jsonschema:"Provider display name"`
	Method         string   `json:"method,omitempty" jsonschema:"HTTP method the provider template uses"`
	RequiredParams []string `json:"required_params,omitempty" jsonschema:"Which credential parameters the provider requires, such as user or api_key; the values are never read"`
}

// SMSState is the normalized SMS notification channel. The provider account,
// API id/key, and template URL are auth material and are never decoded.
type SMSState struct {
	Enabled         bool              `json:"enabled" jsonschema:"Whether SMS notifications are enabled"`
	Provider        string            `json:"provider,omitempty" jsonschema:"Selected SMS provider name"`
	Phones          []SMSPhone        `json:"phones" jsonschema:"Configured recipient phone numbers; empty entries mean unset slots"`
	IntervalMinutes int               `json:"interval_minutes" jsonschema:"Minimum interval between SMS messages in minutes; 0 means unlimited"`
	Providers       []SMSProviderInfo `json:"providers,omitempty" jsonschema:"Available provider catalog; null when the provider API is unavailable"`
}

// RuleEvent is one event of the notification rule catalog.
type RuleEvent struct {
	Name        string `json:"name" jsonschema:"Stable DSM event key, such as StgMgrVolumeCrashedLocked"`
	Tag         string `json:"tag,omitempty" jsonschema:"DSM event tag; usually equal to name"`
	Title       string `json:"title,omitempty" jsonschema:"Localized event title as served by DSM"`
	Group       string `json:"group,omitempty" jsonschema:"Event group, such as Storage or System"`
	Level       string `json:"level,omitempty" jsonschema:"Normalized event severity: info, warn, or error"`
	Source      string `json:"source,omitempty" jsonschema:"Event source reported by DSM, such as dsm"`
	AppID       string `json:"app_id,omitempty" jsonschema:"DSM application the event belongs to"`
	Format      string `json:"format,omitempty" jsonschema:"Channel format DSM reported the entry under"`
	WarnPercent int    `json:"warn_percent,omitempty" jsonschema:"Warning threshold percent for threshold-based events"`
}

// RuleProfile is one notification profile and its event catalog. DSM 7.2+
// serves at least the built-in profile named All.
type RuleProfile struct {
	Name   string      `json:"name" jsonschema:"Profile name; All is the DSM built-in default"`
	Events []RuleEvent `json:"events" jsonschema:"Events DSM serves for this profile"`
}

// RulesState is the normalized notification event rule catalog.
type RulesState struct {
	Profiles []RuleProfile `json:"profiles" jsonschema:"Notification profiles and their event catalogs"`
}

// DesktopCategory is one per-category DSM desktop-notification toggle.
type DesktopCategory struct {
	Category string   `json:"category" jsonschema:"Notification category shown in DSM, such as Storage"`
	AppIDs   []string `json:"app_ids,omitempty" jsonschema:"DSM application ids the category covers"`
	Enabled  bool     `json:"enabled" jsonschema:"Whether desktop notifications are shown for this category"`
}

// DesktopState is the normalized per-category desktop notification settings of
// the signed-in DSM user.
type DesktopState struct {
	Categories []DesktopCategory `json:"categories" jsonschema:"Per-category desktop notification toggles"`
}

// HistoryEntry is one delivered notification from the DSM desktop feed (the
// bell). Title and Message are rendered from DSM's string templates with the
// entry's variables substituted; Key preserves the raw DSM notification key.
type HistoryEntry struct {
	ID       int64             `json:"id" jsonschema:"DSM notification id"`
	Time     string            `json:"time,omitempty" jsonschema:"Delivery time formatted in local time"`
	TimeUnix int64             `json:"time_unix" jsonschema:"Delivery time as a Unix time in seconds"`
	Level    string            `json:"level,omitempty" jsonschema:"Normalized severity: info, warn, or error"`
	Key      string            `json:"key" jsonschema:"Raw DSM notification key, such as StgMgrMountSSDROCacheFail"`
	Source   string            `json:"source,omitempty" jsonschema:"DSM application id that produced the notification"`
	Title    string            `json:"title,omitempty" jsonschema:"Rendered notification title; falls back to the raw key when no template is available"`
	Message  string            `json:"message,omitempty" jsonschema:"Rendered notification message with template variables substituted"`
	Vars     map[string]string `json:"vars,omitempty" jsonschema:"Raw template variables DSM attached to the notification"`
	HasMail  bool              `json:"has_mail" jsonschema:"Whether DSM kept a rendered mail body for this notification"`
}

// HistoryState is a page of the DSM notification history.
type HistoryState struct {
	Total      int            `json:"total" jsonschema:"Total notifications matching the query before pagination"`
	NewestTime int64          `json:"newest_time,omitempty" jsonschema:"Unix time of the newest notification in the feed, when DSM reports it"`
	Entries    []HistoryEntry `json:"entries" jsonschema:"Notifications for the requested page, newest first"`
}

// HistoryQuery selects and pages the DSM notification history. Level and the
// From/To range are applied by DSM server-side.
type HistoryQuery struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum entries to return; defaults to a bounded page size"`
	Offset int    `json:"offset,omitempty" jsonschema:"Number of newest entries to skip for pagination"`
	Level  string `json:"level,omitempty" jsonschema:"Severity filter applied by DSM: info, warn, or error"`
	From   int64  `json:"from,omitempty" jsonschema:"Inclusive lower bound as a Unix time in seconds"`
	To     int64  `json:"to,omitempty" jsonschema:"Inclusive upper bound as a Unix time in seconds"`
	Lang   string `json:"lang,omitempty" jsonschema:"DSM string-table language for rendered titles/messages, such as enu (default) or cht"`
}

// DSMLevel converts the query's normalized level to DSM's wire value. An empty
// level means no filter; an unknown level is an error so a typo cannot silently
// return the unfiltered feed.
func (q HistoryQuery) DSMLevel() (string, error) {
	switch strings.ToLower(strings.TrimSpace(q.Level)) {
	case "":
		return "", nil
	case LevelInfo, "information":
		return "NOTIFICATION_INFO", nil
	case LevelWarning, "warning":
		return "NOTIFICATION_WARN", nil
	case LevelError, "err":
		return "NOTIFICATION_ERROR", nil
	default:
		return "", fmt.Errorf("invalid level %q: use info, warn, or error", q.Level)
	}
}

// NormalizeLevel maps DSM's NOTIFICATION_* severities to the normalized
// info/warn/error vocabulary, passing unknown values through unchanged.
func NormalizeLevel(value string) string {
	switch strings.TrimSpace(value) {
	case "NOTIFICATION_INFO":
		return LevelInfo
	case "NOTIFICATION_WARN":
		return LevelWarning
	case "NOTIFICATION_ERROR":
		return LevelError
	default:
		return strings.TrimSpace(value)
	}
}

// Capabilities reports which notification read areas are currently exposed for
// a NAS. Each is independent: a NAS may expose the mail channel while the
// webhook API is absent.
type Capabilities struct {
	Mail    bool `json:"mail" jsonschema:"Whether the email channel configuration can be read"`
	Push    bool `json:"push" jsonschema:"Whether the push channel configuration can be read"`
	Webhook bool `json:"webhook" jsonschema:"Whether webhook providers can be read"`
	SMS     bool `json:"sms" jsonschema:"Whether the SMS channel configuration can be read"`
	Rules   bool `json:"rules" jsonschema:"Whether the event rule catalog can be read"`
	Desktop bool `json:"desktop" jsonschema:"Whether the per-category desktop notification toggles can be read"`
	History bool `json:"history" jsonschema:"Whether the notification history feed can be read"`
}
