# Control Panel modules

Control Panel support is organized as small typed modules. Each module owns
its state, capability names, DSM API variants, CLI subtree, and MCP tools. This
keeps a DSM-version change in one setting area from turning the shared wrapper
into an untyped `set key=value` API.

## Time and NTP

The first module is read-only and returns the configured time zone, DSM date
and time display formats, synchronization mode, and ordered NTP server list:

```console
dsmctl control-panel time capabilities --nas office
dsmctl control-panel time state --nas office --json
```

MCP exposes the same application results through
`get_control_panel_time_capabilities` and `get_control_panel_time_state`.

The compatibility layer selects `SYNO.Core.Region.NTP` v3, then v2, then a
legacy v1 decoder. V1 does not provide the display-format fields, so they are
omitted instead of synthesized. A missing API makes only this module
unsupported; it does not disable storage, SAN, account, or share features.

No time or NTP mutation is exposed. A future module-specific change contract
must define clock-change risk, stale-state protection, postcondition checks,
and NTP reachability behavior before `set` can be enabled.

## SMB and NFS File Services

SMB and NFS are separate compatibility and failure boundaries even though DSM
shows them on the same File Services page:

```console
dsmctl control-panel file-services capabilities --nas office
dsmctl control-panel file-services smb state --nas office --json
dsmctl control-panel file-services nfs state --nas office --json
```

The normalized SMB state contains the service switch, workgroup, minimum and
maximum SMB protocol, transport-encryption policy, and server-signing policy.
`disabled_for_smb1` is intentionally distinct from fully disabled signing: it
is the meaning of DSM's value `0`. The NFS state contains the service switch,
configured maximum protocol, protocols advertised by DSM, and the NFSv4 domain
when the advanced read API is available.

Base settings use the same hash-bound plan/apply flow for CLI and MCP. A request
owns only its non-null fields; the plan records and hashes the complete current
module state. Apply rejects a changed state, refreshes all normalized fields
before calling DSM, and verifies the requested fields afterward.

Example SMB request:

```json
{
  "protocol": "smb",
  "smb": {
    "minimum_protocol": "smb2",
    "maximum_protocol": "smb3",
    "transport_encryption": "required",
    "server_signing": "required"
  }
}
```

Example NFS request:

```json
{
  "protocol": "nfs",
  "nfs": {
    "enabled": true,
    "maximum_protocol": "nfs4.1"
  }
}
```

```console
dsmctl control-panel file-services plan --nas office --file change.json --output change.plan.json
dsmctl control-panel file-services apply --file change.plan.json --approve <hash-from-plan>
```

MCP exposes `get_file_service_capabilities`, `get_smb_state`, `get_nfs_state`,
`plan_file_service_change`, and `apply_file_service_plan` over the identical
application contract.

DSM's NFS advanced-setting form submits its complete port, packet-size, UNIX
permission, service-state, and domain snapshot. The current domain model reads
`nfsv4_domain` but deliberately reports `set_advanced: false`; domain writes
remain fail-closed until all required preservation fields have a stable typed
contract. Per-shared-folder NFS host/export rules are also a separate future
share module.

## Adding another module

Add a dedicated type under `internal/domain/controlpanel`, an operation package
with strict response decoding and version-scoped variants, and one Synology
facade. Then expose that facade through the shared application service, CLI,
MCP, and compatibility report. Do not add raw DSM response maps or a generic
settings mutation endpoint.
