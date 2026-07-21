package cli

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/domain/disksmart"
)

func newDiskSMARTCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "disk-smart",
		Short:   "Inspect per-disk health and S.M.A.R.T. data",
		Aliases: []string{"disksmart"},
	}
	command.AddCommand(
		newDiskSMARTCapabilitiesCommand(opts),
		newDiskHealthCommand(opts),
		newDiskSMARTAttributesCommand(opts),
	)
	return command
}

func newDiskSMARTCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show which disk-SMART areas can be read and the selected backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDiskSMARTCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Disk health read:\t%s\n", yesNo(result.Capabilities.Health))
			fmt.Fprintf(writer, "SMART attributes read:\t%s\n", yesNo(result.Capabilities.Attributes))
			fmt.Fprintf(writer, "Health thresholds read:\t%s\n", yesNo(result.Capabilities.Thresholds))
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

func newDiskHealthCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "health",
		Short: "Show per-disk health, remaining life, and self-test state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDiskHealth(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Disks:\t%d\n", len(result.Health.Disks))
			if thresholds := result.Health.Thresholds; thresholds != nil {
				fmt.Fprintf(writer, "Bad-sector warning:\t%s\n", yesNo(thresholds.BadSectorThresholdEnabled))
				fmt.Fprintf(writer, "Remaining-life warning:\t%s (%d%%)\n", yesNo(thresholds.RemainingLifeThresholdEnabled), thresholds.RemainingLifeThresholdPercent)
				fmt.Fprintf(writer, "Health report:\t%s\n", yesNo(thresholds.HealthReportEnabled))
			} else {
				fmt.Fprintf(writer, "Health thresholds:\t(not supported)\n")
			}
			fmt.Fprintln(writer, "\nDISK\tNAME\tTYPE\tHEALTH\tSMART\tLIFE\tTEMP\tTEST")
			for _, disk := range result.Health.Disks {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					valueOrDash(disk.ID), valueOrDash(disk.Name), valueOrDash(disk.Type),
					valueOrDash(disk.Health), valueOrDash(disk.SMARTStatus),
					formatRemainingLife(disk.RemainingLifePercent),
					formatDiskTemp(disk.TemperatureC),
					formatTestingState(disk.Testing, disk.TestingType))
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDiskSMARTAttributesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	var diskFilter string
	command := &cobra.Command{
		Use:   "attributes",
		Short: "Show the S.M.A.R.T. attribute table for each disk",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDiskSMARTAttributes(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Disks:\t%d\n", len(result.SMART.Disks))
			for _, disk := range result.SMART.Disks {
				if diskFilter != "" && !strings.EqualFold(disk.ID, diskFilter) && !strings.EqualFold(disk.Device, diskFilter) {
					continue
				}
				fmt.Fprintf(writer, "\n%s (%s)\t%s\n", valueOrDash(disk.ID), valueOrDash(disk.Name), valueOrDash(disk.OverallStatus))
				if disk.NoSMARTData {
					fmt.Fprintf(writer, "\t(no SMART data")
					if disk.AbsenceCode != 0 {
						fmt.Fprintf(writer, ", DSM code %d", disk.AbsenceCode)
					}
					fmt.Fprintf(writer, ")\n")
					continue
				}
				if disk.TestStatus != nil {
					fmt.Fprintf(writer, "\tSelf-test:\t%s\n", formatTestStatus(disk.TestStatus))
				}
				fmt.Fprintln(writer, "\tID\tATTRIBUTE\tCUR\tWORST\tTHRESH\tRAW\tSTATUS")
				for _, attr := range disk.Attributes {
					fmt.Fprintf(writer, "\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
						valueOrDash(attr.ID), valueOrDash(attr.Name), valueOrDash(attr.Current),
						valueOrDash(attr.Worst), valueOrDash(attr.Threshold), valueOrDash(attr.Raw), valueOrDash(attr.Status))
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().StringVar(&diskFilter, "disk", "", "only show attributes for this disk id (such as sda) or device path")
	return command
}

func formatRemainingLife(percent *int) string {
	if percent == nil {
		return "-"
	}
	return strconv.Itoa(*percent) + "%"
}

func formatDiskTemp(temp *int) string {
	if temp == nil {
		return "-"
	}
	return strconv.Itoa(*temp) + "C"
}

func formatTestingState(testing bool, testType string) string {
	if testing {
		if testType != "" && testType != "idle" {
			return "running (" + testType + ")"
		}
		return "running"
	}
	return "idle"
}

func formatTestStatus(status *disksmart.SMARTTestStatus) string {
	if status == nil {
		return "-"
	}
	if status.Testing {
		if status.Remaining != "" {
			return "running (" + status.Remaining + " remaining)"
		}
		return "running"
	}
	if status.LatestResult == "" {
		return "-"
	}
	return status.LatestResult
}
