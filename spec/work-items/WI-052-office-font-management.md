---
id: WI-052
title: Synology Office font management write
status: done
owner: ""
priority: P2
depends_on: [WI-051]
parallel_group: C
touches:
  - internal/domain/office
  - internal/synology/operations/office
  - internal/synology/office.go
  - internal/application/office.go
  - internal/cli/office.go
  - internal/mcpserver/server.go
  - docs/office.md
---

# WI-052 — Synology Office font management write

## Outcome

A CLI user or MCP agent can manage the Synology Office custom font name
registry — add, enable, disable, and delete name-registered fonts — through
the existing Office plan/apply pair, and the font read distinguishes custom
from system fonts and shows the enabled state.

## Scope

- Extend the font read: each entry carries `custom` (DSM `system: false`) and
  `enabled` (absence of DSM `disable: true`) in addition to name and display
  name.
- One new `fonts` scope in the Office change intent (exactly one scope per
  change, now system | preferences | fonts): `{action, names}` with action
  `add`, `enable`, `disable`, or `delete`, executed through
  `SYNO.Office.Setting.Font` v1 with a `fonts` JSON string-array parameter.
- Plan-time validation against the observed font list: managing a **system**
  font is rejected up front because DSM silently skips system fonts (verified
  live) and the failure would otherwise surface only as a late postcondition
  error; enable/disable/delete also require the target to exist.
- Postcondition re-read verifies each name's terminal state (present-custom,
  enabled, disabled, or absent).

## Non-goals

- Binary font-file (TTF) upload: the Setting WebAPI definitions expose no
  upload-enabled method, so the upload path rides a different API and stays
  deferred. This item manages name-registered fonts only.
- Per-document font embedding or substitution behavior.

## Design constraints

- Live-verified on Office 3.7.2-22592 (lab, DSM 7.3, fully reverted):
  - `add`/`enable`/`disable`/`delete` accept `fonts=["name", ...]` (an array
    of objects is rejected with error 120 `fonts: type`) and every mutation
    returns the updated list.
  - A custom entry lists as `{"system": false}`, a disabled custom entry as
    `{"disable": true, "system": false}`; system entries stay `{}` or
    `{"display": ...}`.
  - `disable`/`delete` on a system font (e.g. Arial) and on a nonexistent
    name are silent no-ops — the response is a success carrying an unchanged
    list.
- Font mutations are reversible (the registry is names, not files), so plans
  are medium risk with no warnings.

## Acceptance criteria

- [x] Font decode carries custom/enabled; request-capture tests prove the
      `fonts` string-array encoding and per-action method for all four
      actions.
- [x] Plan rejects: empty/duplicate names, system-font targets for every
      action, enable/disable/delete of absent names, and full no-op patches.
- [x] Apply verifies the per-action terminal state after a re-read and fails
      on DSM silent skips.
- [x] CLI `office fonts` shows CUSTOM/ENABLED; `office plan|apply` and the
      MCP `plan_office_change`/`apply_office_plan` accept the fonts scope
      (no new MCP tools).
- [x] DSM 7.3 live verification (lab, authorized, fully reverted): add →
      disable → enable → delete of a disposable `dsmctl-e2e-font` entry
      through plan/apply, font list identical before and after.

## Verification

- `go test ./... -count=1`, `go vet ./...`.
- Live plan/apply round-trip on the DSM 7.3 lab NAS (Office 3.7.2-22592):
  add -> disable -> enable -> delete of `dsmctl-e2e-font`, each applied and
  postcondition-verified; font list identical before and after (46 fonts).

## Coordination

Extends the WI-051 module in place; parallel group C. Shares
`internal/mcpserver/server.go` with any concurrent module work.
