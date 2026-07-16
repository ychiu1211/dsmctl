# dsmctl

`dsmctl` is a Go client for managing one or more Synology DSM systems. One repository produces two front ends backed by the same typed DSM WebAPI client:

- `dsmctl`: a command-line interface for administrators.
- `dsmctl-mcp`: a stdio MCP server for AI clients.

The first milestone implements one complete connection slice: configure multiple NAS profiles, authenticate with password and DSM two-factor authentication, maintain independent sessions, and read basic system information. Management modules now cover storage and SAN inventory, guarded storage-pool, volume, and SAN lifecycles, local user/group/share management, effective-access explanation, a focused read-only Control Panel time module, and guarded global SMB/NFS File Services through the same CLI/MCP/application stack.

## Architecture

```text
CLI ---------+
             +--> application --> runtime/session manager --> Synology client façade
MCP server --+                           |                              |
                                         +--> OS credential store       +--> compatibility router
                                                                               |
                                                                               +--> operation variant --> WebAPI executor --> DSM
```

CLI and MCP never construct raw DSM requests or select DSM versions. Each operation chooses a backend from the APIs and versions advertised by the NAS, with narrowly scoped DSM-release overrides when behavior actually differs. See [the compatibility architecture](docs/compatibility.md).

Planned work and multi-agent coordination live in [the specification index](spec/README.md). Specs describe incomplete work; `docs/` describes implemented behavior.

Focused Control Panel module conventions are documented in [the Control Panel guide](docs/control-panel.md).

## Build

Go 1.25 or newer is required.

```console
go test ./...
go build -o bin/dsmctl ./cmd/dsmctl
go build -o bin/dsmctl-mcp ./cmd/dsmctl-mcp
```

On Windows, use `bin/dsmctl.exe` and `bin/dsmctl-mcp.exe`.

## Quick start

Add a NAS profile:

```console
dsmctl nas add office --url https://nas.example.com:5001 --username automation --default
```

Authenticate interactively:

```console
dsmctl auth login --nas office
```

The password and DSM trusted-device credential are stored in the operating system's credential store, not in `config.json`. Password and OTP prompts are hidden. If DSM requests an OTP, `dsmctl` exchanges it for a trusted-device ID so later CLI and MCP processes can authenticate without transporting OTP values through an AI client.

Read system information:

```console
dsmctl system info
dsmctl system info --nas office --json
dsmctl nas capabilities --nas office
dsmctl storage capabilities --nas office
dsmctl storage inventory --nas office
dsmctl storage inventory --nas office --json
dsmctl san capabilities --nas office
dsmctl san inventory --nas office --json
dsmctl san plan --nas office --file lun-change.json --output lun-change.plan.json
dsmctl san apply --file lun-change.plan.json --approve <plan-sha256>
dsmctl control-panel time state --nas office --json
dsmctl control-panel file-services capabilities --nas office
dsmctl control-panel file-services smb state --nas office --json
dsmctl control-panel file-services nfs state --nas office --json
dsmctl account capabilities --nas office
dsmctl account inventory --nas office --json
dsmctl account inventory --nas office --memberships --json
dsmctl account inventory --nas office --quotas --application-privileges --principal-type user --principal automation --json
dsmctl share capabilities --nas office
dsmctl share inventory --nas office
dsmctl share inventory --nas office --include-permissions --json
dsmctl access explain --nas office --principal-type user --principal automation --resource-type share --resource team-data --json
```

Account and shared-folder writes use a two-step plan/apply contract. Put the desired change in JSON, create a plan bound to the current DSM resource ID/state, review it, then apply that exact plan with its hash:

```console
dsmctl account plan --nas office --file create-user.json --output create-user.plan.json
dsmctl account apply --file create-user.plan.json --approve <hash-from-plan>

dsmctl share plan --nas office --file create-share.json --output create-share.plan.json
dsmctl share apply --file create-share.plan.json --approve <hash-from-plan>

dsmctl control-panel file-services plan --nas office --file smb-change.json --output smb-change.plan.json
dsmctl control-panel file-services apply --file smb-change.plan.json --approve <hash-from-plan>

dsmctl control-panel time plan --nas office --file time-change.json --output time-change.plan.json
dsmctl control-panel time apply --file time-change.plan.json --approve <hash-from-plan>
```

User passwords are never included in requests or plans. A user create/change refers to an apply-time environment variable such as `"credential_ref":"env:DSMCTL_NEW_USER_PASSWORD"`. Request formats and examples are in [account and share management](docs/account-share-management.md).

Manage more than one NAS:

```console
dsmctl nas add lab --url https://192.168.10.20:5001 --username automation
dsmctl nas list
dsmctl auth login --nas lab
dsmctl system info --nas lab
dsmctl nas use lab
```

The default configuration path is the platform user-config directory, such as `%AppData%\dsmctl\config.json` on Windows. Override it with `--config` or `DSMCTL_CONFIG`.

Example configuration (no secret values):

```json
{
  "default_nas": "office",
  "nas": {
    "office": {
      "url": "https://nas.example.com:5001",
      "username": "automation",
      "password_env": "DSMCTL_PASSWORD_OFFICE",
      "timeout_seconds": 30
    }
  }
}
```

`password_env` remains available as a non-interactive password fallback:

```powershell
$env:DSMCTL_PASSWORD_OFFICE = "your-password"
```

For a 2FA-protected account, run `dsmctl auth login` once on the same host so DSM's trusted-device credential can be placed in the OS credential store.

Use `--insecure-skip-tls-verify` only for a test NAS with a certificate that cannot be trusted. TLS verification is enabled by default.

## MCP server

Run the stdio server:

```console
dsmctl-mcp --config C:\path\to\config.json
```

Available tools:

- `list_nas`: list safe profile metadata; secrets are never returned.
- `get_system_info`: authenticate to a selected profile and return normalized DSM system information.
- `get_capabilities`: report discovered APIs, DSM release, compatibility quirks, and the backend selected for each operation.
- `get_storage_capabilities`: report the storage operations currently exposed for a selected NAS and the selected DSM backend.
- `get_storage_state`: return normalized disk, storage-pool, RAID, volume, capacity, and health state without changing the NAS.
- `plan_storage_change`: validate a storage-pool or volume intent and return a topology-, capacity-, and safety-state-bound approval plan without mutating DSM.
- `apply_storage_plan`: apply an approved, unchanged storage-pool or volume create/expand/delete plan and verify the postcondition.
- `get_san_capabilities`: report SAN Manager inventory and guarded mutation support plus the selected backends.
- `get_san_state`: return normalized iSCSI targets, LUNs, and their stable-ID mapping graph using bulk reads.
- `plan_san_change`: validate a target, LUN, or mapping intent and return a state-bound approval plan without mutating DSM.
- `apply_san_plan`: apply an approved, unchanged SAN plan and verify stable-ID and mapping-graph postconditions.
- `get_control_panel_time_capabilities`: report read and guarded set support plus the selected backend for the focused time/NTP module.
- `get_control_panel_time_state`: return normalized time zone, display formats, synchronization mode, and NTP servers.
- `plan_control_panel_time_change`: validate a patch-only time zone, display format, or NTP request and return a full-state-bound approval plan; manual mode and wall-clock changes are rejected.
- `apply_control_panel_time_plan`: apply an approved, unchanged time plan and verify the configuration without claiming NTP reachability.
- `get_file_service_capabilities`: report independent SMB/NFS read and guarded setting backends.
- `get_smb_state`: return normalized global SMB service, workgroup, protocol, encryption, and signing settings.
- `get_nfs_state`: return normalized global NFS service, protocol, and NFSv4 domain settings.
- `plan_file_service_change`: validate a patch-only SMB/NFS request and return a full-state-bound approval plan.
- `apply_file_service_plan`: apply an approved unchanged SMB/NFS plan and verify the postcondition.
- `get_account_capabilities`: report local user, group, membership, quota, and application privilege operations plus their selected DSM backends.
- `get_account_state`: return normalized local users and groups; membership, quota, and explicit application privilege expansion is opt-in and may be filtered to one principal.
- `plan_account_change`: validate a user, group, membership, quota, or application privilege change and return a current-state-bound approval plan without mutating DSM.
- `apply_account_plan`: apply an approved, unchanged account plan and verify the postcondition.
- `get_share_capabilities`: report shared-folder and permission capabilities plus their selected DSM backends.
- `get_share_state`: return normalized shared folders; set `include_permissions` only when the user/group permission matrix is required.
- `plan_share_change`: validate a shared-folder or permission change and return an approval plan without mutating DSM.
- `apply_share_plan`: apply an approved, unchanged shared-folder plan and verify the postcondition.
- `explain_effective_access`: explain one principal's share or application access with direct/group evidence and conservative indeterminate results for custom rules.

Storage-pool create, add-disk expansion, and delete, plus volume create, expansion, and delete, require independently selected backends and guarded plan/apply; storage-pool RAID migration remains fail-closed. Local user/group CRUD, memberships, per-user/group quotas, explicit application access, shared-folder CRUD, normalized `none`/`read`/`write`/`deny` share permissions, and guarded SAN target/LUN/mapping lifecycles are also available only through plan/apply. SAN deletes refuse active sessions or mappings, mappings never cascade-delete endpoints, and LUN capacity is checked against the selected backing volume. Guarded time-module changes never write wall-clock values or switch to manual synchronization, and NTP servers are validated for syntax without any reachability claim. Encrypted shares, WORM, custom Windows ACLs, IP-specific application rules, and SAN snapshots/clones remain out of scope.

Account expansion is opt-in because DSM exposes quota and application rules per principal. For large systems, filter `get_account_state` or `account inventory` with `principal_type` plus `principal` instead of reading every local principal. Membership expansion scales with local groups rather than users.

Permission expansion is opt-in because DSM exposes permissions by user and group. `get_share_state {"include_permissions":true}` and the matching CLI flag perform additional read-only calls for every local user and group, then aggregate the results by shared folder.

MCP intentionally has no tool that accepts a password or OTP. If interactive authentication is required, it returns an actionable error asking the user to run `dsmctl auth login --nas <name>` first.

An opt-in live smoke test verifies the real stdio process boundary:

```powershell
$env:DSMCTL_MCP_BINARY = (Resolve-Path .\bin\dsmctl-mcp.exe).Path
$env:DSMCTL_LIVE_NAS = "office"
go test ./integration -run TestMCPGetSystemInfoLive -v
```

## Security model

- Passwords, OTP values, DSM device IDs, SIDs, and SynoTokens are never stored in the config file or returned by CLI/MCP display models.
- Account inventory never returns passwords, hashes, recovery codes, or other authentication material.
- User create/password updates accept only an `env:NAME` reference; the secret is resolved at apply time and is absent from the request, plan, hash, result, and logs.
- Every account/share mutation re-reads DSM state before apply. Deletes are bound to the planned UID/GID or shared-folder UUID plus a state fingerprint.
- Built-in `admin`, `guest`, `root`, `administrators`, `users`, `http`, `home`, and `homes` resources cannot be managed by mutation commands.
- Login parameters use an HTTPS POST form, not URL query parameters.
- OTP values are short-lived and never persisted.
- Passwords and trusted-device IDs use Windows Credential Manager, macOS Keychain, or Linux Secret Service.
- Every NAS profile owns a separate session and trusted-device credential.
- DSM session errors 106 and 119 trigger one automatic re-login and retry.

## Adding commands

A management feature normally adds:

1. A stable result model and one or more variants under `internal/synology/operations/<operation>`.
2. Capability matchers for the shared backend and only the version-specific overrides that differ.
3. A use case and policy under `internal/application`.
4. Thin CLI and MCP adapters.

Raw generic DSM calls are not exposed as MCP tools. Mutating operations use guarded plan/apply semantics as the project expands into Control Panel and SAN management.
