---
id: WI-029
title: Guarded Package Center online install and update
status: in_progress
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
  task), polls `status` by task id, and confirms completion via the inventory.
- CLI `package available` and `package install <id> --volume <path> [--approve]`.

Deferred (this WI stays in_progress until done):

- Update / upgrade (`SYNO.Core.Package.Server check` for available updates plus
  `SYNO.Core.Package.Installation` `upgrade`); could not be live-verified without
  an out-of-date package on the lab.
- MCP tools for catalog/install and read-only-gateway exclusion of install-apply.

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
- [ ] Update/upgrade implemented and (where possible) verified.
- [ ] MCP parity for catalog/install with read-only gateway exclusion.

## Verification

- Decoder test for the catalog; `go test ./... -count=1`, `go vet ./...`, builds.
- Live install of Synology Photos on the DSM 7.3.2 lab NAS (explicitly
  authorized by the owner; the package remains installed as requested).
