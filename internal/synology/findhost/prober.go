package findhost

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
)

// initialBursts is when, relative to the start of a sweep, the query is first
// broadcast in quick succession. The early repeats answer fast on a healthy LAN
// and counter startup UDP loss; findhostd likewise answers more than once.
var initialBursts = []time.Duration{0, 200 * time.Millisecond, 700 * time.Millisecond}

// rebroadcastInterval is how often the query keeps being re-sent for the rest of
// the sweep, after the initial bursts. Re-broadcasting throughout the window —
// not just at the start — is what lets a sweep converge on the *complete* device
// set even when another findhost listener on the same host takes a share of each
// response burst. The clearest case is Synology Assistant, which holds UDP 9999
// continuously: on Windows an incoming response is delivered to only one of the
// two sockets bound to the shared port, so any single round can lose many
// devices to Assistant. Every device answers every re-broadcast, so successive
// rounds recover whatever a given round missed (and catch devices that power on
// partway through the sweep).
const rebroadcastInterval = 1 * time.Second

// ProbeOptions carries optional, non-serializable knobs for a sweep.
type ProbeOptions struct {
	// OnDevice, when set, is called once for each newly discovered device as it
	// is first folded into the running set, letting a caller stream progress
	// during a long sweep. It runs on the read loop and must not block. Later
	// responses that only add interfaces to an already-known device do not call
	// it.
	OnDevice func(discovery.Device)
}

// Probe runs one findhost broadcast discovery sweep and returns the Synology
// devices that answered within the query's timeout, deduplicated by device
// identity and each carrying every IPv4 address it answered from.
//
// It re-broadcasts the query throughout the sweep so the result converges on the
// full device set even under contention for UDP 9999 (see rebroadcastInterval).
// If the context is cancelled — e.g. the user presses Ctrl-C — Probe stops early
// and returns the devices collected so far rather than an error.
//
// Probe is read-only: it transmits only discovery query packets and mutates
// nothing. It needs no NAS profile, credential, or DSM session.
func Probe(ctx context.Context, query discovery.Query, opts ProbeOptions) ([]discovery.Device, error) {
	query = query.Normalize()

	listenConfig := net.ListenConfig{
		Control: func(_, _ string, rawConn syscall.RawConn) error {
			return setSocketOptions(rawConn)
		},
	}
	packetConn, err := listenConfig.ListenPacket(ctx, "udp4", fmt.Sprintf(":%d", BroadcastPort))
	if err != nil {
		return nil, fmt.Errorf("bind udp port %d for LAN discovery (another findhost client may be running): %w", BroadcastPort, err)
	}
	conn, ok := packetConn.(*net.UDPConn)
	if !ok {
		_ = packetConn.Close()
		return nil, fmt.Errorf("discovery: unexpected packet connection type %T", packetConn)
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(query.Timeout)); err != nil {
		return nil, fmt.Errorf("discovery: set read deadline: %w", err)
	}

	// Unblock the read loop promptly if the caller cancels (e.g. Ctrl-C).
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.SetReadDeadline(time.Now())
		case <-done:
		}
	}()

	targets := broadcastTargets()
	queryBytes := BuildQuery()
	go sendQuery(ctx, conn, queryBytes, targets, done)

	collected := make(map[string]*discovery.Device)
	buf := make([]byte, 2048)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			// A timeout (deadline reached or cancellation) ends the sweep
			// normally; any other read error also ends it, returning what we
			// have so far.
			break
		}
		if device, isNew := foldResponseInto(collected, buf[:n]); isNew && opts.OnDevice != nil {
			opts.OnDevice(device)
		}
	}

	// A cancelled sweep (Ctrl-C) returns what it has, not an error: partial
	// discovery is still useful, and the caller can tell it was cut short from
	// its own context.
	return sortedDevices(collected), nil
}

// foldResponseInto parses one datagram and folds a usable device answer into
// collected, keyed by device identity so a multi-homed device becomes a single
// entry. It returns the folded device and whether it was newly added (rather
// than merging more interfaces into an already-known device). Non-device
// datagrams (queries, encrypted, malformed) are ignored and reported as not new.
func foldResponseInto(collected map[string]*discovery.Device, payload []byte) (discovery.Device, bool) {
	device, err := ParseResponse(payload)
	if err != nil {
		return discovery.Device{}, false
	}
	if existing, ok := collected[device.DedupID]; ok {
		existing.Merge(device)
		return *existing, false
	}
	copied := device
	collected[device.DedupID] = &copied
	return copied, true
}

// sortedDevices flattens the collected map into a stably ordered slice.
func sortedDevices(collected map[string]*discovery.Device) []discovery.Device {
	devices := make([]discovery.Device, 0, len(collected))
	for _, device := range collected {
		devices = append(devices, *device)
	}
	discovery.SortDevices(devices)
	return devices
}

// sendQuery broadcasts the query to every target: a few quick initial bursts,
// then a steady re-broadcast every rebroadcastInterval until the sweep ends
// (read deadline reached, context cancelled, or Probe returning).
func sendQuery(ctx context.Context, conn *net.UDPConn, queryBytes []byte, targets []*net.UDPAddr, done <-chan struct{}) {
	broadcast := func() {
		for _, target := range targets {
			_, _ = conn.WriteToUDP(queryBytes, target)
		}
	}

	start := time.Now()
	for _, offset := range initialBursts {
		if !waitUntil(ctx, done, start.Add(offset)) {
			return
		}
		broadcast()
	}

	ticker := time.NewTicker(rebroadcastInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			broadcast()
		}
	}
}

// waitUntil blocks until deadline, returning false if the sweep ended first
// (context cancelled or Probe returned).
func waitUntil(ctx context.Context, done <-chan struct{}, deadline time.Time) bool {
	wait := time.Until(deadline)
	if wait <= 0 {
		return true
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-done:
		return false
	case <-timer.C:
		return true
	}
}

// broadcastTargets returns the limited broadcast address plus every up,
// non-loopback interface's directed IPv4 broadcast address, so the query
// reaches every attached subnet even on a multi-homed host.
func broadcastTargets() []*net.UDPAddr {
	targets := []*net.UDPAddr{{IP: net.IPv4bcast, Port: BroadcastPort}}
	seen := map[string]struct{}{net.IPv4bcast.String(): {}}

	interfaces, err := net.Interfaces()
	if err != nil {
		return targets
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 ||
			iface.Flags&net.FlagBroadcast == 0 ||
			iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil || len(ipNet.Mask) != net.IPv4len {
				continue
			}
			broadcast := make(net.IP, net.IPv4len)
			for i := 0; i < net.IPv4len; i++ {
				broadcast[i] = ip4[i] | ^ipNet.Mask[i]
			}
			key := broadcast.String()
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			targets = append(targets, &net.UDPAddr{IP: broadcast, Port: BroadcastPort})
		}
	}
	return targets
}
