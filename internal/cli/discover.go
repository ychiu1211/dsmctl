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
	command := &cobra.Command{
		Use:   "discover",
		Short: "Discover Synology devices on the local network",
		Long: "Broadcast a Synology findhost query on the local network and list the\n" +
			"Synology devices that answer. This needs no configured NAS profile,\n" +
			"credential, or DSM session, and changes nothing on any device; it only\n" +
			"sends discovery query packets and reads the replies. Only devices in the\n" +
			"same broadcast domain as this host can answer.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath, terminalOTPProvider(cmd))
			if err != nil {
				return err
			}
			defer func() {
				closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = service.Close(closeCtx)
			}()

			result, err := service.DiscoverDevices(cmd.Context(), discovery.Query{Timeout: timeout})
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return writeDiscoveredDevices(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().DurationVar(&timeout, "timeout", discovery.DefaultTimeout, "how long to listen for device responses")
	return command
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
