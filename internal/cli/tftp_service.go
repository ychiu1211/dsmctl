package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/tftpservice"
)

func newTFTPServiceCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "tftp",
		Short: "Inspect and manage the TFTP service",
	}
	command.AddCommand(
		newTFTPServiceCapabilitiesCommand(opts),
		newTFTPServiceStateCommand(opts),
		newTFTPServicePlanCommand(opts),
		newTFTPServiceApplyCommand(opts),
	)
	return command
}

func newTFTPServiceCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show whether TFTP can be read and changed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetTFTPServiceCapabilities(cmd.Context(), opts.nas)
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

func newTFTPServiceStateCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "state",
		Short: "Show TFTP service, root folder, permission, logging, and timeout",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetTFTPServiceState(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			state := result.TFTPService
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Enabled:\t%s\n", yesNo(state.Enabled))
			fmt.Fprintf(writer, "Root folder:\t%s\n", valueOrDash(state.RootPath))
			fmt.Fprintf(writer, "Permission:\t%s\n", valueOrDash(string(state.Permission)))
			fmt.Fprintf(writer, "Logging:\t%s\n", yesNo(state.LogEnabled))
			fmt.Fprintf(writer, "Allowed client range:\t%s\n", valueOrDash(clientRange(state.ClientIPLow, state.ClientIPHigh)))
			fmt.Fprintf(writer, "Timeout (s):\t%d\n", state.Timeout)
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func clientRange(low, high string) string {
	if low == "" && high == "" {
		return ""
	}
	return fmt.Sprintf("%s - %s", valueOrDash(low), valueOrDash(high))
}

func newTFTPServicePlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a TFTP patch and emit an approval plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request tftpservice.Change
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read TFTP service change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanTFTPServiceChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "TFTP service change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newTFTPServiceApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a TFTP plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.TFTPServicePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read TFTP service plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyTFTPServicePlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "TFTP service plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by the TFTP service plan")
	_ = command.MarkFlagRequired("approve")
	return command
}
