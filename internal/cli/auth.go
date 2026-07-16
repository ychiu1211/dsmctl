package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

func newAuthCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "Authenticate to DSM securely"}
	command.AddCommand(newAuthLoginCommand(opts))
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
			service := application.NewService(cfg, manager)
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
