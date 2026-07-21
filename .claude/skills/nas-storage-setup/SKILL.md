---
name: nas-storage-setup
description: >-
  Use whenever the user wants to give a Synology NAS that already has DSM installed its first
  usable storage, or to "finish the basic setup" of a freshly-installed NAS — detect the setup
  state, finish the DSM first-time wizard if it is still pending, and create the first storage
  pool + volume so shared folders and packages become possible. Trigger on phrases like
  "create a volume", "set up storage", "make the NAS usable", "finish the first-time wizard",
  "create a storage pool", "build a RAID", "初始化儲存空間", "建立儲存池", "建立磁碟區",
  "把 NAS 弄到可以用", "完成基本設定", "跑完安裝精靈", or any request to take a DSM-installed
  but volume-less NAS to a ready-to-use state. This is the storage half that follows
  `nas-provision`: provision creates the admin and finishes the wizard; a fresh DSM still has
  NO storage, so nothing (shares, most packages) works until this skill runs. Creating a pool
  is DESTRUCTIVE — it wipes every selected disk — so it is gated on explicit user confirmation
  and on asking the user how they want the volume built.
---

# nas-storage-setup — give a DSM-installed NAS its first usable storage

**Goal:** take a NAS whose DSM is installed (admin exists, wizard maybe done) but which has
**no storage pool/volume** to a working state with one all-disk volume mounted at `/volume1`.
A fresh DSM marks itself "set up" with zero volumes, so it looks finished but nothing works —
Storage Manager just prompts for a volume. **Creating a storage pool permanently erases every
disk it uses. Confirm with the user before any `storage apply` that creates or deletes a pool.**

Live-verified on two Synology **DS918+** (4-bay): `10.17.37.51` (DSM 7.3.2, fresh all-disk
Btrfs RAID5 created from scratch) and `10.17.36.255` (DSM 7.3.1, kept its reused pool).

## Relationship to nas-provision

`nas-provision` owns the admin account + first-run wizard and the whole password-vault
security model. This skill assumes DSM is installed and reachable and picks up at storage. If
the admin does not exist yet, run `nas-provision` first. The password rules below are the same
ones nas-provision states — **the model never sees or retrieves the DSM password**; it lives in
the OS credential store and only a human at a terminal reveals it.

## 0. Ask the user how to build the volume (the one decision point)

Storage layout is a real choice with a destructive, hard-to-undo result, so **ask before
building** unless the user already specified it. Default and simplest: **all disks → one Btrfs
RAID5 volume.** Offer the alternatives and the disk-count rules:

| Layout | Min disks | Fault tolerance | Notes |
| --- | --- | --- | --- |
| **RAID5 + Btrfs** (default) | 3 | 1 disk | Btrfs adds snapshots + self-healing; best general choice |
| SHR + Btrfs | 1 | 1 disk (2+) | Synology hybrid RAID; flexible for mixed/upgraded disks |
| RAID6 + Btrfs | 4 | 2 disks | Lower usable capacity |
| RAID10 + Btrfs | 4 (even) | 1 per mirror | Performance-oriented |
| RAID1 + Btrfs | 2 (max 4) | n−1 disks | Mirror |
| ext4 variants | — | — | Only if the user does not want Btrfs |

RAID type belongs to the **pool**; filesystem belongs to the **volume** — they are two separate
operations (below). If the box already has a **reused** pool, see "Handling a reused pool".

## 1. Detect state (read-only — always do this first)

```console
dsmctl --nas <profile> storage capabilities            # gate: Pool create / Volume create / Mutations must be yes
dsmctl --nas <profile> storage inventory --json        # disks (disk_ids), existing pools, volumes
```

Read from the inventory JSON:

- **Disks** — `disks[].id` (e.g. `sda`,`sdb`,…) are the stable IDs you pass to `disk_ids`. A
  free disk on a freshly-installed NAS reports `status: "sys_partition_normal"` (the mirrored
  DSM system partition is on every drive) with `in_use: false`, `selectable: true` — that is
  **normal and eligible**, not a fault.
- **Pools empty + Volumes empty** → this NAS has no storage; do the full create (steps 3–4).
- **A pool/volume already present** → the disks carried a pool DSM reused on reinstall (its id
  is often `reuse_1`); see "Handling a reused pool".
- `compatibility: "not_in_support"` on the disks means the drives are not on Synology's
  compatibility list (common with lab drives). DSM allows a pool on them with a warning; dsmctl
  requires the explicit opt-in `allow_unsupported_disks` (step 3).

## 2. Finish the first-time wizard (idempotent)

A DSM-installed NAS may still show the "Welcome to DSM 7.x" wizard on login. Complete the wizard
preferences (update policy, analytics-off, `hide_welcome`) using the **stored** password:

```console
dsmctl provision <profile> --finish-only --auto-update security
```

**If the welcome wizard keeps reappearing after that, the built-in `admin` is still enabled.**
DSM only stops showing the wizard once its `admin_configured` flag is true, which flips only
when the built-in `admin` account is disabled — and DSM lets `admin` be modified only by the
setup session itself (any other admin gets error 105). A fresh `dsmctl provision` now disables
it automatically; to retrofit a NAS provisioned before that fix (built-in admin still enabled,
likely empty password — also a security hole):

```console
dsmctl provision <profile> --reset-builtin-admin
```

This re-enters the admin setup session and scrambles + expires the built-in admin. Verify it
took by re-running it: a second run should FAIL to start the setup session (admin can no longer
log in), which confirms the account is disabled and the welcome wizard is gone. (If the admin
does not exist yet at all, use full `nas-provision` instead of `--finish-only`.)

## 3. Create the storage pool (DESTRUCTIVE — needs the user's OK)

One `plan` → `apply` cycle. The request carries exactly **one** resource, so pool and volume
are separate cycles. Request JSON (`pool-create.json`):

```json
{"action":"create","resource":"pool",
 "pool":{"name":"pool1","raid_type":"raid5",
         "disk_ids":["sda","sdb","sdc","sdd"],
         "allow_unsupported_disks":true}}
```

- `raid_type`: `raid5` (canonical; `shr`,`shr2`,`raid0`,`raid1`,`raid6`,`raid10`,`jbod`,`basic`
  also valid, subject to the min-disk table). RAID type is a POOL property.
- `disk_ids`: the **complete** set of disk IDs from step 1 — there is no "all"/"auto" shortcut.
- `allow_unsupported_disks`: include `true` **only** when the disks are `not_in_support`; it
  relaxes only the compatibility gate (health/SMART/selectable/in-use still enforced) and adds
  a recorded plan warning. Omit it for HCL-listed drives.

```console
dsmctl --nas <profile> storage plan  -f pool-create.json -o pool-plan.json
# read the "hash" from pool-plan.json, then:
dsmctl --nas <profile> storage apply -f pool-plan.json --approve <hash>
```

`--approve` is required and must equal the plan's `hash`; a stale plan (inventory changed since
planning) is rejected — re-plan. A fresh RAID5 pool comes up in status
**`background_optimizing`** (initial parity pass, runs for hours) while already **writable and
volume-ready** — apply treats that as success. DSM may name the new pool `reuse_1` even on a
clean create.

## 4. Create the volume

**Re-read inventory first** — the volume needs the new pool's stable id and layout, which only
exist after the pool is created:

```console
dsmctl --nas <profile> storage inventory --json    # note the new pool's id + layout
```

Request JSON (`volume-create.json`) — use `capacity.mode: "maximum"` to fill the pool (safe for
both `single` and `multiple` pool layouts):

```json
{"action":"create","resource":"volume",
 "volume":{"name":"volume1","pool_id":"reuse_1","file_system":"btrfs",
           "capacity":{"mode":"maximum"}}}
```

- `file_system`: `btrfs` (default) or `ext4` — a VOLUME property, independent of RAID type.
- `pool_id`: the id from the re-read inventory (e.g. `reuse_1`).

```console
dsmctl --nas <profile> storage plan  -f volume-create.json -o volume-plan.json
dsmctl --nas <profile> storage apply -f volume-plan.json --approve <hash>
```

DSM often does not persist a display name for the first volume (it shows blank); the volume
still lands at `/volume1` and is writable. Like the pool, it may be `background_optimizing`
right after create — usable immediately.

## 5. Verify

```console
dsmctl --nas <profile> storage inventory
```

Expect a `raid_5` (or chosen) pool over all disks and a `btrfs` volume `volume_1` at `/volume1`
with `read only: no`. The NAS can now host shared folders and packages. Then, if the human
needs the admin password (e.g. to log into the web UI), **tell them to run, in their own
terminal**:

```console
dsmctl auth password reveal --nas <profile>       # prints it (interactive terminal only)
# or: dsmctl auth reveal-password --nas <profile>  # copies it to the clipboard
```

Do not attempt the reveal yourself — it is gated by isatty + a typed NAS-name confirmation and
refuses non-interactive/agent callers by design.

## Handling a reused pool (`reuse_1` in `attention`)

Reinstalling DSM over disks that already held a pool leaves that pool **reused** — it appears
with a full-size volume already mounted and the pool often in status **`attention`**. On lab
hardware `attention` is almost always the benign *unsupported-drive advisory* (every disk shows
`compatibility: not_in_support`, all disks `normal`/`in_use`), **not** a fault — a degraded pool
would show a failed disk and a `degraded`/`crashed` status instead. Ask the user:

- **Keep it (recommended when it already matches the target layout).** It is already an
  all-disk Btrfs RAID5 volume; recreating produces the *same* `attention` advisory (it comes
  from the drives, not the pool) while wiping data and taking hours — so recreating gains
  nothing. Just verify the volume is `normal`/writable.
- **Wipe & recreate** only if the user wants a pristine, data-free volume: delete the volume
  then the pool (`action:"delete"` requests, each destructive and confirmed) and run steps 3–4.

## What this skill deliberately skips

Shared folders, package installs, and Synology Account / QuickConnect — those are done later
from DSM once a volume exists.

## Failure handling (live-learned)

- `disk status/health is "sys_partition_normal"/... expected normal or healthy` — you are on an
  old build; current dsmctl accepts `sys_partition_normal` as an eligible fresh-disk state.
- `drive compatibility is "not_in_support"; set allow_unsupported_disks ...` — add
  `"allow_unsupported_disks": true` to the pool request (lab/unlisted drives).
- `RAID type "raid5" has invalid disk count N` — RAID5 needs ≥3 disks (see the table); the
  planner rejects this before any DSM call.
- `pool ... post-mutation status/health is "background_optimizing"/...` — old build; current
  dsmctl accepts the benign background-optimizing state as success. The pool WAS created; do not
  re-apply (the plan is now stale and the pool exists).
- `expected exactly one new volume ... found 0` after a volume apply — the volume may have been
  created with a blank DSM name; current dsmctl matches on id+pool+filesystem+capacity and
  tolerates the empty name. Re-read inventory to confirm before retrying (never blindly
  re-apply a volume create — it can consume the remaining pool space with a second volume).
- Capability gate: if `storage capabilities` shows `Mutations: no`, nothing is sent — the
  backend is unavailable on that DSM.

## Verification (repo)

`go test ./internal/application/...` covers the storage planner: fresh-disk (`sys_partition_normal`)
eligibility, the `allow_unsupported_disks` opt-in + plan warning, the `background_optimizing`
post-status acceptance, and the empty-DSM-name volume postcondition. The pool/volume wire shapes
(`SYNO.Storage.CGI.Pool.create`, `SYNO.Storage.CGI.Volume.create_on_existing_pool` /
`deploy_unused`) are covered by `internal/synology/operations/...`. This flow is live-verified on
two DS918+ (DSM 7.3.1 / 7.3.2); see the `dsm-first-time-storage-setup` memory.
