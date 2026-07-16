package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

func newAuthCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "Manage DSM authentication credentials securely"}
	command.AddCommand(
		newAuthLoginCommand(opts),
		newAuthStatusCommand(opts),
		newAuthLogoutCommand(opts),
		newAuthRotateDeviceCommand(opts),
	)
	return command
}

func newAuthLoginCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Verify and save a DSM password and trusted device",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.NewStore(opts.configPath).Load()
			if err != nil {
				return err
			}
			name, profile, err := cfg.Resolve(opts.nas)
			if err != nil {
				return err
			}
			password, err := promptSecret(cmd, fmt.Sprintf("Password for %s on NAS %q: ", profile.Username, name))
			if err != nil {
				return err
			}

			secrets := credentials.NewSecureStore()
			resolver := credentials.WithPassword(secrets, name, password)
			manager := runtime.NewManager(
				cfg,
				resolver,
				runtime.WithDeviceStore(secrets),
				runtime.WithOTPProvider(terminalOTPProvider(cmd)),
			)
			service := application.NewService(cfg, manager, application.WithCredentialStore(secrets))
			defer func() {
				closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = service.Close(closeCtx)
			}()

			result, err := service.Authenticate(cmd.Context(), name)
			if err != nil {
				return err
			}
			if err := secrets.SavePassword(cmd.Context(), name, password); err != nil {
				return err
			}
			password = ""
			fmt.Fprintf(cmd.OutOrStdout(), "Authenticated to NAS %q. Password and any DSM trusted-device credential are stored in the OS credential store.\n", result.NAS)
			return nil
		},
	}
}

func newAuthStatusCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "status",
		Short: "Show stored credential status without revealing secrets",
		Long: "Report, per NAS profile, whether a password and a DSM trusted-device\n" +
			"credential are stored in the OS credential store, plus the password\n" +
			"environment variable name and whether it is set. The command is fully\n" +
			"offline: it never resolves passwords and never contacts a NAS.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
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
	var passwordOnly, deviceOnly bool
	command := &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored password and trusted device for a NAS (default: both)",
		Long: "Remove stored credentials for one NAS profile from the OS credential store.\n" +
			"By default both the password and the DSM trusted-device credential are\n" +
			"removed; --password or --trusted-device narrows the scope. An explicitly\n" +
			"named profile does not need to exist in the configuration, so credentials\n" +
			"left behind by an earlier 'nas remove' can still be cleaned up. Removal is\n" +
			"local and reversible with 'dsmctl auth login': DSM sessions held by other\n" +
			"running dsmctl processes stay valid until those processes exit.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scope := application.CredentialScope{Password: true, TrustedDevice: true}
			if passwordOnly || deviceOnly {
				scope = application.CredentialScope{Password: passwordOnly, TrustedDevice: deviceOnly}
			}
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.RemoveCredentials(cmd.Context(), opts.nas, scope)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if scope.Password {
				if result.PasswordRemoved {
					fmt.Fprintf(out, "Removed the stored password for NAS %q.\n", result.NAS)
				} else {
					fmt.Fprintf(out, "No password was stored for NAS %q.\n", result.NAS)
				}
			}
			if scope.TrustedDevice {
				if result.TrustedDeviceRemoved {
					fmt.Fprintf(out, "Removed the stored trusted-device credential for NAS %q.\n", result.NAS)
				} else {
					fmt.Fprintf(out, "No trusted-device credential was stored for NAS %q.\n", result.NAS)
				}
			}
			fmt.Fprintln(out, "Running dsmctl processes keep their in-memory credentials and DSM sessions until they exit.")
			if scope.Password {
				fmt.Fprintln(out, "A set password environment variable still enables non-interactive login; check 'dsmctl auth status'.")
			}
			return nil
		},
	}
	command.Flags().BoolVar(&passwordOnly, "password", false, "remove only the stored password")
	command.Flags().BoolVar(&deviceOnly, "trusted-device", false, "remove only the stored trusted-device credential")
	return command
}

func newAuthRotateDeviceCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "rotate-device",
		Short: "Replace the DSM trusted-device credential by re-authenticating",
		Long: "Delete the stored trusted-device credential, then log in again using the\n" +
			"stored password or the password environment variable. On a 2FA-protected\n" +
			"account DSM prompts for an OTP and issues a new trusted-device ID, which is\n" +
			"saved to the OS credential store. The old device entry may remain listed in\n" +
			"DSM Personal > Security and can be revoked there. If login fails after the\n" +
			"deletion, run 'dsmctl auth login' to recover.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.NewStore(opts.configPath).Load()
			if err != nil {
				return err
			}
			name, _, err := cfg.Resolve(opts.nas)
			if err != nil {
				return err
			}
			secrets := credentials.NewSecureStore()
			removed, err := secrets.DeleteTrustedDevice(cmd.Context(), name)
			if err != nil {
				return err
			}
			if removed {
				fmt.Fprintf(cmd.OutOrStdout(), "Removed the stored trusted device for NAS %q.\n", name)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "No trusted device was stored for NAS %q; authenticating to obtain one.\n", name)
			}
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.Authenticate(cmd.Context(), name)
			if err != nil {
				return fmt.Errorf("re-authentication failed and the previous trusted device is no longer stored; run 'dsmctl auth login --nas %s' to recover: %w", name, err)
			}
			stored, err := secrets.HasTrustedDevice(cmd.Context(), name)
			if err != nil {
				return err
			}
			if stored {
				fmt.Fprintf(cmd.OutOrStdout(), "Authenticated to NAS %q and stored a new trusted-device credential. The old entry may remain in DSM Personal > Security until revoked there.\n", result.NAS)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Authenticated to NAS %q. DSM did not issue a trusted-device credential; two-factor authentication may be disabled for this account.\n", result.NAS)
			}
			return nil
		},
	}
}

func writeAuthStatus(cmd *cobra.Command, result application.AuthStatusResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "DEFAULT\tNAME\tPASSWORD\tTRUSTED DEVICE\tPASSWORD ENV\tENV SET")
	for _, status := range result.Statuses {
		marker := ""
		if status.Default {
			marker = "*"
		}
		password, device := storedOrNone(status.PasswordStored), storedOrNone(status.TrustedDeviceStored)
		if status.StoreError != "" {
			password, device = "error", "error"
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", marker, status.NAS, password, device, status.PasswordEnv, yesNo(status.PasswordEnvSet))
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
