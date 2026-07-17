package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/config"
)

type options struct {
	configPath string
	nas        string
}

func New(version string) *cobra.Command {
	opts := &options{}
	root := &cobra.Command{
		Use:           "dsmctl",
		Short:         "Manage one or more Synology NAS devices",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().StringVar(&opts.configPath, "config", config.DefaultPath(), "configuration file path")
	root.PersistentFlags().StringVar(&opts.nas, "nas", "", "NAS profile name (defaults to the configured default)")
	root.AddCommand(
		newAccessCommand(opts),
		newAccountCommand(opts),
		newAuthCommand(opts),
		newControlPanelCommand(opts),
		newDiscoverCommand(opts),
		newDriveCommand(opts),
		newLogCommand(opts),
		newNASCommand(opts),
		newPackageCommand(opts),
		newResourceMonitorCommand(opts),
		newSANCommand(opts),
		newShareCommand(opts),
		newStorageCommand(opts),
		newSystemCommand(opts),
	)
	return root
}

func Execute(ctx context.Context, version string) error {
	return New(version).ExecuteContext(ctx)
}
