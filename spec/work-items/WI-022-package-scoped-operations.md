---
id: WI-022
title: Package-scoped operation framework and Drive Admin read slice
status: done
priority: P1
owner: ""
depends_on:
  - WI-019
parallel_group: C
touches:
  - internal/synology/compatibility/target.go
  - internal/synology/compatibility/selector.go
  - internal/synology/compatibility_report.go
  - internal/synology/client.go
  - internal/synology/driveadmin.go
  - internal/synology/operations/driveadmin
  - internal/domain/driveadmin
  - internal/application/drive_admin.go
  - internal/cli/drive.go
  - internal/cli/root.go
  - internal/mcpserver/server.go
  - docs/compatibility.md
  - docs/drive-admin.md
  - docs/architecture.md
  - README.md
---

# WI-022 — Package-scoped operation framework and Drive Admin read slice

## Outcome

dsmctl can manage functionality provided by an installed DSM package (not only
DSM itself), selecting the operation implementation from the **installed
package version** in addition to the advertised WebAPI versions and DSM
release. Before any command is issued to a package's APIs, dsmctl re-reads the
installed-package inventory, verifies the package is installed (and reports
whether it is running), and routes to the variant whose package-version range
matches — the same NAS and DSM build can therefore select different operation
variants for different installed versions of the same package. Synology Drive
Server's Admin Console is the first consumer: a read-only `drive admin` module
(service status, active connections, team folders, logs) built entirely on the
new selection axis.

## Scope

- Compatibility framework:
  - `PackageVersion` parse/compare for Synology package versions
    (`4.0.3-27892`-style numeric segments).
  - An installed-package catalog on the compatibility target (id → version +
    running flag), populated from the verified WI-019 inventory operation
    (`SYNO.Core.Package` `list`).
  - `PackageInstalled(id)` and `PackageVersionRange(id, min, max)` matchers
    composable with the existing API/DSM matchers; selection reasons carry the
    observed package version as evidence.
  - A client-façade bootstrap that **refreshes the package catalog before every
    package-scoped façade call**, so a package updated mid-session cannot keep
    a stale variant selection.
  - Compatibility report evidence: installed packages relevant to selected
    operations appear in `Report.Packages`.
- Drive Admin read module (package id `SynologyDrive`, module `drive-admin`):
  - `drive.admin.status.read` — `SYNO.SynologyDrive` `get_status`.
  - `drive.admin.connections.read` — `SYNO.SynologyDrive.Connection` `list`,
    with independent v2/v1 variants (both advertised on the verified target).
  - `drive.admin.teamfolders.read` — `SYNO.SynologyDrive.Share` `list`.
  - `drive.admin.log.read` — `SYNO.SynologyDrive.Log` `list` with
    keyword/username/target/time-range/paging filters.
  - All variants gate on `PackageVersionRange("SynologyDrive", 3.0, ∞)`: the
    verified baseline is the Drive 3+/4 Admin Console API family; older
    Drive/Cloud Station generations fail closed instead of receiving untested
    requests.
  - Capabilities include the observed package version and running state, so a
    stopped-but-installed Drive is distinguishable from an unsupported one.
- Thin CLI (`dsmctl drive admin …`) and MCP (`get_drive_admin_*`) adapters over
  the same application methods.

## Non-goals

- Drive Admin **writes** (team-folder enable/disable, config set, connection
  kick, index pause): `drive.admin.teamfolders.set` is modeled variant-less and
  fails closed (`team_folders_set: false`). The first verified write ships as a
  follow-up using the same hash-bound plan/apply contract as Package Center.
- End-user Drive file operations (`SYNO.SynologyDrive.Files`, sharing links,
  labels) — different privilege model and blast radius.
- ShareSync / C2 / Office integrations (`SYNO.SynologyDriveShareSync.*`).
- A generic raw proxy for arbitrary package APIs.

## Design constraints

- Package-version matching is an additional axis, not a replacement: prefer
  advertised API versions when they move; use a package-version range for a
  verified behavioral baseline or difference, mirroring the DSM-release rule.
- The package catalog is only trusted when loaded through the verified
  inventory backend; if inventory is unsupported or fails, package-scoped
  operations report unsupported/fail with an explicit reason instead of
  guessing.
- Mutating package-scoped operations must never run with a stale catalog;
  the façade refresh-before-command rule is part of the contract.
- Decoders normalize DSM responses defensively and error on malformed
  envelopes; they never silently return an empty successful state.
- A missing/stopped Drive package affects only this module.

## Acceptance criteria

- [x] `PackageVersion` parse/compare unit tests cover multi-segment, build
      suffix, leading-zero, and unknown shapes.
- [x] Matcher unit tests cover: catalog not loaded, package missing, version
      below/at/above range bounds, and composition with API matchers.
- [x] Selector tests prove two installed package versions of the same package
      select different variants on an otherwise identical target.
- [x] Façade tests prove the catalog is refreshed before package-scoped calls
      and that selection reasons/report carry the observed package version.
- [x] Drive operations have request-capture tests locking API, version,
      method, and parameters, plus decoder fixtures for every read.
- [x] Drive capabilities report package evidence (installed/version/running)
      and `team_folders_set: false`.
- [x] CLI and MCP adapters expose capabilities/status/connections/team
      folders/logs over the shared application methods.
- [x] `go test ./...`, `go vet ./...`, all three binary builds.
- [x] Read-only live verification against the configured NAS (Drive
      4.0.3-27892 observed via SYNO.API.Info and package webman manifest):
      capabilities, status, connections, team folders, logs.

## Verification

- Unit fixtures and request-capture tests as above.
- `go test ./... -count=1`, `go vet ./...`.
- Live policy: read-only discovery/state checks only; no Drive mutations are
  authorized in this item (the only modeled write fails closed).

## Coordination

Parallel group C (module family shared with WI-019/WI-020). Touches the shared
compatibility target/selector, `compatibility_report.go`, and
`mcpserver/server.go` — high-contention files; no other item is active in
group C at claim time.

## Evidence

- `SYNO.API.Info` discovery on the configured lab NAS (read-only,
  2026-07-17): 60 `SYNO.SynologyDrive*` APIs, including
  `SYNO.SynologyDrive` v1, `Connection` v1-2, `Share` v1-2, `Log` v1,
  `Info` v1-2, `Settings` v1-3.
- NAS-local package assets: `webman/3rdparty/SynologyDrive/config` reports the
  Admin Console app at version `4.0.3-27892`; `cloudstation_util.js` shows the
  WebAPI namespace and `get_status` / `get_directory_service_status` methods.
- Community client (N4S4/synology-api `drive_admin_console.py`) corroborates
  method names and Log `list` filter parameters (`keyword`, `username`,
  `target`, `datefrom`, `dateto`, `limit`).
- Internal codesearch cross-check (after the extension reconnected): the Drive
  server repo is `synosyncfolder`; the WebAPI registry is
  `server/ui-web/webapi/admin-console/SYNO.SynologyDrive.py` and the log
  handler `server/ui-web/src/handlers/log/list.cpp`, with release branches per
  package version (SynologyDrive-3-0/3-4/3-5/BSM-4-0) confirming that the API
  surface follows the package release — the premise of this work item.

## Completion record

- Framework and module completed with unit coverage on 2026-07-17; live
  read-only verification finished the same day once `DSMCTL_PASSWORD_LAB`
  became available (user-scope environment variable; the OS keyring holds only
  the trusted-device entry).
- Live verification against the configured lab NAS (DSM 7.3, Synology Drive
  Server 4.0.3-27892, running) passed for `drive admin capabilities`,
  `status`, `connections`, `team-folders`, and `log list` (including the
  username filter), with the observed package version carried in the
  capability report and selection reasons.
- Live verification corrected the initial evidence-based assumptions, caught
  by the strict decoders and read-only probes:
  - `get_status` reports the service state as `enable_status`, not `status`.
  - `SYNO.SynologyDrive.Share.list` rejects requests (error 120) without
    `offset`/`limit`/`sort_by`/`sort_direction`; items carry `share_name`,
    `share_enable`, `share_status`. The domain model gained `Enabled`.
  - `SYNO.SynologyDrive.Log.list` requires `target` ("user" for the all view,
    "@<share>" for one team folder, matching the Admin Console's beforeload
    logic), takes `log_type` as a numeric event-code array, uses
    `datefrom`/`dateto` (not the client helper's `date_from`), and supports
    `offset` and `username` (confirmed in `handlers/log/list.cpp`). Entries
    are template-coded (numeric `type` + `s*`/`p*` slots), so the domain log
    entry surfaces structured fields instead of a rendered description.
- Connection list items were observed only as an empty set (no live Drive
  clients during verification); per-item field aliases stay defensive and are
  the one shape to re-confirm when a client is connected.
- No mutation was attempted; `drive.admin.teamfolders.set` remains modeled and
  fail-closed as scoped.
