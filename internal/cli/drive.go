package cli

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/driveadmin"
	"github.com/ychiu1211/dsmctl/internal/domain/syslog"
)

func newDriveCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "drive",
		Short: "Manage the Synology Drive Server package",
	}
	command.AddCommand(newDriveAdminCommand(opts), newDriveConfigCommand(opts))
	return command
}

func newDriveConfigCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Inspect and manage the Drive server database configuration (vmtouch)",
	}
	command.AddCommand(
		newDriveConfigStateCommand(opts),
		newDriveConfigPlanCommand(opts),
		newDriveConfigApplyCommand(opts),
	)
	return command
}

func newDriveConfigStateCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "state",
		Short: "Show the Drive server database configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDriveServerConfig(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			cfg := result.Config
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Package:\t%s %s (%s)\n", cfg.Package.ID, valueOrDash(cfg.Package.Version), packageRunState(cfg.Package.Installed, cfg.Package.Running))
			fmt.Fprintf(writer, "Database volume (read-only):\t%s\n", valueOrDash(cfg.VolumePath))
			fmt.Fprintf(writer, "vmtouch (pin DB in memory):\t%s\n", yesNo(cfg.VMTouchEnabled))
			fmt.Fprintf(writer, "vmtouch reserved memory (MB):\t%d\n", cfg.VMTouchReserveMem)
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDriveConfigPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a Drive server config patch and emit an approval plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request driveadmin.ServerConfigChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read Drive config change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanDriveConfigChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "Drive config change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newDriveConfigApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a Drive server config plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.DriveConfigPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read Drive config plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyDriveConfigPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "Drive config plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by the Drive config plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newDriveAdminCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "admin",
		Short: "Inspect the Drive Admin Console: service status, connections, team folders, and logs",
	}
	command.AddCommand(
		newDriveAdminCapabilitiesCommand(opts),
		newDriveAdminStatusCommand(opts),
		newDriveAdminConnectionsCommand(opts),
		newDriveAdminTeamFoldersCommand(opts),
		newDriveAdminLogCommand(opts),
	)
	return command
}

func newDriveAdminCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show Drive Admin operation support, selected backends, and the installed package version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDriveAdminCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeDriveAdminCapabilities(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDriveAdminStatusCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "status",
		Short: "Show the Drive service status and installed package evidence",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDriveAdminStatus(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeDriveAdminStatus(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDriveAdminConnectionsCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "connections",
		Short: "List active Drive client connections",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDriveAdminConnections(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeDriveAdminConnections(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDriveAdminTeamFoldersCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "team-folders",
		Short: "List Drive team folders from the admin perspective",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDriveAdminTeamFolders(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeDriveAdminTeamFolders(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDriveAdminLogCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "log",
		Short: "Inspect Drive server logs",
	}
	command.AddCommand(newDriveAdminLogListCommand(opts))
	return command
}

func newDriveAdminLogListCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	var limit, offset int
	var keyword, username, teamFolder, from, to string
	command := &cobra.Command{
		Use:   "list",
		Short: "List Drive server log entries with optional filters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fromTime, err := syslog.ParseTime(from)
			if err != nil {
				return err
			}
			toTime, err := syslog.ParseTime(to)
			if err != nil {
				return err
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDriveAdminLog(cmd.Context(), opts.nas, driveadmin.LogQuery{
				Limit: limit, Offset: offset, Keyword: keyword, Username: username,
				TeamFolder: teamFolder, From: fromTime, To: toTime,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeDriveAdminLog(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().IntVar(&limit, "limit", 100, "maximum number of entries to return")
	command.Flags().IntVar(&offset, "offset", 0, "number of newest entries to skip for pagination")
	command.Flags().StringVar(&keyword, "keyword", "", "substring filter applied by Drive")
	command.Flags().StringVar(&username, "username", "", "filter to one account name")
	command.Flags().StringVar(&teamFolder, "team-folder", "", "filter to one Drive team folder by shared-folder name")
	command.Flags().StringVar(&from, "from", "", "inclusive lower time bound: Unix seconds or \"2006-01-02 15:04:05\"")
	command.Flags().StringVar(&to, "to", "", "inclusive upper time bound: Unix seconds or \"2006-01-02 15:04:05\"")
	return command
}

func writeDriveAdminPackage(writer *tabwriter.Writer, evidence driveadmin.PackageEvidence) {
	if !evidence.Installed {
		fmt.Fprintf(writer, "Package:\t%s is not installed\n", evidence.ID)
		return
	}
	fmt.Fprintf(writer, "Package:\t%s %s (%s)\n", evidence.ID, valueOrDash(evidence.Version), runningText(evidence.Running))
}

func runningText(running bool) string {
	if running {
		return "running"
	}
	return "stopped"
}

func writeDriveAdminCapabilities(cmd *cobra.Command, result application.DriveAdminCapabilitiesResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	writeDriveAdminPackage(writer, result.Capabilities.Package)
	fmt.Fprintln(writer, "OPERATION\tSUPPORTED")
	fmt.Fprintf(writer, "status read\t%s\n", yesNo(result.Capabilities.StatusRead))
	fmt.Fprintf(writer, "connections read\t%s\n", yesNo(result.Capabilities.ConnectionsRead))
	fmt.Fprintf(writer, "team folders read\t%s\n", yesNo(result.Capabilities.TeamFoldersRead))
	fmt.Fprintf(writer, "log read\t%s\n", yesNo(result.Capabilities.LogRead))
	fmt.Fprintf(writer, "team folders set\t%s\n", yesNo(result.Capabilities.TeamFoldersSet))
	fmt.Fprintln(writer, "\nOPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
	for _, operation := range result.Report.Operations {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), valueOrDash(operation.API), operation.Version)
	}
	return writer.Flush()
}

func writeDriveAdminStatus(cmd *cobra.Command, result application.DriveAdminStatusResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	writeDriveAdminPackage(writer, result.Status.Package)
	fmt.Fprintf(writer, "Service status:\t%s\n", valueOrDash(result.Status.Status))
	return writer.Flush()
}

func writeDriveAdminConnections(cmd *cobra.Command, result application.DriveAdminConnectionsResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Total:\t%d\n", result.Connections.Total)
	if len(result.Connections.Connections) == 0 {
		fmt.Fprintln(writer, "No active Drive connections.")
		return writer.Flush()
	}
	fmt.Fprintln(writer, "\nUSER\tDEVICE\tTYPE\tADDRESS")
	for _, connection := range result.Connections.Connections {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
			valueOrDash(connection.User), valueOrDash(connection.DeviceName),
			valueOrDash(connection.ClientType), valueOrDash(connection.Address))
	}
	return writer.Flush()
}

func writeDriveAdminTeamFolders(cmd *cobra.Command, result application.DriveAdminTeamFoldersResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Total:\t%d\n", result.TeamFolders.Total)
	if len(result.TeamFolders.TeamFolders) == 0 {
		fmt.Fprintln(writer, "No team folders reported.")
		return writer.Flush()
	}
	fmt.Fprintln(writer, "\nNAME\tENABLED\tSTATUS")
	for _, folder := range result.TeamFolders.TeamFolders {
		fmt.Fprintf(writer, "%s\t%s\t%s\n", folder.Name, yesNo(folder.Enabled), valueOrDash(folder.Status))
	}
	return writer.Flush()
}

func writeDriveAdminLog(cmd *cobra.Command, result application.DriveAdminLogResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Total:\t%d\n", result.Log.Total)
	if len(result.Log.Entries) == 0 {
		fmt.Fprintln(writer, "No Drive log entries matched.")
		return writer.Flush()
	}
	fmt.Fprintln(writer, "\nTIME\tUSER\tCLIENT\tEVENT\tTEAM FOLDER\tPATH")
	for _, entry := range result.Log.Entries {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%d\t%s\t%s\n",
			formatUnixTime(entry.TimeUnix), valueOrDash(entry.Username), valueOrDash(entry.ClientType),
			entry.EventType, valueOrDash(entry.TeamFolder), valueOrDash(entry.Path))
	}
	return writer.Flush()
}

func formatUnixTime(seconds int64) string {
	if seconds <= 0 {
		return "-"
	}
	return time.Unix(seconds, 0).Local().Format("2006-01-02 15:04:05")
}
