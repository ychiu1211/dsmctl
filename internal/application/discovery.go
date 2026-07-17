package application

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
	"github.com/ychiu1211/dsmctl/internal/synology/findhost"
)

// DiscoverResult is the outcome of a LAN discovery sweep. It intentionally has
// no NAS field: discovery is NAS-independent and needs no configured profile,
// credential, or DSM session.
type DiscoverResult struct {
	Devices []discovery.Device `json:"devices" jsonschema:"Synology devices that answered the findhost broadcast, deduplicated by device"`
}

// DiscoverDevices broadcasts a findhost discovery query on the local network
// and returns the Synology devices that answer. It contacts no configured NAS,
// requires no credentials, and mutates nothing — it only sends query packets.
func (s *Service) DiscoverDevices(ctx context.Context, query discovery.Query) (DiscoverResult, error) {
	devices, err := findhost.Probe(ctx, query)
	if err != nil {
		return DiscoverResult{}, err
	}
	if devices == nil {
		devices = []discovery.Device{}
	}
	return DiscoverResult{Devices: devices}, nil
}
