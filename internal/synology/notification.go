package synology

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/notification"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	notificationops "github.com/ychiu1211/dsmctl/internal/synology/operations/notification"
)

type NotificationMailState = notification.MailState
type NotificationPushState = notification.PushState
type NotificationWebhookState = notification.WebhookState
type NotificationSMSState = notification.SMSState
type NotificationRulesState = notification.RulesState
type NotificationDesktopState = notification.DesktopState
type NotificationHistoryState = notification.HistoryState
type NotificationHistoryQuery = notification.HistoryQuery
type NotificationCapabilities = notification.Capabilities

const (
	defaultNotificationHistoryLimit = 30
	maxNotificationHistoryLimit     = 1000
)

// NotificationMailState reads the email notification channel (SMTP plus the
// Synology-relay mode when available). The SMTP password is never decoded.
func (c *Client) NotificationMailState(ctx context.Context) (NotificationMailState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, notificationops.APINames()...); err != nil {
		return NotificationMailState{}, fmt.Errorf("prepare notification mail target: %w", err)
	}
	state, _, err := notificationops.ReadMail(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return NotificationMailState{}, fmt.Errorf("get notification mail configuration: %w", err)
	}
	c.target.AddCapability(notificationops.MailReadCapabilityName)
	return state, nil
}

// NotificationPushState reads the push notification channel: the mobile toggle
// and the paired push targets. Push tokens are never decoded.
func (c *Client) NotificationPushState(ctx context.Context) (NotificationPushState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, notificationops.APINames()...); err != nil {
		return NotificationPushState{}, fmt.Errorf("prepare notification push target: %w", err)
	}
	state, _, err := notificationops.ReadPush(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return NotificationPushState{}, fmt.Errorf("get notification push configuration: %w", err)
	}
	c.target.AddCapability(notificationops.PushReadCapabilityName)
	return state, nil
}

// NotificationWebhookState reads the configured webhook notification
// providers. Webhook URLs and secrets are never decoded.
func (c *Client) NotificationWebhookState(ctx context.Context) (NotificationWebhookState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, notificationops.APINames()...); err != nil {
		return NotificationWebhookState{}, fmt.Errorf("prepare notification webhook target: %w", err)
	}
	state, _, err := notificationops.ReadWebhook(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return NotificationWebhookState{}, fmt.Errorf("get notification webhook providers: %w", err)
	}
	c.target.AddCapability(notificationops.WebhookReadCapabilityName)
	return state, nil
}

// NotificationSMSState reads the SMS notification channel and the provider
// catalog. Provider auth material is never decoded.
func (c *Client) NotificationSMSState(ctx context.Context) (NotificationSMSState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, notificationops.APINames()...); err != nil {
		return NotificationSMSState{}, fmt.Errorf("prepare notification SMS target: %w", err)
	}
	state, _, err := notificationops.ReadSMS(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return NotificationSMSState{}, fmt.Errorf("get notification SMS configuration: %w", err)
	}
	c.target.AddCapability(notificationops.SMSReadCapabilityName)
	return state, nil
}

// NotificationRulesState reads the notification event rule catalog.
func (c *Client) NotificationRulesState(ctx context.Context) (NotificationRulesState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, notificationops.APINames()...); err != nil {
		return NotificationRulesState{}, fmt.Errorf("prepare notification rules target: %w", err)
	}
	state, _, err := notificationops.ReadRules(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return NotificationRulesState{}, fmt.Errorf("get notification rule catalog: %w", err)
	}
	c.target.AddCapability(notificationops.RulesReadCapabilityName)
	return state, nil
}

// NotificationDesktopState reads the per-category desktop notification
// toggles of the signed-in DSM user.
func (c *Client) NotificationDesktopState(ctx context.Context) (NotificationDesktopState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, notificationops.APINames()...); err != nil {
		return NotificationDesktopState{}, fmt.Errorf("prepare desktop notification target: %w", err)
	}
	state, _, err := notificationops.ReadDesktop(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return NotificationDesktopState{}, fmt.Errorf("get desktop notification settings: %w", err)
	}
	c.target.AddCapability(notificationops.DesktopReadCapabilityName)
	return state, nil
}

// NotificationHistory reads one page of the DSM notification history (the
// desktop bell feed), newest first. Level and the time range are applied by
// DSM; titles and messages are rendered from DSM's string templates.
func (c *Client) NotificationHistory(ctx context.Context, query NotificationHistoryQuery) (NotificationHistoryState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	level, err := query.DSMLevel()
	if err != nil {
		return NotificationHistoryState{}, err
	}
	if err := c.prepareCompatibilityTargetLocked(ctx, notificationops.APINames()...); err != nil {
		return NotificationHistoryState{}, fmt.Errorf("prepare notification history target: %w", err)
	}
	limit := query.Limit
	if limit <= 0 {
		limit = defaultNotificationHistoryLimit
	}
	if limit > maxNotificationHistoryLimit {
		limit = maxNotificationHistoryLimit
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	state, _, err := notificationops.ReadHistory(ctx, c.target, lockedExecutor{client: c}, notificationops.HistoryInput{
		Offset: offset, Limit: limit, Level: level,
		DateFrom: query.From, DateTo: query.To,
		Lang: strings.TrimSpace(query.Lang),
	})
	if err != nil {
		return NotificationHistoryState{}, fmt.Errorf("get notification history: %w", err)
	}
	c.target.AddCapability(notificationops.HistoryReadCapabilityName)
	return state, nil
}

// NotificationCapabilities reports which notification read areas this NAS
// exposes, each selected independently so one missing API does not disable
// the others.
func (c *Client) NotificationCapabilities(ctx context.Context) (NotificationCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, notificationops.APINames()...); err != nil {
		return NotificationCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare notification capabilities target: %w", err)
	}
	selectors := []struct {
		selectArea func(compatibility.Target) (compatibility.Selection, error)
		capability string
	}{
		{notificationops.SelectMail, notificationops.MailReadCapabilityName},
		{notificationops.SelectPush, notificationops.PushReadCapabilityName},
		{notificationops.SelectWebhook, notificationops.WebhookReadCapabilityName},
		{notificationops.SelectSMS, notificationops.SMSReadCapabilityName},
		{notificationops.SelectRules, notificationops.RulesReadCapabilityName},
		{notificationops.SelectDesktop, notificationops.DesktopReadCapabilityName},
		{notificationops.SelectHistory, notificationops.HistoryReadCapabilityName},
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selector := range selectors {
		selection, err := selector.selectArea(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return NotificationCapabilities{}, CompatibilityReport{}, fmt.Errorf("select notification backend: %w", err)
		}
		if selection.Supported {
			c.target.AddCapability(selector.capability)
		}
		selections = append(selections, selection)
	}
	capabilities := NotificationCapabilities{
		Mail:    selections[0].Supported,
		Push:    selections[1].Supported,
		Webhook: selections[2].Supported,
		SMS:     selections[3].Supported,
		Rules:   selections[4].Supported,
		Desktop: selections[5].Supported,
		History: selections[6].Supported,
	}
	return capabilities, c.target.Report(selections...), nil
}
