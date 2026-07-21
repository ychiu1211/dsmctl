package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newTaskSchedulerCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "task-scheduler",
		Aliases: []string{"taskscheduler"},
		Short:   "Inspect DSM Task Scheduler (scheduled and triggered tasks)",
	}
	command.AddCommand(
		newTaskSchedulerCapabilitiesCommand(opts),
		newTaskSchedulerListCommand(opts),
		newTaskSchedulerTriggeredCommand(opts),
	)
	return command
}

func newTaskSchedulerCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show which Task Scheduler areas can be read and the selected backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetTaskSchedulerCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Scheduled tasks read:\t%s\n", yesNo(result.Capabilities.ScheduledRead))
			fmt.Fprintf(writer, "Triggered tasks read:\t%s\n", yesNo(result.Capabilities.TriggeredRead))
			fmt.Fprintf(writer, "Task detail backend present:\t%s\n", yesNo(result.Capabilities.DetailAvailable))
			fmt.Fprintf(writer, "Task fields wire-unverified:\t%s\n", yesNo(result.Capabilities.TaskFieldsWireUnverified))
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

func newTaskSchedulerListCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List scheduled tasks (metadata, schedule, owner, and last-run status)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetTaskSchedulerScheduled(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Scheduled tasks:\t%d\n", result.Tasks.Total)
			if len(result.Tasks.Tasks) > 0 {
				fmt.Fprintln(writer, "\nID\tNAME\tTYPE\tENABLED\tRUN-AS\tSCHEDULE\tNEXT RUN\tLAST RESULT")
				for _, task := range result.Tasks.Tasks {
					fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
						task.ID, valueOrDash(task.Name), valueOrDash(task.TypeGroup), yesNo(task.Enabled),
						valueOrDash(formatRunAs(task.RunAsOwner, task.RunAsPrivileged)),
						valueOrDash(task.Schedule.Summary), valueOrDash(task.NextRunTime), valueOrDash(task.LastRunStatus))
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newTaskSchedulerTriggeredCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "triggered",
		Short: "List triggered tasks (boot-up, shutdown, and event-triggered)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetTaskSchedulerTriggered(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Triggered tasks:\t%d\n", len(result.Tasks.Tasks))
			if len(result.Tasks.Tasks) > 0 {
				fmt.Fprintln(writer, "\nNAME\tEVENT\tENABLED\tRUN-AS\tACTION")
				for _, task := range result.Tasks.Tasks {
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
						valueOrDash(task.Name), valueOrDash(task.Event), yesNo(task.Enabled),
						valueOrDash(formatRunAs(task.RunAsOwner, task.RunAsPrivileged)), valueOrDash(task.Action))
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

// formatRunAs renders the run-as identity, marking a privileged (root/admin, or
// unset which defaults to root) run-as so the operator sees that the task's
// command runs with elevated privilege.
func formatRunAs(runAs string, privileged bool) string {
	label := strings.TrimSpace(runAs)
	if label == "" {
		label = "root (default)"
	}
	if privileged {
		return label + " (privileged)"
	}
	return label
}
