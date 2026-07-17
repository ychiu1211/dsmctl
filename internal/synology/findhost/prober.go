package findhost

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
)

// resendSchedule is when, relative to the start of a sweep, the query is
// (re)broadcast. Repeating counters UDP loss and catches devices that come up
// slightly after the sweep begins; findhostd likewise answers more than once.
var resendSchedule = []time.Duration{0, 200 * time.Millisecond, 700 * time.Millisecond}

// Probe runs one findhost broadcast discovery sweep and returns the Synology
// devices that answered within the query's timeout, deduplicated by device
// identity and each carrying every IPv4 address it answered from.
//
// Probe is read-only: it transmits only discovery query packets and mutates
// nothing. It needs no NAS profile, credential, or DSM session.
func Probe(ctx context.Context, query discovery.Query) ([]discovery.Device, error) {
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
		foldResponseInto(collected, buf[:n])
	}

	if ctx.Err() != nil && len(collected) == 0 {
		return nil, ctx.Err()
	}
	return sortedDevices(collected), nil
}

// foldResponseInto parses one datagram and folds a usable device answer into
// collected, keyed by device identity so a multi-homed device becomes a single
// entry. Non-device datagrams (queries, encrypted, malformed) are ignored.
func foldResponseInto(collected map[string]*discovery.Device, payload []byte) {
	device, err := ParseResponse(payload)
	if err != nil {
		return
	}
	if existing, ok := collected[device.DedupID]; ok {
		existing.Merge(device)
		return
	}
	copied := device
	collected[device.DedupID] = &copied
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

// sendQuery broadcasts the query to every target on the resend schedule until
// the schedule completes or the sweep ends.
func sendQuery(ctx context.Context, conn *net.UDPConn, queryBytes []byte, targets []*net.UDPAddr, done <-chan struct{}) {
	start := time.Now()
	for _, offset := range resendSchedule {
		wait := time.Until(start.Add(offset))
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-done:
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		for _, target := range targets {
			_, _ = conn.WriteToUDP(queryBytes, target)
		}
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
