package cli

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/domain/accountprotection"
)

func newAccountProtectionCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "account-protection",
		Aliases: []string{"ap"},
		Short:   "Inspect DSM account protection (Control Panel > Security > Account)",
		Long: "Read the Control Panel > Security > Account surface: Auto Block (settings plus the allow/block " +
			"IP lists), Account Protection thresholds, and the enforced-2FA policy scope. This slice is read-only; " +
			"guarded writes are a deferred follow-on.",
	}
	command.AddCommand(
		newAccountProtectionCapabilitiesCommand(opts),
		newAccountProtectionAutoBlockCommand(opts),
		newAccountProtectionAutoBlockListCommand(opts),
		newAccountProtectionProtectionCommand(opts),
		newAccountProtectionEnforce2FACommand(opts),
	)
	return command
}

func newAccountProtectionCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show account-protection read support and the selected backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetAccountProtectionCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "auto block read\t%s\n", yesNo(result.Capabilities.AutoBlockRead))
			fmt.Fprintf(writer, "auto block list read\t%s\n", yesNo(result.Capabilities.AutoBlockListRead))
			fmt.Fprintf(writer, "account protection read\t%s\n", yesNo(result.Capabilities.AccountProtectionRead))
			fmt.Fprintf(writer, "enforce 2fa read\t%s\n", yesNo(result.Capabilities.EnforceTwoFactorRead))
			fmt.Fprintf(writer, "dos api present (read deferred)\t%s\n", yesNo(result.Capabilities.DoSPresent))
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

func newAccountProtectionAutoBlockCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "autoblock",
		Short: "Show Auto Block settings (block a source after repeated failed sign-ins)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetAutoBlockSettings(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			s := result.Settings
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Enabled:\t%s\n", yesNo(s.Enabled))
			fmt.Fprintf(writer, "Attempts:\t%d\n", s.Attempts)
			fmt.Fprintf(writer, "Within minutes:\t%d\n", s.WithinMinutes)
			fmt.Fprintf(writer, "Block expiration:\t%s\n", yesNo(s.ExpireEnabled))
			if s.ExpireEnabled {
				fmt.Fprintf(writer, "Expire after days:\t%d\n", s.ExpireDays)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newAccountProtectionAutoBlockListCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "autoblock-list",
		Short: "Show the Auto Block allow and block IP lists",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetAutoBlockLists(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Allow entries:\t%d\n", result.Lists.Allow.Total)
			fmt.Fprintf(writer, "Block entries:\t%d\n", result.Lists.Block.Total)
			printAutoBlockIPList(writer, result.Lists.Allow)
			printAutoBlockIPList(writer, result.Lists.Block)
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func printAutoBlockIPList(writer *tabwriter.Writer, list accountprotection.IPList) {
	if len(list.Entries) == 0 {
		return
	}
	fmt.Fprintf(writer, "\n%s LIST\tRECORDED\tREASON\n", upperKind(list.Kind))
	for _, entry := range list.Entries {
		recorded := "-"
		if entry.RecordTime > 0 {
			recorded = time.Unix(entry.RecordTime, 0).Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\n", entry.IP, recorded, valueOrDash(entry.Reason))
	}
}

func upperKind(kind string) string {
	switch kind {
	case "allow":
		return "ALLOW"
	case "block":
		return "BLOCK"
	default:
		return kind
	}
}

func newAccountProtectionProtectionCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "protection",
		Short: "Show Account Protection thresholds (block untrusted clients after failed sign-ins)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetAccountProtection(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			p := result.Protection
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Enabled:\t%s\n", yesNo(p.Enabled))
			fmt.Fprintln(writer, "\nCLIENT\tATTEMPTS\tWITHIN MIN\tBLOCK MIN")
			fmt.Fprintf(writer, "untrusted\t%d\t%d\t%d\n", p.UntrustedAttempts, p.UntrustedWithinMinutes, p.UntrustedBlockMinutes)
			fmt.Fprintf(writer, "trusted\t%d\t%d\t%d\n", p.TrustedAttempts, p.TrustedWithinMinutes, p.TrustedBlockMinutes)
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newAccountProtectionEnforce2FACommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "enforce-2fa",
		Short: "Show the domain-wide enforced-2FA policy scope",
		Long: "Show the enforced-2FA/MFA policy scope. This reads the policy only; it never reads any user's " +
			"OTP secret, seed, or recovery codes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetEnforceTwoFactor(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Enforced:\t%s\n", yesNo(result.Policy.Enabled))
			fmt.Fprintf(writer, "Scope:\t%s\n", result.Policy.Option)
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}
