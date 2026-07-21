---
id: WI-075
title: Hardware, power and UPS
status: in_progress
priority: P3
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/hardware
  - internal/synology/operations/hardware
  - internal/synology/hardware.go
  - internal/application/hardware.go
  - internal/cli/hardware.go
  - internal/mcpserver/server.go
  - internal/mcpserver/read_only.go
  - docs/control-panel.md
---

# WI-075 — Hardware, power and UPS

## Outcome

A CLI user or MCP agent can read the Control Panel → Hardware & Power surface —
beep-control events, fan-speed mode, LED brightness, the power on/off schedule,
the power-recovery behavior after a power loss, and the UPS configuration — and,
through the shared hash-bound plan/apply contract, change those settings under
guardrails proportional to their blast radius. This is a focused Control Panel
module in the sense of [WI-006](WI-006-control-panel-modules.md): one typed
module per DSM setting area, never a generic `set key=value` proxy. The cosmetic
comfort settings (beep, LED, fan) and the settings that can physically power the
NAS off or keep it from coming back (power schedule, power recovery, UPS safe
shutdown) are the same Control Panel tab but must not carry the same risk.

## Scope

Sliced read-first, then guarded write, so the read slice ships independently.
All API names, versions, methods, and field names below are the author's best
current knowledge and **must be live-verified at implementation time** with a
throwaway `DSMCTL_DUMP` read-only probe before any decoder or setter is trusted
— the standing policy is that source-doc and mobile-client field names are
frequently stale (see [[dsm-webapi-live-verify-fields]]).

### Slice A — read-only (independently shippable)

Four independent compatibility boundaries, each selecting its own backend and
each degrading to `(not supported)` in isolation:

- **General hardware** — likely `SYNO.Core.Hardware.BeepControl` (get) for the
  per-event beep flags (fan failure, volume full/degraded/crashed, UPS, power
  events), `SYNO.Core.Hardware.FanSpeed` (get) for the fan-speed mode enum
  (e.g. full-speed / cool / quiet / low-noise, possibly split system vs CPU
  fan), and `SYNO.Core.Hardware.Led` / `…LedBrightness` (get) for LED
  brightness level or day/night schedule. Every field here is **model
  dependent**: the setter and value domain differ per hardware model and some
  fields are simply absent, so the read model records only fields the live
  `get` actually returns.
- **Power schedule** — likely `SYNO.Core.Hardware.PowerSchedule` (list/get):
  the list of power-on and power-off tasks (`enabled`, weekday mask, hour,
  minute, task type) plus whether scheduling is enabled.
- **Power recovery** — likely `SYNO.Core.Hardware.PowerRecovery` (get):
  behavior after a power loss (restore previous power state vs stay off) and,
  where present, Wake-on-LAN enable per NIC (may be a sibling
  `SYNO.Core.Hardware.WOL`).
- **UPS** — likely `SYNO.Core.ExternalDevice.UPS` (get): UPS enabled, mode
  (local USB / SNMP / network-UPS slave), attached device model and live
  battery/runtime when local, the safe-shutdown trigger (fixed time threshold
  vs "when battery reaches low"), the network-UPS-server enable and its
  permitted-slave IP allow-list, and the master IP when this NAS is a slave.
  Capability-gated on an external device actually being present.

### Slice B — guarded write (plan/apply, hash-bound)

One plan/apply pair with distinct patch scopes, each carrying its own risk
classification (see Design constraints). Plan records and hashes the complete
observed state of the touched scope; apply rejects stale state, merges the
patch into a fresh read, performs the typed set, and **re-reads to verify the
requested fields took effect** (DSM silently ignores some fields):

- **`comfort` scope** (low risk) — beep events (`SYNO.Core.Hardware.BeepControl`
  set), fan-speed mode (`SYNO.Core.Hardware.FanSpeed` set), LED brightness
  (`SYNO.Core.Hardware.Led` set). Patch-only; omitted fields never sent.
- **`power_schedule` scope** (high risk) — add/enable/disable/remove power
  on/off tasks (`SYNO.Core.Hardware.PowerSchedule` set). A power-off task can
  make the NAS unreachable at a scheduled time.
- **`power_recovery` scope** (high risk) — restore-after-power-loss behavior and
  WOL (`SYNO.Core.Hardware.PowerRecovery` set).
- **`ups` scope** (high risk) — UPS mode, network-UPS-server enable and
  allow-list, and safe-shutdown thresholds (`SYNO.Core.ExternalDevice.UPS` set).

## Non-goals

- **Immediate power actions** — shutdown, reboot, and Wake-on-LAN *now*
  (`SYNO.Core.System` reboot/shutdown and any UPS test/trigger). This module
  manages the *configuration* of hardware/power/UPS, not one-shot power verbs;
  those are a separate, even-higher-risk work item if pursued.
- **HDD hibernation / power-efficiency deep settings** (disk hibernation timer,
  memory compression, `SYNO.Core.Hardware.Hibernation` and related energy
  tuning) beyond the beep/fan/LED comfort trio — deferred to keep the first
  slice focused; they can be added as further scopes later.
- **Scheduled-task engine** (`SYNO.Core.TaskScheduler`): power scheduling here
  is only the dedicated Hardware & Power on/off schedule, not general task
  scheduling.
- **UPS device firmware, SNMP MIB discovery, or NUT driver selection** — the
  module configures the exposed UPS options, it does not manage the underlying
  Network UPS Tools stack.

## Design constraints

- **Model capability is discovered, not assumed.** Beep events, fan-speed
  values, and LED controls vary by physical model; presence is determined from
  the live `get` response, not from `SYNO.API.Info` alone. Absent fields are
  reported `(not supported)` and are never fabricated or written back. Decoders
  must fail on a malformed shape rather than silently returning empty state.
- **Independent compatibility boundaries, fail-closed.** General hardware, power
  schedule, power recovery, and UPS are separate API families and separate
  failure boundaries: a NAS missing one (no UPS attached, no LED control on the
  model) must leave the others usable, each selected per operation and reported
  in capabilities with its stable operation name, backend, API, and version.
- **Risk is asymmetric within one tab and must be classified per scope.**
  Beep/fan/LED are **low** risk (cosmetic/acoustic, reversible, no availability
  impact). Power schedule, power recovery, and UPS safe-shutdown are **high**
  risk: they can power the NAS off, keep it from returning after an outage, or
  trigger a premature safe shutdown that drops all services and risks in-flight
  data. Never classify these medium.
- **Lockout is the headline hazard — call it out in the plan.** A power-off
  schedule combined with power-recovery set to "stay off" can leave the NAS
  down with no in-band path to bring it back; disabling UPS integration removes
  safe shutdown on the next power failure. The high-risk plan summary must name
  the concrete availability/data consequence, not just "changes power settings".
- **Patch + postcondition.** Follow the module pattern: plan hashes the full
  observed scope state, apply rejects a changed state, merges the patch into a
  freshly read config, and re-reads to confirm the requested fields actually
  took effect. Empty patches are rejected in the application layer (DSM treats
  an empty set as a no-op success).
- **Secrets never enter requests, plans, hashes, logs, or MCP args.** The base
  hardware/power config carries no known secret, but if the live UPS config
  exposes any authentication material (e.g. an SNMP community string or a
  network-UPS master credential), it must use the existing
  `credential_ref: env:NAME` mechanism resolved at apply time and be absent from
  the request, plan, hash, result, and logs — confirm at impl time whether any
  such field is present and route it accordingly.

## Acceptance criteria

- [x] Slice A: `hardware capabilities|general|power-schedule|power-recovery|ups`
      (CLI) and the matching `get_*` MCP tools return normalized state with
      semantic field names; model-absent fields are reported not-supported, not
      invented.
- [x] Independent gating: each area selects its own backend and is skipped
      (null / not-supported) when its API or device is absent; a missing area
      does not disable the others; capabilities report the selected backend,
      API, and version per operation.
- [x] Slice A live verification on the DSM 7.3 lab: read beep/fan/LED, the power
      schedule, power-recovery behavior, and UPS state (including the
      no-UPS-attached path) via read-only probe.
- [ ] Slice B `comfort` scope (low risk): beep/fan/LED via guarded hash-bound
      plan/apply with request-capture test and postcondition re-read; read-only
      gateway excludes the plan/apply tools.
- [ ] Slice B power scopes (`power_schedule`, `power_recovery`, `ups`):
      classified high risk with a plan summary naming the concrete
      power-off / no-return / premature-shutdown consequence; request-capture
      tests prove patch-only field encoding; postcondition re-read confirms the
      change or reports DSM silently ignoring it.
- [ ] Any UPS authentication field discovered live is carried via
      `credential_ref` and provably absent from plan/hash/log/result (unit test
      asserts absence).
- [ ] Slice B live verification: `comfort` scope round-tripped and fully
      reverted on the lab; the high-risk power scopes verified only under
      explicit per-session authorization with out-of-band recovery access
      (see Verification).

## Verification

- Decoder + request-capture unit tests; `go test ./... -count=1`,
  `go vet ./...`.
- Live read allowed on the explicitly configured lab NAS; use a throwaway
  `DSMCTL_DUMP` probe to confirm the actual `SYNO.Core.Hardware.*`,
  `SYNO.Core.Hardware.PowerSchedule`, `SYNO.Core.Hardware.PowerRecovery`, and
  `SYNO.Core.ExternalDevice.UPS` API versions, methods, and field names before
  writing decoders/setters. Cross-check field names against the DSM Admin Center
  Hardware & Power JS and the WebAPI conf on codesearch, but treat the live
  probe as authoritative.
- **Live-mutation policy.** The `comfort` scope may be round-tripped live and
  reverted. The `power_schedule`, `power_recovery`, and `ups` scopes must **not**
  be applied live without explicit per-session authorization *and* an
  out-of-band recovery path (physical power button / console / IPMI), because a
  wrong value can power the NAS off or prevent it from returning. Prefer
  plan-only verification for the high-risk scopes; a live apply requires an
  authorized, immediately-reverted change with recovery access confirmed first.

## Coordination

Parallel group C. New operation package under
`internal/synology/operations/hardware`; shares `internal/mcpserver/server.go`,
`internal/mcpserver/read_only.go`, and `docs/control-panel.md` with the other
Control Panel modules. Depends on the [WI-006](WI-006-control-panel-modules.md)
module pattern and uses the per-operation compatibility selection
([WI-044](WI-044-dsm-compatibility-versioning.md)); no field overlap with the
external-access, time, or file-service modules beyond the shared facade and
adapter files.
