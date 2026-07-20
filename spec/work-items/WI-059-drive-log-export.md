---
id: WI-059
title: Drive log export to a local file
status: done
priority: P3
owner: ""
depends_on: [WI-022]
parallel_group: C
touches:
  - internal/synology/operations/driveadmin
  - internal/synology/driveadmin.go
  - internal/application/drive_admin.go
  - internal/cli/drive.go
  - docs/drive-admin.md
---

# WI-059 — Drive log export to a local file

## Outcome

`dsmctl drive admin log export` writes Drive's server log to a local file for
compliance and handover, with the same filters as `log list`.

## Source evidence (synosyncfolder, 4.0 official branch, gathered in WI-053)

`server/ui-web/src/handlers/log/export.cpp`: `SYNO.SynologyDrive.Log`
`export` (POST, admin-only) answers a **file response**
(`BridgeResponse::RESPONSE_FILE`, `SetOutputJsonError(false)`). Parameters:
required `type` (validated against the log factory — the concrete values,
likely `csv` and/or `html`, must be confirmed live), optional `target`
(`@`-prefixed team folder), and the `log list` filters (`keyword`,
`username`, `username_include_system`, `ip_address`, date bounds).

## Implemented

- Confirmed live: `type=csv` is the only supported format (the handler
  comment says "currently we only supports csv" and the writer factory
  rejects others). The export renders human-readable rows (headers `Date
  Time, Operator, Action, Related Path, Related User, Related Share, Device
  Name, Client Type, IP Address, Additional`) — unlike the numeric-coded
  `log list` read.
- Transport: `export` is POST and answers raw file bytes with
  `SetOutputJsonError(false)`, so `executeScriptLocked` (which is GET) does
  not fit. Added `requestFileLocked` — a form-encoded POST that returns raw
  bytes and surfaces a JSON error envelope as an APIError. The facade
  `DriveLogExport` builds the params (the handler prepends `@` to `target`,
  so the bare team-folder name is sent) and gates on a `log.export`
  capability that shares the Log API v1 backend.
- CLI `drive admin log export [-o file] [--keyword/--username/--team-folder/
  --from/--to]` (stdout when no `-o`). MCP `get_drive_log_export` returns the
  CSV as text and is stripped from the read-only gateway (bulk content
  transfer, like `get_filestation_file_content`).
- `Log.delete` (clearing the audit trail) stays deferred/fail-closed.

## Acceptance criteria

- [x] Export uses `type=csv` (live-confirmed sole format) with filter
      pass-through over the raw file POST transport.
- [x] CLI `drive admin log export` (file or stdout) with filters; MCP tool
      excluded from the read-only gateway.
- [x] Live verification on Drive 4.0.3-27892 (2026-07-20): exported the lab
      Drive log to CSV (4911 bytes, valid headers), and confirmed the
      `--username` filter drops system rows.
