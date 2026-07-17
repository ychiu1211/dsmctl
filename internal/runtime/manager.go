package runtime

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/domain/resmon"
	"github.com/ychiu1211/dsmctl/internal/domain/share"
	"github.com/ychiu1211/dsmctl/internal/domain/syslog"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type Client interface {
	Authenticate(ctx context.Context) error
	SystemInfo(ctx context.Context) (synology.SystemInfo, error)
	Compatibility(ctx context.Context) (synology.CompatibilityReport, error)
	ControlPanelTimeState(ctx context.Context) (synology.ControlPanelTimeState, error)
	ControlPanelTimeCapabilities(ctx context.Context) (synology.ControlPanelTimeCapabilities, synology.CompatibilityReport, error)
	StorageState(ctx context.Context) (synology.StorageState, error)
	StorageCapabilities(ctx context.Context) (synology.StorageCapabilities, synology.CompatibilityReport, error)
	ApplyStorageChange(ctx context.Context, input synology.StorageMutationInput) (synology.StorageMutationResult, error)
	IdentityState(ctx context.Context, queries ...identity.StateQuery) (synology.IdentityState, error)
	ApplicationPrivilegePreview(ctx context.Context, principalType, principal string) (identity.ApplicationPrivilegeAssignment, error)
	IdentityCapabilities(ctx context.Context) (synology.IdentityCapabilities, synology.CompatibilityReport, error)
	ApplyIdentityChange(ctx context.Context, request synology.IdentityChangeRequest, password string) (synology.IdentityMutationResult, error)
	ShareState(ctx context.Context, includePermissions bool) (synology.ShareState, error)
	ShareStateForPrincipals(ctx context.Context, principals []share.Principal) (synology.ShareState, error)
	ShareCapabilities(ctx context.Context) (synology.ShareCapabilities, synology.CompatibilityReport, error)
	ApplyShareChange(ctx context.Context, request synology.ShareChangeRequest) (synology.ShareMutationResult, error)
	SANState(ctx context.Context) (synology.SANState, error)
	SANCapabilities(ctx context.Context) (synology.SANCapabilities, synology.CompatibilityReport, error)
	ApplySANChange(ctx context.Context, input synology.SANMutationInput) (synology.SANMutationResult, error)
	LogState(ctx context.Context, query syslog.StateQuery) (synology.LogState, error)
	LogCapabilities(ctx context.Context) (synology.LogCapabilities, synology.CompatibilityReport, error)
	ResourceMonitorState(ctx context.Context) (synology.ResourceUtilization, error)
	ResourceMonitorHistory(ctx context.Context, query resmon.HistoryQuery) (synology.ResourceHistory, error)
	ResourceMonitorSetting(ctx context.Context) (synology.ResourceRecordingSetting, error)
	ResourceMonitorCapabilities(ctx context.Context) (synology.ResourceMonitorCapabilities, synology.CompatibilityReport, error)
	ApplyResourceRecordingChange(ctx context.Context, change resmon.RecordingChange) (synology.ResourceRecordingMutationResult, error)
}

type OTPProvider func(ctx context.Context, profileName string) (string, error)

type Option func(*Manager)

func WithDeviceStore(store credentials.DeviceStore) Option {
	return func(manager *Manager) {
		manager.devices = store
	}
}

func WithOTPProvider(provider OTPProvider) Option {
	return func(manager *Manager) {
		manager.otp = provider
	}
}

func WithDeviceName(name string) Option {
	return func(manager *Manager) {
		if strings.TrimSpace(name) != "" {
			manager.deviceName = strings.TrimSpace(name)
		}
	}
}

type Manager struct {
	config      *config.Config
	credentials credentials.Resolver
	devices     credentials.DeviceStore
	otp         OTPProvider
	deviceName  string

	mu      sync.Mutex
	clients map[string]*synology.Client
}

func NewManager(cfg *config.Config, resolver credentials.Resolver, options ...Option) *Manager {
	manager := &Manager{
		config:      cfg,
		credentials: resolver,
		deviceName:  defaultDeviceName(),
		clients:     make(map[string]*synology.Client),
	}
	for _, option := range options {
		option(manager)
	}
	return manager
}

// Client resolves a NAS profile and lazily creates one reusable authenticated
// client per profile. Separate profiles can therefore hold independent DSM
// sessions at the same time.
func (m *Manager) Client(ctx context.Context, requested string) (string, Client, error) {
	name, profile, err := m.config.Resolve(requested)
	if err != nil {
		return "", nil, err
	}

	m.mu.Lock()
	if client, ok := m.clients[name]; ok {
		m.mu.Unlock()
		return name, client, nil
	}
	m.mu.Unlock()

	password, err := m.credentials.Password(ctx, name, profile)
	if err != nil {
		return "", nil, err
	}
	device := credentials.TrustedDevice{Name: m.deviceName}
	if m.devices != nil {
		device, err = m.devices.TrustedDevice(ctx, name)
		if err != nil {
			return "", nil, err
		}
		if device.Name == "" {
			device.Name = m.deviceName
		}
	}
	var otp synology.OTPProvider
	if m.otp != nil {
		otp = func(ctx context.Context) (string, error) {
			return m.otp(ctx, name)
		}
	}
	var saveDeviceID synology.DeviceIDSaver
	if m.devices != nil {
		saveDeviceID = func(ctx context.Context, deviceID string) error {
			return m.devices.SaveTrustedDevice(ctx, name, credentials.TrustedDevice{
				Name: device.Name,
				ID:   deviceID,
			})
		}
	}
	client, err := synology.NewClient(synology.Options{
		BaseURL:      profile.URL,
		Username:     profile.Username,
		Password:     password,
		DeviceName:   device.Name,
		DeviceID:     device.ID,
		OTPProvider:  otp,
		SaveDeviceID: saveDeviceID,
		HTTPClient:   httpClient(profile),
	})
	if err != nil {
		return "", nil, fmt.Errorf("create client for NAS %q: %w", name, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.clients[name]; ok {
		return name, existing, nil
	}
	m.clients[name] = client
	return name, client, nil
}

// SessionInfo reports in-process session state for one profile without
// resolving credentials or contacting DSM. ClientCached means this process
// created a client for the profile; SessionHeld means that client holds a
// DSM session ID from an earlier login, which may have expired server-side.
type SessionInfo struct {
	ClientCached bool
	SessionHeld  bool
}

func (m *Manager) SessionInfo(profileName string) SessionInfo {
	m.mu.Lock()
	client, ok := m.clients[profileName]
	m.mu.Unlock()
	if !ok {
		return SessionInfo{}
	}
	return SessionInfo{ClientCached: true, SessionHeld: client.HasSession()}
}

func defaultDeviceName() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return "dsmctl"
	}
	return "dsmctl@" + strings.TrimSpace(hostname)
}

func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	clients := m.clients
	m.clients = make(map[string]*synology.Client)
	m.mu.Unlock()

	var closeErrors []error
	for name, client := range clients {
		if err := client.Close(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("NAS %q: %w", name, err))
		}
	}
	return errors.Join(closeErrors...)
}

func httpClient(profile config.Profile) *http.Client {
	timeout := 30 * time.Second
	if profile.TimeoutSeconds > 0 {
		timeout = time.Duration(profile.TimeoutSeconds) * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: profile.InsecureSkipTLSVerify, // Explicit per-profile opt-in for self-signed test NAS devices.
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}
