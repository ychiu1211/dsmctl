package cli

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/accountprotection"
)

func newAccountProtectionCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "account-protection",
		Aliases: []string{"ap"},
		Short:   "Inspect and manage DSM account protection (Control Panel > Security > Account)",
		Long: "Read and change the Control Panel > Security > Account surface: Auto Block (settings plus the " +
			"allow/block IP lists), Account Protection thresholds, and the enforced-2FA policy scope. Reads are " +
			"safe; every change goes through the guarded plan/apply contract. Loosening the posture (disabling a " +
			"protection, weakening a threshold, enabling enforced 2FA, or a broad allow rule) is classified high " +
			"risk, and edits that could lock the operator out are refused without an explicit override.",
	}
	command.AddCommand(
		newAccountProtectionCapabilitiesCommand(opts),
		newAccountProtectionAutoBlockCommand(opts),
		newAccountProtectionAutoBlockListCommand(opts),
		newAccountProtectionProtectionCommand(opts),
		newAccountProtectionEnforce2FACommand(opts),
		newAccountProtectionAutoBlockPlanCommand(opts),
		newAccountProtectionAutoBlockApplyCommand(opts),
		newAccountProtectionListPlanCommand(opts),
		newAccountProtectionListApplyCommand(opts),
		newAccountProtectionProtectionPlanCommand(opts),
		newAccountProtectionProtectionApplyCommand(opts),
		newAccountProtectionEnforce2FAPlanCommand(opts),
		newAccountProtectionEnforce2FAApplyCommand(opts),
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
			fmt.Fprintf(writer, "auto block write\t%s\n", yesNo(result.Capabilities.AutoBlockWrite))
			fmt.Fprintf(writer, "auto block list write\t%s\n", yesNo(result.Capabilities.AutoBlockListWrite))
			fmt.Fprintf(writer, "account protection write\t%s\n", yesNo(result.Capabilities.AccountProtectionWrite))
			fmt.Fprintf(writer, "enforce 2fa write\t%s\n", yesNo(result.Capabilities.EnforceTwoFactorWrite))
			fmt.Fprintf(writer, "dos api present (read/write deferred)\t%s\n", yesNo(result.Capabilities.DoSPresent))
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

func newAccountProtectionAutoBlockPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "autoblock-plan",
		Short: "Validate an Auto Block settings patch and emit an approval plan as JSON",
		Long: "Validate a patch-only Auto Block settings change (enabled, attempts, within_minutes, expire_enabled, " +
			"expire_days) and return an approval plan bound to the complete observed settings. Disabling Auto Block, " +
			"raising the attempt threshold, or lengthening the detection window is classified high risk. This command " +
			"never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request accountprotection.AutoBlockChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read auto block change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanAutoBlockChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "auto block change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newAccountProtectionAutoBlockApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "autoblock-apply",
		Short: "Apply an Auto Block settings plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.AutoBlockSettingsPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read auto block plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyAutoBlockPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "auto block plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by account-protection autoblock-plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newAccountProtectionListPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "list-plan",
		Short: "Validate a single allow/block list add or remove and emit an approval plan as JSON",
		Long: "Validate a patch-only add or remove of exactly one Auto Block allow/block list entry (keyed by ip + " +
			"kind) and return an approval plan bound to the complete observed lists and the active connections. " +
			"Blocking an active source or a broad subnet, removing an active source from the allow list, and " +
			"allow-listing a broad subnet are guarded: the first three are refused without allow_lockout_override. " +
			"This command never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request accountprotection.IPListEdit
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read auto block list edit: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanAutoBlockListChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "list edit JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newAccountProtectionListApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "list-apply",
		Short: "Apply an allow/block list edit plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.AutoBlockListPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read auto block list plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyAutoBlockListPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "auto block list plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by account-protection list-plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newAccountProtectionProtectionPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "protection-plan",
		Short: "Validate an Account Protection thresholds patch and emit an approval plan as JSON",
		Long: "Validate a patch-only Account Protection (SmartBlock) thresholds change and return an approval plan " +
			"bound to the complete observed thresholds. Disabling Account Protection, raising an attempt threshold, " +
			"or lengthening a detection window is classified high risk. This command never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request accountprotection.AccountProtectionChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read account protection change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanAccountProtectionThresholdsChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "account protection change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newAccountProtectionProtectionApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "protection-apply",
		Short: "Apply an Account Protection thresholds plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.AccountProtectionThresholdsPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read account protection plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyAccountProtectionThresholdsPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "account protection plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by account-protection protection-plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newAccountProtectionEnforce2FAPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "enforce-2fa-plan",
		Short: "Validate an enforced-2FA policy change and emit an approval plan as JSON",
		Long: "Validate a change to the domain-wide enforced-2FA policy scope (otp_enforce_option) and return an " +
			"approval plan. Every enforced-2FA change is high risk: enabling enforcement can lock out an admin who " +
			"has not enrolled 2FA (and is refused without allow_lockout_override), and disabling it weakens the " +
			"posture. This sets policy only; it never enrolls a user or touches any OTP secret. This command never " +
			"mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request accountprotection.EnforceTwoFactorChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read enforce 2fa change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanEnforceTwoFactorChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "enforce 2fa change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newAccountProtectionEnforce2FAApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "enforce-2fa-apply",
		Short: "Apply an enforced-2FA policy plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.EnforceTwoFactorPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read enforce 2fa plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyEnforceTwoFactorPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "enforce 2fa plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by account-protection enforce-2fa-plan")
	_ = command.MarkFlagRequired("approve")
	return command
}
