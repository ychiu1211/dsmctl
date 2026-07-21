package cli

import (
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newDSMUpdateCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "dsm-update",
		Short: "Inspect DSM Update & Restore status, auto-update policy, and configuration backup",
	}
	command.AddCommand(
		newDSMUpdateCapabilitiesCommand(opts),
		newDSMUpdateStatusCommand(opts),
		newDSMUpdateAvailableCommand(opts),
		newDSMUpdatePolicyCommand(opts),
		newDSMUpdateConfigBackupCommand(opts),
	)
	return command
}

func newDSMUpdateCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show which Update & Restore areas can be read and the selected backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDSMUpdateCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Update status read:\t%s\n", yesNo(result.Capabilities.Status))
			fmt.Fprintf(writer, "Available-update read:\t%s\n", yesNo(result.Capabilities.Available))
			fmt.Fprintf(writer, "Auto-update policy read:\t%s\n", yesNo(result.Capabilities.Policy))
			fmt.Fprintf(writer, "Configuration backup read:\t%s\n", yesNo(result.Capabilities.ConfigBackup))
			fmt.Fprintln(writer, "\nOPERATION\tSUPPORTED\tBACKEND\tDSM API")
			for _, operation := range result.Report.Operations {
				api := "-"
				if operation.API != "" {
					api = fmt.Sprintf("%s v%d", operation.API, operation.Version)
				}
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), api)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDSMUpdateStatusCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "status",
		Short: "Show the installed DSM version and local update state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDSMUpdateStatus(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Installed DSM version:\t%s\n", valueOrDash(result.Status.InstalledVersion))
			fmt.Fprintf(writer, "Upgrade allowed:\t%s\n", yesNo(result.Status.AllowUpgrade))
			fmt.Fprintf(writer, "Update state:\t%s\n", valueOrDash(result.Status.State))
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDSMUpdateAvailableCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "available",
		Short: "Check the update server for an offered DSM update (a network egress to Synology)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDSMUpdateAvailable(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			if !result.Available.Checked {
				fmt.Fprintf(writer, "Update available:\t(unknown — update server unreachable)\n")
				return writer.Flush()
			}
			fmt.Fprintf(writer, "Update available:\t%s\n", yesNo(result.Available.Available))
			if result.Available.RSSResult != "" {
				fmt.Fprintf(writer, "Feed result:\t%s\n", result.Available.RSSResult)
			}
			if len(result.Available.Details) > 0 {
				keys := make([]string, 0, len(result.Available.Details))
				for key := range result.Available.Details {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				fmt.Fprintln(writer, "\nOFFERED UPDATE\tVALUE")
				for _, key := range keys {
					fmt.Fprintf(writer, "%s\t%s\n", key, result.Available.Details[key])
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDSMUpdatePolicyCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "policy",
		Short: "Show the DSM auto-update policy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDSMUpdatePolicy(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			policy := result.Policy
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Automatic update:\t%s\n", optionalBool(policy.AutoUpdateEnabled))
			fmt.Fprintf(writer, "Auto-update type:\t%s\n", valueOrDash(policy.AutoUpdateType))
			fmt.Fprintf(writer, "Automatic download:\t%s\n", optionalBool(policy.AutoDownload))
			fmt.Fprintf(writer, "Update channel:\t%s\n", valueOrDash(policy.UpgradeType))
			fmt.Fprintf(writer, "Small (nano) updates:\t%s\n", optionalBool(policy.SmartNanoEnabled))
			if policy.Schedule != nil {
				window := fmt.Sprintf("%02d:%02d", policy.Schedule.Hour, policy.Schedule.Minute)
				if policy.Schedule.WeekDay != "" {
					window += fmt.Sprintf(" (week day %s)", policy.Schedule.WeekDay)
				}
				fmt.Fprintf(writer, "Maintenance window:\t%s\n", window)
			} else {
				fmt.Fprintf(writer, "Maintenance window:\t%s\n", valueOrDash(""))
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDSMUpdateConfigBackupCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "config-backup",
		Short: "Show the DSM configuration-backup status and history",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDSMUpdateConfigBackup(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			backup := result.ConfigBackup
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Scheduled backup enabled:\t%s\n", yesNo(backup.Enabled))
			fmt.Fprintf(writer, "Destination account:\t%s\n", valueOrDash(backup.Account))
			fmt.Fprintf(writer, "Encryption method:\t%s\n", valueOrDash(backup.EncryptionMethod))
			fmt.Fprintf(writer, "Last backup status:\t%s\n", valueOrDash(backup.LastStatus))
			fmt.Fprintf(writer, "Stored versions:\t%d\n", len(backup.Versions))
			if len(backup.Versions) > 0 {
				fmt.Fprintln(writer, "\nBACKUP TIME\tDSM VERSION\tHOST\tMODEL")
				for _, version := range backup.Versions {
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
						valueOrDash(version.BackupTime), valueOrDash(version.DSMVersion),
						valueOrDash(version.Host), valueOrDash(version.Model))
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

// optionalBool renders a tri-state boolean: yes/no when DSM reported the field,
// or "(not reported)" when this DSM version does not expose it.
func optionalBool(value *bool) string {
	if value == nil {
		return "(not reported)"
	}
	return yesNo(*value)
}
