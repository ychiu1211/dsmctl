---
id: WI-027
title: Guarded FTP/FTPS and SFTP file services
status: done
priority: P2
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/ftpservices
  - internal/synology/operations/ftpservices
  - internal/synology/ftpservices.go
  - internal/application
  - internal/cli
  - internal/mcpserver/server.go
  - docs/control-panel.md
---

# WI-027 — Guarded FTP/FTPS and SFTP file services

## Outcome

A CLI user or MCP agent can read and, through hash-bound plan/apply, change the
FTP/FTPS and SFTP services: enable state, ports, TLS/security, passive-port
range, reported external IP, brute-force protection, UTF-8, anonymous access,
and transfer speed limits.

## Scope

Implemented (first slice — the service switches):

- Plain FTP and FTPS (explicit TLS) enable switches.
- SFTP (SSH file transfer) enable switch and listening port.
- Independent read/set selection and capability reporting per protocol.

Deferred to a follow-up sub-slice (the remaining `SYNO.Core.FileServ.FTP`
"Others" fields, added the same patch-only way once each value set is confirmed):
FTP port, passive-port range, reported external IP, brute-force protection,
UTF-8, timeouts, transfer-speed limits, and anonymous access
(`SYNO.Core.FileServ.FTP.Security`).

## Non-goals

- Per-user or per-group FTP privileges and speed limits.
- FTP over the DSM firewall / port-forwarding configuration.
- TFTP and rsync (WI-028).

## Design constraints

- DSM evidence gathered 2026-07-18 from `webapi-FTP` (DSM7-3), confirmed live on
  DSM 7.3.2:
  - Plain FTP and FTPS share `SYNO.Core.FileServ.FTP` get/set (v1/v2/v3; this
    module uses v3). `get` returns `enable_ftp` and `enable_ftps` as booleans
    (`src/ftp.cpp` L59-60). `set` **requires both** `enable_ftp` and
    `enable_ftps` present and boolean (`src/ftp.cpp` L158, L164-165) and is a
    partial update for every other field, so the facade always sends the merged
    pair and nothing else.
  - SFTP is `SYNO.Core.FileServ.FTP.SFTP` get/set v1. `get` returns `enable`
    (bool) and `portnum` (int) (`src/sftp.cpp` L35-36). `set` requires `enable`
    (`src/sftp.cpp` L47); `portnum` is optional but always resent to preserve it.
  - Unlike the NFS advanced set (WI-025), FTP/SFTP get and set field types are
    symmetric — no int-vs-bool re-encoding was needed.
- Each protocol is a separate compatibility boundary; SFTP is selected
  independently and reports `(not supported)` / nil when absent.
- Enabling plain FTP (unencrypted credentials) or disabling a service already in
  use is high risk; enabling FTPS or changing the SFTP port is medium.

## Acceptance criteria

- [x] FTP/FTPS and SFTP states decode with semantic fields and strict
      validation (required-field decoders reject missing `enable_ftps`/`portnum`).
- [x] Read/set support selected independently per protocol with API evidence.
- [x] Apply preserves every unspecified switch and verifies the postcondition
      (FTP always resends both switches merged from the fresh read; SFTP resends
      the port).
- [x] Request-capture tests lock each enabled set shape.
- [x] CLI (`control-panel file-services ftp`) and MCP reuse one application
      contract.
- [x] DSM 7.3.2 live verification (lab, explicitly authorized, fully reverted
      2026-07-18): enabled FTPS with plain FTP preserved off, then reverted;
      changed the SFTP port 22→2222 with SFTP preserved off, then reverted to 22.

## Verification

- Decoder fixtures and request-capture tests per protocol.
- `go test ./... -count=1`, `go vet ./...`, CLI and MCP builds.
- Read-only state/capability checks plus reverted live writes on the DSM 7.3.2
  lab NAS.

## Coordination

New operation package is an independent parallel boundary; only
`internal/mcpserver/server.go` (+ its read-only gate and tool-count test) and the
compatibility report overlap with the other file-protocol items.

## Handoff

Implemented, tested, and live-verified.

- Done: `internal/domain/ftpservices`, operation package
  `internal/synology/operations/ftpservices` (FTP + SFTP read/set with
  request-capture tests), facade `internal/synology/ftpservices.go`, application
  plan/apply `internal/application/ftp_services.go`, CLI under
  `control-panel file-services ftp`, four MCP tools + read-only gating, and
  `docs/control-panel.md`.
- Pending: the deferred advanced-FTP "Others" sub-slice above (partial set, add
  each field the same way after confirming its DSM-permitted value set), and the
  `SYNO.Core.FileServ.FTP.Security` anonymous-access surface.
