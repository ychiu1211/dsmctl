---
id: WI-090
title: Snapshot Replication relation create (guarded, vault-brokered pairing)
status: in_progress
owner: "claude/snapshot-replication-integration-afbc59"
priority: P2
depends_on: [WI-089]
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

# WI-090 — Snapshot Replication relation create (guarded, vault-brokered pairing)

## Outcome

A CLI user or MCP agent can create a **shared-folder replication relation from
a source NAS profile to a destination NAS profile** through the hash-bound
plan/apply contract, and read back the resulting relation. This is the guarded
"replication mutation" deferred by [WI-089](WI-089-snapshot-replication.md),
now buildable because two real DS918+ units (nas51 DSM 7.3.2, nas255 DSM 7.3.1)
run the package outside the 7.3-81168 feed deadlock.

The load-bearing design decision this item settles: **the source-to-destination
trust handshake's only bootstrap secret — the destination NAS admin password —
is resolved by dsmctl's existing audited credential resolver at apply time,
from the destination profile's own vault entry, and never enters the plan, the
approval hash, logs, MCP arguments, or the caller's hands.** The operator names
the destination *by profile* (both source and destination are already
configured, authenticated profiles); dsmctl brokers the password internally
exactly as it already does to authenticate every read against that profile.
There is no new plaintext path and no model-visible secret.

## Scope

One guarded operation plus its read-back, all package-gated on
`SnapshotReplication` and fail-closed without it:

- **Create a share replication relation** `source-profile → dest-profile` for
  one shared folder, via the mapped DR flow (all under one apply):
  1. Resolve the destination profile credential (audited resolver) and mint a
     destination DSM session.
  2. `SYNO.DR.Node.Credential temp_create` (session → `cred_id`).
  3. `SYNO.DR.Plan check_remote_conn` (verify reachability before writing).
  4. `SYNO.DR.Plan create` v3 for the share target; poll `get_poll_task` to
     completion.
- **Read back the created relation**: `SYNO.DR.Plan list`/`info` with the full
  `additional` set — the single act that upgrades the WI-089 replication read
  decoder from source-derived to **live-verified per-plan fields**.
- **Delete a relation** (cleanup / teardown): `SYNO.DR.Plan delete`, guarded,
  so the live test and operators can remove a relation.

## Non-goals (deferred — extreme risk, role-flipping)

- **Failover / switchover / test-failover / reprotect / commit-failover /
  undo-failover.** These flip production roles between sites and can strand
  data or split-brain a pair. This item exposes them **read-only** as `can_*`
  capability reporting only; none are executable.
- **Sync-now / stop / pause** of an existing relation — a smaller follow-on
  once create is proven (kept out of the first slice to bound blast radius).
- **Editing** an existing relation's schedule/retention/encryption.
- **LUN replication** (`target_type` LUN) — SAN territory.
- **Multi-controller / VMware SRM solution types** — `solution_type=1`
  (Synology DR) only.

## Design constraints

- **Vault-brokered credential, never model-visible.** The plan references the
  destination *profile name* (or a `credential_ref`); the password is resolved
  only inside `applySnapshotReplicationRelationPlan`, via the same
  `credentials.SecureStore.Password` path used for ordinary authentication —
  never `RevealPassword`, never surfaced. The plan/hash/logs/MCP args carry the
  reference, not the secret. Live verification confirms no plaintext leaks.
- **Extreme risk classification.** Every variant is high risk. The read-only
  developer gateway strips the plan/apply tools. Remote apply additionally
  requires the existing single-use approval (WI-016 `authorizeRemoteApply`).
- **Never overwrite destination data (fail closed).** Before create, apply
  reads the destination for an existing share/replica of the target name and
  refuses if one exists; it verifies the destination volume exists and the
  source share is snapshot-capable. `check_remote_conn` must pass before any
  create is sent.
- **Async task postcondition.** Create returns a `task_id`; apply polls
  `get_poll_task` to a terminal state (bounded deadline, the WI-029
  package-install poller precedent) and then confirms the relation is present
  via `DR.Plan list` before reporting success — never a false success on a
  still-running task.
- **Hash-bound plan/apply**, observed-state fingerprint, stale rejection, and
  postcondition re-read, matching the WI-089 contract. Because create is
  effect-ful and not idempotent, the plan binds the source/destination
  identity + target share + observed absence of a conflicting relation.
- **Per-operation compatibility + capabilities**; each DR call is its own typed
  operation+variant (no shipped generic raw call), package-gated.
- **Live-mutation policy.** The live test replicates a **throwaway
  `dsmctl-e2e-*` share** created on the source for the test, to the
  destination, then deletes the relation and the throwaway shares on both ends;
  no existing share is ever replicated.

## Acceptance criteria

- [ ] `snapshot replication create` (CLI) + `plan_snapshot_replication_create`
      / `apply_snapshot_replication_create` (MCP) create a share relation
      source→dest through hash-bound plan/apply, resolving the destination
      credential internally at apply, with the never-overwrite + reachability
      guards and the async-task postcondition.
- [ ] The destination password never appears in the plan, hash, logs, MCP
      arguments, or any model-visible output (verified live with logging on).
- [ ] Extreme-risk classification; read-only gateway strips both tools; remote
      apply requires single-use approval; `server_test` tool count + allowlist
      updated.
- [ ] Replication read decoder upgraded to **live-verified** per-plan fields
      from a real relation; the WI-089 WIRE-UNVERIFIED caveat is cleared.
- [ ] `snapshot replication delete` removes a relation (guarded).
- [ ] Failover family exposed as read-only `can_*` reporting only; no
      executable failover/switchover/reprotect in this slice.
- [ ] Unit: request-capture for temp_create/check_remote_conn/create-v3/
      get_poll_task/list/delete; plan hash + stale + tamper tests; a test
      proving the plan/hash never contains the destination secret;
      `go build ./...`, `go vet ./...`, `go test ./... -count=1` clean.
- [ ] Live verification nas51→nas255 on a throwaway share, read back, then full
      teardown (relation + throwaway shares), lab restored.

## Verification

- Unit + request-capture as above.
- Live: create a throwaway `dsmctl-e2e-*` btrfs share on nas51, replicate it to
  nas255 via `snapshot replication create`, read the relation back on both
  ends, then delete the relation and both throwaway shares.
- Wire blueprint (from the 7.4.7 `disaster_recovery.js`, to confirm live) is in
  [WI-089](WI-089-snapshot-replication.md) → "Replication exploration".

## Coordination

- Parallel group C; extends the WI-089 snapshot module in place. Shared files
  (`internal/mcpserver/server.go`, `read_only.go`, `server_test.go`,
  `spec/roadmap.md`, `spec/gap-inventory.md`) overlap the in-flight security
  and provisioning sessions — coordinate the tool count.
- Numbering: WI-087 (Hyper Backup) and WI-088 (gateway-nas-credential-copy,
  merged) are parallel-session claims; this item is WI-090 after WI-089.

## Handoff

Fill this only when pausing incomplete work.
