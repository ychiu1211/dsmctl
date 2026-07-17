package cli

import (
	"errors"
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
)

func newNASCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "nas", Short: "Manage NAS connection profiles"}
	command.AddCommand(
		newNASAddCommand(opts),
		newNASCapabilitiesCommand(opts),
		newNASListCommand(opts),
		newNASUseCommand(opts),
		newNASRemoveCommand(opts),
	)
	return command
}

func newNASAddCommand(opts *options) *cobra.Command {
	var profile config.Profile
	var makeDefault bool
	command := &cobra.Command{
		Use:   "add <name>",
		Short: "Add or update a NAS profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			store := config.NewStore(opts.configPath)
			cfg, err := store.Load()
			if err != nil {
				return err
			}
			cfg.NAS[name] = profile
			if makeDefault || cfg.DefaultNAS == "" {
				cfg.DefaultNAS = name
			}
			if err := store.Save(cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved NAS %q in %s\nRun 'dsmctl auth login --nas %s' to sign in through your web browser.\n", name, store.Path(), name)
			return nil
		},
	}
	command.Flags().StringVar(&profile.URL, "url", "", "DSM base URL, for example https://nas.example.com:5001")
	command.Flags().StringVar(&profile.Username, "username", "", "DSM account name (optional; only used for display)")
	command.Flags().BoolVar(&profile.InsecureSkipTLSVerify, "insecure-skip-tls-verify", false, "accept an untrusted TLS certificate (unsafe)")
	command.Flags().IntVar(&profile.TimeoutSeconds, "timeout", 30, "request timeout in seconds")
	command.Flags().BoolVar(&makeDefault, "default", false, "make this the default NAS")
	_ = command.MarkFlagRequired("url")
	return command
}

func newNASListCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured NAS profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.NewStore(opts.configPath).Load()
			if err != nil {
				return err
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(writer, "DEFAULT\tNAME\tURL\tUSERNAME\tTLS VERIFY")
			for _, summary := range cfg.Summaries(credentials.DefaultEnvironmentVariable) {
				marker := ""
				if summary.Default {
					marker = "*"
				}
				tlsVerify := "enabled"
				if summary.InsecureSkipTLSVerify {
					tlsVerify = "DISABLED"
				}
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", marker, summary.Name, summary.URL, summary.Username, tlsVerify)
			}
			return writer.Flush()
		},
	}
}

func newNASUseCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Select the default NAS",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := config.NewStore(opts.configPath)
			cfg, err := store.Load()
			if err != nil {
				return err
			}
			if _, ok := cfg.NAS[args[0]]; !ok {
				return fmt.Errorf("NAS profile %q is not configured", args[0])
			}
			cfg.DefaultNAS = args[0]
			if err := store.Save(cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Default NAS is now %q.\n", args[0])
			return nil
		},
	}
}

func newNASRemoveCommand(opts *options) *cobra.Command {
	var keepCredentials bool
	command := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a NAS profile and its stored session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := config.NewStore(opts.configPath)
			cfg, err := store.Load()
			if err != nil {
				return err
			}
			name := args[0]
			if _, ok := cfg.NAS[name]; !ok {
				return fmt.Errorf("NAS profile %q is not configured", name)
			}
			// The service must load the configuration while the profile still
			// exists: signing out below needs the NAS URL to revoke the DSM
			// session, and after Save the profile is gone from disk.
			var service *application.Service
			if !keepCredentials {
				service, err = loadService(opts.configPath)
				if err != nil {
					return err
				}
				defer closeService(service)
			}
			delete(cfg.NAS, name)
			if cfg.DefaultNAS == name {
				cfg.DefaultNAS = ""
				if len(cfg.NAS) == 1 {
					remaining := make([]string, 0, 1)
					for candidate := range cfg.NAS {
						remaining = append(remaining, candidate)
					}
					sort.Strings(remaining)
					cfg.DefaultNAS = remaining[0]
				}
			}
			if err := store.Save(cfg); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Removed NAS %q.\n", name)
			if keepCredentials {
				return nil
			}
			// Sign out like auth logout: revoke the DSM session server-side
			// (best-effort), then delete the stored entry.
			result, logoutErr := service.Logout(cmd.Context(), name)
			// Best-effort cleanup of any credential left by an earlier release.
			secrets := credentials.NewSecureStore()
			_, passwordErr := secrets.DeletePassword(cmd.Context(), name)
			_, deviceErr := secrets.DeleteTrustedDevice(cmd.Context(), name)
			if cleanupErr := errors.Join(logoutErr, passwordErr, deviceErr); cleanupErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not clean the OS credential store for NAS %q; run 'dsmctl auth logout --nas %s' to retry: %v\n", name, name, cleanupErr)
				return nil
			}
			if result.RevocationError != "" {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"Warning: could not revoke the DSM session on NAS %q: %s\n"+
						"The stored copy was removed; the DSM session stays valid until it expires.\n",
					name, result.RevocationError)
			}
			if result.Removed {
				if result.Revoked {
					fmt.Fprintf(out, "Signed out of NAS %q and removed the stored session from the OS credential store.\n", name)
				} else {
					fmt.Fprintf(out, "Removed the stored session for NAS %q from the OS credential store.\n", name)
				}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&keepCredentials, "keep-credentials", false, "keep the stored session in the OS credential store")
	return command
}
