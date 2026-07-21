package cli

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/hyperbackup"
)

// newBackupCommand exposes the Hyper Backup module: task, version, log, and
// vault reads plus the guarded run/cancel task actions.
func newBackupCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "backup",
		Aliases: []string{"hyperbackup", "hb"},
		Short:   "Inspect Synology Hyper Backup tasks, versions, logs, and the Vault view, and run or cancel backups",
	}
	command.AddCommand(
		newBackupCapabilitiesCommand(opts),
		newBackupTasksCommand(opts),
		newBackupTaskCommand(opts),
		newBackupVersionsCommand(opts),
		newBackupLogsCommand(opts),
		newBackupVaultCommand(opts),
	)
	return command
}

func newBackupCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show Hyper Backup read/action support, package evidence, and selected DSM backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetHyperBackupCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeBackupCapabilities(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newBackupTasksCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "tasks",
		Short: "List Hyper Backup tasks with state, last result, and next run",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetHyperBackupTasks(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeBackupTasks(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.AddCommand(
		newBackupTaskPlanCommand(opts),
		newBackupTaskApplyCommand(opts),
	)
	return command
}

func parseTaskIDArgument(argument string) (int, error) {
	taskID, err := strconv.Atoi(strings.TrimSpace(argument))
	if err != nil || taskID <= 0 {
		return 0, fmt.Errorf("task id must be a positive integer, got %q", argument)
	}
	return taskID, nil
}

func newBackupTaskCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "task <task-id>",
		Short: "Show one task's repository, transfer options, live status, and destination reachability",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := parseTaskIDArgument(args[0])
			if err != nil {
				return err
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetHyperBackupTaskDetail(cmd.Context(), opts.nas, taskID)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeBackupTaskDetail(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newBackupVersionsCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	var offset, limit int
	command := &cobra.Command{
		Use:   "versions <task-id>",
		Short: "List the backup versions a task has produced",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := parseTaskIDArgument(args[0])
			if err != nil {
				return err
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetHyperBackupVersions(cmd.Context(), opts.nas, taskID, offset, limit)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeBackupVersions(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().IntVar(&offset, "offset", 0, "number of versions to skip")
	command.Flags().IntVar(&limit, "limit", 50, "maximum versions to return")
	return command
}

func newBackupLogsCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	var offset, limit int
	command := &cobra.Command{
		Use:   "logs",
		Short: "Show the Hyper Backup log feed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetHyperBackupLogs(cmd.Context(), opts.nas, offset, limit)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeBackupLogs(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().IntVar(&offset, "offset", 0, "number of log entries to skip")
	command.Flags().IntVar(&limit, "limit", 50, "maximum log entries to return")
	return command
}

func newBackupVaultCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "vault",
		Short: "Show the Hyper Backup Vault view: inbound targets stored on this NAS",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetHyperBackupVault(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeBackupVault(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newBackupTaskPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a backup/cancel task action and emit an approval plan as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request hyperbackup.TaskChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read task change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanHyperBackupTaskChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "task change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newBackupTaskApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a task action plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.HyperBackupTaskPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read task plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyHyperBackupTaskPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "task plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by tasks plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func writeBackupCapabilities(cmd *cobra.Command, result application.HyperBackupCapabilitiesResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	c := result.Capabilities
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Package:\t%s\n", valueOrDash(c.Package.ID))
	fmt.Fprintf(writer, "Installed:\t%s\n", yesNo(c.Package.Installed))
	fmt.Fprintf(writer, "Version:\t%s\n", valueOrDash(c.Package.Version))
	fmt.Fprintf(writer, "Running:\t%s\n", yesNo(c.Package.Running))
	fmt.Fprintf(writer, "Vault package:\t%s\n", valueOrDash(c.VaultPackage.ID))
	fmt.Fprintf(writer, "Vault installed:\t%s\n", yesNo(c.VaultPackage.Installed))
	fmt.Fprintf(writer, "Vault version:\t%s\n", valueOrDash(c.VaultPackage.Version))
	fmt.Fprintf(writer, "Vault running:\t%s\n", yesNo(c.VaultPackage.Running))
	fmt.Fprintf(writer, "Task read:\t%s\n", yesNo(c.TaskRead))
	fmt.Fprintf(writer, "Detail read:\t%s\n", yesNo(c.DetailRead))
	fmt.Fprintf(writer, "Version read:\t%s\n", yesNo(c.VersionRead))
	fmt.Fprintf(writer, "Log read:\t%s\n", yesNo(c.LogRead))
	fmt.Fprintf(writer, "Vault read:\t%s\n", yesNo(c.VaultRead))
	fmt.Fprintf(writer, "Task run/cancel (guarded):\t%s\n", yesNo(c.TaskRun))
	fmt.Fprintln(writer, "\nOPERATIONS")
	fmt.Fprintln(writer, "OPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
	for _, operation := range result.Report.Operations {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), valueOrDash(operation.API), operation.Version)
	}
	return writer.Flush()
}

func writeBackupTasks(cmd *cobra.Command, result application.HyperBackupTasksResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Total:\t%d\n", result.Tasks.Total)
	fmt.Fprintln(writer, "\nTASKS")
	if len(result.Tasks.Tasks) == 0 {
		fmt.Fprintln(writer, "(no tasks)")
		return writer.Flush()
	}
	fmt.Fprintln(writer, "ID\tNAME\tTYPE\tSTATE\tACTIVITY\tLAST RESULT\tLAST BACKUP\tNEXT BACKUP")
	for _, task := range result.Tasks.Tasks {
		fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			task.TaskID, valueOrDash(task.Name), valueOrDash(task.Type), valueOrDash(task.State),
			valueOrDash(task.Status), valueOrDash(task.LastBackupResult),
			valueOrDash(task.LastBackupTime), valueOrDash(task.NextBackupTime))
	}
	return writer.Flush()
}

func writeBackupTaskDetail(cmd *cobra.Command, result application.HyperBackupTaskDetailResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	d := result.Task
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Task:\t%d (%s)\n", d.Task.TaskID, valueOrDash(d.Task.Name))
	fmt.Fprintf(writer, "Type:\t%s\n", valueOrDash(d.Task.Type))
	fmt.Fprintf(writer, "State:\t%s\n", valueOrDash(d.Task.State))
	fmt.Fprintf(writer, "Activity:\t%s\n", valueOrDash(d.Task.Status))
	fmt.Fprintf(writer, "Last backup:\t%s\n", valueOrDash(d.Status.LastBackupTime))
	fmt.Fprintf(writer, "Last result:\t%s\n", valueOrDash(d.Status.LastBackupResult))
	fmt.Fprintf(writer, "Last success:\t%s\n", valueOrDash(d.Status.LastSuccessTime))
	fmt.Fprintf(writer, "Next backup:\t%s\n", valueOrDash(d.Status.NextBackupTime))
	if d.Status.LastBackupError != "" {
		fmt.Fprintf(writer, "Last error:\t%s\n", d.Status.LastBackupError)
	}
	if d.Status.Progress != nil {
		p := d.Status.Progress
		fmt.Fprintf(writer, "Progress:\t%d%% (%s), %d/%d bytes, %d B/s\n", p.Percent, valueOrDash(p.Step), p.ProcessedBytes, p.TotalBytes, p.AverageSpeedBps)
	}
	fmt.Fprintf(writer, "Repository:\t%s (id %d)\n", valueOrDash(d.Repository.Name), d.Repository.RepositoryID)
	fmt.Fprintf(writer, "Destination:\t%s\n", valueOrDash(strings.TrimSpace(strings.Join([]string{d.Repository.Share, d.Task.TargetID}, "/"))))
	fmt.Fprintf(writer, "Transport:\t%s\n", valueOrDash(d.Task.TransferType))
	fmt.Fprintf(writer, "Target online:\t%s\n", yesNo(d.Target.Online))
	fmt.Fprintf(writer, "Target host:\t%s\n", valueOrDash(d.Target.HostName))
	fmt.Fprintf(writer, "Owner:\t%s\n", valueOrDash(d.Target.OwnerName))
	fmt.Fprintf(writer, "Compression:\t%s\n", yesNo(d.BackupParams.CompressionEnabled))
	fmt.Fprintf(writer, "Client encryption:\t%s\n", yesNo(d.BackupParams.EncryptionEnabled))
	fmt.Fprintf(writer, "Notifications:\t%s\n", yesNo(d.BackupParams.NotifyEnabled))
	if len(d.Task.SourceFolders) > 0 {
		fmt.Fprintf(writer, "Source folders:\t%s\n", strings.Join(d.Task.SourceFolders, ", "))
	}
	if len(d.Task.SourceApps) > 0 {
		fmt.Fprintf(writer, "Source applications:\t%s\n", strings.Join(d.Task.SourceApps, ", "))
	}
	return writer.Flush()
}

func writeBackupVersions(cmd *cobra.Command, result application.HyperBackupVersionsResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Task:\t%d\n", result.Versions.TaskID)
	fmt.Fprintf(writer, "Total:\t%d\n", result.Versions.Total)
	fmt.Fprintln(writer, "\nVERSIONS")
	if len(result.Versions.Entries) == 0 {
		fmt.Fprintln(writer, "(no versions)")
		return writer.Flush()
	}
	fmt.Fprintln(writer, "ID\tSTART\tCOMPLETED\tSTATUS\tLOCKED")
	for _, version := range result.Versions.Entries {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
			valueOrDash(version.VersionID), valueOrDash(version.StartTime), valueOrDash(version.CompleteTime),
			valueOrDash(version.Status), yesNo(version.Locked))
	}
	return writer.Flush()
}

func writeBackupLogs(cmd *cobra.Command, result application.HyperBackupLogsResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Total:\t%d (errors %d, warnings %d, info %d)\n",
		result.Logs.Total, result.Logs.ErrorCount, result.Logs.WarnCount, result.Logs.InfoCount)
	fmt.Fprintln(writer, "\nLOGS")
	if len(result.Logs.Entries) == 0 {
		fmt.Fprintln(writer, "(no log entries)")
		return writer.Flush()
	}
	fmt.Fprintln(writer, "TIME\tLEVEL\tUSER\tEVENT")
	for _, entry := range result.Logs.Entries {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
			valueOrDash(entry.Time), valueOrDash(entry.Level), valueOrDash(entry.User), valueOrDash(entry.Event))
	}
	return writer.Flush()
}

func writeBackupVault(cmd *cobra.Command, result application.HyperBackupVaultResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Parallel backup limit:\t%d\n", result.Vault.ParallelBackupLimit)
	fmt.Fprintln(writer, "\nINBOUND TARGETS")
	if len(result.Vault.Targets) == 0 {
		fmt.Fprintln(writer, "(no inbound targets)")
		return writer.Flush()
	}
	fmt.Fprintln(writer, "ID\tSHARE\tTARGET\tSTATUS\tUSED BYTES\tENCRYPTED\tLAST BACKUP (UNIX)\tDURATION (s)")
	for _, target := range result.Vault.Targets {
		fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%d\t%s\t%d\t%d\n",
			target.TargetID, valueOrDash(target.Share), valueOrDash(target.TargetName), valueOrDash(target.Status),
			target.UsedSizeBytes, yesNo(target.Encrypted), target.LastBackupStart, target.LastBackupDurationSec)
	}
	return writer.Flush()
}
