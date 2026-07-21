package network

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/network"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// WI-069 Slice B write wire, live-probed on the DSM 7.3 lab (build 81168) with
// the persisted web-login session:
//
//	SYNO.Core.Network         set  v1/v2  CONFIRMED — full general object; returns
//	                                       {"hostname_changed_and_join_domain":bool}.
//	SYNO.Core.Network.Ethernet set  v1/v2  WIRE-UNVERIFIED — the method exists (it
//	                                       is the only write method; every other
//	                                       verb returns code 103), but it rejects
//	                                       every request-body shape probed (flat
//	                                       fields, full record, static and DHCP,
//	                                       array/object wrappers, on a disconnected
//	                                       NIC and on a connected non-management
//	                                       NIC) with code 4302 and applies nothing.
//	                                       DSM drives the interface change through a
//	                                       precondition/confirm flow visible only in
//	                                       the (webpack-chunked, unfetchable) admin
//	                                       JS, so the exact body could not be
//	                                       confirmed. Interface writes are therefore
//	                                       plan+guard only in this build; the apply
//	                                       is refused (ErrInterfaceWriteUnverified).
//
// The general set body was confirmed by a no-op round-trip: writing back the exact
// observed values succeeded and left the config unchanged. The minimal accepted
// body carries the posture booleans (arp_ignore, ipv4_first, multi_gateway,
// enable_ip_conflict_detect); a body of only server_name returned 403. This module
// always sends the full merged general object so DSM has every field.
const (
	GeneralWriteCapabilityName   = "network.general.write"
	InterfaceWriteCapabilityName = "network.interfaces.write"

	// CurrentConnectionAPIName feeds the guard the operator's active source IPs.
	// Read best-effort; it carries no session secret.
	CurrentConnectionAPIName = "SYNO.Core.CurrentConnection"
)

// MutationResult records the DSM backend that accepted a network write.
type MutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

// GeneralSetInput carries the complete desired general block (the caller merges
// its patch onto a freshly read general first).
type GeneralSetInput struct {
	General network.General
}

// InterfaceSetInput carries the complete desired interface record.
type InterfaceSetInput struct {
	Interface network.Interface
}

// encodeGeneral renders the merged general block into the confirmed
// SYNO.Core.Network set body. enableIPConflict is included only for v2 (the
// enable_ip_conflict_detect field is v2-only).
func encodeGeneral(general network.General, enableIPConflict bool) map[string]any {
	body := map[string]any{
		"server_name":     general.Hostname,
		"gateway":         general.DefaultGatewayV4,
		"v6gateway":       general.DefaultGatewayV6,
		"dns_manual":      general.DNSManual,
		"dns_primary":     general.DNSPrimary,
		"dns_secondary":   general.DNSSecondary,
		"use_dhcp_domain": general.UseDHCPDomain,
		"ipv4_first":      general.IPv4First,
		"multi_gateway":   general.MultiGateway,
		"arp_ignore":      general.ARPIgnore,
	}
	if enableIPConflict {
		body["enable_ip_conflict_detect"] = general.IPConflictDetect
	}
	return body
}

// encodeInterface renders the merged interface into the best-known (UNVERIFIED)
// SYNO.Core.Network.Ethernet set body. It is used only by the request-capture
// test and a future enablement; the live apply is refused while the wire is
// unverified.
func encodeInterface(iface network.Interface) map[string]any {
	return map[string]any{
		"ifname":   iface.Name,
		"ip":       iface.IPv4,
		"mask":     iface.Netmask,
		"gateway":  iface.GatewayV4,
		"use_dhcp": iface.UseDHCP,
		"mtu":      iface.MTU,
	}
}

var generalSetOperation = compatibility.Operation[GeneralSetInput, MutationResult]{
	Name: GeneralWriteCapabilityName,
	Variants: []compatibility.Variant[GeneralSetInput, MutationResult]{
		{
			Name: "network-general-set-v2", API: NetworkAPIName, Version: 2, Priority: 20,
			Match: compatibility.APIVersion(NetworkAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input GeneralSetInput) (MutationResult, error) {
				return runGeneralSet(ctx, executor, 2, input, true)
			},
		},
		{
			Name: "network-general-set-v1", API: NetworkAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(NetworkAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input GeneralSetInput) (MutationResult, error) {
				return runGeneralSet(ctx, executor, 1, input, false)
			},
		},
	},
}

func runGeneralSet(ctx context.Context, executor compatibility.Executor, version int, input GeneralSetInput, enableIPConflict bool) (MutationResult, error) {
	body := encodeGeneral(input.General, enableIPConflict)
	if _, err := executor.Execute(ctx, compatibility.Request{API: NetworkAPIName, Version: version, Method: "set", JSONParameters: body}); err != nil {
		return MutationResult{}, fmt.Errorf("call %s.set v%d: %w", NetworkAPIName, version, err)
	}
	return MutationResult{Method: "set"}, nil
}

// interfaceSetOperation encodes the best-known (WIRE-UNVERIFIED) Ethernet.set
// body. The facade never runs it against a live NAS while the wire is unverified;
// it exists to document the shape and to be locked by a request-capture test.
var interfaceSetOperation = compatibility.Operation[InterfaceSetInput, MutationResult]{
	Name: InterfaceWriteCapabilityName,
	Variants: []compatibility.Variant[InterfaceSetInput, MutationResult]{
		{
			Name: "network-ethernet-set-v2", API: EthernetAPIName, Version: 2, Priority: 20,
			Match: compatibility.APIVersion(EthernetAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input InterfaceSetInput) (MutationResult, error) {
				return runInterfaceSet(ctx, executor, 2, input)
			},
		},
		{
			Name: "network-ethernet-set-v1", API: EthernetAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(EthernetAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input InterfaceSetInput) (MutationResult, error) {
				return runInterfaceSet(ctx, executor, 1, input)
			},
		},
	},
}

func runInterfaceSet(ctx context.Context, executor compatibility.Executor, version int, input InterfaceSetInput) (MutationResult, error) {
	body := encodeInterface(input.Interface)
	if _, err := executor.Execute(ctx, compatibility.Request{API: EthernetAPIName, Version: version, Method: "set", JSONParameters: body}); err != nil {
		return MutationResult{}, fmt.Errorf("call %s.set v%d: %w", EthernetAPIName, version, err)
	}
	return MutationResult{Method: "set"}, nil
}

func SelectGeneralSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := generalSetOperation.Select(target)
	return selection, err
}

func SelectInterfaceSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := interfaceSetOperation.Select(target)
	return selection, err
}

// ExecuteGeneralSet writes the complete desired general block. The caller merges
// its patch onto a freshly read general first (patch-only ownership).
func ExecuteGeneralSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input GeneralSetInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := generalSetOperation.Run(ctx, target, executor, input)
	if err == nil {
		result.Backend, result.API, result.Version = selection.Backend, selection.API, selection.Version
	}
	return result, selection, err
}

// ExecuteInterfaceSet runs the best-known interface-set body. It is exported for
// the request-capture test; the live apply path stays disabled while the wire is
// unverified.
func ExecuteInterfaceSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input InterfaceSetInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := interfaceSetOperation.Run(ctx, target, executor, input)
	if err == nil {
		result.Backend, result.API, result.Version = selection.Backend, selection.API, selection.Version
	}
	return result, selection, err
}

// ExecuteCurrentSources lists active connection source IPs so the guard can
// report the protected sources. Best-effort: no API or a transient failure yields
// no sources rather than an error, and no session secret is read.
func ExecuteCurrentSources(ctx context.Context, target compatibility.Target, executor compatibility.Executor) []string {
	if !target.SupportsAPI(CurrentConnectionAPIName, 1) {
		return nil
	}
	data, err := executor.Execute(ctx, compatibility.Request{API: CurrentConnectionAPIName, Version: 1, Method: "list", ReadOnly: true})
	if err != nil {
		return nil
	}
	return decodeCurrentSources(data)
}

func decodeCurrentSources(data json.RawMessage) []string {
	root, err := decodeObject(data, "current connection")
	if err != nil {
		return nil
	}
	value, ok := root["items"]
	if !ok || value == nil {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	sources := make([]string, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		from := stringValue(object, "from", "ip", "ip_addr")
		if from == "" || seen[from] {
			continue
		}
		seen[from] = true
		sources = append(sources, from)
	}
	return sources
}
