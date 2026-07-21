package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/firewall"
)

func newFirewallCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "firewall",
		Aliases: []string{"fw"},
		Short:   "Inspect and manage the DSM firewall (Control Panel > Security > Firewall)",
		Long: "Read and change the Control Panel > Security > Firewall surface: whether the firewall is enabled and " +
			"which profile is active, the firewall profiles, the network adapters, and each profile's per-adapter " +
			"default policy and ordered rule list. Reads are safe; every change (rule create/delete/reorder, default " +
			"policy, firewall enable/disable) goes through the guarded plan/apply contract. A mandatory never-lockout " +
			"guard refuses any effect-taking change whose resulting ruleset would deny the operator's current session, " +
			"unless allow_connectivity_break is set.",
	}
	command.AddCommand(
		newFirewallCapabilitiesCommand(opts),
		newFirewallStatusCommand(opts),
		newFirewallProfilesCommand(opts),
		newFirewallRulesCommand(opts),
		newFirewallProfilePlanCommand(opts),
		newFirewallProfileApplyCommand(opts),
		newFirewallEnablePlanCommand(opts),
		newFirewallEnableApplyCommand(opts),
	)
	return command
}

func newFirewallProfilePlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "profile-plan",
		Short: "Validate a firewall profile change (rules + default policy) and emit an approval plan as JSON",
		Long: "Validate a full-desired-state change to a firewall profile's adapter sections (each adapter's default " +
			"policy and complete ordered rule list) and return an approval plan bound to the complete observed state " +
			"and the management connection tuple. A change that would take effect (activating a profile, or editing the " +
			"active profile while the firewall is enabled) is run through the never-lockout guard: if the resulting " +
			"ruleset would deny the operator's session it is refused without allow_connectivity_break. This command " +
			"never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request firewall.ProfileChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read firewall profile change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanFirewallProfileChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "firewall profile change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newFirewallProfileApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "profile-apply",
		Short: "Apply a firewall profile plan after hash, stale-state, and never-lockout validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.FirewallProfilePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read firewall profile plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyFirewallProfilePlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "firewall profile plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by firewall profile-plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newFirewallEnablePlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "enable-plan",
		Short: "Validate a firewall enable/disable (or active-profile switch) and emit an approval plan as JSON",
		Long: "Validate a change to the global firewall enable flag and/or the active profile and return an approval " +
			"plan. Enabling the firewall (or switching the active profile while enabled) runs the never-lockout guard " +
			"against the profile that would become active: it is refused if the resulting ruleset would deny the " +
			"operator's session and allow_connectivity_break is not set, or if the session source cannot be determined " +
			"and no keep_reachable is supplied. Disabling removes all filtering and cannot lock the operator out. This " +
			"command never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request firewall.EnableChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read firewall enable change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanFirewallEnableChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "firewall enable change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newFirewallEnableApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "enable-apply",
		Short: "Apply a firewall enable/disable plan after hash, stale-state, and never-lockout validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.FirewallEnablePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read firewall enable plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyFirewallEnablePlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "firewall enable plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by firewall enable-plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newFirewallCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show firewall read support and the selected backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFirewallCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "status read\t%s\n", yesNo(result.Capabilities.StatusRead))
			fmt.Fprintf(writer, "profiles read\t%s\n", yesNo(result.Capabilities.ProfilesRead))
			fmt.Fprintf(writer, "adapters read\t%s\n", yesNo(result.Capabilities.AdaptersRead))
			fmt.Fprintf(writer, "rules read\t%s\n", yesNo(result.Capabilities.RulesRead))
			fmt.Fprintf(writer, "rule fields wire-unverified\t%s\n", yesNo(result.Capabilities.RuleFieldsWireUnverified))
			fmt.Fprintf(writer, "profile write\t%s\n", yesNo(result.Capabilities.ProfileWrite))
			fmt.Fprintf(writer, "enable write\t%s\n", yesNo(result.Capabilities.EnableWrite))
			fmt.Fprintf(writer, "mutations\t%s\n", yesNo(result.Capabilities.Mutations))
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

func newFirewallStatusCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "status",
		Short: "Show whether the firewall is enabled, the active profile, and the adapters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFirewallStatus(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Enabled:\t%s\n", yesNo(result.Status.Enabled))
			fmt.Fprintf(writer, "Active profile:\t%s\n", valueOrDash(result.Status.ActiveProfile))
			fmt.Fprintf(writer, "Adapters:\t%s\n", valueOrDash(strings.Join(result.Status.Adapters, ", ")))
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newFirewallProfilesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "profiles",
		Short: "Show the firewall profiles, marking the active one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFirewallProfiles(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintln(writer, "\nPROFILE\tACTIVE")
			for _, profile := range result.Profiles {
				fmt.Fprintf(writer, "%s\t%s\n", profile.Name, yesNo(profile.IsActive))
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newFirewallRulesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	var profile string
	command := &cobra.Command{
		Use:   "rules",
		Short: "Show each profile's per-adapter default policy and ordered rules",
		Long: "Show the firewall rule view: for each profile (or the one named by --profile), the per-adapter default " +
			"(no-match) policy and the ordered rule list. Note: both DSM-shipped profiles carry no rules by default, so " +
			"the per-rule field decoding is wire-unverified until a live rule confirms the field names.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFirewallRules(cmd.Context(), opts.nas, profile)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Active profile:\t%s\n", valueOrDash(result.RuleSet.ActiveProfile))
			for _, profileRules := range result.RuleSet.Profiles {
				writeFirewallProfileRules(writer, profileRules)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().StringVar(&profile, "profile", "", "limit to a single profile by name (default: every profile)")
	return command
}

func writeFirewallProfileRules(writer *tabwriter.Writer, profileRules firewall.ProfileRules) {
	active := ""
	if profileRules.IsActive {
		active = " (active)"
	}
	fmt.Fprintf(writer, "\nPROFILE %s%s\n", profileRules.Profile, active)
	if len(profileRules.Adapters) == 0 {
		fmt.Fprintln(writer, "  (no configured adapters)")
		return
	}
	for _, adapter := range profileRules.Adapters {
		fmt.Fprintf(writer, "  adapter %s\tdefault policy: %s\trules: %d\n", adapter.Adapter, valueOrDash(adapter.Policy), adapter.Total)
		if len(adapter.Rules) == 0 {
			continue
		}
		fmt.Fprintln(writer, "  #\tENABLED\tPOLICY\tPROTO\tSOURCE\tPORTS\tNAME")
		for index, rule := range adapter.Rules {
			fmt.Fprintf(writer, "  %d\t%s\t%s\t%s\t%s\t%s\t%s\n",
				index+1, yesNo(rule.Enabled), valueOrDash(rule.Policy), valueOrDash(rule.Protocol),
				valueOrDash(firewallSource(rule)), valueOrDash(rule.Ports), valueOrDash(rule.Name))
		}
	}
}

func firewallSource(rule firewall.Rule) string {
	if rule.Source != "" {
		return rule.Source
	}
	return rule.SourceGroup
}
