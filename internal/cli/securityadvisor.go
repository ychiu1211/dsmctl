package cli

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/securityadvisor"
)

func newSecurityAdvisorCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "security-advisor",
		Aliases: []string{"secadvisor", "sa"},
		Short:   "Inspect and manage the Security Advisor scan (Control Panel > Security > Security Advisor)",
		Long: "Read the Security Advisor surface: the last-scan status, the per-category findings with their " +
			"severity breakdown, and the current scan schedule and security baseline. Trigger a scan on demand " +
			"with 'run', and change the schedule and baseline through the guarded plan/apply contract.",
	}
	command.AddCommand(
		newSecurityAdvisorCapabilitiesCommand(opts),
		newSecurityAdvisorStatusCommand(opts),
		newSecurityAdvisorFindingsCommand(opts),
		newSecurityAdvisorScheduleCommand(opts),
		newSecurityAdvisorPlanCommand(opts),
		newSecurityAdvisorApplyCommand(opts),
		newSecurityAdvisorRunCommand(opts),
	)
	return command
}

func newSecurityAdvisorPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a schedule + baseline patch and emit an approval plan as JSON",
		Long: "Validate a patch-only schedule and security-baseline change and return an approval plan bound to " +
			"the complete observed configuration. Loosening the baseline (business to home) or disabling the " +
			"scheduled scan is classified high risk. This command never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request securityadvisor.ScheduleChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read security advisor change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanSecurityAdvisorScheduleChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "schedule change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newSecurityAdvisorApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a schedule + baseline plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.SecurityAdvisorSchedulePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read security advisor plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplySecurityAdvisorSchedulePlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "security advisor plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by security-advisor plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newSecurityAdvisorRunCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "run",
		Short: "Trigger a full Security Advisor scan now (load-heavy action, no configuration change)",
		Long: "Trigger a full Security Advisor scan on demand. A scan is CPU/IO-heavy on the NAS and changes no " +
			"configuration; track its progress with 'security-advisor status'. This action is never invoked " +
			"implicitly by a read.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.RunSecurityScan(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Scan started:\t%s\n", yesNo(result.Scan.Started))
			fmt.Fprintf(writer, "Backend:\t%s %s v%d\n", result.Scan.API, result.Scan.Method, result.Scan.Version)
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newSecurityAdvisorCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show Security Advisor operation support and the selected backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetSecurityAdvisorCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "status + findings read\t%s\n", yesNo(result.Capabilities.StatusRead))
			fmt.Fprintf(writer, "schedule + baseline read\t%s\n", yesNo(result.Capabilities.ScheduleRead))
			fmt.Fprintf(writer, "run scan (action)\t%s\n", yesNo(result.Capabilities.RunScan))
			fmt.Fprintf(writer, "schedule/baseline write\t%s\n", yesNo(result.Capabilities.ScheduleWrite))
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

func newSecurityAdvisorStatusCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "status",
		Short: "Show the last-scan status and per-severity finding totals",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetSecurityAdvisorStatus(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			status := result.Status
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Overall severity:\t%s\n", status.OverallSeverity)
			fmt.Fprintf(writer, "Scan running:\t%s\n", yesNo(status.Running))
			fmt.Fprintf(writer, "Progress:\t%d%%\n", status.Progress)
			fmt.Fprintf(writer, "Last scan:\t%s\n", scanTime(status.LastScanTime))
			fmt.Fprintf(writer, "Checks:\t%d total, %d findings, %d passed\n",
				status.TotalChecks, status.TotalFindings, status.TotalChecks-status.TotalFindings)
			fmt.Fprintf(writer, "By severity:\t%s\n", severityCountLine(status.Counts))
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newSecurityAdvisorFindingsCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "findings",
		Short: "List the per-category scan findings with their severity breakdown",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetSecurityAdvisorStatus(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			status := result.Status
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Overall severity:\t%s\n", status.OverallSeverity)
			if len(status.Categories) == 0 {
				fmt.Fprintln(writer, "No categories reported (no scan has run).")
				return writer.Flush()
			}
			fmt.Fprintln(writer, "\nCATEGORY\tSEVERITY\tTOTAL\tFINDINGS\tPASSED\tDANGER\tRISK\tWARNING\tOUTOFDATE\tINFO")
			for _, category := range status.Categories {
				fmt.Fprintf(writer, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n",
					category.Category, category.FailSeverity, category.Total, category.Findings, category.Passed,
					category.Counts.Danger, category.Counts.Risk, category.Counts.Warning, category.Counts.OutOfDate, category.Counts.Info)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newSecurityAdvisorScheduleCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "schedule",
		Short: "Show the scan schedule and security baseline",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetSecurityAdvisorSchedule(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			configuration := result.Configuration
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Baseline:\t%s\n", configuration.Baseline)
			fmt.Fprintf(writer, "Scheduled scan:\t%s\n", yesNo(configuration.Schedule.Enabled))
			fmt.Fprintf(writer, "Scheduled time:\t%02d:%02d\n", configuration.Schedule.Hour, configuration.Schedule.Minute)
			fmt.Fprintf(writer, "Weekday:\t%s\n", valueOrDash(configuration.Schedule.Weekday))
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func severityCountLine(counts securityadvisor.SeverityCounts) string {
	return fmt.Sprintf("danger %d, risk %d, warning %d, out-of-date %d, info %d",
		counts.Danger, counts.Risk, counts.Warning, counts.OutOfDate, counts.Info)
}

func scanTime(unix int64) string {
	if unix <= 0 {
		return "-"
	}
	return time.Unix(unix, 0).Local().Format("2006-01-02 15:04:05")
}
