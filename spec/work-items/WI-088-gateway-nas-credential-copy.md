---
id: WI-088
title: Copy stored NAS account and password from the Gateway NAS list
status: done
priority: P2
owner: ""
depends_on: [WI-015, WI-032, WI-047]
parallel_group: G
touches:
  - internal/gateway/admin/handler.go
  - internal/gateway/admin/handler_test.go
  - internal/gateway/admin/ui.go
  - docs/gateway.md
  - docs/gateway-admin-guide.md
  - spec/roadmap.md
---

# WI-088 — Copy stored NAS account and password from the Gateway NAS list

## Outcome

A signed-in Gateway administrator can copy a NAS profile's DSM account and its
vault-stored DSM password directly from the NAS list, completing the
discover → add → enroll → retrieve loop inside the Admin UI. The password
leaves the vault only through an explicit, audited administrator action and is
never reachable over `/mcp`.

## Scope

- `POST /admin/api/profiles/{name}/credentials/reveal` returns the profile's
  DSM account and the vault-stored password to an authenticated administrator
  browser session. The endpoint is POST-only so the existing browser-mutation
  boundary (`X-DSMCTL-Request`, JSON Content-Type, Origin match) applies.
- Reveal reads only the encrypted vault secret enrolled through the Admin UI
  (`password_stored`). Environment-variable password fallback is deliberately
  not revealed; a profile without a stored password answers 404.
- Every reveal request is audited as its own `credential.reveal` action
  (started/success/failure), distinct from `credential.manage`.
- The NAS list row menu gains `Copy account` (client-side, from the already
  non-secret `username` field) and `Copy password` (reveal then clipboard;
  shown only while `password_stored` is true), localized in all five locales.
- The password is written to the clipboard only; it is never rendered into the
  DOM, stored in browser storage, or echoed into Toast text.

## Non-goals

- No reveal of web-login session material (SID/SynoToken/keys) — sessions stay
  resumable secrets, not copyable credentials.
- No reveal of environment-provided passwords or apply-time `vault:` secrets.
- No MCP tool or `/mcp` surface change; WI-008's decision stands (human-gated
  reveal only, never over MCP).
- No re-authentication prompt: the administrator session that can enroll and
  delete credentials is the same trust anchor that may reveal them.
- No change to LAN discovery or credential enrollment, which already exist in
  the NAS wizard (WI-047).

## Design constraints

- The admin cookie session and MCP bearer tokens are disjoint authenticators;
  the reveal route lives under `/admin/api/` only.
- Keep the WI-047 secret non-disclosure guarantee for every existing endpoint:
  profile lists, credential status, and enrollment responses still never carry
  plaintext. Reveal is the single deliberate exception at a dedicated path.
- Responses carry `Cache-Control: no-store` (existing writeJSON behavior).
- Audit events must not contain the revealed value.

## Acceptance criteria

- [x] An authenticated POST to the reveal path returns the stored account and
      password; the same request without a session cookie is 401 and GET is 405.
- [x] A profile with no vault-stored password (including session-only
      profiles) answers 404 without touching the environment fallback.
- [x] Audit records `credential.reveal` with started/success (and failure on
      404), and the password never appears in audit output.
- [x] The NAS list shows Copy account / Copy password in the row menu exactly
      when the profile has a username / stored password, in all five locales.
- [x] `go test ./internal/gateway/...`, `go test ./...`, `go vet ./...`, and a
      Node syntax check of the embedded UI script pass.

## Verification

- `go test ./internal/gateway/admin -count=1`
- `go test ./...` and `go vet ./...`
- Node `--check` on the extracted `<script>` body of `ui.go`, plus the embedded
  i18n diagnostics (no missing/extra keys per locale).
- No live DSM contact is required; enrollment fixtures cover storage.

Completed 2026-07-21: all of the above ran clean (Go 1.25.0 toolchain rules —
no `gofmt` sweep). Reveal verified against a bbolt-backed test repository with
an enrolled password; browser-boundary tests reuse `performJSON` fixtures.

## Coordination

Parallel group G. WI-081 (TLS trust) and WI-083 (provision) are in flight on
other branches; this item touches only the admin handler/UI/tests and gateway
docs. WI-084–WI-087 numbers are consumed by parallel provision/Hyper Backup
sessions; this item takes WI-088.

## Handoff

Fill this only when pausing incomplete work.
