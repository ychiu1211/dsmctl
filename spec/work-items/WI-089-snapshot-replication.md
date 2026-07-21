---
id: WI-089
title: Snapshot Replication module
status: done
owner: ""
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

# WI-089 — Snapshot Replication module

## Outcome

A CLI user or MCP agent can manage the Snapshot Replication surface of a NAS:
read per-shared-folder btrfs snapshots (with description/lock attributes),
snapshot schedules and retention policy, replication relations and their sync
status, and the Snapshot Replication log feed — and, through the hash-bound
plan/apply contract, take, describe/lock, and delete shared-folder snapshots.
This is the first data-protection module (the gap-inventory group that also
holds Hyper Backup and Active Backup).

**Gating model (revised at implementation, live-verified):** the snapshot
lifecycle, share snapshot configuration, retention policy, log feed, and node
identity are **DSM core APIs** that answer without the package —
`SYNO.Core.Share.Snapshot create` was proven live on a package-less DSM
7.3-81168 — so those operations gate on advertised API versions only. Only the
replication surface (`SYNO.DR.Plan`) is package-version gated on
`SnapshotReplication` 7.x and fails closed without it. This deliberate split
is what made full live verification possible, because of the discovery below.

**DSM 7.3-81168 package deadlock (discovered during this item):** that build's
own online feed cannot install Snapshot Replication at all. The feed offers
SnapshotReplication 7.4.7-1859 paired with ReplicationService 1.3.0-0423, but
7.4.7-1859 requires ReplicationService ≥ 0501 (upload error 4526), DSM
7.3-81168 refuses any SnapshotReplication < 7.4.7-1850 (error 4583), and every
ReplicationService build ≥ 0501 requires DSM ≥ 7.3-81179 (error 4538; the
0504/0505 builds are the DSM 7.2 line, error 4537). Installs fail silently in
the Package Center machinery (the WI-029 poller sees the task vanish and the
inventory never confirms). Consequence: replication-plan fields ship
source-derived and WIRE-UNVERIFIED until a NAS on DSM ≥ 7.3-81179 exists in
the lab; ReplicationService 1.3.0-0423 was left installed on the lab.

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
  **Outcome: deferred** — `SYNO.DisasterRecovery.Retention set` takes the
  policy numbers plus an embedded schedule and task id (`tid`) with
  interacting semantics; it was not verifiable end-to-end in this item.
  The retention/schedule READ shipped (`Retention get {type:"share",name}`,
  live-verified).

## Non-goals

- **Replication mutations**: creating/editing/deleting replication relations,
  sync-now, failover, switchover, test-failover, and re-protect. These carry
  extreme risk and require a server-to-server trust handshake whose only
  bootstrap secret is the remote NAS's admin password — a human-gated vault
  secret the model must not resolve itself. Deferred to a dedicated work item;
  the full wire flow is mapped in the exploration note below.
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

- [x] Slice A: CLI `snapshot capabilities|state|share|replication|log` and the
      `get_snapshot_capabilities|state|share|replication_status|log` MCP tools
      return normalized state (package evidence, per-share snapshot lists with
      attributes, config, retention policy, replication availability, log
      feed) with tolerant decoders that reject malformed shapes. (The
      commands landed as `state`/`share` rather than `list`/`schedule`;
      schedule read is the retention policy's `schedule` presence flag.)
- [x] Package gate: the replication read fails closed without the package
      (live-verified `(not supported)` + honest reason in the replication
      command) including the installed-but-not-running hint path; the snapshot
      surface deliberately stays core-gated (see the revised gating model).
- [x] Slice B: take/edit/delete snapshot and share-config write via hash-bound
      plan/apply with observed-set fingerprint, stale rejection, and
      postcondition re-read; delete is high-risk with locked-snapshot
      warnings; the read-only gateway strips `plan_snapshot_change` /
      `apply_snapshot_plan`.
- [x] Live verification on the DSM 7.3-81168 lab: all reads against real state
      (including a populated log entry); the full write lifecycle — create
      with description+lock → edit description + unlock → snapshot-browsing
      on → off → delete both snapshots — against the throwaway
      `dsmctl-e2e-snap-r1` share, then share deletion verified (lab restored
      to its original 4 shares).
- [x] Unit: decoder tolerance + malformed rejection, request-capture asserting
      wire shapes, plan hash/tamper/staleness/postcondition tests;
      `go build ./...`, `go vet ./...`, `go test ./... -count=1` clean.
- [x] MCP server tool-count (180 → 187) and allowlist tests updated;
      `docs/snapshot-replication.md` shipped and linked from the README;
      roadmap + gap inventory updated.

## Verification

Completed 2026-07-21 on the DSM 7.3-81168 lab (DS3018xs, btrfs `/volume1`):

- `go build ./...`, `go vet ./...`, `go test ./... -count=1` — clean.
- Unit + request-capture tests in
  `internal/synology/operations/snapshotreplication/operation_test.go`
  (inline live-captured shapes) and
  `internal/application/snapshot_replication_test.go` (fake-client
  plan/apply, stale rejection, tamper rejection, silent-no-op postcondition).
- Live wire facts: `SYNO.Core.Share.Snapshot` — `list` v2 with
  `additional=["desc","lock","schedule_snapshot","worm_lock"]`; `create` v1
  `{name, snapinfo:{desc,lock}}` returning the bare time-name string; `set`
  v1 `{name, snapshot, snapinfo:{...}}` (fields inside the `snapinfo`
  envelope — bare desc/lock params are rejected); `delete` v1
  `{name, snapshots:[...]}`; `get/set_share_conf` v1
  `{name, sharesnapinfo}`. `SYNO.DisasterRecovery.Retention` `get/info` v1;
  `SYNO.DisasterRecovery.Log` `list` v1 (entry: string `time`, text under
  `event`, `user`, `level`); `SYNO.DR.Node` `info` v1. All live-verified with
  postcondition re-reads per [[dsm-webapi-live-verify-fields]].
- Research sources: probe sweeps against the lab and the SnapshotReplication
  7.2.1-0607 plain-tar SPK (webapi `.lib` descriptors + `disaster_recovery.js`
  — the 7.4.7 SPK is a signed envelope and was only used for its INFO). The
  `SYNO.DR.Plan list` additional set and per-plan fields remain
  source-derived (package uninstallable on this DSM build).

## Replication exploration (nas51 ↔ nas255, 2026-07-21)

The DSM 7.3-81168 package deadlock does not affect the two DS918+ test units:
`ReplicationService 1.3.0-0600` + `SnapshotReplication 7.4.7-1859` installed and
run on **nas51** (DSM 7.3.2-86009) and **nas255** (DSM 7.3.1-86003) via local
`.spk` upload. With the package present, the module's replication read now
selects a real backend and returns `0 plans` cleanly, and `SYNO.DR.Node info`
decodes live (hostname + node UUID). The 15-API DR family is present:
`SYNO.DR.Node[.Credential/.Session]`, `SYNO.DR.Plan[.Site/.MainSite/.DRSite]`,
`SYNO.DR.Topology`, `SYNO.DR.Credential`, `SYNO.Replica.Share/.Volume`,
`SYNO.Btrfs.Replica[.Core]`, `SYNO.DisasterRecovery.Log/.Retention`.

**Full create flow, mapped from the 7.4.7 `disaster_recovery.js` (blueprint for
the replication-mutation follow-on):**

1. **Pair** — the source logs the operator into the *destination* NAS
   (`synocredential.Issue` → a browser login to the remote yielding a remote
   `sid`), then trades it for a persistent credential:
   `SYNO.DR.Node.Credential temp_create` v1
   `{conn:{addr,port,protocol}, auth:"session", session:<remote-sid>}` →
   `{cred_id}`. Thereafter calls use `auth:"cred_id", cred_id:<id>`.
2. **Test** — `SYNO.DR.Plan check_remote_conn` v1 `{src_to_dst_conns:[…]}`.
3. **Create** — `SYNO.DR.Plan create` **v3**, one compound entry per target:
   `{nowait:true, auto_remove:false, is_to_local:false, solution_type:1,
   target:{target_id:<share>, target_type:2}, dst_volume:"/volumeN",
   sync_policy:{enabled, mode:2, schedule:{…}, is_send_encrypted,
   is_sync_local_snapshots, is_app_aware, sync_window},
   src_to_dst_conns:[{cred:{conn:{addr,port,protocol}, auth:"cred_id",
   cred_id}, replica_conn:{replica_addr:"_AUTO_FILL_", replica_port:5566,
   replica_type:2}}], dst_to_src_conns?, src_output_conns?, dst_output_conns?}`
   → `{task_id}`, polled via `SYNO.DR.Plan get_poll_task`.
   Constants: `TARGET_TYPE_SHARE=2`, `TARGET_TYPE_LUN=1`,
   `REPLICA_TYPE_BTRFS=2`, `REPLICA_PORT_BTRFS=5566`, `SOLUTION_SYNOLOGY_DR=1`,
   protocol `http`/`https`, addr `_AUTO_FILL_` = wizard auto-detect.
4. **Read** — `SYNO.DR.Plan list` v1 with `additional=[sync_policy, sync_report,
   main_site_info, dr_site_info, can_do, op_info, last_op_info, topology,
   testfailover_info, retention_lock_report]`; `info`/`get`.
5. **Operate** — `sync`/`stop`/`pause`; failover family guarded by `can_*`
   predicates (`can_switchover`/`can_failover`/`can_reprotect`/…) before
   `switchover`/`failover`/`reprotect`/`commit_failover`/`undo_failover`.

**Blocked at step 1 for automation.** Pairing's only bootstrap secret is the
destination NAS's admin password (to obtain the remote `sid`). Per the security
model the model must not resolve a vault password itself; a throwaway harness
that did so was correctly refused by the safety classifier. The follow-on work
item must express the remote credential as a `credential_ref` resolved by the
existing audited resolver at apply time (never by the model, never printed), or
require the operator to establish the relation first. **The per-plan DR.Plan
read fields therefore remain source-derived / WIRE-UNVERIFIED** until a real
relation exists — the one open item from this exploration.

## Coordination

- WI-084–WI-086 are reserved by the provisioning program and WI-087 by the
  parallel Hyper Backup module session; this item takes WI-089 (renumbered twice: an initial WI-087 claim, then
  WI-088, both of which collided with parallel sessions — WI-088 is the merged
  `gateway-nas-credential-copy`).
- Parallel group C; new operation packages are the parallel boundary. Shared
  files touched: `internal/mcpserver/server.go`, `read_only.go`,
  `server_test.go`, `spec/roadmap.md`, `spec/gap-inventory.md` — coordinate
  with the five in-flight security read slices (WI-066/067/068/070/071) and
  WI-072, which touch the same registration points.
- Replication mutations and restore/rollback are intended follow-on items;
  LUN snapshots stay with the SAN module (WI-005 deferred list).

## Handoff

Fill this only when pausing incomplete work.
