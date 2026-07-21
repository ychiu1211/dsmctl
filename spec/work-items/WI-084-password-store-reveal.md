---
id: WI-084
title: NAS password credential-store lifecycle and human-gated reveal
status: in_progress
owner: "claude"
depends_on: [WI-009, WI-015, WI-032]
parallel_group: G
touches:
  - internal/credentials/secure_store.go
  - internal/cli/auth.go
  - internal/cli/service.go
  - internal/gateway/state/vault.go
  - internal/gateway/admin/handler.go
  - internal/gateway/admin/ui.go
  - docs/credentials.md
  - docs/gateway-admin-guide.md
---

# WI-084 — NAS password credential-store lifecycle and human-gated reveal

## Outcome

A DSM account password can be stored in a real credential store on every
deployment surface, is actually consulted for automatic re-login, and can be
inspected by a human through an explicitly gated reveal:

- **CLI (desktop)**: `dsmctl auth password set/remove/reveal` manage the
  password in the OS credential store (Windows Credential Manager, macOS
  Keychain, Linux Secret Service) that already holds web-login sessions.
- **Gateway (container / Synology SPK)**: the existing AES-256-GCM vault
  (WI-015) already stores enrolled passwords; the Admin UI gains a
  password-reveal action so the local administrator can view the stored DSM
  password for any NAS profile after re-entering their administrator password.

This implements the WI-008 product decision (2026-07-20): secret material is
vault-managed and retrievable only via a human-gated reveal — never over MCP.

## Scope

- CLI `auth password set`: verify the account password against the NAS
  (including OTP and trusted-device registration, mirroring the gateway
  password enrollment), then persist it in the OS credential store. Interactive
  hidden prompt by default; `--password-stdin` for automation; `--otp` for
  non-interactive OTP. Updates the profile's configured username when
  `--account` differs.
- CLI `auth password remove`: delete the stored password (and, with
  `--trusted-device`, the trusted-device credential).
- CLI `auth password reveal`: print the stored password only when stdin and
  stdout are both interactive terminals and the operator retypes the NAS name
  as confirmation. Refuses redirection/pipes.
- `auth status` displays a PASSWORD column (stored / env:NAME / none) from the
  existing `AuthStatus` fields.
- The CLI runtime credential resolver consults the OS credential store first
  and falls back to the profile environment variable (previously env-only), so
  a stored password is actually used for automatic re-login when a session
  cannot be resumed.
- Gateway vault: `StoredPassword` read that never falls back to environment
  variables.
- Gateway Admin API: `POST /admin/api/profiles/{name}/credentials/password/reveal`
  requiring administrator password re-verification
  (`VerifyAdministratorCredentials`), rate-limited per remote host, audited as
  `credential.reveal` with the NAS name; 404 when no password is stored.
- Admin UI: per-NAS "Reveal stored password" action (visible only when a
  password is stored) opening a re-authentication dialog, showing the DSM
  account and password once with a copy button, cleared when the dialog
  closes. Localized in en, zh-TW, zh-CN, ja, de.

## Non-goals

- Fresh-NAS provisioning and generated first-admin passwords (WI-083).
- Any MCP tool that returns, accepts, or previews a password (unchanged
  contract; the read-only gateway MCP surface is untouched).
- The gateway reading the desktop OS keyring, or the CLI reading the gateway
  vault (the stores remain deployment-local by design).
- Portable/encrypted secret export, printing passwords to non-TTY outputs, or
  a "show password" toggle on the enrollment form.
- Revealing session material (SID, SynoToken, resume keys) or trusted-device
  IDs anywhere.

## Design constraints

- Architecture contract amendment (this item): passwords stay out of display
  models, plans, logs, and MCP tool arguments; the only sanctioned exceptions
  are the two dedicated reveal surfaces defined here — Admin UI reveal after
  administrator re-verification (audited), and CLI reveal on an interactive
  terminal after typed confirmation. Neither is reachable over MCP.
- Reveal responses/outputs are never logged; audit events carry only actor,
  NAS, action, outcome.
- Admin reveal follows the existing browser-mutation rules (same-origin JSON,
  `X-DSMCTL-Request`, HttpOnly session cookie) plus a dedicated attempt
  limiter, and reuses the OAuth-consent re-verification primitive.
- CLI verification uses the same fail-closed TLS trust flow as web login
  (`prepareWebLoginTLS`); a password is stored only after DSM accepts it.
- The OS-keyring resolver keeps the environment fallback for automation and
  degrades with actionable guidance when no credential source exists.

## Acceptance criteria

- [x] `dsmctl auth password reveal` refuses when stdin or stdout is not a
  terminal, and prints the password only after the NAS name is retyped
  (unit-verified).
- [x] `dsmctl auth password remove` deletes the entry and reports what was
  removed; repeat removal is a no-op, not an error (unit-verified via
  `DeletePassword`).
- [x] Gateway reveal returns the stored password only after correct
  administrator re-verification; a wrong administrator password yields 401 and
  a denied `credential.reveal` audit event; missing stored password yields
  404; repeated failures are rate limited (429) (unit-verified in
  `handler_test.go`).
- [x] Admin UI shows the reveal action only for profiles with a stored
  password, requires the administrator password, offers copy, and clears the
  revealed value when the dialog closes; all five UI languages carry the new
  strings (parity guard passes).
- [x] Admin UI adds an **Open NAS** row action (opens `profile.url` in a new
  tab via `window.open`, allowed under the current CSP) and, in the reveal
  dialog, **Copy account** + **Open NAS** buttons alongside Copy password, so an
  operator can copy the DSM account and password and jump to the NAS to connect
  (CMS convenience); localized in all five languages and guarded by UI markers.
- [x] No MCP tool schema changes; passwords appear in no audit event, log
  record, or MCP result (the reveal handler builds its own NAS-only audit
  events; the reveal response is never logged).
- [ ] Live: `dsmctl auth password set --nas lab` verifies against DSM and
  stores the password; `auth status` shows `PASSWORD stored`; a broken session
  then re-authenticates automatically without `DSMCTL_PASSWORD_*`. **Pending a
  single live auth call against the lab NAS (see Handoff).**

## Verification

- `go build ./... && go vet ./... && go test ./...`
- Focused: `go test ./internal/gateway/... ./internal/cli/... ./internal/credentials/... ./internal/application/...`
- Live (lab NAS, read-only auth call): `auth password set` with the existing
  lab automation password, `auth status`, non-TTY `reveal` refusal. No DSM
  mutation is performed; the only NAS traffic is one authentication.
- DSM: protocol-neutral (login API already covered by existing tests).

## Coordination

Overlaps `internal/gateway/admin/ui.go` and `handler.go` with any concurrent
Admin UI item (WI-017 certification is asset-frozen; no active overlap known).
WI-083 (provision) consumes this item's storage + reveal path for generated
passwords.

## Handoff

Implementation complete and unit-verified; `go build ./...`, `go vet ./...`,
and `go test ./...` all pass. Added a `golang.org/x/term` direct dependency.

Remaining before `done`: one live pass against the lab NAS (per
[[dsmctl-lab-nas-credentials]]) exercising
`dsmctl auth password set --nas lab` (a single read-only DSM authentication,
no mutation), confirming `auth status` shows `PASSWORD stored`, then a
non-terminal `auth password reveal` refusal and an interactive reveal. No DSM
state is changed by this test.

New/changed surfaces:
- CLI: `internal/cli/auth_password.go` (+`_test.go`); `auth status` PASSWORD
  column and keyring-first CLI runtime resolver in `service.go`/`auth.go`.
- Store: `credentials.SecureStore.StoredPassword` /
  `state.Repository.StoredPassword` (no env fallback).
- Gateway: `POST /admin/api/profiles/{name}/credentials/password/reveal` in
  `handler.go` (rate-limited, admin re-verify, `credential.reveal` audit) and
  the Admin UI reveal dialog/action in `ui.go` (en/zh-TW/zh-CN/ja/de).
