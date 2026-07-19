package application

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
)

func TestDiscoveryStoreMergesAcrossRuns(t *testing.T) {
	store := newDiscoveryStore(filepath.Join(t.TempDir(), "discovered.json"))
	t0 := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)

	// First run finds A and B.
	run1 := []discovery.Device{
		{Hostname: "nas-a", Serial: "SERIALA", IPAddress: "10.0.0.5", IPv4Addresses: []string{"10.0.0.5"}, State: discovery.StateReady},
		{Hostname: "nas-b", Serial: "SERIALB", IPAddress: "10.0.0.9", IPv4Addresses: []string{"10.0.0.9"}, State: discovery.StateReady},
	}
	if set, err := store.merge(run1, t0); err != nil {
		t.Fatalf("merge run1: %v", err)
	} else if len(set.Devices) != 2 {
		t.Fatalf("after run1 saved %d, want 2", len(set.Devices))
	}

	// Second run finds only A (B lost to port contention) plus a new C.
	run2 := []discovery.Device{
		{Hostname: "nas-a", Serial: "SERIALA", IPAddress: "10.0.0.5", IPv4Addresses: []string{"10.0.0.5"}, State: discovery.StateReady},
		{Hostname: "nas-c", Serial: "SERIALC", IPAddress: "10.0.0.2", IPv4Addresses: []string{"10.0.0.2"}, State: discovery.StateReady},
	}
	set, err := store.merge(run2, t1)
	if err != nil {
		t.Fatalf("merge run2: %v", err)
	}

	// The union of both runs must survive — B is not dropped because run2 missed
	// it. That is the whole point of saving across flaky sweeps.
	if len(set.Devices) != 3 {
		t.Fatalf("saved %d devices, want 3 (union of both runs)", len(set.Devices))
	}
	bySerial := map[string]SavedDevice{}
	for _, record := range set.Devices {
		bySerial[record.Serial] = record
	}
	for _, serial := range []string{"SERIALA", "SERIALB", "SERIALC"} {
		if _, ok := bySerial[serial]; !ok {
			t.Errorf("saved set missing %s", serial)
		}
	}
	// A was seen in both runs: FirstSeen from run1, LastSeen advanced to run2.
	if a := bySerial["SERIALA"]; !a.FirstSeen.Equal(t0) || !a.LastSeen.Equal(t1) {
		t.Errorf("nas-a FirstSeen=%s LastSeen=%s, want %s / %s", a.FirstSeen, a.LastSeen, t0, t1)
	}
	// Records are ordered numerically by IP: C (.2) < A (.5) < B (.9).
	wantOrder := []string{"SERIALC", "SERIALA", "SERIALB"}
	for i, serial := range wantOrder {
		if set.Devices[i].Serial != serial {
			t.Errorf("record %d serial = %s, want %s (IP-sorted)", i, set.Devices[i].Serial, serial)
		}
	}

	// A reload sees the same persisted union.
	reloaded, err := store.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(reloaded.Devices) != 3 {
		t.Fatalf("reloaded %d devices, want 3", len(reloaded.Devices))
	}
}

func TestDiscoveryStoreEmptyRunKeepsHistory(t *testing.T) {
	store := newDiscoveryStore(filepath.Join(t.TempDir(), "discovered.json"))
	seed := []discovery.Device{{Hostname: "nas-a", Serial: "SERIALA", IPAddress: "10.0.0.5", State: discovery.StateReady}}
	if _, err := store.merge(seed, time.Unix(1, 0).UTC()); err != nil {
		t.Fatalf("merge seed: %v", err)
	}
	// A sweep that found nothing (e.g. cancelled immediately) must not erase the
	// saved set.
	set, err := store.merge(nil, time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatalf("merge empty: %v", err)
	}
	if len(set.Devices) != 1 {
		t.Fatalf("saved %d devices after empty run, want 1 (history kept)", len(set.Devices))
	}
}

func TestDiscoveryStoreLoadMissingFileIsEmpty(t *testing.T) {
	store := newDiscoveryStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	set, err := store.load()
	if err != nil {
		t.Fatalf("load on missing file: %v", err)
	}
	if len(set.Devices) != 0 {
		t.Fatalf("missing store returned %d devices, want 0", len(set.Devices))
	}
}

func TestDiscoveryStoreClear(t *testing.T) {
	store := newDiscoveryStore(filepath.Join(t.TempDir(), "discovered.json"))
	if _, err := store.merge([]discovery.Device{{Serial: "S", IPAddress: "10.0.0.1"}}, time.Unix(1, 0).UTC()); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if err := store.clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	set, err := store.load()
	if err != nil {
		t.Fatalf("load after clear: %v", err)
	}
	if len(set.Devices) != 0 {
		t.Fatalf("after clear saved %d devices, want 0", len(set.Devices))
	}
	// Clearing an already-absent store is not an error.
	if err := store.clear(); err != nil {
		t.Fatalf("clear when absent: %v", err)
	}
}

func TestDiscoveryDeviceKeyPrefersSerialThenMACThenIP(t *testing.T) {
	cases := []struct {
		name   string
		device discovery.Device
		want   string
	}{
		{"serial wins", discovery.Device{Serial: "ABC", MACAddress: "00:11:22:33:44:55", IPAddress: "10.0.0.1"}, "serial:abc"},
		{"mac when no serial", discovery.Device{MACAddress: "00:11:22:33:44:55", IPAddress: "10.0.0.1"}, "mac:00:11:22:33:44:55"},
		{"ip when no serial or mac", discovery.Device{IPv4Addresses: []string{"10.0.0.1"}}, "ip:10.0.0.1"},
		{"hostname as last resort", discovery.Device{Hostname: "NAS"}, "host:nas"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := discoveryDeviceKey(tc.device); got != tc.want {
				t.Errorf("discoveryDeviceKey = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestServiceDiscoveryStoreRoundTrip(t *testing.T) {
	// The Service-level cached/clear methods operate on the same saved set the
	// store writes, so a merge is visible through CachedDiscoveries and removed by
	// ClearDiscoveries. This is the surface both the CLI and MCP share.
	path := filepath.Join(t.TempDir(), "discovered.json")
	svc := &Service{discoveryStore: newDiscoveryStore(path)}
	ctx := context.Background()

	if _, err := svc.discoveryStore.merge([]discovery.Device{{Serial: "S1", IPAddress: "10.0.0.1"}}, time.Unix(10, 0).UTC()); err != nil {
		t.Fatalf("seed merge: %v", err)
	}
	saved, err := svc.CachedDiscoveries(ctx)
	if err != nil {
		t.Fatalf("CachedDiscoveries: %v", err)
	}
	if len(saved.Devices) != 1 {
		t.Fatalf("CachedDiscoveries returned %d, want 1", len(saved.Devices))
	}
	if err := svc.ClearDiscoveries(ctx); err != nil {
		t.Fatalf("ClearDiscoveries: %v", err)
	}
	saved, err = svc.CachedDiscoveries(ctx)
	if err != nil {
		t.Fatalf("CachedDiscoveries after clear: %v", err)
	}
	if len(saved.Devices) != 0 {
		t.Fatalf("after clear CachedDiscoveries returned %d, want 0", len(saved.Devices))
	}
}

func TestServiceWithoutStoreIsInert(t *testing.T) {
	// A Service with no discovery store (as the gateway and unit tests build it)
	// must treat cached/clear as empty no-ops rather than panicking.
	svc := &Service{}
	ctx := context.Background()
	saved, err := svc.CachedDiscoveries(ctx)
	if err != nil {
		t.Fatalf("CachedDiscoveries without store: %v", err)
	}
	if len(saved.Devices) != 0 {
		t.Fatalf("CachedDiscoveries without store returned %d, want 0", len(saved.Devices))
	}
	if err := svc.ClearDiscoveries(ctx); err != nil {
		t.Fatalf("ClearDiscoveries without store: %v", err)
	}
}
