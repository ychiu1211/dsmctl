package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
)

func newDiscoverCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	var timeout time.Duration
	var showCached bool
	var clearCached bool
	command := &cobra.Command{
		Use:   "discover",
		Short: "Discover Synology devices on the local network",
		Long: "Broadcast a Synology findhost query on the local network and list the\n" +
			"Synology devices that answer. This needs no configured NAS profile,\n" +
			"credential, or DSM session, and changes nothing on any device; it only\n" +
			"sends discovery query packets and reads the replies. Only devices in the\n" +
			"same broadcast domain as this host can answer.\n\n" +
			"The scan re-broadcasts throughout the listen window and accumulates the\n" +
			"answers, so it keeps filling in devices for the full --timeout. Press\n" +
			"Ctrl-C to stop early and keep whatever has been found so far. Each run's\n" +
			"results are saved; --cached prints the last saved set without scanning\n" +
			"and --clear discards it.",
		Args: cobra.NoArgs,
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

			if clearCached {
				if err := service.ClearDiscoveries(cmd.Context()); err != nil {
					return err
				}
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "Cleared saved discovery results.")
				return err
			}
			if showCached {
				saved, err := service.CachedDiscoveries(cmd.Context())
				if err != nil {
					return err
				}
				return writeCachedDevices(cmd, saved, jsonOutput)
			}

			result, err := service.DiscoverDevicesStream(cmd.Context(), discovery.Query{Timeout: timeout}, discoverProgress(cmd))
			if err != nil {
				return err
			}

			if cmd.Context().Err() != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Interrupted — keeping the %d device(s) found so far.\n", len(result.Devices))
			}
			if !jsonOutput {
				warnIfUndercounted(cmd, len(result.Devices), result.SavedTotal)
			}

			if jsonOutput {
				return writeDiscoveredJSON(cmd, result)
			}
			return writeDiscoveredDevices(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().DurationVarP(&timeout, "timeout", "t", discovery.DefaultTimeout, "how long to scan for devices; press Ctrl-C to stop early")
	command.Flags().BoolVar(&showCached, "cached", false, "print the last saved discovery results without scanning")
	command.Flags().BoolVar(&clearCached, "clear", false, "discard the saved discovery results and exit")
	return command
}

// discoverProgress returns a callback that streams each newly found device to
// stderr as the sweep runs, so the user watches the list fill up. It writes to
// stderr so structured stdout (a table or --json) stays clean and pipeable.
func discoverProgress(cmd *cobra.Command) func(discovery.Device) {
	out := cmd.ErrOrStderr()
	return func(device discovery.Device) {
		label := device.Hostname
		if label == "" {
			label = device.Serial
		}
		fmt.Fprintf(out, "  found %-16s %-11s %s\n",
			dashIfEmpty(device.IPAddress), dashIfEmpty(device.Model), dashIfEmpty(label))
	}
}

// warnIfUndercounted flags a sweep that answered with noticeably fewer devices
// than the saved set — the signature of another findhost listener (typically
// Synology Assistant) winning the shared UDP 9999 port for this run. It points
// the user at the durable saved set and the simple workaround.
func warnIfUndercounted(cmd *cobra.Command, found, savedTotal int) {
	if savedTotal <= found {
		return
	}
	// Only speak up when more than a quarter of the known devices are missing;
	// a couple of genuinely-offline units should stay quiet.
	if found*4 >= savedTotal*3 {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(),
		"Note: only %d of %d recently-seen devices answered this scan. Another findhost app "+
			"(e.g. Synology Assistant) may be holding UDP 9999; wait a few seconds and rerun, "+
			"or run 'dsmctl discover --cached' for the full saved set.\n",
		found, savedTotal)
}

func writeDiscoveredJSON(cmd *cobra.Command, result application.DiscoverResult) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func writeDiscoveredDevices(cmd *cobra.Command, result application.DiscoverResult) error {
	out := cmd.OutOrStdout()
	if len(result.Devices) == 0 {
		_, err := fmt.Fprintln(out, "No Synology devices answered on the local network.")
		return err
	}
	writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "HOSTNAME\tMODEL\tOS VERSION\tIP ADDRESS\tMAC\tSERIAL\tSTATE")
	for _, device := range result.Devices {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			dashIfEmpty(device.Hostname),
			dashIfEmpty(device.Model),
			dashIfEmpty(device.OSVersion),
			dashIfEmpty(discoverAddressColumn(device)),
			dashIfEmpty(device.MACAddress),
			dashIfEmpty(device.Serial),
			dashIfEmpty(device.State),
		)
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "\n%d device(s) found.\n", len(result.Devices))
	return err
}

// writeCachedDevices renders the saved cross-run set, adding a LAST SEEN column
// the live table omits.
func writeCachedDevices(cmd *cobra.Command, saved application.SavedDiscoveries, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(saved)
	}
	out := cmd.OutOrStdout()
	if len(saved.Devices) == 0 {
		_, err := fmt.Fprintln(out, "No saved discovery results yet. Run 'dsmctl discover' first.")
		return err
	}
	writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "HOSTNAME\tMODEL\tOS VERSION\tIP ADDRESS\tMAC\tSERIAL\tSTATE\tLAST SEEN")
	for _, record := range saved.Devices {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			dashIfEmpty(record.Hostname),
			dashIfEmpty(record.Model),
			dashIfEmpty(record.OSVersion),
			dashIfEmpty(discoverAddressColumn(record.Device)),
			dashIfEmpty(record.MACAddress),
			dashIfEmpty(record.Serial),
			dashIfEmpty(record.State),
			record.LastSeen.Local().Format("2006-01-02 15:04"),
		)
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "\n%d device(s) saved (last updated %s).\n",
		len(saved.Devices), saved.UpdatedAt.Local().Format("2006-01-02 15:04"))
	return err
}

// discoverAddressColumn shows every address a multi-homed device answered from,
// falling back to the representative address.
func discoverAddressColumn(device discovery.Device) string {
	if len(device.IPv4Addresses) > 1 {
		return strings.Join(device.IPv4Addresses, ", ")
	}
	return device.IPAddress
}

func dashIfEmpty(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
