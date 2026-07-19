package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/downloadstation"
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
		newDownloadSettingsCommand(opts),
	)
	return command
}

func newDownloadSettingsCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "settings",
		Short: "Show the full Download Station settings (BT, eMule, FTP/HTTP, NZB, auto-extract, location, RSS, scheduler)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDownloadStationSettings(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeDownloadSettings(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.AddCommand(
		newDownloadSettingsPlanCommand(opts),
		newDownloadSettingsApplyCommand(opts),
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
	command.AddCommand(
		newDownloadTaskPlanCommand(opts),
		newDownloadTaskApplyCommand(opts),
	)
	return command
}

func newDownloadTaskPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a task create/pause/resume/delete request and emit an approval plan as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request downloadstation.TaskChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read task change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanDownloadStationTaskChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "task change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newDownloadTaskApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a task plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.DownloadStationTaskPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read task plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyDownloadStationTaskPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "task plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by tasks plan")
	_ = command.MarkFlagRequired("approve")
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

func newDownloadSettingsPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a settings patch (BT, FTP/HTTP, RSS, location, scheduler, or global group) and emit an approval plan as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request downloadstation.SettingsChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read settings change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanDownloadStationSettingsChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "settings change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newDownloadSettingsApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a settings plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.DownloadStationSettingsPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read settings plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyDownloadStationSettingsPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "settings plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by settings plan")
	_ = command.MarkFlagRequired("approve")
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
	fmt.Fprintf(writer, "Settings read:\t%s\n", yesNo(c.SettingsRead))
	fmt.Fprintf(writer, "Task write (guarded):\t%s\n", yesNo(c.TaskWrite))
	fmt.Fprintf(writer, "Settings write (guarded):\t%s\n", yesNo(c.SettingsWrite))
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

func writeDownloadSettings(cmd *cobra.Command, result application.DownloadStationSettingsResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	s := result.Settings
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Download volume:\t%s\n", valueOrDash(s.Global.DownloadVolume))
	fmt.Fprintf(writer, "eMule enabled:\t%s\n", yesNo(s.Global.EmuleEnabled))
	fmt.Fprintf(writer, "Auto-unzip service:\t%s\n", yesNo(s.Global.UnzipServiceEnabled))

	fmt.Fprintln(writer, "\nBITTORRENT")
	fmt.Fprintf(writer, "  TCP / DHT port:\t%d / %d\n", s.BT.TCPPort, s.BT.DHTPort)
	fmt.Fprintf(writer, "  DHT / port-forwarding / preview:\t%s / %s / %s\n", yesNo(s.BT.EnableDHT), yesNo(s.BT.EnablePortForwarding), yesNo(s.BT.EnablePreview))
	fmt.Fprintf(writer, "  Encryption:\t%s\n", valueOrDash(s.BT.Encryption))
	fmt.Fprintf(writer, "  Max down/up (KB/s):\t%d / %d\n", s.BT.MaxDownloadRate, s.BT.MaxUploadRate)
	fmt.Fprintf(writer, "  Max peers:\t%d\n", s.BT.MaxPeer)
	fmt.Fprintf(writer, "  Seeding ratio / interval / auto-remove:\t%d%% / %dm / %s\n", s.BT.SeedingRatio, s.BT.SeedingInterval, yesNo(s.BT.EnableSeedingAutoRemove))

	fmt.Fprintln(writer, "\nEMULE")
	fmt.Fprintf(writer, "  Enabled:\t%s\n", yesNo(s.Emule.Enabled))
	fmt.Fprintf(writer, "  Default destination:\t%s\n", valueOrDash(s.Emule.DefaultDestination))

	fmt.Fprintln(writer, "\nFTP/HTTP")
	fmt.Fprintf(writer, "  Max download (KB/s):\t%d\n", s.FtpHttp.MaxDownloadRate)
	fmt.Fprintf(writer, "  Per-task conn limit / max conn:\t%s / %d\n", yesNo(s.FtpHttp.EnableMaxConn), s.FtpHttp.MaxConn)

	fmt.Fprintln(writer, "\nNZB")
	fmt.Fprintf(writer, "  Server:\t%s:%d\n", valueOrDash(s.Nzb.Server), s.Nzb.Port)
	fmt.Fprintf(writer, "  Auth / SSL:\t%s / %s\n", yesNo(s.Nzb.EnableAuth), yesNo(s.Nzb.EnableEncryption))
	fmt.Fprintf(writer, "  PAR2 repair / remove:\t%s / %s\n", yesNo(s.Nzb.EnableParchive), yesNo(s.Nzb.EnableRemoveParfiles))
	fmt.Fprintf(writer, "  Conn per download / max down (KB/s):\t%d / %d\n", s.Nzb.ConnPerDownload, s.Nzb.MaxDownloadRate)

	fmt.Fprintln(writer, "\nAUTO-EXTRACTION")
	fmt.Fprintf(writer, "  Enabled / service:\t%s / %s\n", yesNo(s.AutoExtraction.EnableUnzip), yesNo(s.AutoExtraction.EnableUnzipService))
	fmt.Fprintf(writer, "  Subfolder / delete archive / overwrite:\t%s / %s / %s\n", yesNo(s.AutoExtraction.CreateSubfolder), yesNo(s.AutoExtraction.DeleteArchive), yesNo(s.AutoExtraction.UnzipOverwrite))
	fmt.Fprintf(writer, "  Location:\t%s\n", valueOrDash(s.AutoExtraction.UnzipLocation))
	fmt.Fprintf(writer, "  Password configured:\t%s\n", yesNo(s.AutoExtraction.PasswordConfigured))

	fmt.Fprintln(writer, "\nLOCATION / WATCH FOLDER")
	fmt.Fprintf(writer, "  Default destination:\t%s\n", valueOrDash(s.Location.DefaultDestination))
	fmt.Fprintf(writer, "  Watch enabled / delete after import:\t%s / %s\n", yesNo(s.Location.EnableTorrentNzbWatch), yesNo(s.Location.EnableDeleteTorrentNzbWatch))
	fmt.Fprintf(writer, "  Watch folder:\t%s\n", valueOrDash(s.Location.TorrentNzbWatchFolder))

	fmt.Fprintln(writer, "\nRSS")
	fmt.Fprintf(writer, "  Update interval (min):\t%d\n", s.Rss.UpdateIntervalMinutes)

	fmt.Fprintln(writer, "\nSCHEDULER")
	fmt.Fprintf(writer, "  Enabled:\t%s\n", yesNo(s.Scheduler.EnableSchedule))
	fmt.Fprintf(writer, "  Scheduled down/up (KB/s):\t%d / %d\n", s.Scheduler.DownloadRate, s.Scheduler.UploadRate)
	fmt.Fprintf(writer, "  Max tasks (limit):\t%d (%d)\n", s.Scheduler.MaxTasks, s.Scheduler.MaxTasksLimit)
	fmt.Fprintf(writer, "  Order:\t%s\n", valueOrDash(s.Scheduler.Order))
	return writer.Flush()
}
