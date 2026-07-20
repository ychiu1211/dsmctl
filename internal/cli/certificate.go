package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/domain/certificate"
)

func newCertificateCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "certificate",
		Aliases: []string{"cert"},
		Short:   "Inspect DSM certificates (Control Panel > Security > Certificate)",
	}
	command.AddCommand(
		newCertificateCapabilitiesCommand(opts),
		newCertificateListCommand(opts),
	)
	return command
}

func newCertificateCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show certificate operation support and the selected backend",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetCertificateCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "certificates read\t%s\n", yesNo(result.Capabilities.CertificatesRead))
			fmt.Fprintln(writer, "\nOPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
			for _, op := range result.Report.Operations {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", op.Operation, yesNo(op.Supported), valueOrDash(op.Backend), valueOrDash(op.API), op.Version)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newCertificateListCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List installed certificates, their expiry, and the services they serve",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetCertificates(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Total:\t%d\n", result.Certificates.Total)
			if len(result.Certificates.Certificates) == 0 {
				fmt.Fprintln(writer, "No certificates installed.")
				return writer.Flush()
			}
			fmt.Fprintln(writer, "\nSUBJECT\tDEFAULT\tEXPIRES\tRENEWABLE\tBROKEN\tSERVICES\tID")
			for _, cert := range result.Certificates.Certificates {
				subject := cert.Subject.CommonName
				if subject == "" {
					subject = valueOrDash(cert.Description)
				}
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					subject, yesNo(cert.IsDefault), certExpiry(cert.ValidTill, cert.ValidTillUnix),
					yesNo(cert.Renewable), yesNo(cert.IsBroken), certServiceList(cert), cert.ID)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

// certExpiry renders the not-after with a computed days-to-expiry hint.
func certExpiry(raw string, unix int64) string {
	if unix <= 0 {
		return valueOrDash(raw)
	}
	days := int(time.Until(time.Unix(unix, 0)).Hours() / 24)
	when := time.Unix(unix, 0).Local().Format("2006-01-02")
	switch {
	case days < 0:
		return fmt.Sprintf("%s (expired)", when)
	case days == 0:
		return fmt.Sprintf("%s (today)", when)
	default:
		return fmt.Sprintf("%s (%dd)", when, days)
	}
}

func certServiceList(cert certificate.Certificate) string {
	if len(cert.Services) == 0 {
		return "-"
	}
	names := make([]string, 0, len(cert.Services))
	for _, svc := range cert.Services {
		name := svc.DisplayName
		if name == "" {
			name = svc.Service
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}
