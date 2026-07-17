package findhost

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
)

// --- TLV/packet builders shared by the findhost tests -----------------------

func tlvString(id uint8, value string) []byte {
	out := []byte{id, byte(len(value))}
	return append(out, []byte(value)...)
}

func tlvU32(id uint8, value uint32) []byte {
	encoded := make([]byte, 4)
	binary.LittleEndian.PutUint32(encoded, value)
	return append([]byte{id, 4}, encoded...)
}

func tlvIPv4(id uint8, a, b, c, d byte) []byte {
	return []byte{id, 4, a, b, c, d}
}

func responsePacket(tlvs ...[]byte) []byte {
	packet := append([]byte{}, headerPlain...)
	for _, tlv := range tlvs {
		packet = append(packet, tlv...)
	}
	return packet
}

func fullResponse() []byte {
	return responsePacket(
		tlvString(idName, "MyNAS"),
		tlvString(idMAC, "00:11:32:aa:bb:cc"),
		tlvIPv4(idIP, 192, 168, 1, 50),
		tlvIPv4(idNetmask, 255, 255, 255, 0),
		tlvIPv4(idGateway, 192, 168, 1, 1),
		tlvIPv4(idDNS, 192, 168, 1, 1),
		tlvString(idDSMModel, "DS3018xs"),
		tlvString(idDSMVersion, "7.3.2"),
		tlvU32(idBuildNumber, 81180),
		tlvU32(idCritical, 3),
		tlvString(idSerial, "1234567890"),
		tlvString(idNewSerial, "2150ABC123456"),
		tlvU32(idSupportRAID, 1),
		tlvString(idOSName, "DSM"),
		tlvU32(idQuickConfDone, qcDone),
		tlvU32(idPacketType, ptypeBroadcastResponse),
		tlvU32(idFindHostVer, findHostVersion),
	)
}

// --- tests ------------------------------------------------------------------

func TestBuildQueryBytes(t *testing.T) {
	want := []byte{
		0x12, 0x34, 0x56, 0x78, 'S', 'Y', 'N', 'O', // header
		0x01, 0x04, 0x01, 0x00, 0x00, 0x00, // PKT_ID_PACKET_TYPE = BROADCAST_QUERY (LE)
		0xa4, 0x04, 0x00, 0x00, 0x02, 0x01, // PKT_ID_FINDHOST_VERSION = 0x01020000 (LE)
	}
	got := BuildQuery()
	if !bytes.Equal(got, want) {
		t.Fatalf("BuildQuery() = % x, want % x", got, want)
	}
}

func TestBuildQueryIsAcceptedShape(t *testing.T) {
	// The query must round-trip through the parser as a query packet (not a
	// device answer), and must carry both TLVs findhostd requires.
	_, err := ParseResponse(BuildQuery())
	if !errors.Is(err, ErrQueryPacket) {
		t.Fatalf("ParseResponse(BuildQuery()) error = %v, want ErrQueryPacket", err)
	}
}

func TestParseResponseFields(t *testing.T) {
	device, err := ParseResponse(fullResponse())
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}

	checks := []struct {
		name string
		got  string
		want string
	}{
		{"Hostname", device.Hostname, "MyNAS"},
		{"Model", device.Model, "DS3018xs"},
		{"OS", device.OS, "DSM"},
		{"OSVersion", device.OSVersion, "7.3.2-81180 Update 3"},
		{"Serial", device.Serial, "2150ABC123456"}, // NEW_SERIAL preferred over SERIAL
		{"MACAddress", device.MACAddress, "00:11:32:aa:bb:cc"},
		{"IPAddress", device.IPAddress, "192.168.1.50"}, // network-order IP as dotted quad
		{"Netmask", device.Netmask, "255.255.255.0"},
		{"Gateway", device.Gateway, "192.168.1.1"},
		{"DNS", device.DNS, "192.168.1.1"},
		{"State", device.State, discovery.StateReady},
		{"ResponseKind", device.ResponseKind, discovery.KindResponse},
		{"DedupID", device.DedupID, "serial:2150abc123456"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if device.BuildNumber != 81180 { // little-endian integer TLV
		t.Errorf("BuildNumber = %d, want 81180", device.BuildNumber)
	}
	if device.UpdateNumber != 3 {
		t.Errorf("UpdateNumber = %d, want 3", device.UpdateNumber)
	}
	if !device.SupportsRAID {
		t.Errorf("SupportsRAID = false, want true")
	}
	if len(device.IPv4Addresses) != 1 || device.IPv4Addresses[0] != "192.168.1.50" {
		t.Errorf("IPv4Addresses = %v, want [192.168.1.50]", device.IPv4Addresses)
	}
	if len(device.MACAddresses) != 1 || device.MACAddresses[0] != "00:11:32:aa:bb:cc" {
		t.Errorf("MACAddresses = %v, want [00:11:32:aa:bb:cc]", device.MACAddresses)
	}
}

func TestParseResponseFirstNICMACPreferred(t *testing.T) {
	packet := responsePacket(
		tlvString(idMAC, "00:11:32:aa:bb:cc"),
		tlvString(idFirstNICMAC, "00:11:32:00:00:01"),
		tlvU32(idPacketType, ptypeBroadcastResponse),
	)
	device, err := ParseResponse(packet)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if device.MACAddress != "00:11:32:00:00:01" {
		t.Errorf("MACAddress = %q, want first NIC MAC 00:11:32:00:00:01", device.MACAddress)
	}
	// The responding-interface MAC is still recorded in the observed set.
	if len(device.MACAddresses) != 1 || device.MACAddresses[0] != "00:11:32:aa:bb:cc" {
		t.Errorf("MACAddresses = %v, want [00:11:32:aa:bb:cc]", device.MACAddresses)
	}
	// No serial and no first-NIC MAC fallback path yet: dedup keys on first NIC.
	if device.DedupID != "mac:00:11:32:00:00:01" {
		t.Errorf("DedupID = %q, want mac:00:11:32:00:00:01", device.DedupID)
	}
}

func TestParseResponseZeroIPOmitted(t *testing.T) {
	packet := responsePacket(
		tlvString(idMAC, "00:11:32:aa:bb:cc"),
		tlvIPv4(idIP, 0, 0, 0, 0),
		tlvU32(idPacketType, ptypeBroadcastResponse),
	)
	device, err := ParseResponse(packet)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if device.IPAddress != "" {
		t.Errorf("IPAddress = %q, want empty for 0.0.0.0", device.IPAddress)
	}
	if len(device.IPv4Addresses) != 0 {
		t.Errorf("IPv4Addresses = %v, want empty", device.IPv4Addresses)
	}
}

func TestParseResponseStateFromQuickConf(t *testing.T) {
	cases := map[uint32]string{
		qcNotInstall: discovery.StateNotInstalled,
		qcSysMigrat:  discovery.StateMigratable,
		qcMemTesting: discovery.StateMemoryTesting,
		qcBooting:    discovery.StateBooting,
	}
	for code, want := range cases {
		packet := responsePacket(
			tlvString(idMAC, "00:11:32:aa:bb:cc"),
			tlvU32(idQuickConfDone, code),
			tlvU32(idPacketType, ptypeBroadcastJuniorResponse),
		)
		device, err := ParseResponse(packet)
		if err != nil {
			t.Fatalf("ParseResponse(qc=%d) error = %v", code, err)
		}
		if device.State != want {
			t.Errorf("quick-config %d => State %q, want %q", code, device.State, want)
		}
	}
}

func TestParseResponseStateFallsBackToKind(t *testing.T) {
	// A junior response with no quick-config TLV falls back to the packet kind.
	packet := responsePacket(
		tlvString(idMAC, "00:11:32:aa:bb:cc"),
		tlvU32(idPacketType, ptypeBroadcastJuniorResponse),
	)
	device, err := ParseResponse(packet)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if device.State != discovery.StateNotInstalled {
		t.Errorf("State = %q, want %q", device.State, discovery.StateNotInstalled)
	}
	if device.ResponseKind != discovery.KindJuniorResponse {
		t.Errorf("ResponseKind = %q, want %q", device.ResponseKind, discovery.KindJuniorResponse)
	}
}

func TestParseResponseV2KindsFold(t *testing.T) {
	packet := responsePacket(
		tlvString(idMAC, "00:11:32:aa:bb:cc"),
		tlvU32(idPacketType, ptypeBroadcastResponseV2),
	)
	device, err := ParseResponse(packet)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if device.ResponseKind != discovery.KindResponse {
		t.Errorf("ResponseKind = %q, want %q (V2 folds onto base)", device.ResponseKind, discovery.KindResponse)
	}
}

func TestParseResponseSkips(t *testing.T) {
	encrypted := append([]byte{0x12, 0x34, 0x55, 0x66, 'S', 'Y', 'N', 'O'}, tlvU32(idPacketType, ptypeBroadcastResponse)...)
	cases := []struct {
		name    string
		payload []byte
		want    error
	}{
		{"query", responsePacket(tlvU32(idPacketType, ptypeBroadcastQuery), tlvU32(idFindHostVer, findHostVersion)), ErrQueryPacket},
		{"query_v2", responsePacket(tlvU32(idPacketType, ptypeBroadcastQueryV2)), ErrQueryPacket},
		{"encrypted", encrypted, ErrEncryptedPacket},
		{"not_findhost", []byte("hello world not synology"), ErrNotFindhost},
		{"short", []byte{0x12, 0x34}, ErrNotFindhost},
		{"missing_type", responsePacket(tlvString(idName, "x")), ErrNotFindhost},
		{"unhandled_type", responsePacket(tlvU32(idPacketType, 3 /* NETSETTING */)), ErrUnhandledType},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseResponse(c.payload)
			if !errors.Is(err, c.want) {
				t.Fatalf("ParseResponse() error = %v, want %v", err, c.want)
			}
		})
	}
}

func TestParseResponseBounds(t *testing.T) {
	t.Run("truncated_tlv_header", func(t *testing.T) {
		packet := append(append([]byte{}, headerPlain...), idName) // id with no length byte
		if _, err := ParseResponse(packet); err == nil {
			t.Fatal("expected error for truncated TLV header")
		}
	})
	t.Run("length_overruns", func(t *testing.T) {
		packet := append(append([]byte{}, headerPlain...), idName, 200, 'a', 'b') // claims 200 bytes
		if _, err := ParseResponse(packet); err == nil {
			t.Fatal("expected error for TLV length overrun")
		}
	})
	t.Run("too_many_tlvs", func(t *testing.T) {
		packet := append([]byte{}, headerPlain...)
		for i := 0; i < maxTLVs+5; i++ {
			packet = append(packet, tlvU32(0xa3 /* OUTIF_INDEX, harmless padding */, 0)...)
		}
		if _, err := ParseResponse(packet); err == nil {
			t.Fatal("expected error for exceeding the TLV guard")
		}
	})
}
