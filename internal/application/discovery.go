package application

import (
	"context"
	"time"

	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
	"github.com/ychiu1211/dsmctl/internal/synology/findhost"
)

// DiscoverResult is the outcome of a LAN discovery sweep. It intentionally has
// no NAS field: discovery is NAS-independent and needs no configured profile,
// credential, or DSM session.
type DiscoverResult struct {
	Devices []discovery.Device `json:"devices" jsonschema:"Synology devices that answered the findhost broadcast, deduplicated by device"`
	// SavedTotal is how many devices are in the saved cross-run set after this
	// sweep was merged in. It equals len(Devices) when no store is configured, and
	// can exceed it when earlier sweeps saw devices this one missed — the signal
	// that a sweep under-counted under UDP-9999 contention.
	SavedTotal int `json:"saved_total,omitempty" jsonschema:"Devices in the saved cross-run set after merging this sweep"`
}

// DiscoverDevices broadcasts a findhost discovery query on the local network
// and returns the Synology devices that answer. It contacts no configured NAS,
// requires no credentials, and mutates nothing on any device — it only sends
// query packets. When a discovery store is configured it also merges the result
// into the saved cross-run set.
func (s *Service) DiscoverDevices(ctx context.Context, query discovery.Query) (DiscoverResult, error) {
	return s.DiscoverDevicesStream(ctx, query, nil)
}

// DiscoverDevicesStream is DiscoverDevices with progress streaming: onDevice, if
// non-nil, is called for each newly discovered device as the sweep runs, so a
// long-running caller (for example the CLI) can show devices as they appear. The
// returned result still holds the full, deduplicated set.
func (s *Service) DiscoverDevicesStream(ctx context.Context, query discovery.Query, onDevice func(discovery.Device)) (DiscoverResult, error) {
	devices, err := findhost.Probe(ctx, query, findhost.ProbeOptions{OnDevice: onDevice})
	if err != nil {
		return DiscoverResult{}, err
	}
	if devices == nil {
		devices = []discovery.Device{}
	}
	result := DiscoverResult{Devices: devices, SavedTotal: len(devices)}
	if s.discoveryStore != nil {
		// Persisting is best-effort: a scan that succeeded must still be returned
		// even if the cache write fails. A failure just leaves SavedTotal at this
		// sweep's own count.
		if set, err := s.discoveryStore.merge(devices, time.Now().UTC()); err == nil {
			result.SavedTotal = len(set.Devices)
		}
	}
	return result, nil
}

// CachedDiscoveries returns the saved cross-run discovery set without scanning.
// It returns an empty set when no store is configured or nothing has been saved.
func (s *Service) CachedDiscoveries(ctx context.Context) (SavedDiscoveries, error) {
	if err := ctx.Err(); err != nil {
		return SavedDiscoveries{}, err
	}
	if s.discoveryStore == nil {
		return SavedDiscoveries{}, nil
	}
	return s.discoveryStore.load()
}

// ClearDiscoveries discards the saved cross-run discovery set. It is a no-op when
// no store is configured or nothing has been saved.
func (s *Service) ClearDiscoveries(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.discoveryStore == nil {
		return nil
	}
	return s.discoveryStore.clear()
}
