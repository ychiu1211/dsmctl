package admin

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/gateway/state"
	"github.com/ychiu1211/dsmctl/internal/provision"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

// provisionProfile brings up a factory-fresh NAS behind an already-added,
// credential-less profile: it creates the first administrator through the shared
// application operation and stores the generated password in the encrypted
// vault bound to this profile, so it is then retrievable only through the
// human-gated reveal. The password never appears in the response, logs, or
// audit. The profile must already exist with its TLS trust decided (the Add NAS
// wizard pins a fresh NAS's self-signed certificate).
func (h *Handler) provisionProfile(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var input struct {
		AdminUser      string `json:"admin_user"`
		DeviceName     string `json:"device_name,omitempty"`
		AutoUpdate     string `json:"auto_update,omitempty"`
		Analytics      bool   `json:"analytics,omitempty"`
		PasswordLength int    `json:"password_length,omitempty"`
	}
	if err := decodeJSON(req, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.AdminUser) == "" {
		writeError(w, http.StatusBadRequest, "admin_user is required")
		return
	}
	if _, ok := h.requireProfileTLS(w, req, name); !ok {
		return
	}
	cfg, err := h.repository.Snapshot(req.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load profile")
		return
	}
	runtimeProfile, ok := cfg.NAS[name]
	if !ok {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(runtimeProfile.URL)), "https://") {
		writeError(w, http.StatusBadRequest, "provisioning requires an https NAS URL so the administrator password is not sent in cleartext")
		return
	}
	target, err := provisionTarget(runtimeProfile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "prepare provisioning client")
		return
	}
	service := application.NewService(nil, h.manager)
	sink := &vaultProvisionSink{repository: h.repository, manager: h.manager}
	result, err := service.ProvisionFirstAdmin(req.Context(), target, application.ProvisionRequest{
		Name: name, URL: runtimeProfile.URL, AdminUser: input.AdminUser, DeviceName: input.DeviceName,
		AutoUpdate: input.AutoUpdate, Analytics: input.Analytics, PasswordLength: input.PasswordLength,
	}, sink)
	if err != nil {
		// The error carries DSM codes / operational context but never the
		// generated password (the sink holds it). It is safe to surface.
		h.logger.ErrorContext(req.Context(), "provision failed",
			"request_id", correlationID(req), "nas", name, "error", err)
		writeError(w, http.StatusBadGateway, "provision failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// vaultProvisionSink persists a provisioned administrator credential into the
// gateway's encrypted vault, bound to the existing profile. Reading the current
// revision and enrolling happen inside one MutateProfile so a concurrent profile
// edit cannot attach the credential to changed connection settings. It stores no
// trusted device (the setup session is not an OTP flow).
// NewVaultProvisionSink returns a ProvisionSink that stores a provisioned
// administrator credential in the gateway's encrypted vault, bound to an
// existing profile. It is exported so the gateway entry point can install it on
// the shared Service (WithProvisionSink), which the remote provision_nas MCP
// tool then uses; the admin HTTP endpoint constructs its own equivalent.
func NewVaultProvisionSink(repository *state.Repository, manager *runtime.Manager) application.ProvisionSink {
	return &vaultProvisionSink{repository: repository, manager: manager}
}

type vaultProvisionSink struct {
	repository *state.Repository
	manager    *runtime.Manager
}

func (s *vaultProvisionSink) PersistProvisioned(ctx context.Context, persist application.ProvisionPersist) error {
	return s.manager.MutateProfile(ctx, persist.Name, func() error {
		profile, err := s.repository.Profile(ctx, persist.Name)
		if err != nil {
			return err
		}
		_, err = s.repository.EnrollPasswordForAccount(ctx, persist.Name, profile.Revision, persist.Username, persist.Password, credentials.TrustedDevice{})
		return err
	})
}

// CreateProvisionProfile creates a vault profile for a discovered, un-enrolled
// device (its certificate pinned trust-on-first-use) before its administrator is
// provisioned. The new profile is deliberately NOT added to any MCP token's
// allowlist: a provisioning grant creates a profile but never silently grants
// ongoing remote management of it.
func (s *vaultProvisionSink) CreateProvisionProfile(ctx context.Context, spec application.ProvisionProfileSpec) error {
	return s.manager.MutateProfile(ctx, spec.Name, func() error {
		_, err := s.repository.CreateProfile(ctx, state.ProfileInput{
			Name:                   spec.Name,
			URL:                    spec.URL,
			TLSMode:                spec.TLSMode,
			CertificateFingerprint: spec.CertificateFingerprint,
		})
		return err
	})
}

// provisionTarget builds a provisioning HTTP client that reuses the profile's
// TLS policy (the pinned fresh-NAS certificate) and adds a cookie jar to carry
// the DSM setup/login session across the compound calls.
func provisionTarget(profile config.Profile) (provision.Target, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return provision.Target{}, err
	}
	client := runtime.HTTPClient(profile)
	client.Jar = jar
	client.Timeout = 90 * time.Second
	return provision.Target{BaseURL: profile.URL, HTTPClient: client}, nil
}
