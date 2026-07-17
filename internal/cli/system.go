package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
)

func newSystemCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "system", Short: "Inspect DSM system information"}
	command.AddCommand(newSystemInfoCommand(opts))
	return command
}

func newSystemInfoCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "info",
		Short: "Show basic system information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer func() {
				closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = service.Close(closeCtx)
			}()

			result, err := service.GetSystemInfo(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return writeSystemInfo(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func writeSystemInfo(cmd *cobra.Command, result application.SystemInfoResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	row := func(label string, value any) {
		empty := value == nil
		switch typed := value.(type) {
		case string:
			empty = typed == ""
		case int:
			empty = typed == 0
		case int64:
			empty = typed == 0
		}
		if empty {
			return
		}
		fmt.Fprintf(writer, "%s:\t%v\n", label, value)
	}
	row("NAS", result.NAS)
	row("Hostname", result.System.Hostname)
	row("Model", result.System.Model)
	row("Serial", result.System.Serial)
	row("DSM", result.System.DSMVersion)
	row("CPU", result.System.CPU)
	row("CPU cores", result.System.CPUCores)
	row("Memory (MiB)", result.System.MemoryMiB)
	row("Uptime", result.System.Uptime)
	row("Time zone", result.System.TimeZone)
	if result.System.TemperatureC != nil {
		fmt.Fprintf(writer, "Temperature:\t%.1f °C\n", *result.System.TemperatureC)
	}
	if result.System.TemperatureWarn {
		row("Temperature warning", "yes")
	}
	return writer.Flush()
}
