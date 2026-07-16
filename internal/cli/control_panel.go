package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
)

func newControlPanelCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "control-panel",
		Aliases: []string{"controlpanel"},
		Short:   "Inspect focused DSM Control Panel modules",
	}
	command.AddCommand(
		newControlPanelTimeCommand(opts),
		newFileServicesCommand(opts),
	)
	return command
}

func newControlPanelTimeCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "time", Short: "Inspect regional time and NTP configuration"}
	command.AddCommand(
		newControlPanelTimeStateCommand(opts),
		newControlPanelTimeCapabilitiesCommand(opts),
	)
	return command
}

func newControlPanelTimeStateCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "state",
		Short: "Show normalized time zone, display formats, and NTP settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetControlPanelTimeState(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeControlPanelTimeState(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newControlPanelTimeCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show time-module support and the selected DSM backend",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetControlPanelTimeCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return writeControlPanelTimeCapabilities(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func writeControlPanelTimeState(cmd *cobra.Command, result application.ControlPanelTimeStateResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Time zone:\t%s\n", result.Time.TimeZone)
	fmt.Fprintf(writer, "Date format:\t%s\n", valueOrDash(result.Time.DateFormat))
	fmt.Fprintf(writer, "Time format:\t%s\n", valueOrDash(result.Time.TimeFormat))
	fmt.Fprintf(writer, "Synchronization:\t%s\n", result.Time.SynchronizationMode)
	fmt.Fprintf(writer, "NTP servers:\t%s\n", valueOrDash(strings.Join(result.Time.NTPServers, ", ")))
	return writer.Flush()
}

func writeControlPanelTimeCapabilities(cmd *cobra.Command, result application.ControlPanelTimeCapabilitiesResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Module:\t%s\n", result.Capabilities.Module)
	fmt.Fprintf(writer, "Read:\t%s\n", yesNo(result.Capabilities.Read))
	fmt.Fprintf(writer, "Set:\t%s\n", yesNo(result.Capabilities.Set))
	fmt.Fprintln(writer, "\nOPERATIONS")
	fmt.Fprintln(writer, "OPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
	for _, operation := range result.Report.Operations {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), valueOrDash(operation.API), operation.Version)
	}
	return writer.Flush()
}
