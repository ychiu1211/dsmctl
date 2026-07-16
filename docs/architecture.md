# v0.1 architecture

## Goal

The first release proves one complete path shared by both products:

1. Select one of multiple named NAS profiles.
2. Resolve its password without placing a secret in the configuration model.
3. Discover supported DSM APIs and versions.
4. Authenticate with password, optional OTP, and a DSM trusted-device credential.
5. Retain an independent session for that NAS.
6. Read normalized system information through CLI and MCP.
7. Read a normalized disk, storage-pool, RAID, volume, capacity, and health inventory through the same application service.

## Dependency direction

```text
CLI adapter ----+
                +--> Application service --> Runtime/session manager --> Synology client façade
MCP adapter ----+                                  |                               |
                                                   +--> Config                     +--> Compatibility target/router
                                                   +--> Credential stores                    |
                                                                                             +--> Operation variant
                                                                                                      |
                                                                                                      +--> WebAPI executor --> DSM
```

Dependencies only point to the right:

- MCP does not invoke the CLI process.
- CLI and MCP do not construct raw DSM WebAPI calls.
- The Synology client has no knowledge of commands, tools, or profile storage.
- Secret values never enter configuration or display models.

## Version compatibility

Version selection is operation-scoped rather than implemented as monolithic DSM 6, DSM 7, or DSM 8 clients. A target contains the DSM release, discovered WebAPI catalog, derived capabilities, and known transport or API quirks. Each operation registers a small ordered set of variants.

`system.info` currently demonstrates the pattern:

```text
core-system-v3          SYNO.Core.System v3   priority 30
core-system-v2          SYNO.Core.System v2   priority 20
core-system-v1-legacy   SYNO.Core.System v1   priority 10
```

The highest-priority matching variant is selected. Shared HTTP, session, retry, validation, and normalization behavior stays in the executor and common decoder. A future DSM-specific override uses a higher priority plus both API and DSM-range matchers, without copying unrelated operations.

`storage.inventory` follows the same operation-scoped pattern. Its first backend uses `SYNO.Storage.CGI.Storage` v1 and normalizes the aggregate response into the stable `internal/domain/storage` model. Future DSM-specific field or endpoint differences can add a higher-priority variant without changing the CLI, MCP schemas, or application use case.

See [`docs/compatibility.md`](compatibility.md) for rules and extension examples.

## Authentication flow

```text
Password from OS keyring (or environment fallback)
                       |
                       v
       Login with saved device_name + device_id
                       |
             +---------+---------+
             |                   |
          success          DSM requests OTP
             |                   |
             |          CLI prompts without echo
             |                   |
             |          Login with otp_code and
             |          enable_device_token=yes
             |                   |
             +---------+---------+
                       |
              SID + SynoToken + did
```

The returned `did` and its `device_name` are stored per profile in the OS credential store. Later CLI and MCP processes reuse that pair. MCP never accepts password or OTP inputs; if the trusted device is absent or expired, the user completes `dsmctl auth login` in a terminal.

The client prefers `SYNO.API.Auth` v6, clamped to the versions reported by API discovery. Version 6 supplies SynoToken and trusted-device support. OTP-only login remains possible with older supported Auth versions, but a trusted device requires v6 or newer.

## Session model

`runtime.Manager` owns a map keyed by NAS profile name. Each entry is a separate `synology.Client` containing its own SID, SynoToken, trusted-device state, discovered API versions, and HTTP transport. Clients are created lazily and reused until the CLI command or MCP process exits.

Calls to one NAS are serialized inside that NAS client to protect mutable session state. Calls to different profiles use different clients. DSM session errors 106 and 119 clear the local session, perform one automatic login, and retry the failed API call once.

## Public surfaces

CLI:

```text
dsmctl nas add <name>
dsmctl nas list
dsmctl nas use <name>
dsmctl nas remove <name>
dsmctl nas capabilities [--nas <name>] [--json]
dsmctl auth login [--nas <name>]
dsmctl system info [--nas <name>] [--json]
dsmctl storage capabilities [--nas <name>] [--json]
dsmctl storage inventory [--nas <name>] [--json]
```

MCP:

```text
list_nas
get_system_info { nas?: string }
get_capabilities { nas?: string }
get_storage_capabilities { nas?: string }
get_storage_state { nas?: string }
```

## Extension rule

A new management feature normally adds four pieces:

1. A canonical model and operation variants under `internal/synology/operations`.
2. Capability and version matchers, reusing the common executor and normalizer.
3. A use case under `internal/application`, including validation, idempotency, and safety policy.
4. Thin CLI and MCP adapters.

Raw DSM calls are not exposed as MCP tools. Mutating operations will use plan/apply semantics so a CLI user or MCP host can inspect potentially destructive changes before execution.

Storage MCP tools declare read-only, idempotent annotations. These are routing hints for MCP clients, while the actual safety boundary is that the application and Synology layers contain no storage mutation operation in this milestone.

## Planned follow-ups

- Credential removal, status, and trusted-device rotation commands.
- DSM error descriptions and structured application errors.
- Versioned storage manifests plus plan/apply and plan-hash preconditions.
- Guarded storage-pool and volume creation after write APIs are modeled per DSM version.
- Control Panel read operations, followed by plan/apply mutations.
- SAN inventory, followed by guarded LUN and target mutations.
