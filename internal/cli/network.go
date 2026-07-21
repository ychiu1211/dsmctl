package cli

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/network"
)

func newNetworkCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "network",
		Aliases: []string{"net"},
		Short:   "Inspect and manage the DSM network configuration (Control Panel > Network)",
		Long: "Read the Control Panel > Network surface: the general settings (hostname, default gateway, DNS, outbound " +
			"proxy), the per-interface configuration and link status, link-aggregation bonds, and the static-route table. " +
			"Reads are safe. The general settings can be changed through the guarded plan/apply contract; a mandatory " +
			"never-sever-the-management-NIC guard refuses any change to the interface carrying dsmctl's connection or to " +
			"the default gateway unless allow_connectivity_break is set. Interface reconfiguration is plan-only in this " +
			"build (the DSM interface-set wire is unverified). The proxy password is never surfaced.",
	}
	command.AddCommand(
		newNetworkCapabilitiesCommand(opts),
		newNetworkGeneralCommand(opts),
		newNetworkInterfacesCommand(opts),
		newNetworkBondsCommand(opts),
		newNetworkRoutesCommand(opts),
		newNetworkTrafficControlCommand(opts),
		newNetworkGeneralPlanCommand(opts),
		newNetworkGeneralApplyCommand(opts),
		newNetworkInterfacePlanCommand(opts),
		newNetworkInterfaceApplyCommand(opts),
	)
	return command
}

func newNetworkGeneralPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "general-plan",
		Short: "Validate a general network change (hostname, DNS, default gateway) and emit an approval plan as JSON",
		Long: "Validate a patch-only change to the Control Panel > Network > General settings and return an approval plan " +
			"bound to the complete observed general block and the resolved management path. A default-gateway change is run " +
			"through the never-sever guard and refused without allow_connectivity_break; hostname and DNS changes are " +
			"medium risk. This command never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request network.GeneralChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read network general change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanNetworkGeneralChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "network general change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newNetworkGeneralApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "general-apply",
		Short: "Apply a general network plan after hash, stale-state, and never-sever validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.NetworkGeneralPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read network general plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyNetworkGeneralPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "network general plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by network general-plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newNetworkInterfacePlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "interface-plan",
		Short: "Validate a per-interface change and emit an approval plan as JSON (plan-only; the apply is wire-unverified)",
		Long: "Validate a patch-only change to one network interface (IP/netmask/gateway/DHCP/MTU) and return an approval " +
			"plan. The never-sever guard refuses any change to the management interface (the NIC carrying dsmctl's " +
			"connection), or ANY interface change when the connection is ambiguous (hostname/relay/NAT), unless " +
			"allow_connectivity_break is set; a non-management NIC change is permitted (medium risk). NOTE: the DSM " +
			"interface-set request shape is wire-unverified (DSM returns code 4302), so the apply is refused; this command " +
			"and the guard still work. This command never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request network.InterfaceChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read network interface change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanNetworkInterfaceChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "network interface change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newNetworkInterfaceApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "interface-apply",
		Short: "Apply an interface plan (currently refused: the DSM interface-set wire is unverified)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.NetworkInterfacePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read network interface plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyNetworkInterfacePlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "network interface plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by network interface-plan")
	_ = command.MarkFlagRequired("approve")
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
			fmt.Fprintf(writer, "general write\t%s\n", yesNo(result.Capabilities.GeneralWrite))
			fmt.Fprintf(writer, "interface write wire-unverified\t%s\n", yesNo(result.Capabilities.InterfaceWriteWireUnverified))
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
