# Resource Monitor

`dsmctl` reads DSM Resource Monitor's current utilization and recorded history,
and can turn history recording on or off. Reads are read-only and shared by the
CLI and MCP server; the recording toggle is a guarded plan/apply mutation.

```console
dsmctl resource-monitor capabilities --nas office
dsmctl resource-monitor current --nas office
dsmctl resource-monitor current --nas office --json
dsmctl resource-monitor history --nas office --period week --dimension cpu --dimension memory
dsmctl resource-monitor setting --nas office
```

`resmon` is an alias for `resource-monitor`.

## Current utilization

`current` reads a live snapshot from `SYNO.Core.System.Utilization` and
normalizes it to stable fields:

- **CPU** — user/system/other load percent and the 1/5/15-minute load averages.
- **Memory** — physical and swap usage percent, plus total/available/cached/
  buffer/swap byte counts (DSM reports these in kilobytes; dsmctl converts to
  bytes at the boundary).
- **Network** — per-interface transmit/receive bytes per second, including the
  synthetic `total` aggregate DSM reports.
- **Disk** — aggregate and per-disk read/write IOPS, read/write bytes per
  second, and busy percent.
- **Volumes** — per-volume space I/O in the same shape as disks.

The snapshot is volatile: two reads seconds apart differ, so it is only ever
read, never planned against. It also carries `recording_enabled` when DSM
reports it alongside the snapshot.

## History

`history` reads recorded samples from the same API with `type=history`,
returning one series per dimension, metric, and device (for example CPU
`user_load`, `eth0` `rx`, or `sda` `read_byte`). DSM returns evenly-spaced
samples spanning the window with no absolute timestamps, so each series is an
ordered `values` slice in DSM's native order (a one-week window is ~10 080
one-minute samples).

- `--period` selects the window: `week` (default), `month`, `half_year`, or
  `year`. DSM 7.x does not record a day window — use `current` for the live
  snapshot.
- `--dimension` limits the returned series to `cpu`, `memory`, `network`,
  `disk`, or `volume` (repeatable); omitted returns every dimension.

DSM requires a per-device interface list for the `disk`, `network`, and
`volume` groups (it rejects them with error 1057 otherwise), so `history`
reads the current snapshot once to enumerate the live devices and passes them
automatically; `cpu` and `memory` need none.

History exists only while recording is enabled. When it is off, DSM returns
error 1008 (`WEBAPI_CORE_SYSTEM_ERR_NOT_ENABLE_HISTORY`) and dsmctl reports a
clear "history recording is disabled" error — enable recording first.

## History-recording toggle

Whether DSM records history is read from `SYNO.ResourceMonitor.Setting`
(`resource-monitor setting`) and changed through a hash-bound plan/apply, the
same contract used by other dsmctl mutations:

```console
dsmctl resource-monitor plan-recording --nas office --enable -o plan.json
dsmctl resource-monitor apply-recording --nas office -f plan.json --approve <hash-from-plan>
```

The plan binds the requested toggle to the observed setting and prints an
approval hash; `apply-recording` re-checks the hash and the observed setting,
applies the change, and verifies the setting persisted. Disabling stops
collecting new samples but keeps history DSM already recorded, so it is planned
as `medium` risk; enabling is `low` risk. The apply re-sends the complete
Resource Monitor setting object, so co-located settings (such as performance
alarm rules) dsmctl does not manage are never reset.

## Compatibility

The module selects `SYNO.Core.System.Utilization` v1 (`resource.read`) for
current and history, and `SYNO.ResourceMonitor.Setting` v1 for reading
(`resource.recording_read`) and writing (`resource.recording_set`) the toggle.
`resource-monitor capabilities` and `nas capabilities` report each backend. A
DSM missing one API makes only the affected part unsupported; other modules are
unaffected.

## MCP

MCP exposes the reads as `get_resource_monitor_capabilities`,
`get_resource_monitor_state`, `get_resource_monitor_history`, and
`get_resource_monitor_setting` (read-only annotations). The toggle is
`plan_resource_recording_change` and `apply_resource_recording_plan`; like every
other apply, both are removed from the read-only gateway surface.
