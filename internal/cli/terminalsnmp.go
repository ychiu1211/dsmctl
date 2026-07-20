package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newControlPanelTerminalSNMPCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "terminal-snmp",
		Aliases: []string{"terminalsnmp"},
		Short:   "Inspect the Terminal (SSH/Telnet) and SNMP configuration",
	}
	command.AddCommand(
		newTerminalSNMPCapabilitiesCommand(opts),
		newTerminalStateCommand(opts),
		newSNMPStateCommand(opts),
	)
	return command
}

func newTerminalSNMPCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show Terminal and SNMP read support and the selected DSM backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetTerminalSNMPCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Module:\t%s\n", result.Capabilities.Module)
			fmt.Fprintf(writer, "Terminal read:\t%s\n", yesNo(result.Capabilities.TerminalRead))
			fmt.Fprintf(writer, "SNMP read:\t%s\n", yesNo(result.Capabilities.SNMPRead))
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

func newTerminalStateCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "terminal",
		Short: "Show whether SSH and Telnet are enabled and the SSH port",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetTerminalState(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "SSH enabled:\t%s\n", yesNo(result.Terminal.SSHEnabled))
			fmt.Fprintf(writer, "SSH port:\t%d\n", result.Terminal.SSHPort)
			fmt.Fprintf(writer, "Telnet enabled:\t%s\n", yesNo(result.Terminal.TelnetEnabled))
			fmt.Fprintf(writer, "Console forbidden:\t%s\n", yesNo(result.Terminal.ConsoleForbidden))
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newSNMPStateCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "snmp",
		Short: "Show SNMP service state, versions, device info, and whether a community/v3/trap is configured",
		Long: "Show the normalized SNMP configuration. The read-only community string, the SNMPv3 auth/privacy " +
			"passwords, and any trap community are secrets and are never read or displayed; only presence flags " +
			"(community configured, trap configured) and non-secret fields (versions, location, contact, v3 " +
			"username) are shown.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetSNMPState(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			snmp := result.SNMP
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "SNMP enabled:\t%s\n", yesNo(snmp.Enabled))
			fmt.Fprintf(writer, "SNMPv1/v2c enabled:\t%s\n", yesNo(snmp.V1V2cEnabled))
			fmt.Fprintf(writer, "SNMPv3 enabled:\t%s\n", yesNo(snmp.V3Enabled))
			fmt.Fprintf(writer, "Location:\t%s\n", valueOrDash(snmp.Location))
			fmt.Fprintf(writer, "Contact:\t%s\n", valueOrDash(snmp.Contact))
			fmt.Fprintf(writer, "Community configured:\t%s\n", yesNo(snmp.CommunityConfigured))
			fmt.Fprintf(writer, "SNMPv3 user:\t%s\n", valueOrDash(snmp.V3User))
			fmt.Fprintf(writer, "Trap configured:\t%s\n", yesNo(snmp.TrapConfigured))
			fmt.Fprintf(writer, "Trap host present:\t%s\n", yesNo(snmp.TrapHostPresent))
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}
