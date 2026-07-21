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
- **`applications`** reads `SYNO.Backup.App2.Backup` v2 (`list`): the packages
  Hyper Backup can include in a backup task, each with its identifier (what a
  create request's `applications` list accepts), whether it is currently
  backupable (or the reason it is not), and whether it is backed up online.
- **`luns`** reads `SYNO.Backup.Lunbackup` v1 (`enum_lun`): the LUNs the legacy
  LUN-backup engine can protect (file/regular LUNs; block-level LUNs use a
  separate engine dsmctl does not implement). DSM omits already-backed-up LUNs.
- **`lun-backups`** lists the legacy LUN backup tasks (`loclunbkp` local /
  `netlunbkp` remote) with activity and last result — a separate task space
  from the image tasks (their `task_id` is the name string, not an integer).
- **`vault`** reads `SYNO.Backup.Service.VersionBackup.Config` (`get`) and
  `SYNO.Backup.Service.VersionBackup.Target` (`list`): the parallel inbound
  session limit and each inbound target stored on this NAS — id, share and
  path, activity, encryption, used size, and the last inbound backup's start
  time and duration (live-verified against a real NAS-to-NAS `image_remote`
  backup).

MCP exposes the same reads through `get_hyper_backup_capabilities`,
`get_hyper_backup_tasks`, `get_hyper_backup_task`, `get_hyper_backup_versions`,
`get_hyper_backup_logs`, `get_hyper_backup_applications`,
`get_hyper_backup_luns`, `get_hyper_backup_lun_backups`, and
`get_hyper_backup_vault`.

## Guarded LUN backup create

`dsmctl backup lun-backups plan` / `apply` (MCP:
`plan_hyper_backup_lun_backup_create` / `apply_hyper_backup_lun_backup_plan`)
create a local LUN backup (`loclunbkp`): back up one file/regular LUN (from the
`luns` read) to a shared folder on this NAS. The plan binds to the source LUN
and the set of existing LUN backup task names, so an apply fails if the LUN
disappeared or the name collides. `backup_now:true` runs the first backup
immediately (this is `apply_lun`'s own flag — the standalone `bkpnow` method is
a no-op on 4.2.2).

```console
echo '{"action":"create","create":{"task_name":"nightly-lun","lun_source":"data-lun",
  "destination_share":"backups","backup_now":true}}' \
  | dsmctl backup lun-backups plan --nas office -o lun.plan.json
dsmctl backup lun-backups apply --nas office -f lun.plan.json --approve <hash>
```

The apply resolves the LUN size from the observed LUN (so the caller need not
supply it), proposes the destination directory via `get_local_dest_dir` unless
`directory` overrides it, creates the task via `apply_lun`, and verifies it
exists (and, with `backup_now`, that the first backup started). Remote LUN
backup (`netlunbkp`) is deferred. Both tools are stripped from the read-only
remote gateway.

## Guarded run/cancel/create

`dsmctl backup tasks plan` / `apply` (MCP: `plan_hyper_backup_task_change` /
`apply_hyper_backup_task_plan`) run one task's backup now (`SYNO.Backup.Task`
`backup`), cancel its running backup (`cancel`, which also sends the task's
live `task_state`), or **create a new folder backup task**. The plan binds to
the observed task state — for run/cancel the target task's id, state,
activity, and last result; for create the full set of existing task names —
so an apply fails when anything changed in between. Both tools are stripped
from the read-only remote gateway.

A create backs up shared-folder paths and/or applications (identifiers from
the `applications` read; at least one source of either kind) on the source NAS
to exactly one destination:

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
