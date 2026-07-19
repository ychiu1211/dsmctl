package findhost

import (
	"net"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
)

func TestFoldResponseIntoDedupsBySerial(t *testing.T) {
	// The same physical NAS answers on two interfaces: same serial, different
	// responding MAC and IP. It must fold into one device carrying both
	// addresses.
	first := responsePacket(
		tlvString(idName, "MyNAS"),
		tlvString(idMAC, "00:11:32:aa:bb:01"),
		tlvIPv4(idIP, 192, 168, 1, 51),
		tlvString(idNewSerial, "2150ABC123456"),
		tlvU32(idPacketType, ptypeBroadcastResponse),
	)
	second := responsePacket(
		tlvString(idName, "MyNAS"),
		tlvString(idMAC, "00:11:32:aa:bb:02"),
		tlvIPv4(idIP, 192, 168, 1, 50),
		tlvString(idNewSerial, "2150ABC123456"),
		tlvU32(idPacketType, ptypeBroadcastResponse),
	)

	collected := map[string]*discovery.Device{}
	foldResponseInto(collected, first)
	foldResponseInto(collected, second)

	devices := sortedDevices(collected)
	if len(devices) != 1 {
		t.Fatalf("got %d devices, want 1 (deduped by serial)", len(devices))
	}
	device := devices[0]
	wantIPs := []string{"192.168.1.50", "192.168.1.51"}
	if len(device.IPv4Addresses) != 2 || device.IPv4Addresses[0] != wantIPs[0] || device.IPv4Addresses[1] != wantIPs[1] {
		t.Errorf("IPv4Addresses = %v, want %v (sorted)", device.IPv4Addresses, wantIPs)
	}
	if device.IPAddress != "192.168.1.50" {
		t.Errorf("IPAddress = %q, want lowest 192.168.1.50", device.IPAddress)
	}
	if len(device.MACAddresses) != 2 {
		t.Errorf("MACAddresses = %v, want two entries", device.MACAddresses)
	}
}

func TestFoldResponseInfoAvailableUpgradedByResponse(t *testing.T) {
	// A lightweight INFO_AVAILABLE hint arrives first, then the full response.
	// The merged device should reflect the richer response.
	info := responsePacket(
		tlvString(idMAC, "00:11:32:aa:bb:cc"),
		tlvIPv4(idIP, 192, 168, 1, 50),
		tlvString(idNewSerial, "2150ABC123456"),
		tlvU32(idPacketType, ptypeInfoAvailable),
	)
	full := responsePacket(
		tlvString(idName, "MyNAS"),
		tlvString(idMAC, "00:11:32:aa:bb:cc"),
		tlvIPv4(idIP, 192, 168, 1, 50),
		tlvString(idNewSerial, "2150ABC123456"),
		tlvString(idDSMModel, "DS3018xs"),
		tlvU32(idQuickConfDone, qcDone),
		tlvU32(idPacketType, ptypeBroadcastResponse),
	)

	collected := map[string]*discovery.Device{}
	foldResponseInto(collected, info)
	foldResponseInto(collected, full)

	devices := sortedDevices(collected)
	if len(devices) != 1 {
		t.Fatalf("got %d devices, want 1", len(devices))
	}
	device := devices[0]
	if device.ResponseKind != discovery.KindResponse {
		t.Errorf("ResponseKind = %q, want %q after upgrade", device.ResponseKind, discovery.KindResponse)
	}
	if device.State != discovery.StateReady {
		t.Errorf("State = %q, want %q", device.State, discovery.StateReady)
	}
	if device.Model != "DS3018xs" {
		t.Errorf("Model = %q, want DS3018xs", device.Model)
	}
}

func TestFoldResponseIntoIgnoresNonDevice(t *testing.T) {
	collected := map[string]*discovery.Device{}
	if _, isNew := foldResponseInto(collected, BuildQuery()); isNew { // our own query reflected back
		t.Error("reflected query reported as a new device")
	}
	if _, isNew := foldResponseInto(collected, []byte("garbage-packet")); isNew { // unrelated datagram
		t.Error("garbage datagram reported as a new device")
	}
	if len(collected) != 0 {
		t.Fatalf("collected %d devices, want 0", len(collected))
	}
}

func TestFoldResponseIntoReportsNewnessForStreaming(t *testing.T) {
	// The prober streams a device to OnDevice only the first time it is folded in.
	// The first response for a device is new; a second response for the same
	// physical device (another interface) merges silently.
	first := responsePacket(
		tlvString(idName, "MyNAS"),
		tlvString(idMAC, "00:11:32:aa:bb:01"),
		tlvIPv4(idIP, 192, 168, 1, 51),
		tlvString(idNewSerial, "2150ABC123456"),
		tlvU32(idPacketType, ptypeBroadcastResponse),
	)
	second := responsePacket(
		tlvString(idName, "MyNAS"),
		tlvString(idMAC, "00:11:32:aa:bb:02"),
		tlvIPv4(idIP, 192, 168, 1, 50),
		tlvString(idNewSerial, "2150ABC123456"),
		tlvU32(idPacketType, ptypeBroadcastResponse),
	)

	collected := map[string]*discovery.Device{}
	device, isNew := foldResponseInto(collected, first)
	if !isNew {
		t.Fatal("first response for a device: isNew = false, want true")
	}
	if device.Hostname != "MyNAS" {
		t.Errorf("streamed device Hostname = %q, want MyNAS", device.Hostname)
	}
	if _, isNew := foldResponseInto(collected, second); isNew {
		t.Error("second interface of a known device: isNew = true, want false")
	}
}

func TestBroadcastTargetsIncludeLimitedBroadcast(t *testing.T) {
	targets := broadcastTargets()
	if len(targets) == 0 {
		t.Fatal("broadcastTargets() returned none")
	}
	seen := map[string]int{}
	foundLimited := false
	for _, target := range targets {
		if target.Port != BroadcastPort {
			t.Errorf("target %v has port %d, want %d", target, target.Port, BroadcastPort)
		}
		if target.IP.Equal(net.IPv4bcast) {
			foundLimited = true
		}
		seen[target.IP.String()]++
	}
	if !foundLimited {
		t.Error("broadcastTargets() missing the limited broadcast 255.255.255.255")
	}
	for ip, count := range seen {
		if count > 1 {
			t.Errorf("broadcast target %s appears %d times, want deduplicated", ip, count)
		}
	}
}
