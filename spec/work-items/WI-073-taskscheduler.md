---
id: WI-073
title: Task Scheduler module (scheduled + triggered tasks)
status: proposed
priority: P2
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/taskscheduler
  - internal/synology/operations/taskscheduler
  - internal/synology/taskscheduler.go
  - internal/runtime/manager.go
  - internal/application/taskscheduler.go
  - internal/cli/taskscheduler.go
  - internal/mcpserver/server.go
  - docs/task-scheduler.md
---

# WI-073 — Task Scheduler module (scheduled + triggered tasks)

## Outcome

A CLI user or MCP agent can read Control Panel → Task Scheduler — the full
inventory of scheduled and triggered tasks (user-defined scripts,
service-control tasks, and retention tasks), each task's run-as owner, schedule,
enabled state, next/last run time, last-run result, and, on explicit request, the
command/script body — and, through the hash-bound plan/apply contract,
enable/disable, retime, run-now, create/edit, and delete tasks under strict
high-risk guardrails. This is a focused Control Panel module in the sense of
[WI-006](WI-006-control-panel-modules.md): one typed module for the Task
Scheduler surface, never a generic `set key=value` or raw `create` proxy.

The defining risk of this module: **a scheduled user-defined task runs an
arbitrary command as its run-as user, which is normally root.** Creating or
editing a script task is remote code execution on a schedule; running one now is
remote code execution now; enabling a previously disabled task arms whatever
command it already carries. Reading and listing are side-effect-free and safe;
every write is not.

The API map, families, versions, methods, and field names below are the author's
best knowledge from DSM Admin Center's TaskScheduler app JS and prior WebAPI
work. Every one of them is marked **to be live-verified at implementation time**:
per standing policy the source-doc / mobile-client field names are often stale,
so each API family, version, method, task-type enum, and payload field must be
confirmed against the lab with a throwaway `DSMCTL_DUMP` probe (and, for the
create/set payload, one authorized fully-reverted create/delete of a disposable
no-op task) before code depends on it. See
[[dsm-webapi-live-verify-fields]].

## Scope

Sliced read-first, then guarded write, so the read slice ships independently.

### Slice A — read-only

- **Scheduled task inventory** — likely `SYNO.Core.TaskScheduler` `list` →
  per task: `id`, `name`, `type` (script / service-control / retention / built-in
  maintenance), `owner` / `real_owner` (the run-as identity), `enable`, the
  schedule summary, `next_trigger_time`, and current run status.
- **Task detail** — `SYNO.Core.TaskScheduler` `get` (by `id`) → the full schedule
  object (frequency, days/date, hour/minute, repeat), notification settings, and
  the **command / script body** for user-defined script tasks. Reading the body
  is inspection, not mutation; it is exposed only through the explicit `get`
  path, not in list output.
- **Triggered tasks** — boot-up, shutdown, and event-triggered tasks live in a
  separate API family (likely `SYNO.Core.EventScheduler`, to be confirmed);
  surfaced as its own independently-gated area with the same list/get shape.
- **Last-run result / output** — the exit status and captured output of a task's
  most recent run (likely a `get_output` / result method), read-only.
- **Capabilities** — advertise the supported task types and, per operation, the
  selected backend API/version/method.

### Slice B — guarded write (plan/apply, hash-bound) — all HIGH risk

- **Enable / disable an existing task** — `SYNO.Core.TaskScheduler` `set` toggling
  `enable`. Enabling arms an existing command; disabling silences a running
  schedule.
- **Retime (schedule edit) of an existing task** — `set` with a new schedule
  object; the command/body is left untouched.
- **Run now** — `SYNO.Core.TaskScheduler` `run` (by `id`); executes the task's
  command immediately, then reads back the last-run result so apply reports the
  exit status rather than firing and forgetting.
- **Create / edit a user-defined script task** — `create` / `set` with `name`,
  `owner`/`real_owner`, `enable`, the schedule object, and the `extra`/`script`
  body plus notification fields. This is the highest-risk operation in the
  module (scheduled RCE) and requires explicit approval.
- **Delete a task** — `delete` (by `id`).

## Non-goals

- **A generic "create any task type from raw fields" proxy.** The module exposes
  typed intents (enable/disable, retime, run, script create/edit, delete); it
  does not accept an arbitrary task-type payload.
- **Create/edit of the built-in system-maintenance task types** — DSM Update
  install, Btrfs / RAID data scrubbing, S.M.A.R.T. tests, recycle-bin retention
  (owned by the share module), and backup tasks. The module **reads** these task
  types in the inventory, but writing or creating them belongs to their owning
  modules (or is deferred); coordinate before adding writes for them.
- **Hyper Backup / Snapshot Replication task creation** and package-specific
  triggered tasks beyond the generic boot/shutdown/event set.
- **Privilege escalation as a feature.** The run-as (`real_owner`) identity is
  read and is part of the create/edit approval surface, but changing an existing
  task's owner is not offered as a convenience toggle.

## Design constraints

- **Focused module, never a raw proxy** ([WI-006](WI-006-control-panel-modules.md)).
- **Two independent compatibility boundaries.** Scheduled tasks
  (`SYNO.Core.TaskScheduler`) and triggered tasks (`SYNO.Core.EventScheduler`,
  to be confirmed) are separate API families and separate failure boundaries: a
  NAS or DSM version missing one must still list the other, reported
  `(not supported)` rather than erroring the whole module. Selection is per
  operation; a missing family fails closed for that area only.
- **Every write is HIGH risk — no exceptions, no "medium".** A script task runs
  arbitrary commands as root on a schedule, so enabling a task, retiming it,
  running it now, creating/editing its body, or deleting it are all RCE-class or
  destructive. Classify all Slice-B mutations high, require explicit approval,
  and exclude the plan/apply **and `run`** operations from the read-only gateway.
- **The script body IS the approval surface.** Unlike a secret, the command /
  script body of a create or edit MUST appear **verbatim in the plan** so the
  human approves exactly what will execute, and the approval hash binds it. It is
  high-sensitivity content — kept out of general and debug logs, present only in
  the plan artifact the operator reviews — but it is intentionally **not**
  redacted from the plan.
- **Secrets never enter requests/plans/hashes/logs.** Any password or token a
  task type carries (notification account, remote mount, DSM account) uses the
  existing `credential_ref: env:NAME` mechanism, resolved at apply time and
  absent from the request, plan, hash, result, and logs. dsmctl never embeds
  credentials into a script body; scripts must read their own secrets from the
  environment or files at runtime.
- **Patch + postcondition.** plan records and hashes the complete observed task
  (every field DSM returns), apply rejects a changed task, merges the patch into
  a freshly read task, and re-reads to verify `enable` / schedule / body actually
  took effect — DSM silently ignores some fields, the recurring lesson.
- **Stable identity; create binds absence.** Existing-task writes are keyed by
  the DSM task `id` plus the observed-state fingerprint. `create` yields a
  server-assigned `id`, so the create plan binds the canonical intent plus the
  absence of a colliding task name, and its postcondition confirms the new task
  exists with the requested body and schedule.
- **Run-as protection.** A task whose `real_owner` is root or the current admin
  is flagged, and the create/edit plan summary states the run-as identity
  explicitly; the module never silently changes a task's owner.
- **run-now is bounded.** After `run`, poll the last-run result/output so apply
  reports the exit status instead of fire-and-forget.

## Acceptance criteria

- [ ] Slice A: `task-scheduler capabilities|list|get|triggered` (CLI) and the
      matching `get_task_scheduler_*` MCP tools return normalized inventory and
      detail — id, name, type, run-as owner, enabled, schedule, next/last run,
      last result — with the script body exposed only via the explicit `get`
      path, never in `list`.
- [ ] Independent gating: scheduled and triggered families each select their own
      backend; a missing family is reported `(not supported)` without disabling
      the other, and absence fails closed for that area.
- [ ] Capabilities report the supported task types and, per operation, the stable
      operation name, selected backend, API, and version.
- [ ] Slice A live-verified on the DSM 7.x lab via a throwaway read probe: list
      existing tasks, `get` one user-defined script task including its body, read
      a triggered (boot/shutdown) task, and read a last-run result.
- [ ] API families, versions, methods, and payload fields confirmed live before
      code depends on them — both `SYNO.Core.TaskScheduler` and the triggered
      family — and the `create`/`set` payload shape (schedule object,
      `extra`/`script`, `owner`/`real_owner`, `type` enum) verified with one
      authorized, fully-reverted create/delete of a disposable no-op task.
- [ ] Slice B (first write): enable/disable an existing task via hash-bound
      plan/apply, with request-capture test and postcondition re-read; classified
      HIGH risk; read-only gateway excludes the plan/apply and `run` tools.
- [ ] Slice B run-now: `run` executes the target task and apply reads back the
      last-run result / exit status; classified HIGH risk; live-verified reverted
      against a disposable no-op task (e.g. a task running `/bin/true`).
- [ ] Slice B schedule edit and create/edit of a script task: the script body
      appears verbatim in the plan and is hash-bound; any task-type password uses
      `credential_ref`; live-verified reverted (create a disposable no-op task →
      run → delete). Unit tests assert no password parameter is ever emitted and
      that the script body is present in the plan artifact but absent from debug
      logs.
- [ ] Delete a task via plan/apply, classified HIGH risk, with a postcondition
      that confirms the task is gone.

## Verification

- Decoder + request-capture unit tests; `go test ./... -count=1`, `go vet ./...`.
- Live reads are allowed on the configured lab. Every Slice-B live write requires
  explicit per-session authorization and operates only on a **disposable no-op
  task** the session creates and fully deletes (a pre-existing task's body is
  never touched); the run-now check uses a command with no side effects.
- Source of truth to confirm: DSM Admin Center TaskScheduler app JS (task-type
  enum, schedule object, create/set payload) and the codesearch WebAPI handler
  for `SYNO.Core.TaskScheduler` / `SYNO.Core.EventScheduler`; live-verify per the
  standing policy before trusting any field.

## Coordination

- Control Panel module (parallel group C), depends on the WI-006 module pattern
  and shares `internal/synology/controlpanel.go` and the module registry. New
  packages under `internal/domain/taskscheduler` and
  `internal/synology/operations/taskscheduler`.
- Reads every task type, but defers create/edit of the built-in maintenance task
  types (DSM update, storage scrubbing, S.M.A.R.T., recycle-bin retention,
  backup) to their owning modules; coordinate with the share module (recycle-bin
  retention) and any future storage-maintenance or backup work item before adding
  writes for those types.

## Handoff

Fill this only when pausing incomplete work.
