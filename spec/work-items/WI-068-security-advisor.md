---
id: WI-068
title: Security Advisor module (scan status, findings, schedule/baseline)
status: proposed
priority: P2
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/securityadvisor
  - internal/synology/operations/securityadvisor
  - internal/synology/securityadvisor.go
  - internal/runtime/manager.go
  - internal/application/securityadvisor.go
  - internal/cli/securityadvisor.go
  - internal/mcpserver/server.go
  - docs/control-panel.md
---

# WI-068 — Security Advisor module (scan status, findings, schedule/baseline)

## Outcome

A CLI user or MCP agent can read the Security Advisor surface — last-scan
status and progress, the list of findings with their severity, and the current
scan schedule and security baseline (home vs business/custom) — trigger a scan
on demand, and, through the hash-bound plan/apply contract, change the schedule
and baseline under guardrails. This is a focused module in the sense of
[WI-006](WI-006-control-panel-modules.md): one typed module for the Security
Advisor setting area, never a generic `set key=value` proxy over
`SYNO.Core.SecurityScan.*`.

Reads dominate this surface. The only configuration write is the schedule +
baseline; running a scan is a safe but load-heavy action that changes no
configuration and is exposed as an explicit, non-implicit command.

The API family, methods, versions, and field names below are the author's best
current knowledge (Security Advisor is served by the built-in
`SYNO.Core.SecurityScan.*` family: `Conf`, `Status`, `Result`, and possibly a
`Category`/`Info` companion). **All API specifics here are to be live-verified
at implementation time**: per the standing policy, source-doc and mobile/desktop
client field names for DSM WebAPI are frequently stale, so confirm every method
and field against the lab (DSM 7.3) with a throwaway `DSMCTL_DUMP` probe before
trusting them (see [[dsm-webapi-live-verify-fields]]). There is no committed
Security Advisor API map in memory yet; this WI establishes one.

## Scope

Sliced read-first, then a load-heavy action, then a guarded config write, so the
read slice ships independently.

### Slice A — read-only (to be live-verified)

- **Capabilities:** `securityadvisor.read` reports the selected backend, API,
  and version, and fails closed (`(not supported)`) when the
  `SYNO.Core.SecurityScan.*` family is absent from `SYNO.API.Info.query`
  (older DSM, or a build without Security Advisor).
- **Scan status:** likely `SYNO.Core.SecurityScan.Status` `get`/`query` →
  normalized `{running, progress, last_scan_time, current_item, overall_result}`.
  Volatile per-poll fields (e.g. a live progress counter) are surfaced only on
  the status read, not baked into the config state model.
- **Findings / results:** likely `SYNO.Core.SecurityScan.Result` `list` →
  per-check items normalized to `{check_id, category, title, severity, result,
  fail_count, detail}`. The severity taxonomy (expected values along the lines
  of `safe` / `info` / `warning` / `danger` / `out-of-date`) is
  **to be live-verified**; the decoder normalizes to a stable domain enum and
  returns an error on an unrecognized value rather than silently coercing it.
- **Schedule + baseline (read):** likely `SYNO.Core.SecurityScan.Conf`
  `get`/`load` → `{schedule_enabled, schedule_day, schedule_time, baseline,
  enabled_categories}`, where `baseline` distinguishes the home/personal profile
  from the business/high-security profile (and a possible `custom` mode with an
  explicit category set). Field names (`type` vs `baseline` vs `security_level`,
  the category-group keys) are **to be live-verified**.

### Action — run scan (safe, load-heavy, not a config write)

- **Trigger a scan:** likely `SYNO.Core.SecurityScan.Status` `start`/`rescan`
  (or `SecurityScan.Conf` `start_scan`) and its cancel counterpart. Exact
  method is **to be live-verified**. This changes no persisted configuration and
  is therefore not routed through the hash-bound plan/apply state contract;
  it is exposed as its own explicit `run` command / `run_security_scan` tool.
  It is **never** invoked implicitly by a status or result read, because a full
  scan is CPU/IO-heavy on the NAS. Classified low risk (no posture change), but
  gated behind explicit invocation and reported as an in-flight async action the
  status read then tracks to completion.

### Slice B — guarded write (plan/apply, hash-bound)

- **Schedule + baseline set** — `SYNO.Core.SecurityScan.Conf` `set` for
  `{schedule_enabled, schedule_day, schedule_time, baseline, enabled_categories}`
  only. Plan records and hashes the complete current `Conf` state; apply rejects
  a changed state, merges the patch into a freshly read `Conf`, writes the typed
  operation, and **re-reads** to verify the requested fields took effect (DSM
  silently ignores some fields — the recurring lesson, and specifically likely
  here for `enabled_categories` under a non-`custom` baseline). Set-field
  symmetry with the read shape, and whether baseline and category selection are
  independent or coupled, are **to be live-verified** with one authorized,
  fully-reverted probe before this slice ships.

## Non-goals

- **Acting on individual findings / auto-remediation.** Security Advisor's
  "fix"/"ignore"/"suppress" actions on a specific finding (e.g. dismissing a
  weak-password or open-port warning) are per-finding remediation with real
  posture side effects; each such action belongs in its own scoped work item,
  not this settings module.
- **The underlying settings a finding flags.** Security Advisor only *audits*;
  it does not own firewall rules, account password policy, auto-block, 2FA
  enforcement, certificate expiry, or DSM-update state. Changing those is the
  job of their respective modules (e.g. External Access / firewall / account
  work items), not this one. This module reports findings and manages the audit
  itself.
- **Notification / report delivery config** for scan results (email/push report
  wiring) — deferred; that overlaps the system notification module.
- **A generic `securityscan set key=value`** command or any raw
  `SYNO.Core.SecurityScan.*` passthrough.

## Design constraints

- **Independent compatibility boundary.** `SYNO.Core.SecurityScan.*` is its own
  API family and its own failure boundary. A NAS that does not advertise it
  (or advertises `Status`/`Result` but not the expected `Conf` version) reports
  the affected area `(not supported)` and does not error the module or any other
  Control Panel module. Backend selection is per operation, keyed on advertised
  API/version, not a monolithic client.
- **Run-scan is not a state mutation; schedule/baseline is.** Keep the two
  strictly separated: `run` is an idempotent-ish action with no state fingerprint
  and no plan hash; the schedule/baseline change is the only thing that flows
  through the plan/apply state contract (fingerprint of the full observed `Conf`,
  stale-state rejection, patch merge, postcondition re-read).
- **Weakening the audit is HIGH risk.** Disabling the scheduled scan, switching
  from the business/high-security baseline to the home/personal baseline, or
  removing checks from `enabled_categories` reduces the strictness of the
  security audit and can hide genuine posture problems from an operator who
  believes the NAS is being watched. Per policy, changes that weaken security
  posture are HIGH risk — classify baseline-loosening, schedule-disable, and
  category-removal HIGH, with the plan summary naming exactly which checks are
  being dropped. Tightening (enabling the schedule, business baseline, adding
  categories) is medium. There is no silent posture downgrade.
- **No secrets, no session identity in output.** This surface has no password /
  key / token write fields, so `credential_ref` is not needed for its writes.
  The standing invariant still holds: SIDs, SynoTokens, and any session identity
  never enter the display model, plan, hash, logs, or MCP tool arguments.
  Finding text is passed through as descriptive audit output only — the module
  must not resolve, echo, or reconstruct any actual credential a finding
  references (e.g. a "weak admin password" finding surfaces the finding, never a
  password value), and must not treat finding `detail` strings as a place to
  leak the caller's session token.
- **Patch-only ownership, stated explicitly.** The schedule/baseline write is
  patch-only over `Conf`; unspecified `Conf` fields are read fresh and preserved,
  never reset. Ownership semantics are declared in the plan.
- **JSON param quoting.** If `SYNO.Core.SecurityScan.Conf` `set` is a
  JSON-request API, send typed JSON literals for `schedule_time` /
  `enabled_categories` rather than bare form values, since JSON-request APIs
  silently drop bare/empty string values (see
  [[dsm-webapi-string-param-quoting]]); the postcondition re-read is the backstop.

## Acceptance criteria

- [ ] Slice A: `security-advisor capabilities|status|findings|schedule` (CLI) and
      the matching `get_security_advisor_*` MCP tools return normalized state;
      severity is decoded to a stable enum and an unknown severity value errors
      rather than coercing; no SID/SynoToken appears in any output.
- [ ] Capability gating: the module selects its backend by advertised
      API/version and reports `(not supported)` fail-closed when
      `SYNO.Core.SecurityScan.*` (or a required sub-API/version) is absent,
      without disabling other Control Panel modules.
- [ ] Slice A live verification on the DSM 7.3 lab: read scan status, the full
      findings list with severities, and the current schedule + baseline, with a
      `--json` grep confirming no session-token leak.
- [ ] Run-scan action: `security-advisor run` (and `run_security_scan` tool)
      triggers a scan via the live-verified method, is never invoked implicitly
      by a read, is classified low risk, and the status read tracks the running
      scan to completion; a live run on the lab completes and updates results.
- [ ] Slice B: schedule + baseline change via guarded hash-bound plan/apply with
      a request-capture test and a postcondition re-read that proves the fields
      took effect; baseline-loosening / schedule-disable / category-removal are
      classified HIGH and the plan summary enumerates dropped checks; the
      read-only gateway excludes the plan/apply and run tools.
- [ ] Slice B live verification on the DSM 7.3 lab (authorized, fully reverted):
      a baseline or schedule round-trip through plan/apply with postcondition
      proof, and a documented record of any live-verify correction where the
      source/client field names were stale.

## Verification

- Decoder + request-capture unit tests; `go test ./... -count=1`, `go vet ./...`.
- Live reads allowed on the explicitly configured lab NAS (DSM 7.3). The
  run-scan action and the Slice-B write require explicit per-session
  authorization; the schedule/baseline write must be fully reverted after the
  round-trip. Record the throwaway `DSMCTL_DUMP` probe results (method names,
  `Conf` field names, severity value set) and open a
  `[[dsm-security-advisor-webapi-map]]` memory note as the source of truth once
  live-verified.
- Source of truth to reconcile against live results: the DSM WebAPI conf and
  handlers for `SYNO.Core.SecurityScan.*` on codesearch (and the Security
  Advisor admin-center JS declaring the `Conf` version and `set`/`start`
  methods) — treated as leads, not authority, until the lab confirms them.

## Coordination

- Shares `internal/domain/controlpanel` conventions and the
  `internal/synology/controlpanel.go` facade pattern with the other Control
  Panel modules (parallel group C, `depends_on: [WI-006]`). New operation
  package under `internal/synology/operations/securityadvisor`; no overlap with
  the External Access ([WI-041](WI-041-external-access.md)), time, or
  file-services modules beyond the shared facade and the read-only-gateway
  exclusion list, which must be extended to cover this module's `run`,
  `plan_*`, and `apply_*` tools.
- Findings in this module will reference conditions owned by other modules
  (external exposure, accounts, updates); this WI only reports them and must not
  reach into those modules to remediate.

## Handoff

Fill this only when pausing incomplete work.
