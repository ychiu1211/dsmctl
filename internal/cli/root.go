package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/config"
)

type options struct {
	configPath string
	nas        string
	logLevel   string
}

func New(version string) *cobra.Command {
	opts := &options{}
	root := &cobra.Command{
		Use:           "dsmctl",
		Short:         "Manage one or more Synology NAS devices",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		// Stamp a per-invocation correlation id so all of a command's DSM calls
		// share one id in the diagnostic log (when logging is enabled).
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SetContext(withCorrelationID(cmd.Context()))
			return nil
		},
	}
	root.PersistentFlags().StringVar(&opts.configPath, "config", config.DefaultPath(), "configuration file path")
	root.PersistentFlags().StringVar(&opts.nas, "nas", "", "NAS profile name (defaults to the configured default)")
	root.PersistentFlags().StringVar(&opts.logLevel, "log-level", "", "diagnostic log level: debug, info, warn, or error (default: off; also DSMCTL_LOG_LEVEL)")
	root.AddCommand(
		newAccessCommand(opts),
		newAccountCommand(opts),
		newAccountProtectionCommand(opts),
		newAuthCommand(opts),
		newBackupCommand(opts),
		newControlPanelCommand(opts),
		newDiscoverCommand(opts),
		newDiskSMARTCommand(opts),
		newDownloadCommand(opts),
		newDSMUpdateCommand(opts),
		newCertificateCommand(opts),
		newDriveCommand(opts),
		newExternalAccessCommand(opts),
		newFileCommand(opts),
		newHardwareCommand(opts),
		newInstallCommand(opts),
		newFirewallCommand(opts),
		newLogCommand(opts),
		newNASCommand(opts),
		newNetworkCommand(opts),
		newNotificationCommand(opts),
		newOfficeCommand(opts),
		newPackageCommand(opts),
		newPhotoCommand(opts),
		newProvisionCommand(opts),
		newResourceMonitorCommand(opts),
		newSANCommand(opts),
		newShareCommand(opts),
		newStorageCommand(opts),
		newSurveillanceCommand(opts),
		newSystemCommand(opts),
		newSecurityAdvisorCommand(opts),
		newTaskSchedulerCommand(opts),
		newUniversalSearchCommand(opts),
	)
	return root
}

func Execute(ctx context.Context, version string) error {
	return New(version).ExecuteContext(ctx)
}
