# Account and shared-folder management

Account and shared-folder writes share one guarded workflow across CLI and MCP:

1. Submit a declarative JSON change to `plan`.
2. dsmctl validates it and reads the current DSM state.
3. Review the returned request, risk, precondition, summary, and `hash`.
4. Submit the unchanged plan and exact hash to `apply`.
5. dsmctl re-reads DSM, rejects stale or forged plans, performs the typed WebAPI operation, and verifies the result.

Plans are single-use in practice: after a successful mutation, the old precondition no longer matches.

## Users

Create a user without putting its password in JSON:

```json
{
  "action": "create",
  "resource": "user",
  "user": {
    "name": "automation",
    "description": "Managed by dsmctl",
    "email": "automation@example.com",
    "expired": "normal",
    "cannot_change_password": true,
    "password_never_expires": true,
    "credential_ref": "env:DSMCTL_NEW_USER_PASSWORD"
  }
}
```

Set the environment variable only in the process that runs apply. The resolved value is sent over HTTPS directly to DSM and never appears in the plan or result.

Update fields by including only the desired values. Empty strings clear string settings:

```json
{
  "action": "update",
  "resource": "user",
  "user": {
    "name": "automation",
    "new_name": "nas-automation",
    "description": "NAS automation service account",
    "email": ""
  }
}
```

Delete is intentionally explicit:

```json
{"action":"delete","resource":"user","user":{"name":"nas-automation"}}
```

`expired` accepts `normal`, `now`, or a DSM date in `YYYY/M/D` form. A `credential_ref` on update changes the password at apply time.

## Groups

Groups use the same `create`, `update`, and `delete` actions:

```json
{
  "action": "create",
  "resource": "group",
  "group": {
    "name": "nas-operators",
    "description": "Operators managed by dsmctl"
  }
}
```

Use `new_name` and/or `description` for updates.

## Memberships

Membership is a declarative `set` resource. `groups` is the complete desired direct group set for one user, so planning can show exact additions and removals. DSM's mandatory `users` group must always be present:

```json
{
  "action": "set",
  "resource": "membership",
  "membership": {
    "user": "automation",
    "groups": ["users", "nas-operators"]
  }
}
```

Removing a group or adding `administrators` makes the plan high-risk. Built-in users remain protected from mutation.

## User and group quotas

Quota changes are partial: targets listed in `limits` are changed and all unspecified targets are preserved. Values are integer MiB; zero means unlimited. DSM reports whether a target supports a volume or shared-folder quota, and planning rejects targets absent from that principal's quota inventory.

```json
{
  "action": "set",
  "resource": "quota",
  "quota": {
    "principal_type": "group",
    "principal": "nas-operators",
    "limits": [
      {"target_type":"share","target":"team-data","quota_mib":10240}
    ]
  }
}
```

`principal_type` is `user` or `group`; `target_type` is `volume` or `share`.

## Application privileges

Application privilege changes are also partial. Each rule sets an explicit `allow` or `deny`, or uses `inherit` to remove the explicit rule and return to DSM inheritance:

```json
{
  "action": "set",
  "resource": "application_privilege",
  "application_privilege": {
    "principal_type": "user",
    "principal": "automation",
    "permissions": [
      {"application_id":"SYNO.SDS.App.FileStation3.Instance","access":"allow"},
      {"application_id":"SYNO.FTP","access":"deny"}
    ]
  }
}
```

Inventory reports pre-existing IP-specific rules as `custom`, but plan/apply intentionally accepts only `allow`, `deny`, or `inherit`. Changing a custom rule requires explicit replacement and is always high-risk.

## Shared folders

Create supports common reversible DSM settings:

```json
{
  "action": "create",
  "resource": "share",
  "share": {
    "name": "team-data",
    "volume_path": "/volume1",
    "description": "Team data",
    "hidden": false,
    "recycle_bin": true,
    "recycle_bin_admin_only": true,
    "hide_unreadable": true,
    "enable_cow": true,
    "enable_compression": false,
    "quota_mib": 102400
  }
}
```

Update accepts the same optional settings plus `new_name`; moving an existing share between volumes is rejected. DSM requires the current `vol_path` even for an in-place update, so dsmctl reads and supplies that value internally rather than exposing it as repeated user intent. `quota_mib` maps to DSM's MiB setting, while inventory normalizes configured quota and usage to bytes. A quota of zero disables the quota. Delete accepts only the current name and is bound to the UUID observed during planning.

Encrypted shares, encryption-key storage, WORM, and custom Windows ACLs are deliberately deferred because they need separate irreversible-action and key-lifecycle policies.

## Permissions

Set one principal's permissions on one or more shares:

```json
{
  "action": "set",
  "resource": "permission",
  "permission": {
    "principal_type": "group",
    "principal": "nas-operators",
    "permissions": [
      {"share_name":"team-data","access":"write"},
      {"share_name":"archive","access":"read"}
    ]
  }
}
```

`principal_type` is `user` or `group`. Access is `none`, `read`, `write`, or `deny`. Permission plans are high-risk because they can reduce or revoke access.

## CLI

```console
dsmctl account plan --nas office -f change.json -o change.plan.json
dsmctl account apply -f change.plan.json --approve <hash>

dsmctl share plan --nas office -f change.json -o change.plan.json
dsmctl share apply -f change.plan.json --approve <hash>
```

Use `-` for stdin/stdout. JSON decoding rejects unknown fields and trailing values.

Expanded reads are opt-in:

```console
dsmctl account inventory --nas office --memberships --json
dsmctl account inventory --nas office --quotas --application-privileges --principal-type user --principal automation --json
```

Without a principal filter, quota and application privilege reads fan out across all local users and groups.
