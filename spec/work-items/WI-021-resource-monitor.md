---
id: WI-021
title: Resource Monitor utilization, history, and recording toggle
status: done
priority: P2
owner: ""
depends_on: []
parallel_group: D
touches:
  - internal/domain/resmon/model.go
  - internal/synology/operations/utilizationread
  - internal/synology/operations/resmonsetting
  - internal/synology/operations/resmonsettingmutation
  - internal/synology/resmon.go
  - internal/synology/compatibility_report.go
  - internal/runtime/manager.go
  - internal/application/service.go
  - internal/application/resource_monitor.go
  - internal/cli/resource_monitor.go
  - internal/mcpserver/server.go
  - internal/mcpserver/read_only.go
  - docs/resource-monitor.md
---

# WI-021 — Resource Monitor utilization, history, and recording toggle

## Outcome

A CLI or MCP user can read DSM Resource Monitor's current utilization, read
recorded utilization history, read the history-recording setting, and turn the
history recording on or off through a guarded plan/apply mutation. Reads never
expose a raw DSM call; the recording toggle follows the shared hash-bound
plan/apply contract and is not exposed by the read-only gateway.

## Scope

Read slice (read-only module, follows the WI-018 system-log pattern):

- Current utilization via `SYNO.Core.System.Utilization` v1 `get`
  (`type=current`): normalize CPU load, memory, per-interface network throughput,
  aggregate disk I/O, and per-volume space I/O into stable semantic fields, plus
  the DSM-reported `enable_history` flag.
- Recorded history via the same API/method with `type=history`, a `time_range`
  window (DSM 7.x tokens: `week`, `month`, `half_year`, `year` — no `day`), a
  `resource` group array (cpu, memory, network, disk, space), and, for the
  per-device groups (disk/network/space), an `interfaces` device list derived
  from the current snapshot (DSM rejects them with 1057 otherwise). DSM returns
  bare evenly-spaced value arrays with no timestamps, normalized to an ordered
  `values` slice per metric/device. DSM returns error
  `WEBAPI_CORE_SYSTEM_ERR_NOT_ENABLE_HISTORY` (code 1008) when recording is off;
  surface that as a typed, actionable "history recording is disabled" error.
- Recording setting via `SYNO.ResourceMonitor.Setting` v1 `get`: normalize
  whether history recording is enabled (and retention, if DSM reports it).
- `resource.read` capability in the compatibility report.

Mutation slice (recording toggle, follows the WI-011 time-mutation pattern):

- Enable/disable history recording via `SYNO.ResourceMonitor.Setting` v1 `set`,
  patch-only on the recording-enabled field. dsmctl re-sends the observed values
  of any co-located fields (event/notification settings) unchanged and never
  resets them.
- Hash-bound plan/apply: observed-state fingerprint, approval hash, apply-time
  revalidation, and postcondition verification that the setting persisted.
- `resource.recording_set` capability in the compatibility report.

Surfaces:

- CLI `dsmctl resource-monitor` (alias `resmon`): `current`, `history`,
  `setting`, `capabilities`, `plan-recording`, `apply-recording`.
- MCP read-only: `get_resource_monitor_state`, `get_resource_monitor_history`,
  `get_resource_monitor_setting`, `get_resource_monitor_capabilities`.
- MCP mutation: `plan_resource_recording_change`, `apply_resource_recording_plan`
  (both added to `read_only.go` `RemoveTools`).

## Non-goals

- Performance/notification alarm rules and event thresholds
  (`SYNO.ResourceMonitor.EventRule`), process/connection tables, and the
  live-monitor task list.
- History retention-period changes, history export/download, and per-package
  (SMB/NFS/iSCSI/LUN/SAN) drill-down history beyond the shared device
  dimensions.
- Any wall-clock or SNMP-daemon configuration beyond the single
  history-recording toggle.

## Design constraints

- Reads reuse `lockedExecutor` and `prepareCompatibilityTargetLocked`; no
  mutation surface leaks into the read slice.
- The recording toggle is a DSM-side setting, so it uses plan/apply (not the
  local-only reversible exception). Risk is low; the plan still carries observed
  state, fingerprint, and approval hash and verifies the postcondition.
- Patch-only ownership: `set` must not silently reset event/notification fields;
  the plan reads and re-sends their observed values.
- Domain models expose stable semantic names, not raw DSM field names.
- Decoders normalize DSM responses and error on malformed shapes; they must not
  silently return an empty successful state (per architecture-contracts.md).

## Acceptance criteria

- [x] `resource-monitor capabilities` reports `resource.read`,
      `resource.recording_read`, `resource.recording_set`, and the selected
      backends.
- [x] `resource-monitor current` returns normalized CPU/memory/network/disk/
      volume utilization plus the recording-enabled flag, with `--json`
      (decode fixture-locked and live-verified against the NAS snapshot).
- [x] `resource-monitor history` returns time-ordered samples per dimension and,
      when recording is disabled, maps DSM code 1008 to a clear typed error
      (`resmon.ErrHistoryRecordingDisabled`).
- [x] `resource-monitor setting` reports whether history recording is enabled.
- [x] `plan-recording`/`apply-recording` toggle `enable_history`, revalidate the
      plan, and verify the postcondition; the mutation is absent from the
      read-only gateway surface (`read_only.go` `RemoveTools`, asserted in tests).
- [x] Request+decode locked by fixture-backed operation tests for `get`
      (current), `get` (history), `Setting.get`, and `Setting.set`.
- [x] MCP read tools registered read-only; mutation tools registered and removed
      in `read_only.go`.

## Verification

- `go test ./...` and `go vet ./...` pass; decode is fixture-locked for `get`
  (current), `get` (history), `Setting.get`, and `Setting.set`. Reference schema:
  `webapi-System/SynoTest/webapi_subsuite/test_syno_core_system_utilization.py`;
  error codes from `webapi-Core/include/webapi-Core/webapi_core_error.h`
  (1008 not-enabled, 1051 bad-params, 1057 bad-interface).
- Live-verified end-to-end on **DS3018xs / DSM 7.3-81168**: `capabilities`
  (all backends), `current` (fields match the live snapshot), `setting`,
  `history` recording-off (mapped 1008 → typed error), enable via plan/apply,
  `history` all dimensions (49 series across cpu/memory/network/disk/volume,
  ~10 080 weekly samples with real values), then disable via plan/apply to
  restore the original state. Both toggle directions verified their
  postcondition. The recording toggle change was reversible and fully restored.

## Coordination

New `internal/domain/resmon`, `internal/synology/operations/utilizationread`,
`internal/synology/operations/resmonsetting`, and
`internal/synology/operations/resmonsettingmutation` packages are the parallel
boundary. Additive edits to the high-contention files `compatibility_report.go`,
`runtime/manager.go`, `application/service.go`, `mcpserver/server.go`, and
`mcpserver/read_only.go`; new `internal/application/resource_monitor.go` and
`internal/cli/resource_monitor.go`. Coordinate before another active item edits
those shared files.
