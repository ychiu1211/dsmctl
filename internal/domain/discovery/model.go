// Package discovery holds the stable, protocol-independent model for Synology
// LAN device discovery.
//
// Discovery speaks the Synology "findhost" UDP broadcast protocol used by
// Synology Assistant and DSM's findhostd: a client broadcasts a query datagram
// and reachable Synology devices answer with their identity and network
// configuration. It is connectionless, unauthenticated, and NAS-independent —
// there is no DSM session, WebAPI call, or configured profile involved — so it
// sits outside the per-operation DSM WebAPI contract by design (see WI-023).
//
// Field names here are stable semantics, not raw findhost packet IDs. The
// findhost codec translates the wire TLVs into these values at the boundary.
package discovery

import (
	"sort"
	"strings"
	"time"
)

// DefaultTimeout is the default listen window for one discovery sweep. Devices
// answer within a few hundred milliseconds on a healthy LAN; a few seconds
// absorbs UDP loss and slower responders without making the command feel stuck.
const DefaultTimeout = 3 * time.Second

// Device states, as self-reported by the device's findhost quick-config status
// (and, when that is absent, inferred from the response packet type). These are
// stable dsmctl labels, not raw DSM strings. A device reports its own state; it
// is a hint about what the device is ready for, not a guarantee.
const (
	// StateReady is an installed, configured, running device.
	StateReady = "ready"
	// StateNotInstalled is a device with no DSM installed yet (a fresh or reset
	// unit waiting for installation).
	StateNotInstalled = "not_installed"
	// StateMigratable is a device offering system migration (disks moved from
	// another unit).
	StateMigratable = "migratable"
	// StateRecoverable is a device whose system is recoverable/needs recovery.
	StateRecoverable = "recoverable"
	// StateInstalling covers installing, quick-configuring, or updating.
	StateInstalling = "installing"
	// StateBooting is a device still booting.
	StateBooting = "booting"
	// StateStarting covers post-boot service start and quota checking.
	StateStarting = "starting"
	// StateConfiguringNetwork is a device applying network settings.
	StateConfiguringNetwork = "configuring_network"
	// StateMemoryTesting is a device running a memory test.
	StateMemoryTesting = "memory_testing"
	// StateNetworkTesting is a device running a network test.
	StateNetworkTesting = "network_testing"
	// StateCrashed is a device reporting a crashed system partition.
	StateCrashed = "crashed"
	// StateOffline is a device reporting itself offline.
	StateOffline = "offline"
	// StateUnknown is used when the device did not report a state we recognize.
	StateUnknown = "unknown"
)

// Response kinds, the stable dsmctl name for the findhost response packet type
// that carried a device. Both the original and the "_V2" packet types map to
// the same kind.
const (
	// KindResponse is a standard installed-system response.
	KindResponse = "response"
	// KindJuniorResponse is an installer/uninstalled-system response.
	KindJuniorResponse = "junior_response"
	// KindRecoverResponse is a system-recovery response.
	KindRecoverResponse = "recover_response"
	// KindInfoAvailable is the lightweight "information available" announcement
	// a device broadcasts to invite a query; it carries only minimal fields.
	KindInfoAvailable = "info_available"
)

// Query parameterizes one discovery sweep.
type Query struct {
	// Timeout is the total listen window for the sweep. Zero uses DefaultTimeout.
	Timeout time.Duration `json:"timeout,omitempty" jsonschema:"Total listen window for the discovery sweep; defaults to 3s"`
}

// Normalize fills defaults and clamps unreasonable values.
func (q Query) Normalize() Query {
	if q.Timeout <= 0 {
		q.Timeout = DefaultTimeout
	}
	if q.Timeout > time.Minute {
		q.Timeout = time.Minute
	}
	return q
}

// Device is one Synology device discovered on the LAN. A device that answers on
// more than one network interface is reported once, with every observed IPv4
// address collected in IPv4Addresses.
type Device struct {
	Hostname      string   `json:"hostname,omitempty" jsonschema:"Device hostname as reported by findhost"`
	Model         string   `json:"model,omitempty" jsonschema:"Marketing model name, e.g. DS3018xs"`
	OS            string   `json:"os,omitempty" jsonschema:"Operating system family reported by the device: DSM, SRM, or BSM"`
	OSVersion     string   `json:"os_version,omitempty" jsonschema:"Human-readable OS version combining version, build, and update, e.g. 7.3.2-81180 Update 3"`
	Serial        string   `json:"serial,omitempty" jsonschema:"Device serial number"`
	MACAddress    string   `json:"mac_address,omitempty" jsonschema:"Representative MAC address (the device's first NIC when reported, otherwise the responding NIC)"`
	IPAddress     string   `json:"ip_address,omitempty" jsonschema:"Representative IPv4 address (the lowest observed address)"`
	IPv4Addresses []string `json:"ipv4_addresses,omitempty" jsonschema:"All IPv4 addresses observed for this device across its interfaces"`
	MACAddresses  []string `json:"mac_addresses,omitempty" jsonschema:"All responding-interface MAC addresses observed for this device"`
	Netmask       string   `json:"netmask,omitempty" jsonschema:"IPv4 netmask of the representative interface"`
	Gateway       string   `json:"gateway,omitempty" jsonschema:"Default gateway reported by the representative interface"`
	DNS           string   `json:"dns,omitempty" jsonschema:"Primary DNS server reported by the representative interface"`
	BuildNumber   int      `json:"build_number,omitempty" jsonschema:"OS build number"`
	UpdateNumber  int      `json:"update_number,omitempty" jsonschema:"Small-fix / update number (the N in 'Update N')"`
	SupportsRAID  bool     `json:"supports_raid,omitempty" jsonschema:"Whether the device reports RAID support"`
	State         string   `json:"state" jsonschema:"Device-reported install/run state: ready, not_installed, migratable, recoverable, installing, booting, starting, configuring_network, memory_testing, network_testing, crashed, offline, or unknown"`
	ResponseKind  string   `json:"response_kind" jsonschema:"findhost response type that carried this device: response, junior_response, recover_response, or info_available"`
	// DedupID is the stable identity used to fold multiple interface responses
	// into one device. It is not part of the wire model; callers may ignore it.
	DedupID string `json:"-"`
}

// Merge folds another response for the same physical device into d, preferring
// existing non-empty scalar fields (the first, richest response wins) and
// unioning the observed address and MAC sets.
func (d *Device) Merge(other Device) {
	preferString(&d.Hostname, other.Hostname)
	preferString(&d.Model, other.Model)
	preferString(&d.OS, other.OS)
	preferString(&d.OSVersion, other.OSVersion)
	preferString(&d.Serial, other.Serial)
	preferString(&d.MACAddress, other.MACAddress)
	preferString(&d.Netmask, other.Netmask)
	preferString(&d.Gateway, other.Gateway)
	preferString(&d.DNS, other.DNS)
	if d.BuildNumber == 0 {
		d.BuildNumber = other.BuildNumber
	}
	if d.UpdateNumber == 0 {
		d.UpdateNumber = other.UpdateNumber
	}
	d.SupportsRAID = d.SupportsRAID || other.SupportsRAID
	// A fuller response kind should win over a bare info_available hint.
	if d.ResponseKind == KindInfoAvailable && other.ResponseKind != "" && other.ResponseKind != KindInfoAvailable {
		d.ResponseKind = other.ResponseKind
		d.State = other.State
	}
	d.IPv4Addresses = mergeUnique(d.IPv4Addresses, other.IPv4Addresses)
	d.MACAddresses = mergeUnique(d.MACAddresses, other.MACAddresses)
	d.reconcileRepresentatives()
}

// reconcileRepresentatives keeps IPAddress the lowest observed address and
// ensures the representative MAC is part of the observed MAC set.
func (d *Device) reconcileRepresentatives() {
	if len(d.IPv4Addresses) > 0 {
		d.IPAddress = d.IPv4Addresses[0]
	}
	if d.MACAddress == "" && len(d.MACAddresses) > 0 {
		d.MACAddress = d.MACAddresses[0]
	}
}

// SortDevices orders devices by IPv4 address (numerically), then hostname, then
// dedup identity, for stable, human-friendly output.
func SortDevices(devices []Device) {
	sort.SliceStable(devices, func(i, j int) bool {
		li, lj := ipv4SortKey(devices[i].IPAddress), ipv4SortKey(devices[j].IPAddress)
		if li != lj {
			return li < lj
		}
		if devices[i].Hostname != devices[j].Hostname {
			return devices[i].Hostname < devices[j].Hostname
		}
		return devices[i].DedupID < devices[j].DedupID
	})
}

func preferString(dst *string, src string) {
	if *dst == "" && src != "" {
		*dst = src
	}
}

func mergeUnique(existing []string, incoming []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	result := make([]string, 0, len(existing)+len(incoming))
	for _, values := range [][]string{existing, incoming} {
		for _, v := range values {
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			result = append(result, v)
		}
	}
	sortIPv4Aware(result)
	return result
}

// sortIPv4Aware sorts a slice numerically when the entries are IPv4 addresses,
// and lexically otherwise (so MAC lists stay stable too).
func sortIPv4Aware(values []string) {
	sort.SliceStable(values, func(i, j int) bool {
		ki, kj := ipv4SortKey(values[i]), ipv4SortKey(values[j])
		if ki != 0 || kj != 0 {
			if ki != kj {
				return ki < kj
			}
		}
		return values[i] < values[j]
	})
}

// ipv4SortKey converts a dotted-quad string into a comparable integer, or 0 if
// it is not an IPv4 address.
func ipv4SortKey(addr string) uint32 {
	parts := strings.Split(addr, ".")
	if len(parts) != 4 {
		return 0
	}
	var key uint32
	for _, part := range parts {
		if part == "" || len(part) > 3 {
			return 0
		}
		octet := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				return 0
			}
			octet = octet*10 + int(r-'0')
		}
		if octet > 255 {
			return 0
		}
		key = key<<8 | uint32(octet)
	}
	return key
}
