---
id: WI-029
title: Guarded Package Center online install and update
status: done
priority: P2
owner: ""
depends_on: [WI-019]
parallel_group: C
touches:
  - internal/domain/packagecenter
  - internal/synology/operations/packagecenter
  - internal/synology/packagecenter.go
  - internal/application/package_center.go
  - internal/cli/package.go
  - internal/mcpserver/server.go
  - docs/package-center.md
---

# WI-029 — Guarded Package Center online install and update

## Outcome

A CLI user or MCP agent can browse the online package catalog and, through a
hash-bound plan/apply, install a package from Synology's repository (and, as a
follow-up, update an installed package), lifting the install/update deferral from
WI-019.

## Scope

Implemented (install):

- Online catalog read (`SYNO.Core.Package.Server` v2/v1 `list`), merging the
  stable `packages` and `beta_packages` arrays into a normalized catalog with the
  download link, checksum, size, version, beta flag, and quick-install flag.
- Guarded online install: plan resolves the catalog entry, rejects a package that
  is already installed or not offered, and hashes the resolved intent; apply
  starts `SYNO.Core.Package.Installation` `install` (server download+install
  task) and confirms completion via the inventory (the download taskid is not
  queryable via `status`, which needs its own id).
- **Dependency-aware install (precheck):** the catalog decodes each package's
  `deppkgs`, and the plan resolves the full dependency closure (deps-first) from
  it, listing missing dependencies as ordered install steps; apply installs each
  dependency before the target. A required dependency that is neither installed
  nor offered is a hard precheck error (the "install X first" message DSM's UI
  shows). This is what lets a package like Surveillance Station install headlessly
  (it requires SurveillanceVideoExtension first). Per-package install timeout is
  30 minutes for large packages.
- CLI `package available` and `package install <id> --volume <path> [--approve]`.

Implemented (update check):

- The catalog read cross-references the installed inventory and marks each
  offered package `installed` and, when the installed version differs from the
  offered (latest) version, `update_available`. CLI `package available --updates`
  lists only packages with a pending update. Live-verified: SynologyPhotos and
  SynologyDrive show up-to-date while SecureSignIn/SynologyApplicationService/
  UniversalViewer correctly surface available updates.

Implemented (MCP parity, 2026-07-20):

- `get_package_available` (catalog read with `updates_only` filter),
  `plan_package_install`, and `apply_package_install_plan`; the read-only
  gateway strips the plan/apply pair. The install plan now carries the gateway
  `profile_revision` in its approval hash, and install-apply passes the same
  remote authorization and single-use high-risk approval checks as every other
  apply (install plans are always high risk).

Implemented (update apply, 2026-07-20):

- Guarded update through the shared install machinery: `PlanPackageUpdate`
  binds to the installed version (apply re-reads the inventory and rejects a
  changed/removed package), refuses a repository build that is not newer than
  the installed one (File Station on DSM 7.3 ships 1.4.3-2210 while the repo
  offers 1.4.3-1610 — a version difference is not permission to downgrade),
  prefers the stable catalog row over a beta of the same package, resolves
  new dependencies deps-first, and completes when the inventory reports the
  offered version. CLI `package update <id> [--approve]`; MCP
  `plan_package_update` + the shared `apply_package_install_plan`; the
  `packagecenter.update` capability now selects the Installation backend.

## Design constraints

- DSM 7.3.2 evidence, confirmed live (source/mobile-client field names were
  partly stale):
  - The online install is `SYNO.Core.Package.Installation` method **`install`**
    (not `download`, which returns code 103 on DSM 7.3) with `name`, `url`,
    `checksum`, `filesize`, `volume_path`, `beta`, `blqinst`, `installrunpackage`,
    `is_syno`, `type`. It returns a `taskid` (e.g. `@SYNOPKG_DOWNLOAD_<id>`).
  - `status` requires the `taskid` param (omitting it returns code 120). The task
    id changes between the download and install phases, so success is confirmed
    by the inventory rather than by a single task's `finished` flag; the status
    poll is best-effort and surfaces an explicit task error fast.
  - `blqinst: true` performs a quick install with defaults (no wizard), which is
    how a headless install completes a package whose catalog `qinst` is false.
- Installing downloads and runs third-party software; the plan is always high
  risk and requires an explicit approval hash. The read-only gateway must never
  expose install/update apply.

## Acceptance criteria

- [x] Online catalog decodes with normalized fields (stable + beta), with a
      request-capture/decoder test.
- [x] Guarded install: plan rejects already-installed/not-offered; apply is
      hash-bound and confirms via the inventory.
- [x] DSM 7.3.2 live verification: installed **Synology Photos 1.9.1-10928** to
      `/volume1` via `dsmctl package install` (the package is installed and
      running); the already-installed guard then rejects a re-install.
- [x] Update **check**: catalog cross-references the inventory and flags
      installed/update-available; live-verified via `package available --updates`.
- [x] Update/upgrade **apply** implemented (guarded, version-bound, downgrade
      refused); live-verified updating PHP 8.2 8.2.28-0107 -> 8.2.30-0170 on
      the DSM 7.3 lab, with the up-to-date and downgrade guards confirmed
      live afterwards.
- [x] MCP parity for catalog/install with read-only gateway exclusion (plus
      profile-revision-bound hashes and remote high-risk authorization on
      install-apply).

## Verification

- Decoder test for the catalog; `go test ./... -count=1`, `go vet ./...`, builds.
- Live install of Synology Photos on the DSM 7.3.2 lab NAS (explicitly
  authorized by the owner; the package remains installed as requested).
