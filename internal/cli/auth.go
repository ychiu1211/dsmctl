package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
	"github.com/ychiu1211/dsmctl/internal/tlstrust"
	"github.com/ychiu1211/dsmctl/internal/weblogin"
)

func newAuthCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "Manage DSM authentication"}
	command.AddCommand(
		newAuthLoginCommand(opts),
		newAuthStatusCommand(opts),
		newAuthLogoutCommand(opts),
		newAuthPasswordCommand(opts),
		newAuthRevealPasswordCommand(opts),
	)
	return command
}

func newAuthLoginCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Sign in to a NAS through your web browser",
		Long: "Open the NAS's own sign-in page in your web browser and store the\n" +
			"resulting DSM session. The password (and any two-factor, passkey, or\n" +
			"approve-sign-in step) is entered only in the browser against the NAS;\n" +
			"dsmctl never handles it. The stored session and its renewal keys are\n" +
			"reused by later commands.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store := config.NewStore(opts.configPath)
			cfg, err := store.Load()
			if err != nil {
				return err
			}
			name, profile, err := cfg.Resolve(opts.nas)
			if err != nil {
				return err
			}
			profile, err = prepareWebLoginTLS(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), store, cfg, name, profile)
			if err != nil {
				return err
			}
			result, err := weblogin.Login(cmd.Context(), profile.URL, weblogin.Options{
				HTTPClient: runtime.HTTPClient(profile),
				Prompt:     cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			now := time.Now()
			session := credentials.SessionCredential{
				SID:             result.SID,
				SynoToken:       result.SynoToken,
				DeviceID:        result.DeviceID,
				ServerPublicKey: result.ServerPublicKey,
				LocalPublicKey:  result.LocalPublicKey,
				LocalPrivateKey: result.LocalPrivateKey,
				Account:         result.Account,
				IssuedAt:        now,
				LastVerified:    now,
			}
			if err := credentials.NewSecureStore().SaveSession(cmd.Context(), name, session); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Signed in to NAS %q as %s. The session is stored in the OS credential store.\n", name, result.Account)
			return nil
		},
	}
}

func prepareWebLoginTLS(ctx context.Context, input io.Reader, output io.Writer, store *config.Store, cfg *config.Config, name string, profile config.Profile) (config.Profile, error) {
	if profile.InsecureSkipTLSVerify {
		return profile, nil
	}
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
		return config.Profile{}, fmt.Errorf("verify TLS certificate for NAS %q before web login: %w", name, err)
	}
	certificate := trustErr.Certificate
	fmt.Fprintf(output, "NAS %q presented a certificate that requires explicit trust.\n", name)
	if trustErr.Code == tlstrust.CodePinMismatch {
		fmt.Fprintf(output, "The certificate changed and no longer matches the stored pin.\nPreviously pinned: %s\n", displayFingerprint(trustErr.ExpectedFingerprint))
	} else {
		fmt.Fprintln(output, "The certificate did not pass normal verification.")
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
		return config.Profile{}, fmt.Errorf("certificate for NAS %q was not trusted; web login did not start: %w", name, readErr)
	}
	if answer != "y" && answer != "yes" {
		return config.Profile{}, fmt.Errorf("certificate for NAS %q was not trusted; web login did not start", name)
	}
	profile.InsecureSkipTLSVerify = false
	profile.TLSMode = "pinned_fingerprint"
	profile.CertificateFingerprint = certificate.Fingerprint
	cfg.NAS[name] = profile
	if err := store.Save(cfg); err != nil {
		return config.Profile{}, fmt.Errorf("save pinned certificate for NAS %q: %w", name, err)
	}
	fmt.Fprintln(output, "Pinned the observed certificate. Your browser may still show its own warning for a self-signed DSM page.")
	return profile, nil
}

func displayFingerprint(value string) string {
	value = strings.ToUpper(strings.ReplaceAll(value, ":", ""))
	parts := make([]string, 0, len(value)/2)
	for len(value) >= 2 {
		parts = append(parts, value[:2])
		value = value[2:]
	}
	return strings.Join(parts, ":")
}

func terminalSafe(value string) string {
	return strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return ' '
		}
		return character
	}, value)
}

func newAuthStatusCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "status",
		Short: "Show stored session status without revealing secrets",
		Long: "Report, per NAS profile, whether a web-login session is stored in the OS\n" +
			"credential store and whether it can be renewed. The command is fully\n" +
			"offline: it never reveals secrets and never contacts a NAS.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetAuthStatus(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeAuthStatus(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newAuthLogoutCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "logout",
		Short: "Sign out of a NAS and remove the stored session",
		Long: "Ask DSM to revoke the stored web-login session, then delete it (and its\n" +
			"renewal keys) for one NAS profile from the OS credential store. The\n" +
			"revocation is best-effort: when the NAS is unreachable, or an explicitly\n" +
			"named profile no longer exists in the configuration (a session left\n" +
			"behind by an earlier 'nas remove'), the local copy is still removed and\n" +
			"the DSM session lapses on its own expiry. Sign in again with\n" +
			"'dsmctl auth login'.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.Logout(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			writeLogoutResult(cmd, result)
			return nil
		},
	}
	return command
}

// writeLogoutResult narrates a sign-out outcome. Revocation problems go to
// stderr so scripts reading stdout still see the primary outcome line.
func writeLogoutResult(cmd *cobra.Command, result application.LogoutResult) {
	if result.RevocationError != "" {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"Warning: could not revoke the DSM session on NAS %q: %s\n"+
				"The stored copy is removed anyway; the DSM session stays valid until it expires.\n",
			result.NAS, result.RevocationError)
	}
	out := cmd.OutOrStdout()
	switch {
	case result.Revoked && result.Removed:
		fmt.Fprintf(out, "Signed out of NAS %q: DSM accepted the sign-out and the stored session was removed.\n", result.NAS)
	case result.Revoked:
		fmt.Fprintf(out, "Signed out of NAS %q: DSM accepted the sign-out; no stored session remained to remove.\n", result.NAS)
	case result.Removed:
		fmt.Fprintf(out, "Removed the stored session for NAS %q.\n", result.NAS)
		if !result.Configured {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"NAS %q is not configured, so its URL is unknown and the DSM session was not revoked server-side; it stays valid until it expires.\n",
				result.NAS)
		}
	default:
		fmt.Fprintf(out, "No session was stored for NAS %q.\n", result.NAS)
	}
}

func writeAuthStatus(cmd *cobra.Command, result application.AuthStatusResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "DEFAULT\tNAME\tSESSION\tRENEWABLE\tPASSWORD\tACCOUNT")
	for _, status := range result.Statuses {
		marker := ""
		if status.Default {
			marker = "*"
		}
		session := storedOrNone(status.SessionStored)
		renewable := yesNo(status.SessionRenewable)
		if !status.SessionStored {
			renewable = "-"
		}
		password := passwordSourceLabel(status)
		if status.StoreError != "" {
			session, renewable, password = "error", "error", "error"
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", marker, status.NAS, session, renewable, password, status.Account)
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	for _, status := range result.Statuses {
		if status.StoreError != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "NAS %q credential store error: %s\n", status.NAS, status.StoreError)
		}
	}
	return nil
}

func storedOrNone(stored bool) string {
	if stored {
		return "stored"
	}
	return "none"
}

// passwordSourceLabel names where an automatic password sign-in would come
// from: the OS credential store, the environment fallback, or nowhere.
func passwordSourceLabel(status application.AuthStatus) string {
	if status.PasswordStored {
		return "stored"
	}
	if status.PasswordEnvSet {
		return "env:" + status.PasswordEnv
	}
	return "none"
}
