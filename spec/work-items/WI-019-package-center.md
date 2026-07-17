---
id: WI-019
title: Package Center inventory, settings, and guarded lifecycle
status: done
owner: ""
priority: P1
depends_on: []
parallel_group: C
touches:
  - internal/domain/packagecenter/model.go
  - internal/synology/operations/packagecenter
  - internal/synology/packagecenter.go
  - internal/synology/compatibility_report.go
  - internal/application/package_center.go
  - internal/cli/package.go
  - internal/cli/root.go
  - internal/mcpserver/server.go
  - docs/package-center.md
  - README.md
---

# WI-019 — Package Center inventory, settings, and guarded lifecycle

## Outcome

A CLI user or MCP agent can inventory installed DSM packages, read global Package
Center settings, and plan/apply guarded global-settings and package-lifecycle
(`start`, `stop`, `uninstall`) changes through the shared application layer,
without dsmctl exposing a raw DSM settings or installation proxy.

## Scope

- Normalized read-only inventory from `SYNO.Core.Package` `list`: package id,
  display name, installed version, normalized run status, running flag, beta
  flag, and install volume, plus start/stop/uninstall eligibility derived from
  DSM's `startable` flag, the run status, and `install_type`. (Update-available
  detection needs the online catalog API and ships with the deferred update
  feature.)
- Normalized read-only global settings from `SYNO.Core.Package.Setting` `get`:
  publisher trust level and the automatic-update policy (enabled + important-only).
  These are the fields the aggregated `get` exposes; settings **changes** are
  deferred (see Non-goals).
- Independent compatibility selections for inventory, settings read, control
  (start/stop), and uninstall; settings set reports unsupported.
- One shared hash-bound plan/apply contract carrying a `lifecycle` action, with
  observed-state fingerprints, stale-state rejection, service-disruption
  warnings, risk classification, and postcondition verification.
- Thin CLI (`dsmctl package …`) and MCP (`*_package_*`) adapters over the same
  application methods, and Package Center operations in the compatibility report.

## Non-goals

- Package **install-from-repository** and **update** are modeled in capabilities
  but **fail closed** (report `false`, request validator rejects them). They
  contact Synology's online repository, run asynchronously, and download and run
  remote code, which does not fit the synchronous plan/apply postcondition
  contract. Tracked as a follow-up (WI-020).
- **Settings changes** (`set`): modeled but fail closed (`settings_set: false`,
  `settings` plans refused). Verified live on DSM 7.3 that the set surface is
  split across `SYNO.Core.Package.Setting.Update` (auto-update),
  `SYNO.Core.Package.Setting.Volume` (default volume), and the base
  `SYNO.Core.Package.Setting` (notifications/channel only, which silently ignores
  trust level and auto-update). A correct set needs the per-section sub-APIs;
  tracked as a follow-up. Settings are read-only in this slice.
- The online package **catalog** browse (`SYNO.Core.Package.Server`).
- The **beta channel** toggle and **default install volume**: verified on DSM 7.3
  to be absent from `SYNO.Core.Package.Setting` (beta lives in the online package
  server; the volume is chosen per install), so they ship with the deferred
  settings-set / install / update work rather than being reported here.
- Per-package, application-specific settings (each package owns its own schema);
  only the global Package Center settings page is in scope.
- Uploading a local `.spk`, package repositories/keyring management, and package
  auto-update scheduling windows.
- Live package mutations without explicit authorization for the exact test.

## Design constraints

- Settings are read-only in this slice; the modeled (deferred) settings-change
  plumbing keeps patch-only ownership so an omitted field would preserve its
  current DSM value once per-section set is implemented.
- Lifecycle actions bind to the stable package id plus an observed-state
  fingerprint; `uninstall` is refused when the observed package reports it is not
  uninstallable, and `stop`/`uninstall` and lowering the trust level to any
  publisher are high risk.
- The mutation operation package never decodes the write response for
  correctness; correctness is proven by a fresh-inventory/settings postcondition
  re-read. Start/stop/uninstall verify the terminal state (running / stopped /
  absent); a still-transitional DSM yields an explicit not-yet-confirmed error
  rather than a false success.
- API/version and field evidence come from `SYNO.API.Info` discovery on the
  configured NAS and NAS-local Package Center assets or official Synology
  documentation. A missing Package Center API makes only this module unsupported.

## Acceptance criteria

- [x] Package inventory decoder exposes only stable semantic fields.
- [x] Inventory, settings read, control, and uninstall support are selected
      independently; install, update, and settings set report `false`.
- [x] CLI and MCP schemas use the same application contract.
- [x] The lifecycle plan binds to the single observed package; apply rejects
      stale state and verifies a fresh terminal-state postcondition.
- [x] Request-capture tests lock every enabled DSM mutation shape.
- [x] Read-only inventory/settings/capability verification passes against a
      configured DSM NAS (DSM 7.3-81168; see Completion record).
- [x] Live start/stop and uninstall verified with explicit user authorization; no
      settings mutation was applied (settings set is deferred).

## Verification

- Sanitized decoder fixtures and request-capture unit tests, plus application
  plan/apply unit tests with a fake package client.
- `go test ./... -count=1`, `go vet ./...`, CLI and MCP builds.
- Read-only API discovery/state checks on a configured DSM NAS when reachable.

## Coordination

Extends the focused-module pattern established by WI-006/WI-012. Package Center
is its own top-level module (not a Control Panel submodule) and adds a new
operation package, so it does not overlap active items. The user requested
Package Center management on 2026-07-17 and explicitly authorized live changes on
the configured test NAS the same day.

## Completion record

- Completed end to end on 2026-07-17. Domain model, operation package
  (inventory, settings read, control, uninstall; install/update/settings-set
  variant-less and fail-closed), Synology facade, application plan/apply, CLI
  (`dsmctl package capabilities|inventory|settings|plan|apply`), five MCP tools,
  compatibility report wiring, docs, and README are in place.
- Verified with `go test ./... -count=1`, `go vet ./...`, `gofmt` clean on all
  touched files, both binary builds, and `git diff --check`. Unit coverage
  includes decoder fixtures, request-capture shapes for control / uninstall,
  install/update/settings-set fail-closed selection, and application plan/apply,
  stale-state, no-op, non-removable-uninstall, capability-gating, deferred-action
  rejection, and hash-tamper cases.
- Read-only live verification on a DS-model NAS running **DSM 7.3-81168**:
  `dsmctl package capabilities` selected `SYNO.Core.Package` v2,
  `SYNO.Core.Package.Control` v1, and `SYNO.Core.Package.Uninstallation` v1, with
  install/update/settings-set reported `false`; `package inventory` returned 20
  packages with correct start/stop/uninstall derivation; `package settings`
  returned the trust level and auto-update policy.
- Live verification corrected the initial DSM-field assumptions. Inventory: the
  `SYNO.Core.Package.list` `additional` request rejected
  `stoppable`/`removable`/`installing` (error 120), so the valid keys are
  `["status","beta","startable","install_type"]` and stop/uninstall eligibility
  is derived (`startable` is a "has start/stop control" flag, not "can start
  now"). Settings read: `SYNO.Core.Package.Setting.get` exposes
  `enable_autoupdate` (master toggle, not `autoupdateall`), `autoupdateimportant`,
  and integer `trust_level`, but no beta or default-volume field. Settings set:
  `SYNO.Core.Package.Setting.set` accepts only notification/channel fields and
  silently ignored trust level and auto-update (the postcondition caught the
  no-op), and `SYNO.API.Info` enumeration showed the set surface split across
  `SYNO.Core.Package.Setting.Update` and `.Setting.Volume` — so settings set was
  deferred (fail closed) and settings are read-only.
- Live mutation testing (user-authorized): a `stop` then `start` round-trip on
  the `Spreadsheet` (Synology Office) package applied successfully with verified
  terminal-state postconditions, confirming `SYNO.Core.Package.Control` `start`/
  `stop` with an `id` parameter. `stop` on `AIConsole`/`UniversalViewer` returned
  DSM error 4582 (DSM refuses to stop packages required by others), surfaced as
  an error rather than a false success. Uninstall was then verified live too:
  `uninstall` of `Node.js_v18` applied successfully via
  `SYNO.Core.Package.Uninstallation`, the package was removed, and the
  absent-package postcondition passed. Uninstall is irreversible through dsmctl
  (install is deferred), so a removed package is reinstalled from the Package
  Center UI.
