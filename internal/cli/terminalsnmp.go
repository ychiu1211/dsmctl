package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/terminalsnmp"
)

func newControlPanelTerminalSNMPCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "terminal-snmp",
		Aliases: []string{"terminalsnmp"},
		Short:   "Inspect and manage the Terminal (SSH/Telnet) and SNMP configuration",
		Long: "Read and change the Control Panel > Terminal & SNMP surface. Reads are safe; every change goes " +
			"through the guarded plan/apply contract. Enabling SSH or Telnet, or disabling SSH, is classified high " +
			"risk (it changes the remote-shell attack surface). The SNMP read community is a secret supplied by an " +
			"env:NAME credential reference, resolved only at apply time and never written to the plan, hash, result, " +
			"or logs.",
	}
	command.AddCommand(
		newTerminalSNMPCapabilitiesCommand(opts),
		newTerminalStateCommand(opts),
		newSNMPStateCommand(opts),
		newTerminalPlanCommand(opts),
		newTerminalApplyCommand(opts),
		newSNMPPlanCommand(opts),
		newSNMPApplyCommand(opts),
	)
	return command
}

func newTerminalPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "terminal-plan",
		Short: "Validate a Terminal (SSH enable/port/Telnet/console) patch and emit an approval plan as JSON",
		Long: "Validate a patch-only Terminal change (ssh_enabled, ssh_port, telnet_enabled, console_forbidden) and " +
			"return an approval plan bound to the complete observed Terminal state. Enabling SSH or Telnet, or " +
			"disabling SSH, is classified high risk; an SSH-port change warns to verify the matching firewall/port " +
			"forward separately. This command never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request terminalsnmp.TerminalChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read terminal change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanTerminalChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "terminal change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newTerminalApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "terminal-apply",
		Short: "Apply a Terminal plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.TerminalChangePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read terminal plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyTerminalPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "terminal plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by terminal-snmp terminal-plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newSNMPPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "snmp-plan",
		Short: "Validate an SNMP patch and emit an approval plan as JSON",
		Long: "Validate a patch-only SNMP change (enabled, v1_v2c_enabled, v3_enabled, location, contact, and the " +
			"read community via community_credential_ref) and return an approval plan bound to the complete observed " +
			"SNMP state. The community is a SECRET: supply it as community_credential_ref (env:NAME); only the " +
			"reference name enters the plan and approval hash, never the community value. Enabling SNMPv3 is not " +
			"supported (its DSM credential write wire is unverified); only disabling v3 is available. This command " +
			"never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request terminalsnmp.SNMPChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read snmp change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanSNMPChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "snmp change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newSNMPApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "snmp-apply",
		Short: "Apply an SNMP plan after hash and stale-state validation",
		Long: "Apply an unmodified SNMP plan only while its approval hash and the complete observed state still " +
			"match, then re-read to verify. If the plan sets the read community, the secret is resolved from its " +
			"env:NAME reference only now and rides only the SNMP set request — never the plan, hash, result, or logs.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.SNMPChangePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read snmp plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplySNMPPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "snmp plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by terminal-snmp snmp-plan")
	_ = command.MarkFlagRequired("approve")
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
			fmt.Fprintf(writer, "Terminal write:\t%s\n", yesNo(result.Capabilities.TerminalWrite))
			fmt.Fprintf(writer, "SNMP write:\t%s\n", yesNo(result.Capabilities.SNMPWrite))
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
