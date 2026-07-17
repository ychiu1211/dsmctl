---
id: WI-013
title: SSD cache management
status: done
priority: P2
owner: ""
depends_on: [WI-001, WI-002, WI-003]
parallel_group: A
touches:
  - internal/domain/storage/model.go
  - internal/synology/operations/storageflashcachemutation
  - internal/synology/operations/storageinventory
  - internal/synology/storage.go
  - internal/application/storage_management.go
  - internal/cli/storage.go
  - internal/mcpserver/server.go
  - docs/storage-management.md
---

# WI-013 — SSD cache management

## Outcome

A CLI or MCP user can inventory SSD (flashcache) caches and create or remove a
read-only or read-write SSD cache on a volume through the existing guarded
storage plan/apply flow, without dsmctl exposing a raw DSM call.

## Scope

- Read `ssdCaches` state (mode, backing SSDs, protection RAID, status/health,
  dirty-data, size) from `SYNO.Storage.CGI.Storage.load_info`, plus model-level
  cache constraints (`env.cache_max_ssd_num`) and read-only/read-write/protection
  support.
- Add `cache` as a third `resource` in `storage.ChangeRequest` with `create`,
  `update` (expand), `convert`, and `delete` actions, reusing `storage plan` /
  `storage apply` and the MCP `plan_storage_change` / `apply_storage_plan` tools.
- Guarded create (`SYNO.Storage.CGI.Flashcache` `enable`) and delete (`remove`)
  with request-capture tests, candidate-SSD eligibility (SSD media, unused,
  selectable, healthy — tolerating the boot SSDs' `sys_partition_normal`
  status), parent-volume writability, one-cache-per-volume, read-write disk/RAID
  minimums, topology + safety fingerprints, risk/destructive rules (read-write
  removal flushes dirty data → high risk), and postcondition verification.

## Non-goals

- SSD cache **expand** and **read-only↔read-write convert** are modeled in the
  domain and capability-gated, but have **no backend method on the discovered
  DSM 7.3 (`SYNO.Storage.CGI.Flashcache` exposes only `enable` and `remove`)**,
  so they report unsupported and fail closed. Wiring them is deferred to a DSM
  release that advertises the methods.
- SSD cache advisor, dirty-data flush progress, pin-metadata tuning, and shared
  (multi-volume) cache allocation.

## Design constraints

- Preserve the storage mutation-safety contract: mutation package never decodes
  the write response; correctness is proven by a fresh-inventory postcondition.
- `convert` uses a dedicated `storage.ActionConvert` because read-write→read-only
  conversion flushes dirty data and is differently-risked than a patch update.
- Read-write support is only reported when `SYNO.Storage.CGI.Cache.Protection`
  is present.

## Acceptance criteria

- [x] `storage capabilities` reports cache status/create/expand/convert/delete
      with selected backends; expand/convert are `no` on this DSM.
- [x] `storage inventory` shows an SSD CACHES section when a cache exists.
- [x] `storage plan`/`apply` create a read-only or read-write cache and remove
      it, bound by hash + topology + safety fingerprints.
- [x] Request-capture tests lock the `enable`/`remove` parameter shapes.
- [x] Unit tests cover mode validation, HDD rejection, one-cache-per-volume,
      read-write protection requirement, and delete risk by mode.
- [x] Live create + remove verified on the configured test NAS (read-only and
      read-write, DS3018xs / DSM 7.3-81168). Note: on this model a read-only cache
      uses RAID 0 across >=2 SSDs, `enable` requires an explicit non-zero size
      resolved via `estimate_raid_size`, and read-write removal flushes
      asynchronously (the delete postcondition tolerates an in-progress teardown).

## Verification

- `go test ./...` and `go vet ./...`.
- Request shapes captured read-only from the DSM 7.3 Storage Manager
  `storage_panel.js` asset (`enable`/`remove`).
- Live-mutation policy: a read-only cache create + remove on the explicitly
  authorized test NAS, cleaned up immediately; read-write requires separate
  confirmation because removal flushes dirty data.

## Coordination

Shares `internal/domain/storage/model.go`, `internal/synology/storage.go`, and
`internal/application/storage_management.go` with WI-002/WI-003 (both `done`);
cache branches are additive.

## Completion record

- Completed end to end on 2026-07-17 in commit `4303e36`. SSD cache inventory,
  capability reporting, guarded read-only/read-write create, and mode-aware
  remove are wired through the shared CLI/MCP/application plan/apply surface.
- Verified with `go test ./...` and `go vet ./...`.
- Live read-only and read-write cache create/remove were verified on DS3018xs
  running DSM 7.3-81168. Stable postconditions handled asynchronous
  read-write-cache teardown, and all temporary cache resources were removed.
