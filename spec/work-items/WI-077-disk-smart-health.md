---
id: WI-077
title: Disk SMART and health
status: in_progress
priority: P2
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/disksmart
  - internal/synology/operations/disksmart
  - internal/synology/disksmart.go
  - internal/runtime/manager.go
  - internal/application/disksmart.go
  - internal/cli/disksmart.go
  - internal/mcpserver/server.go
  - docs/disk-health.md
---

# WI-077 — Disk SMART and health

## Outcome

A CLI user or MCP agent can read Storage Manager → HDD/SSD per-disk health and
the full SMART attribute table for each installed drive, and, through the
hash-bound plan/apply contract, start a SMART self-test on a specific disk and
manage the recurring SMART-test schedule under guardrails. This is a focused,
typed module in the sense of [WI-006](WI-006-control-panel-modules.md): one
module per DSM feature area, never a generic `set key=value` proxy over
`SYNO.Storage.CGI.*`.

The module deliberately complements — and does not overlap — the existing
storage inventory read (`SYNO.Storage.CGI.Storage.load_info` in
`internal/synology/operations/storageinventory`). That module surfaces only a
coarse per-disk `SMARTStatus` string and a temperature reading; it carries no
attribute table, no self-test state, and no bad-sector/lifespan detail. WI-077
owns the per-disk SMART and health surface those disk fields intentionally stop
short of.

The API map, versions, methods, and field names below are the author's current
best knowledge and **must be live-verified at implementation time**: the
standing policy is that source-doc and mobile/desktop-client field names are
frequently stale, so each API/method/field is to be confirmed against the lab
(DS3018xs) with a throwaway read-only `DSMCTL_DUMP` probe before it is trusted
(see [[dsm-webapi-live-verify-fields]]).

## Scope

Sliced read-first, then guarded write, so the read slice ships independently.

### Slice A — read-only (independently shippable)

All API/method/field names to be live-verified against the lab before trusting.

- **Disk list + health (`SYNO.Storage.CGI.HddMan`, likely v1):** enumerate
  installed drives with a stable disk identity and health-lifecycle detail that
  the storage inventory does not carry — health status, bad-sector /
  remapped-sector count, estimated lifespan / wear (for SSDs), current
  spin-down / hibernation state, firmware/model, and interface/bay location.
  Likely method `list` / `enum` (to be confirmed; may be a single `get`).
- **SMART attributes (`SYNO.Storage.CGI.Smart`, likely v1):** per-disk SMART
  attribute table — attribute id, name, current/worst/threshold values, raw
  value, and pass/fail/prefail flag — plus the current self-test status
  (idle / running with percent-remaining / last-result / last-run time) and the
  configured test schedule read-back. Likely `get` / `get_smart_attrs` keyed by
  disk id (to be confirmed).
- **Graceful per-disk absence.** Disks that expose no SMART data (many USB
  drives, some NVMe/SATADOM/M.2 devices) are reported as "no SMART data" rather
  than erroring the whole read; a NAS with zero eligible disks yields an empty,
  successful state.

### Slice B — guarded write (plan/apply, hash-bound)

- **Run a SMART self-test** — `SYNO.Storage.CGI.Smart` `run_test` (method and
  test-type encoding to be live-verified; likely a `type` of `quick` /
  `extended` or an integer enum). Keyed by the observed disk id. Long-running
  and asynchronous: the plan targets one disk; apply starts the test and the
  postcondition re-read confirms the disk now reports a running/queued test of
  the requested type.
- **Set / clear the SMART-test schedule** — `SYNO.Storage.CGI.Smart`
  `set_schedule` (to be verified; fields likely frequency + day-of-week/month +
  hour + enable, possibly per-disk or system-wide). Persistent config; apply
  merges the patch into a freshly read schedule and re-reads to confirm.

## Non-goals

- **Spin-down / hibernation and disk power-management *writes*.** HDD
  hibernation timers, disk deep-sleep, and power-behavior toggles change device
  power state and belong in a dedicated power/hardware module; WI-077 reports
  spin-down state read-only and does not write it.
- **Stopping / cancelling a running self-test.** Deferred to a Slice-B
  follow-on once the run/postcondition path is proven; cancellation has its own
  postcondition (test returns to idle) and its own live-verify need.
- **Disk deactivation / secure-erase / drive replacement / bad-sector repair.**
  These are destructive drive-lifecycle actions (some overlap
  `storagepoolmutation` / `volumemutation`) and are explicitly out of scope.
- **Pool/volume/cache health rollups.** Aggregate pool and volume health stay
  with the storage inventory, pool, volume, and SSD-cache modules
  ([WI-002](WI-002-storage-pool-management.md),
  [WI-013](WI-013-ssd-cache.md)); this module is strictly per-physical-disk.
- **A generic `SYNO.Storage.CGI.*` passthrough** command or MCP tool.

## Design constraints

- **Independent compatibility boundaries per API family.**
  `SYNO.Storage.CGI.Smart` and `SYNO.Storage.CGI.HddMan` are separate API
  families with independent version selection. Each operation
  (health read, SMART-attr read, test-run, schedule-set) is a separately
  selectable backend that appears in capability reports with a stable operation
  name, selected API, and version. A NAS that advertises one but not the other
  (or neither) fails **closed** for the missing operation — reported
  `(not supported)` — without disabling the operations that are present.
- **The guarded writes are LOW risk, and the spec says so explicitly.** A SMART
  self-test is non-destructive: it does not change external exposure, security
  posture, or data, and it cannot lock the admin out. It only consumes disk I/O
  and time (an extended test can run for hours). It therefore does **not** meet
  the HIGH-risk bar; classify the test-run as low risk (with a "safe but slow"
  caveat surfaced to the user) and the schedule-set as low risk (persistent but
  fully reversible config). This is a deliberate contrast with exposure-changing
  modules such as [WI-041](WI-041-external-access.md) — do not inflate the risk
  label where the action does not warrant it. Mutations still go through
  plan/apply per the mutation-safety contract because they are remote,
  non-local operations, not because they are dangerous.
- **Patch + postcondition.** Follow the module pattern: plan records and hashes
  the complete observed state of the target disk (identity fingerprint, health,
  current test status, current schedule); apply rejects a changed/stale state
  (disk removed, moved bay, or a test already running), merges the patch into a
  freshly read state, performs the typed operation, and re-reads to verify the
  effect actually took — DSM may silently ignore a `run_test` if the disk is
  busy (e.g. a pool rebuild in progress) or a field if the schedule shape is
  wrong. Never assume success from a `success: true` envelope alone.
- **Stable disk identity, not bay position.** The plan's resource identifier is
  the disk's stable id (device path plus a serial-derived fingerprint), so a
  disk that was pulled and reseated in a different bay between plan and apply is
  detected as stale rather than silently retargeted.
- **Identity handling instead of secrets.** SMART and health data carry no
  passwords, keys, or tokens, so `credential_ref` is not needed here. But disk
  **serial numbers** are stable hardware identifiers and must be treated as
  identity per the WI-002 evidence policy: they must not enter committed test
  fixtures or logs. Sanitize serials (and any host/model specifics that
  identify a physical unit) out of recorded request captures and decoder
  fixtures.
- **Read-only-gateway exclusion.** The read operations are available through the
  read-only gateway; the plan/apply tools (`plan_disk_smart_change` /
  `apply_disk_smart_plan`) are excluded, consistent with every other guarded
  module.
- **Impl-time live verification is mandatory before Slice B ships.** The
  `run_test` type encoding and the schedule field shape are the highest-risk
  unknowns; both require one authorized, fully reverted live run (start a quick
  self-test on a disposable disk, confirm the postcondition, let it complete or
  cancel out-of-band) before Slice B is considered done.

## Acceptance criteria

- [x] Slice A: `disk-smart capabilities|health|attributes` (CLI) and the
      matching `get_disk_smart_capabilities` / `get_disk_health` /
      `get_disk_smart_attributes` MCP tools return normalized per-disk state.
      Live-verified API map differs from the guesses above (see Evidence):
      per-disk health/bad-sector/lifespan/spin-down comes from
      `SYNO.Core.Storage.Disk.list` (not `HddMan`, which is the NAS-wide
      threshold config); the SMART attribute table + summary comes from
      `SYNO.Storage.CGI.Smart.get_health_info`; the detailed self-test status
      comes from `SYNO.Core.Storage.Disk.get_smart_test_log`. The schedule
      read-back is Slice B (`SYNO.Storage.CGI.Smart.Scheduler`).
- [x] Disks with no SMART data are reported as "no SMART data" (not an error),
      and a NAS with no eligible disks returns an empty successful state.
      (`get_health_info` returns DSM code 117 for a disk with no attribute
      table; the facade marks that disk `no_smart_data` and continues.)
- [x] Independent gating: each area selects its own backend and appears in the
      capability report with a stable operation name, API, and version; a
      missing API family fails closed for its own operations only.
- [x] Decoder unit tests cover the SMART attribute table, self-test status, and
      health/lifespan fields from sanitized fixtures with **no serial numbers**
      or physical-unit identifiers committed (fake `TESTDISK*` serials).
- [x] Slice A live verification on the lab (read-only raw probe first to pin
      real method/field names): read health + full SMART attributes for every
      installed disk, with the exact API family/version recorded in the Evidence
      note below.
- [ ] Slice B (test run): guarded hash-bound plan/apply starts a quick self-test
      on one disk; the plan carries the disk identity fingerprint and current
      test status; apply rejects stale disk state and a test-already-running
      state; the postcondition re-read confirms the test is running/queued;
      classified **low** risk with a "safe but slow" caveat; the read-only
      gateway excludes the plan/apply tools.
- [ ] Slice B (schedule): plan/apply sets and clears the SMART-test schedule
      as a patch with a postcondition re-read; unspecified schedule fields are
      never silently reset.
- [ ] Slice B live verification (authorized, fully reverted): a quick self-test
      round-trip through plan/apply on a disposable disk with postcondition
      proof, and the real `run_test` type encoding + schedule shape recorded,
      replacing the to-be-verified guesses above.
- [ ] CLI and MCP invoke the same application methods; capability output
      describes each read and write operation independently.

## Verification

- Decoder + request-capture unit tests; `go test ./... -count=1`,
  `go vet ./...`, `git diff --check`.
- Live reads allowed on the explicitly configured lab NAS (authenticated
  session; a throwaway `DSMCTL_DUMP` probe pins the real API/method/field names
  before any code trusts them).
- Live mutation requires explicit per-session authorization on a disposable
  disk: a quick (not extended) self-test, fully reverted, is the minimum needed
  to confirm the `run_test` and schedule wire shapes.
- Primary field sources to check on codesearch (branch to match the lab DSM):
  `webapi-Storage` conf/handlers for `SYNO.Storage.CGI.Smart` and
  `SYNO.Storage.CGI.HddMan`, plus the NAS-local Storage Manager assets
  (`storage_panel.js` / the HDD/SSD-management and SMART-test dialogs) for
  request assembly. Treat all of it as stale until live-verified.

## Coordination

- New domain package `internal/domain/disksmart`, operation package
  `internal/synology/operations/disksmart`, and facade
  `internal/synology/disksmart.go`; registered in `internal/runtime/manager.go`
  alongside the other storage facades.
- Read boundary with `internal/synology/operations/storageinventory`: that
  module keeps its coarse per-disk `SMARTStatus`/temperature fields; WI-077 owns
  the attribute table, self-test control, and bad-sector/lifespan detail. Do not
  duplicate or move the inventory's disk fields.
- No overlap with the pool/volume/SSD-cache mutation modules
  ([WI-002](WI-002-storage-pool-management.md),
  [WI-003](WI-003-volume-management.md), [WI-013](WI-013-ssd-cache.md)) beyond
  reading the same physical disks; this module performs no pool/volume topology
  change.

## Evidence

Slice A live-verified against the lab (DS3018xs, DSM 7.3, 6 disks: 2 SATA SSD +
4 SATA HDD) with a throwaway read-only raw probe. The proposed API map above was
partly stale; the shipped read slice uses:

- **Per-disk health / lifespan / coarse test state — `SYNO.Core.Storage.Disk`
  v1 `list`** (params `{offset:0, limit:-1}`, JSON request). Returns
  `{disks:[…]}` with, per disk: `id`, `device` (`/dev/sdX`), `name`/`longName`,
  `model`, `firm`, `serial`/`ui_serial`, `vendor`, `diskType` (bus), `isSsd`,
  `slot_id`, `disk_location`, `container{str,type}`, `size_total`, `temp`,
  `status`, `overview_status`/`drive_status_key`/`summary_status_key`,
  `smart_status`, `smart_test_support`, `smart_testing`, `testing_type`,
  `remain_life{trustable,value}` (value `-1` = not applicable, e.g. HDDs),
  `remain_life_danger`, `below_remain_life_thr`, `sb_days_left`(+`_critical`/
  `_warning`), `unc` (`-1` = unavailable). This is the per-disk health authority
  the storage inventory (`SYNO.Storage.CGI.Storage.load_info`) stops short of.
- **SMART attribute table + summary — `SYNO.Storage.CGI.Smart` v1
  `get_health_info`** (params `{device:"/dev/sdX"}`, JSON request — the value
  must be the full device path and JSON-encoded; a bare form value or the short
  `sda` id returns code 117/114). Returns `{healthInfo:{count, history{…},
  overview{smart,smart_info,smart_test,remain_life_attr,remain_life,isNVMeDisk,
  isSsd,…}, smartInfo:[{id,name,current,worst,threshold,raw,status}]}}`.
  `smartInfo` is the attribute table; every field is a string (values are
  zero-padded, raw values can be composite like `0/0`). A disk with no attribute
  table answers with **DSM error code 117** → reported as `no_smart_data`.
- **Detailed self-test status/log — `SYNO.Core.Storage.Disk` v1
  `get_smart_test_log`** (params `{device:"/dev/sdX"}`). Returns
  `{latest_test_time, testInfo:[{device, latest_test_result, latest_test_type,
  testing, remain, quickTime, extendTime}]}`.
- **Global disk-health warning thresholds — `SYNO.Storage.CGI.HddMan` v1
  `get`** (no params). Returns `{BadSctrThrEn, RemainLifeThrEn, RemainLifeThrVal,
  SBMonthLeftThrEn, SBMonthLeftThrVal, WddaEn, healthReportEn, …}`. NAS-wide, not
  per-disk.

Method/param names were confirmed against the on-NAS Storage Manager JS
(`/webman/3rdparty/StorageManager/storage_panel.js`, found via the desktop
initdata) after the raw-probe error-code sweep; codesearch was OAuth-blocked.

Slice-B wire shapes observed for the follow-on (not implemented): test run =
`SYNO.Core.Storage.Disk` `do_smart_test` (`{device, type}`, `type:"stop"` to
cancel); schedule = `SYNO.Storage.CGI.Smart.Scheduler` (`get`/`set`/`list`/
`delete`/`run`/`change_state`).

## Handoff

Fill this only when pausing incomplete work.
