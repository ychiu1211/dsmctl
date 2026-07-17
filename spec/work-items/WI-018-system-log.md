---
id: WI-018
title: System log reading
status: done
priority: P2
owner: ""
depends_on: []
parallel_group: D
touches:
  - internal/domain/syslog/model.go
  - internal/synology/operations/syslogread
  - internal/synology/syslog.go
  - internal/synology/compatibility_report.go
  - internal/runtime/manager.go
  - internal/application/service.go
  - internal/cli/log.go
  - internal/mcpserver/server.go
  - docs/logs.md
---

# WI-018 — System log reading

## Outcome

A CLI or MCP user can read the DSM system log (Log Center) with keyword, log
type, severity, and paging filters, through a read-only module that never
exposes a raw DSM call and never mutates or clears logs.

## Scope

- Read `SYNO.Core.SyslogClient.Log` `list` and normalize each entry to
  `time`, `level` (info/warn/error), `type` (system/connection/fileTransfer),
  `who`, and `message`, plus whole-log severity counts.
- DSM-applied filters: `keyword`, `logtype` (defaults to system; also connection,
  package, fileTransfer), a `date_from`/`date_to` time range (Unix seconds), and
  `start`/`limit` paging.
- Client-side severity filter over the retrieved page (DSM exposes no stable
  server-side level filter).
- `dsmctl log list` and `dsmctl log capabilities`; MCP `get_logs` and
  `get_log_capabilities` (read-only annotations); `log.read` capability in the
  compatibility report.

## Non-goals

- Log Center settings/notification/archiving management, log export/download,
  and per-package log APIs (FileTransfer/PersonalActivity) beyond the shared
  `logtype` filter.

## Design constraints

- Read-only: no mutation surface, no plan/apply.
- Follow the read-only module pattern (system info / SAN inventory); reuse the
  shared `lockedExecutor` and `prepareCompatibilityTargetLocked`.

## Acceptance criteria

- [x] `log capabilities` reports `log.read` and the selected backend.
- [x] `log list` returns normalized entries + counts with keyword/type/level/
      time-range/paging filters and `--json`.
- [x] `--from`/`--to` accept a local timestamp or Unix seconds and are
      live-verified to filter by time (and compose with `--type`).
- [x] Request+decode locked by a fixture-backed operation test.
- [x] MCP `get_logs` / `get_log_capabilities` registered read-only.

## Verification

- `go test ./...` and `go vet ./...`.
- Response shape and filter params (`keyword`, `logtype`, `start`/`limit`)
  captured read-only from the live NAS; a `list` fixture drives the decode test.
- Live-verified `log capabilities` and `log list` (keyword, connection type,
  error level, JSON) on DS3018xs / DSM 7.3-81168.

## Coordination

New `internal/domain/syslog` and `internal/synology/operations/syslogread`
packages; additive edits to `compatibility_report.go`, `manager.go`,
`service.go`, `server.go`, and a new `internal/cli/log.go`.
