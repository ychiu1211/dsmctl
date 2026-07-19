package application

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
)

// SavedDevice is one device in the cross-run saved discovery set: the discovery
// model plus the window over which it has been seen. Because any single findhost
// sweep can miss devices under UDP-9999 contention, the saved set is the running
// union of everything recent sweeps found, so repeated scans keep building it.
type SavedDevice struct {
	discovery.Device
	FirstSeen time.Time `json:"first_seen" jsonschema:"When this device was first discovered"`
	LastSeen  time.Time `json:"last_seen" jsonschema:"When this device most recently answered a discovery sweep"`
}

// SavedDiscoveries is the persisted cross-run union of discovered devices.
type SavedDiscoveries struct {
	UpdatedAt time.Time     `json:"updated_at" jsonschema:"When the saved set was last updated"`
	Devices   []SavedDevice `json:"devices" jsonschema:"Saved devices, ordered by IPv4 address"`
}

// DiscoveryStorePath returns the path of the saved-discovery file for a given
// configuration path: a sibling of the config file, so it shares the per-user
// config directory. Both the CLI and the local MCP server derive the store from
// their config path, so they read and write the same saved set.
func DiscoveryStorePath(configPath string) string {
	if configPath == "" {
		configPath = config.DefaultPath()
	}
	return filepath.Join(filepath.Dir(configPath), "discovered.json")
}

// discoveryStore persists the saved discovery set to a JSON file. It is the
// single shared implementation behind both entry points' "saved results".
type discoveryStore struct {
	path string
}

func newDiscoveryStore(path string) *discoveryStore {
	return &discoveryStore{path: path}
}

func (s *discoveryStore) load() (SavedDiscoveries, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return SavedDiscoveries{}, nil
	}
	if err != nil {
		return SavedDiscoveries{}, fmt.Errorf("read saved discovery results %s: %w", s.path, err)
	}
	var set SavedDiscoveries
	if err := json.Unmarshal(data, &set); err != nil {
		return SavedDiscoveries{}, fmt.Errorf("decode saved discovery results %s: %w", s.path, err)
	}
	return set, nil
}

func (s *discoveryStore) write(set SavedDiscoveries) error {
	data, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return fmt.Errorf("encode discovery results: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write saved discovery results %s: %w", s.path, err)
	}
	return nil
}

// merge folds this sweep's devices into the saved set, updating LastSeen for
// devices seen again and preserving FirstSeen, and returns the resulting set. An
// empty sweep leaves the saved set untouched (a sweep that happened to find
// nothing must not erase history).
func (s *discoveryStore) merge(devices []discovery.Device, now time.Time) (SavedDiscoveries, error) {
	set, err := s.load()
	if err != nil {
		return SavedDiscoveries{}, err
	}
	if len(devices) == 0 {
		return set, nil
	}
	index := make(map[string]int, len(set.Devices))
	for i, record := range set.Devices {
		index[discoveryDeviceKey(record.Device)] = i
	}
	for _, device := range devices {
		key := discoveryDeviceKey(device)
		if i, ok := index[key]; ok {
			set.Devices[i] = SavedDevice{Device: device, FirstSeen: set.Devices[i].FirstSeen, LastSeen: now}
			continue
		}
		index[key] = len(set.Devices)
		set.Devices = append(set.Devices, SavedDevice{Device: device, FirstSeen: now, LastSeen: now})
	}
	sortSavedDevices(set.Devices)
	set.UpdatedAt = now
	if err := s.write(set); err != nil {
		return SavedDiscoveries{}, err
	}
	return set, nil
}

func (s *discoveryStore) clear() error {
	err := os.Remove(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear saved discovery results %s: %w", s.path, err)
	}
	return nil
}

// discoveryDeviceKey is the stable identity used to merge a device across runs,
// mirroring the findhost dedup order: serial, then representative MAC, then
// address, then hostname.
func discoveryDeviceKey(device discovery.Device) string {
	switch {
	case device.Serial != "":
		return "serial:" + strings.ToLower(device.Serial)
	case device.MACAddress != "":
		return "mac:" + strings.ToLower(device.MACAddress)
	case len(device.IPv4Addresses) > 0:
		return "ip:" + device.IPv4Addresses[0]
	case device.IPAddress != "":
		return "ip:" + device.IPAddress
	default:
		return "host:" + strings.ToLower(device.Hostname)
	}
}

// sortSavedDevices orders saved records by IPv4 address (numerically), then
// hostname, matching the live discovery ordering.
func sortSavedDevices(records []SavedDevice) {
	sort.SliceStable(records, func(i, j int) bool {
		if less, decided := ipLess(records[i].IPAddress, records[j].IPAddress); decided {
			return less
		}
		return records[i].Hostname < records[j].Hostname
	})
}

// ipLess compares two dotted-quad addresses numerically. decided is false when
// the two are equal (or not both parseable), so the caller can fall back to a
// secondary key.
func ipLess(a, b string) (less bool, decided bool) {
	ipA, ipB := net.ParseIP(a), net.ParseIP(b)
	if ipA == nil || ipB == nil {
		if a == b {
			return false, false
		}
		return a < b, true
	}
	cmp := bytes.Compare(ipA.To16(), ipB.To16())
	if cmp == 0 {
		return false, false
	}
	return cmp < 0, true
}
