package cli

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newNetworkCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "network",
		Aliases: []string{"net"},
		Short:   "Inspect the DSM network configuration (Control Panel > Network)",
		Long: "Read the Control Panel > Network surface: the general settings (hostname, default gateway, DNS, outbound " +
			"proxy), the per-interface configuration and link status, link-aggregation bonds, and the static-route table. " +
			"All commands are read-only; the connectivity-affecting writes are a deferred, guarded follow-on. The proxy " +
			"password is never surfaced.",
	}
	command.AddCommand(
		newNetworkCapabilitiesCommand(opts),
		newNetworkGeneralCommand(opts),
		newNetworkInterfacesCommand(opts),
		newNetworkBondsCommand(opts),
		newNetworkRoutesCommand(opts),
		newNetworkTrafficControlCommand(opts),
	)
	return command
}

func newNetworkCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show network read support and the selected backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNetworkCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "general read\t%s\n", yesNo(result.Capabilities.GeneralRead))
			fmt.Fprintf(writer, "interfaces read\t%s\n", yesNo(result.Capabilities.InterfacesRead))
			fmt.Fprintf(writer, "bonds read\t%s\n", yesNo(result.Capabilities.BondsRead))
			fmt.Fprintf(writer, "proxy read\t%s\n", yesNo(result.Capabilities.ProxyRead))
			fmt.Fprintf(writer, "routes read\t%s\n", yesNo(result.Capabilities.RoutesRead))
			fmt.Fprintf(writer, "traffic-control read (detect only)\t%s\n", yesNo(result.Capabilities.TrafficControlRead))
			fmt.Fprintf(writer, "bond fields wire-unverified\t%s\n", yesNo(result.Capabilities.BondFieldsWireUnverified))
			fmt.Fprintf(writer, "route fields wire-unverified\t%s\n", yesNo(result.Capabilities.RouteFieldsWireUnverified))
			fmt.Fprintf(writer, "ipv6 fields wire-unverified\t%s\n", yesNo(result.Capabilities.IPv6FieldsWireUnverified))
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

func newNetworkGeneralCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "general",
		Short: "Show hostname, default gateway, DNS, and proxy settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNetworkGeneral(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			g := result.General
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Hostname:\t%s\n", valueOrDash(g.Hostname))
			fmt.Fprintf(writer, "Default gateway (IPv4):\t%s\n", valueOrDash(g.DefaultGatewayV4))
			fmt.Fprintf(writer, "Default gateway (IPv6):\t%s\n", valueOrDash(g.DefaultGatewayV6))
			fmt.Fprintf(writer, "Gateway interface:\t%s\n", valueOrDash(g.DefaultGateway.Interface))
			fmt.Fprintf(writer, "DNS primary:\t%s\n", valueOrDash(g.DNSPrimary))
			fmt.Fprintf(writer, "DNS secondary:\t%s\n", valueOrDash(g.DNSSecondary))
			fmt.Fprintf(writer, "DNS manual:\t%s\n", yesNo(g.DNSManual))
			fmt.Fprintf(writer, "Prefer IPv4:\t%s\n", yesNo(g.IPv4First))
			fmt.Fprintf(writer, "Multiple gateways:\t%s\n", yesNo(g.MultiGateway))
			fmt.Fprintf(writer, "IP conflict detect:\t%s\n", yesNo(g.IPConflictDetect))
			if g.Proxy.Supported {
				fmt.Fprintf(writer, "Proxy enabled:\t%s\n", yesNo(g.Proxy.Enabled))
				if g.Proxy.Enabled {
					fmt.Fprintf(writer, "Proxy HTTP:\t%s\n", valueOrDash(hostPort(g.Proxy.HTTPHost, g.Proxy.HTTPPort)))
					if g.Proxy.DifferentHTTPS {
						fmt.Fprintf(writer, "Proxy HTTPS:\t%s\n", valueOrDash(hostPort(g.Proxy.HTTPSHost, g.Proxy.HTTPSPort)))
					}
					fmt.Fprintf(writer, "Proxy auth:\t%s\n", yesNo(g.Proxy.AuthEnabled))
				}
			} else {
				fmt.Fprintf(writer, "Proxy:\t%s\n", "(not supported)")
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func hostPort(host, port string) string {
	if host == "" {
		return ""
	}
	if port == "" {
		return host
	}
	return host + ":" + port
}

func newNetworkInterfacesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "interfaces",
		Short: "Show each interface's IP, DHCP, MTU, and link status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNetworkInterfaces(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			if len(result.Interfaces) == 0 {
				fmt.Fprintln(writer, "(no interfaces)")
				return writer.Flush()
			}
			fmt.Fprintln(writer, "\nNAME\tTYPE\tIPV4\tNETMASK\tDHCP\tMTU\tLINK\tSPEED\tDGW")
			for _, iface := range result.Interfaces {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					iface.Name, valueOrDash(iface.Type), valueOrDash(iface.IPv4), valueOrDash(iface.Netmask),
					yesNo(iface.UseDHCP), mtuText(iface.MTU), valueOrDash(iface.LinkStatus),
					speedText(iface.SpeedMbps), yesNo(iface.IsDefaultGateway))
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func mtuText(mtu int) string {
	if mtu == 0 {
		return "-"
	}
	if mtu >= 9000 {
		return strconv.Itoa(mtu) + " (jumbo)"
	}
	return strconv.Itoa(mtu)
}

func speedText(speed int) string {
	if speed < 0 {
		return "down"
	}
	if speed == 0 {
		return "-"
	}
	return strconv.Itoa(speed) + " Mbps"
}

func newNetworkBondsCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "bonds",
		Short: "Show link-aggregation bonds, their mode, and member NICs",
		Long: "Show the link-aggregation (bonding) interfaces with their bonding mode and member NICs. Note: the per-bond " +
			"mode and member field names are wire-unverified because the lab had no bond to confirm them against.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNetworkBonds(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			if len(result.Bonds) == 0 {
				fmt.Fprintln(writer, "(no bonds configured)")
				return writer.Flush()
			}
			fmt.Fprintln(writer, "\nNAME\tIPV4\tSTATUS\tMODE\tMEMBERS")
			for _, bond := range result.Bonds {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
					bond.Name, valueOrDash(bond.IPv4), valueOrDash(bond.Status),
					valueOrDash(bond.Mode), valueOrDash(strings.Join(bond.Members, ", ")))
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newNetworkRoutesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "routes",
		Short: "Show the static-route table",
		Long: "Show the static-route table (destination, netmask, gateway, egress interface, address family). Note: on a " +
			"NAS without advanced routing configured DSM reports no route table (shown as not configured); the per-route " +
			"field names are wire-unverified until confirmed against a NAS with static routes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNetworkRoutes(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			if !result.Routes.Configured {
				fmt.Fprintln(writer, "Static routes:\t(not configured on this NAS)")
				return writer.Flush()
			}
			if len(result.Routes.Routes) == 0 {
				fmt.Fprintln(writer, "Static routes:\t(none)")
				return writer.Flush()
			}
			fmt.Fprintln(writer, "\nFAMILY\tDESTINATION\tNETMASK\tGATEWAY\tINTERFACE")
			for _, route := range result.Routes.Routes {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
					valueOrDash(route.Family), valueOrDash(route.Destination), valueOrDash(route.Netmask),
					valueOrDash(route.Gateway), valueOrDash(route.Interface))
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newNetworkTrafficControlCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "traffic-control",
		Short: "Show whether traffic-control (bandwidth) rules are available",
		Long: "Report whether the DSM traffic-control (bandwidth) rules API is present on the NAS. This area is " +
			"capability-detected only: the required read parameter for SYNO.Core.Network.TrafficControl.Rules could not be " +
			"live-verified, so the rule contents are not decoded in this read slice.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNetworkCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			if result.Capabilities.TrafficControlRead {
				fmt.Fprintf(writer, "Traffic-control API:\t%s\n", "present (capability-detected only; rule contents not decoded this pass)")
			} else {
				fmt.Fprintf(writer, "Traffic-control API:\t%s\n", "(not supported)")
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}
