// Package network implements the read-only DSM operations for the Control Panel
// > Network surface. Each area is a separate DSM API (a separate compatibility
// boundary) and selects its own backend per operation, so a NAS missing one
// area leaves the others usable and reports it unsupported rather than erroring
// the whole module.
//
// Live-verified on DSM 7.3 (lab, build 81168) via a throwaway raw probe:
//   - General: SYNO.Core.Network get (v1 and v2; v2 adds enable_ip_conflict_detect)
//     → {server_name, gateway, v6gateway, dns_primary, dns_secondary, dns_manual,
//     use_dhcp_domain, ipv4_first, multi_gateway, arp_ignore, gateway_info{...}}.
//   - Interfaces: SYNO.Core.Network.Ethernet list (v1 and v2) → a JSON ARRAY of
//     rich per-NIC records {ifname, ip, mask, gateway, dns, use_dhcp, mtu,
//     mtu_config, speed, max_supported_speed, duplex, status, type,
//     is_default_gateway, enable_vlan, vlan_id, ipv6[]}. This is preferred over
//     SYNO.Core.Network.Interface list, which returns a sparse subset.
//   - Bonds: SYNO.Core.Network.Bond list (v1 and v2) → a JSON ARRAY (empty on
//     the lab, which has no bond).
//   - Proxy: SYNO.Core.Network.Proxy get (v1) → {enable, enable_auth, username,
//     http_host, http_port, https_host, https_port, enable_bypass,
//     enable_different_host, password}. The password is DROPPED by the decoder.
//   - Routes: SYNO.Core.Network.Router.Static.Route get (v1) — method EXISTS
//     (returned code 4302 "not configured" on the lab, which has no advanced
//     routing). Read attempted; the facade treats code 4302 as an empty,
//     not-configured table. The success field shape is wire-unverified.
//   - Traffic control: SYNO.Core.Network.TrafficControl.Rules load (v1) — method
//     exists but needs a parameter that could not be discovered (code 114).
//     Capability-detected only; no decoder is shipped (WI rule: never ship a
//     guessed decoder for an undiscoverable read).
package network

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/network"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	NetworkAPIName        = "SYNO.Core.Network"
	EthernetAPIName       = "SYNO.Core.Network.Ethernet"
	InterfaceAPIName      = "SYNO.Core.Network.Interface"
	BondAPIName           = "SYNO.Core.Network.Bond"
	ProxyAPIName          = "SYNO.Core.Network.Proxy"
	StaticRouteAPIName    = "SYNO.Core.Network.Router.Static.Route"
	TrafficControlAPIName = "SYNO.Core.Network.TrafficControl.Rules"

	GeneralReadCapabilityName        = "network.general.read"
	InterfacesReadCapabilityName     = "network.interfaces.read"
	BondsReadCapabilityName          = "network.bonds.read"
	ProxyReadCapabilityName          = "network.proxy.read"
	RoutesReadCapabilityName         = "network.routes.read"
	TrafficControlReadCapabilityName = "network.traffic_control.read"

	// RouteFeatureNotConfiguredCode is the DSM error code observed for
	// SYNO.Core.Network.Router.Static.Route get on a NAS with no advanced
	// routing / static routes configured. The facade treats it as an empty
	// (not-configured) route table rather than an error.
	RouteFeatureNotConfiguredCode = 4302
)

// Input is the empty input for the parameterless reads.
type Input struct{}

var generalOperation = compatibility.Operation[Input, network.General]{
	Name: GeneralReadCapabilityName,
	Variants: []compatibility.Variant[Input, network.General]{
		{
			Name: "network-general-get-v2", API: NetworkAPIName, Version: 2, Priority: 20,
			Match: compatibility.APIVersion(NetworkAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (network.General, error) {
				return runGeneral(ctx, executor, 2)
			},
		},
		{
			Name: "network-general-get-v1", API: NetworkAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(NetworkAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (network.General, error) {
				return runGeneral(ctx, executor, 1)
			},
		},
	},
}

func runGeneral(ctx context.Context, executor compatibility.Executor, version int) (network.General, error) {
	data, err := executor.Execute(ctx, compatibility.Request{API: NetworkAPIName, Version: version, Method: "get", ReadOnly: true})
	if err != nil {
		return network.General{}, fmt.Errorf("call %s.get v%d: %w", NetworkAPIName, version, err)
	}
	return decodeGeneral(data)
}

var proxyOperation = compatibility.Operation[Input, network.ProxySettings]{
	Name: ProxyReadCapabilityName,
	Variants: []compatibility.Variant[Input, network.ProxySettings]{
		{
			Name: "network-proxy-get-v1", API: ProxyAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(ProxyAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (network.ProxySettings, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: ProxyAPIName, Version: 1, Method: "get", ReadOnly: true})
				if err != nil {
					return network.ProxySettings{}, fmt.Errorf("call %s.get: %w", ProxyAPIName, err)
				}
				return decodeProxy(data)
			},
		},
	},
}

var interfacesOperation = compatibility.Operation[Input, []network.Interface]{
	Name: InterfacesReadCapabilityName,
	Variants: []compatibility.Variant[Input, []network.Interface]{
		{
			Name: "network-ethernet-list-v2", API: EthernetAPIName, Version: 2, Priority: 30,
			Match: compatibility.APIVersion(EthernetAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]network.Interface, error) {
				return runInterfaces(ctx, executor, EthernetAPIName, 2)
			},
		},
		{
			Name: "network-ethernet-list-v1", API: EthernetAPIName, Version: 1, Priority: 20,
			Match: compatibility.APIVersion(EthernetAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]network.Interface, error) {
				return runInterfaces(ctx, executor, EthernetAPIName, 1)
			},
		},
		{
			// Fallback for a NAS that exposes only the sparser Interface list.
			Name: "network-interface-list-v1", API: InterfaceAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(InterfaceAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]network.Interface, error) {
				return runInterfaces(ctx, executor, InterfaceAPIName, 1)
			},
		},
	},
}

func runInterfaces(ctx context.Context, executor compatibility.Executor, api string, version int) ([]network.Interface, error) {
	data, err := executor.Execute(ctx, compatibility.Request{API: api, Version: version, Method: "list", ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("call %s.list v%d: %w", api, version, err)
	}
	return decodeInterfaces(data)
}

var bondsOperation = compatibility.Operation[Input, []network.Bond]{
	Name: BondsReadCapabilityName,
	Variants: []compatibility.Variant[Input, []network.Bond]{
		{
			Name: "network-bond-list-v2", API: BondAPIName, Version: 2, Priority: 20,
			Match: compatibility.APIVersion(BondAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]network.Bond, error) {
				return runBonds(ctx, executor, 2)
			},
		},
		{
			Name: "network-bond-list-v1", API: BondAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(BondAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]network.Bond, error) {
				return runBonds(ctx, executor, 1)
			},
		},
	},
}

func runBonds(ctx context.Context, executor compatibility.Executor, version int) ([]network.Bond, error) {
	data, err := executor.Execute(ctx, compatibility.Request{API: BondAPIName, Version: version, Method: "list", ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("call %s.list v%d: %w", BondAPIName, version, err)
	}
	return decodeBonds(data)
}

var routesOperation = compatibility.Operation[Input, []network.Route]{
	Name: RoutesReadCapabilityName,
	Variants: []compatibility.Variant[Input, []network.Route]{
		{
			// The method is live-verified to EXIST; the success field shape is
			// wire-unverified (the lab returned code 4302, no advanced routing).
			Name: "network-static-route-get-v1", API: StaticRouteAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(StaticRouteAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]network.Route, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: StaticRouteAPIName, Version: 1, Method: "get", ReadOnly: true})
				if err != nil {
					return nil, fmt.Errorf("call %s.get: %w", StaticRouteAPIName, err)
				}
				return decodeRoutes(data)
			},
		},
	},
}

// trafficControlProbe carries no decoder: the load method needs an
// undiscoverable parameter, so this area is capability-detected only. The
// operation is defined solely so the capability report can select and report it.
var trafficControlProbe = compatibility.Operation[Input, struct{}]{
	Name: TrafficControlReadCapabilityName,
	Variants: []compatibility.Variant[Input, struct{}]{
		{
			Name: "network-traffic-control-detect-v1", API: TrafficControlAPIName, Version: 1, Priority: 10,
			Match:   compatibility.APIVersion(TrafficControlAPIName, 1),
			Execute: func(context.Context, compatibility.Executor, Input) (struct{}, error) { return struct{}{}, nil },
		},
	},
}

// APINames lists every DSM API this module reads or probes so the facade can
// discover them in one call before selecting variants.
func APINames() []string {
	return []string{
		NetworkAPIName,
		EthernetAPIName,
		InterfaceAPIName,
		BondAPIName,
		ProxyAPIName,
		StaticRouteAPIName,
		TrafficControlAPIName,
	}
}

func SelectGeneral(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := generalOperation.Select(target)
	return selection, err
}

func SelectProxy(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := proxyOperation.Select(target)
	return selection, err
}

func SelectInterfaces(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := interfacesOperation.Select(target)
	return selection, err
}

func SelectBonds(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := bondsOperation.Select(target)
	return selection, err
}

func SelectRoutes(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := routesOperation.Select(target)
	return selection, err
}

func SelectTrafficControl(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := trafficControlProbe.Select(target)
	return selection, err
}

func ExecuteGeneral(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (network.General, compatibility.Selection, error) {
	return generalOperation.Run(ctx, target, executor, Input{})
}

func ExecuteProxy(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (network.ProxySettings, compatibility.Selection, error) {
	return proxyOperation.Run(ctx, target, executor, Input{})
}

func ExecuteInterfaces(ctx context.Context, target compatibility.Target, executor compatibility.Executor) ([]network.Interface, compatibility.Selection, error) {
	return interfacesOperation.Run(ctx, target, executor, Input{})
}

func ExecuteBonds(ctx context.Context, target compatibility.Target, executor compatibility.Executor) ([]network.Bond, compatibility.Selection, error) {
	return bondsOperation.Run(ctx, target, executor, Input{})
}

func ExecuteRoutes(ctx context.Context, target compatibility.Target, executor compatibility.Executor) ([]network.Route, compatibility.Selection, error) {
	return routesOperation.Run(ctx, target, executor, Input{})
}
