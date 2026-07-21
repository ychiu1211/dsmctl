package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
	"github.com/ychiu1211/dsmctl/internal/synology"
	"github.com/ychiu1211/dsmctl/internal/tlstrust"
)

// The terminal probes and the hidden reader are indirected so tests can
// simulate interactive and non-interactive invocations without a real TTY.
var (
	stdinIsTerminal  = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	stdoutIsTerminal = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }
	readHiddenLine   = func() (string, error) {
		value, err := term.ReadPassword(int(os.Stdin.Fd()))
		return string(value), err
	}
)

const passwordVerifyTimeout = 30 * time.Second

func newAuthPasswordCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "password",
		Short: "Manage the DSM account password stored in the OS credential store",
		Long: "Store, remove, or reveal a DSM account password kept in the OS\n" +
			"credential store (Windows Credential Manager, macOS Keychain, or the\n" +
			"Linux Secret Service). A stored password lets dsmctl sign in again\n" +
			"automatically when no web-login session can be resumed, without keeping\n" +
			"the password in an environment variable.",
	}
	command.AddCommand(
		newAuthPasswordSetCommand(opts),
		newAuthPasswordRemoveCommand(opts),
		newAuthPasswordRevealCommand(opts),
	)
	return command
}

func newAuthPasswordSetCommand(opts *options) *cobra.Command {
	var account string
	var otp string
	var passwordStdin bool
	command := &cobra.Command{
		Use:   "set",
		Short: "Verify a DSM password against the NAS and store it",
		Long: "Ask for the DSM account password (hidden prompt), verify it by signing\n" +
			"in to the NAS once (answering an OTP challenge registers this machine as\n" +
			"a trusted device), and only then store it in the OS credential store.\n" +
			"With --password-stdin the password is read from standard input instead,\n" +
			"for automation; that path never prompts, so the NAS certificate must\n" +
			"already be trusted and OTP (if required) must come from --otp.",
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
			accountName := strings.TrimSpace(account)
			if accountName == "" {
				accountName = strings.TrimSpace(profile.Username)
			}
			if accountName == "" {
				return fmt.Errorf("NAS %q has no DSM account name; pass --account or set one with 'dsmctl nas add'", name)
			}
			var password string
			if passwordStdin {
				profile, err = requireNonInteractiveTLS(cmd.Context(), name, profile)
				if err != nil {
					return err
				}
				password, err = readPasswordFromStdin(cmd.InOrStdin())
			} else {
				if !stdinIsTerminal() {
					return errors.New("standard input is not a terminal; use --password-stdin to pipe the password")
				}
				profile, err = prepareWebLoginTLS(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), store, cfg, name, profile)
				if err != nil {
					return err
				}
				password, err = promptHiddenPassword(cmd.ErrOrStderr(), accountName, name)
			}
			if err != nil {
				return err
			}
			device, err := verifyDSMPassword(cmd, name, profile, accountName, password, otp, passwordStdin)
			if err != nil {
				return err
			}
			secrets := credentials.NewSecureStore()
			if err := secrets.SavePassword(cmd.Context(), name, password); err != nil {
				return err
			}
			if device.ID != "" {
				if err := secrets.SaveTrustedDevice(cmd.Context(), name, device); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: the password was stored, but the trusted device could not be saved: %v\n", err)
				}
			}
			if accountName != profile.Username {
				profile.Username = accountName
				cfg.NAS[name] = profile
				if err := store.Save(cfg); err != nil {
					return fmt.Errorf("the password was stored, but the account name could not be saved to the configuration: %w", err)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Verified and stored the password for account %q on NAS %q in the OS credential store.\n", accountName, name)
			if device.ID != "" {
				fmt.Fprintln(cmd.OutOrStdout(), "This machine is now a trusted device, so future sign-ins skip the OTP.")
			}
			return nil
		},
	}
	command.Flags().StringVar(&account, "account", "", "DSM account name (defaults to the profile's configured username)")
	command.Flags().StringVar(&otp, "otp", "", "one-time password for accounts with 2-step verification")
	command.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the password from standard input (for automation)")
	return command
}

// requireNonInteractiveTLS verifies the NAS certificate without prompting.
// Automation cannot answer a trust question, so an untrusted certificate is
// an error that points at the interactive commands which can pin it.
func requireNonInteractiveTLS(ctx context.Context, name string, profile config.Profile) (config.Profile, error) {
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
	if errors.As(err, &trustErr) {
		return config.Profile{}, fmt.Errorf("the certificate for NAS %q is not trusted; run 'dsmctl auth password set --nas %s' interactively once to review and pin it", name, name)
	}
	return config.Profile{}, fmt.Errorf("verify TLS certificate for NAS %q: %w", name, err)
}

func readPasswordFromStdin(input io.Reader) (string, error) {
	value, err := io.ReadAll(io.LimitReader(input, 4096))
	if err != nil {
		return "", fmt.Errorf("read password from standard input: %w", err)
	}
	password := strings.TrimRight(string(value), "\r\n")
	if password == "" {
		return "", errors.New("standard input contained no password")
	}
	return password, nil
}

func promptHiddenPassword(prompt io.Writer, account, nas string) (string, error) {
	fmt.Fprintf(prompt, "DSM password for account %q on NAS %q (input hidden): ", account, nas)
	password, err := readHiddenLine()
	fmt.Fprintln(prompt)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if password == "" {
		return "", errors.New("no password was entered")
	}
	return password, nil
}

// verifyDSMPassword signs in to the NAS once with the candidate password so
// only a working credential is ever persisted. An OTP challenge registers a
// trusted device whose ID is returned for storage.
func verifyDSMPassword(cmd *cobra.Command, name string, profile config.Profile, account, password, otp string, nonInteractive bool) (credentials.TrustedDevice, error) {
	device := credentials.TrustedDevice{Name: runtime.DefaultDeviceName()}
	if existing, err := credentials.NewSecureStore().TrustedDevice(cmd.Context(), name); err == nil && existing.ID != "" {
		device = existing
	}
	registered := credentials.TrustedDevice{}
	client, err := synology.NewClient(synology.Options{
		BaseURL:    profile.URL,
		Username:   account,
		Password:   password,
		DeviceName: device.Name,
		DeviceID:   device.ID,
		HTTPClient: runtime.HTTPClient(profile),
		OTPProvider: func(context.Context) (string, error) {
			if otp != "" {
				return otp, nil
			}
			if nonInteractive || !stdinIsTerminal() {
				return "", errors.New("the account requires a one-time password; pass --otp")
			}
			fmt.Fprint(cmd.ErrOrStderr(), "One-time password (OTP): ")
			line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if err != nil && strings.TrimSpace(line) == "" {
				return "", fmt.Errorf("read OTP: %w", err)
			}
			return strings.TrimSpace(line), nil
		},
		SaveDeviceID: func(_ context.Context, id string) error {
			registered = credentials.TrustedDevice{Name: device.Name, ID: id}
			return nil
		},
	})
	if err != nil {
		return credentials.TrustedDevice{}, fmt.Errorf("create client for NAS %q: %w", name, err)
	}
	verifyCtx, cancel := context.WithTimeout(cmd.Context(), passwordVerifyTimeout)
	defer cancel()
	err = client.Authenticate(verifyCtx)
	closeCtx, closeCancel := context.WithTimeout(context.Background(), passwordVerifyTimeout)
	_ = client.Close(closeCtx)
	closeCancel()
	if err != nil {
		return credentials.TrustedDevice{}, fmt.Errorf("DSM rejected the password for account %q on NAS %q: %w", account, name, err)
	}
	return registered, nil
}

func newAuthPasswordRemoveCommand(opts *options) *cobra.Command {
	var removeTrustedDevice bool
	command := &cobra.Command{
		Use:   "remove",
		Short: "Remove the stored DSM password",
		Long: "Delete the DSM password stored for one NAS profile from the OS\n" +
			"credential store. Removing an entry that does not exist is not an\n" +
			"error. The trusted-device registration is kept unless\n" +
			"--trusted-device is also given.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, err := resolveCredentialProfileName(opts)
			if err != nil {
				return err
			}
			secrets := credentials.NewSecureStore()
			removed, err := secrets.DeletePassword(cmd.Context(), name)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if removed {
				fmt.Fprintf(out, "Removed the stored password for NAS %q from the OS credential store.\n", name)
			} else {
				fmt.Fprintf(out, "No password was stored for NAS %q.\n", name)
			}
			if removeTrustedDevice {
				deviceRemoved, err := secrets.DeleteTrustedDevice(cmd.Context(), name)
				if err != nil {
					return err
				}
				if deviceRemoved {
					fmt.Fprintf(out, "Removed the trusted-device registration for NAS %q; the next password sign-in will ask for an OTP again.\n", name)
				} else {
					fmt.Fprintf(out, "No trusted device was stored for NAS %q.\n", name)
				}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&removeTrustedDevice, "trusted-device", false, "also remove the trusted-device registration")
	return command
}

func newAuthPasswordRevealCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "reveal",
		Short: "Print the stored DSM password (interactive terminals only)",
		Long: "Print the DSM password stored for one NAS profile in clear text.\n" +
			"Because the output is a secret, the command runs only when standard\n" +
			"input and output are both interactive terminals (no pipes, files, or\n" +
			"captured sessions) and asks you to retype the NAS name first. The\n" +
			"reveal shows the stored entry only; environment-variable fallbacks are\n" +
			"never printed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, err := resolveCredentialProfileName(opts)
			if err != nil {
				return err
			}
			if !stdinIsTerminal() || !stdoutIsTerminal() {
				return errors.New("'auth password reveal' prints a secret and runs only on an interactive terminal; redirection and pipes are refused")
			}
			secrets := credentials.NewSecureStore()
			password, err := secrets.StoredPassword(cmd.Context(), name)
			if errors.Is(err, credentials.ErrNoStoredPassword) {
				return fmt.Errorf("no password is stored for NAS %q; store one with 'dsmctl auth password set --nas %s'", name, name)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(),
				"This prints the DSM password stored for NAS %q in clear text.\nType the NAS name to confirm: ", name)
			line, readErr := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if strings.TrimSpace(line) != name {
				if readErr != nil {
					return fmt.Errorf("confirmation was not read; nothing was revealed: %w", readErr)
				}
				return errors.New("confirmation did not match the NAS name; nothing was revealed")
			}
			fmt.Fprintln(cmd.OutOrStdout(), password)
			return nil
		},
	}
	return command
}

// resolveCredentialProfileName resolves the target profile like other auth
// commands, but keeps an explicitly named NAS usable even when it is no
// longer configured, so credentials orphaned by 'nas remove --keep-credentials'
// stay removable and revealable.
func resolveCredentialProfileName(opts *options) (string, error) {
	cfg, err := config.NewStore(opts.configPath).Load()
	if err != nil {
		return "", err
	}
	name, _, err := cfg.Resolve(opts.nas)
	if err == nil {
		return name, nil
	}
	requested := strings.TrimSpace(opts.nas)
	if requested == "" {
		return "", err
	}
	if nameErr := config.ValidateName(requested); nameErr != nil {
		return "", fmt.Errorf("invalid NAS name %q: %w", requested, nameErr)
	}
	return requested, nil
}
