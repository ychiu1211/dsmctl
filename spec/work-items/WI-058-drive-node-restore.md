---
id: WI-058
title: Guarded Drive node restore (deleted files and versions)
status: ready
priority: P2
owner: ""
depends_on: [WI-057]
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

# WI-058 — Guarded Drive node restore (deleted files and versions)

## Outcome

Complete the rescue story WI-057 started: restore removed nodes (and, if the
API allows selecting one, a specific stored version) in a Drive view through
a guarded plan/apply built on Drive's asynchronous restore task.

## Source evidence (synosyncfolder, 4.0 official branch, gathered in WI-057)

`server/ui-web/src/handlers/node/restore/{start,status,finish}.cpp`:

- `SYNO.SynologyDrive.Node.Restore` `start` (POST, enabled-user with admin
  switch via `view_role`): `target` (user or share, same forms as Node.list),
  `copy_to`, `override` (default true), `include_removed` (default false),
  `nodes` (JSON array). Forks a child that walks the nodes; only one restore
  task runs at a time (`HANDLER_ERR_RESTORE_TASK_RUNNING`), progress is kept
  in shared memory (`task_id`, `current`, `total`, `last_update_time`).
- `status` polls that progress; `finish` clears it.
- Whether `start` restores a *specific version* (vs. the latest/removed
  state) must be determined live — the version selector may be `nodes`
  entries carrying version ids from `list_version` (`sync_id`?).

## Plan/apply sketch

- Plan binds the observed node entry (WI-057 read: `is_removed`, `ver_cnt`)
  and the requested scope; risk high when `override` would replace current
  content.
- Apply: `start` → poll `status` until `current == total` (bounded, the
  existing async patterns apply) → `finish` → postcondition via the WI-057
  reads (node no longer removed / expected version restored).
- Live verification choreography on a disposable `dsmctl-e2e-*` team folder:
  upload a file (FileStation module), overwrite it (second version), delete
  it (FileStation delete), then restore via the new write and verify content.

## Acceptance criteria

- [ ] Restore start/status/finish operations with request-capture tests.
- [ ] Guarded plan/apply bound to the observed node, async poll, explicit
      not-yet-confirmed error.
- [ ] CLI + MCP with read-only gateway exclusion.
- [ ] Live verification, fully reverted (disposable team folder deleted).
