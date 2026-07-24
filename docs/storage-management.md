# Storage management contract

Storage inventory and guarded storage-pool and volume lifecycles are
implemented. Pool create/add-disk/delete and volume create/expand/delete each
have an independently selected DSM backend. RAID migration remains fail closed
and returns:

```text
storage mutation backend is not available; no DSM request was sent
```

Discovering the inventory API alone never enables writes. The capability report
must select the exact create, expand, or delete operation; volume creation also
requires the model-definition capability reader. Unsupported DSM targets remain
read-only.

## Guarded pool apply

Planning reads normalized inventory and rejects disks unless DSM reports each
one healthy, unused, selectable, and supported. Pool creation reports
`applicable_raid_types`, calculated from the model SHR capability and selected
disk count. Add-disk expansion also requires a writable, idle pool whose DSM
`can_do` data advertises expansion. Delete requires DSM to advertise deletion,
uses only the observed stable pool ID, and is always high risk.

The plan binds two different fingerprints:

- `topology_fingerprint` covers stable disks, pools, volumes, and membership;
- `safety_fingerprint` covers selected disk health/allocation/compatibility and
  current pool actionability.

Apply re-reads inventory and rejects either fingerprint if it changed. After a
successful DSM call it reads inventory again and verifies member disks, RAID
type, pool status, and stable-ID absence for deletion. Create resolves the new
stable pool ID from the verified postcondition.

The DSM 7.3 backend uses three separate `SYNO.Storage.CGI.Pool` v1 operations:
`create`, `expand_by_add_disk`, and `remove`. Their typed parameters and RAID to
`device_type` mappings are locked by sanitized request-capture tests. Discovery
of those schemas used only the NAS-local Storage Manager Admin Center assets
and read-only `load_info`; no live storage mutation was run.

## Guarded volume apply

Volume filesystem choices are read from the authenticated
`SYNO.Core.Desktop.Defs.getjs` model definitions and normalized to Btrfs/ext4;
they are never inferred from an existing volume. Storage inventory separately
normalizes `pool_path`, `space_path`, DSM's `single`/`multiple` layout, pool
unallocated bytes, volume allocated device bytes, per-volume filesystem limits,
and DSM actionability flags.

An exact capacity must be a whole GiB. A `maximum` policy is resolved during
planning to an explicit byte value, bounded by pool unallocated capacity and
the model/filesystem limit. The resolved value, pool paths/layout, supported
filesystems, current unallocated capacity, and volume identity are covered by
the approval hash. Apply re-reads all of them and rejects a stale plan before a
write.

The DSM 7.3 volume backend uses independent `SYNO.Storage.CGI.Volume` v1
operations:

- multi-volume create: `create_on_existing_pool`, with allocation converted to
  DSM's MiB string field;
- unused single-volume pool create: `deploy_unused`, which only accepts the
  explicit `maximum` policy;
- multi-volume expansion: `expand_pool_child`, with an explicit total byte
  target;
- single-volume expansion: `expand_unallocated`, which consumes available
  unallocated space and therefore only accepts `maximum`;
- delete: `delete`, with the exact stable volume ID.

Postconditions resolve exactly one newly created stable volume ID, preserve the
parent pool and filesystem during expansion, verify the approved capacity or an
active non-failed DSM task, and require stable-ID absence after deletion. Live
verification for WI-003 was read-only: filesystem capabilities and operation
selection were queried, and a create plan was correctly rejected because the
test pool had less than the minimum unallocated space. No volume mutation was
sent.

## Ownership semantics

Storage changes are action-specific rather than ambiguous partial desired
state:

- Pool `create` owns the complete initial `raid_type` and `disk_ids` topology.
- Pool `update` is patch-only. `add_disk_ids` can only add stable DSM disk IDs;
  it cannot remove existing disks. `target_raid_type` is optional and its
  omission preserves the current RAID type.
- Pool `delete` accepts only a stable DSM pool `id`.
- Volume `create` owns the parent `pool_id`, `file_system`, and initial
  `capacity` policy.
- Volume `update` is patch-only and accepts only `expand_to`. Shrinking,
  filesystem replacement, and moving a volume between pools are not
  expressible.
- Volume `delete` accepts only a stable DSM volume `id`.

Disk, pool, and volume references are DSM stable identifiers, never display
row numbers, bay ordering, or human-readable names. Duplicate, unknown, busy,
or malformed disk references are rejected by the planner.

## Pool manifests

Create a RAID 5 pool from three stable disk IDs:

```json
{
  "action": "create",
  "resource": "pool",
  "pool": {
    "name": "data",
    "raid_type": "raid5",
    "disk_ids": ["disk-id-a", "disk-id-b", "disk-id-c"]
  }
}
```

The contract represents `shr`, `shr2`, `raid0`, `raid1`, `raid5`, `raid6`,
`raid10`, `jbod`, and `basic`. This does not claim every NAS supports every
type. The selected operation backend must check the model, disks, installed
DSM version, and advertised API before apply.

Add a disk while preserving the current RAID type:

```json
{
  "action": "update",
  "resource": "pool",
  "pool": {
    "id": "pool-stable-id",
    "add_disk_ids": ["disk-id-d"]
  }
}
```

The shared contract can represent an explicit RAID migration with
`target_raid_type`, but WI-002 deliberately has no migration backend. Planning
that intent fails before any DSM request is sent.

Delete uses only the stable pool ID:

```json
{
  "action": "delete",
  "resource": "pool",
  "pool": { "id": "pool-stable-id" }
}
```

The resulting plan lists the pool and every child volume under
`destructive_consequences` and marks the plan `destructive: true` with
`risk: "high"`.

## Volume manifests

Create a Btrfs volume consuming the backend-supported maximum capacity:

```json
{
  "action": "create",
  "resource": "volume",
  "volume": {
    "name": "volume1",
    "pool_id": "pool-stable-id",
    "file_system": "btrfs",
    "capacity": { "mode": "maximum" }
  }
}
```

An exact byte capacity is explicit:

```json
"capacity": { "mode": "exact", "size_bytes": 1099511627776 }
```

Expansion uses the same policy under `expand_to`. An exact expansion must be
larger than the observed volume size:

```json
{
  "action": "update",
  "resource": "volume",
  "volume": {
    "id": "volume-stable-id",
    "expand_to": { "mode": "maximum" }
  }
}
```

## SSD cache manifests

SSD caches attach to a volume and reuse the same `storage plan` / `storage apply`
flow under `resource: "cache"`. Read the current caches with `storage inventory`
(an `SSD CACHES` section appears when one exists) and check support with
`storage capabilities`.

Create a read-only cache from one or more SSDs:

```json
{
  "action": "create",
  "resource": "cache",
  "cache": {
    "name": "read-cache",
    "volume_id": "volume_1",
    "cache_type": "read_only",
    "disk_ids": ["sda"]
  }
}
```

A read-write cache requires a protection RAID and at least two SSDs:

```json
{
  "action": "create",
  "resource": "cache",
  "cache": {
    "name": "rw-cache",
    "volume_id": "volume_1",
    "cache_type": "read_write",
    "protection_raid": "raid1",
    "disk_ids": ["sda", "sdb"]
  }
}
```

Remove a cache by its stable DSM ID:

```json
{"action":"delete","resource":"cache","cache":{"id":"cache_1"}}
```

Cache SSD candidates must be SSD media, unused by a storage pool, selectable, and
healthy; the DSM boot SSDs (reported as `sys_partition_normal`) are accepted as
cache media. A volume may hold only one cache. Removing a **read-write** cache
flushes dirty data and is planned as high risk with a `cache_dirty_flush`
destructive consequence; removing a read-only cache is non-destructive.

The contract also models `update` (add SSDs) and `convert` (read-only↔read-write,
a dedicated `convert` action because read-write→read-only flushes dirty data).
These are only offered where a DSM advertises the backend; the discovered DSM 7.3
`SYNO.Storage.CGI.Flashcache` API exposes only create (`enable`) and remove
(`remove`), so `storage capabilities` reports cache expand and convert as
unsupported there and planning fails closed.

## Plan binding

The storage plan contains:

- the NAS profile and canonical intent;
- the stable pool, volume, and disk references;
- a resource-state precondition and fingerprint;
- a normalized topology fingerprint that excludes volatile health,
  temperature, and usage counters;
- a separate selected-disk and pool safety-state fingerprint;
- warnings and explicitly enumerated destructive consequences;
- a SHA-256 approval hash over all of the above.

Changing intent, a stable reference, resolved capacity, observed resource
state, topology, or mutation safety state changes the approval hash. Apply
validates the schema and hash before selecting the exact pool or volume
backend. RAID-migration requests still stop without calling a DSM write API.

CLI schemas:

```text
dsmctl storage plan --nas office --file request.json --output plan.json
dsmctl storage apply --file plan.json --approve <sha256>
```

MCP schemas:

```text
plan_storage_change { nas?: string, request: StorageChangeRequest }
apply_storage_plan { plan: StoragePlan, approval_hash: string }
```

## SAN backing-volume preconditions

LUN create and expansion plans bind the stable backing-volume ID, path,
filesystem, status, and read-only state. The exact available-byte counter is
deliberately excluded from the approval fingerprint because normal package and
filesystem activity changes it continuously. Apply still re-reads storage
state and recomputes the request: if current free space is below the requested
LUN capacity, or the volume is no longer normal and writable, the operation
fails before DSM receives a SAN mutation.
