package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
)

func newSystemCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "system", Short: "Inspect and manage DSM system settings"}
	command.AddCommand(newSystemInfoCommand(opts), newSystemSetHostnameCommand(opts))
	return command
}

func newSystemSetHostnameCommand(opts *options) *cobra.Command {
	var assumeYes bool
	command := &cobra.Command{
		Use:   "set-hostname <hostname>",
		Short: "Set the DSM server name (hostname)",
		Long: "Change the DSM server name — the hostname shown in Control Panel and on the\n" +
			"network. It plans the change against the current name (hash-bound), shows the\n" +
			"summary, applies it after confirmation (--yes to skip), and verifies it by\n" +
			"re-reading; it fails closed if DSM does not report the requested name.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			out := cmd.OutOrStdout()
			plan, err := service.PlanSystemHostname(cmd.Context(), opts.nas, application.SystemHostnameChange{Hostname: name})
			if err != nil {
				return err
			}
			for _, line := range plan.Summary {
				fmt.Fprintf(out, "Plan: %s\n", line)
			}
			for _, warning := range plan.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", warning)
			}
			if !assumeYes {
				fmt.Fprint(cmd.ErrOrStderr(), "Apply? [y/N]: ")
				answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if answer = strings.ToLower(strings.TrimSpace(answer)); answer != "y" && answer != "yes" {
					return errors.New("server name was not changed")
				}
			}
			result, err := service.ApplySystemHostnamePlan(cmd.Context(), plan, plan.Hash)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Server name changed from %q to %q on NAS %q.\n", result.Previous, result.Hostname, result.NAS)
			return nil
		},
	}
	command.Flags().BoolVar(&assumeYes, "yes", false, "skip the confirmation prompt (for automation)")
	return command
}

func newSystemInfoCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "info",
		Short: "Show basic system information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
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
