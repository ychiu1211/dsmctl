---
id: WI-052
title: Drive Admin observability and activation reads
status: done
priority: P2
owner: ""
depends_on: [WI-022]
parallel_group: C
touches:
  - internal/domain/driveadmin
  - internal/synology/operations/driveadmin
  - internal/synology/driveadmin.go
  - internal/application/drive_admin.go
  - internal/cli/drive.go
  - internal/mcpserver/server.go
  - docs/drive-admin.md
---

# WI-052 — Drive Admin observability and activation reads

## Outcome

Four read-only Drive Admin operations that let an agent report Drive health
without the Admin Console: the overview connection counters, the cached
database usage breakdown, the top-accessed-files ranking, and the package
activation state.

## Scope

- `SYNO.SynologyDrive.Connection` `summary` **v2** (v1 answers 103, verified
  live): desktop/mobile/sharesync/total counters.
- `SYNO.SynologyDrive.DBUsage` `get` v1: cached repo/database/office sizes in
  bytes plus the calculation time. The recalculation task
  (`status`/`start`/`stop`) stays deferred.
- `SYNO.SynologyDrive.Dashboard` `top_access_files` v1 with `ranking_by`
  (both/preview/download), `period_days`, `limit`, `offset`; the envelope was
  verified live (empty ranking) and rows decode leniently.
- `SYNO.SynologyDrive.Activation` `get` v1: activated flag, NAS serial, and
  activation time. **Activation set stays deferred**: it requires the Admin
  Console's online activation-code exchange (`activation_code` + `request_id`
  bound to the serial number, per `handlers/activation/set.cpp`), and an
  unactivated Drive serves clients anyway (verified live: the lab package is
  running and unactivated).
- CLI `drive admin summary|db-usage|top-files|activation`; MCP
  `get_drive_connection_summary`, `get_drive_db_usage`, `get_drive_top_files`,
  `get_drive_activation`. All package-gated on `SynologyDrive >= 3.0` like the
  rest of the module.

## Acceptance criteria

- [x] Four operations with live-verified response decoding (summary, db-usage,
      activation exact; dashboard envelope) and strict envelope checks.
- [x] Capability reporting for each operation, CLI commands, and read-only MCP
      tools.
- [x] DSM live verification on the lab NAS (Drive 4.0.3-27892), 2026-07-20:
      all four reads returned real data through the CLI.

## Verification

- `go test ./... -count=1`, `go vet ./...`.
- Live reads on the DSM 7.3.2 lab NAS.
