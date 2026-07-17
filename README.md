# dsmctl

`dsmctl` is a Go client for managing one or more Synology DSM systems. One repository produces three front ends backed by the same typed DSM WebAPI client:

- `dsmctl`: a command-line interface for administrators.
- `dsmctl-mcp`: a stdio MCP server for AI clients.
- `dsmctl-gateway`: a portable amd64 Streamable HTTP MCP gateway; its current developer mode is read-only.

The first milestone implements one complete connection slice: configure multiple NAS profiles, authenticate with password and DSM two-factor authentication, maintain independent sessions, and read basic system information. Management modules now cover storage and SAN inventory, guarded storage-pool, volume, and SAN lifecycles, local user/group/share management, effective-access explanation, a focused read-only Control Panel time module, guarded global SMB/NFS File Services, and Package Center inventory, settings, and guarded package lifecycle through the same CLI/MCP/application stack. Functionality provided by installed packages is managed through package-scoped operations that re-check the installed package version before every command; the read-only Synology Drive Admin module is the first consumer.

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
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/dsmctl-gateway ./cmd/dsmctl-gateway
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

`auth login` opens the NAS's own sign-in page in your web browser; the password — and any two-factor, passkey, or approve-sign-in step — is entered only there and never passes through dsmctl. The resulting DSM session (SID, SynoToken, and its renewal keys) is stored per profile in the operating system's credential store, not in `config.json`, and is reused by later CLI and MCP processes.

Inspect or remove the stored session without revealing any secret value:

```console
dsmctl auth status
dsmctl auth logout --nas office
```

`auth status` is fully offline. `auth logout` signs out: it asks DSM to revoke the session (best-effort; an unreachable NAS only skips the revocation) and then deletes the stored copy. `nas remove` also cleans the removed profile's stored session unless `--keep-credentials` is passed. Details and cross-process caveats are in [the credential lifecycle guide](docs/credentials.md).

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
dsmctl log capabilities --nas office
dsmctl log list --nas office
dsmctl log list --nas office --type connection --limit 50
dsmctl log list --nas office --from "2026-07-01" --to "2026-07-08"
dsmctl log list --nas office --keyword cache --level error --json
dsmctl package capabilities --nas office
dsmctl package inventory --nas office --json
dsmctl package settings --nas office --json
dsmctl resource-monitor capabilities --nas office
dsmctl resource-monitor current --nas office --json
dsmctl resource-monitor history --nas office --period week --dimension cpu --dimension memory
dsmctl resource-monitor setting --nas office
dsmctl drive admin capabilities --nas office
dsmctl drive admin status --nas office
dsmctl drive admin connections --nas office --json
dsmctl drive admin team-folders --nas office
dsmctl drive admin log list --nas office --username alice --limit 50
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

dsmctl package plan --nas office --file package-change.json --output package-change.plan.json
dsmctl package apply --file package-change.plan.json --approve <hash-from-plan>

dsmctl resource-monitor plan-recording --nas office --enable --output recording.plan.json
dsmctl resource-monitor apply-recording --file recording.plan.json --approve <hash-from-plan>
```

Package Center changes use the same plan/apply contract: a `lifecycle` action
(`start`, `stop`, or `uninstall`) on one installed package, or a `settings`
change to the automatic-update policy. The publisher trust level is read-only
(no DSM write endpoint). See [the Package Center guide](docs/package-center.md).

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

`password_env` remains available as a non-interactive password fallback for automation; it is consulted only when no web-login session is stored for the profile:

```powershell
$env:DSMCTL_PASSWORD_OFFICE = "your-password"
```

Two-factor authentication needs no special handling: the challenge is completed in the browser during `dsmctl auth login`, and the stored session is what later processes reuse.

Use `--insecure-skip-tls-verify` only for a test NAS with a certificate that cannot be trusted. TLS verification is enabled by default.

## MCP server

Run the stdio server:

```console
dsmctl-mcp --config C:\path\to\config.json
```

The portable HTTP developer gateway, its loopback-only Compose project, and
its read-only security boundary are documented in [the gateway guide](docs/gateway.md).

Available tools:

- `list_nas`: list safe profile metadata; secrets are never returned.
- `get_auth_status`: report per-NAS credential presence, the password environment variable name and set state, and this process's session state; fully offline and never returns secret values.
- `get_system_info`: authenticate to a selected profile and return normalized DSM system information.
- `get_capabilities`: report discovered APIs, DSM release, compatibility quirks, and the backend selected for each operation.
- `get_storage_capabilities`: report the storage operations currently exposed for a selected NAS and the selected DSM backend.
- `get_storage_state`: return normalized disk, storage-pool, RAID, volume, SSD cache, capacity, and health state without changing the NAS.
- `plan_storage_change`: validate a storage-pool, volume, or SSD cache intent and return a topology-, capacity-, and safety-state-bound approval plan without mutating DSM.
- `apply_storage_plan`: apply an approved, unchanged storage-pool, volume, or SSD cache plan and verify the postcondition.
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
- `get_log_capabilities`: report whether DSM system log reading is available and the selected backend.
- `get_logs`: read normalized DSM system log entries with optional keyword, log-type, severity, and paging filters; never mutates or clears logs.
- `get_package_capabilities`: report supported Package Center inventory, settings, and lifecycle operations plus the selected DSM backends; install and update report unsupported.
- `get_package_state`: return the normalized installed-package inventory with run status and start/stop/uninstall eligibility without changing packages.
- `get_package_settings`: return normalized global Package Center settings (publisher trust level and automatic-update policy); read-only.
- `get_drive_admin_capabilities`: report supported Drive Admin operations, the selected backends, and the installed SynologyDrive package version/running state the selection used.
- `get_drive_admin_status`: return the Drive service status with installed-package evidence; read-only.
- `get_drive_admin_connections`: list active Drive client connections; read-only.
- `get_drive_admin_team_folders`: list Drive team folders from the admin perspective; read-only.
- `get_drive_admin_logs`: read Drive server logs with keyword/username/target/time-range filters; read-only.
- `plan_package_change`: validate a start/stop/uninstall lifecycle action or an automatic-update settings change and return a state-bound approval plan without mutating DSM; install, update, and trust-level changes are rejected.
- `apply_package_plan`: apply an approved, unchanged Package Center plan (lifecycle or settings) and verify the terminal package-state or settings postcondition.

Storage-pool create, add-disk expansion, and delete, plus volume create, expansion, and delete, require independently selected backends and guarded plan/apply; storage-pool RAID migration remains fail-closed. SSD cache create and remove (read-only or read-write) are also guarded plan/apply on a `cache` resource; SSD cache expand and read-only/read-write conversion are modeled but fail closed on DSMs whose flashcache API exposes only create and remove. Local user/group CRUD, memberships, per-user/group quotas, explicit application access, shared-folder CRUD, normalized `none`/`read`/`write`/`deny` share permissions, and guarded SAN target/LUN/mapping lifecycles are also available only through plan/apply. SAN deletes refuse active sessions or mappings, mappings never cascade-delete endpoints, and LUN capacity is checked against the selected backing volume. Guarded time-module changes never write wall-clock values or switch to manual synchronization, and NTP servers are validated for syntax without any reachability claim. Package Center exposes installed-package inventory, global settings (publisher trust level and automatic-update policy), guarded automatic-update settings changes, and guarded start/stop/uninstall on installed packages; uninstall is refused for packages DSM reports as non-removable, and start/stop/uninstall verify the terminal package state. The publisher trust level is read-only (no DSM write endpoint). Package install-from-repository and update are modeled but fail closed because they contact the online repository, run asynchronously, and download and run remote code. The trust-level, beta-channel, and default-volume writes, the online catalog browse, and per-package application-specific settings are also deferred. The Drive Admin module is read-only in this slice (service status, connections, team folders, logs); it selects operations from the installed SynologyDrive package version, re-checks that version before every command, and fails closed on Drive releases older than the verified 3.0 baseline or when the package is missing. Team-folder changes are modeled but fail closed. Encrypted shares, WORM, custom Windows ACLs, IP-specific application rules, and SAN snapshots/clones remain out of scope.

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
- Web-login sessions (SID, SynoToken, and renewal keys) use Windows Credential Manager, macOS Keychain, or Linux Secret Service; dsmctl adds no encryption layer of its own because the OS store already provides at-rest encryption, and on hosts without a store (headless Linux) it fails closed instead of writing secrets to disk.
- Passwords are never stored by dsmctl: web login keeps them in the browser, and the optional automation fallback reads an environment variable at login time.
- Credential status reports booleans, metadata, and the environment variable name only; session IDs, tokens, keys, and password values are never displayed.
- `auth logout` and `nas remove` revoke the DSM session server-side (best-effort, with a bounded wait) before deleting the stored copy, so a signed-out session cannot be replayed; when the NAS is unreachable the local copy is still removed and the session lapses on its own expiry.
- Closing a process never invalidates the stored web-login session; only an explicit sign-out does.
- Every NAS profile owns a separate stored session.
- DSM session errors 106 and 119 trigger one automatic re-login and retry when the client logged in with a password; a client using a stored web-login session instead recovers without a password — it re-reads the store (picking up a newer `auth login` from another process) or renews the session with the stored Noise resume keys — and otherwise reports that a new `auth login` is required.

## Adding commands

A management feature normally adds:

1. A stable result model and one or more variants under `internal/synology/operations/<operation>`.
2. Capability matchers for the shared backend and only the version-specific overrides that differ.
3. A use case and policy under `internal/application`.
4. Thin CLI and MCP adapters.

Raw generic DSM calls are not exposed as MCP tools. Mutating operations use guarded plan/apply semantics as the project expands into Control Panel and SAN management.
