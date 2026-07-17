package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/syslog"
)

func newLogCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "log", Short: "Inspect DSM system logs"}
	command.AddCommand(
		newLogListCommand(opts),
		newLogCapabilitiesCommand(opts),
	)
	return command
}

func newLogListCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	var limit, offset int
	var level, keyword, logType, from, to string
	command := &cobra.Command{
		Use:   "list",
		Short: "List DSM system log entries with optional filters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fromTime, err := syslog.ParseTime(from)
			if err != nil {
				return err
			}
			toTime, err := syslog.ParseTime(to)
			if err != nil {
				return err
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetLogState(cmd.Context(), opts.nas, syslog.StateQuery{
				Limit: limit, Offset: offset, Keyword: keyword, LogType: logType, Level: level,
				From: fromTime, To: toTime,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeLogList(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().IntVar(&limit, "limit", 100, "maximum number of entries to return")
	command.Flags().IntVar(&offset, "offset", 0, "number of newest entries to skip for pagination")
	command.Flags().StringVar(&level, "level", "", "filter severity within the retrieved page: info, warn, or error")
	command.Flags().StringVar(&keyword, "keyword", "", "case-insensitive substring filter applied by DSM")
	command.Flags().StringVar(&logType, "type", "", "DSM log category (default system): system, connection, package, or fileTransfer")
	command.Flags().StringVar(&from, "from", "", "only entries at or after this local time (2006-01-02[ 15:04:05]) or Unix seconds")
	command.Flags().StringVar(&to, "to", "", "only entries at or before this local time (2006-01-02[ 15:04:05]) or Unix seconds; requires --from")
	return command
}

func newLogCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show DSM log read support and the selected backend",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetLogCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Log read:\t%s\n", yesNo(result.Capabilities.Read))
			for _, operation := range result.Report.Operations {
				fmt.Fprintf(writer, "Backend:\t%s\n", valueOrDash(operation.Backend))
				fmt.Fprintf(writer, "DSM API:\t%s v%d\n", valueOrDash(operation.API), operation.Version)
				fmt.Fprintf(writer, "Selection:\t%s\n", operation.Reason)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func writeLogList(cmd *cobra.Command, result application.LogStateResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	logs := result.Logs
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Total matching:\t%d (info %d, warn %d, error %d)\n", logs.Total, logs.InfoCount, logs.WarnCount, logs.ErrorCount)
	fmt.Fprintf(writer, "Showing:\t%d\n", len(logs.Entries))
	fmt.Fprintln(writer, "\nTIME\tLEVEL\tTYPE\tWHO\tMESSAGE")
	for _, entry := range logs.Entries {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
			valueOrDash(entry.Time), valueOrDash(entry.Level), valueOrDash(entry.Type), valueOrDash(entry.Who), entry.Message)
	}
	return writer.Flush()
}
