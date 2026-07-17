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
	"github.com/ychiu1211/dsmctl/internal/domain/storage"
)

func newStorageCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "storage", Short: "Inspect and plan DSM storage resources"}
	command.AddCommand(
		newStorageCapabilitiesCommand(opts),
		newStorageInventoryCommand(opts),
		newStoragePlanCommand(opts),
		newStorageApplyCommand(opts),
	)
	return command
}

func newStoragePlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Plan a guarded storage pool or volume change",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request storage.ChangeRequest
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("decode storage change request: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanStorageChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "storage change request JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "", "write the plan JSON to this file instead of stdout")
	return command
}

func newStorageApplyCommand(opts *options) *cobra.Command {
	var inputPath, approveHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply and verify an approved storage plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.StoragePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("decode storage plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyStoragePlan(cmd.Context(), plan, approveHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "storage plan JSON file, or - for stdin")
	command.Flags().StringVar(&approveHash, "approve", "", "exact SHA-256 hash printed by storage plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newStorageCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show supported storage operations and selected DSM backend",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
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
			service, err := loadService(opts.configPath)
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
	fmt.Fprintf(writer, "Pool create/expand/delete:\t%s/%s/%s\n", yesNo(result.Capabilities.PoolCreate), yesNo(result.Capabilities.PoolUpdate), yesNo(result.Capabilities.PoolDelete))
	fmt.Fprintf(writer, "Volume create/update/delete:\t%s/%s/%s\n", yesNo(result.Capabilities.VolumeCreate), yesNo(result.Capabilities.VolumeUpdate), yesNo(result.Capabilities.VolumeDelete))
	fmt.Fprintf(writer, "SSD cache status:\t%s\n", yesNo(result.Capabilities.CacheStatus))
	fmt.Fprintf(writer, "SSD cache create/expand/convert/delete:\t%s/%s/%s/%s\n", yesNo(result.Capabilities.CacheCreate), yesNo(result.Capabilities.CacheExpand), yesNo(result.Capabilities.CacheConvert), yesNo(result.Capabilities.CacheDelete))
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

	if len(result.Storage.Caches) > 0 {
		fmt.Fprintln(writer, "\nSSD CACHES")
		fmt.Fprintln(writer, "ID\tVOLUME\tMODE\tRAID\tSTATUS\tHEALTH\tSIZE\tDISKS")
		for _, cache := range result.Storage.Caches {
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				valueOrDash(cache.ID), valueOrDash(cache.VolumeID), valueOrDash(cache.CacheType), valueOrDash(cache.ProtectionRAID),
				valueOrDash(cache.Status), valueOrDash(cache.Health), formatBytes(cache.SizeBytes), valueOrDash(strings.Join(cache.DiskIDs, ",")),
			)
		}
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
