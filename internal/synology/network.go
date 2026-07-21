package synology

import (
	"context"
	"errors"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/network"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	netops "github.com/ychiu1211/dsmctl/internal/synology/operations/network"
)

type NetworkGeneral = network.General
type NetworkInterface = network.Interface
type NetworkBond = network.Bond
type NetworkRouteTable = network.RouteTable
type NetworkCapabilities = network.Capabilities

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
	} {
		if sel.selection.Supported {
			c.target.AddCapability(sel.capability)
		}
	}

	capabilities := NetworkCapabilities{
		Module:                    network.ModuleName,
		GeneralRead:               general.Supported,
		InterfacesRead:            interfaces.Supported,
		BondsRead:                 bonds.Supported,
		ProxyRead:                 proxy.Supported,
		RoutesRead:                routes.Supported,
		TrafficControlRead:        traffic.Supported,
		BondFieldsWireUnverified:  bonds.Supported,
		RouteFieldsWireUnverified: routes.Supported,
		IPv6FieldsWireUnverified:  interfaces.Supported,
		Mutations:                 false,
	}
	return capabilities, c.target.Report(general, interfaces, bonds, proxy, routes, traffic), nil
}
