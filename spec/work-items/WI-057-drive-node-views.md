---
id: WI-057
title: Drive node views: browse (including removed) and version history
status: done
priority: P2
owner: ""
depends_on: [WI-022, WI-050]
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

# WI-057 — Drive node views: browse (including removed) and version history

## Outcome

The admin rescue perspective as reads: browse a Drive view (team folder or
the signed-in account's My Drive) **including removed entries**, and list a
node's stored version history — the discovery half of "restore a deleted or
overwritten file".

## Scope

- `SYNO.SynologyDrive.Node` `list` v1 (the target advertises only v1):
  `target` is `user` for the calling account's My Drive or
  `@<shared-folder-name>` for a team folder (both verified live);
  `list_removed` defaults on; `pattern`, `recursive`, and paging pass
  through. Items carry name, path, a stringified `node_id`, `file_type`
  (1 = folder), `is_removed`, `v_file_size`, `ver_cnt`, `mtime`, and
  `permanent_link`.
- `SYNO.SynologyDrive.Node` `list_version` v1 with `target` + `path`:
  envelope `is_removed`/`disable_restore`/`permanent_link` plus version
  items (`create_time`, `modify_time`, `size`, `hash`, `version_updater`).
- CLI `drive admin files` / `drive admin file-versions`; MCP
  `get_drive_files` / `get_drive_file_versions`.

## Deferred (follow-up: Node.Restore)

The restore write (`SYNO.SynologyDrive.Node.Restore` `start`/`status`/
`finish`: fork-based task with `target`, `copy_to`, `override`,
`include_removed`, `nodes`; single task at a time per the handler) is
researched but not implemented: it needs the async-task plan/apply pattern
plus a live choreography (upload → version → delete → restore) that deserves
its own item. `Node.Download`, `Node.Delete` (scheduled purge), `list_parent`,
`dry_run`, and admin `view_role` impersonation are likewise out of scope.

## Acceptance criteria

- [x] Both reads with live-verified request/response shapes (My Drive and a
      disposable `dsmctl-e2e-*` team folder, removed entries listed, version
      history decoded) and strict envelope checks.
- [x] Capability reporting, CLI commands, and read-only MCP tools.
- [x] Live verification on Drive 4.0.3-27892 (2026-07-20), disposable team
      folder fully reverted afterwards.

## Verification

- `go test ./... -count=1`, `go vet ./...`.
- Live browse + version reads on the DSM 7.3.2 lab NAS.
