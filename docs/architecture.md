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
8. Read normalized local users, groups, memberships, quotas, application privileges, shared folders, and opt-in permission bindings without exposing authentication material.
9. Plan and apply guarded local identity and shared-folder changes through identical CLI and MCP use cases.
10. Plan and apply guarded storage-pool, volume, and SAN target/LUN/mapping changes through operation-scoped DSM backends.
11. Explain focused share/application access and read the first focused Control Panel time/NTP module.

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

Storage mutation manifests and hash-bound plans are also versioned at the
application boundary. Pool create owns its complete initial RAID/disk topology;
pool update is patch-only disk addition. Three independently selected
`SYNO.Storage.CGI.Pool` v1 variants implement create, add-disk expansion, and
delete. Plans bind stable DSM IDs, a non-volatile topology fingerprint, and a
separate disk/pool safety fingerprint. Volume create, expansion, and delete use
independently selected `SYNO.Storage.CGI.Volume` v1 variants behind the same
guarded plan/apply contract. Storage-pool RAID migration has no registered
backend and therefore fails closed before any DSM write. See
[`docs/storage-management.md`](storage-management.md) for schemas and examples.

SAN inventory and mutations use the same operation-scoped design. Target,
LUN, and mapping operations select independently; the application layer binds
stable target IDs, LUN UUIDs, mapping edges, active sessions, and backing-volume
capacity into a guarded plan. See [`docs/san-management.md`](san-management.md).

Identity and share support is split into independently selectable operations:

```text
identity.users.list         SYNO.Core.User v1
identity.groups.list        SYNO.Core.Group v1
identity.users.mutate       SYNO.Core.User v1
identity.groups.mutate      SYNO.Core.Group v1
identity.memberships.list   SYNO.Core.Group.Member v1
identity.memberships.set    SYNO.Core.Group.Member v1
identity.quotas.get         SYNO.Core.Quota v1
identity.quotas.set         SYNO.Core.Quota v1
identity.applications.list  SYNO.Core.AppPriv.App v2
identity.application_privileges.get  SYNO.Core.AppPriv.Rule v1
identity.application_privileges.preview  SYNO.Core.AppPriv.App v2
identity.application_privileges.set  SYNO.Core.AppPriv.Rule v1
shares.list                 SYNO.Core.Share v1
shares.permissions.list     SYNO.Core.Share.Permission v1
shares.mutate               SYNO.Core.Share v1
shares.permissions.mutate   SYNO.Core.Share.Permission v1
```

DSM returns share permissions from the perspective of one user or group. Permission expansion therefore lists local principals, calls `list_by_user` or `list_by_group` for each principal, and aggregates bindings into the canonical shared-folder model. This fan-out is opt-in so ordinary share inventory remains one management API call.

Functionality provided by an installed package selects on a third axis: the
installed package version. The compatibility target carries an
installed-package catalog loaded from the verified Package Center inventory,
refreshed before every package-scoped command, and package matchers compose
with the API/DSM matchers. The read-only Drive Admin module is the first
consumer:

```text
drive.admin.status.read       SYNO.SynologyDrive v1            + SynologyDrive >= 3.0
drive.admin.connections.read  SYNO.SynologyDrive.Connection v1 + SynologyDrive >= 3.0
drive.admin.teamfolders.read  SYNO.SynologyDrive.Share v1      + SynologyDrive >= 3.0
drive.admin.log.read          SYNO.SynologyDrive.Log v1        + SynologyDrive >= 3.0
drive.admin.teamfolders.set   (no backend; fails closed)
```

See [`docs/compatibility.md`](compatibility.md) for rules and extension examples.

## Authentication flow

```text
dsmctl auth login opens the DSM sign-in page in the user's browser
        (password / 2FA / passkey stay in the browser, against the NAS)
                       |
                       v
   One-time code returned to a loopback listener (PKCE-bound)
                       |
                       v
   Code exchanged over a Noise_IK channel with the NAS
                       |
                       v
   SID + SynoToken + device ID + durable Noise resume keys
                       |
                       v
   Stored per profile in the OS credential store
```

Later CLI and MCP processes seed their DSM client from the stored session
instead of logging in; closing such a client drops only the in-memory copy,
so the stored session stays valid across processes until `auth logout`
revokes it server-side and deletes it. The password resolver (environment
variable, or a password stored by an older release) remains as the
automation fallback and is consulted only when no session is stored. MCP
never accepts password or OTP inputs; if no usable session exists, the user
completes `dsmctl auth login` in a terminal.

The client prefers `SYNO.API.Auth` v7, clamped to the versions reported by API discovery. DSM 7.3 requires the v7 session for privileged Control Panel mutations such as local-user writes; the same client automatically falls back to v6 or an older advertised version instead of maintaining a separate DSM client. SynoToken and trusted-device support require v6 or newer.

WebAPI parameter shape remains operation-specific. User mutations use typed JSON parameters, and passwords are resolved only at apply time. HTTPS carries the value inside TLS; non-TLS legacy targets use DSM's advertised RSA/AES parameter envelope. This transport behavior stays in the common executor, so CLI and MCP share it without adding DSM-version branches.

## Session model

`runtime.Manager` owns a map keyed by NAS profile name. Each entry is a separate `synology.Client` containing its own SID, SynoToken, trusted-device state, discovered API versions, and HTTP transport. Clients are created lazily and reused until the CLI command or MCP process exits.

Two session lifetimes exist:

- A client seeded from the persisted web-login session borrows it: it is
  created with `PreserveSessionOnClose`, so `Manager.Close` drops its
  in-memory copy without contacting DSM. When DSM rejects the session, the
  client's `Resume` closure recovers without a browser or password: it first
  re-reads the store — picking up a session renewed by another process's
  `auth login`, which keeps a long-running MCP server usable after the
  session it started with expires — and otherwise performs the browserless
  Noise resume with the stored renewal keys, persisting the refreshed
  session. Revocation is an explicit act: `application.Service.Logout` (used
  by `auth logout` and `nas remove`) calls `Manager.RevokeStoredSession` with
  a bounded timeout, which asks DSM to invalidate the stored SID via the
  client's explicit `Logout` verb before the local entry is deleted
  (best-effort; an unreachable NAS never blocks the local removal).
- A client that logged in itself (password fallback) owns its session and
  logs it out on `Close`.

The persisted session is stored per profile in the OS credential store
(Windows Credential Manager, macOS Keychain, Linux Secret Service) under
`dsmctl` / `session/<profile>`, so many NAS can hold independent sessions at
once. The blob bundles the live SID/SynoToken with the durable Noise resume
keys; display paths only ever see the non-secret `SessionMeta` projection.
The credential store provides the at-rest encryption; dsmctl adds no layer of
its own, and on hosts without a store (headless Linux without a D-Bus Secret
Service) it fails closed rather than writing secrets to disk.

Calls to one NAS are serialized inside that NAS client to protect mutable session state. Calls to different profiles use different clients. DSM session errors 106 and 119 clear the local session, re-establish one, and retry the failed API call once; a session-seeded client has no password, so re-establishing means the store re-read plus Noise resume described above, and when neither yields a session it reports that a new `auth login` is required.

## Public surfaces

CLI:

```text
dsmctl nas add <name>
dsmctl nas list
dsmctl nas use <name>
dsmctl nas remove <name> [--keep-credentials]
dsmctl nas capabilities [--nas <name>] [--json]
dsmctl auth login [--nas <name>]
dsmctl auth status [--nas <name>] [--json]
dsmctl auth logout [--nas <name>]
dsmctl system info [--nas <name>] [--json]
dsmctl storage capabilities [--nas <name>] [--json]
dsmctl storage inventory [--nas <name>] [--json]
dsmctl storage plan [--nas <name>] --file <request.json> [--output <plan.json>]
dsmctl storage apply --file <plan.json> --approve <sha256>
dsmctl san capabilities [--nas <name>] [--json]
dsmctl san inventory [--nas <name>] [--json]
dsmctl san plan [--nas <name>] --file <request.json> [--output <plan.json>]
dsmctl san apply --file <plan.json> --approve <sha256>
dsmctl control-panel time capabilities [--nas <name>] [--json]
dsmctl control-panel time state [--nas <name>] [--json]
dsmctl control-panel time plan [--nas <name>] --file <request.json> [--output <plan.json>]
dsmctl control-panel time apply --file <plan.json> --approve <sha256>
dsmctl control-panel file-services capabilities [--nas <name>] [--json]
dsmctl control-panel file-services smb state [--nas <name>] [--json]
dsmctl control-panel file-services nfs state [--nas <name>] [--json]
dsmctl control-panel file-services plan [--nas <name>] --file <request.json> [--output <plan.json>]
dsmctl control-panel file-services apply --file <plan.json> --approve <sha256>
dsmctl account capabilities [--nas <name>] [--json]
dsmctl account inventory [--nas <name>] [--memberships] [--quotas] [--application-privileges] [--principal-type user|group --principal <name>] [--json]
dsmctl account plan [--nas <name>] --file <request.json> [--output <plan.json>]
dsmctl account apply --file <plan.json> --approve <sha256>
dsmctl share capabilities [--nas <name>] [--json]
dsmctl share inventory [--nas <name>] [--include-permissions] [--json]
dsmctl share plan [--nas <name>] --file <request.json> [--output <plan.json>]
dsmctl share apply --file <plan.json> --approve <sha256>
dsmctl access explain [--nas <name>] --principal-type user|group --principal <name> --resource-type share|application --resource <id> [--json]
dsmctl drive admin capabilities [--nas <name>] [--json]
dsmctl drive admin status [--nas <name>] [--json]
dsmctl drive admin connections [--nas <name>] [--json]
dsmctl drive admin team-folders [--nas <name>] [--json]
dsmctl drive admin log list [--nas <name>] [--limit <n>] [--keyword <text>] [--username <name>] [--target <path>] [--from <time>] [--to <time>] [--json]
```

MCP:

```text
list_nas
get_auth_status { nas?: string }
get_system_info { nas?: string }
get_capabilities { nas?: string }
get_storage_capabilities { nas?: string }
get_storage_state { nas?: string }
plan_storage_change { nas?: string, request: StorageChangeRequest }
apply_storage_plan { plan: StoragePlan, approval_hash: string }
get_san_capabilities { nas?: string }
get_san_state { nas?: string }
plan_san_change { nas?: string, request: SANChangeRequest }
apply_san_plan { plan: SANPlan, approval_hash: string }
get_control_panel_time_capabilities { nas?: string }
get_control_panel_time_state { nas?: string }
plan_control_panel_time_change { nas?: string, request: TimeChange }
apply_control_panel_time_plan { plan: ControlPanelTimePlan, approval_hash: string }
get_file_service_capabilities { nas?: string }
get_smb_state { nas?: string }
get_nfs_state { nas?: string }
plan_file_service_change { nas?: string, request: FileServiceChangeRequest }
apply_file_service_plan { plan: FileServicePlan, approval_hash: string }
get_account_capabilities { nas?: string }
get_account_state { nas?: string, include_memberships?: boolean, include_quotas?: boolean, include_application_privileges?: boolean, principal_type?: "user"|"group", principal?: string }
plan_account_change { nas?: string, request: IdentityChangeRequest }
apply_account_plan { plan: IdentityPlan, approval_hash: string }
get_share_capabilities { nas?: string }
get_share_state { nas?: string, include_permissions?: boolean }
plan_share_change { nas?: string, request: ShareChangeRequest }
apply_share_plan { plan: SharePlan, approval_hash: string }
explain_effective_access { nas?: string, principal_type: "user"|"group", principal: string, resource_type: "share"|"application", resource: string }
get_drive_admin_capabilities { nas?: string }
get_drive_admin_status { nas?: string }
get_drive_admin_connections { nas?: string }
get_drive_admin_team_folders { nas?: string }
get_drive_admin_logs { nas?: string, limit?: number, keyword?: string, username?: string, target?: string, from?: number, to?: number }
```

## Extension rule

A new management feature normally adds four pieces:

1. A canonical model and operation variants under `internal/synology/operations`.
2. Capability and version matchers, reusing the common executor and normalizer.
3. A use case under `internal/application`, including validation, idempotency, and safety policy.
4. Thin CLI and MCP adapters.

Raw DSM calls are not exposed as MCP tools. Account and shared-folder mutations use plan/apply semantics so a CLI user or MCP host can inspect potentially destructive changes before execution. Plans contain the resolved NAS profile, canonical intent, stable resource identifier, observed-state fingerprint, risk label, and SHA-256 approval hash. Apply accepts only the unchanged canonical plan, re-reads the resource, checks the precondition, performs one typed operation, and verifies the postcondition.

Planning and inventory MCP tools declare read-only annotations. Apply tools declare mutating/destructive annotations; these are routing hints, while the application plan hash, optimistic precondition, protected-resource policy, credential references, and postcondition checks form the actual safety boundary.

## Planned follow-ups

Incomplete work, dependencies, ownership, and acceptance criteria are maintained
in the [specification roadmap](../spec/roadmap.md). This document describes the
architecture implemented today.
