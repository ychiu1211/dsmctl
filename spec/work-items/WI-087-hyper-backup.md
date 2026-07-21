# WI-087 — Hyper Backup: task/version/log reads + guarded run/cancel

- Priority: P2
- Status: `in_progress`
- Surface: C (CLI + MCP)
- Depends on: WI-019 (package inventory), WI-022 (package-scoped operations)

## Outcome

An operator (or an LLM through MCP) can see the Hyper Backup posture of a NAS —
every backup task with its state, last/next run and result, the versions a task
has produced, the Hyper Backup log feed, and the Hyper Backup Vault service view
— and can run or cancel a backup task through the guarded plan/apply contract.
Data protection was previously "recorded for later" in the gap inventory; this
work item pulls the Hyper Backup client (and the read side of Vault) forward
because operating backups is the single most requested data-protection action.

## Scope

Read (package-gated on `HyperBackup` >= 4.0, fail-closed):

- **Tasks** — `SYNO.Backup.Task` v1 `list` with
  `additional=["last_bkp_time","next_bkp_time","last_bkp_result","is_modified","schedule"]`:
  per-task id, name, state/status, type (`image:image_local`, …), repo linkage,
  last/next backup times and result.
- **Task detail** — `SYNO.Backup.Task` v1 `get`
  (`additional=["backup_params","rotate_params","schedule","repository"]`) +
  `status` (+ last/next/result additionals) + `SYNO.Backup.Target` v1 `get`
  (`additional=["is_online"]`): repository binding, backup params, live
  status/progress, and target reachability in one view.
- **Versions** — `SYNO.Backup.Version` **v2** `list` (v1 has no `list`;
  live-verified error 103) with `task_id`/`offset`/`limit`.
- **Logs** — `SYNO.SDS.Backup.Client.Common.Log` v1 `list` with
  `offset`/`limit`; level filtering is applied client-side (the wire
  `filter_level` parameter is not shipped unverified).
- **Vault** — package-gated on `HyperBackupVault` >= 4.0:
  `SYNO.Backup.Service.VersionBackup.Config` v1 `get`
  (`parallel_backup_limit`) + `SYNO.Backup.Service.VersionBackup.Target` v1
  `list` (inbound vault targets: `target_id`, `share`, `target_name`,
  `target_path`, `status`, `is_enc`, `is_resumable`, `used_size`,
  `computing_size`, `last_backup_start_time`, `last_backup_duration` — all
  live-verified against a real inbound `image_remote` backup).
- **Capabilities** — one bool per area plus both packages' evidence.

Guarded write (plan/apply, hash-bound, same contract as Download Station task
changes):

- **Run backup now** — `SYNO.Backup.Task` v1 `backup` (`task_id`).
- **Cancel a running backup** — `SYNO.Backup.Task` v1 `cancel`
  (`task_id`, `task_state`).
- Plan binds to the observed task (id, name, state, status, last result, last
  backup time). Backup requires a currently idle task; cancel requires a
  currently running one; apply re-plans and fails on stale state; postcondition
  re-reads status (run: task is running or has a fresh last-backup time;
  cancel: task is no longer actively backing up).

## Non-goals (deferred)

- Task create/edit/delete, relink/reauth, suspend/resume, discard, integrity
  check (error_detect), rotation writes, restore of any kind, statistics
  (`SYNO.SDS.Backup.Client.Common.Statistic`), Vault writes
  (`VersionBackup.Config` set), and the rsync NetworkBackup service toggle
  (`SYNO.Backup.Service.NetworkBackup` — its enable state belongs to the
  WI-028 rsync service module if ever needed). Task creation over the wire is
  understood (Repository `create` → Task `create`) and recorded in the memory
  map, but creating backup topology is a design-heavy write that needs its own
  slice.
- Entire-DSM backup tasks and LUN backup tasks (`SYNO.Backup.Lunbackup`,
  `MultiVerLun`) — the task list read reports them as tasks if present, but no
  action targets them.

## Design constraints

- Both packages are separate gates: task/version/log operations fail closed
  without `HyperBackup`; the vault read fails closed without
  `HyperBackupVault`. A NAS with only one of the two reports the other side
  `(not supported)` in capabilities rather than erroring the module.
- All `SYNO.Backup.*` APIs are `entry.cgi` JSON-request APIs: string parameters
  are sent as JSON literals (quoted) per the string-param-quoting house rule.
- `Task.status` progress counters (`processed_size`, `total_size`,
  `counted_file_count`, …) arrive as **strings** while `progress`/`avg_speed`
  are numbers — decoders use the flexible int idiom everywhere.
- `last_bkp_error_code` 4401 is a sentinel ("no error"), not an error;
  timestamps are `YYYY/MM/DD HH:MM:SS` local strings and pass through
  untranslated.
- The run/cancel apply is excluded from the read-only gateway
  (`read_only.go`), like every other `plan_*`/`apply_*` pair.

## Acceptance criteria

- [ ] `dsmctl backup tasks|task|versions|logs|vault|capabilities` render tables
  and `--json`, on the CLI and as `get_hyper_backup_*` MCP tools.
- [ ] `plan_hyper_backup_task_change` + `apply_hyper_backup_task_plan` (MCP)
  and `dsmctl backup tasks plan|apply` (CLI) run/cancel a task end to end with
  hash approval, stale-plan rejection, and postcondition verification.
- [ ] A NAS without HyperBackup fails closed with the package named in the
  unsupported reason; unit tests cover missing package, below-baseline
  version, and malformed payload rejection.
- [ ] Live verification against the lab NAS (HyperBackup 4.2.2-4262,
  HyperBackupVault 4.2.2-4262) using the standing `dsmctl-probe-task`
  (task_id 1, unscheduled image_local task backing `/Share/dsmctl_probe_src`
  into `Share/dsmctl_probe_1`) for list/detail/versions/run/cancel.

## Verification

Wire shapes were live-verified on the lab (2026-07-21) with a throwaway
authenticated probe before implementation: task list/get/status (idle, running,
canceling, done, cancel results), backup/cancel actions, Version v2 list, log
list, Vault config/target list, Target get. The lab fixture task and its
repository (`repo_id` 1) were created through the same probe (Repository
`create` → Task `create`; the create response body can arrive empty on success,
so the postcondition re-read is the source of truth). Full field map recorded
in the auto-memory (`dsm-hyperbackup-webapi-map`).

NAS-to-NAS verification (2026-07-21, nas255 → nas51, both DS918+): HyperBackup
4.2.2 online-installed on nas255 and HyperBackupVault 4.2.2 local-.spk-installed
on offline nas51 through dsmctl's own package plans; destination share
`hb_vault` created through the share plan; an `image_remote` repository + task
(`dsmctl-remote-task`, source `/homes`, vault dir `DiskStation_1`, port 6281,
`sslcheck` on with `ssl_trust_mode:"ignore"`, plain-credential
`is_webapi_authen:false`) created by probe with the target credential resolved
in-process from the OS credential store (never printed); backup run to `done`
twice — once by probe, once through the shipped `plan`/`apply` — and the vault
side re-read after each (used size 729 → 961 bytes). The split-package gating
was verified for real: nas255 (client only) reports vault read unsupported,
nas51 (vault only) fails every client read closed. Remote-target detail
(`Target.get` → `is_online`, vault host name) and remote version list verified
through the CLI. Both machines keep their tasks/targets as standing fixtures.

## Coordination

- WI-084/085/086 are claimed by the provisioning program on other branches;
  this item takes WI-087.
- Branch: `claude/hyper-backup-mcp-cli-697260`.
