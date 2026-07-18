package state

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ychiu1211/dsmctl/internal/config"
)

const (
	SchemaVersion        = 4
	MaxProfiles          = 32
	MaxMCPTokenNameBytes = 64
	DefaultApprovalTTL   = 10 * time.Minute
	AdminModeLocal       = "local"
	MaxAdminSessions     = 16
	AdminSessionTTL      = 12 * time.Hour

	TLSSystemCA          = "system_ca"
	TLSPinnedFingerprint = "pinned_fingerprint"
)

var (
	ErrNotFound              = errors.New("gateway state entry not found")
	ErrRevisionConflict      = errors.New("profile revision conflict")
	ErrAdministratorRequired = errors.New("gateway local administrator setup is required")
	ErrAlreadyInitialized    = errors.New("gateway local administrator is already initialized")
	ErrUnauthorized          = errors.New("gateway administrator authentication failed")
	ErrTokenUnauthorized     = errors.New("MCP bearer token authentication failed")
	ErrApprovalRequired      = errors.New("an exact unexpired administrator approval is required")
)

// Profile is the non-secret, persistent connection definition exposed by the
// administration API. Credential values and their internal vault identifiers
// are deliberately absent.
type Profile struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	URL                    string    `json:"url"`
	Username               string    `json:"username,omitempty"`
	TLSMode                string    `json:"tls_mode"`
	CertificateFingerprint string    `json:"certificate_fingerprint,omitempty"`
	TimeoutSeconds         int       `json:"timeout_seconds,omitempty"`
	Revision               uint64    `json:"revision"`
	Default                bool      `json:"default"`
	PasswordStored         bool      `json:"password_stored"`
	TrustedDeviceStored    bool      `json:"trusted_device_stored"`
	SessionStored          bool      `json:"session_stored"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type ProfileInput struct {
	Name                   string `json:"name"`
	URL                    string `json:"url"`
	Username               string `json:"username,omitempty"`
	TLSMode                string `json:"tls_mode,omitempty"`
	CertificateFingerprint string `json:"certificate_fingerprint,omitempty"`
	TimeoutSeconds         int    `json:"timeout_seconds,omitempty"`
}

type Health struct {
	SchemaVersion int    `json:"schema_version"`
	ProfileCount  int    `json:"profile_count"`
	Initialized   bool   `json:"initialized"`
	Ready         bool   `json:"ready"`
	AdminMode     string `json:"admin_mode,omitempty"`
}

// AdministratorSession is non-secret session metadata. The browser token and
// its SHA-256 database key are deliberately absent.
type AdministratorSession struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type SecretMetadata struct {
	ID        string    `json:"id"`
	ProfileID string    `json:"profile_id"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func normalizeProfileInput(input ProfileInput) (ProfileInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.URL = strings.TrimRight(strings.TrimSpace(input.URL), "/")
	input.Username = strings.TrimSpace(input.Username)
	input.TLSMode = strings.TrimSpace(input.TLSMode)
	if input.TLSMode == "" {
		input.TLSMode = TLSSystemCA
	}
	if err := config.ValidateName(input.Name); err != nil {
		return ProfileInput{}, err
	}
	parsed, err := url.Parse(input.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ProfileInput{}, errors.New("URL must be an absolute http or https URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return ProfileInput{}, errors.New("URL must contain only scheme, host, and optional port")
	}
	input.URL = parsed.Scheme + "://" + parsed.Host
	if input.TimeoutSeconds < 0 || input.TimeoutSeconds > 120 {
		return ProfileInput{}, errors.New("timeout_seconds must be between 0 and 120")
	}
	switch input.TLSMode {
	case TLSSystemCA:
		if strings.TrimSpace(input.CertificateFingerprint) != "" {
			return ProfileInput{}, errors.New("certificate_fingerprint is valid only with pinned_fingerprint TLS mode")
		}
		input.CertificateFingerprint = ""
	case TLSPinnedFingerprint:
		if parsed.Scheme != "https" {
			return ProfileInput{}, errors.New("pinned_fingerprint TLS mode requires an https URL")
		}
		fingerprint, err := normalizeFingerprint(input.CertificateFingerprint)
		if err != nil {
			return ProfileInput{}, err
		}
		input.CertificateFingerprint = fingerprint
	default:
		return ProfileInput{}, fmt.Errorf("TLS mode must be %q or %q", TLSSystemCA, TLSPinnedFingerprint)
	}
	return input, nil
}

func normalizeFingerprint(value string) (string, error) {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), ":", ""))
	if len(value) != 64 {
		return "", errors.New("certificate fingerprint must be a SHA-256 fingerprint")
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return "", errors.New("certificate fingerprint must contain only hexadecimal characters")
		}
	}
	return value, nil
}
