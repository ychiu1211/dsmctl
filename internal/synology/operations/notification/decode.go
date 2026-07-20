package notification

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ychiu1211/dsmctl/internal/domain/notification"
)

// unmarshalObject rejects an empty or non-object payload before decoding, so a
// silently changed DSM response shape fails loudly instead of yielding a zero
// value.
func unmarshalObject(data json.RawMessage, what string, destination any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return fmt.Errorf("decode %s: empty response", what)
	}
	if trimmed[0] != '{' {
		return fmt.Errorf("decode %s: expected an object", what)
	}
	if err := json.Unmarshal(trimmed, destination); err != nil {
		return fmt.Errorf("decode %s: %w", what, err)
	}
	return nil
}

func decodeMailConf(data json.RawMessage) (mailConf, error) {
	var resp struct {
		EnableMail      *bool             `json:"enable_mail"`
		EnableOauth     *bool             `json:"enable_oauth"`
		SenderMail      *string           `json:"sender_mail"`
		SenderName      *string           `json:"sender_name"`
		SubjectPrefix   *string           `json:"subject_prefix"`
		SendWelcomeMail *bool             `json:"send_welcome_mail"`
		SMTPInfo        *json.RawMessage  `json:"smtp_info"`
		SMTPAuth        *json.RawMessage  `json:"smtp_auth"`
		Profiles        []json.RawMessage `json:"profiles"`
		Mail            []json.RawMessage `json:"mail"`
	}
	if err := unmarshalObject(data, "notification mail configuration", &resp); err != nil {
		return mailConf{}, err
	}
	if resp.EnableMail == nil {
		return mailConf{}, errors.New("decode notification mail configuration: required field \"enable_mail\" is missing")
	}
	conf := mailConf{
		Enabled:            *resp.EnableMail,
		OAuthEnabled:       resp.EnableOauth != nil && *resp.EnableOauth,
		SenderName:         strings.TrimSpace(deref(resp.SenderName)),
		SenderMail:         strings.TrimSpace(deref(resp.SenderMail)),
		SubjectPrefix:      strings.TrimSpace(deref(resp.SubjectPrefix)),
		WelcomeMailEnabled: resp.SendWelcomeMail != nil && *resp.SendWelcomeMail,
	}
	if resp.SMTPInfo != nil {
		var info struct {
			Server     *string `json:"server"`
			Port       *int    `json:"port"`
			SSL        *bool   `json:"ssl"`
			VerifyCert *bool   `json:"verifyCert"`
		}
		if err := unmarshalObject(*resp.SMTPInfo, "notification SMTP info", &info); err != nil {
			return mailConf{}, err
		}
		conf.SMTP.Server = strings.TrimSpace(deref(info.Server))
		if info.Port != nil {
			conf.SMTP.Port = *info.Port
		}
		conf.SMTP.SSL = info.SSL != nil && *info.SSL
		conf.SMTP.VerifyCert = info.VerifyCert != nil && *info.VerifyCert
	}
	if resp.SMTPAuth != nil {
		// Only the auth toggle and username are read; DSM's get does not
		// return the password and this decoder must never learn one.
		var auth struct {
			Enable *bool   `json:"enable"`
			User   *string `json:"user"`
		}
		if err := unmarshalObject(*resp.SMTPAuth, "notification SMTP auth", &auth); err != nil {
			return mailConf{}, err
		}
		conf.SMTP.AuthEnabled = auth.Enable != nil && *auth.Enable
		conf.SMTP.AuthUser = strings.TrimSpace(deref(auth.User))
	}
	recipients := decodeRecipients(resp.Profiles)
	recipients = append(recipients, decodeRecipients(resp.Mail)...)
	conf.Recipients = recipients
	return conf, nil
}

// decodeRecipients tolerantly decodes a recipient list whose entries DSM
// serves either as plain address strings or as objects (recipient profiles).
// The lab used to model this has none configured, so unknown extras are
// ignored rather than guessed at.
func decodeRecipients(entries []json.RawMessage) []notification.MailRecipient {
	recipients := make([]notification.MailRecipient, 0, len(entries))
	for _, raw := range entries {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 {
			continue
		}
		if trimmed[0] == '"' {
			var address string
			if err := json.Unmarshal(trimmed, &address); err == nil {
				if address = strings.TrimSpace(address); address != "" {
					recipients = append(recipients, notification.MailRecipient{Address: address})
				}
			}
			continue
		}
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &entry); err != nil {
			continue
		}
		recipient := notification.MailRecipient{
			Address: firstString(entry, "mail", "email", "address", "recipient"),
			Name:    firstString(entry, "name", "profile_name", "username"),
		}
		if recipient.Address != "" || recipient.Name != "" {
			recipients = append(recipients, recipient)
		}
	}
	return recipients
}

func decodeRelayMailConf(data json.RawMessage) (relayMailConf, error) {
	var resp struct {
		EnableMail    *bool             `json:"enable_mail"`
		Mail          []json.RawMessage `json:"mail"`
		SubjectPrefix *string           `json:"subject_prefix"`
	}
	if err := unmarshalObject(data, "notification relay mail configuration", &resp); err != nil {
		return relayMailConf{}, err
	}
	if resp.EnableMail == nil {
		return relayMailConf{}, errors.New("decode notification relay mail configuration: required field \"enable_mail\" is missing")
	}
	return relayMailConf{
		Enabled:       *resp.EnableMail,
		Recipients:    decodeRecipients(resp.Mail),
		SubjectPrefix: strings.TrimSpace(deref(resp.SubjectPrefix)),
	}, nil
}

func decodePushConf(data json.RawMessage) (pushConf, error) {
	// The response also carries dead legacy MSN/Skype fields; only the mobile
	// toggle is meaningful on supported DSM releases.
	var resp struct {
		MobileEnable *bool `json:"mobile_enable"`
	}
	if err := unmarshalObject(data, "notification push configuration", &resp); err != nil {
		return pushConf{}, err
	}
	if resp.MobileEnable == nil {
		return pushConf{}, errors.New("decode notification push configuration: required field \"mobile_enable\" is missing")
	}
	return pushConf{MobileEnabled: *resp.MobileEnable}, nil
}

// decodePushDevices decodes the paired push-target list tolerantly: the lab
// used to model this has no paired device, so only fields DSM actually returns
// are populated. Per-device push tokens are never decoded.
func decodePushDevices(data json.RawMessage) ([]notification.PushDevice, error) {
	var resp struct {
		List *[]map[string]json.RawMessage `json:"list"`
	}
	if err := unmarshalObject(data, "notification push devices", &resp); err != nil {
		return nil, err
	}
	if resp.List == nil {
		return nil, errors.New("decode notification push devices: required field \"list\" is missing")
	}
	devices := make([]notification.PushDevice, 0, len(*resp.List))
	for _, entry := range *resp.List {
		device := notification.PushDevice{
			TargetID: firstStringish(entry, "target_id", "id"),
			Name:     firstString(entry, "name", "device_name", "nickname"),
			Model:    firstString(entry, "model", "device_model"),
			LastSeen: firstStringish(entry, "last_login_time", "last_seen", "login_time"),
		}
		// DSM's UI distinguishes a mobile-app pairing from a browser
		// registration by the presence of an app version.
		if firstStringish(entry, "app_version") != "" {
			device.Kind = "device"
		} else {
			device.Kind = "browser"
		}
		devices = append(devices, device)
	}
	return devices, nil
}

// decodeWebhookProviders decodes the webhook provider list tolerantly. The
// webhook URL and any token/secret field are deliberately never read.
func decodeWebhookProviders(data json.RawMessage) ([]notification.WebhookProvider, error) {
	var resp struct {
		List *[]map[string]json.RawMessage `json:"list"`
	}
	if err := unmarshalObject(data, "notification webhook providers", &resp); err != nil {
		return nil, err
	}
	if resp.List == nil {
		return nil, errors.New("decode notification webhook providers: required field \"list\" is missing")
	}
	providers := make([]notification.WebhookProvider, 0, len(*resp.List))
	for _, entry := range *resp.List {
		provider := notification.WebhookProvider{
			ProfileID: firstStringish(entry, "profile_id", "id"),
			Name:      firstString(entry, "name", "provider_name", "profile_name"),
			Kind:      firstString(entry, "type", "provider_type", "service"),
		}
		if raw, ok := entry["enable"]; ok {
			var enabled bool
			if err := json.Unmarshal(raw, &enabled); err == nil {
				provider.Enabled = &enabled
			}
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

func decodeSMSConf(data json.RawMessage) (smsConf, error) {
	// api_id and user are provider auth material and are deliberately not
	// decoded.
	var resp struct {
		EnableSMS    *bool   `json:"enable_sms"`
		ProviderName *string `json:"provider_name"`
		MsgInterval  *int    `json:"msg_interval"`
		PhoneInfo    []struct {
			Code   *string `json:"code"`
			Num    *string `json:"num"`
			Prefix *string `json:"prefix"`
		} `json:"phone_info"`
	}
	if err := unmarshalObject(data, "notification SMS configuration", &resp); err != nil {
		return smsConf{}, err
	}
	if resp.EnableSMS == nil {
		return smsConf{}, errors.New("decode notification SMS configuration: required field \"enable_sms\" is missing")
	}
	conf := smsConf{
		Enabled:  *resp.EnableSMS,
		Provider: strings.TrimSpace(deref(resp.ProviderName)),
	}
	if resp.MsgInterval != nil {
		conf.IntervalMinutes = *resp.MsgInterval
	}
	for _, phone := range resp.PhoneInfo {
		entry := notification.SMSPhone{
			CountryCode: strings.TrimSpace(deref(phone.Code)),
			Number:      strings.TrimSpace(deref(phone.Num)),
			Prefix:      strings.TrimSpace(deref(phone.Prefix)),
		}
		if entry.CountryCode != "" || entry.Number != "" || entry.Prefix != "" {
			conf.Phones = append(conf.Phones, entry)
		}
	}
	return conf, nil
}

// decodeSMSProviders reduces the provider catalog to identity, method, and
// which credential parameters each provider requires. The send-URL template
// and request header/body patterns may embed user credentials on custom
// providers and are never decoded.
func decodeSMSProviders(data json.RawMessage) ([]notification.SMSProviderInfo, error) {
	var resp struct {
		ProviderInfo *[]struct {
			ProviderID   *string          `json:"provider_id"`
			ProviderName *string          `json:"provider_name"`
			ReqMethod    *string          `json:"req_method"`
			ParamUsed    map[string]*bool `json:"param_used"`
		} `json:"provider_info"`
	}
	if err := unmarshalObject(data, "notification SMS providers", &resp); err != nil {
		return nil, err
	}
	if resp.ProviderInfo == nil {
		return nil, errors.New("decode notification SMS providers: required field \"provider_info\" is missing")
	}
	providers := make([]notification.SMSProviderInfo, 0, len(*resp.ProviderInfo))
	for _, entry := range *resp.ProviderInfo {
		provider := notification.SMSProviderInfo{
			ID:     strings.TrimSpace(deref(entry.ProviderID)),
			Name:   strings.TrimSpace(deref(entry.ProviderName)),
			Method: strings.TrimSpace(deref(entry.ReqMethod)),
		}
		for name, used := range entry.ParamUsed {
			if used != nil && *used {
				provider.RequiredParams = append(provider.RequiredParams, name)
			}
		}
		sort.Strings(provider.RequiredParams)
		providers = append(providers, provider)
	}
	return providers, nil
}

// decodeRules decodes the FilterSettings catalog: an object keyed by profile
// name (the DSM built-in default is "All"), each value an array of events.
func decodeRules(data json.RawMessage) (notification.RulesState, error) {
	var raw map[string]json.RawMessage
	if err := unmarshalObject(data, "notification rule catalog", &raw); err != nil {
		return notification.RulesState{}, err
	}
	profiles := make([]notification.RuleProfile, 0, len(raw))
	for name, value := range raw {
		trimmed := bytes.TrimSpace(value)
		if len(trimmed) == 0 || trimmed[0] != '[' {
			continue
		}
		var entries []map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &entries); err != nil {
			return notification.RulesState{}, fmt.Errorf("decode notification rule catalog: profile %q: %w", name, err)
		}
		events := make([]notification.RuleEvent, 0, len(entries))
		for _, entry := range entries {
			event := notification.RuleEvent{
				Name:        firstString(entry, "name"),
				Tag:         firstString(entry, "tag"),
				Title:       firstString(entry, "title"),
				Group:       firstString(entry, "group"),
				Level:       notification.NormalizeLevel(firstString(entry, "level")),
				Source:      firstString(entry, "source"),
				AppID:       firstString(entry, "appid"),
				Format:      firstString(entry, "format"),
				WarnPercent: firstInt(entry, "warnPercent"),
			}
			if event.Name == "" && event.Tag == "" {
				return notification.RulesState{}, fmt.Errorf("decode notification rule catalog: profile %q has an event without name or tag", name)
			}
			events = append(events, event)
		}
		profiles = append(profiles, notification.RuleProfile{Name: name, Events: events})
	}
	if len(profiles) == 0 {
		return notification.RulesState{}, errors.New("decode notification rule catalog: no profile array found in the response")
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return notification.RulesState{Profiles: profiles}, nil
}

func decodeDesktop(data json.RawMessage) (notification.DesktopState, error) {
	var resp struct {
		Items *[]struct {
			Category  *string  `json:"category"`
			AppIDs    []string `json:"appids"`
			DSMNotify *string  `json:"dsmnotify"`
		} `json:"items"`
	}
	if err := unmarshalObject(data, "desktop notification settings", &resp); err != nil {
		return notification.DesktopState{}, err
	}
	if resp.Items == nil {
		return notification.DesktopState{}, errors.New("decode desktop notification settings: required field \"items\" is missing")
	}
	categories := make([]notification.DesktopCategory, 0, len(*resp.Items))
	for _, item := range *resp.Items {
		categories = append(categories, notification.DesktopCategory{
			Category: strings.TrimSpace(deref(item.Category)),
			AppIDs:   item.AppIDs,
			Enabled:  strings.EqualFold(strings.TrimSpace(deref(item.DSMNotify)), "on"),
		})
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i].Category < categories[j].Category })
	return notification.DesktopState{Categories: categories}, nil
}

func decodeHistoryPage(data json.RawMessage) (historyPage, error) {
	var resp struct {
		Total         *int              `json:"total"`
		NewestMsgTime *int64            `json:"newestMsgTime"`
		Items         []json.RawMessage `json:"items"`
	}
	if err := unmarshalObject(data, "notification history", &resp); err != nil {
		return historyPage{}, err
	}
	if resp.Total == nil || resp.Items == nil {
		return historyPage{}, errors.New("decode notification history: required fields \"total\" and \"items\" are missing")
	}
	page := historyPage{Total: *resp.Total}
	if resp.NewestMsgTime != nil {
		page.NewestTime = *resp.NewestMsgTime
	}
	for _, raw := range resp.Items {
		var entry struct {
			NotifyID  *int64   `json:"notifyId"`
			Time      *int64   `json:"time"`
			Level     *string  `json:"level"`
			Title     *string  `json:"title"`
			ClassName *string  `json:"className"`
			HasMail   *bool    `json:"hasMail"`
			Msg       []string `json:"msg"`
		}
		if err := unmarshalObject(raw, "notification history entry", &entry); err != nil {
			return historyPage{}, err
		}
		if entry.NotifyID == nil || entry.Title == nil {
			return historyPage{}, errors.New("decode notification history entry: required fields \"notifyId\" and \"title\" are missing")
		}
		item := historyItem{
			ID:      *entry.NotifyID,
			Key:     strings.TrimSpace(deref(entry.Title)),
			Level:   notification.NormalizeLevel(deref(entry.Level)),
			Source:  strings.TrimSpace(deref(entry.ClassName)),
			HasMail: entry.HasMail != nil && *entry.HasMail,
		}
		if entry.Time != nil {
			item.Time = *entry.Time
		}
		item.Vars = decodeHistoryVars(entry.Msg)
		page.Items = append(page.Items, item)
	}
	return page, nil
}

// decodeHistoryVars merges the entry's msg elements, each a JSON-encoded map
// of %VAR% placeholders to values. A non-JSON element is kept whole under the
// empty key as literal message text.
func decodeHistoryVars(elements []string) map[string]string {
	vars := map[string]string{}
	for _, element := range elements {
		trimmed := strings.TrimSpace(element)
		if trimmed == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
			vars[""] = trimmed
			continue
		}
		for key, value := range raw {
			switch typed := value.(type) {
			case string:
				vars[key] = typed
			case float64:
				vars[key] = strconv.FormatFloat(typed, 'f', -1, 64)
			case bool:
				vars[key] = strconv.FormatBool(typed)
			}
		}
	}
	if len(vars) == 0 {
		return nil
	}
	return vars
}

func decodeStringTable(data json.RawMessage) (map[string]stringTemplates, error) {
	var raw map[string]json.RawMessage
	if err := unmarshalObject(data, "notification string table", &raw); err != nil {
		return nil, err
	}
	table := make(map[string]stringTemplates, len(raw))
	for key, value := range raw {
		var entry stringTemplates
		if err := json.Unmarshal(value, &entry); err != nil {
			continue
		}
		table[key] = entry
	}
	if len(table) == 0 {
		return nil, errors.New("decode notification string table: no template entries found")
	}
	return table, nil
}

// renderHistory converts the decoded page into the domain state, rendering
// each entry's title and message from its string templates when available.
func renderHistory(page historyPage, templates map[string]stringTemplates) notification.HistoryState {
	state := notification.HistoryState{
		Total:      page.Total,
		NewestTime: page.NewestTime,
		Entries:    make([]notification.HistoryEntry, 0, len(page.Items)),
	}
	for _, item := range page.Items {
		entry := notification.HistoryEntry{
			ID:       item.ID,
			TimeUnix: item.Time,
			Level:    item.Level,
			Key:      item.Key,
			Source:   item.Source,
			HasMail:  item.HasMail,
			Title:    item.Key,
		}
		if item.Time > 0 {
			entry.Time = time.Unix(item.Time, 0).Format("2006/01/02 15:04:05")
		}
		vars := map[string]string{}
		for key, value := range item.Vars {
			if key != "" {
				vars[key] = value
			}
		}
		if len(vars) > 0 {
			entry.Vars = vars
		}
		if template, ok := templates[item.Key]; ok {
			if title := renderTemplate(template.Title, vars); title != "" {
				entry.Title = title
			}
			entry.Message = renderTemplate(template.Msg, vars)
		}
		if entry.Message == "" {
			// A non-templated notification carries its literal text in msg.
			entry.Message = strings.TrimSpace(item.Vars[""])
		}
		state.Entries = append(state.Entries, entry)
	}
	return state
}

// renderTemplate substitutes %VAR% placeholders (the vars keys carry their %
// delimiters) into a DSM string template. Two passes cover one level of
// nesting (a variable value that itself contains another placeholder);
// unresolved table:section:key indirections are left as-is rather than
// guessed. DSM templates embed <br> line breaks, which become newlines.
func renderTemplate(template string, vars map[string]string) string {
	if template == "" {
		return ""
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rendered := template
	for pass := 0; pass < 2; pass++ {
		replaced := false
		for _, key := range keys {
			if strings.Contains(rendered, key) {
				rendered = strings.ReplaceAll(rendered, key, vars[key])
				replaced = true
			}
		}
		if !replaced {
			break
		}
	}
	for _, lineBreak := range []string{"<br/>", "<br />", "<br>"} {
		rendered = strings.ReplaceAll(rendered, lineBreak, "\n")
	}
	return strings.TrimSpace(rendered)
}

func firstString(entry map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw, ok := entry[key]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// firstStringish reads a field DSM serves as either a string or a number,
// normalizing a number to its decimal form.
func firstStringish(entry map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw, ok := entry[key]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		var asString string
		if err := json.Unmarshal(raw, &asString); err == nil {
			if trimmed := strings.TrimSpace(asString); trimmed != "" {
				return trimmed
			}
			continue
		}
		var asNumber json.Number
		if err := json.Unmarshal(raw, &asNumber); err == nil {
			return asNumber.String()
		}
	}
	return ""
}

func firstInt(entry map[string]json.RawMessage, keys ...string) int {
	for _, key := range keys {
		raw, ok := entry[key]
		if !ok {
			continue
		}
		var value int
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return 0
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
