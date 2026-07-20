---
id: WI-054
title: Guarded Drive client-session disconnect
status: done
priority: P2
owner: ""
depends_on: [WI-022, WI-053]
parallel_group: C
touches:
  - internal/domain/driveadmin
  - internal/synology/operations/driveadmin
  - internal/synology/driveadmin.go
  - internal/application/drive_admin.go
  - internal/cli/drive.go
  - internal/mcpserver/server.go
  - internal/mcpserver/read_only.go
  - docs/drive-admin.md
---

# WI-054 — Guarded Drive client-session disconnect

## Outcome

A CLI user or MCP agent can disconnect one Synology Drive client session
(stale device, departed employee's laptop) through the standard hash-bound
plan/apply, targeted by the session id the connections read now exposes.

## Scope

- Correct the connections read to the Drive server's actual item fields
  (`handlers/connection/list.cpp`): `client_session_id`, `client_id`,
  `client_name` (device), `client_ip`, `client_status`, `client_type`,
  `client_version`, `client_location`, `client_is_relay`, `client_can_wipe`,
  `device_uuid`, stringified `login_time`, `last_auth_time`. Sessions carry
  no account name; the previous field set (`username`/`device_name`/
  `address`) never matched the handler and stays only as a decode fallback.
- Guarded kick via `SYNO.SynologyDrive.Connection` `delete` v2
  (`client_sess_id` JSON array; dsmctl sends exactly one id). The remote
  data-wipe companion (`data_wipe`) is deliberately never sent.
- Plan binds the observed connection entry; apply re-reads, rejects stale
  state, deletes, and verifies the session left the list with bounded
  retries (the handler answers an empty success).
- CLI `drive admin connections kick --session <id>` (plan) + `connections
  apply`; MCP `plan_drive_connection_kick` / `apply_drive_connection_kick_plan`,
  stripped from the read-only gateway.

## Verification limits

The lab NAS has no live Drive sync clients, so the delete call itself is
**source-verified** (`handlers/connection/delete.cpp`) rather than
live-executed; the surrounding contract is live-verified: the list read, the
capability selection, and the plan path (a missing session id returns an
explicit error before any mutation). The blast radius is small — a kicked
client re-authenticates and resumes; synced files stay on the device.

## Acceptance criteria

- [x] Connections read decodes the source-true field set, including the
      session id and stringified login time.
- [x] Guarded kick with entry-bound fingerprint, stale rejection, and
      list-based postcondition; silent-skip surfaces as not confirmed.
- [x] CLI + MCP wiring with read-only gateway exclusion.
- [x] Live checks on the lab NAS: list/summary reads, capability selection,
      and the plan not-found path (2026-07-20). Delete request shape
      source-verified; no live sync client was available to kick.

## Verification

- `go test ./... -count=1`, `go vet ./...`.
- Live reads + plan path on the DSM 7.3.2 lab NAS (Drive 4.0.3-27892).
