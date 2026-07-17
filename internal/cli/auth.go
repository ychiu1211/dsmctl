package cli

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
	"github.com/ychiu1211/dsmctl/internal/weblogin"
)

func newAuthCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "Manage DSM authentication"}
	command.AddCommand(
		newAuthLoginCommand(opts),
		newAuthStatusCommand(opts),
		newAuthLogoutCommand(opts),
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
			cfg, err := config.NewStore(opts.configPath).Load()
			if err != nil {
				return err
			}
			name, profile, err := cfg.Resolve(opts.nas)
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
			service, err := loadService(opts.configPath)
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
		Short: "Remove the stored session for a NAS",
		Long: "Delete the stored web-login session (and its renewal keys) for one NAS\n" +
			"profile from the OS credential store. An explicitly named profile does\n" +
			"not need to exist in the configuration, so a session left behind by an\n" +
			"earlier 'nas remove' can still be cleaned up. Removal is local: DSM\n" +
			"sessions held by other running dsmctl processes stay valid until they\n" +
			"exit. Sign in again with 'dsmctl auth login'.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.NewStore(opts.configPath).Load()
			if err != nil {
				return err
			}
			name := opts.nas
			if name == "" {
				resolved, _, resolveErr := cfg.Resolve("")
				if resolveErr != nil {
					return resolveErr
				}
				name = resolved
			} else if err := config.ValidateName(name); err != nil {
				return fmt.Errorf("invalid NAS name %q: %w", name, err)
			}
			secrets := credentials.NewSecureStore()
			removed, err := secrets.DeleteSession(cmd.Context(), name)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if removed {
				fmt.Fprintf(out, "Removed the stored session for NAS %q.\n", name)
			} else {
				fmt.Fprintf(out, "No session was stored for NAS %q.\n", name)
			}
			fmt.Fprintln(out, "Running dsmctl processes keep their in-memory session until they exit.")
			return nil
		},
	}
	return command
}

func writeAuthStatus(cmd *cobra.Command, result application.AuthStatusResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "DEFAULT\tNAME\tSESSION\tRENEWABLE\tACCOUNT")
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
		if status.StoreError != "" {
			session, renewable = "error", "error"
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", marker, status.NAS, session, renewable, status.Account)
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
