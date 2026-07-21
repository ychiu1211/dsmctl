package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/domain/kmip"
)

func newKMIPCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "kmip",
		Short: "Inspect KMIP key-management client/server status (read-only)",
	}
	command.AddCommand(
		newKMIPCapabilitiesCommand(opts),
		newKMIPStatusCommand(opts),
	)
	return command
}

func newKMIPCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show whether KMIP status can be read and whether the NAS supports KMIP",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetKMIPCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "KMIP status read:\t%s\n", yesNo(result.Capabilities.Read))
			fmt.Fprintf(writer, "KMIP supported on NAS:\t%s\n", yesNo(result.Capabilities.Supported))
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

func newKMIPStatusCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "status",
		Short: "Show KMIP client/server role, connection status, and certificate bindings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetKMIPStatus(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			status := result.Status
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Supported on NAS:\t%s\n", yesNo(status.Supported))
			if status.Mode != "" {
				fmt.Fprintf(writer, "Mode:\t%s\n", status.Mode)
			}

			fmt.Fprintln(writer, "\nLocal KMIP server:")
			fmt.Fprintf(writer, "  Enabled:\t%s\n", yesNo(status.Server.Enabled))
			fmt.Fprintf(writer, "  Listen port:\t%s\n", valueOrDash(status.Server.ListenPort))
			fmt.Fprintf(writer, "  Key database:\t%s\n", valueOrDash(status.Server.DatabaseLocation))
			writeKMIPCert(writer, status.Server.Certificate)

			fmt.Fprintln(writer, "\nKMIP client (external KMS):")
			fmt.Fprintf(writer, "  Enabled:\t%s\n", yesNo(status.Client.Enabled))
			fmt.Fprintf(writer, "  Server:\t%s\n", valueOrDash(status.Client.ServerAddress))
			fmt.Fprintf(writer, "  Server port:\t%s\n", valueOrDash(status.Client.ServerPort))
			fmt.Fprintf(writer, "  Server name:\t%s\n", valueOrDash(status.Client.ServerName))
			fmt.Fprintf(writer, "  Connection OK:\t%s\n", yesNo(status.Client.ConnectionOK))
			fmt.Fprintf(writer, "  Last connected:\t%s\n", valueOrDash(status.Client.LastConnectedAt))
			writeKMIPCert(writer, status.Client.Certificate)
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func writeKMIPCert(writer *tabwriter.Writer, cert *kmip.CertBinding) {
	if cert == nil {
		fmt.Fprintf(writer, "  Certificate:\t%s\n", "(none)")
		return
	}
	identity := cert.Subject
	if identity == "" {
		identity = cert.Description
	}
	if identity == "" {
		identity = cert.ID
	}
	fmt.Fprintf(writer, "  Certificate:\t%s\n", valueOrDash(identity))
	if cert.Fingerprint != "" {
		fmt.Fprintf(writer, "  Cert fingerprint:\t%s\n", cert.Fingerprint)
	}
	if cert.ValidTill != "" {
		fmt.Fprintf(writer, "  Cert valid till:\t%s\n", cert.ValidTill)
	}
}
