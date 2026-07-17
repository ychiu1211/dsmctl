// Package findhost implements the Synology "findhost" LAN discovery protocol as
// a read-only client: it builds the broadcast query that DSM's findhostd (and
// Synology Assistant) accept, and parses the response datagrams into the stable
// discovery.Device model.
//
// Wire format (unencrypted):
//
//	8-byte header: 12 34 56 78 'S' 'Y' 'N' 'O'
//	then a sequence of TLVs: [id:1][len:1][value:len]
//
// Endianness is mixed, and this is the subtle part of the protocol. On the
// little-endian x86 hosts DSM and Synology Assistant run on, integer TLVs are
// written with a raw memcpy, i.e. little endian; but IP-address TLVs are stored
// in network byte order (the sender copies s_addr, which is already big endian).
// This codec therefore decodes IP-family TLVs as network-order dotted quads and
// every other integer TLV as little endian, independent of the host running
// dsmctl. See juniorinstaller/util_fhost.c (FITxx/PGETxx macros) and
// memtest86plus/system/net/syno_net_progress.c for the reference behavior.
//
// The encrypted transport (header 12 34 55 66, libsodium crypto_box_seal) is
// recognized and skipped, never decoded: dsmctl sends no public key, so
// findhostd answers in plaintext.
package findhost

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
)

// BroadcastPort is the UDP port findhostd binds and answers on. A discovery
// query is sent to this port, and responses arrive on it.
const BroadcastPort = 9999

// findHostVersion is the protocol version dsmctl advertises in a query. It
// matches FINDHOSTD_VERSION (1.2.0.0) used across juniorinstaller, lnxfhd, and
// libfindhost; findhostd rejects a query that omits the version TLV.
const findHostVersion uint32 = 0x01020000

// Unencrypted and encrypted 8-byte packet headers.
var (
	headerPlain     = []byte{0x12, 0x34, 0x56, 0x78, 'S', 'Y', 'N', 'O'}
	headerEncrypted = []byte{0x12, 0x34, 0x55, 0x66, 'S', 'Y', 'N', 'O'}
)

const headerSize = 8

// findhost packet types (PKT_ID_PACKET_TYPE values), from libfindhost
// FHOST_PKT_PTYPE. Only the query we send and the responses we parse are named.
const (
	ptypeBroadcastQuery           uint32 = 1
	ptypeBroadcastResponse        uint32 = 2
	ptypeBroadcastJuniorResponse  uint32 = 6
	ptypeBroadcastRecoverResponse uint32 = 16
	ptypeInfoAvailable            uint32 = 18
	ptypeBroadcastQueryV2         uint32 = 19
	ptypeBroadcastResponseV2      uint32 = 20
	ptypeBroadcastJuniorRespV2    uint32 = 21
)

// findhost TLV field IDs (FHOST_PKT_ID), limited to the ids dsmctl reads.
const (
	idPacketType    uint8 = 0x01
	idName          uint8 = 0x11
	idIP            uint8 = 0x12
	idNetmask       uint8 = 0x13
	idGateway       uint8 = 0x14
	idDNS           uint8 = 0x15
	idDNS2          uint8 = 0x16
	idMAC           uint8 = 0x19
	idRemoteIP      uint8 = 0x1e
	idModelEnum     uint8 = 0x48
	idBuildNumber   uint8 = 0x49
	idSupportRAID   uint8 = 0x71
	idSerial        uint8 = 0x73
	idDSMVersion    uint8 = 0x77
	idDSMModel      uint8 = 0x78
	idFirstNICMAC   uint8 = 0xa2
	idFindHostVer   uint8 = 0xa4
	idQuickConfDone uint8 = 0xa7
	idCritical      uint8 = 0x90
	idNewSerial     uint8 = 0xc0
	idOSName        uint8 = 0xc1
)

// findhost quick-config status codes (FHOSTQC_STATUS).
const (
	qcFalse         uint32 = 0
	qcDone          uint32 = 1
	qcNotInstall    uint32 = 2
	qcUpdating      uint32 = 3
	qcSysCrash      uint32 = 4
	qcBooting       uint32 = 5
	qcQuotaChecking uint32 = 6
	qcServiceStart  uint32 = 7
	qcNetSet        uint32 = 8
	qcMemTesting    uint32 = 9
	qcNetTesting    uint32 = 10
	qcSysRecover    uint32 = 11
	qcOffline       uint32 = 12
	qcInstalling    uint32 = 13
	qcSysMigrat     uint32 = 14
)

// maxTLVs bounds parsing, mirroring findhostd's own 64-TLV anti-abuse guard.
const maxTLVs = 64

// Skip sentinels. ParseResponse returns one of these for a datagram that is not
// a device answer we can use; the prober treats them as "ignore, keep reading"
// rather than hard failures.
var (
	// ErrNotFindhost is returned for a datagram without the plaintext header.
	ErrNotFindhost = errors.New("not a findhost packet")
	// ErrEncryptedPacket is returned for the encrypted findhost variant, which
	// dsmctl does not decode.
	ErrEncryptedPacket = errors.New("encrypted findhost packet ignored")
	// ErrQueryPacket is returned for a query packet (another client's or our own
	// broadcast reflected back), which is not a device answer.
	ErrQueryPacket = errors.New("findhost query packet ignored")
	// ErrUnhandledType is returned for a well-formed findhost packet whose type
	// is not a discovery response.
	ErrUnhandledType = errors.New("findhost packet type is not a discovery response")
)

// BuildQuery returns the bytes of a findhost broadcast discovery query. The
// query carries the two TLVs findhostd requires (PKT_MASK_REQ_QUERY): the
// packet type and the protocol version.
func BuildQuery() []byte {
	buf := make([]byte, 0, headerSize+12)
	buf = append(buf, headerPlain...)
	buf = appendU32TLV(buf, idPacketType, ptypeBroadcastQuery)
	buf = appendU32TLV(buf, idFindHostVer, findHostVersion)
	return buf
}

// ParseResponse decodes one findhost response datagram into a discovery.Device.
// It returns a skip sentinel (see the Err* values) for datagrams that are not
// usable device answers.
func ParseResponse(payload []byte) (discovery.Device, error) {
	if len(payload) >= headerSize && bytesEqual(payload[:4], headerEncrypted[:4]) && bytesEqual(payload[4:headerSize], headerEncrypted[4:headerSize]) {
		return discovery.Device{}, ErrEncryptedPacket
	}
	if len(payload) < headerSize || !bytesEqual(payload[:headerSize], headerPlain) {
		return discovery.Device{}, ErrNotFindhost
	}

	fields, err := scanTLVs(payload[headerSize:])
	if err != nil {
		return discovery.Device{}, err
	}

	packetTypeBytes, ok := fields[idPacketType]
	if !ok {
		// findhostd always tags a packet with its type; without one we cannot
		// tell a response from a query, so treat it as not-a-findhost answer.
		return discovery.Device{}, ErrNotFindhost
	}
	packetType, ok := decodeU32LE(packetTypeBytes)
	if !ok {
		return discovery.Device{}, fmt.Errorf("findhost: malformed packet-type field")
	}

	kind, ok := responseKind(packetType)
	if !ok {
		if packetType == ptypeBroadcastQuery || packetType == ptypeBroadcastQueryV2 {
			return discovery.Device{}, ErrQueryPacket
		}
		return discovery.Device{}, ErrUnhandledType
	}

	return buildDevice(kind, fields), nil
}

// responseKind maps a findhost packet type to a stable discovery kind, folding
// the "_V2" variants onto their base kind.
func responseKind(packetType uint32) (string, bool) {
	switch packetType {
	case ptypeBroadcastResponse, ptypeBroadcastResponseV2:
		return discovery.KindResponse, true
	case ptypeBroadcastJuniorResponse, ptypeBroadcastJuniorRespV2:
		return discovery.KindJuniorResponse, true
	case ptypeBroadcastRecoverResponse:
		return discovery.KindRecoverResponse, true
	case ptypeInfoAvailable:
		return discovery.KindInfoAvailable, true
	default:
		return "", false
	}
}

func buildDevice(kind string, fields map[uint8][]byte) discovery.Device {
	device := discovery.Device{ResponseKind: kind}

	device.Hostname = trimString(fields[idName])
	device.Model = trimString(fields[idDSMModel])
	device.OS = trimString(fields[idOSName])

	respondingMAC := normalizeMAC(trimString(fields[idMAC]))
	firstNICMAC := normalizeMAC(trimString(fields[idFirstNICMAC]))
	if firstNICMAC != "" {
		device.MACAddress = firstNICMAC
	} else {
		device.MACAddress = respondingMAC
	}
	if respondingMAC != "" {
		device.MACAddresses = []string{respondingMAC}
	}

	if ip := parseIPv4(fields[idIP]); ip != "" {
		device.IPAddress = ip
		device.IPv4Addresses = []string{ip}
	}
	device.Netmask = parseIPv4(fields[idNetmask])
	device.Gateway = parseIPv4(fields[idGateway])
	device.DNS = parseIPv4(fields[idDNS])

	if serial := trimString(fields[idNewSerial]); serial != "" {
		device.Serial = serial
	} else {
		device.Serial = trimString(fields[idSerial])
	}

	if build, ok := decodeU32LE(fields[idBuildNumber]); ok {
		device.BuildNumber = int(build)
	}
	if update, ok := decodeU32LE(fields[idCritical]); ok {
		device.UpdateNumber = int(update)
	}
	if raid, ok := decodeU32LE(fields[idSupportRAID]); ok {
		device.SupportsRAID = raid != 0
	}
	device.OSVersion = formatOSVersion(trimString(fields[idDSMVersion]), device.BuildNumber, device.UpdateNumber)

	if quickConf, ok := decodeU32LE(fields[idQuickConfDone]); ok {
		device.State = stateFromQuickConf(quickConf)
	} else {
		device.State = stateFromKind(kind)
	}

	device.DedupID = dedupID(device.Serial, firstNICMAC, respondingMAC, device.IPAddress)
	return device
}

// scanTLVs walks the TLV region after the header, returning the last value seen
// for each id. It enforces byte bounds and the findhostd 64-TLV guard.
func scanTLVs(body []byte) (map[uint8][]byte, error) {
	fields := make(map[uint8][]byte)
	offset := 0
	for count := 0; offset < len(body); count++ {
		if count >= maxTLVs {
			return nil, fmt.Errorf("findhost: too many fields (>%d)", maxTLVs)
		}
		if offset+2 > len(body) {
			return nil, fmt.Errorf("findhost: truncated TLV header at offset %d", offset)
		}
		id := body[offset]
		length := int(body[offset+1])
		valueStart := offset + 2
		if valueStart+length > len(body) {
			return nil, fmt.Errorf("findhost: TLV %#x length %d overruns packet", id, length)
		}
		value := make([]byte, length)
		copy(value, body[valueStart:valueStart+length])
		fields[id] = value
		offset = valueStart + length
	}
	return fields, nil
}

func stateFromQuickConf(code uint32) string {
	switch code {
	case qcFalse, qcDone:
		return discovery.StateReady
	case qcNotInstall:
		return discovery.StateNotInstalled
	case qcUpdating, qcInstalling:
		return discovery.StateInstalling
	case qcSysCrash:
		return discovery.StateCrashed
	case qcBooting:
		return discovery.StateBooting
	case qcQuotaChecking, qcServiceStart:
		return discovery.StateStarting
	case qcNetSet:
		return discovery.StateConfiguringNetwork
	case qcMemTesting:
		return discovery.StateMemoryTesting
	case qcNetTesting:
		return discovery.StateNetworkTesting
	case qcSysRecover:
		return discovery.StateRecoverable
	case qcOffline:
		return discovery.StateOffline
	case qcSysMigrat:
		return discovery.StateMigratable
	default:
		return discovery.StateUnknown
	}
}

func stateFromKind(kind string) string {
	switch kind {
	case discovery.KindResponse:
		return discovery.StateReady
	case discovery.KindJuniorResponse:
		return discovery.StateNotInstalled
	case discovery.KindRecoverResponse:
		return discovery.StateRecoverable
	default:
		return discovery.StateUnknown
	}
}

// formatOSVersion combines the version string with the build and update numbers
// the way Synology Assistant presents them, e.g. "7.3.2-81180 Update 3".
func formatOSVersion(version string, build, update int) string {
	result := version
	if build > 0 {
		if result == "" {
			result = fmt.Sprintf("%d", build)
		} else {
			result = fmt.Sprintf("%s-%d", result, build)
		}
	}
	if update > 0 && result != "" {
		result = fmt.Sprintf("%s Update %d", result, update)
	}
	return result
}

// dedupID picks the most stable identity available to fold multiple interface
// responses from one physical device into a single entry.
func dedupID(serial, firstNICMAC, respondingMAC, ip string) string {
	switch {
	case serial != "":
		return "serial:" + strings.ToLower(serial)
	case firstNICMAC != "":
		return "mac:" + firstNICMAC
	case respondingMAC != "":
		return "mac:" + respondingMAC
	default:
		return "ip:" + ip
	}
}

func appendU32TLV(buf []byte, id uint8, value uint32) []byte {
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], value)
	buf = append(buf, id, 4)
	return append(buf, encoded[:]...)
}

func decodeU32LE(value []byte) (uint32, bool) {
	if len(value) != 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(value), true
}

// parseIPv4 decodes a 4-byte network-order IP-address TLV into a dotted quad,
// returning "" for a missing or all-zero (unset) address.
func parseIPv4(value []byte) string {
	if len(value) != 4 {
		return ""
	}
	if value[0] == 0 && value[1] == 0 && value[2] == 0 && value[3] == 0 {
		return ""
	}
	return net.IPv4(value[0], value[1], value[2], value[3]).String()
}

// trimString drops trailing NUL padding and surrounding whitespace from a
// string TLV value.
func trimString(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	trimmed := strings.TrimRight(string(value), "\x00")
	return strings.TrimSpace(trimmed)
}

// normalizeMAC lowercases a colon-separated MAC and rejects an all-zero MAC.
func normalizeMAC(mac string) string {
	if mac == "" {
		return ""
	}
	lower := strings.ToLower(mac)
	if lower == "00:00:00:00:00:00" {
		return ""
	}
	return lower
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
