package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
)

// newDownloadCommand exposes the read-only Synology Download Station module.
func newDownloadCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "download",
		Aliases: []string{"downloadstation", "ds"},
		Short:   "Inspect Synology Download Station service, tasks, and statistics",
	}
	command.AddCommand(
		newDownloadCapabilitiesCommand(opts),
		newDownloadServiceCommand(opts),
		newDownloadTasksCommand(opts),
		newDownloadStatisticsCommand(opts),
	)
	return command
}

func newDownloadCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show Download Station read support, package evidence, and selected DSM backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDownloadStationCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeDownloadCapabilities(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDownloadServiceCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "service",
		Short: "Show Download Station service configuration and schedule",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDownloadStationService(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeDownloadService(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDownloadTasksCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "tasks",
		Short: "List Download Station download tasks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDownloadStationTasks(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeDownloadTasks(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDownloadStatisticsCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:     "statistics",
		Aliases: []string{"stats"},
		Short:   "Show current aggregate download/upload speed",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDownloadStationStatistics(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeDownloadStatistics(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func writeDownloadCapabilities(cmd *cobra.Command, result application.DownloadStationCapabilitiesResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	c := result.Capabilities
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Package:\t%s\n", valueOrDash(c.Package.ID))
	fmt.Fprintf(writer, "Installed:\t%s\n", yesNo(c.Package.Installed))
	fmt.Fprintf(writer, "Version:\t%s\n", valueOrDash(c.Package.Version))
	fmt.Fprintf(writer, "Running:\t%s\n", yesNo(c.Package.Running))
	fmt.Fprintf(writer, "Service read:\t%s\n", yesNo(c.ServiceRead))
	fmt.Fprintf(writer, "Task read:\t%s\n", yesNo(c.TaskRead))
	fmt.Fprintf(writer, "Statistic read:\t%s\n", yesNo(c.StatisticRead))
	fmt.Fprintln(writer, "\nOPERATIONS")
	fmt.Fprintln(writer, "OPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
	for _, operation := range result.Report.Operations {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), valueOrDash(operation.API), operation.Version)
	}
	return writer.Flush()
}

func writeDownloadService(cmd *cobra.Command, result application.DownloadStationServiceResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	s := result.Service
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Version:\t%s\n", valueOrDash(s.Version))
	fmt.Fprintf(writer, "Manager:\t%s\n", yesNo(s.IsManager))
	fmt.Fprintf(writer, "Default destination:\t%s\n", valueOrDash(s.Config.DefaultDestination))
	fmt.Fprintf(writer, "eMule enabled:\t%s\n", yesNo(s.Config.EmuleEnabled))
	fmt.Fprintf(writer, "Auto-unzip:\t%s\n", yesNo(s.Config.UnzipServiceEnabled))
	fmt.Fprintf(writer, "BT max down/up (KB/s):\t%d / %d\n", s.Config.BTMaxDownloadKBs, s.Config.BTMaxUploadKBs)
	fmt.Fprintf(writer, "eMule max down/up (KB/s):\t%d / %d\n", s.Config.EmuleMaxDownloadKBs, s.Config.EmuleMaxUploadKBs)
	fmt.Fprintf(writer, "FTP/HTTP/NZB max down (KB/s):\t%d / %d / %d\n", s.Config.FTPMaxDownloadKBs, s.Config.HTTPMaxDownloadKBs, s.Config.NZBMaxDownloadKBs)
	fmt.Fprintf(writer, "Schedule enabled:\t%s\n", yesNo(s.Schedule.Enabled))
	fmt.Fprintf(writer, "eMule schedule enabled:\t%s\n", yesNo(s.Schedule.EmuleEnabled))
	return writer.Flush()
}

func writeDownloadTasks(cmd *cobra.Command, result application.DownloadStationTasksResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Total:\t%d\n", result.Tasks.Total)
	fmt.Fprintln(writer, "\nTASKS")
	if len(result.Tasks.Tasks) == 0 {
		fmt.Fprintln(writer, "(no tasks)")
		return writer.Flush()
	}
	fmt.Fprintln(writer, "ID\tTYPE\tTITLE\tSIZE\tSTATUS\tDOWN B/s\tUP B/s")
	for _, task := range result.Tasks.Tasks {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%d\t%s\t%d\t%d\n",
			valueOrDash(task.ID), valueOrDash(task.Type), valueOrDash(task.Title), task.Size, valueOrDash(task.Status),
			task.Transfer.SpeedDownload, task.Transfer.SpeedUpload)
	}
	return writer.Flush()
}

func writeDownloadStatistics(cmd *cobra.Command, result application.DownloadStationStatisticsResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Download speed (B/s):\t%d\n", result.Statistics.SpeedDownload)
	fmt.Fprintf(writer, "Upload speed (B/s):\t%d\n", result.Statistics.SpeedUpload)
	return writer.Flush()
}
