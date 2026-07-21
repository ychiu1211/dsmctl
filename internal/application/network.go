package application

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type NetworkGeneralResult struct {
	NAS     string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	General synology.NetworkGeneral `json:"general" jsonschema:"General network settings: hostname, default gateway, DNS, and outbound proxy"`
}

type NetworkInterfacesResult struct {
	NAS        string                      `json:"nas" jsonschema:"NAS profile used for the request"`
	Interfaces []synology.NetworkInterface `json:"interfaces" jsonschema:"Per-interface configuration and link status"`
}

type NetworkBondsResult struct {
	NAS   string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Bonds []synology.NetworkBond `json:"bonds" jsonschema:"Link-aggregation bonds with their mode and member NICs"`
}

type NetworkRoutesResult struct {
	NAS    string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Routes synology.NetworkRouteTable `json:"routes" jsonschema:"The static-route table; configured is false when advanced routing is not set up"`
}

type NetworkCapabilitiesResult struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.NetworkCapabilities `json:"capabilities" jsonschema:"Network reads currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected network backends"`
}

type networkClient interface {
	NetworkGeneral(context.Context) (synology.NetworkGeneral, error)
	NetworkInterfaces(context.Context) ([]synology.NetworkInterface, error)
	NetworkBonds(context.Context) ([]synology.NetworkBond, error)
	NetworkRoutes(context.Context) (synology.NetworkRouteTable, error)
	NetworkCapabilities(context.Context) (synology.NetworkCapabilities, synology.CompatibilityReport, error)
	NetworkGeneralFresh(context.Context) (synology.NetworkGeneral, error)
	NetworkTransportInfo() synology.NetworkTransport
	NetworkCurrentSources(context.Context) []string
	ApplyNetworkGeneralChange(context.Context, synology.NetworkGeneralChange) (synology.NetworkMutationResult, error)
	ApplyNetworkInterfaceChange(context.Context, synology.NetworkInterfaceChange) (synology.NetworkMutationResult, error)
}

func (s *Service) GetNetworkGeneral(ctx context.Context, requestedNAS string) (NetworkGeneralResult, error) {
	name, client, err := s.networkClient(ctx, requestedNAS)
	if err != nil {
		return NetworkGeneralResult{}, err
	}
	general, err := client.NetworkGeneral(ctx)
	if err != nil {
		return NetworkGeneralResult{}, authenticationError(name, err)
	}
	return NetworkGeneralResult{NAS: name, General: general}, nil
}

func (s *Service) GetNetworkInterfaces(ctx context.Context, requestedNAS string) (NetworkInterfacesResult, error) {
	name, client, err := s.networkClient(ctx, requestedNAS)
	if err != nil {
		return NetworkInterfacesResult{}, err
	}
	interfaces, err := client.NetworkInterfaces(ctx)
	if err != nil {
		return NetworkInterfacesResult{}, authenticationError(name, err)
	}
	return NetworkInterfacesResult{NAS: name, Interfaces: interfaces}, nil
}

func (s *Service) GetNetworkBonds(ctx context.Context, requestedNAS string) (NetworkBondsResult, error) {
	name, client, err := s.networkClient(ctx, requestedNAS)
	if err != nil {
		return NetworkBondsResult{}, err
	}
	bonds, err := client.NetworkBonds(ctx)
	if err != nil {
		return NetworkBondsResult{}, authenticationError(name, err)
	}
	return NetworkBondsResult{NAS: name, Bonds: bonds}, nil
}

func (s *Service) GetNetworkRoutes(ctx context.Context, requestedNAS string) (NetworkRoutesResult, error) {
	name, client, err := s.networkClient(ctx, requestedNAS)
	if err != nil {
		return NetworkRoutesResult{}, err
	}
	routes, err := client.NetworkRoutes(ctx)
	if err != nil {
		return NetworkRoutesResult{}, authenticationError(name, err)
	}
	return NetworkRoutesResult{NAS: name, Routes: routes}, nil
}

func (s *Service) GetNetworkCapabilities(ctx context.Context, requestedNAS string) (NetworkCapabilitiesResult, error) {
	name, client, err := s.networkClient(ctx, requestedNAS)
	if err != nil {
		return NetworkCapabilitiesResult{}, err
	}
	capabilities, report, err := client.NetworkCapabilities(ctx)
	if err != nil {
		return NetworkCapabilitiesResult{}, authenticationError(name, err)
	}
	return NetworkCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) networkClient(ctx context.Context, requestedNAS string) (string, networkClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(networkClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement network")
	}
	return name, client, nil
}

var _ networkClient = (*synology.Client)(nil)
