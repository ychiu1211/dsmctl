---
id: WI-085
title: DSM OS installation on never-installed hardware (online-first)
status: in_progress
owner: "claude"
depends_on: [WI-023]
parallel_group: H
touches:
  - internal/synology/findhost (install-state driving)
  - internal/provision (install stage, shared core)
  - internal/application
  - internal/cli
  - internal/gateway/admin
---

# WI-085 — DSM OS installation on never-installed hardware (online-first)

## Outcome

dsmctl can take a discovered device that has **no DSM installed**
(`not_installed` / junior/installer response), drive DSM installation, and wait
until the device reaches fresh-setup `ready`, at which point WI-083 creates the
first administrator. This is the "brand-new hardware out of the carton" stage
(2026-07-21 decision: provisioning scope includes OS install, not only
first-admin creation).

## Install source: online-first (2026-07-21 decision)

The **primary and default path is online install**: the device downloads the
recommended DSM build from Synology over its own network connection, driven by
the DSM web assistant's install flow — dsmctl/MCP only triggers it and polls.
This means **MCP/the gateway never uploads a large `.pat` image**, which is the
user's explicit preference and is materially simpler and lower-risk than an
image transfer through the gateway. A local `.pat`-upload path is a **deferred,
optional** secondary source (offline / air-gapped installs), not required for
version 1 and possibly never exposed over MCP. Like everything else, the
install itself is a shared application-layer operation reusing the
`internal/provision` core with thin CLI / MCP / Admin-UI adapters — not a
duplicate implementation.

## Shipped + live-verified (2026-07-21)

The online-install protocol was reverse-engineered from the Web Assistant and
**live-verified end to end on a DS918+** (see [[dsm-web-assistant-install-api]]).
Shipped: `internal/provision/install.go` (`GetState`, `InstallOnline`,
`GetInstallProgress`, `Pingpong`, `DeviceState.OnlineInstallPlan`) + a thin CLI
`dsmctl install` (detect-only by default; `--install --yes` performs it, gated by
a serial-confirmation prompt) with unit tests against the captured JSON shapes.

Endpoints (all `/webman/*.cgi`, no password, http or https):
- Detect: `get_state.cgi` → `dsinfo.status` (`not_install`/`sys_crash`/`sys_migrat`)
  + `internet_install_ok`/`internet_reinstall_ok` + `internet_*_version`.
- Install: `POST install.cgi?upload=false&status=<S>&localinstallreq=false` (online).
- Progress: `get_install_progress.cgi` (`{stage, progress}`; progress is a string).
- Completion: DSM answering `SYNO.API.Info` (JSON) at the https setup URL, because
  the assistant stops serving once DSM takes the port.

**Offline path also shipped:** when the DEVICE has no internet, `dsmctl install`
downloads the `.pat` matching the device's own flash build from Synology on the
HOST (`provision.DefaultPatURL` →
`…/release/<ver>/<build>/DSM_<model>_<build>.pat`) and uploads it via
`provision.InstallLocal` (single multipart POST, field `filename`). Critical
gotcha (live-verified): `install.cgi` rejects a chunked upload with
`error_upload`, so the body is sent with an exact `Content-Length`. `--pat <file>`
uses a local image instead. Documented as the `nas-install` Claude skill.

Live runs (DS918+): `192.0.2.255` (sys_crash, online) → online-reinstalled DSM
7.3.1-86003 → provisioned admin `testuser` (password in Windows Credential
Manager). `192.0.2.51` (not_install, no device internet) → host auto-downloaded
DSM 7.3.2-86009 and offline-installed it (after fixing the Content-Length upload)
→ provisioned admin `testuser`. (Note: a stray concurrent `admin`/empty setup login
makes `User.create` return 105; run provision against a clean setup session.)

Follow-ons shipped this session:
- **Combined install → first-admin**: `dsmctl install --admin-user <user>` creates
  the first administrator after DSM comes up (a shared `runFirstAdmin` helper used
  by both `dsmctl install` and `dsmctl provision`), password to the OS vault.
- **`sys_migrat`**: recognized and mapped (migrate preserves data — the confirm
  prompt no longer says "erase"); marked not-live-verified (no migrated-disk
  hardware).
- **Gateway/MCP surface**: `Service.InstallDiscoveredNAS` + MCP tool
  `install_discovered_nas` (nas.provision scope, LAN-only, read-only-stripped,
  fire-and-forget online trigger; offline `.pat` stays CLI-only). Unit-verified;
  live remote verify pending.

Still pending: live-verifying `sys_migrat` and the gateway/MCP install path
against real hardware.

## Why this is its own item (not part of WI-083)

WI-083 begins once DSM is already installed and in fresh-setup state. Installing
the OS itself is a categorically larger, unresearched flow: a reboot, a
multi-minute install with progress polling, and a findhost/HTTP state machine
(`not_installed` → `installing` → `booting` → `ready`). It was an explicit
non-goal of the original WI-083 and has **zero existing research or code** in
this program. Keeping it separate lets WI-083 ship the researched first-admin
path without blocking on the heavier installer.

## Scope (to be researched, then specced concretely)

- **Online install (primary)**: trigger the device's own DSM download+install
  from Synology and poll to completion; no image passes through dsmctl/gateway.
- Drive the install: install trigger, reboot, and progress polling through the
  reported install states until fresh-setup `ready`.
- Surface progress and terminal outcome in CLI + Admin UI; hand off to WI-083.
- **Local `.pat` upload (deferred/optional)**: for offline installs only; the
  operator supplies the image, with a model/platform-match precheck that fails
  closed before any transfer. Not required for version 1.

## Non-goals

- Hosting or choosing DSM builds (online install uses Synology's recommended
  build); migration/recovery of an existing DSM (`migratable` / `recoverable`
  states are out of scope here).
- Uploading a `.pat` through MCP (the online path avoids uploads entirely; the
  optional offline upload path, if built, may stay local-admin-only).
- Any credential handling (that is WI-083/WI-084).
- Fan-out install across multiple devices in one call.

## Design constraints

- This is a **destructive, ownership-taking** operation (it writes the OS to the
  device's disks). It is very high risk, guarded plan/apply, and — over MCP —
  gated by the WI-086 authorization amendment exactly like WI-083.
- The install protocol must be reverse-engineered and **live-verified on real
  never-installed hardware** before `status: ready`; do not trust reference docs
  (see [[dsm-webapi-live-verify-fields]]).
- WI-063: install operations carry DSM-version/model ranges; out-of-range fails
  closed.

## Acceptance criteria

- [ ] A `not_installed` device installs DSM **online** end to end and reaches
  fresh-setup `ready`, then WI-083 provisions its first admin — with no image
  uploaded through dsmctl/gateway.
- [ ] Install progress and terminal success/failure are reported in CLI + UI.
- [ ] Interrupted/failed installs fail closed with actionable state, never a
  silent partial success.
- [ ] (If the optional offline path is built) a model/platform-mismatched `.pat`
  is refused before transfer.

## Verification

- Request-capture/unit tests for the online install state machine.
- One live online install on disposable never-installed hardware (explicit
  authorization required for that exact destructive test).

## Coordination

Depends on WI-023 (discovery states). Feeds WI-083 (first-admin). Shares the
`internal/provision` core and Admin UI wizard with WI-083 (thin adapters, no
duplicate implementation).

## Open questions

- The **online** DSM install WebAPI/flow (trigger + progress polling on a
  never-installed device's web assistant) is unresearched — a discovery spike
  against a never-installed unit is the prerequisite to a concrete spec.
- Does install belong on the remote MCP surface at all, or should the OS-install
  stage stay local-admin-only even if first-admin (WI-083) is remote-exposed?
  (Online install needs no upload, which makes remote exposure more tractable.)
