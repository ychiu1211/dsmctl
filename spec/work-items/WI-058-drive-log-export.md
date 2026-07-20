---
id: WI-058
title: Drive log export to a local file
status: ready
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

# WI-058 — Drive log export to a local file

## Outcome

`dsmctl drive admin log export` writes Drive's server log to a local file for
compliance and handover, with the same filters as `log list`.

## Source evidence (synosyncfolder, 4.0 official branch, gathered in WI-052)

`server/ui-web/src/handlers/log/export.cpp`: `SYNO.SynologyDrive.Log`
`export` (POST, admin-only) answers a **file response**
(`BridgeResponse::RESPONSE_FILE`, `SetOutputJsonError(false)`). Parameters:
required `type` (validated against the log factory — the concrete values,
likely `csv` and/or `html`, must be confirmed live), optional `target`
(`@`-prefixed team folder), and the `log list` filters (`keyword`,
`username`, `username_include_system`, `ip_address`, date bounds).

## Design constraints

- The response is raw file bytes, not the JSON envelope: reuse the streaming
  byte transport the FileStation module added (`executeScriptLocked` /
  the download path) rather than the JSON executor.
- CLI writes to a caller-named local file. MCP is read-only in nature but
  returns bulk content; decide between a size-bounded base64 tool and
  CLI-only (the FileStation content-transfer precedent strips such tools
  from the read-only gateway).
- `Log.delete` (clearing the audit trail) stays fail-closed/deferred.

## Acceptance criteria

- [ ] Export operation with the live-confirmed `type` vocabulary and filter
      pass-through, using the byte transport.
- [ ] CLI `drive admin log export --output <file>` with filters.
- [ ] Live verification: export the lab Drive log and inspect the file.
