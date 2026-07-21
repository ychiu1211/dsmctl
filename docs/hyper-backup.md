# Hyper Backup

The Hyper Backup module reads the backup posture of a NAS — tasks, versions,
the log feed, and the Hyper Backup Vault view — and runs or cancels a backup
task through the guarded plan/apply contract. It is package-version gated like
the Download Station, Photos, and Surveillance modules: the task, version, and
log reads require the installed `HyperBackup` package (4.x baseline,
live-verified on 4.2.2), and the vault view requires `HyperBackupVault`. A NAS
with only one of the two reports the other side unsupported instead of
erroring. All `SYNO.Backup.*` APIs are `entry.cgi` JSON-request APIs, so every
parameter is sent as a JSON literal.

```console
dsmctl backup capabilities --nas office
dsmctl backup tasks --nas office
dsmctl backup task 1 --nas office
dsmctl backup versions 1 --nas office
dsmctl backup logs --nas office --limit 20
dsmctl backup vault --nas office
echo '{"action":"backup","task_id":1}' | dsmctl backup tasks plan --nas office
dsmctl backup tasks apply --nas office -f plan.json --approve <hash>
```

- **`capabilities`** reports both packages' evidence (installed, version,
  running) and which reads/actions are available, each selected independently.
- **`tasks`** reads `SYNO.Backup.Task` (`list` with the last/next run, result,
  schedule, and source additionals): each task's id, name, type
  (`image:image_local`, `share:local`, cloud types, …), lifecycle state, live
  activity, last backup time/result, next scheduled run, and the backed-up
  folders and applications.
- **`task <id>`** composes `SYNO.Backup.Task` (`get` + `status`) and
  `SYNO.Backup.Target` (`get`): the destination repository, transfer options
  (compression, client-side encryption, notifications), live status with
  progress while a run is active (Hyper Backup reports several progress
  counters as quoted strings; they are normalized to integers), and whether
  the destination is currently reachable.
- **`versions <id>`** reads `SYNO.Backup.Version` v2 (`list`): each version's
  id, start/completion time, integrity status, and rotation-lock flag.
- **`logs`** reads `SYNO.SDS.Backup.Client.Common.Log` (`list`): the Hyper
  Backup event feed with per-level totals.
- **`vault`** reads `SYNO.Backup.Service.VersionBackup.Config` (`get`) and
  `SYNO.Backup.Service.VersionBackup.Target` (`list`): the parallel inbound
  session limit and each inbound target stored on this NAS — id, share and
  path, activity, encryption, used size, and the last inbound backup's start
  time and duration (live-verified against a real NAS-to-NAS `image_remote`
  backup).

MCP exposes the same reads through `get_hyper_backup_capabilities`,
`get_hyper_backup_tasks`, `get_hyper_backup_task`, `get_hyper_backup_versions`,
`get_hyper_backup_logs`, and `get_hyper_backup_vault`.

## Guarded run/cancel/create

`dsmctl backup tasks plan` / `apply` (MCP: `plan_hyper_backup_task_change` /
`apply_hyper_backup_task_plan`) run one task's backup now (`SYNO.Backup.Task`
`backup`), cancel its running backup (`cancel`, which also sends the task's
live `task_state`), or **create a new folder backup task**. The plan binds to
the observed task state — for run/cancel the target task's id, state,
activity, and last result; for create the full set of existing task names —
so an apply fails when anything changed in between. Both tools are stripped
from the read-only remote gateway.

A create backs up a list of shared-folder paths on the source NAS to exactly
one destination:

- `local_share` — a shared folder on the source NAS itself (`image_local`);
- `target_nas` — **another NAS known to dsmctl**: the profile's address,
  account, and stored credential are resolved from the OS credential store at
  apply time and never enter the plan (`image_remote`, Hyper Backup Vault on
  the destination);
- `host` + `account` + `password_ref` — an explicit remote Synology NAS with
  the credential supplied as a reference resolved at apply.

```console
echo '{"action":"create","create":{"task_name":"nightly-homes","source_folders":["/homes"],
  "target_nas":"nas51","destination_share":"hb_vault"}}' \
  | dsmctl backup tasks plan --nas nas255 -o create.plan.json
dsmctl backup tasks apply --nas nas255 -f create.plan.json --approve <hash>
```

The apply probes the destination first (`SYNO.Backup.Target`
`get_candidate_dir` — authentication and share check, and the proposed
directory name unless `directory` overrides it), registers the repository
(`SYNO.Backup.Repository` `create`), creates the task (`SYNO.Backup.Task`
`create` — a response that arrives empty still succeeds; the postcondition
re-read recovers the new task id), and verifies the task exists. Remote
transfers use the Vault port (default 6281) with transfer encryption on by
default; the destination certificate is not verified. The created task has no
schedule — it runs when triggered. The destination credential is stored by
DSM inside the task configuration on the source NAS (that is how Hyper Backup
works); the plan warns about it.

Task edit/delete, restores, schedules, suspend/resume, integrity checks,
rotation writes, statistics, client-side encryption, and Vault writes are
deferred — see [WI-087](../spec/work-items/WI-087-hyper-backup.md).
