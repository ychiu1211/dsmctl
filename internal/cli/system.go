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
	command.AddCommand(newSystemInfoCommand(opts), newSystemSetNameCommand(opts))
	return command
}

func newSystemSetNameCommand(opts *options) *cobra.Command {
	var assumeYes bool
	command := &cobra.Command{
		Use:   "set-name <server-name>",
		Short: "Set the DSM server name (hostname)",
		Long: "Change the DSM server name (the hostname shown in Control Panel and on the\n" +
			"network). It reads the current name, applies the change, and verifies it by\n" +
			"re-reading; it fails closed if DSM does not report the requested name. Requires\n" +
			"confirmation unless --yes is given.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			out := cmd.OutOrStdout()
			if !assumeYes {
				fmt.Fprintf(cmd.ErrOrStderr(), "Change the DSM server name to %q? [y/N]: ", name)
				answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if answer = strings.ToLower(strings.TrimSpace(answer)); answer != "y" && answer != "yes" {
					return errors.New("server name was not changed")
				}
			}
			result, err := service.SetServerName(cmd.Context(), opts.nas, name)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Server name changed from %q to %q on NAS %q.\n", result.Previous, result.ServerName, result.NAS)
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
