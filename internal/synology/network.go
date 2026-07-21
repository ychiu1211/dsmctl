package synology

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/network"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	netops "github.com/ychiu1211/dsmctl/internal/synology/operations/network"
)

type NetworkGeneral = network.General
type NetworkInterface = network.Interface
type NetworkBond = network.Bond
type NetworkRouteTable = network.RouteTable
type NetworkCapabilities = network.Capabilities
type NetworkTransport = network.Transport
type NetworkManagementPath = network.ManagementPath
type NetworkGeneralChange = network.GeneralChange
type NetworkInterfaceChange = network.InterfaceChange
type NetworkGuardVerdict = network.GuardVerdict
type NetworkMutationResult = netops.MutationResult

// ErrNetworkInterfaceWriteUnverified is returned by ApplyNetworkInterfaceChange.
// The interface-write method (SYNO.Core.Network.Ethernet set) is present but its
// request-body shape is unverified (DSM returns code 4302 for every probed
// shape), so interface changes are plan+guard only until the wire is confirmed
// against a NAS whose admin JS resolves the confirm/apply flow.
var ErrNetworkInterfaceWriteUnverified = errors.New(
	"interface reconfiguration is plan-only in this build: the SYNO.Core.Network.Ethernet set request shape is wire-unverified (DSM returns code 4302 for every known body); the never-sever guard still evaluates the plan")

// NetworkTransportInfo reports the immutable address and port dsmctl connects to.
// It is ground truth for the never-sever guard's management-interface
// identification: the management NIC is the one whose IPv4 equals this host.
func (c *Client) NetworkTransportInfo() NetworkTransport {
	host := c.baseURL.Hostname()
	port := 0
	if p := c.baseURL.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	if port == 0 {
		if strings.EqualFold(c.baseURL.Scheme, "https") {
			port = 443
		} else {
			port = 80
		}
	}
	return NetworkTransport{Host: host, Port: port}
}

// NetworkGeneral reads the Control Panel > Network > General settings
// (SYNO.Core.Network get): hostname, default gateway (IPv4/IPv6), DNS
// nameservers, and the default-gateway interface. The outbound proxy
// (SYNO.Core.Network.Proxy) is read best-effort and attached; if that area is
// unsupported the general block still returns. The proxy password is never
// surfaced.
func (c *Client) NetworkGeneral(ctx context.Context) (NetworkGeneral, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, netops.APINames()...); err != nil {
		return NetworkGeneral{}, fmt.Errorf("prepare network target: %w", err)
	}
	general, _, err := netops.ExecuteGeneral(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return NetworkGeneral{}, fmt.Errorf("get network general: %w", err)
	}
	c.target.AddCapability(netops.GeneralReadCapabilityName)
	if proxy, _, err := netops.ExecuteProxy(ctx, c.target, lockedExecutor{client: c}); err == nil {
		general.Proxy = proxy
		c.target.AddCapability(netops.ProxyReadCapabilityName)
	} else if !compatibility.IsUnsupported(err) {
		return NetworkGeneral{}, fmt.Errorf("get network proxy: %w", err)
	}
	return general, nil
}

// NetworkInterfaces reads per-NIC configuration and link status
// (SYNO.Core.Network.Ethernet list, falling back to .Interface list).
func (c *Client) NetworkInterfaces(ctx context.Context) ([]NetworkInterface, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, netops.APINames()...); err != nil {
		return nil, fmt.Errorf("prepare network target: %w", err)
	}
	interfaces, _, err := netops.ExecuteInterfaces(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return nil, fmt.Errorf("get network interfaces: %w", err)
	}
	c.target.AddCapability(netops.InterfacesReadCapabilityName)
	return interfaces, nil
}

// NetworkBonds reads the link-aggregation bond list
// (SYNO.Core.Network.Bond list).
func (c *Client) NetworkBonds(ctx context.Context) ([]NetworkBond, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, netops.APINames()...); err != nil {
		return nil, fmt.Errorf("prepare network target: %w", err)
	}
	bonds, _, err := netops.ExecuteBonds(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return nil, fmt.Errorf("get network bonds: %w", err)
	}
	c.target.AddCapability(netops.BondsReadCapabilityName)
	return bonds, nil
}

// NetworkRoutes reads the static-route table
// (SYNO.Core.Network.Router.Static.Route get). On a NAS without advanced
// routing configured DSM returns error code 4302; that is treated as an empty,
// not-configured table rather than a module error (the API is present, just
// nothing to read).
func (c *Client) NetworkRoutes(ctx context.Context) (NetworkRouteTable, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, netops.APINames()...); err != nil {
		return NetworkRouteTable{}, fmt.Errorf("prepare network target: %w", err)
	}
	routes, _, err := netops.ExecuteRoutes(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		if isRouteNotConfigured(err) {
			c.target.AddCapability(netops.RoutesReadCapabilityName)
			return NetworkRouteTable{Configured: false, Routes: []network.Route{}}, nil
		}
		return NetworkRouteTable{}, fmt.Errorf("get network routes: %w", err)
	}
	c.target.AddCapability(netops.RoutesReadCapabilityName)
	return NetworkRouteTable{Configured: true, Routes: routes}, nil
}

// isRouteNotConfigured reports whether err is the DSM "advanced routing not
// configured" signal for the static-route read (code 4302).
func isRouteNotConfigured(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == netops.RouteFeatureNotConfiguredCode
}

// NetworkCapabilities reports which network reads dsmctl exposes for the
// selected NAS, plus the discovered backends. Each area is an independent
// boundary: one being absent leaves the others usable.
func (c *Client) NetworkCapabilities(ctx context.Context) (NetworkCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, netops.APINames()...); err != nil {
		return NetworkCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare network capabilities target: %w", err)
	}

	general, err := selectSupported(netops.SelectGeneral, c.target)
	if err != nil {
		return NetworkCapabilities{}, CompatibilityReport{}, fmt.Errorf("select network general backend: %w", err)
	}
	interfaces, err := selectSupported(netops.SelectInterfaces, c.target)
	if err != nil {
		return NetworkCapabilities{}, CompatibilityReport{}, fmt.Errorf("select network interfaces backend: %w", err)
	}
	bonds, err := selectSupported(netops.SelectBonds, c.target)
	if err != nil {
		return NetworkCapabilities{}, CompatibilityReport{}, fmt.Errorf("select network bonds backend: %w", err)
	}
	proxy, err := selectSupported(netops.SelectProxy, c.target)
	if err != nil {
		return NetworkCapabilities{}, CompatibilityReport{}, fmt.Errorf("select network proxy backend: %w", err)
	}
	routes, err := selectSupported(netops.SelectRoutes, c.target)
	if err != nil {
		return NetworkCapabilities{}, CompatibilityReport{}, fmt.Errorf("select network routes backend: %w", err)
	}
	traffic, err := selectSupported(netops.SelectTrafficControl, c.target)
	if err != nil {
		return NetworkCapabilities{}, CompatibilityReport{}, fmt.Errorf("select network traffic-control backend: %w", err)
	}
	generalWrite, err := selectSupported(netops.SelectGeneralSet, c.target)
	if err != nil {
		return NetworkCapabilities{}, CompatibilityReport{}, fmt.Errorf("select network general write backend: %w", err)
	}
	interfaceWrite, err := selectSupported(netops.SelectInterfaceSet, c.target)
	if err != nil {
		return NetworkCapabilities{}, CompatibilityReport{}, fmt.Errorf("select network interface write backend: %w", err)
	}

	for _, sel := range []struct {
		selection  compatibility.Selection
		capability string
	}{
		{general, netops.GeneralReadCapabilityName},
		{interfaces, netops.InterfacesReadCapabilityName},
		{bonds, netops.BondsReadCapabilityName},
		{proxy, netops.ProxyReadCapabilityName},
		{routes, netops.RoutesReadCapabilityName},
		{traffic, netops.TrafficControlReadCapabilityName},
		{generalWrite, netops.GeneralWriteCapabilityName},
	} {
		if sel.selection.Supported {
			c.target.AddCapability(sel.capability)
		}
	}

	capabilities := NetworkCapabilities{
		Module:                       network.ModuleName,
		GeneralRead:                  general.Supported,
		InterfacesRead:               interfaces.Supported,
		BondsRead:                    bonds.Supported,
		ProxyRead:                    proxy.Supported,
		RoutesRead:                   routes.Supported,
		TrafficControlRead:           traffic.Supported,
		BondFieldsWireUnverified:     bonds.Supported,
		RouteFieldsWireUnverified:    routes.Supported,
		IPv6FieldsWireUnverified:     interfaces.Supported,
		GeneralWrite:                 generalWrite.Supported,
		InterfaceWriteWireUnverified: interfaceWrite.Supported,
		Mutations:                    generalWrite.Supported,
	}
	return capabilities, c.target.Report(general, interfaces, bonds, proxy, routes, traffic, generalWrite), nil
}

// NetworkGeneralFresh reads only the general block (without the proxy) so the
// write path can merge a patch onto a freshly read config.
func (c *Client) NetworkGeneralFresh(ctx context.Context) (NetworkGeneral, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, netops.APINames()...); err != nil {
		return NetworkGeneral{}, fmt.Errorf("prepare network target: %w", err)
	}
	general, _, err := netops.ExecuteGeneral(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return NetworkGeneral{}, fmt.Errorf("get network general: %w", err)
	}
	return general, nil
}

// NetworkCurrentSources lists the active connection source IPs so the guard can
// record the protected sources. Best-effort.
func (c *Client) NetworkCurrentSources(ctx context.Context) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, netops.APINames()...); err != nil {
		return nil
	}
	if err := c.ensureAPIsLocked(ctx, netops.CurrentConnectionAPIName); err != nil {
		return nil
	}
	return netops.ExecuteCurrentSources(ctx, c.target, lockedExecutor{client: c})
}

// ApplyNetworkGeneralChange merges the patch onto a freshly read general block
// (patch-only ownership) and submits the whole object via SYNO.Core.Network set.
// The application-layer never-sever guard runs before this and is the authority
// on connectivity safety; this method performs no guard of its own.
func (c *Client) ApplyNetworkGeneralChange(ctx context.Context, change NetworkGeneralChange) (NetworkMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, netops.APINames()...); err != nil {
		return NetworkMutationResult{}, fmt.Errorf("prepare network mutation target: %w", err)
	}
	current, _, err := netops.ExecuteGeneral(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return NetworkMutationResult{}, fmt.Errorf("refresh network general before apply: %w", err)
	}
	merged := network.MergeGeneral(current, change)
	result, _, err := netops.ExecuteGeneralSet(ctx, c.target, lockedExecutor{client: c}, netops.GeneralSetInput{General: merged})
	if err != nil {
		return NetworkMutationResult{}, fmt.Errorf("apply network general: %w", err)
	}
	return result, nil
}

// ApplyNetworkInterfaceChange is refused while the interface-set wire is
// unverified. The plan and the never-sever guard still run; only the live write
// is withheld so an unconfirmed body is never sent to a NAS.
func (c *Client) ApplyNetworkInterfaceChange(ctx context.Context, _ NetworkInterfaceChange) (NetworkMutationResult, error) {
	return NetworkMutationResult{}, ErrNetworkInterfaceWriteUnverified
}
