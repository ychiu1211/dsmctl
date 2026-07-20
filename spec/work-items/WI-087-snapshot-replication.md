---
id: WI-087
title: Snapshot Replication module
status: in_progress
owner: "claude/snapshot-replication-integration-afbc59"
priority: P2
depends_on: [WI-019, WI-022, WI-029]
parallel_group: C
touches:
  - internal/domain/snapshotreplication
  - internal/synology/operations/snapshotreplication
  - internal/synology/snapshot_replication.go
  - internal/application/snapshot_replication.go
  - internal/cli/snapshot_replication.go
  - internal/mcpserver/server.go
  - internal/mcpserver/read_only.go
  - internal/mcpserver/server_test.go
  - docs/snapshot-replication.md
---

# WI-087 — Snapshot Replication module

## Outcome

A CLI user or MCP agent can manage the Snapshot Replication surface of a NAS:
read per-shared-folder btrfs snapshots (with description/lock attributes),
snapshot schedules and retention policy, replication relations and their sync
status, and the Snapshot Replication log feed — and, through the hash-bound
plan/apply contract, take, describe/lock, and delete shared-folder snapshots.
This is the first data-protection module (the gap-inventory group that also
holds Hyper Backup and Active Backup); it is package-version gated on the
installed `SnapshotReplication` package (dependency `ReplicationService`) and
fails closed when the package is absent or below baseline, exactly like the
Drive/Photos/Download Station modules.

All API names, methods, and field names below are researched from the package
UI bundles and probes and **MUST be treated as to-be-live-verified**; the
standing policy is that source-derived names are often stale — confirm every
shape against the lab and re-read after any write (see
[[dsm-webapi-live-verify-fields]], [[dsm-webapi-string-param-quoting]]).

## Scope

Sliced read-first, then guarded snapshot writes; replication mutations are
explicitly out of scope for this item.

### Slice A — read-only (independently shippable)

- **Package evidence + capabilities** — installed/running/version of
  `SnapshotReplication`, per-operation selected backends.
- **Share snapshot inventory** — `SYNO.Core.Share.Snapshot` `list`
  (v1–v2 advertised on DSM 7.3; probe-verified) per shared folder, with
  attribute expansion: snapshot time/name, description, lock/pin state, and
  worm state where present. Aggregate per-share counts plus a focused
  single-share listing.
- **Snapshot schedule + retention** — the per-share snapshot schedule and
  retention policy reads (`SYNO.DisasterRecovery.Retention` `get` is
  probe-confirmed to exist; schedule API name from package JS, to verify).
- **Replication relations** — the replication plan/relation list and per-relation
  sync status from the post-install `SYNO.DR.*` family (names from package JS,
  to verify). The lab has no configured replication, so per-relation fields
  ship spec-derived and are marked WIRE-UNVERIFIED until a populated
  relation exists (WI-066 precedent for absent rules).
- **Snapshot Replication log feed** — `SYNO.DisasterRecovery.Log` `list`
  (probe-verified shape: `log_list`, `total`, `error_count`, `warn_count`,
  `info_count`) with paging.

### Slice B — guarded snapshot writes (plan/apply, hash-bound)

Share-snapshot lifecycle only, all through the mutation-safety contract:

- **Take snapshot** — `SYNO.Core.Share.Snapshot` `create` (v1) with optional
  description and lock; medium risk.
- **Edit snapshot attributes** — `set` (v1): description and lock/unlock;
  medium risk.
- **Delete snapshots** — `delete` (v1): named snapshots of one share;
  **high risk, destructive and irreversible**.
- Snapshot schedule/retention **set** ships only if the wire shape is
  live-verified end-to-end on the lab within this item; otherwise it is
  recorded as a deferred follow-on (fail closed, never guessed).

## Non-goals

- **Replication mutations**: creating/editing/deleting replication relations,
  sync-now, failover, switchover, test-failover, and re-protect. These need a
  second prepared NAS as a replication target and carry extreme risk; they are
  a follow-on item once a target NAS exists in the lab.
- **Restore paths**: in-place rollback of a share to a snapshot and
  clone-snapshot-to-new-share are destructive restore surfaces deferred to a
  dedicated item (the FileStation `#snapshot` browse path already gives
  read access to snapshot contents).
- **LUN snapshots and LUN replication** (`SYNO.Core.ISCSI.*`) — SAN module
  territory, already recorded as deferred under WI-005.
- **Hyper Backup / Active Backup / Shared Folder Sync / C2** — separate
  gap-inventory areas.
- Any generic raw snapshot API proxy or an unguarded convenience mutation.

## Design constraints

- **Package gating**: every operation is gated on
  `PackageVersionRange("SnapshotReplication", 7.0, ∞)` plus advertised API
  versions; a NAS without the package reports `(not supported)` and fails
  closed. The core `SYNO.Core.Share.Snapshot` API technically answers `list`
  without the package, but the module deliberately stays package-gated so the
  advertised surface always matches the DSM UI's Snapshot Replication app.
- **Snapshot identity**: DSM identifies a share snapshot by its time-stamped
  name within a share. Plans bind `(share, snapshot-name)` tuples plus the
  observed snapshot set fingerprint; apply rejects stale state (a snapshot
  taken or deleted out-of-band invalidates the plan).
- **Patch semantics**: `set` edits are patch-only per snapshot (description,
  lock); unspecified attributes are never rewritten. Delete names an explicit
  snapshot list — never "all", never a retention sweep.
- **Postcondition re-reads** after every write (create → the new snapshot is
  listed; set → attributes match; delete → the snapshots are gone), honoring
  [[dsm-webapi-string-param-quoting]] for string/array parameters.
- **No secrets** flow through this module (snapshot metadata only). Future
  replication-target credentials would use `credential_ref` — out of scope.
- **Live-mutation policy**: snapshot writes during development and live tests
  run **only against a dedicated throwaway `dsmctl-e2e-snap-*` share** created
  for the test and removed after stable-ID-verified cleanup, per AGENTS.md.
  Existing shares are never snapshotted, edited, or pruned by tests.
- Per-operation compatibility and capability reporting with stable operation
  names, like every other module.

## Acceptance criteria

- [ ] Slice A: CLI `snapshot capabilities|list|schedule|replication|log` and
      matching `get_*` MCP tools return normalized state (package evidence,
      per-share snapshot lists with attributes, schedule/retention, replication
      relations, log feed) with tolerant decoders that reject malformed shapes.
- [ ] Package gate: absent/stopped `SnapshotReplication` fails closed with the
      installed-but-not-running hint, without disabling adjacent modules.
- [ ] Slice B: take/edit/delete snapshot via hash-bound plan/apply with
      observed-set fingerprint, stale rejection, and postcondition re-read;
      delete is high-risk; the read-only gateway strips plan/apply tools.
- [ ] Live verification on the DSM 7.3 lab: reads against real state; the full
      write lifecycle (create → describe → lock → unlock → delete) against a
      throwaway `dsmctl-e2e-snap-*` share, then share cleanup verified.
- [ ] Unit: decoder tolerance + malformed rejection, request-capture asserting
      wire shapes (JSON-literal string params), plan hash + staleness tests;
      `go build ./...`, `go vet ./...`, `go test ./... -count=1` clean.
- [ ] MCP server tool-count and allowlist tests updated; docs page shipped and
      linked; roadmap + gap inventory updated.

## Verification

- Unit and request-capture tests as above (fixtures under
  `internal/synology/operations/snapshotreplication/testdata/`).
- Live reads against the DSM 7.3-81168 lab (DS3018xs, btrfs `/volume1`).
- Live writes only on the throwaway `dsmctl-e2e-snap-*` share as described.
- Research sources: probe sweep against the lab (method existence + shapes)
  and the SnapshotReplication 7.4.7 UI bundles; every name re-verified live
  before ship per [[dsm-webapi-live-verify-fields]].

## Coordination

- WI-084–WI-086 are reserved by the provisioning program (parallel sessions);
  this item deliberately takes WI-087.
- Parallel group C; new operation packages are the parallel boundary. Shared
  files touched: `internal/mcpserver/server.go`, `read_only.go`,
  `server_test.go`, `spec/roadmap.md`, `spec/gap-inventory.md` — coordinate
  with the five in-flight security read slices (WI-066/067/068/070/071) and
  WI-072, which touch the same registration points.
- Replication mutations and restore/rollback are intended follow-on items;
  LUN snapshots stay with the SAN module (WI-005 deferred list).

## Handoff

Fill this only when pausing incomplete work.
