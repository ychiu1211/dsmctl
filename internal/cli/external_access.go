package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/externalaccess"
)

// newExternalAccessCommand exposes the read-only Control Panel → External
// Access surface: Synology Account binding, QuickConnect, and DDNS.
func newExternalAccessCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "external-access",
		Aliases: []string{"externalaccess"},
		Short:   "Inspect Synology Account, QuickConnect, and DDNS external-access settings",
	}
	command.AddCommand(
		newExternalAccessCapabilitiesCommand(opts),
		newExternalAccessAccountCommand(opts),
		newExternalAccessQuickConnectCommand(opts),
		newExternalAccessDDNSCommand(opts),
		newExternalAccessPortForwardCommand(opts),
	)
	return command
}

func newExternalAccessCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show which External Access read areas are available and their selected DSM backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetExternalAccessCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeExternalAccessCapabilities(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newExternalAccessAccountCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "account",
		Short: "Show the Synology Account (MyDS) binding; never reveals the account token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetExternalAccessAccount(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeExternalAccessAccount(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newExternalAccessQuickConnectCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:     "quickconnect",
		Aliases: []string{"qc"},
		Short:   "Show QuickConnect configuration, relay setting, and live status",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetExternalAccessQuickConnect(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeExternalAccessQuickConnect(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.AddCommand(
		newExternalAccessQuickConnectPlanCommand(opts),
		newExternalAccessQuickConnectApplyCommand(opts),
		newExternalAccessQuickConnectConfigCommand(opts),
		newExternalAccessQuickConnectPermissionCommand(opts),
	)
	return command
}

func newExternalAccessQuickConnectPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a QuickConnect relay-toggle patch and emit an approval plan as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request externalaccess.QuickConnectChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read QuickConnect change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanExternalAccessQuickConnectChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "QuickConnect change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newExternalAccessQuickConnectApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a QuickConnect plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.ExternalAccessQuickConnectPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read QuickConnect plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyExternalAccessQuickConnectPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "QuickConnect plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by quickconnect plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newExternalAccessDDNSCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "ddns",
		Short: "Show configured DDNS records and detected external addresses",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetExternalAccessDDNS(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeExternalAccessDDNS(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.AddCommand(
		newExternalAccessDDNSPlanCommand(opts),
		newExternalAccessDDNSApplyCommand(opts),
	)
	return command
}

func newExternalAccessPortForwardCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:     "port-forward",
		Aliases: []string{"portforward", "router"},
		Short:   "Show the router configuration and port-forwarding rules",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetExternalAccessPortForward(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeExternalAccessPortForward(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func writeExternalAccessCapabilities(cmd *cobra.Command, result application.ExternalAccessCapabilitiesResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Account:\t%s\n", yesNo(result.Capabilities.Account))
	fmt.Fprintf(writer, "QuickConnect:\t%s\n", yesNo(result.Capabilities.QuickConnect))
	fmt.Fprintf(writer, "QuickConnect relay (set):\t%s\n", yesNo(result.Capabilities.QuickConnectSet))
	fmt.Fprintf(writer, "DDNS:\t%s\n", yesNo(result.Capabilities.DDNS))
	fmt.Fprintf(writer, "Port forwarding:\t%s\n", yesNo(result.Capabilities.PortForward))
	fmt.Fprintln(writer, "\nOPERATIONS")
	fmt.Fprintln(writer, "OPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
	for _, operation := range result.Report.Operations {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), valueOrDash(operation.API), operation.Version)
	}
	return writer.Flush()
}

func writeExternalAccessAccount(cmd *cobra.Command, result application.ExternalAccessAccountResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	account := result.Account
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Signed in:\t%s\n", yesNo(account.LoggedIn))
	fmt.Fprintf(writer, "Activated:\t%s\n", yesNo(account.Activated))
	fmt.Fprintf(writer, "Account:\t%s\n", valueOrDash(account.Account))
	fmt.Fprintf(writer, "MyDS ID:\t%s\n", valueOrDash(account.MyDSID))
	fmt.Fprintf(writer, "Serial:\t%s\n", valueOrDash(account.Serial))
	return writer.Flush()
}

func writeExternalAccessQuickConnect(cmd *cobra.Command, result application.ExternalAccessQuickConnectResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	qc := result.QuickConnect
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Enabled:\t%s\n", yesNo(qc.Enabled))
	fmt.Fprintf(writer, "QuickConnect ID:\t%s\n", valueOrDash(qc.ID))
	fmt.Fprintf(writer, "Region:\t%s\n", valueOrDash(qc.Region))
	fmt.Fprintf(writer, "Relay enabled:\t%s\n", yesNoPointer(qc.RelayEnabled))
	fmt.Fprintf(writer, "Connection status:\t%s\n", valueOrDash(qc.ConnectionStatus))
	fmt.Fprintf(writer, "Alias status:\t%s\n", valueOrDash(qc.AliasStatus))
	if qc.ID != "" && qc.Domain != "" {
		fmt.Fprintf(writer, "Relay hostname:\t%s.%s\n", qc.ID, qc.Domain)
	}
	if qc.ID != "" && qc.DirectDomain != "" {
		fmt.Fprintf(writer, "Direct hostname:\t%s.%s\n", qc.ID, qc.DirectDomain)
	}
	if qc.Services != nil {
		fmt.Fprintln(writer, "\nSERVICES")
		fmt.Fprintln(writer, "SERVICE\tEXPOSED")
		for _, service := range qc.Services {
			fmt.Fprintf(writer, "%s\t%s\n", service.ID, yesNo(service.Enabled))
		}
	}
	return writer.Flush()
}

func writeExternalAccessDDNS(cmd *cobra.Command, result application.ExternalAccessDDNSResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	ddns := result.DDNS
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Next update:\t%s\n", valueOrDash(ddns.NextUpdateTime))
	fmt.Fprintln(writer, "\nRECORDS")
	if len(ddns.Records) == 0 {
		fmt.Fprintln(writer, "(none configured)")
	} else {
		fmt.Fprintln(writer, "HOSTNAME\tPROVIDER\tSTATUS\tIPV4\tIPV6")
		for _, record := range ddns.Records {
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", valueOrDash(record.Hostname), valueOrDash(record.Provider), valueOrDash(record.Status), valueOrDash(record.IPv4), valueOrDash(record.IPv6))
		}
	}
	fmt.Fprintln(writer, "\nEXTERNAL ADDRESSES")
	if len(ddns.ExternalAddress) == 0 {
		fmt.Fprintln(writer, "(none detected)")
	} else {
		fmt.Fprintln(writer, "TYPE\tIPV4\tIPV6")
		for _, address := range ddns.ExternalAddress {
			fmt.Fprintf(writer, "%s\t%s\t%s\n", valueOrDash(address.Type), valueOrDash(address.IP), valueOrDash(address.IPv6))
		}
	}
	return writer.Flush()
}

func writeExternalAccessPortForward(cmd *cobra.Command, result application.ExternalAccessPortForwardResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	pf := result.PortForward
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Router brand:\t%s\n", valueOrDash(pf.Router.Brand))
	fmt.Fprintf(writer, "Router model:\t%s\n", valueOrDash(pf.Router.Model))
	fmt.Fprintf(writer, "Router version:\t%s\n", valueOrDash(pf.Router.Version))
	fmt.Fprintf(writer, "Supports UPnP:\t%s\n", valueOrDash(pf.Router.SupportUPnP))
	fmt.Fprintf(writer, "Supports NAT-PMP:\t%s\n", valueOrDash(pf.Router.SupportNATPMP))
	fmt.Fprintf(writer, "Can change port:\t%s\n", yesNo(pf.Router.SupportChangePort))
	fmt.Fprintln(writer, "\nRULES")
	if len(pf.Rules) == 0 {
		fmt.Fprintln(writer, "(none configured)")
	} else {
		fmt.Fprintln(writer, "DESCRIPTION\tPROTOCOL\tPUBLIC\tPRIVATE")
		for _, rule := range pf.Rules {
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", valueOrDash(rule.Description), valueOrDash(rule.Protocol), valueOrDash(rule.PublicPort), valueOrDash(rule.PrivatePort))
		}
	}
	return writer.Flush()
}

func yesNoPointer(value *bool) string {
	if value == nil {
		return "-"
	}
	return yesNo(*value)
}

func newExternalAccessQuickConnectConfigCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Guarded QuickConnect enable/alias/region change (plan/apply; high risk)",
	}
	var inputPath, outputPath string
	plan := &cobra.Command{
		Use:   "plan",
		Short: "Validate a QuickConnect enable/alias/region patch and emit an approval plan as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request externalaccess.QuickConnectConfigChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read QuickConnect config change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.PlanExternalAccessQuickConnectConfigChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, result)
		},
	}
	plan.Flags().StringVarP(&inputPath, "file", "f", "-", "config change JSON file, or - for stdin")
	plan.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	var applyPath, approvalHash string
	apply := &cobra.Command{
		Use:   "apply",
		Short: "Apply a QuickConnect config plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var p application.ExternalAccessQuickConnectConfigPlan
			if err := decodeJSONInput(cmd, applyPath, &p); err != nil {
				return fmt.Errorf("read QuickConnect config plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyExternalAccessQuickConnectConfigPlan(cmd.Context(), p, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	apply.Flags().StringVarP(&applyPath, "file", "f", "-", "config plan JSON file, or - for stdin")
	apply.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by config plan")
	_ = apply.MarkFlagRequired("approve")
	command.AddCommand(plan, apply)
	return command
}

func newExternalAccessQuickConnectPermissionCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "permission",
		Short: "Guarded QuickConnect per-service exposure change (plan/apply; high risk)",
	}
	var inputPath, outputPath string
	plan := &cobra.Command{
		Use:   "plan",
		Short: "Validate a per-service exposure patch and emit an approval plan as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request externalaccess.QuickConnectPermissionChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read QuickConnect permission change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.PlanExternalAccessQuickConnectPermissionChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, result)
		},
	}
	plan.Flags().StringVarP(&inputPath, "file", "f", "-", "permission change JSON file, or - for stdin")
	plan.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	var applyPath, approvalHash string
	apply := &cobra.Command{
		Use:   "apply",
		Short: "Apply a QuickConnect permission plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var p application.ExternalAccessQuickConnectPermissionPlan
			if err := decodeJSONInput(cmd, applyPath, &p); err != nil {
				return fmt.Errorf("read QuickConnect permission plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyExternalAccessQuickConnectPermissionPlan(cmd.Context(), p, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	apply.Flags().StringVarP(&applyPath, "file", "f", "-", "permission plan JSON file, or - for stdin")
	apply.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by permission plan")
	_ = apply.MarkFlagRequired("approve")
	command.AddCommand(plan, apply)
	return command
}

func newExternalAccessDDNSPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a DDNS record create/set/delete and emit an approval plan as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request externalaccess.DDNSRecordChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read DDNS change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.PlanExternalAccessDDNSChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "DDNS change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newExternalAccessDDNSApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a DDNS record plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var p application.ExternalAccessDDNSPlan
			if err := decodeJSONInput(cmd, inputPath, &p); err != nil {
				return fmt.Errorf("read DDNS plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyExternalAccessDDNSPlan(cmd.Context(), p, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "DDNS plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by ddns plan")
	_ = command.MarkFlagRequired("approve")
	return command
}
