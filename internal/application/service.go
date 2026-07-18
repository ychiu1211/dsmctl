package application

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/domain/resmon"
	"github.com/ychiu1211/dsmctl/internal/domain/syslog"
	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
	"github.com/ychiu1211/dsmctl/internal/runtime"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type Service struct {
	config           *config.Config
	configSource     config.Source
	manager          *runtime.Manager
	secretReferences credentials.ReferenceResolver
	credentialStore  CredentialStore
	remoteApply      RemoteApplyAuthorizer
}

type SystemInfoResult struct {
	NAS    string              `json:"nas" jsonschema:"NAS profile used for the request"`
	System synology.SystemInfo `json:"system" jsonschema:"System information returned by DSM"`
}

type AuthenticationResult struct {
	NAS string `json:"nas"`
}

type CompatibilityResult struct {
	NAS    string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Report synology.CompatibilityReport `json:"report" jsonschema:"Discovered DSM compatibility target and selected operation backends"`
}

type ControlPanelTimeStateResult struct {
	NAS  string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Time synology.ControlPanelTimeState `json:"time" jsonschema:"Normalized Control Panel time and NTP configuration"`
}

type ControlPanelTimeCapabilitiesResult struct {
	NAS          string                                `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.ControlPanelTimeCapabilities `json:"capabilities" jsonschema:"Control Panel time module operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport          `json:"report" jsonschema:"Discovered API and selected time-module compatibility backend"`
}

type StorageStateResult struct {
	NAS     string                `json:"nas" jsonschema:"NAS profile used for the request"`
	Storage synology.StorageState `json:"storage" jsonschema:"Normalized disk, storage-pool, and volume inventory"`
}

type StorageCapabilitiesResult struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.StorageCapabilities `json:"capabilities" jsonschema:"Storage operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected storage compatibility backend"`
}

type IdentityStateResult struct {
	NAS      string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Identity synology.IdentityState `json:"identity" jsonschema:"Normalized local user and group inventory"`
}

type IdentityCapabilitiesResult struct {
	NAS          string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.IdentityCapabilities `json:"capabilities" jsonschema:"Identity management operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport  `json:"report" jsonschema:"Discovered APIs and selected identity compatibility backends"`
}

type ShareStateResult struct {
	NAS    string              `json:"nas" jsonschema:"NAS profile used for the request"`
	Shares synology.ShareState `json:"shares" jsonschema:"Normalized shared-folder inventory and optional permissions"`
}

type ShareCapabilitiesResult struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.ShareCapabilities   `json:"capabilities" jsonschema:"Shared-folder operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected shared-folder compatibility backends"`
}

type SANStateResult struct {
	NAS string            `json:"nas" jsonschema:"NAS profile used for the request"`
	SAN synology.SANState `json:"san" jsonschema:"Normalized iSCSI target, LUN, and mapping inventory"`
}

type SANCapabilitiesResult struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.SANCapabilities     `json:"capabilities" jsonschema:"SAN inventory and management operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected SAN compatibility backends"`
}

type LogStateResult struct {
	NAS  string            `json:"nas" jsonschema:"NAS profile used for the request"`
	Logs synology.LogState `json:"logs" jsonschema:"Normalized DSM system log entries and severity counts"`
}

type LogCapabilitiesResult struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.LogCapabilities     `json:"capabilities" jsonschema:"DSM log read operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected log compatibility backend"`
}

type ResourceMonitorStateResult struct {
	NAS         string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Utilization synology.ResourceUtilization `json:"utilization" jsonschema:"Current normalized resource utilization snapshot"`
}

type ResourceMonitorHistoryResult struct {
	NAS     string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	History synology.ResourceHistory `json:"history" jsonschema:"Recorded utilization history series"`
}

type ResourceRecordingSettingResult struct {
	NAS     string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Setting synology.ResourceRecordingSetting `json:"setting" jsonschema:"History-recording setting reported by DSM"`
}

type ResourceMonitorCapabilitiesResult struct {
	NAS          string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.ResourceMonitorCapabilities `json:"capabilities" jsonschema:"Resource Monitor operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport         `json:"report" jsonschema:"Discovered APIs and selected Resource Monitor compatibility backends"`
}

type ServiceOption func(*Service)

// RemoteApplyAuthorizer is the mandatory second authorization boundary for a
// request carrying a remote gateway principal. Local CLI and stdio contexts do
// not carry such a principal and retain their existing behavior.
type RemoteApplyAuthorizer interface {
	AdmitRemoteApply(ctx context.Context, tokenID, nas string, profileRevision uint64, planHash, risk string) error
}

func WithRemoteApplyAuthorizer(authorizer RemoteApplyAuthorizer) ServiceOption {
	return func(service *Service) { service.remoteApply = authorizer }
}

func WithSecretReferenceResolver(resolver credentials.ReferenceResolver) ServiceOption {
	return func(service *Service) {
		if resolver != nil {
			service.secretReferences = resolver
		}
	}
}

func WithConfigSource(source config.Source) ServiceOption {
	return func(service *Service) {
		if source != nil {
			service.configSource = source
		}
	}
}

func NewService(cfg *config.Config, manager *runtime.Manager, options ...ServiceOption) *Service {
	service := &Service{
		config:           cfg,
		configSource:     config.StaticSource{Config: cfg},
		manager:          manager,
		secretReferences: credentials.NewEnvironmentReferenceResolver(),
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) ListNAS() []config.Summary {
	summaries, _ := s.ListNASContext(context.Background())
	return summaries
}

func (s *Service) ListNASContext(ctx context.Context) ([]config.Summary, error) {
	cfg, err := s.configSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return filterRemoteSummaries(ctx, cfg.Summaries(credentials.DefaultEnvironmentVariable)), nil
}

func filterRemoteSummaries(ctx context.Context, summaries []config.Summary) []config.Summary {
	principal, remote := remotepolicy.PrincipalFromContext(ctx)
	if !remote {
		return summaries
	}
	filtered := make([]config.Summary, 0, len(summaries))
	for _, summary := range summaries {
		if principal.AllowsNAS(summary.Name) {
			filtered = append(filtered, summary)
		}
	}
	return filtered
}

// AuthorizeRemoteTarget resolves an omitted NAS exactly as the application
// would, then applies the caller's explicit allowlist without revealing which
// hidden profile (if any) caused the denial.
func (s *Service) AuthorizeRemoteTarget(ctx context.Context, requested string) (string, error) {
	principal, remote := remotepolicy.PrincipalFromContext(ctx)
	if !remote {
		return requested, nil
	}
	cfg, err := s.configSnapshot(ctx)
	if err != nil {
		return "", remotepolicy.ErrDenied
	}
	name, _, err := cfg.Resolve(requested)
	if err != nil || !principal.AllowsNAS(name) {
		return "", remotepolicy.ErrDenied
	}
	return name, nil
}

func (s *Service) authorizeRemoteApply(ctx context.Context, nas string, revision uint64, hash, risk string) error {
	principal, remote := remotepolicy.PrincipalFromContext(ctx)
	if !remote {
		return nil
	}
	if s.remoteApply == nil || !principal.HasScope(remotepolicy.ScopeApply) || !principal.AllowsNAS(nas) {
		return remotepolicy.ErrDenied
	}
	return s.remoteApply.AdmitRemoteApply(ctx, principal.TokenID, nas, revision, hash, risk)
}

func (s *Service) configSnapshot(ctx context.Context) (*config.Config, error) {
	if s.configSource == nil {
		if s.config == nil {
			return nil, fmt.Errorf("config is nil")
		}
		return s.config, nil
	}
	cfg, err := s.configSource.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("load NAS profiles: %w", err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("profile source returned a nil config")
	}
	return cfg, nil
}

func (s *Service) profileRevision(ctx context.Context, name string) (uint64, error) {
	cfg, err := s.configSnapshot(ctx)
	if err != nil {
		return 0, err
	}
	profile, ok := cfg.NAS[name]
	if !ok {
		return 0, fmt.Errorf("NAS profile %q is no longer configured", name)
	}
	return profile.Revision, nil
}

func (s *Service) verifyProfileRevision(ctx context.Context, name string, planned uint64) error {
	current, err := s.profileRevision(ctx, name)
	if err != nil {
		return err
	}
	if current != planned {
		return fmt.Errorf("NAS profile %q changed after planning (planned revision %d, current revision %d); create a new plan", name, planned, current)
	}
	return nil
}

func (s *Service) GetSystemInfo(ctx context.Context, requestedNAS string) (SystemInfoResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return SystemInfoResult{}, err
	}
	info, err := client.SystemInfo(ctx)
	if err != nil {
		return SystemInfoResult{}, authenticationError(name, err)
	}
	return SystemInfoResult{NAS: name, System: info}, nil
}

func (s *Service) Authenticate(ctx context.Context, requestedNAS string) (AuthenticationResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return AuthenticationResult{}, err
	}
	if err := client.Authenticate(ctx); err != nil {
		return AuthenticationResult{}, authenticationError(name, err)
	}
	return AuthenticationResult{NAS: name}, nil
}

func (s *Service) GetCompatibility(ctx context.Context, requestedNAS string) (CompatibilityResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return CompatibilityResult{}, err
	}
	report, err := client.Compatibility(ctx)
	if err != nil {
		return CompatibilityResult{}, authenticationError(name, err)
	}
	return CompatibilityResult{NAS: name, Report: report}, nil
}

func (s *Service) GetControlPanelTimeState(ctx context.Context, requestedNAS string) (ControlPanelTimeStateResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ControlPanelTimeStateResult{}, err
	}
	state, err := client.ControlPanelTimeState(ctx)
	if err != nil {
		return ControlPanelTimeStateResult{}, authenticationError(name, err)
	}
	return ControlPanelTimeStateResult{NAS: name, Time: state}, nil
}

func (s *Service) GetControlPanelTimeCapabilities(ctx context.Context, requestedNAS string) (ControlPanelTimeCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ControlPanelTimeCapabilitiesResult{}, err
	}
	capabilities, report, err := client.ControlPanelTimeCapabilities(ctx)
	if err != nil {
		return ControlPanelTimeCapabilitiesResult{}, authenticationError(name, err)
	}
	return ControlPanelTimeCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetStorageState(ctx context.Context, requestedNAS string) (StorageStateResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return StorageStateResult{}, err
	}
	state, err := client.StorageState(ctx)
	if err != nil {
		return StorageStateResult{}, authenticationError(name, err)
	}
	return StorageStateResult{NAS: name, Storage: state}, nil
}

func (s *Service) GetStorageCapabilities(ctx context.Context, requestedNAS string) (StorageCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return StorageCapabilitiesResult{}, err
	}
	capabilities, report, err := client.StorageCapabilities(ctx)
	if err != nil {
		return StorageCapabilitiesResult{}, authenticationError(name, err)
	}
	return StorageCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetIdentityState(ctx context.Context, requestedNAS string) (IdentityStateResult, error) {
	return s.GetIdentityStateWithQuery(ctx, requestedNAS, identity.StateQuery{})
}

func (s *Service) GetIdentityStateWithQuery(ctx context.Context, requestedNAS string, query identity.StateQuery) (IdentityStateResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return IdentityStateResult{}, err
	}
	state, err := client.IdentityState(ctx, query)
	if err != nil {
		return IdentityStateResult{}, authenticationError(name, err)
	}
	return IdentityStateResult{NAS: name, Identity: state}, nil
}

func (s *Service) GetIdentityCapabilities(ctx context.Context, requestedNAS string) (IdentityCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return IdentityCapabilitiesResult{}, err
	}
	capabilities, report, err := client.IdentityCapabilities(ctx)
	if err != nil {
		return IdentityCapabilitiesResult{}, authenticationError(name, err)
	}
	return IdentityCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetShareState(ctx context.Context, requestedNAS string, includePermissions bool) (ShareStateResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ShareStateResult{}, err
	}
	state, err := client.ShareState(ctx, includePermissions)
	if err != nil {
		return ShareStateResult{}, authenticationError(name, err)
	}
	return ShareStateResult{NAS: name, Shares: state}, nil
}

func (s *Service) GetShareCapabilities(ctx context.Context, requestedNAS string) (ShareCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ShareCapabilitiesResult{}, err
	}
	capabilities, report, err := client.ShareCapabilities(ctx)
	if err != nil {
		return ShareCapabilitiesResult{}, authenticationError(name, err)
	}
	return ShareCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetSANState(ctx context.Context, requestedNAS string) (SANStateResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return SANStateResult{}, err
	}
	state, err := client.SANState(ctx)
	if err != nil {
		return SANStateResult{}, authenticationError(name, err)
	}
	return SANStateResult{NAS: name, SAN: state}, nil
}

func (s *Service) GetSANCapabilities(ctx context.Context, requestedNAS string) (SANCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return SANCapabilitiesResult{}, err
	}
	capabilities, report, err := client.SANCapabilities(ctx)
	if err != nil {
		return SANCapabilitiesResult{}, authenticationError(name, err)
	}
	return SANCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetLogState(ctx context.Context, requestedNAS string, query syslog.StateQuery) (LogStateResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return LogStateResult{}, err
	}
	state, err := client.LogState(ctx, query)
	if err != nil {
		return LogStateResult{}, authenticationError(name, err)
	}
	return LogStateResult{NAS: name, Logs: state}, nil
}

func (s *Service) GetLogCapabilities(ctx context.Context, requestedNAS string) (LogCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return LogCapabilitiesResult{}, err
	}
	capabilities, report, err := client.LogCapabilities(ctx)
	if err != nil {
		return LogCapabilitiesResult{}, authenticationError(name, err)
	}
	return LogCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetResourceMonitorState(ctx context.Context, requestedNAS string) (ResourceMonitorStateResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ResourceMonitorStateResult{}, err
	}
	state, err := client.ResourceMonitorState(ctx)
	if err != nil {
		return ResourceMonitorStateResult{}, authenticationError(name, err)
	}
	return ResourceMonitorStateResult{NAS: name, Utilization: state}, nil
}

func (s *Service) GetResourceMonitorHistory(ctx context.Context, requestedNAS string, query resmon.HistoryQuery) (ResourceMonitorHistoryResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ResourceMonitorHistoryResult{}, err
	}
	history, err := client.ResourceMonitorHistory(ctx, query)
	if err != nil {
		return ResourceMonitorHistoryResult{}, authenticationError(name, err)
	}
	return ResourceMonitorHistoryResult{NAS: name, History: history}, nil
}

func (s *Service) GetResourceMonitorSetting(ctx context.Context, requestedNAS string) (ResourceRecordingSettingResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ResourceRecordingSettingResult{}, err
	}
	setting, err := client.ResourceMonitorSetting(ctx)
	if err != nil {
		return ResourceRecordingSettingResult{}, authenticationError(name, err)
	}
	return ResourceRecordingSettingResult{NAS: name, Setting: setting}, nil
}

func (s *Service) GetResourceMonitorCapabilities(ctx context.Context, requestedNAS string) (ResourceMonitorCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ResourceMonitorCapabilitiesResult{}, err
	}
	capabilities, report, err := client.ResourceMonitorCapabilities(ctx)
	if err != nil {
		return ResourceMonitorCapabilitiesResult{}, authenticationError(name, err)
	}
	return ResourceMonitorCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func authenticationError(name string, err error) error {
	if synology.IsOTPRequired(err) {
		return fmt.Errorf("NAS %q requires a one-time password; run 'dsmctl auth login --nas %s' in an interactive terminal first: %w", name, name, err)
	}
	return fmt.Errorf("NAS %q: %w", name, err)
}

func (s *Service) Close(ctx context.Context) error {
	return s.manager.Close(ctx)
}
