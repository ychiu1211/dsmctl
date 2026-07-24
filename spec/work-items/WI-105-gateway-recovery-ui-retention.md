---
id: WI-105
title: Add Gateway recovery UI and backup retention
status: done
priority: P1
owner: ""
depends_on: [WI-015, WI-032, WI-033]
parallel_group: G
touches:
  - cmd/dsmctl-gateway/main.go
  - cmd/dsmctl-gateway/main_test.go
  - internal/gateway/admin/handler.go
  - internal/gateway/admin/handler_test.go
  - internal/gateway/admin/ui.go
  - internal/gateway/recovery/
  - deploy/synology/spk/package/project/compose.yaml.template
  - deploy/synology/spk/scripts/preupgrade
  - deploy/synology/spk/scripts/postinst
  - deploy/synology/spk/scripts/postupgrade
  - deploy/synology/spk/scripts/recovery-retention
  - deploy/synology/validate-spk.sh
  - docs/synology-package.md
---

# WI-105 — Add Gateway recovery UI and backup retention

## Outcome

Synology SPK administrators can inspect complete upgrade and pre-restore
Gateway recovery sets in the Admin UI, select one, and safely restore it
through a confirmed restart workflow. All recovery sets share one bounded
ten-entry retention policy instead of growing without bound.

## Scope

- Retain the newest ten complete recovery directories across `pre-upgrade-*`
  and `pre-restore-*` safety copies. Once ten complete sets exist, remove older
  incomplete recovery directories because they cannot be restored.
- Add a deployment-enabled recovery service that lists only safe, direct-child
  recovery directories and reports version, timestamp, size, completeness,
  and whether the backed-up keys match the active installation.
- Add authenticated Admin API and UI surfaces to list recovery sets and request
  a restore with an exact typed confirmation.
- Bind a pending restore request to the selected backup file hashes.
- Gracefully restart after scheduling a restore.
- Before opening managed state on restart, revalidate the request and hashes,
  require matching active keys, validate/migrate a staged database copy, create
  a current-state `pre-restore-*` safety copy, expose it as another restorable
  UI entry, enforce retention, and atomically replace the state database.
- Record a bounded success/failure result for the Admin UI and logs without
  exposing key material.

## Non-goals

- Whole-NAS, shared-folder, package-image, or cross-installation disaster
  recovery.
- Restoring a backup whose master or DSM assertion key differs from the active
  installation.
- MCP or CLI recovery operations.
- Giving the container Docker socket or DSM package-manager access.

## Design constraints

- Recovery is available only when explicitly configured by a deployment
  adapter; generic managed containers remain unchanged by default.
- Never overwrite an open SQLite database.
- Backup names must be validated basenames and resolved direct children of the
  configured recovery root. Symlinks and non-regular recovery files fail
  closed.
- Secrets, key bytes, and decrypted state never enter API responses, logs,
  audit, or restore request files.
- The restore request is content-bound and revalidated at startup.
- A failed validation leaves the current database untouched and the Gateway
  starts normally with a visible failure result.

## Acceptance criteria

- [x] A complete recovery set appears in the Admin UI with version, timestamp,
      size, and restorable state; malformed or symlinked sets are rejected.
- [x] Restore requires an authenticated administrator and an exact typed
      confirmation naming the selected backup.
- [x] A queued request is bound to all recovery file hashes and cannot be
      retargeted by changing files after confirmation.
- [x] Startup restores through a staged, successfully opened database and
      atomically replaces state only after a current-state safety copy exists.
- [x] Key mismatch or corrupt/tampered backup leaves current state untouched and
      records a non-secret failure result.
- [x] Successful restore restarts healthy and exposes the restored state.
- [x] The SPK lifecycle and restore workflow preserve only the newest ten
      complete recovery directories across `pre-upgrade-*` and `pre-restore-*`
      and do not count unusable incomplete sets beyond the requested retention.
- [x] A `pre-restore-*` safety copy appears in the Admin UI and can be selected
      for a subsequent restore.
- [x] Existing generic Linux and Synology Admin authentication behavior is
      unchanged.
- [x] User documentation describes retention, restore behavior, forced logout,
      safety copies, and manual fallback.

## Verification

- `go test ./internal/gateway/recovery ./internal/gateway/admin ./cmd/dsmctl-gateway -count=1`
- `go test ./... -count=1`
- `go vet ./...`
- `deploy/synology/validate-spk.sh <artifact>`
- Live SPK test on the DSM 7.3 test NAS: upgrade prunes to ten, UI lists them,
  restore one known recovery set, Gateway restarts healthy, then return to the
  newest state through the same UI.

## Coordination

- WI-091 and WI-092 also touch Gateway Admin authentication/UI. This item adds
  isolated recovery routes and one panel; it does not refactor login,
  credential, or NAS-profile flows.
- WI-017 owns broad distribution certification. This item changes only the
  already-shipped SPK lifecycle behavior and records its own live evidence.

## Handoff

- Implemented recovery inventory, authenticated Admin API/UI, exact-confirmation
  scheduling, hash-bound startup restore, staged SQLite validation/migration,
  atomic replacement, and `pre-restore-*` safety copies.
- Added SPK lifecycle retention for the newest ten complete `pre-upgrade-*`
  sets. Once ten usable sets existed, the live upgrade removed twelve older
  incomplete sets that lacked required files and could not be restored.
- Built and installed `dsmctl-gateway` `7.3.2-30` on the DSM 7.3 test NAS.
  The installed container uses `dsmctl-gateway:7.3.2-30` and is healthy.
- Live Admin recovery inventory reported ten complete/restorable entries.
  Restored `pre-upgrade-7.3.2-27-20260724131329`, observed a healthy restart,
  then restored `pre-upgrade-7.3.2-29-20260724141418` to return to the newest
  state. Both runs recorded success, left no pending request/staging files, and
  created a complete current-state safety copy.
- Post-restore MCP verification listed all three configured NAS targets,
  retained their stored authentication state, and successfully read system
  information from each NAS.
- Verification passed: `go test ./... -count=1`, `go vet ./...`, package-script
  syntax checks, and `validate-spk.sh` against the `7.3.2-30` artifact.
- Follow-up deployment verification found that the two `pre-restore-*` safety
  copies were hidden from the UI and excluded from retention, so repeated
  restores could still grow the directory without bound. WI-105 was reopened
  to make those safety copies visible, restorable, and part of the shared
  ten-set limit.
- The follow-up fix lists both backup kinds through the same Recovery API and
  applies one ten-set limit in both the SPK lifecycle and the in-process
  pre-restore workflow. Malformed entries and symlinks remain fail-closed and
  outside automatic deletion.
- Live `7.3.2-34` Admin API verification returned exactly ten entries: all ten
  were complete and restorable, two had version `pre-restore`, and no restore
  request was pending. On-disk inventory also contained exactly ten
  directories.
- The final image `dsmctl-gateway:7.3.2-34` is healthy. Full Go tests, `go vet`,
  shell syntax checks, and SPK validation passed.
