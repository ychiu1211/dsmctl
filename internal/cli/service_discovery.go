package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/servicediscovery"
)

func newServiceDiscoveryCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "discovery",
		Short: "Inspect and manage File Services discovery (Time Machine advertising, WS-Discovery)",
	}
	command.AddCommand(
		newServiceDiscoveryCapabilitiesCommand(opts),
		newServiceDiscoveryStateCommand(opts),
		newServiceDiscoveryPlanCommand(opts),
		newServiceDiscoveryApplyCommand(opts),
	)
	return command
}

func newServiceDiscoveryCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show which service-discovery operations are supported",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetServiceDiscoveryCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Time Machine read:\t%s\n", yesNo(result.Capabilities.Read))
			fmt.Fprintf(writer, "Time Machine set:\t%s\n", yesNo(result.Capabilities.Set))
			fmt.Fprintf(writer, "WS-Discovery:\t%s\n", yesNo(result.Capabilities.WSDiscovery))
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

func newServiceDiscoveryStateCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "state",
		Short: "Show Time Machine advertising and WS-Discovery settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetServiceDiscoveryState(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "SMB Time Machine:\t%s\n", yesNo(result.ServiceDiscovery.SMBTimeMachine))
			fmt.Fprintf(writer, "AFP Time Machine:\t%s\n", yesNo(result.ServiceDiscovery.AFPTimeMachine))
			if result.ServiceDiscovery.WSDiscovery == nil {
				fmt.Fprintf(writer, "WS-Discovery:\t%s\n", "(not supported)")
			} else {
				fmt.Fprintf(writer, "WS-Discovery:\t%s\n", yesNo(*result.ServiceDiscovery.WSDiscovery))
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newServiceDiscoveryPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a service-discovery patch and emit an approval plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request servicediscovery.Change
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read service discovery change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanServiceDiscoveryChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "service discovery change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newServiceDiscoveryApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a service-discovery plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.ServiceDiscoveryPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read service discovery plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyServiceDiscoveryPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "service discovery plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by the service discovery plan")
	_ = command.MarkFlagRequired("approve")
	return command
}
