package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/san"
)

func newSANCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "san", Short: "Inspect and manage Synology SAN Manager resources"}
	command.AddCommand(
		newSANCapabilitiesCommand(opts),
		newSANInventoryCommand(opts),
		newSANPlanCommand(opts),
		newSANApplyCommand(opts),
	)
	return command
}

func newSANPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a SAN change and create a hash-bound approval plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request san.ChangeRequest
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("decode SAN change request: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanSANChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "SAN change request JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "", "write the plan JSON to this file instead of stdout")
	return command
}

func newSANApplyCommand(opts *options) *cobra.Command {
	var inputPath, approveHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply an approved SAN plan and verify its stable-ID postcondition",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.SANPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("decode SAN plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, applyErr := service.ApplySANPlan(cmd.Context(), plan, approveHash)
			if result.NAS != "" {
				if err := encodeIndentedJSON(cmd.OutOrStdout(), result); err != nil {
					return err
				}
			}
			return applyErr
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "SAN plan JSON file, or - for stdin")
	command.Flags().StringVar(&approveHash, "approve", "", "exact SHA-256 hash printed by SAN plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newSANCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show SAN support and selected DSM backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetSANCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeSANCapabilities(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newSANInventoryCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "inventory",
		Short: "Show normalized iSCSI targets, LUNs, and mappings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetSANState(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeSANInventory(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func writeSANCapabilities(cmd *cobra.Command, result application.SANCapabilitiesResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Inventory read:\t%s\n", yesNo(result.Capabilities.InventoryRead))
	fmt.Fprintf(writer, "Target read:\t%s\n", yesNo(result.Capabilities.TargetRead))
	fmt.Fprintf(writer, "LUN read:\t%s\n", yesNo(result.Capabilities.LUNRead))
	fmt.Fprintf(writer, "Mapping read:\t%s\n", yesNo(result.Capabilities.MappingRead))
	fmt.Fprintf(writer, "Target create/update/delete:\t%s/%s/%s\n", yesNo(result.Capabilities.TargetCreate), yesNo(result.Capabilities.TargetUpdate), yesNo(result.Capabilities.TargetDelete))
	fmt.Fprintf(writer, "LUN create/update/delete:\t%s/%s/%s\n", yesNo(result.Capabilities.LUNCreate), yesNo(result.Capabilities.LUNUpdate), yesNo(result.Capabilities.LUNDelete))
	fmt.Fprintf(writer, "Mapping attach/detach:\t%s/%s\n", yesNo(result.Capabilities.MappingAttach), yesNo(result.Capabilities.MappingDetach))
	fmt.Fprintf(writer, "Mutations:\t%s\n", yesNo(result.Capabilities.Mutations))
	fmt.Fprintln(writer, "\nOPERATIONS")
	fmt.Fprintln(writer, "OPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
	for _, operation := range result.Report.Operations {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), valueOrDash(operation.API), operation.Version)
	}
	return writer.Flush()
}

func writeSANInventory(cmd *cobra.Command, result application.SANStateResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintln(writer, "\nTARGETS")
	fmt.Fprintln(writer, "ID\tNAME\tIQN\tENABLED\tSTATUS\tHEALTH\tAUTH\tSESSIONS")
	for _, target := range result.SAN.Targets {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\n", target.ID, target.Name, valueOrDash(target.IQN), yesNo(target.Enabled), valueOrDash(target.Status), valueOrDash(target.Health), valueOrDash(target.Authentication), target.ConnectedSessions)
	}
	fmt.Fprintln(writer, "\nLUNS")
	fmt.Fprintln(writer, "ID\tNUMERIC ID\tNAME\tSTATUS\tHEALTH\tSIZE\tALLOCATED\tPROVISIONING\tBACKING\tLOCATION\tMAPPED")
	for _, lun := range result.SAN.LUNs {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", lun.ID, valueOrDash(lun.NumericID), lun.Name, valueOrDash(lun.Status), valueOrDash(lun.Health), formatBytes(lun.SizeBytes), formatBytes(lun.AllocatedBytes), lun.Provisioning, lun.BackingKind, valueOrDash(lun.BackingLocation), yesNo(lun.Mapped))
	}
	fmt.Fprintln(writer, "\nMAPPINGS")
	fmt.Fprintln(writer, "TARGET ID\tLUN ID")
	for _, mapping := range result.SAN.Mappings {
		fmt.Fprintf(writer, "%s\t%s\n", mapping.TargetID, mapping.LUNID)
	}
	return writer.Flush()
}
