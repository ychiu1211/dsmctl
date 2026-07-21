package cli

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/domain/hardware"
)

func newHardwareCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "hardware",
		Short:   "Inspect Control Panel Hardware & Power settings",
		Aliases: []string{"hw"},
	}
	command.AddCommand(
		newHardwareCapabilitiesCommand(opts),
		newHardwareGeneralCommand(opts),
		newHardwarePowerScheduleCommand(opts),
		newHardwarePowerRecoveryCommand(opts),
		newHardwareUPSCommand(opts),
	)
	return command
}

func newHardwareCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show which Hardware & Power areas can be read and the selected backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetHardwareCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Beep control read:\t%s\n", yesNo(result.Capabilities.Beep))
			fmt.Fprintf(writer, "Fan speed read:\t%s\n", yesNo(result.Capabilities.Fan))
			fmt.Fprintf(writer, "LED brightness read:\t%s\n", yesNo(result.Capabilities.LED))
			fmt.Fprintf(writer, "Power schedule read:\t%s\n", yesNo(result.Capabilities.PowerSchedule))
			fmt.Fprintf(writer, "Power recovery read:\t%s\n", yesNo(result.Capabilities.PowerRecovery))
			fmt.Fprintf(writer, "UPS read:\t%s\n", yesNo(result.Capabilities.UPS))
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

func newHardwareGeneralCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "general",
		Short: "Show beep control, fan-speed mode, and LED brightness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetHardwareGeneral(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)

			if fan := result.General.Fan; fan != nil {
				fmt.Fprintf(writer, "Fan speed mode:\t%s\n", valueOrDash(fan.Mode))
			} else {
				fmt.Fprintf(writer, "Fan speed mode:\t(not supported)\n")
			}
			if led := result.General.LED; led != nil {
				fmt.Fprintf(writer, "LED brightness:\t%d\n", led.Brightness)
				if led.Schedule != "" {
					fmt.Fprintf(writer, "LED schedule:\t%s\n", summarizeLEDSchedule(led.Schedule))
				}
			} else {
				fmt.Fprintf(writer, "LED brightness:\t(not supported)\n")
			}

			if beep := result.General.Beep; beep != nil {
				fmt.Fprintln(writer, "\nBEEP EVENT\tENABLED\tSUPPORTED")
				for _, event := range beep.Events {
					fmt.Fprintf(writer, "%s\t%s\t%s\n", event.Event, yesNo(event.Enabled), yesNo(event.Supported))
				}
			} else {
				fmt.Fprintf(writer, "\nBeep control:\t(not supported)\n")
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newHardwarePowerScheduleCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "power-schedule",
		Short: "Show scheduled power on/off tasks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetHardwarePowerSchedule(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Enabled tasks:\t%d\n", result.Schedule.EnabledTaskCount)
			printPowerTasks(writer, "POWER-ON", result.Schedule.PowerOnTasks)
			printPowerTasks(writer, "POWER-OFF", result.Schedule.PowerOffTasks)
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newHardwarePowerRecoveryCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "power-recovery",
		Short: "Show after-power-loss behavior and Wake-on-LAN state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetHardwarePowerRecovery(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			restore := "stay off (manual power-on required)"
			if result.Recovery.RestorePowerState {
				restore = "restore previous power state"
			}
			fmt.Fprintf(writer, "After power loss:\t%s\n", restore)
			fmt.Fprintf(writer, "Internal NICs:\t%d\n", result.Recovery.InternalLANCount)
			if len(result.Recovery.WOL) > 0 {
				fmt.Fprintln(writer, "\nNIC\tWAKE-ON-LAN")
				for _, nic := range result.Recovery.WOL {
					fmt.Fprintf(writer, "%d\t%s\n", nic.Index, yesNo(nic.Enabled))
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newHardwareUPSCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "ups",
		Short: "Show UPS configuration and live status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetHardwareUPS(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			ups := result.UPS
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "UPS enabled:\t%s\n", yesNo(ups.Enabled))
			if !ups.Enabled && !ups.USBConnected {
				fmt.Fprintf(writer, "Device:\t(not connected)\n")
			}
			fmt.Fprintf(writer, "Mode:\t%s\n", valueOrDash(ups.Mode))
			fmt.Fprintf(writer, "USB UPS connected:\t%s\n", yesNo(ups.USBConnected))
			fmt.Fprintf(writer, "Status:\t%s\n", valueOrDash(ups.Status))
			if ups.Manufacturer != "" || ups.Model != "" {
				fmt.Fprintf(writer, "Device:\t%s %s\n", ups.Manufacturer, ups.Model)
			}
			if ups.USBConnected {
				fmt.Fprintf(writer, "Battery charge:\t%d%%\n", ups.ChargePercent)
				fmt.Fprintf(writer, "Battery runtime:\t%ds\n", ups.RuntimeSeconds)
			}
			if ups.SafeShutdownDelaySeconds != nil {
				fmt.Fprintf(writer, "Safe-shutdown threshold:\t%d (fixed)\n", *ups.SafeShutdownDelaySeconds)
			} else {
				fmt.Fprintf(writer, "Safe-shutdown threshold:\twhen battery reaches low\n")
			}
			fmt.Fprintf(writer, "Signal UPS to power off:\t%s\n", yesNo(ups.ShutdownUPS))
			if ups.NetworkServerIP != "" {
				fmt.Fprintf(writer, "Network UPS server:\t%s\n", ups.NetworkServerIP)
			}
			fmt.Fprintf(writer, "Network UPS server enabled:\t%s\n", yesNo(ups.NetworkUPSServerEnabled))
			if len(ups.PermittedSlaves) > 0 {
				fmt.Fprintf(writer, "Permitted slaves:\t%s\n", strings.Join(ups.PermittedSlaves, ", "))
			}
			if snmp := ups.SNMP; snmp != nil {
				fmt.Fprintf(writer, "\nSNMP UPS server:\t%s\n", valueOrDash(snmp.ServerIP))
				fmt.Fprintf(writer, "SNMP version:\t%s\n", valueOrDash(snmp.Version))
				fmt.Fprintf(writer, "SNMP community set:\t%s\n", yesNo(snmp.CommunitySet))
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func printPowerTasks(writer *tabwriter.Writer, label string, tasks []hardware.PowerScheduleTask) {
	if len(tasks) == 0 {
		fmt.Fprintf(writer, "\n%s tasks:\t(none)\n", label)
		return
	}
	fmt.Fprintf(writer, "\n%s\tENABLED\tTIME\tWEEKDAYS\n", label)
	for _, task := range tasks {
		fmt.Fprintf(writer, "\t%s\t%02d:%02d\t%s\n", yesNo(task.Enabled), task.Hour, task.Minute, valueOrDash(task.Weekdays))
	}
}

// summarizeLEDSchedule reports the length of DSM's weekly LED mask rather than
// dumping the 168-character string in table output.
func summarizeLEDSchedule(mask string) string {
	on := strings.Count(mask, "1")
	return strconv.Itoa(on) + "/" + strconv.Itoa(len(mask)) + " hours on"
}
