---
id: WI-011
title: Define guarded Control Panel time changes
status: done
priority: P2
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/controlpanel
  - internal/synology/operations/controlpaneltime
  - internal/synology/controlpanel.go
  - internal/application
  - internal/cli
  - internal/mcpserver/server.go
---

# WI-011 — Define guarded Control Panel time changes

## Outcome

Time zone, display format, and NTP configuration can be changed through a typed
hash-bound plan/apply contract without exposing raw `SYNO.Core.Region.NTP.set`.

## Scope

- Separate intent fields for time zone, date/time display format, NTP mode,
  and ordered NTP servers.
- Current-state fingerprint, explicit risk summary, approval hash, and
  postcondition verification.
- Version-scoped `set` variants with strict request-capture tests.
- Fail closed when a time zone or NTP server cannot be validated.

## Safety requirements

- Re-read configuration immediately before apply and reject stale plans.
- Do not set wall-clock time in this item; switching to manual mode requires a
  separate decision and safety review.
- Treat removal of the last NTP server and loss of synchronization as high
  risk; never infer a replacement server.
- Do not claim NTP reachability from syntax validation alone.
- Verify the normalized configuration after DSM accepts the change and return
  an actionable partial-failure result when synchronization does not converge.

## Verification

- Fixture and request-capture tests, `go test ./...`, and `go vet ./...`.
- No live time/NTP mutation without separate explicit authorization.

## Primary evidence and decisions

- WI-006 (2026-07-16, DSM 7.3.2 test NAS): `SYNO.API.Info.query` advertises
  `SYNO.Core.Region.NTP` v1-v3 with JSON request format, and Admin Center's
  `Region.NTPTab` declares version 3 with `get` and `set`. The set backend is
  therefore v3-only; v1/v2-only targets report `set: false` and planning
  fails closed.
- 2026-07-17 read-only verification on the same NAS: read and set operations
  both select `core-region-ntp-v3`, `control-panel time capabilities` reports
  `Set: yes`, and a live medium-risk plan was generated. No `set` call was
  made.
- The set request submits the complete merged configuration using the field
  names confirmed by the v3 `get` shape: `timezone`, `date_format`,
  `time_format`, `enable_ntp` (always `"ntp"`), and `server` (comma-joined
  ordered list). Recorded assumption: v3 `set` accepts exactly these `get`
  field names and does not require wall-clock parameters while
  `enable_ntp="ntp"`. The first separately authorized live apply is the
  confirming step; a wrong assumption surfaces as a DSM parameter error or a
  named postcondition failure, never as a silent partial write. Should `set`
  demand wall-clock fields, this contract forbids sending them and the
  mutation must remain fail-closed.
- `synchronization_mode` accepts only `ntp`. Switching to manual requires
  wall-clock ownership and stays excluded. Enabling NTP from manual mode is
  allowed, classified high risk, and requires at least one server in the same
  patch; while a NAS stays manual, every other time change is rejected.
- Time zones validate against the NAS's current value or the embedded IANA
  database (`time/tzdata`, including DSM's bare-city vocabulary such as
  `Taipei` -> `Asia/Taipei`). Display formats validate against DSM's token
  grammar (`Y`/`m`/`d` once each with one separator; `H:i` or `h:i A`). NTP
  servers are IP/RFC-1123 syntax only, at most 8, case-insensitive duplicates
  rejected, order preserved end to end.
- The plan carries no destructive flag: time changes cannot destroy data, so
  the medium/high risk label and warnings express the consequences. This
  mirrors the module-state fingerprint approach shipped by WI-012 rather than
  the stable-resource plan shape.

## Completion record

- Completed end to end on 2026-07-17. CLI adds `control-panel time plan` and
  `control-panel time apply`; MCP adds `plan_control_panel_time_change` and
  `apply_control_panel_time_plan` over the identical application contract,
  and the time capabilities report now includes `controlpanel.time.set`.
- Verified with `go test ./... -count=1`, `go vet ./...`, and read-only live
  checks against DSM 7.3.2: capabilities report read and set on
  `core-region-ntp-v3`, state returned the known configuration, and a plan
  was generated. No set method was called.
