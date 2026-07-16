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
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type Client interface {
	Authenticate(ctx context.Context) error
	SystemInfo(ctx context.Context) (synology.SystemInfo, error)
	Compatibility(ctx context.Context) (synology.CompatibilityReport, error)
	StorageState(ctx context.Context) (synology.StorageState, error)
	StorageCapabilities(ctx context.Context) (synology.StorageCapabilities, synology.CompatibilityReport, error)
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
