package application

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/domain/notification"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type NotificationCapabilitiesResult struct {
	NAS          string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.NotificationCapabilities `json:"capabilities" jsonschema:"Notification read areas currently exposed by dsmctl"`
	Report       synology.CompatibilityReport      `json:"report" jsonschema:"Discovered APIs and selected notification compatibility backends"`
}

type NotificationMailResult struct {
	NAS  string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Mail synology.NotificationMailState `json:"mail" jsonschema:"Normalized email notification channel without any password material"`
}

type NotificationPushResult struct {
	NAS  string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Push synology.NotificationPushState `json:"push" jsonschema:"Normalized push notification channel without any device tokens"`
}

type NotificationWebhookResult struct {
	NAS     string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Webhook synology.NotificationWebhookState `json:"webhook" jsonschema:"Configured webhook providers without URLs or secrets"`
}

type NotificationSMSResult struct {
	NAS string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	SMS synology.NotificationSMSState `json:"sms" jsonschema:"Normalized SMS notification channel without provider auth material"`
}

type NotificationRulesResult struct {
	NAS   string                          `json:"nas" jsonschema:"NAS profile used for the request"`
	Rules synology.NotificationRulesState `json:"rules" jsonschema:"Notification event rule catalog per profile"`
}

type NotificationDesktopResult struct {
	NAS     string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Desktop synology.NotificationDesktopState `json:"desktop" jsonschema:"Per-category desktop notification toggles of the signed-in user"`
}

type NotificationHistoryResult struct {
	NAS     string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	History synology.NotificationHistoryState `json:"history" jsonschema:"One page of the DSM notification history, newest first"`
}

func (s *Service) GetNotificationCapabilities(ctx context.Context, requestedNAS string) (NotificationCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return NotificationCapabilitiesResult{}, err
	}
	capabilities, report, err := client.NotificationCapabilities(ctx)
	if err != nil {
		return NotificationCapabilitiesResult{}, authenticationError(name, err)
	}
	return NotificationCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetNotificationMail(ctx context.Context, requestedNAS string) (NotificationMailResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return NotificationMailResult{}, err
	}
	state, err := client.NotificationMailState(ctx)
	if err != nil {
		return NotificationMailResult{}, authenticationError(name, err)
	}
	return NotificationMailResult{NAS: name, Mail: state}, nil
}

func (s *Service) GetNotificationPush(ctx context.Context, requestedNAS string) (NotificationPushResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return NotificationPushResult{}, err
	}
	state, err := client.NotificationPushState(ctx)
	if err != nil {
		return NotificationPushResult{}, authenticationError(name, err)
	}
	return NotificationPushResult{NAS: name, Push: state}, nil
}

func (s *Service) GetNotificationWebhook(ctx context.Context, requestedNAS string) (NotificationWebhookResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return NotificationWebhookResult{}, err
	}
	state, err := client.NotificationWebhookState(ctx)
	if err != nil {
		return NotificationWebhookResult{}, authenticationError(name, err)
	}
	return NotificationWebhookResult{NAS: name, Webhook: state}, nil
}

func (s *Service) GetNotificationSMS(ctx context.Context, requestedNAS string) (NotificationSMSResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return NotificationSMSResult{}, err
	}
	state, err := client.NotificationSMSState(ctx)
	if err != nil {
		return NotificationSMSResult{}, authenticationError(name, err)
	}
	return NotificationSMSResult{NAS: name, SMS: state}, nil
}

func (s *Service) GetNotificationRules(ctx context.Context, requestedNAS string) (NotificationRulesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return NotificationRulesResult{}, err
	}
	state, err := client.NotificationRulesState(ctx)
	if err != nil {
		return NotificationRulesResult{}, authenticationError(name, err)
	}
	return NotificationRulesResult{NAS: name, Rules: state}, nil
}

func (s *Service) GetNotificationDesktop(ctx context.Context, requestedNAS string) (NotificationDesktopResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return NotificationDesktopResult{}, err
	}
	state, err := client.NotificationDesktopState(ctx)
	if err != nil {
		return NotificationDesktopResult{}, authenticationError(name, err)
	}
	return NotificationDesktopResult{NAS: name, Desktop: state}, nil
}

func (s *Service) GetNotificationHistory(ctx context.Context, requestedNAS string, query notification.HistoryQuery) (NotificationHistoryResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return NotificationHistoryResult{}, err
	}
	state, err := client.NotificationHistory(ctx, query)
	if err != nil {
		return NotificationHistoryResult{}, authenticationError(name, err)
	}
	return NotificationHistoryResult{NAS: name, History: state}, nil
}
