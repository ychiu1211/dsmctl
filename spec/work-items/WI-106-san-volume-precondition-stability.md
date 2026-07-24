---
id: WI-106
title: Stabilize SAN backing-volume preconditions
status: done
priority: P1
owner: ""
depends_on: [WI-005]
parallel_group: B
touches:
  - internal/application/san_management.go
  - internal/application/san_management_test.go
  - docs/storage-management.md
---

# WI-106 — Stabilize SAN backing-volume preconditions

## Outcome

An approved LUN create plan remains applicable while unrelated background
activity changes a healthy backing volume's available-byte counter, while apply
still revalidates that the stable volume is healthy, writable, compatible, and
has enough current capacity.

## Scope

- Remove volatile available-capacity counters from the hash-bound backing-volume
  identity fingerprint.
- Preserve apply-time capacity validation through a fresh storage-state read.
- Keep stable volume ID, path, filesystem, status, and read-only state bound to
  the plan.
- Add regression tests for harmless free-space drift and unsafe volume changes.
- Rebuild and deploy the Gateway, then repeat the disposable LUN create/delete
  test on all three configured DSM versions.

## Non-goals

- Relaxing SAN graph, stable-ID, mapping, session, or postcondition guards.
- Storage-pool or volume mutation.
- Target or mapping live mutation.

## Acceptance criteria

- [x] Available-byte drift alone does not change the SAN plan hash or volume
      fingerprint.
- [x] A changed volume ID, path, filesystem, status, or read-only state still
      invalidates the plan.
- [x] Apply re-reads the backing volume and refuses insufficient current space.
- [x] Unit tests, `go test ./...`, and `go vet ./...` pass.
- [x] A uniquely named, unmapped LUN can be created on each configured NAS,
      verified by stable DSM LUN ID, and deleted only after an ID match.

## Verification

- `go test ./internal/application -run SAN -count=1`
- `go test ./... -count=1`
- `go vet ./...`
- Live MCP plan/apply on DSM 7.3-81168, DSM 7.3.2-86009 Update 4, and DSM
  7.3.1-86003 Update 1.

## Handoff

- Live write audit reproduced two consecutive false stale-plan rejections for
  `dsmctl-e2e-lun-wr-235` while no SAN object changed. The old
  `setPlanVolume` fingerprint included `AvailableBytes`, which naturally
  drifted on the active package-hosting volume.
- The volume fingerprint now binds stable volume ID, path, filesystem, health,
  and read-only state but excludes current free bytes. Apply still performs a
  fresh storage read and refuses insufficient current capacity before calling
  DSM.
- Live disposable LUN lifecycle passed on DSM 7.3-81168, DSM
  7.3.2-86009 Update 4, and DSM 7.3.1-86003 Update 1. The three LUNs were never
  mapped, were verified by stable UUID, and were deleted only after the UUID
  matched.
- Final MCP inventory found no `dsmctl-e2e-*` SAN objects on any NAS. Focused
  SAN tests, full Go tests, `go vet`, SPK validation, and the deployed
  `7.3.2-34` Gateway all passed.
