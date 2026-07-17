package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
)

func newFileServicesCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "file-services",
		Aliases: []string{"fileservices"},
		Short:   "Inspect and manage global SMB and NFS services",
	}
	command.AddCommand(
		newFileServicesCapabilitiesCommand(opts),
		newSMBCommand(opts),
		newNFSCommand(opts),
		newFileServicesPlanCommand(opts),
		newFileServicesApplyCommand(opts),
	)
	return command
}

func newSMBCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "smb", Short: "Inspect global SMB service settings"}
	command.AddCommand(newSMBStateCommand(opts))
	return command
}

func newNFSCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "nfs", Short: "Inspect global NFS service settings"}
	command.AddCommand(newNFSStateCommand(opts))
	return command
}

func newSMBStateCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "state",
		Short: "Show normalized SMB service and protocol settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetSMBState(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeSMBState(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newNFSStateCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "state",
		Short: "Show normalized NFS service, protocol, and NFSv4 domain settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNFSState(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeNFSState(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newFileServicesCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show SMB/NFS support and selected DSM backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFileServiceCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return writeFileServiceCapabilities(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newFileServicesPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate an SMB or NFS patch and emit an approval plan as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request controlpanel.FileServiceChangeRequest
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read File Services change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanFileServiceChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "File Services change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newFileServicesApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply an SMB or NFS plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.FileServicePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read File Services plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyFileServicePlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "File Services plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by File Services plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func writeSMBState(cmd *cobra.Command, result application.SMBStateResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Enabled:\t%s\n", yesNo(result.SMB.Enabled))
	fmt.Fprintf(writer, "Workgroup:\t%s\n", valueOrDash(result.SMB.Workgroup))
	fmt.Fprintf(writer, "Protocol range:\t%s - %s\n", valueOrDash(string(result.SMB.MinimumProtocol)), valueOrDash(string(result.SMB.MaximumProtocol)))
	fmt.Fprintf(writer, "Transport encryption:\t%s\n", valueOrDash(string(result.SMB.TransportEncryption)))
	fmt.Fprintf(writer, "Server signing:\t%s\n", valueOrDash(string(result.SMB.ServerSigning)))
	return writer.Flush()
}

func writeNFSState(cmd *cobra.Command, result application.NFSStateResult) error {
	protocols := make([]string, 0, len(result.NFS.SupportedProtocols))
	for _, protocol := range result.NFS.SupportedProtocols {
		protocols = append(protocols, string(protocol))
	}
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Enabled:\t%s\n", yesNo(result.NFS.Enabled))
	fmt.Fprintf(writer, "Maximum protocol:\t%s\n", result.NFS.MaximumProtocol)
	fmt.Fprintf(writer, "Supported protocols:\t%s\n", valueOrDash(strings.Join(protocols, ", ")))
	fmt.Fprintf(writer, "NFSv4 domain:\t%s\n", valueOrDash(result.NFS.NFSv4Domain))
	return writer.Flush()
}

func writeFileServiceCapabilities(cmd *cobra.Command, result application.FileServiceCapabilitiesResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintln(writer, "MODULE\tREAD\tSET\tSET ADVANCED")
	fmt.Fprintf(writer, "SMB\t%s\t%s\t-\n", yesNo(result.Capabilities.SMB.Read), yesNo(result.Capabilities.SMB.Set))
	fmt.Fprintf(writer, "NFS\t%s\t%s\t%s\n", yesNo(result.Capabilities.NFS.Read), yesNo(result.Capabilities.NFS.Set), yesNo(result.Capabilities.NFS.SetAdvanced))
	fmt.Fprintln(writer, "\nOPERATIONS")
	fmt.Fprintln(writer, "OPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
	for _, operation := range result.Report.Operations {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), valueOrDash(operation.API), operation.Version)
	}
	return writer.Flush()
}
