---
id: WI-047
title: Redesign Admin UI around lists and guided workflows
status: done
priority: P1
owner: ""
depends_on: [WI-045, WI-046]
parallel_group: G
touches:
  - internal/gateway/admin/ui.go
  - internal/gateway/admin/handler_test.go
  - docs/gateway.md
  - docs/gateway-admin-guide.md
  - spec/roadmap.md
---

# WI-047 — Redesign Admin UI around lists and guided workflows

## Outcome

Every authenticated Admin UI page leads with state and the next useful action.
Creation, credential enrollment, manual fallback, filtering, and security forms
open only when requested instead of permanently displacing the resources they
manage.

## Scope

- Make the NAS page list-first, with a first-NAS empty state, an on-demand
  create/enroll wizard, honest stored-auth labels, state-aware primary actions,
  an overflow menu, and structured connection diagnostics.
- Make MCP Access connection-first, with a guided connection wizard, Full
  access as the default preset, explicit NAS checkboxes, a 365-day default,
  prefix-correct endpoint derivation, possession/Agent-consent warnings, and a
  one-time configuration handoff.
- Keep pending approvals and approval history list-first while moving manual
  approval creation into an on-demand fallback dialog.
- Keep Audit events list-first and reveal filters only when requested.
- Keep administrator/session state visible while moving password change into
  an on-demand dialog.
- Use consistent empty states, primary actions, overflow menus, dialogs,
  localization, responsive behavior, and accessible controls across pages.

## Non-goals

- No OAuth/OIDC implementation, new client provenance schema, or per-human
  identity model.
- No changes to DSM operations, MCP authorization, approval admission, audit
  retention, profile/token persistence, or API authority.
- No claim that a stored DSM session is currently valid. Online diagnosis is
  explicit and uses the existing staged test endpoint.
- No live DSM mutation.

## Design constraints

- Preserve the embedded/offline UI, CSP, stable API endpoints, secret
  non-disclosure, five locales, and existing security confirmations.
- `session_stored` and `password_stored` describe retained authentication
  material only; health and authentication method remain distinct concepts.
- Reauthentication remains available for account/method changes but is not a
  primary action while credentials are already stored.
- Diagnostics present DNS, TCP, TLS/HTTP, and DSM authentication stages rather
  than raw JSON.
- Token secrets are shown once, never placed in browser storage, and never
  repeated in Toast messages or audit output.

## Acceptance criteria

- [x] With zero NAS profiles, the NAS page shows one centered Add first NAS CTA;
      with profiles, the persistent form is gone and Add NAS moves to the page
      header.
- [x] NAS creation proceeds through connection details and DSM sign-in; an
      incomplete profile exposes Complete setup, while stored credentials move
      reauthentication, edit, diagnosis, and delete into deliberate actions.
- [x] Connection diagnosis renders named stages and remediation without raw
      JSON; stored credentials are not mislabeled as a currently live session.
- [x] MCP Access defaults to Full access over explicit NAS targets and 365 days,
      then reveals a one-time token, absolute endpoint, and copyable generic
      Streamable HTTP configuration.
- [x] Tokens/connections, pending approvals, approval history, Audit events,
      and administrator state remain the primary page content; secondary forms
      open only on request.
- [x] All five locales, desktop and narrow layouts, keyboard focus, empty states,
      and secret non-disclosure have regression coverage.
- [x] Focused tests, `go test ./...`, `go vet ./...`, browser walkthrough, and
      `git diff --check` pass without a live DSM mutation.

## Verification

- Run `go test ./internal/gateway/admin -count=1`.
- Run `go test ./...` and `go vet ./...`.
- Browser-walk all six authenticated views using the existing local managed
  state. Do not invoke profile diagnosis or DSM login against the configured
  NAS during verification; use rendered-state and unit fixtures for those paths.
- Check root and `/dsmctl/admin` endpoint derivation with rendered DOM fixtures.
- Run `git diff --check`.

Completed 2026-07-19:

- `go test ./internal/gateway/admin`
- `go test ./...`
- `go vet ./...`
- JavaScript syntax compilation with Node
- Browser walkthrough of Overview, NAS, MCP Access, Approvals, Audit, and
  Administrator at 1256×864, including all on-demand forms except secret-
  creating or DSM-contacting submissions
- DOM checks: no duplicate IDs, no unlabeled inputs/selects, no horizontal
  document overflow, and zero missing/extra localization keys in all five
  locales
- `git diff --check`

No DSM login, profile diagnostics, approval creation, token creation, password
change, or DSM mutation was invoked during browser verification.

Follow-up 2026-07-19: changed the Administrator view from the comparison-style
two-column `content-grid` to a semantic vertical `panel-stack`. Focused tests
passed, and browser measurement confirmed two equal-width cards with the
Security card above Current session and an 18 px gap.

Follow-up 2026-07-19: replaced the NAS table/overflow wrapper with a responsive
resource list. Browser verification confirmed zero NAS table wrappers, visible
list overflow, no document-level horizontal overflow, and a fully visible
menu-role action dropdown that closes on outside click. `go test ./...`,
`go vet ./...`, and `git diff --check` passed.

Follow-up 2026-07-19: restored TLS trust visibility throughout NAS setup. The
NAS list and pre-login review now show the active System CA or Pinned
fingerprint mode, and Connection settings exposes both choices before Web
Login. Browser verification confirmed the current value and both selectable
options without invoking DSM login or saving a profile change.

Follow-up 2026-07-19: flattened Complete setup so the existing DSM URL, TLS
mode, and optional pinned fingerprint are directly editable beside the sign-in
choices. Choosing a sign-in method now persists changed connection settings
first; unchanged settings proceed without a redundant write. Browser
verification confirmed the direct form and both TLS choices without saving or
starting DSM login.

Follow-up 2026-07-19: replaced the remaining NAS enrollment/edit/auth dialogs
with one reusable three-step wizard: find or enter a NAS, review its connection,
then sign in. Added an authenticated Admin LAN-discovery endpoint backed by the
shared application service. Add starts at discovery, edit and Complete setup at
connection, and sign-in-again at authentication with a route back to connection.
Browser verification exercised an actual three-second LAN sweep, manual IP
prefill, both TLS modes, Complete setup, and Edit without creating/updating a
profile or starting DSM login.

## Coordination

WI-017 remains active in parallel group G and owns packaging/certification.
This item edits only the embedded Admin UI, its tests, and operator-facing UI
documentation. Existing uncommitted WI-017/WI-044 changes must be preserved.

## Handoff

Fill this only when pausing incomplete work.
