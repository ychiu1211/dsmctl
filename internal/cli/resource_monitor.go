package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/resmon"
)

func newResourceMonitorCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "resource-monitor",
		Aliases: []string{"resmon"},
		Short:   "Read DSM Resource Monitor utilization and history, and toggle history recording",
	}
	command.AddCommand(
		newResourceMonitorCurrentCommand(opts),
		newResourceMonitorHistoryCommand(opts),
		newResourceMonitorSettingCommand(opts),
		newResourceMonitorCapabilitiesCommand(opts),
		newResourceMonitorPlanRecordingCommand(opts),
		newResourceMonitorApplyRecordingCommand(opts),
	)
	return command
}

func newResourceMonitorCurrentCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "current",
		Short: "Show the current CPU, memory, network, disk, and volume utilization",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetResourceMonitorState(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeResourceCurrent(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newResourceMonitorHistoryCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	var period string
	var dimensions []string
	command := &cobra.Command{
		Use:   "history",
		Short: "Show recorded utilization history (requires history recording to be enabled)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetResourceMonitorHistory(cmd.Context(), opts.nas, resmon.HistoryQuery{Period: period, Dimensions: dimensions})
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeResourceHistory(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().StringVar(&period, "period", "week", "history window: week, month, half_year, or year")
	command.Flags().StringSliceVar(&dimensions, "dimension", nil, "limit to dimensions: cpu, memory, network, disk, volume (repeatable)")
	return command
}

func newResourceMonitorSettingCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "setting",
		Short: "Show whether history recording is enabled",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetResourceMonitorSetting(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "History recording:\t%s\n", enabledLabel(result.Setting.Enabled))
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newResourceMonitorCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show Resource Monitor support and the selected DSM backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetResourceMonitorCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Utilization read:\t%s\n", yesNo(result.Capabilities.Read))
			fmt.Fprintf(writer, "Recording read:\t%s\n", yesNo(result.Capabilities.RecordingRead))
			fmt.Fprintf(writer, "Recording set:\t%s\n", yesNo(result.Capabilities.RecordingSet))
			fmt.Fprintln(writer, "\nOPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
			for _, operation := range result.Report.Operations {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), valueOrDash(operation.API), operation.Version)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newResourceMonitorPlanRecordingCommand(opts *options) *cobra.Command {
	var enable, disable bool
	var outputPath string
	command := &cobra.Command{
		Use:   "plan-recording",
		Short: "Emit an approval plan that turns history recording on or off",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if enable == disable {
				return fmt.Errorf("specify exactly one of --enable or --disable")
			}
			desired := enable
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanResourceRecordingChange(cmd.Context(), opts.nas, resmon.RecordingChange{Enable: &desired})
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().BoolVar(&enable, "enable", false, "plan enabling history recording")
	command.Flags().BoolVar(&disable, "disable", false, "plan disabling history recording")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newResourceMonitorApplyRecordingCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply-recording",
		Short: "Apply a recording plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.ResourceRecordingPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read recording plan: %w", err)
			}
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyResourceRecordingPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "recording plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by plan-recording")
	_ = command.MarkFlagRequired("approve")
	return command
}

func writeResourceCurrent(cmd *cobra.Command, result application.ResourceMonitorStateResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	utilization := result.Utilization
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "History recording:\t%s\n", enabledLabel(utilization.RecordingEnabled))
	fmt.Fprintf(writer, "CPU:\tuser %d%%, system %d%%, other %d%% (load %d/%d/%d)\n",
		utilization.CPU.UserPercent, utilization.CPU.SystemPercent, utilization.CPU.OtherPercent,
		utilization.CPU.LoadAverage1, utilization.CPU.LoadAverage5, utilization.CPU.LoadAverage15)
	fmt.Fprintf(writer, "Memory:\treal %d%%, swap %d%% (total %s, available %s)\n",
		utilization.Memory.RealUsagePercent, utilization.Memory.SwapUsagePercent,
		humanBytes(utilization.Memory.TotalRealBytes), humanBytes(utilization.Memory.AvailRealBytes))
	fmt.Fprintf(writer, "Disk total:\tread %s/s, write %s/s, busy %d%%\n",
		humanBytes(utilization.Disk.Total.ReadBytesPerSec), humanBytes(utilization.Disk.Total.WriteBytesPerSec), utilization.Disk.Total.UtilizationPercent)
	if len(utilization.Network) > 0 {
		fmt.Fprintln(writer, "\nINTERFACE\tTX/s\tRX/s")
		for _, iface := range utilization.Network {
			fmt.Fprintf(writer, "%s\t%s\t%s\n", iface.Device, humanBytes(iface.TxBytesPerSec), humanBytes(iface.RxBytesPerSec))
		}
	}
	if len(utilization.Disk.Disks) > 0 {
		fmt.Fprintln(writer, "\nDISK\tNAME\tREAD/s\tWRITE/s\tBUSY")
		for _, disk := range utilization.Disk.Disks {
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d%%\n", disk.Device, valueOrDash(disk.DisplayName),
				humanBytes(disk.ReadBytesPerSec), humanBytes(disk.WriteBytesPerSec), disk.UtilizationPercent)
		}
	}
	if len(utilization.Volumes) > 0 {
		fmt.Fprintln(writer, "\nVOLUME\tNAME\tREAD/s\tWRITE/s\tBUSY")
		for _, volume := range utilization.Volumes {
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d%%\n", volume.Device, valueOrDash(volume.DisplayName),
				humanBytes(volume.ReadBytesPerSec), humanBytes(volume.WriteBytesPerSec), volume.UtilizationPercent)
		}
	}
	return writer.Flush()
}

func writeResourceHistory(cmd *cobra.Command, result application.ResourceMonitorHistoryResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Period:\t%s\n", result.History.Period)
	fmt.Fprintf(writer, "Series:\t%d\n", len(result.History.Series))
	fmt.Fprintln(writer, "\nDIMENSION\tDEVICE\tMETRIC\tSAMPLES\tLATEST")
	for _, series := range result.History.Series {
		latest := "-"
		if count := len(series.Values); count > 0 {
			latest = fmt.Sprintf("%g", series.Values[count-1])
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%d\t%s\n", series.Dimension, valueOrDash(series.Device), series.Metric, len(series.Values), latest)
	}
	return writer.Flush()
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// humanBytes renders a byte count with a binary unit suffix for compact
// terminal output. JSON output keeps the exact integer.
func humanBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	divisor, exponent := int64(unit), 0
	for next := value / unit; next >= unit && exponent < 4; next /= unit {
		divisor *= unit
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(divisor), "KMGTP"[exponent])
}
