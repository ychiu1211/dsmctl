package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http/cookiejar"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/provision"
	"github.com/ychiu1211/dsmctl/internal/runtime"
	"github.com/ychiu1211/dsmctl/internal/tlstrust"
)

func newProvisionCommand(opts *options) *cobra.Command {
	var adminUser, targetURL, deviceName, autoUpdate string
	var skipTLS, analytics, finishOnly, resetBuiltinAdmin bool
	var length int
	command := &cobra.Command{
		Use:   "provision <name>",
		Short: "Bring up a fresh NAS: create the first administrator and store a generated password",
		Long: "Take a Synology NAS in its DSM first-run setup window to a working DSM: create the first\n" +
			"administrator (username yours via --admin-user; password generated locally, stored in the OS\n" +
			"credential store, never printed), apply the update policy and privacy defaults, and finish the\n" +
			"setup wizard. Retrieve the password later, at a terminal, with 'dsmctl auth password reveal'.\n" +
			"The provisioning sequence is the shared application operation; this command is a thin adapter.\n" +
			"Use --finish-only to run just the post-account wizard steps against a NAS whose administrator\n" +
			"already exists (it signs in with the stored password).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			if err := config.ValidateName(name); err != nil {
				return err
			}
			store := config.NewStore(opts.configPath)
			cfg, err := store.Load()
			if err != nil {
				return err
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			setup := provision.SetupOptions{AutoUpdate: autoUpdate, Analytics: analytics}

			if finishOnly {
				profile, ok := cfg.NAS[name]
				if !ok {
					return fmt.Errorf("NAS profile %q is not configured", name)
				}
				target, err := buildProvisionTarget(profile)
				if err != nil {
					return err
				}
				password, err := credentials.NewSecureStore().StoredPassword(ctx, name)
				if err != nil {
					return fmt.Errorf("need the stored administrator password to finish setup: %w", err)
				}
				if err := service.FinishSetup(ctx, target, profile.Username, password, setup); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Finished the DSM setup wizard for NAS %q.\n", name)
				return nil
			}

			if resetBuiltinAdmin {
				profile, ok := cfg.NAS[name]
				if !ok {
					return fmt.Errorf("NAS profile %q is not configured", name)
				}
				target, err := buildProvisionTarget(profile)
				if err != nil {
					return err
				}
				if err := service.DisableBuiltinAdmin(ctx, target); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Scrambled and disabled the built-in admin on NAS %q; the DSM first-run welcome wizard should no longer appear.\n", name)
				return nil
			}

			if strings.TrimSpace(adminUser) == "" {
				return errors.New("--admin-user is required; the administrator username is yours to choose")
			}
			if !strings.HasPrefix(strings.ToLower(targetURL), "https://") {
				return errors.New("--url must be an https URL, for example https://10.17.37.51:5001")
			}
			profile := config.Profile{URL: targetURL, Username: adminUser, InsecureSkipTLSVerify: skipTLS}
			if !skipTLS {
				profile, err = trustProvisionCertificate(ctx, cmd.InOrStdin(), cmd.ErrOrStderr(), name, profile)
				if err != nil {
					return err
				}
			}
			return runFirstAdmin(cmd, store, cfg, service, firstAdminRequest{
				name: name, adminUser: adminUser, deviceName: deviceName,
				autoUpdate: autoUpdate, analytics: analytics, length: length, profile: profile,
			})
		},
	}
	command.Flags().StringVar(&adminUser, "admin-user", "", "administrator username to create (required unless --finish-only; your choice, never generated)")
	command.Flags().StringVar(&targetURL, "url", "", "DSM https URL of the NAS in its setup window, e.g. https://10.17.37.51:5001 (required unless --finish-only)")
	command.Flags().StringVar(&deviceName, "device-name", "", "DSM server name (hostname) to set")
	command.Flags().StringVar(&autoUpdate, "auto-update", "security", "DSM update policy: security (auto-install security hotfixes), all, or notify")
	command.Flags().BoolVar(&skipTLS, "insecure-skip-tls-verify", false, "accept the NAS's fresh self-signed certificate without pinning (for an explicitly isolated lab NAS)")
	command.Flags().BoolVar(&analytics, "analytics", false, "opt in to Synology device analytics / Active Insight (default off)")
	command.Flags().BoolVar(&finishOnly, "finish-only", false, "skip account creation; only run the post-account wizard steps, signing in with the stored password")
	command.Flags().BoolVar(&resetBuiltinAdmin, "reset-builtin-admin", false, "retrofit only: scramble and expire the built-in admin from the DSM setup session (fixes a NAS whose welcome wizard keeps showing because provisioning could not disable admin)")
	command.Flags().IntVar(&length, "length", credentials.DefaultGeneratedPasswordLength, "generated password length")
	return command
}

// firstAdminRequest carries the parameters for creating a NAS's first
// administrator. profile has its TLS trust already decided by the caller.
type firstAdminRequest struct {
	name       string
	adminUser  string
	deviceName string
	autoUpdate string
	analytics  bool
	length     int
	profile    config.Profile
}

// runFirstAdmin creates the first administrator through the shared application
// operation, storing the generated password in the OS keyring and recording the
// profile. It is used both by `dsmctl provision` and by `dsmctl install
// --admin-user` (the combined install→provision flow).
func runFirstAdmin(cmd *cobra.Command, store *config.Store, cfg *config.Config, service *application.Service, req firstAdminRequest) error {
	target, err := buildProvisionTarget(req.profile)
	if err != nil {
		return err
	}
	sink := &keyringProvisionSink{store: store, cfg: cfg, secrets: credentials.NewSecureStore(), profile: req.profile}
	preq := application.ProvisionRequest{
		Name: req.name, URL: req.profile.URL, AdminUser: req.adminUser, DeviceName: req.deviceName,
		AutoUpdate: req.autoUpdate, Analytics: req.analytics, PasswordLength: req.length,
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Creating administrator %q on %s ...\n", req.adminUser, req.profile.URL)
	result, err := service.ProvisionFirstAdmin(cmd.Context(), target, preq, sink)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Administrator %q created and verified.\n", result.AdminUser)
	fmt.Fprintf(out, "Stored the generated password in the OS credential store (service dsmctl, profile %q).\n", req.name)
	for _, warning := range result.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "Note: %s; the administrator is created and usable.\n", warning)
	}
	fmt.Fprintf(out, "\nProvisioned NAS %q as administrator %q.\n", req.name, req.adminUser)
	fmt.Fprintf(out, "Retrieve the password (a human, at a terminal) with:\n    dsmctl auth password reveal --nas %s\n", req.name)
	return nil
}

// keyringProvisionSink persists a provisioned administrator on the CLI: it saves
// the generated password in the OS keyring and records the NAS profile (with the
// TLS trust decided during provisioning) in the configuration. The gateway will
// implement the same ProvisionSink against its encrypted vault; the operation
// does not know which one it holds.
type keyringProvisionSink struct {
	store   *config.Store
	cfg     *config.Config
	secrets *credentials.SecureStore
	profile config.Profile
}

func (s *keyringProvisionSink) PersistProvisioned(ctx context.Context, persist application.ProvisionPersist) error {
	if err := s.secrets.SavePassword(ctx, persist.Name, persist.Password); err != nil {
		return err
	}
	profile := s.cfg.NAS[persist.Name]
	profile.URL = persist.URL
	profile.Username = persist.Username
	profile.InsecureSkipTLSVerify = s.profile.InsecureSkipTLSVerify
	profile.TLSMode = s.profile.TLSMode
	profile.CertificateFingerprint = s.profile.CertificateFingerprint
	s.cfg.NAS[persist.Name] = profile
	if s.cfg.DefaultNAS == "" {
		s.cfg.DefaultNAS = persist.Name
	}
	return s.store.Save(s.cfg)
}

// buildProvisionTarget builds a provisioning HTTP client that reuses the
// standard TLS policy (skip-verify or pinned fingerprint) and adds a cookie jar
// to carry the DSM setup/login session across the compound calls.
func buildProvisionTarget(profile config.Profile) (provision.Target, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return provision.Target{}, err
	}
	client := runtime.HTTPClient(profile)
	client.Jar = jar
	client.Timeout = 90 * time.Second
	return provision.Target{BaseURL: profile.URL, HTTPClient: client}, nil
}

// trustProvisionCertificate establishes pin-on-first-use trust for a fresh NAS's
// self-signed certificate before any credential is sent. It returns the profile
// updated with the pinned fingerprint; the sink persists that pin with the rest
// of the profile once provisioning succeeds. A changed pin still fails closed.
func trustProvisionCertificate(ctx context.Context, input io.Reader, output io.Writer, name string, profile config.Profile) (config.Profile, error) {
	pin := ""
	if profile.TLSMode == "pinned_fingerprint" {
		pin = profile.CertificateFingerprint
	}
	err := tlstrust.Probe(ctx, profile.URL, pin)
	if err == nil {
		return profile, nil
	}
	var trustErr *tlstrust.TrustError
	if !errors.As(err, &trustErr) {
		return config.Profile{}, fmt.Errorf("verify TLS certificate for NAS %q before provisioning: %w", name, err)
	}
	certificate := trustErr.Certificate
	fmt.Fprintf(output, "NAS %q presented a certificate that requires explicit trust.\n", name)
	if trustErr.Code == tlstrust.CodePinMismatch {
		fmt.Fprintf(output, "The certificate changed and no longer matches the stored pin.\nPreviously pinned: %s\n", displayFingerprint(trustErr.ExpectedFingerprint))
	} else {
		fmt.Fprintln(output, "A factory-fresh NAS uses a self-signed certificate; verify this fingerprint out of band.")
	}
	if len(trustErr.ValidationWarnings) != 0 {
		fmt.Fprintln(output, "Verification warnings:")
		for _, warning := range trustErr.ValidationWarnings {
			fmt.Fprintf(output, "- %s\n", terminalSafe(warning))
		}
	}
	fmt.Fprintf(output,
		"Subject: %s\nIssuer: %s\nValid: %s to %s\nSHA-256: %s\n",
		terminalSafe(certificate.Subject), terminalSafe(certificate.Issuer),
		certificate.NotBefore.Local().Format(time.RFC3339), certificate.NotAfter.Local().Format(time.RFC3339),
		displayFingerprint(certificate.Fingerprint),
	)
	fmt.Fprint(output, "Trust this observed certificate and pin it for this NAS? [y/N]: ")
	answer, readErr := bufio.NewReader(input).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if readErr != nil && answer == "" {
		return config.Profile{}, fmt.Errorf("certificate for NAS %q was not trusted; provisioning did not start: %w", name, readErr)
	}
	if answer != "y" && answer != "yes" {
		return config.Profile{}, fmt.Errorf("certificate for NAS %q was not trusted; provisioning did not start", name)
	}
	profile.InsecureSkipTLSVerify = false
	profile.TLSMode = "pinned_fingerprint"
	profile.CertificateFingerprint = certificate.Fingerprint
	fmt.Fprintln(output, "Pinned the observed certificate.")
	return profile, nil
}
