# Package Center

Package Center is a focused, typed module with its own state, capability names,
DSM API variants, CLI subtree, and MCP tools. It manages installed packages and
the global Package Center configuration without exposing a raw DSM installation
or settings proxy.

## Inventory and capabilities

```console
dsmctl package capabilities --nas office
dsmctl package inventory --nas office --json
dsmctl package settings --nas office --json
```

`inventory` returns each installed package with normalized, semantic fields: the
stable DSM id, display name, installed version, a normalized run status
(`running`, `stopped`, `starting`, `stopping`, `installing`, `error`, or
`unknown`), a running flag, a beta flag, the install volume when DSM reports it,
and whether DSM allows the package to be started, stopped, or uninstalled.

`capabilities` reports which operations are available and the DSM backend
selected for each, including the guarded `install` (online by package id and
local from an uploaded `.spk` — see
[Local (manual) install](#local-manual-install)) and `update`, all backed by
`SYNO.Core.Package.Installation`.

MCP exposes the same application results through `get_package_capabilities`,
`get_package_state`, and `get_package_settings`.

The inventory backend is `SYNO.Core.Package` `list`; settings use
`SYNO.Core.Package.Setting`. A missing Package Center API makes only this module
unsupported; storage, SAN, account, share, and Control Panel features are
unaffected.

## Settings

`dsmctl package settings` reads the global settings exposed by
`SYNO.Core.Package.Setting`: the publisher trust level (`synology`,
`synology_and_trusted`, or `any`) and the automatic-update policy. DSM's three
automatic-update choices map to two booleans:

| DSM choice | `auto_update_enabled` | `auto_update_important_only` |
| --- | --- | --- |
| Do not install automatically | `false` | (ignored) |
| Install important updates only | `true` | `true` |
| Install all updates | `true` | `false` |

The **automatic-update policy is writable** through the same hash-bound
plan/apply flow. A settings change is patch-only: an omitted field keeps its
current value. The plan records and hashes the complete current settings state;
apply rejects a changed state, merges the patch into a freshly read full state,
submits the three DSM auto-update fields consistently through
`SYNO.Core.Package.Setting.set`, and verifies the requested fields afterward.

```json
{
  "kind": "settings",
  "settings": { "auto_update_enabled": true, "auto_update_important_only": true }
}
```

```console
dsmctl package plan --nas office --file settings.json --output settings.plan.json
dsmctl package apply --file settings.plan.json --approve <hash-from-plan>
```

**Trust level is read-only** and cannot be changed: no DSM WebAPI writes it, and
the base `set` silently ignores it, so `trust_level` is not accepted in a change.
The beta channel and default install volume are likewise not writable here (see
[Deferred operations](#deferred-operations)).

## Guarded package lifecycle

The lifecycle actions on already-installed packages are `start`, `stop`, and
`uninstall`. A lifecycle change identifies the package by its stable DSM id and
binds the plan to the observed package state.

Example lifecycle request:

```json
{
  "kind": "lifecycle",
  "lifecycle": {
    "action": "stop",
    "package_id": "WebStation"
  }
}
```

```console
dsmctl package plan --nas office --file stop.json --output stop.plan.json
dsmctl package apply --file stop.plan.json --approve <hash-from-plan>
```

Planning refuses a no-op (starting a running package or stopping a stopped one),
refuses `uninstall` when DSM reports the package is not removable, and requires
the matching verified backend. `stop` is high risk because it interrupts the
package's service and any dependents; `uninstall` is destructive and high risk
because it removes the package and may delete its configuration and data.

Apply re-reads the inventory and verifies the terminal state: `start` expects a
running package, `stop` expects a stopped package, and `uninstall` expects the
package to be absent. If DSM is still mid-transition, apply returns an explicit
not-yet-confirmed error and asks the caller to re-check `package inventory`
rather than reporting a false success.

MCP exposes the same contract through `plan_package_change` and
`apply_package_plan`.

## Online catalog and guarded install

The online catalog read merges the package server's stable and beta arrays and
cross-references the installed inventory:

```console
dsmctl package available --nas office
dsmctl package available --nas office --updates
```

Each entry reports the offered version, size, beta flag, dependencies, whether
it is already installed, and whether an installed package has a newer offered
version. MCP: `get_package_available` (`updates_only` filters to pending
updates).

Install is a hash-bound plan/apply. Planning resolves the target against the
catalog and inventory, rejects an already-installed or not-offered package,
and resolves the full dependency closure: missing dependencies become ordered
install steps before the target (the "install X first" precheck DSM's UI
shows). Installing downloads and runs third-party software, so the plan is
always **high risk**:

```console
dsmctl package install SurveillanceStation --volume /volume1 --nas office
dsmctl package install SurveillanceStation --volume /volume1 --nas office --approve <hash-from-plan>
```

Apply starts DSM's server-side download+install task per step (dependencies
first) and confirms completion against the installed-package inventory; large
packages take minutes per step. MCP: `plan_package_install` and
`apply_package_install_plan` (`run_after_install` and `quick_install` default
to true). The read-only gateway strips the plan/apply pair, and the remote
gateway's high-risk approval flow applies.

## Guarded update

Updating an installed package to the version offered by the online catalog is
its own plan/apply, sharing the install machinery and apply tool:

```console
dsmctl package update PHP8.2 --nas office
dsmctl package update PHP8.2 --nas office --approve <hash-from-plan>
```

The update plan **binds to the installed version**: apply re-reads the
inventory and rejects the plan when the package was updated or removed in
between. Planning rejects a package that is not installed, is already at the
offered version, or where the repository offers an **older** build than the
NAS ships (seen live with File Station on DSM 7.3) — a version difference is
never treated as permission to downgrade. When the catalog lists both a
stable and a beta build, the stable one is used. New dependencies of the
offered version become ordered install steps before the target. A package
update has no supported downgrade path, so the plan is always **high risk**.
Completion is confirmed when the inventory reports the offered version; the
package's run state is restored (`run_after_install` follows the observed
running state). MCP: `plan_package_update` +
`apply_package_install_plan`; the read-only gateway strips both.

Live-verified on DSM 7.3: PHP 8.2 updated 8.2.28-0107 → 8.2.30-0170 with the
inventory confirming the new version, and the downgrade guard refusing the
File Station 1.4.3-2210 → 1.4.3-1610 "update".

## Local (manual) install

Install a package from a local `.spk` file instead of the online repository —
Package Center's "Manual Install". Like the online install it is **high risk**
(it uploads and runs third-party code) and gated by the same hash-bound
plan/apply:

```console
dsmctl package install --spk ./mypackage.spk --volume /volume1 --nas office
dsmctl package install --spk ./mypackage.spk --volume /volume1 --nas office --approve <hash-from-plan>
```

Unlike the online plan, the local plan is **bound to the exact file content**
(byte size + SHA-256) rather than a catalog entry, so apply refuses a `.spk`
that changed since planning. Apply uploads the file
(`SYNO.Core.Package.Installation` `upload`, multipart, reusing the same
streaming transport as FileStation uploads), then installs from the uploaded
temp file (`install`, referencing it by the returned task id — or its path as a
fallback) and confirms completion against the installed-package inventory; a
failed install cleans up the uploaded temp file. When DSM already has the same
package installed, DSM performs an upgrade instead, and completion is confirmed
against the uploaded package's version. Pass `--allow-unsigned` to install a
package that is not signed by Synology (or a trusted publisher); without it DSM
enforces its code-signature policy. `--volume` (target install volume) and
`--start` (start after install, default on) are shared with the online install.

MCP: `plan_package_local_install` and `apply_package_local_install_plan`; the
read-only gateway strips both, and the remote gateway's high-risk approval flow
applies.

## Deferred operations

Writing the **trust level**, **beta channel**, and **default install volume** is
also not supported: trust level has no DSM write endpoint, and the beta channel
(base `Setting.set` `update_channel`) and default volume
(`SYNO.Core.Package.Setting.Volume.set`) are separate follow-ups. Per-package
application-specific settings are deferred too.

## DSM backends (verified on DSM 7.3)

The API names and fields are verified against DSM 7.3-81168:

- Inventory: `SYNO.Core.Package` `list` v2 with
  `additional=["status","beta","startable","install_type"]`. That API rejects the
  whole request (error 120) if any requested key is unknown; `stoppable`,
  `removable`, and `installing` are **not** valid keys. `status`, `beta`,
  `startable`, and `install_type` are returned inside each package's `additional`
  object. `startable` marks a package that exposes a start/stop control (not one
  that can start right now), so `can_stop` is `startable && running`, `can_start`
  is `startable && not running`, and `can_uninstall` is `install_type != system`.
- Settings: `SYNO.Core.Package.Setting` `get`/`set` v1. `trust_level` is an
  integer (0/1/2, read-only); `enable_autoupdate` is the master auto-update
  toggle, with `autoupdateimportant` / `autoupdateall` selecting important-only vs
  all. The `set` write applies the auto-update fields even though its response
  echoes only the notification/channel fields (verified live); it silently
  ignores `trust_level`, which is why trust is read-only.
- Start/stop: `SYNO.Core.Package.Control` `start`/`stop` with `id` (verified live;
  DSM refuses to stop packages required by others, surfaced as an error).
- Uninstall: `SYNO.Core.Package.Uninstallation` `uninstall` with `id`.
- Catalog: `SYNO.Core.Package.Server` `list`, merging the `packages` and
  `beta_packages` arrays with the download link, checksum, size, and `deppkgs`.
- Install: `SYNO.Core.Package.Installation` method **`install`** (not
  `download`, which returns code 103 on DSM 7.3) with `name`, `url`,
  `checksum`, `filesize`, `volume_path`, `beta`, `blqinst`,
  `installrunpackage`. It returns a download `taskid` whose id changes between
  the download and install phases, so completion is confirmed against the
  inventory; `blqinst: true` performs the headless quick install. Verified
  live installing Synology Photos 1.9.1 (WI-029).

Reads decode every optional field defensively, and each mutation operation gates
on `SYNO.API.Info` discovery: if a target does not advertise its API, that
operation reports unsupported and fails closed instead of issuing a wrong
request. Confirm the selected backends on any target with
`dsmctl package capabilities`.
