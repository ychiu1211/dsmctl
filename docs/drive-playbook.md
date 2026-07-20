# Drive setup playbook: from zero to a working team

This runbook takes a NAS from "no Drive" to "team working in a shared folder
with versioning", then covers the day-2 operations, using only shipped dsmctl
commands. Every mutation is a hash-bound plan/apply: run the plan, read the
returned summary/warnings, then apply with the exact hash. Each step names
the MCP tool an agent would use instead.

Every step of this flow was executed live against a DSM 7.3.2 target with
Synology Drive Server 4.0.3 during WI-050…WI-056 verification.

## 1. Install Synology Drive Server

```console
dsmctl package available --nas office            # is SynologyDrive offered?
dsmctl package install SynologyDrive --volume /volume1 --nas office
dsmctl package install SynologyDrive --volume /volume1 --nas office --approve <hash>
dsmctl drive admin status --nas office           # service enabled?
```

Missing dependencies install first automatically. MCP:
`get_package_available`, `plan_package_install`, `apply_package_install_plan`,
then `get_drive_admin_status`. Keep it current later with
`package available --updates` and `package update SynologyDrive` (plan/apply;
upgrades cannot be rolled back).

## 2. Create the shared folder

```json
{"action":"create","resource":"share","share":{
  "name":"team-data","volume_path":"/volume1",
  "description":"Team data","recycle_bin":true}}
```

```console
dsmctl share plan --nas office -f share.json -o share.plan.json
dsmctl share apply -f share.plan.json --approve <hash>
```

MCP: `plan_share_change` / `apply_share_plan`.

## 3. Accounts and permissions

Create users/groups, set memberships, then grant the shared-folder
permission (account and share modules; see
[account-share-management.md](account-share-management.md)):

```json
{"action":"set","resource":"permission","permission":{
  "principal_type":"group","principal":"team",
  "permissions":[{"share_name":"team-data","access":"write"}]}}
```

Who may use Drive itself is the DSM **application privilege**
(`SYNO.SDS.Drive.Application`) — grant or deny it per user/group through the
account module:

```json
{"action":"set","resource":"application_privilege","application_privilege":{
  "principal_type":"group","principal":"team",
  "permissions":[{"application_id":"SYNO.SDS.Drive.Application","access":"allow"}]}}
```

Check the result with `dsmctl drive admin users --nas office` and explain any
surprise with `dsmctl access explain`. MCP: `plan_account_change` /
`apply_account_plan`, `get_drive_users`, `explain_effective_access`.

## 4. Enable the team folder

```json
{"action":"enable","name":"team-data","max_versions":8,
 "version_policy":"smart","retention_days":0}
```

```console
dsmctl drive admin team-folders plan --nas office -f enable.json -o tf.plan.json
dsmctl drive admin team-folders apply -f tf.plan.json --approve <hash>
dsmctl drive admin team-folders --nas office     # enabled with versioning?
```

`max_versions` is required (DSM refuses the enable without it) and
`version_policy` must be explicit while versioning is on. Adjust later with
`set_versioning` (reducing versions prunes stored ones — high risk), and tune
the global My Drive policy through the `homes/mydrive_home` entry (always
high risk: it touches every user home). MCP: `plan_drive_team_folder_change`
/ `apply_drive_team_folder_plan`, `get_drive_admin_team_folders`.

## 5. Day-2 operations

| Task | CLI | MCP |
| --- | --- | --- |
| Service health | `drive admin status` / `summary` / `db-usage` | `get_drive_admin_status`, `get_drive_connection_summary`, `get_drive_db_usage` |
| Who is connected | `drive admin connections` | `get_drive_admin_connections` |
| Kick a stale client | `drive admin connections kick --session <id>` + `apply` | `plan_drive_connection_kick` / `apply_drive_connection_kick_plan` |
| Who may use Drive | `drive admin users` (+ account module to change) | `get_drive_users`, `plan_account_change` |
| Audit activity | `drive admin log list --username … --team-folder …` | `get_drive_admin_logs` |
| Export the audit log | `drive admin log export -o drive.csv` | `get_drive_log_export` |
| Hot files | `drive admin top-files --period-days 7` | `get_drive_top_files` |
| Find & recover deleted files | `drive admin files` (incl. removed) → `drive admin restore plan/apply` | `get_drive_files`, `plan_drive_restore` / `apply_drive_restore_plan` |
| Versioning changes | `drive admin team-folders plan/apply` | `plan_drive_team_folder_change` |
| Database memory pinning | `drive config state/plan/apply` | `get_drive_config`, `plan_drive_config_change` |
| Keep Drive updated | `package available --updates`, `package update` | `get_package_available`, `plan_package_update` |
| File operations & DSM sharing links | `file …` (FileStation module) | `get_filestation_*`, `plan_filestation_change` |

Notes that save a support ticket:

- Drive answers several writes with an empty success even when it silently
  skips the change; dsmctl always re-reads and reports "not confirmed"
  instead of lying. If that happens, re-check with the matching read.
- Disabling a team folder deletes Drive's database and stored versions for
  it (files stay). Revoking a user's Drive privilege deletes their Drive
  database (home files stay). Both are deliberately high risk.
- The activation state (`drive admin activation`) is informational: an
  unactivated Drive still serves clients.
