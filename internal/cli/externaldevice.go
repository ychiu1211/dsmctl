package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/domain/externaldevice"
)

func newExternalDeviceCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "external-device",
		Short:   "Inspect Control Panel External Devices (USB/eSATA storage, printers)",
		Aliases: []string{"external-devices", "extdev"},
	}
	command.AddCommand(
		newExternalDeviceCapabilitiesCommand(opts),
		newExternalStorageCommand(opts),
		newExternalPrintersCommand(opts),
	)
	return command
}

func newExternalDeviceCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show which External Devices areas can be read and the selected backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetExternalDeviceCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "USB storage read:\t%s\n", yesNo(result.Capabilities.USBStorage))
			fmt.Fprintf(writer, "eSATA storage read:\t%s\n", yesNo(result.Capabilities.ESATAStorage))
			fmt.Fprintf(writer, "Printer read:\t%s\n", yesNo(result.Capabilities.Printer))
			fmt.Fprintf(writer, "Printer sharing read:\t%s\n", yesNo(result.Capabilities.PrinterSharing))
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

func newExternalStorageCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "storage",
		Short: "Show attached USB and eSATA external disks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetExternalStorage(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			printStorageBus(writer, "USB", result.Storage.USB)
			printStorageBus(writer, "eSATA", result.Storage.ESATA)
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newExternalPrintersCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:     "printers",
		Short:   "Show connected printers and the Bonjour/AirPrint sharing toggle",
		Aliases: []string{"printer"},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetExternalPrinters(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			if sharing := result.Printers.Sharing; sharing != nil {
				fmt.Fprintf(writer, "Bonjour/AirPrint sharing:\t%s\n", yesNo(sharing.BonjourEnabled))
			} else {
				fmt.Fprintf(writer, "Bonjour/AirPrint sharing:\t(not supported)\n")
			}
			if len(result.Printers.Printers) == 0 {
				fmt.Fprintf(writer, "Printers:\t(none connected)\n")
				return writer.Flush()
			}
			fmt.Fprintln(writer, "\nID\tNAME\tTYPE\tSTATUS\tDEFAULT\tQUEUED")
			for _, printer := range result.Printers.Printers {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%d\n",
					valueOrDash(printer.ID), valueOrDash(printer.Name), valueOrDash(printer.Type),
					valueOrDash(printer.Status), yesNo(printer.Default), printer.SpoolerCount)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func printStorageBus(writer *tabwriter.Writer, label string, area *externaldevice.ExternalStorageArea) {
	if area == nil {
		fmt.Fprintf(writer, "%s storage:\t(not supported)\n", label)
		return
	}
	if len(area.Devices) == 0 {
		fmt.Fprintf(writer, "%s storage:\t(no device attached)\n", label)
		return
	}
	fmt.Fprintf(writer, "\n%s DEVICE\tTITLE\tVENDOR/PRODUCT\tSIZE(MB)\tSTATUS\n", label)
	for _, device := range area.Devices {
		id := valueOrDash(firstNonEmpty(device.DevID, device.DevPath))
		vendorProduct := valueOrDash(joinNonEmpty(device.Vendor, device.Product))
		fmt.Fprintf(writer, "%s\t%s\t%s\t%d\t%s\n",
			id, valueOrDash(device.Title), vendorProduct, device.TotalSizeMB, valueOrDash(device.Status))
		for _, part := range device.Partitions {
			mount := part.MountPoint
			if part.ShareName != "" {
				mount = fmt.Sprintf("%s (%s)", mount, part.ShareName)
			}
			fmt.Fprintf(writer, "  %s\t%s\t%d/%d MB used\t%s\t%s\n",
				valueOrDash(part.Name), valueOrDash(part.Filesystem), part.UsedSizeMB, part.TotalSizeMB,
				valueOrDash(mount), valueOrDash(part.Status))
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func joinNonEmpty(values ...string) string {
	result := ""
	for _, value := range values {
		if value == "" {
			continue
		}
		if result != "" {
			result += " "
		}
		result += value
	}
	return result
}
