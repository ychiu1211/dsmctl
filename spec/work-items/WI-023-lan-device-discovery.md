---
id: WI-023
title: LAN device discovery (findhost broadcast)
status: done
priority: P2
owner: ""
depends_on: []
parallel_group: H
touches:
  - internal/domain/discovery/model.go
  - internal/synology/findhost/codec.go
  - internal/synology/findhost/prober.go
  - internal/synology/findhost/sockopt_unix.go
  - internal/synology/findhost/sockopt_windows.go
  - internal/application/discovery.go
  - internal/cli/discover.go
  - internal/cli/root.go
  - internal/mcpserver/server.go
  - internal/mcpserver/read_only.go
  - docs/lan-discovery.md
---

# WI-023 — LAN device discovery (findhost broadcast)

## Outcome

A CLI or local MCP user can discover Synology devices on the local network
without any configured NAS profile, credential, or DSM session. dsmctl speaks
the same UDP broadcast "findhost" protocol that Synology Assistant and DSM's
`findhostd` use, sends a discovery query on the LAN, and returns a normalized
list of the devices that answer: hostname, model, OS version, serial, MAC and
IPv4 address(es), and self-reported install/quick-config state.

## Scope

- A read-only findhost protocol codec (`internal/synology/findhost`) that:
  - builds a `PKT_ID_PTYPE_BROADCAST_QUERY` datagram (8-byte
    `12 34 56 78 'S' 'Y' 'N' 'O'` header + `PKT_ID_PACKET_TYPE` and
    `PKT_ID_FINDHOST_VERSION` TLVs — both required for `findhostd` to accept a
    query, per `FHOSTReadPktToNas` `PKT_MASK_REQ_QUERY`);
  - parses response datagrams (`BROADCAST_RESPONSE`, `_JUNIOR_RESPONSE`,
    `_RECOVER_RESPONSE`, `INFO_AVAILABLE`, and their `_V2` forms) into the
    stable `discovery.Device` model, honoring the protocol's mixed endianness
    (IP-family TLVs are network byte order; all other u32 TLVs are little
    endian) and the 64-TLV parse guard;
  - skips (does not error on) query packets and unrelated/malformed datagrams,
    including the encrypted variant (`12 34 55 66 ...`), which is not decoded.
- A UDP prober that binds `:9999` (`SO_REUSEADDR` + `SO_BROADCAST`), sends the
  query to the limited broadcast address and every up interface's directed
  IPv4 broadcast, listens for a bounded window, and deduplicates answers by
  serial (falling back to first-NIC MAC, then responding MAC).
- Application method `Service.DiscoverDevices` and MCP tool
  `discover_lan_devices` (no NAS input), plus a `dsmctl discover` CLI command
  with text and `--json` output and a `--timeout` flag.

## Non-goals

- Any device mutation: quick-config / network-setting / install packet types
  (`PKT_ID_PTYPE_NETSETTING`, `QUICKCONF`, `GROUP_INSTALL`) are never sent.
- The encrypted findhost transport (libsodium `crypto_box_seal`, header
  `12 34 55 66`). dsmctl sends no public key, so `findhostd` answers in
  plaintext; encrypted datagrams from other clients are ignored, not decoded.
- TCP findhost, share/DRAuth enumeration, printer/USB/CMS fields, and
  service-bitmap decoding beyond RAID support.
- Exposing discovery over the remote gateway surface (see design constraints).

## Design constraints

- Discovery is intentionally outside the per-operation DSM WebAPI contract in
  `architecture-contracts.md`: it uses no session manager, no WebAPI executor,
  no per-NAS compatibility routing, and no plan/apply, because it is a
  connectionless, unauthenticated, NAS-independent, read-only LAN probe. This
  is an approved, documented carve-out for this item only; the WebAPI contract
  still governs every DSM operation. Because there is no DSM API/version or
  session, discovery does not appear in the per-NAS compatibility report.
- CLI and MCP remain thin adapters: both call `Service.DiscoverDevices`, which
  owns the behavior and delegates to the `findhost` package. The `findhost`
  package knows nothing about Cobra, MCP, config files, or prompts.
- The codec normalizes to stable semantic field names, not raw findhost packet
  IDs, and returns typed skip/parse outcomes rather than silently producing an
  empty device (per the decoder contract).
- Socket options are set through platform-split `sockopt_unix.go` /
  `sockopt_windows.go` files so the prober builds on the amd64 Linux gateway
  target and the Windows developer host.
- LAN discovery is removed from the remote read-only gateway surface
  (`read_only.go` `RemoveTools`): a remote caller must not be able to trigger a
  broadcast scan of, or enumerate, the gateway host's local network. It remains
  available on the local stdio MCP server and the CLI.

## Acceptance criteria

- [x] `dsmctl discover` sends a findhost broadcast query and lists answering
      Synology devices (text and `--json`), with a configurable `--timeout`.
- [x] The codec builds a query `findhostd` accepts and parses a captured
      response fixture into the expected `discovery.Device` fields, with IP
      fields decoded as dotted-quad network order and integer fields as
      little endian. Round-trip and malformed/short/oversized-TLV inputs are
      covered by unit tests.
- [x] Query and encrypted-header datagrams are skipped without error; the
      64-TLV guard bounds parsing.
- [x] The prober deduplicates a multi-NIC device into one entry that collects
      all observed IPv4 addresses.
- [x] MCP `discover_lan_devices` is registered read-only and is absent from the
      remote gateway surface (asserted in `read_only_test.go`).
- [x] `go test ./...` and `go vet ./...` pass.

## Verification

- `go test ./...` and `go vet ./...` pass. Unit coverage in
  `internal/synology/findhost`: query bytes are golden-locked
  (`TestBuildQueryBytes`), response parsing uses synthesized captured-shape
  packets (`TestParseResponseFields`) asserting network-order IP decode and
  little-endian integer decode, serial preference (NEW_SERIAL over SERIAL),
  first-NIC-MAC preference, quick-config and packet-type state mapping, `_V2`
  folding, and the skip/bounds cases (query, encrypted, non-findhost, short,
  missing type, unhandled type, truncated/overrun TLV, 64-TLV guard). Prober
  dedup/merge, info-available upgrade, non-device rejection, and broadcast-target
  enumeration are unit-tested without sockets via `foldResponseInto`. MCP
  registration is asserted by `TestNewExposesLANDiscovery` and the gateway
  exclusion by `TestNewReadOnlyOmitsPlanAndApplyTools`.
- Live end-to-end on the office LAN (read-only; sent only query packets, no NAS
  profile or credential): `go run ./cmd/dsmctl discover` discovered 171
  Synology devices across DSM 6.2.4 → 8.0, SRM (RT6600ax/RT1900ac), VirtualDSM,
  BeeStation, and DP340, with correct hostname/model/OS-version/IP/MAC/serial.
  Multi-homed and HA units (e.g. a DS3622xs+ with four addresses) folded into a
  single entry carrying every IPv4 address. `--json` confirmed the full model
  including netmask/gateway/DNS. Note: a sweep launched in the same second as a
  prior sweep can return empty while the OS releases port 9999; re-running
  succeeds.
- Reference sources (Synology internal): `libfindhost` `include/findhost/findhost.h`
  (packet-type and TLV-ID enums, incl. the `_V2` types on master),
  `juniorinstaller` `util_fhost.c` (`FHOSTReadPktToNas` / `FITxx` build+parse
  macros, header bytes, ports), `lnxfhd` `util_fhost.c` (installed-system
  `BROADCAST_RESPONSE`), and `memtest86plus` `system/net/syno_net_progress.c`
  (self-contained build/parse reference incl. the IP-vs-integer endianness note).

## Coordination

New `internal/domain/discovery` and `internal/synology/findhost` packages are
the parallel boundary. Additive edits to the high-contention files
`internal/mcpserver/server.go`, `internal/mcpserver/read_only.go`, and
`internal/cli/root.go`; new `internal/application/discovery.go` and
`internal/cli/discover.go`. `internal/application/service.go` is not modified
(the result type and method live in `discovery.go`). Coordinate before another
active item edits those shared files.

## Handoff

Not paused.
