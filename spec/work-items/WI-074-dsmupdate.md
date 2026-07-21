---
id: WI-074
title: DSM Update & Restore module (update status, auto-update policy, config backup)
status: in_progress
priority: P2
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/dsmupdate
  - internal/synology/operations/dsmupdate
  - internal/synology/dsmupdate.go
  - internal/runtime/manager.go
  - internal/application/dsmupdate.go
  - internal/cli/dsmupdate.go
  - internal/mcpserver/server.go
  - docs/control-panel.md
---

# WI-074 — DSM Update & Restore module (update status, auto-update policy, config backup)

## Outcome

A CLI user or MCP agent can read the Control Panel → Update & Restore surface —
the installed DSM version and whether an update is available, the update-server's
offered version and its restart/criticality flags, the DSM auto-update policy,
and the scheduled configuration-backup settings — and, through the hash-bound
plan/apply contract, change the low-blast-radius policy settings (the auto-update
policy and the config-backup schedule) under guardrails, plus download a
configuration-backup bundle. This is a focused Control Panel module in the sense
of [WI-006](WI-006-control-panel-modules.md): one typed module per DSM setting
area, never a generic `set key=value` proxy.

Installing a DSM update and restoring a configuration backup are deliberately
**not** part of this WI (see Non-goals). They are a different order of risk from
a package install: a DSM update reboots the entire NAS and rewrites firmware, and
a config restore overwrites the whole system configuration and can lock the admin
out. The package-upgrade precedent ([WI-029](WI-029-package-install-update.md))
shipped an install/update write only because it is per-package, session-survivable,
and inventory-verifiable; neither DSM-update install nor config restore has those
properties, so both are deferred with reason rather than shipped guarded.

The API families, versions, and field names below are the author's best current
knowledge and **must be live-verified at implementation time** against the lab
with a throwaway `DSMCTL_DUMP` probe before being trusted — the standing policy
is that source-doc and mobile-client field names are frequently stale (see
[[dsm-webapi-live-verify-fields]]). The likely families are `SYNO.Core.Upgrade`,
`SYNO.Core.Upgrade.Server`, `SYNO.Core.Upgrade.Setting` (and the related
download-policy setter), and `SYNO.Core.ConfigBackup`.

## Scope

Sliced read-first, then guarded write, so the read slice can ship independently.

### Slice A — read-only (independently shippable)

- **Update status:** `SYNO.Core.Upgrade` `status` (and `check`) → the installed
  DSM version/build, whether an update has been detected, whether an update has
  already been downloaded and is install-ready, and any in-progress
  download/install state. Note that `check` contacts Synology's update server; it
  is side-effect-free on the NAS but is a network egress, so the read must treat a
  reachability failure as `(unknown)` rather than erroring the module.
- **Available version (offered update):** `SYNO.Core.Upgrade.Server`
  `check`/`get` → the offered version string and build number, the
  major/minor/micro decomposition, whether the update **requires a restart**, its
  **criticality/type** (normal vs. important/security), availability for this
  model, and the release-note reference. These flags are exactly what a caller
  needs to decide whether a human should be present for an install.
- **Auto-update policy (read):** `SYNO.Core.Upgrade.Setting` `get` (and the
  related download-policy getter) → whether automatic update is enabled, its mode
  (off / notify-only / download-only / important-security-only / download-and-
  install-all), the scheduled day/hour of the maintenance window, and the
  notification setting. This is the DSM-firmware analog of the Package Center
  auto-update policy in [WI-020](WI-020-package-settings-write.md); the exact
  field/enum encoding must be live-verified because Update & Restore uses its own
  setter, not the package one.
- **Configuration-backup settings (read):** `SYNO.Core.ConfigBackup` `get` → the
  scheduled config-backup state (enabled/disabled, schedule, and destination —
  local path vs. Synology account/C2 cloud), the last-backup timestamp, and the
  backup history if the API exposes it. Destination account tokens/credentials
  are never surfaced by the display model.
- **Configuration-backup export (download):** the local config-export transfer
  (historically a file-streaming `SYNO.Core.ConfigBackup` export path; confirm the
  exact method/transport live) streamed to a caller-named local file. This is a
  read-side transfer, modeled after the FileStation download and Drive log-export
  binary transports ([WI-049](WI-049-file-station.md),
  [WI-059](WI-059-drive-log-export.md)); the exported `.dss` bundle is sensitive
  (it contains system configuration) and must be handled under the secret-hygiene
  rules below. Like those transfers it is **excluded from the read-only gateway**.

### Slice B — guarded write (plan/apply, hash-bound)

- **Auto-update policy write** — `SYNO.Core.Upgrade.Setting` `set` (plus the
  download-policy setter if the mode is split across two APIs). Patch-only
  ownership: the plan records and hashes the complete observed policy, apply
  rejects a changed state, merges the patch into a **freshly read** policy,
  submits the full policy, and **re-reads to verify** the fields actually took
  effect (DSM silently ignores some fields — the recurring lesson; the WI-020
  auto-update setter already echoes only a subset of what it accepts).
- **Configuration-backup schedule write** — `SYNO.Core.ConfigBackup` `set` for
  the scheduled-backup policy (enable/disable, schedule, and — where applicable —
  destination selection). Same patch-only, stale-rejecting, postcondition-re-read
  contract. Any destination that carries an account/cloud credential uses
  `credential_ref` (see Design constraints); the credential never enters the
  request preview, plan, or hash.

## Non-goals

- **Installing a DSM update (deferred, HIGH risk).**
  `SYNO.Core.Upgrade` download + `install`/apply (and the reboot it triggers) is
  out of scope for this WI. Unlike a package install/update
  ([WI-029](WI-029-package-install-update.md)), a DSM firmware update reboots the
  **entire NAS**, taking the management plane offline, so the plan/apply
  postcondition cannot be verified in the same session (the session dies on
  reboot); the change is not cleanly reversible (no in-place rollback to the prior
  DSM build), and a failed update can require physical/Assistant recovery. The
  intent may be modeled, but the apply is deferred until a reboot-aware,
  reconnect-and-verify install contract exists and explicit per-session
  authorization is obtained. This goes one step further than the package-upgrade
  precedent, which shipped guarded; here the safe move is to defer the apply
  entirely and document why.
- **Configuration restore / import (deferred, HIGH risk).**
  `SYNO.Core.ConfigBackup` import/restore overwrites the system configuration
  wholesale — accounts, network, services — and can lock the administrator out or
  cut the connection dsmctl is using; it is not cleanly reversible and its
  postcondition is unverifiable (the read surface it would confirm against is the
  very thing being replaced). Deferred with an explicit reason, like the DSM-update
  install.
- **DSM downgrade and manual `.pat` upload install.** Uploading and applying a
  specific DSM `.pat` firmware image (the manual-update path) is a superset of the
  deferred install with the same reboot/irreversibility profile.
- **Update rollback / "undo an update".** DSM does not offer a supported in-place
  DSM-version rollback API; not modeled.
- A generic `update set key=value` command. Only the typed auto-update-policy and
  config-backup-schedule patches are exposed.

## Design constraints

- **Independent compatibility boundaries.** DSM update (the `SYNO.Core.Upgrade*`
  family) and configuration backup (`SYNO.Core.ConfigBackup`) are separate API
  families and separate failure boundaries. A NAS or DSM build missing one (for
  example a config-backup API that is not advertised) must leave the other usable,
  reported `(not supported)` rather than erroring the whole module. Within the
  update family, `SYNO.Core.Upgrade.Server` (network-facing offer check) and the
  local status/setting APIs are independently selectable so a blocked update-server
  reach does not blank out the local status read.
- **Every write is at least security/availability-relevant.** The auto-update
  policy governs whether DSM will install firmware — including security updates —
  and whether it will **reboot unattended** at the scheduled window. Classify a
  policy change that enables unattended download-and-install, or that disables
  automatic installation of important/security updates, as **HIGH risk** (it
  either schedules an unattended reboot or reduces the NAS's security posture).
  Lower-impact fields (notify-only, download-but-do-not-install, changing the
  window time) are medium. The config-backup schedule write is medium when the
  destination is local; a change that begins pushing the system configuration to
  an off-box destination (Synology account/C2 cloud) is HIGH because it exports
  sensitive configuration off the NAS.
- **Secrets never enter requests/plans/hashes/logs/MCP args.** A config-backup
  destination that authenticates to Synology account/C2 or a remote target uses
  the existing `credential_ref: env:NAME` mechanism, resolved at apply time and
  absent from the request preview, plan, hash, result, and logs. Config-backup
  destination account tokens and any update-account token are never surfaced by
  display models. The **exported `.dss` config bundle is itself sensitive**: it is
  streamed to a caller-named file and never buffered into plans, results, logs, or
  MCP tool arguments, and transfer errors must redact `_sid`/`SynoToken` as the
  FileStation/Drive transports already do.
- **Patch + postcondition.** Both writes follow the module pattern: plan records
  and hashes the complete current state, apply rejects a changed state, merges the
  patch into a freshly read config, submits the full typed object, and re-reads to
  verify the requested fields took effect — never reporting a false success off a
  setter whose response echoes only a subset of fields.
- **Capabilities + per-operation backend selection, fail-closed.** Every
  operation (`dsmupdate.status.read`, `dsmupdate.available.read`,
  `dsmupdate.policy.read`, `dsmupdate.policy.set`, `dsmupdate.configbackup.read`,
  `dsmupdate.configbackup.export`, `dsmupdate.configbackup.schedule.set`) appears
  in capability reports with a stable operation name, selected backend, API, and
  version; a write whose setter API/version is absent reports the capability
  `false` and fails closed rather than attempting a raw call.
- **Field/version/method uncertainty is real.** The auto-update policy enum
  encoding (how off / notify / download-only / important-only / all map to DSM
  fields, and whether the policy is split between `SYNO.Core.Upgrade.Setting` and a
  download-policy API), the `SYNO.Core.Upgrade.Server` restart/criticality field
  names, and the config-export transport (method vs. cgi file GET) must each be
  confirmed with a throwaway `DSMCTL_DUMP` read before Slice A ships, and any write
  confirmed with one authorized, fully-reverted live plan/apply before Slice B
  ships (the WI-041 relay write shipped with the wrong setter method until a live
  apply caught it — do not ship a write here without a live round-trip).

## Acceptance criteria

- [x] Slice A: `dsm-update capabilities|status|available|policy|config-backup`
      (CLI, shipped as the `dsm-update` command) and the matching `get_dsm_update_*`
      MCP tools return normalized state; no update account token or config-backup
      destination credential appears in any output (unit test asserts the decoded
      state carries no `pwd`; live `--json` grep confirms it). The config-backup
      destination **account identifier** is surfaced (like an SMTP auth user); only
      the destination password is suppressed.
- [x] Independent gating: the update family and the config-backup family select
      their own backends; a missing/blocked config-backup API or an unreachable
      update server does not disable the other reads, and each absent area reports
      `(not supported)`/`(unknown)` rather than erroring the module.
- [~] The `available` read surfaces the offered version, the **restart-required**
      flag, and the **criticality/type**. Live-partial: no update was pending on
      the lab to type these, so per the no-guessed-decoder rule they are surfaced
      verbatim under `details` by their raw DSM key (typing them as first-class
      fields is a follow-on once a pending update can be captured live).
- [ ] Config-backup export streams the `.dss` bundle to a caller-named file
      without buffering it into plan/result/log/MCP output; transfer errors redact
      `_sid`/`SynoToken`; the export tool is excluded from the read-only gateway.
      (Not in this read pass; the config-export transport is a follow-on.)
- [ ] Slice B (auto-update policy): guarded hash-bound plan/apply toggles the
      auto-update policy with a request-capture test and a postcondition re-read;
      a policy change enabling unattended install or disabling automatic
      security-update installation is classified HIGH; a no-op patch and an
      unwritable/ignored field are rejected; the read-only gateway excludes the
      plan/apply pair.
- [ ] Slice B (config-backup schedule): guarded hash-bound plan/apply changes the
      scheduled config-backup policy with postcondition proof; an off-box
      destination change is HIGH; any destination credential uses `credential_ref`
      and is provably absent from the request/plan/hash/log.
- [x] Deferral is explicit: DSM-update **install** and config **restore/import**
      are documented as deferred HIGH-risk items with the reboot-unverifiable /
      irreversible / admin-lockout rationale, and no install or restore apply path
      is exposed on any surface (CLI, MCP, or gateway).
- [x] Live Slice-A verification on the lab NAS: read status, available version
      (with restart/criticality flags), auto-update policy, and config-backup
      settings, with no token leak. (Config-bundle export is deferred to the
      export follow-on; all four reads live-verified on DSM 7.3-81168.)
- [ ] Live Slice-B verification on the lab NAS (authorized, fully reverted): an
      auto-update-policy plan/apply round-trip and a config-backup-schedule
      plan/apply round-trip, each with a postcondition-verified change and revert
      to the original state.

## Verification

- Decoder + request-capture unit tests for both read families and both setters;
  `go test ./... -count=1`, `go vet ./...`, `gofmt`, CLI and MCP builds.
- Live reads allowed on the explicitly configured lab NAS (`SYNO.API.Info`
  advertisement + authenticated session reads); the update-server `check` is a
  network egress and its failure is treated as `(unknown)`.
- Live writes require explicit per-session authorization and are fully reverted:
  the auto-update-policy and config-backup-schedule round-trips are safe because
  they do not reboot the NAS; **no DSM-update install and no config restore is
  ever run** as part of this WI.
- Source of truth for fields (to be confirmed live, not trusted as-is): the DSM
  WebAPI conf + handlers for `webapi-Upgrade` / `SYNO.Core.Upgrade*` and
  `SYNO.Core.ConfigBackup`, cross-checked with a `DSMCTL_DUMP` probe on the lab.

## Coordination

- New Control Panel module in parallel group C, depending on the module pattern
  from [WI-006](WI-006-control-panel-modules.md). New operation package under
  `internal/synology/operations/dsmupdate` and domain model under
  `internal/domain/dsmupdate`; facade `internal/synology/dsmupdate.go` registered
  in `internal/runtime/manager.go`; application in
  `internal/application/dsmupdate.go`; thin CLI in `internal/cli/dsmupdate.go` and
  thin MCP tools in `internal/mcpserver/server.go`. No overlap with the other
  Control Panel modules beyond the shared facade/registry.
- Reuses the config-export binary transport pattern from the FileStation download
  ([WI-049](WI-049-file-station.md)) and the Drive log export
  ([WI-059](WI-059-drive-log-export.md)) and their gateway-exclusion of content
  transfer; reuses the auto-update-policy plan/apply shape from
  [WI-020](WI-020-package-settings-write.md).
