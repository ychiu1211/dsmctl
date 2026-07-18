---
id: WI-031
title: Guarded Synology Drive server database configuration
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
---

# WI-031 — Guarded Synology Drive server database configuration

## Outcome

Extends the WI-022 Drive Admin module (read-only) with the first guarded Drive
write: the Drive server database configuration (`SYNO.SynologyDrive.Config`),
package-version gated on the installed SynologyDrive package.

## Scope

- Read the Drive server config: database volume (read-only), the vmtouch
  memory-pinning switch, and its reserved memory (MB).
- Guarded write of the vmtouch pair (`enable_vmtouch` + `vmtouch_reserve_mem`),
  which DSM couples; the facade submits both, merged from the freshly read
  config. Postcondition re-read verifies the change.
- `volume_select` is deliberately read-only: DSM changes it by physically moving
  the Drive database between volumes, which is out of scope for a settings write.

## Non-goals

- Drive database volume migration (the `volume_select` move).
- Team-folder enable/disable (the WI-022 `team_folders_set` stub remains
  backend-less) and other Drive Admin write surfaces.

## Design constraints

- DSM 7.3.2 / Drive 4.0.3 evidence from `synosyncfolder` handlers/config, confirmed
  live: get returns `volume_select`, `enable_vmtouch`, `vmtouch_reserve_mem`
  (default 30), `display_vmtouch_option`. set is a partial update; the vmtouch
  handler applies the enable flag and reserved memory together, so both are sent.
- Config read/set are selected separately from the stable `driveops.Select()`
  Admin Console order so the existing capability contract/test is undisturbed.
- Enabling vmtouch reserves memory to pin the database (high risk); changing the
  reserved amount while vmtouch is off has no runtime effect (medium).

## Acceptance criteria

- [x] Config decodes with semantic fields (volume read-only, vmtouch pair) and a
      required `enable_vmtouch` guard against API drift.
- [x] Package-version gating: read/set fail closed without SynologyDrive.
- [x] Guarded write of the coupled vmtouch pair with postcondition verification.
- [x] CLI (`drive config state|plan|apply`) and three MCP tools
      (`get_drive_config`, `plan_drive_config_change`, `apply_drive_config_plan`)
      with read-only-gateway exclusion of plan/apply.
- [x] DSM 7.3.2 live verification (lab, authorized, fully reverted): read the
      config and changed `vmtouch_reserve_mem` 30→40→30 through plan/apply with
      vmtouch left disabled (no memory reserved).

## Verification

- `go test ./... -count=1`, `go vet ./...`, CLI and MCP builds.
- Live read and reverted write on the DSM 7.3.2 lab NAS (Drive 4.0.3).
