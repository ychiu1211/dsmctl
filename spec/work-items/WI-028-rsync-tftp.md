---
id: WI-028
title: Guarded rsync service and TFTP file services
status: done
priority: P3
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/rsyncservice
  - internal/domain/tftpservice
  - internal/synology/operations/rsyncservice
  - internal/synology/operations/tftpservice
  - internal/synology/rsyncservice.go
  - internal/synology/tftpservice.go
  - internal/application
  - internal/cli
  - internal/mcpserver/server.go
  - docs/control-panel.md
---

# WI-028 — Guarded rsync service and TFTP file services

## Outcome

A CLI user or MCP agent can read and, through hash-bound plan/apply, change the
rsync network-backup service (enable state, port, rsync account switch) and the
TFTP service (enable state, root folder, allowed clients).

## Scope

- rsync service global settings used by network backup.
- TFTP service global settings.
- Independent read/set selection and capability reporting per protocol.

## Non-goals

- rsync backup task or destination management.
- AFP (deprecated in DSM 7.x) and WebDAV (a separate installable package best
  handled by the WI-022 package-scoped framework, not core File Services).

## Design constraints

- API names/versions were verified in source (DSM7-3), but the exact field
  names were confirmed by live DSM 7.3.2 reads because the source doc comments
  were stale:
  - rsync is `SYNO.Backup.Service.NetworkBackup` v1 get/set (repo webapi-Rsync).
    Live get returns `enable` (bool), `enable_rsync_account` (bool), and
    `rsync_sshd_port` (string) — the same names the set uses (the source doc's
    `enable_rsync` is stale). The set applies `enable_rsync_account` only when
    present. `rsync_sshd_port` is shared with the SSH daemon, so it is read-only
    here.
  - TFTP is `SYNO.Core.TFTP` v1 get/set (repo webapi-TFTP). Live get returns the
    complete config with the same names the set uses — `enable`, `enable_log`,
    `startip`, `endip`, `permission` ("r"/"rw"), `root_path`, `timeout` — with no
    `additional` selector needed (the source doc's `enabled`/`log_enabled`/
    `ip_high`/`ip_low` and "additional" gating are stale). The set is partial.
- TFTP root folder must resolve to an existing shared-folder path; DSM rejects an
  invalid path (WEBAPI_CORE_ERR_TFTP_INVALID_ROOT_PATH). Enabling TFTP without a
  root is rejected at plan time; path validity itself is left to DSM plus the
  postcondition re-read.
- Exposing TFTP (no authentication), granting TFTP write permission, or enabling
  the rsync service/account is high risk.
- No live rsync/TFTP mutation without new explicit authorization.

## Non-goals (implemented as deferrals)

- rsync-over-SSH port write (shared with the SSH daemon) — read-only state.
- TFTP allowed-client IP-range write (`startip`/`endip` interact with an
  "allow all" flag) — read-only state.

## Acceptance criteria

- [x] rsync and TFTP states decode with semantic fields and strict validation
      (required-field decoders; permission enum; port string coercion).
- [x] Read/set support selected independently with API evidence; each protocol is
      its own compatibility boundary and fails closed on a missing API.
- [x] Apply preserves unspecified fields (rsync sends the required service anchor
      plus the merged account switch; TFTP sends only changed fields) and verifies
      the postcondition.
- [x] Request-capture tests lock each set shape, including the rsync required
      service anchor and the TFTP partial set with live field names.
- [x] CLI (`control-panel file-services rsync` / `tftp`) and MCP (eight tools)
      reuse one application contract; adversarial review workflow run before live.
- [x] DSM 7.3.2 live verification (lab, explicitly authorized, fully reverted
      2026-07-18): see Handoff.

## Verification

- Decoder fixtures and request-capture tests per protocol.
- `go test ./... -count=1`, `go vet ./...`, CLI and MCP builds.
- Read-only state/capability checks plus reverted live writes on the DSM 7.3.2
  lab NAS.

## Coordination

Lowest-priority file-protocol items; independent operation packages minimize
overlap (only `internal/mcpserver/server.go` + its read-only gate and tool-count
test, and `internal/cli/file_services.go`, are shared). AFP and WebDAV are
explicitly out of scope and recorded here so they are not re-proposed without a
product decision.

## Handoff

Implemented, tested, adversarially reviewed, and live-verified.

- Done: `internal/domain/{rsyncservice,tftpservice}`, operation packages
  `internal/synology/operations/{rsyncservice,tftpservice}` (read/set with
  request-capture tests), facades `internal/synology/{rsyncservice,tftpservice}.go`,
  application plan/apply `internal/application/{rsync_service,tftp_service}.go`,
  CLI under `control-panel file-services rsync`/`tftp`, eight MCP tools with
  read-only gating (tool count 61 -> 69), and `docs/control-panel.md`.
- Pending: rsync-over-SSH port write and TFTP allowed-client IP-range write (both
  deferred above); TFTP permission `read_write` and `root_path` writes are
  implemented but were not live-toggled (no-auth exposure), only the safe
  logging/timeout round-trip was.
