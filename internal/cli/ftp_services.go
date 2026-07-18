package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/ftpservices"
)

func newFTPServicesCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "ftp",
		Short: "Inspect and manage FTP, FTPS, and SFTP file services",
	}
	command.AddCommand(
		newFTPServicesCapabilitiesCommand(opts),
		newFTPServicesStateCommand(opts),
		newFTPServicesPlanCommand(opts),
		newFTPServicesApplyCommand(opts),
	)
	return command
}

func newFTPServicesCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show which FTP and SFTP operations are supported",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFTPServicesCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "FTP read:\t%s\n", yesNo(result.Capabilities.FTPRead))
			fmt.Fprintf(writer, "FTP set:\t%s\n", yesNo(result.Capabilities.FTPSet))
			fmt.Fprintf(writer, "SFTP read:\t%s\n", yesNo(result.Capabilities.SFTPRead))
			fmt.Fprintf(writer, "SFTP set:\t%s\n", yesNo(result.Capabilities.SFTPSet))
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

func newFTPServicesStateCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "state",
		Short: "Show FTP, FTPS, and SFTP service settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFTPServicesState(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "FTP (plain):\t%s\n", yesNo(result.FTPServices.FTP.Plain))
			fmt.Fprintf(writer, "FTPS (TLS):\t%s\n", yesNo(result.FTPServices.FTP.FTPS))
			if result.FTPServices.SFTP == nil {
				fmt.Fprintf(writer, "SFTP:\t%s\n", "(not supported)")
			} else {
				fmt.Fprintf(writer, "SFTP:\t%s\n", yesNo(result.FTPServices.SFTP.Enabled))
				fmt.Fprintf(writer, "SFTP port:\t%d\n", result.FTPServices.SFTP.Port)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newFTPServicesPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate an FTP/SFTP patch and emit an approval plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request ftpservices.Change
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read FTP services change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanFTPServicesChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "FTP services change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newFTPServicesApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply an FTP/SFTP plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.FTPServicesPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read FTP services plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyFTPServicesPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "FTP services plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by the FTP services plan")
	_ = command.MarkFlagRequired("approve")
	return command
}
