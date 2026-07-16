package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
)

func newStorageCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "storage", Short: "Inspect DSM storage resources"}
	command.AddCommand(
		newStorageCapabilitiesCommand(opts),
		newStorageInventoryCommand(opts),
	)
	return command
}

func newStorageCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show supported storage operations and selected DSM backend",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
			if err != nil {
				return err
			}
			defer closeService(service)

			result, err := service.GetStorageCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return writeStorageCapabilities(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newStorageInventoryCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "inventory",
		Short: "Show disks, storage pools, volumes, and their status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
			if err != nil {
				return err
			}
			defer closeService(service)

			result, err := service.GetStorageState(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return writeStorageInventory(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func closeService(service *application.Service) {
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = service.Close(closeCtx)
}

func writeStorageCapabilities(cmd *cobra.Command, result application.StorageCapabilitiesResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Inventory read:\t%s\n", yesNo(result.Capabilities.InventoryRead))
	fmt.Fprintf(writer, "Disk status:\t%s\n", yesNo(result.Capabilities.DiskStatus))
	fmt.Fprintf(writer, "Pool status:\t%s\n", yesNo(result.Capabilities.PoolStatus))
	fmt.Fprintf(writer, "Volume status:\t%s\n", yesNo(result.Capabilities.VolumeStatus))
	fmt.Fprintf(writer, "Mutations:\t%s\n", yesNo(result.Capabilities.Mutations))
	for _, operation := range result.Report.Operations {
		fmt.Fprintf(writer, "Backend:\t%s\n", valueOrDash(operation.Backend))
		fmt.Fprintf(writer, "DSM API:\t%s v%d\n", valueOrDash(operation.API), operation.Version)
		fmt.Fprintf(writer, "Selection:\t%s\n", operation.Reason)
	}
	return writer.Flush()
}

func writeStorageInventory(cmd *cobra.Command, result application.StorageStateResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)

	fmt.Fprintln(writer, "\nDISKS")
	fmt.Fprintln(writer, "ID\tNAME\tSLOT\tSTATUS\tHEALTH\tMODEL\tSERIAL\tSIZE\tTEMP")
	for _, disk := range result.Storage.Disks {
		temperature := "-"
		if disk.TemperatureC != nil {
			temperature = fmt.Sprintf("%.1f C", *disk.TemperatureC)
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			valueOrDash(disk.ID), valueOrDash(disk.Name), valueOrDash(disk.Slot), valueOrDash(disk.Status),
			valueOrDash(disk.Health), valueOrDash(disk.Model), valueOrDash(disk.Serial), formatBytes(disk.SizeBytes), temperature,
		)
	}

	fmt.Fprintln(writer, "\nSTORAGE POOLS")
	fmt.Fprintln(writer, "ID\tNAME\tRAID\tSTATUS\tHEALTH\tSIZE\tUSED\tDISKS")
	for _, pool := range result.Storage.Pools {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			valueOrDash(pool.ID), valueOrDash(pool.Name), valueOrDash(pool.RAIDType), valueOrDash(pool.Status),
			valueOrDash(pool.Health), formatBytes(pool.SizeBytes), formatBytes(pool.UsedBytes), valueOrDash(strings.Join(pool.DiskIDs, ",")),
		)
	}

	fmt.Fprintln(writer, "\nVOLUMES")
	fmt.Fprintln(writer, "ID\tNAME\tPOOL\tFILESYSTEM\tSTATUS\tHEALTH\tSIZE\tUSED\tREAD ONLY")
	for _, volume := range result.Storage.Volumes {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			valueOrDash(volume.ID), valueOrDash(volume.Name), valueOrDash(volume.PoolID), valueOrDash(volume.FileSystem),
			valueOrDash(volume.Status), valueOrDash(volume.Health), formatBytes(volume.SizeBytes), formatBytes(volume.UsedBytes), yesNo(volume.ReadOnly),
		)
	}
	return writer.Flush()
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func formatBytes(bytes uint64) string {
	if bytes == 0 {
		return "-"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	value := float64(bytes)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", bytes, units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}
