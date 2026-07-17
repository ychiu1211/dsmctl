package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
)

func newNASCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show discovered APIs and selected compatibility backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer func() {
				closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = service.Close(closeCtx)
			}()

			result, err := service.GetCompatibility(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return writeCompatibility(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func writeCompatibility(cmd *cobra.Command, result application.CompatibilityResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "DSM:\t%s\n", result.Report.DSM.String())

	fmt.Fprintln(writer, "\nCAPABILITIES")
	for _, capability := range result.Report.Capabilities {
		fmt.Fprintf(writer, "  %s\n", capability)
	}
	if len(result.Report.Quirks) > 0 {
		fmt.Fprintln(writer, "\nQUIRKS")
		for _, quirk := range result.Report.Quirks {
			fmt.Fprintf(writer, "  %s\n", quirk)
		}
	}

	fmt.Fprintln(writer, "\nAPIS")
	fmt.Fprintln(writer, "NAME\tVERSIONS\tPATH\tFORMAT")
	for _, api := range result.Report.APIs {
		fmt.Fprintf(writer, "%s\t%d-%d\t%s\t%s\n", api.Name, api.MinVersion, api.MaxVersion, api.Path, api.RequestFormat)
	}

	fmt.Fprintln(writer, "\nOPERATIONS")
	fmt.Fprintln(writer, "OPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION\tREASON")
	for _, operation := range result.Report.Operations {
		supported := "no"
		if operation.Supported {
			supported = "yes"
		}
		version := ""
		if operation.Version > 0 {
			version = fmt.Sprintf("v%d", operation.Version)
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", operation.Operation, supported, operation.Backend, operation.API, version, operation.Reason)
	}
	return writer.Flush()
}
