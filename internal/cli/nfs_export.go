package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/nfsexport"
)

func newNFSExportCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "export",
		Short: "Inspect and manage per-shared-folder NFS export rules",
	}
	command.AddCommand(
		newNFSExportCapabilitiesCommand(opts),
		newNFSExportListCommand(opts),
		newNFSExportPlanCommand(opts),
		newNFSExportApplyCommand(opts),
	)
	return command
}

func newNFSExportCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show whether the NFS export backend is supported",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNFSExportCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Read:\t%s\n", yesNo(result.Capabilities.Read))
			fmt.Fprintf(writer, "Set:\t%s\n", yesNo(result.Capabilities.Set))
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

func newNFSExportListCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	var share string
	command := &cobra.Command{
		Use:   "list",
		Short: "Show the NFS export rule set of one shared folder",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if share == "" {
				return fmt.Errorf("--share is required")
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNFSExportState(cmd.Context(), opts.nas, share)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeNFSExportState(cmd, result)
		},
	}
	command.Flags().StringVar(&share, "share", "", "shared-folder name")
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newNFSExportPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a complete desired NFS export rule set and emit an approval plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request nfsexport.ChangeRequest
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read NFS export change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanNFSExportChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "NFS export change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newNFSExportApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply an NFS export plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.NFSExportPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read NFS export plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyNFSExportPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "NFS export plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by the NFS export plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func writeNFSExportState(cmd *cobra.Command, result application.NFSExportStateResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Shared folder:\t%s\n", result.Export.Share)
	if len(result.Export.Rules) == 0 {
		fmt.Fprintln(writer, "Rules:\t(none)")
		return writer.Flush()
	}
	fmt.Fprintln(writer, "\nCLIENT\tPRIVILEGE\tSQUASH\tSECURITY\tASYNC\tNON-PRIV PORTS\tSUBFOLDER")
	for _, rule := range result.Export.Rules {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			rule.Client, rule.Privilege, rule.Squash, rule.Security,
			yesNo(rule.Async), yesNo(rule.AllowNonprivilegedPorts), yesNo(rule.AllowSubfolderAccess))
	}
	return writer.Flush()
}
