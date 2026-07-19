---
id: WI-043
title: Download Station module (read-only)
status: in_progress
priority: P2
owner: ""
depends_on: [WI-019, WI-022]
parallel_group: C
touches:
  - internal/domain/downloadstation
  - internal/synology/operations/downloadstation
  - internal/synology/downloadstation.go
  - internal/runtime/manager.go
  - internal/application/download_station.go
  - internal/cli/download.go
  - internal/mcpserver/server.go
  - docs/download-station.md
---

# WI-043 — Download Station module (read-only)

## Outcome

A CLI user or MCP agent can read the Synology Download Station service
configuration, the download task list, and transfer statistics, package-version
gated on the installed DownloadStation package exactly like the Photos (WI-030)
and Surveillance (WI-034) modules.

## Scope

Read-only, targeting the stable, publicly-documented **legacy
`SYNO.DownloadStation.*`** API (both it and the newer `SYNO.DownloadStation2.*`
are present on DSM 7.3; the legacy surface is simpler and version-stable).

- **Service** — `SYNO.DownloadStation.Info` (`getinfo` + `getconfig`) and
  `SYNO.DownloadStation.Schedule` (`getconfig`): version, manager flag, default
  destination, eMule/auto-unzip switches, per-protocol (BT/eMule/FTP/HTTP/NZB)
  rate limits, and the bandwidth schedule.
- **Tasks** — `SYNO.DownloadStation.Task` (`list` with `additional=detail,transfer`):
  each task's id, type, title, size, status, destination, and live transfer
  speed.
- **Statistics** — `SYNO.DownloadStation.Statistic` (`getinfo`): aggregate
  download/upload speed.
- **Settings** — the newer `SYNO.DownloadStation2.Settings.*` generation (all on
  `entry.cgi`) composed into one detailed configuration: BT (ports, DHT, port
  forwarding, preview, encryption, rate limits, max peers, seeding), eMule,
  FTP/HTTP, NZB, auto-extraction, destination/watch-folder, RSS interval, and the
  scheduler (raw 168-slot weekly bitmap). The NZB and archive-extraction
  passwords are never decoded into the model.
- Package-version gating on `DownloadStation` (>= 3.0, verified on 4.1.2) so a
  NAS without it fails closed with package evidence.

## Non-goals

- Task `edit` (rename / re-target an existing task) — the other four task
  actions (create/pause/resume/delete) are implemented as guarded plan/apply.
- RSS (`RSS.Site` / `RSS.Feed`), BT search (`BTSearch`), eMule search, eMule
  server management, and the per-task BT/file/tracker/peer/NZB detail
  sub-resources.
- **Settings writes** for the remaining groups (eMule, Nzb, AutoExtraction,
  Location, Rss, Scheduler, Global) — the BitTorrent and FTP/HTTP group `set`s
  are implemented via a group-dispatched plan/apply; the rest follow the same
  full-object read-merge-set pattern.
- The task-management side of `SYNO.DownloadStation2.*` (`Task`, `Task.List`,
  `Task.BT.*`); the read module uses only the `Settings.*` slice of that
  generation plus the legacy Task list.

## Design constraints

- **Legacy per-API CGI paths.** Unlike `entry.cgi` APIs, each legacy Download
  Station API is served from its own CGI (`DownloadStation/task.cgi`,
  `info.cgi`, `schedule.cgi`, `statistic.cgi`). The client resolves these from
  the discovered API registry, so operations only name API+version+method.
- **Package-gated, fail-closed.** Every variant matches
  `PackageVersionRange(DownloadStation, 3.0, ∞)` plus the API version; a NAS
  without the package (or below baseline) reports the module unsupported and
  reads return an actionable "installed but not running" error when applicable.
- **Field shapes live-verified on 4.1.2** (installed via dsmctl's guarded online
  install for this work). `Info.getconfig` returns rate limits as numbers and
  `default_destination` as null when unset; `Task.list` returns
  `{total, offset, tasks}`. Task **entry** fields are decoded tolerantly and
  handle a size/speed returned as a quoted string (`flexInt64`), because the lab
  had no task to populate the list — a live populated task should confirm the
  entry shape before task writes are added.

## Acceptance criteria

- [x] `download capabilities|service|tasks|statistics|settings` (CLI) and the
      matching `get_download_station_*` MCP tools return normalized state with
      package evidence.
- [x] `settings` composes the ten `DownloadStation2.Settings.*` reads, live-verified
      on 4.1.2; NZB/archive passwords never surface (unit test asserts no leak).
- [x] Package-gating: reads/selection fail closed without DownloadStation and
      below the 3.0 baseline; capabilities carry installed/version/running.
- [x] Decoder + composition unit tests (service composes three reads; tolerant
      task entry incl. string numbers; malformed-shape rejection).
- [x] DSM 7.3 live verification: installed DownloadStation 4.1.2 via dsmctl
      guarded install; read capabilities (all three supported), service
      (4.1-5012, BT up cap 20 KB/s), empty task list, zero statistics.
- [x] Guarded task control (create / pause / resume / delete) via hash-bound
      plan/apply, bound to the target tasks' stable identity, with per-action
      postcondition verification and per-task failure surfacing; live-verified on
      4.1.2 (create→pause→resume→delete round-trip, fully reverted), which also
      confirmed the populated task-entry shape (uri/create_time added).
- [x] Guarded settings write via a group-dispatched hash-bound plan/apply:
      full-object read-merge-set (the DSM set is a full replace), bound to the
      complete observed group, per-field postcondition; enabling BT port
      forwarding is high risk. BitTorrent and FTP/HTTP groups implemented and
      live-verified on 4.1.2 (BT max-upload+preview, FTP/HTTP max-conn — both
      reverted); `set` method + full-object form encoding confirmed against the
      C++ handler registry and a reverted live probe. Remaining groups plug into
      the same dispatch.
- [ ] `edit` (rename/re-target) and settings writes for the other groups (eMule,
      FTP/HTTP, NZB, auto-extraction, location, RSS, scheduler, global).

## Verification

- Decoder + selection tests; `go test ./... -count=1`, `go vet ./...`.
- Live reads on the DSM 7.3 lab against DownloadStation 4.1.2 (installed for this
  work via `dsmctl package install DownloadStation`).

## Coordination

- Package-scoped module (parallel group C) alongside Photos, Surveillance, and
  Drive Admin; new packages under `internal/domain/downloadstation` and
  `internal/synology/operations/downloadstation`. No overlap with the External
  Access module (WI-041).
