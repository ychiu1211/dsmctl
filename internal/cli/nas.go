package cli

import (
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

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
			if profile.PasswordEnv == "" {
				profile.PasswordEnv = credentials.DefaultEnvironmentVariable(name)
			}
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
			fmt.Fprintf(cmd.OutOrStdout(), "Saved NAS %q in %s\nRun 'dsmctl auth login --nas %s' to authenticate, or set %s for automation.\n", name, store.Path(), name, profile.PasswordEnv)
			return nil
		},
	}
	command.Flags().StringVar(&profile.URL, "url", "", "DSM base URL, for example https://nas.example.com:5001")
	command.Flags().StringVar(&profile.Username, "username", "", "DSM account name")
	command.Flags().StringVar(&profile.PasswordEnv, "password-env", "", "environment variable containing the DSM password")
	command.Flags().BoolVar(&profile.InsecureSkipTLSVerify, "insecure-skip-tls-verify", false, "accept an untrusted TLS certificate (unsafe)")
	command.Flags().IntVar(&profile.TimeoutSeconds, "timeout", 30, "request timeout in seconds")
	command.Flags().BoolVar(&makeDefault, "default", false, "make this the default NAS")
	_ = command.MarkFlagRequired("url")
	_ = command.MarkFlagRequired("username")
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
			fmt.Fprintln(writer, "DEFAULT\tNAME\tURL\tUSERNAME\tPASSWORD ENV\tTLS VERIFY")
			for _, summary := range cfg.Summaries(credentials.DefaultEnvironmentVariable) {
				marker := ""
				if summary.Default {
					marker = "*"
				}
				tlsVerify := "enabled"
				if summary.InsecureSkipTLSVerify {
					tlsVerify = "DISABLED"
				}
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", marker, summary.Name, summary.URL, summary.Username, summary.PasswordEnv, tlsVerify)
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
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a NAS profile",
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
			fmt.Fprintf(cmd.OutOrStdout(), "Removed NAS %q.\n", name)
			return nil
		},
	}
}
