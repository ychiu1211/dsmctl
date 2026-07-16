# v0.1 architecture

## Goal

The first release proves one complete path shared by both products:

1. Select one of multiple named NAS profiles.
2. Resolve its password without placing a secret in the configuration model.
3. Discover supported DSM APIs and versions.
4. Authenticate with password, optional OTP, and a DSM trusted-device credential.
5. Retain an independent session for that NAS.
6. Read normalized system information through CLI and MCP.

## Dependency direction

```text
CLI adapter ----+
                +--> Application service --> Runtime/session manager --> Synology client --> DSM
MCP adapter ----+                                  |                        |
                                                   +--> Config              +--> API discovery
                                                   +--> Credential stores
```

Dependencies only point to the right:

- MCP does not invoke the CLI process.
- CLI and MCP do not construct raw DSM WebAPI calls.
- The Synology client has no knowledge of commands, tools, or profile storage.
- Secret values never enter configuration or display models.

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
dsmctl auth login [--nas <name>]
dsmctl system info [--nas <name>] [--json]
```

MCP:

```text
list_nas
get_system_info { nas?: string }
```

## Extension rule

A new management feature normally adds three pieces:

1. A typed method and response model under `internal/synology`.
2. A use case under `internal/application`, including validation, idempotency, and safety policy.
3. Thin CLI and MCP adapters.

Raw DSM calls are not exposed as MCP tools. Mutating operations will use plan/apply semantics so a CLI user or MCP host can inspect potentially destructive changes before execution.

## Planned follow-ups

- Credential removal, status, and trusted-device rotation commands.
- DSM error descriptions and structured application errors.
- Capability reporting and DSM compatibility contract tests.
- Control Panel read operations, followed by plan/apply mutations.
- SAN inventory, followed by guarded LUN and target mutations.
