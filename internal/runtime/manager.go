package runtime

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	neturl "net/url"
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
	"github.com/ychiu1211/dsmctl/internal/weblogin"
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
	NotificationMailState(ctx context.Context) (synology.NotificationMailState, error)
	NotificationPushState(ctx context.Context) (synology.NotificationPushState, error)
	NotificationWebhookState(ctx context.Context) (synology.NotificationWebhookState, error)
	NotificationSMSState(ctx context.Context) (synology.NotificationSMSState, error)
	NotificationRulesState(ctx context.Context) (synology.NotificationRulesState, error)
	NotificationDesktopState(ctx context.Context) (synology.NotificationDesktopState, error)
	NotificationHistory(ctx context.Context, query synology.NotificationHistoryQuery) (synology.NotificationHistoryState, error)
	NotificationCapabilities(ctx context.Context) (synology.NotificationCapabilities, synology.CompatibilityReport, error)
	TaskSchedulerScheduled(ctx context.Context) (synology.TaskSchedulerScheduledTasks, error)
	TaskSchedulerTriggered(ctx context.Context) (synology.TaskSchedulerTriggeredTasks, error)
	TaskSchedulerCapabilities(ctx context.Context) (synology.TaskSchedulerCapabilities, synology.CompatibilityReport, error)
	DSMUpdateStatus(ctx context.Context) (synology.DSMUpdateStatus, error)
	DSMUpdateAvailable(ctx context.Context) (synology.DSMUpdateAvailable, error)
	DSMUpdatePolicy(ctx context.Context) (synology.DSMUpdatePolicy, error)
	DSMUpdateConfigBackup(ctx context.Context) (synology.DSMUpdateConfigBackup, error)
	DSMUpdateCapabilities(ctx context.Context) (synology.DSMUpdateCapabilities, synology.CompatibilityReport, error)
	DiskHealth(ctx context.Context) (synology.DiskHealthState, error)
	DiskSMARTAttributes(ctx context.Context) (synology.DiskSMARTState, error)
	DiskSMARTCapabilities(ctx context.Context) (synology.DiskSMARTCapabilities, synology.CompatibilityReport, error)
	ResourceMonitorState(ctx context.Context) (synology.ResourceUtilization, error)
	ResourceMonitorHistory(ctx context.Context, query resmon.HistoryQuery) (synology.ResourceHistory, error)
	ResourceMonitorSetting(ctx context.Context) (synology.ResourceRecordingSetting, error)
	ResourceMonitorCapabilities(ctx context.Context) (synology.ResourceMonitorCapabilities, synology.CompatibilityReport, error)
	ApplyResourceRecordingChange(ctx context.Context, change resmon.RecordingChange) (synology.ResourceRecordingMutationResult, error)
	ExternalAccessAccountState(ctx context.Context) (synology.ExternalAccessAccountState, error)
	ExternalAccessQuickConnectState(ctx context.Context) (synology.ExternalAccessQuickConnectState, error)
	ExternalAccessDDNSState(ctx context.Context) (synology.ExternalAccessDDNSState, error)
	ExternalAccessPortForwardState(ctx context.Context) (synology.ExternalAccessPortForwardState, error)
	ExternalAccessCapabilities(ctx context.Context) (synology.ExternalAccessCapabilities, synology.CompatibilityReport, error)
	ApplyExternalAccessQuickConnectChange(ctx context.Context, change synology.ExternalAccessQuickConnectChange) (synology.ExternalAccessQuickConnectMutationResult, error)
	DownloadStationServiceState(ctx context.Context) (synology.DownloadStationServiceState, error)
	DownloadStationTasks(ctx context.Context) (synology.DownloadStationTasks, error)
	DownloadStationStatistics(ctx context.Context) (synology.DownloadStationStatistics, error)
	DownloadStationSettings(ctx context.Context) (synology.DownloadStationSettings, error)
	DownloadStationCapabilities(ctx context.Context) (synology.DownloadStationCapabilities, synology.CompatibilityReport, error)
	FileStationCapabilities(ctx context.Context) (synology.FileStationCapabilities, synology.CompatibilityReport, error)
	FileStationInfoState(ctx context.Context) (synology.FileStationService, error)
	FileStationListShares(ctx context.Context, query synology.FileStationListShareQuery) (synology.FileStationListing, error)
	FileStationList(ctx context.Context, query synology.FileStationListQuery) (synology.FileStationListing, error)
	FileStationGetInfo(ctx context.Context, query synology.FileStationGetInfoQuery) (synology.FileStationInfo, error)
	FileStationSearch(ctx context.Context, query synology.FileStationSearchQuery) (synology.FileStationSearchResult, error)
	FileStationDirSize(ctx context.Context, query synology.FileStationDirSizeQuery) (synology.FileStationDirSize, error)
	FileStationMD5(ctx context.Context, query synology.FileStationMD5Query) (synology.FileStationMD5, error)
	FileStationVirtualFolders(ctx context.Context, query synology.FileStationVirtualFolderQuery) (synology.FileStationListing, error)
	FileStationCheckPermission(ctx context.Context, query synology.FileStationCheckPermissionQuery) (synology.FileStationPermissionCheck, error)
	DownloadFile(ctx context.Context, path string) (*synology.DownloadContent, error)
	FileStationThumbnail(ctx context.Context, path string, opts synology.ThumbnailOptions) (*synology.DownloadContent, error)
	UploadFile(ctx context.Context, dir, name string, src io.Reader, size int64, opts synology.UploadOptions) (synology.UploadResult, error)
	ApplyFileStationChange(ctx context.Context, request synology.FileStationChangeRequest, password string) (synology.FileStationMutationResult, error)
	FileStationFavoriteList(ctx context.Context) (synology.FileStationFavorites, error)
	FileStationFavoriteAdd(ctx context.Context, path, name string) error
	FileStationFavoriteDelete(ctx context.Context, path string) error
	FileStationSharingList(ctx context.Context) (synology.FileStationSharingLinks, error)
	FileStationBackgroundTasks(ctx context.Context) (synology.FileStationBackgroundTasks, error)
	ApplyDownloadStationTaskChange(ctx context.Context, change synology.DownloadStationTaskChange) (synology.DownloadStationTaskMutationResult, error)
	DownloadStationSettingsGroup(ctx context.Context, group string) (json.RawMessage, error)
	ApplyDownloadStationSettingsChange(ctx context.Context, change synology.DownloadStationSettingsChange, secrets synology.DownloadStationSettingsSecrets) (synology.DownloadStationSettingsMutationResult, error)
	HyperBackupTasks(ctx context.Context) (synology.HyperBackupTasks, error)
	HyperBackupTaskDetail(ctx context.Context, taskID int) (synology.HyperBackupTaskDetail, error)
	HyperBackupTaskStatus(ctx context.Context, taskID int) (synology.HyperBackupTaskStatus, error)
	HyperBackupVersions(ctx context.Context, taskID, offset, limit int) (synology.HyperBackupVersions, error)
	HyperBackupLogs(ctx context.Context, offset, limit int) (synology.HyperBackupLogs, error)
	HyperBackupVault(ctx context.Context) (synology.HyperBackupVault, error)
	HyperBackupApplications(ctx context.Context) (synology.HyperBackupApplications, error)
	HyperBackupCapabilities(ctx context.Context) (synology.HyperBackupCapabilities, synology.CompatibilityReport, error)
	ApplyHyperBackupTaskChange(ctx context.Context, change synology.HyperBackupTaskChange, secrets synology.HyperBackupTaskSecrets) (synology.HyperBackupTaskMutationResult, error)
}

type Option func(*Manager)

func WithDeviceStore(store credentials.DeviceStore) Option {
	return func(manager *Manager) {
		manager.devices = store
	}
}

func WithDeviceName(name string) Option {
	return func(manager *Manager) {
		if strings.TrimSpace(name) != "" {
			manager.deviceName = strings.TrimSpace(name)
		}
	}
}

// WithSessionStore lets the manager reuse a persisted web-login session for a
// profile instead of resolving a password. When a stored session is present it
// is preferred; profiles without one fall back to the password path unchanged.
func WithSessionStore(store credentials.SessionStore) Option {
	return func(manager *Manager) {
		manager.sessions = store
	}
}

// WithConfigSource replaces the static CLI configuration with a dynamic,
// snapshot-based source such as the gateway state repository.
func WithConfigSource(source config.Source) Option {
	return func(manager *Manager) {
		if source != nil {
			manager.source = source
		}
	}
}

// WithLogger threads an opt-in diagnostic logger into every DSM client the
// manager builds. A nil logger (the default) disables per-request logging.
func WithLogger(logger *slog.Logger) Option {
	return func(manager *Manager) {
		manager.logger = logger
	}
}

type clientEntry struct {
	client   *synology.Client
	revision uint64
}

type Manager struct {
	config      *config.Config
	source      config.Source
	credentials credentials.Resolver
	devices     credentials.DeviceStore
	sessions    credentials.SessionStore
	deviceName  string
	logger      *slog.Logger

	// profileGate orders dynamic repository commits against client acquisition.
	// Admin mutations hold it exclusively through commit and cache eviction;
	// requests hold it for profile resolution and cache insertion.
	profileGate sync.RWMutex
	mu          sync.Mutex
	clients     map[string]clientEntry
}

func NewManager(cfg *config.Config, resolver credentials.Resolver, options ...Option) *Manager {
	manager := &Manager{
		config:      cfg,
		source:      config.StaticSource{Config: cfg},
		credentials: resolver,
		deviceName:  defaultDeviceName(),
		clients:     make(map[string]clientEntry),
	}
	for _, option := range options {
		option(manager)
	}
	return manager
}

// OutboundCredential resolves one profile's connection identity — host,
// username, and password — for use as an OUTBOUND credential: one NAS
// authenticating to another (for example a Hyper Backup destination). The
// password comes from the same keyring-first resolver logins use and is
// returned for immediate in-process use inside a DSM call, never for display.
func (m *Manager) OutboundCredential(ctx context.Context, requested string) (name, host, username, password string, err error) {
	m.profileGate.RLock()
	defer m.profileGate.RUnlock()

	cfg, err := m.snapshot(ctx)
	if err != nil {
		return "", "", "", "", err
	}
	name, profile, err := cfg.Resolve(requested)
	if err != nil {
		return "", "", "", "", err
	}
	parsed, err := neturl.Parse(profile.URL)
	if err != nil || parsed.Hostname() == "" {
		return "", "", "", "", fmt.Errorf("profile %q has no usable URL to derive a destination host from", name)
	}
	if m.credentials == nil {
		return "", "", "", "", fmt.Errorf("no credential resolver is configured")
	}
	password, err = m.credentials.Password(ctx, name, profile)
	if err != nil {
		return "", "", "", "", fmt.Errorf("resolve the stored password for profile %q: %w", name, err)
	}
	return name, parsed.Hostname(), profile.Username, password, nil
}

// Client resolves a NAS profile and lazily creates one reusable authenticated
// client per profile. Separate profiles can therefore hold independent DSM
// sessions at the same time.
func (m *Manager) Client(ctx context.Context, requested string) (string, Client, error) {
	m.profileGate.RLock()
	defer m.profileGate.RUnlock()

	cfg, err := m.snapshot(ctx)
	if err != nil {
		return "", nil, err
	}
	name, profile, err := cfg.Resolve(requested)
	if err != nil {
		return "", nil, err
	}

	m.mu.Lock()
	if entry, ok := m.clients[name]; ok && entry.revision == profile.Revision {
		m.mu.Unlock()
		return name, entry.client, nil
	}
	stale := m.clients[name]
	delete(m.clients, name)
	m.mu.Unlock()
	if stale.client != nil {
		_ = stale.client.Close(ctx)
	}

	// Prefer a persisted web-login session over the password path. A profile
	// without a stored session (or configured only for password auth) falls
	// through unchanged.
	if m.sessions != nil {
		client, ok, err := m.sessionClient(ctx, name, profile)
		if err != nil {
			return "", nil, err
		}
		if ok {
			m.mu.Lock()
			if existing, exists := m.clients[name]; exists && existing.revision == profile.Revision {
				m.mu.Unlock()
				_ = client.Close(ctx)
				return name, existing.client, nil
			}
			m.clients[name] = clientEntry{client: client, revision: profile.Revision}
			m.mu.Unlock()
			return name, client, nil
		}
	}

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
	var saveDeviceID synology.DeviceIDSaver
	if m.devices != nil {
		saveDeviceID = func(ctx context.Context, deviceID string) error {
			return m.devices.SaveTrustedDevice(ctx, name, credentials.TrustedDevice{
				Name: device.Name,
				ID:   deviceID,
			})
		}
	}
	// Persist the session this password login establishes so later dsmctl
	// invocations reuse its session ID (through sessionClient) instead of logging
	// in again. The stored session carries no Noise resume material, so when DSM
	// evicts it after its idle timeout the seeded client recovers with a fresh
	// password login rather than a browserless resume — which is why a password
	// login is safe to persist. This makes a provisioned NAS (password in the
	// keyring, no web-login session) efficient across separate commands, not just
	// within one process.
	var saveSession func(ctx context.Context, sid, synoToken string) error
	if m.sessions != nil {
		saveSession = func(ctx context.Context, sid, synoToken string) error {
			stored, err := m.sessions.Session(ctx, name)
			if err != nil {
				stored = credentials.SessionCredential{}
			}
			stored.SID = sid
			stored.SynoToken = synoToken
			stored.Account = profile.Username
			now := time.Now()
			if stored.IssuedAt.IsZero() {
				stored.IssuedAt = now
			}
			stored.LastVerified = now
			return m.sessions.SaveSession(ctx, name, stored)
		}
	}
	client, err := synology.NewClient(synology.Options{
		BaseURL:      profile.URL,
		Username:     profile.Username,
		Password:     password,
		DeviceName:   device.Name,
		DeviceID:     device.ID,
		SaveDeviceID: saveDeviceID,
		SaveSession:  saveSession,
		HTTPClient:   httpClient(profile),
		Logger:       m.logger,
	})
	if err != nil {
		return "", nil, fmt.Errorf("create client for NAS %q: %w", name, err)
	}

	m.mu.Lock()
	if existing, ok := m.clients[name]; ok && existing.revision == profile.Revision {
		m.mu.Unlock()
		_ = client.Close(ctx)
		return name, existing.client, nil
	}
	m.clients[name] = clientEntry{client: client, revision: profile.Revision}
	m.mu.Unlock()
	return name, client, nil
}

func (m *Manager) snapshot(ctx context.Context) (*config.Config, error) {
	if m.source == nil {
		if m.config == nil {
			return nil, errors.New("config is nil")
		}
		return m.config, nil
	}
	cfg, err := m.source.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("load NAS profiles: %w", err)
	}
	if cfg == nil {
		return nil, errors.New("profile source returned a nil config")
	}
	return cfg, nil
}

// MutateProfile serializes a persistent profile/credential commit with client
// acquisition. After mutate succeeds, the named cached client is removed
// before new requests may resolve the committed revision. Passing an empty
// name is useful for default-selection changes that need ordering but no
// client eviction.
func (m *Manager) MutateProfile(ctx context.Context, name string, mutate func() error) error {
	if mutate == nil {
		return errors.New("profile mutation is required")
	}
	m.profileGate.Lock()
	if err := mutate(); err != nil {
		m.profileGate.Unlock()
		return err
	}
	var stale clientEntry
	if name != "" {
		m.mu.Lock()
		stale = m.clients[name]
		delete(m.clients, name)
		m.mu.Unlock()
	}
	m.profileGate.Unlock()
	if stale.client != nil {
		// The repository commit is already durable and cannot be rolled back.
		// Cache removal is the security boundary; a best-effort close failure
		// must not turn a successful admin mutation into an ambiguous response.
		_ = stale.client.Close(ctx)
	}
	return nil
}

// sessionClient builds a client seeded with a persisted web-login session, so
// it reuses that session instead of authenticating with a password. It reports
// ok=false when no usable session is stored, leaving the caller to fall back
// to the password path. Recovery of a rejected session ID is the seeded
// client's Resume closure, wired in seededClient.
func (m *Manager) sessionClient(ctx context.Context, name string, profile config.Profile) (*synology.Client, bool, error) {
	session, err := m.sessions.Session(ctx, name)
	if err != nil {
		return nil, false, fmt.Errorf("read stored session for NAS %q: %w", name, err)
	}
	if session.SID == "" {
		return nil, false, nil
	}
	// allowPasswordFallback lets the seeded client recover on its own if DSM has
	// evicted the session and refuses a Noise resume: it lazily resolves the
	// password and logs in again instead of forcing an interactive sign-in.
	// Reusing a live session never resolves the password.
	client, err := m.seededClient(name, profile, session, true)
	if err != nil {
		return nil, false, err
	}
	return client, true, nil
}

// seededClient builds the one client shape used for a persisted web-login
// session, so reuse (sessionClient) and revocation (RevokeStoredSession)
// cannot drift apart. The session is borrowed, not owned: the client never
// logs it out on Close (revocation is an explicit Logout), and when DSM
// rejects the session the Resume closure recovers without a browser or
// password. It first re-reads the store — picking up a session renewed by
// another process's 'auth login', which keeps a long-running MCP server
// usable after the session it started with expires — and otherwise performs
// the browserless Noise resume with the stored renewal keys, persisting the
// refreshed session for other processes to pick up the same way.
func (m *Manager) seededClient(name string, profile config.Profile, session credentials.SessionCredential, allowPasswordFallback bool) (*synology.Client, error) {
	current := session
	var passwordFunc func(ctx context.Context) (string, error)
	if allowPasswordFallback {
		passwordFunc = func(ctx context.Context) (string, error) {
			return m.credentials.Password(ctx, name, profile)
		}
	}
	client, err := synology.NewClient(synology.Options{
		BaseURL:                profile.URL,
		Username:               profile.Username,
		PasswordFunc:           passwordFunc,
		SessionID:              session.SID,
		SynoToken:              session.SynoToken,
		HTTPClient:             httpClient(profile),
		PreserveSessionOnClose: true,
		// SaveSession persists a session recovered by the password fallback so
		// later processes reuse its session ID instead of logging in again. It
		// keeps the existing resume material and account metadata and updates
		// only the live tokens.
		SaveSession: func(ctx context.Context, sid, synoToken string) error {
			stored, err := m.sessions.Session(ctx, name)
			if err != nil || stored.SID == "" {
				stored = current
			}
			stored.SID = sid
			stored.SynoToken = synoToken
			stored.LastVerified = time.Now()
			current = stored
			return m.sessions.SaveSession(ctx, name, stored)
		},
		Resume: func(ctx context.Context) (string, string, error) {
			if latest, err := m.sessions.Session(ctx, name); err == nil && latest.SID != "" && latest.SID != current.SID {
				current = latest
				return latest.SID, latest.SynoToken, nil
			}
			if !current.CanResume() {
				return "", "", fmt.Errorf("the stored session for NAS %q cannot be renewed; run 'dsmctl auth login --nas %s'", name, name)
			}
			// The webui session resumes an existing session keyed by its sid, so
			// pass the latest known sid. On success DSM returns a fresh sid;
			// update the captured session so the next resume uses it.
			refreshed, err := weblogin.Resume(ctx, profile.URL, weblogin.ResumeInput{
				Account:         current.Account,
				DeviceID:        current.DeviceID,
				SID:             current.SID,
				ServerPublicKey: current.ServerPublicKey,
				LocalPublicKey:  current.LocalPublicKey,
				LocalPrivateKey: current.LocalPrivateKey,
			}, httpClient(profile))
			if err != nil {
				return "", "", fmt.Errorf("could not renew the session for NAS %q; run 'dsmctl auth login --nas %s': %w", name, name, err)
			}
			updated := current
			updated.SID = refreshed.SID
			updated.SynoToken = refreshed.SynoToken
			updated.LastVerified = time.Now()
			current = updated
			_ = m.sessions.SaveSession(ctx, name, updated)
			return refreshed.SID, refreshed.SynoToken, nil
		},
		Logger: m.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create client for NAS %q from stored session: %w", name, err)
	}
	return client, nil
}

// RevokeStoredSession asks DSM to invalidate the persisted web-login session
// for a profile, so signing out revokes the session server-side instead of
// only forgetting the local copy. It never touches the store itself: callers
// delete the stored entry regardless of the outcome, because a revocation
// failure (NAS offline) must not keep dead material around locally.
//
// It reports (false, nil) when there is nothing it can revoke: no session
// store, no stored session ID, or a profile that is no longer configured (the
// NAS URL of an orphaned session is unknown). Transport or DSM failures are
// returned so callers can warn that the server-side session may outlive the
// local removal until it expires.
func (m *Manager) RevokeStoredSession(ctx context.Context, name string) (bool, error) {
	if m.sessions == nil {
		return false, nil
	}
	cfg, err := m.snapshot(ctx)
	if err != nil {
		return false, err
	}
	profile, ok := cfg.NAS[name]
	if !ok || profile.URL == "" {
		return false, nil
	}
	session, err := m.sessions.Session(ctx, name)
	if err != nil {
		return false, fmt.Errorf("read stored session for NAS %q: %w", name, err)
	}
	if session.SID == "" {
		return false, nil
	}
	// No password fallback for revocation: signing out must not silently start a
	// new session.
	client, err := m.seededClient(name, profile, session, false)
	if err != nil {
		return false, err
	}
	if err := client.Logout(ctx); err != nil {
		return false, fmt.Errorf("revoke DSM session for NAS %q: %w", name, err)
	}
	return true, nil
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
	entry, ok := m.clients[profileName]
	m.mu.Unlock()
	if !ok {
		return SessionInfo{}
	}
	return SessionInfo{ClientCached: true, SessionHeld: entry.client.HasSession()}
}

func defaultDeviceName() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return "dsmctl"
	}
	return "dsmctl@" + strings.TrimSpace(hostname)
}

// DefaultDeviceName is the DSM trusted-device name this host registers under.
// Enrollment flows outside the manager (for example 'auth password set') use
// it so a device registered there is recognized by later manager logins.
func DefaultDeviceName() string {
	return defaultDeviceName()
}

func (m *Manager) Close(ctx context.Context) error {
	m.profileGate.Lock()
	defer m.profileGate.Unlock()
	m.mu.Lock()
	clients := m.clients
	m.clients = make(map[string]clientEntry)
	m.mu.Unlock()

	var closeErrors []error
	for name, entry := range clients {
		if err := entry.client.Close(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("NAS %q: %w", name, err))
		}
	}
	return errors.Join(closeErrors...)
}

// HTTPClient builds an HTTP client honoring a profile's TLS and timeout
// settings. It is exported so login flows outside the manager (for example the
// web-login handshake) can reach a NAS with the same policy.
func HTTPClient(profile config.Profile) *http.Client {
	return httpClient(profile)
}

func httpClient(profile config.Profile) *http.Client {
	timeout := 30 * time.Second
	if profile.TimeoutSeconds > 0 {
		timeout = time.Duration(profile.TimeoutSeconds) * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: profile.InsecureSkipTLSVerify} //nolint:gosec // CLI-only explicit opt-in; production gateway validation rejects this field.
	if profile.TLSMode == "pinned_fingerprint" {
		expected, err := hex.DecodeString(strings.ReplaceAll(profile.CertificateFingerprint, ":", ""))
		if err == nil && len(expected) == sha256.Size {
			// Pin mode authenticates the exact leaf certificate explicitly bound
			// to this profile. It intentionally replaces CA, hostname, and validity
			// policy so LAN IP-only NAS endpoints remain usable after confirmation.
			tlsConfig.InsecureSkipVerify = true //nolint:gosec
			tlsConfig.VerifyConnection = func(state tls.ConnectionState) error {
				if len(state.PeerCertificates) == 0 {
					return errors.New("TLS peer did not provide a certificate")
				}
				leaf := state.PeerCertificates[0]
				actual := sha256.Sum256(leaf.Raw)
				if subtle.ConstantTimeCompare(actual[:], expected) != 1 {
					return errors.New("TLS server certificate does not match the pinned SHA-256 fingerprint")
				}
				return nil
			}
		} else {
			tlsConfig.InsecureSkipVerify = false
			tlsConfig.VerifyConnection = func(tls.ConnectionState) error {
				return errors.New("pinned TLS profile has an invalid SHA-256 fingerprint")
			}
		}
	}
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport, Timeout: timeout}
}
