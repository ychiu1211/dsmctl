---
id: WI-072
title: Notification settings module
status: proposed
priority: P2
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/notification
  - internal/synology/operations/notification
  - internal/synology/notification.go
  - internal/runtime/manager.go
  - internal/application/notification.go
  - internal/cli/notification.go
  - internal/mcpserver/server.go
  - internal/mcpserver/read_only.go
  - docs/control-panel.md
---

# WI-072 — Notification settings module

## Outcome

A CLI user or MCP agent can read the Control Panel → Notification surface — the
email (SMTP) channel, the SMS channel, the push channel (mobile + DSM desktop +
Synology relay), the custom webhook providers, and the per-event rule matrix —
and, through the shared hash-bound plan/apply contract, change the channel
configuration and the rule matrix under guardrails, plus send a test message on
a channel. This is a focused Control Panel module in the sense of
[WI-006](WI-006-control-panel-modules.md): one typed module per DSM setting
area with stable semantic field names, never a generic `notification set
key=value` or raw-API proxy.

The API map, versions, live-read shapes, and set fields below are the author's
best current knowledge and **must be live-verified at implementation time**: per
standing policy the WebAPI conf / mobile-client field names are frequently stale
(see [[dsm-webapi-live-verify-fields]]), so confirm every API family, version,
method, and field against the lab with a throwaway `DSMCTL_DUMP` probe before
trusting it. The most likely source of truth on codesearch is `webapi-Core`'s
notification handlers plus the DSM Admin Center `NotificationApp` JS; the likely
API family is `SYNO.Core.Notification.*`.

## Scope

Sliced read-first, then guarded write, so the read slice can ship independently.

### Slice A — read-only

Each channel is a separate compatibility boundary with its own decoder and
capability entry. Likely APIs (all to-be-live-verified):

- **Email (SMTP):** `SYNO.Core.Notification.Mail` (or `.Mail.Conf`) `get` →
  normalized `{enabled, smtp_server, smtp_port, security (none/ssl/tls),
  auth_enabled, username, from_address, from_name, primary_recipient,
  secondary_recipient/cc, subject_prefix}`. The SMTP `passwd` is **never**
  decoded into the state model.
- **SMS:** `SYNO.Core.Notification.SMS` (or `.SMS.Conf`) `get` →
  `{enabled, provider, phone_primary, phone_secondary, message_limit_enabled,
  daily_limit}`; provider catalog via `SYNO.Core.Notification.SMS.ServiceProvider`
  `list`. Provider auth material (account id, API token, or credentials embedded
  in the send URL template) is never decoded.
- **Push:** `SYNO.Core.Notification.Push` (or `.Push.Conf`) `get` →
  `{mobile_enabled, dsm_desktop_enabled, relay_enabled}`; paired mobile devices
  via `SYNO.Core.Notification.Push.Mobile` `list` → `{name, model, last_seen}`.
  Per-device push tokens are identity material and are never surfaced (treated
  like SIDs).
- **Webhook:** `SYNO.Core.Notification.Webhook` (or `.Webhook.Provider`) `list`
  → configured custom providers `{id, name, url_host, method, enabled}` (the URL
  is surfaced host-only or redacted; any bearer/signing secret is never
  decoded). Newer-DSM-only tab; absent on older releases.
- **Rule matrix:** `SYNO.Core.Notification.Rule` / `.Event` `list` → the
  per-event × per-channel enable grid `{event_key, category, mail, sms, push,
  webhook}` for the events DSM exposes (storage/SMART, security/login, package,
  system, etc.).

### Slice B — guarded write (plan/apply, hash-bound) + test send

One plan/apply pair with several patch scopes; the first shipped write should be
the lowest-risk single scope (`mail` SMTP config), following the
WI-041/WI-051 precedent of proving the pattern on one scope before broadening:

- `mail` — SMTP host/port/security/from/recipients/auth via
  `SYNO.Core.Notification.Mail` `set`. SMTP password supplied as
  `credential_ref` (below), never in the patch/plan/hash.
- `sms` — provider/phone/limit via `SYNO.Core.Notification.SMS` `set`; provider
  auth token/credentials via `credential_ref`.
- `push` — mobile/desktop/relay enable toggles via
  `SYNO.Core.Notification.Push` `set`.
- `webhook` — custom-provider create/set/delete (keyed by `id`, DDNS-record-style
  CRUD from WI-041) via `SYNO.Core.Notification.Webhook`; URL secret/token via
  `credential_ref`.
- `rules` — per-event channel toggles via `SYNO.Core.Notification.Rule` `set`
  (patch-only over the observed grid).

Plus a **safe action** (not a config mutation): send a test message on a
channel — likely `SYNO.Core.Notification.Mail` `send_test` (and per-channel
equivalents for SMS/push/webhook), method names to-be-live-verified. It uses the
currently-saved channel config, produces no persistent state change, and is
classified low risk; it is still an outbound side effect, so it is a discrete
CLI command / MCP tool (not gated behind plan/apply) and is excluded from the
read-only gateway.

## Non-goals

- **SMS service-provider template CRUD** — adding/editing custom SMS provider
  definitions (`SYNO.Core.Notification.SMS.ServiceProvider` create/set/delete),
  which embed credential-bearing URL templates and need a disposable provider
  identity to verify.
- **Mobile-device pairing / unpairing** for push; the module reads the paired
  list but does not register or revoke devices.
- **The desktop-notification beep / display / "read" state** and other purely
  per-session UI notification prefs.
- **Certificate, mail-relay MTA, and any account-binding actions** implied by
  Synology push relay (that is account lifecycle, see WI-041's MyDSCenter
  non-goal).
- A generic `notification set key=value` command or exposing every undocumented
  DSM field.

## Design constraints

- **Independent compatibility boundaries.** Mail, SMS, push, webhook, and rules
  are separate API families and separate failure boundaries: a NAS missing one
  (webhook absent on older DSM, SMS unconfigured, push relay off) must leave the
  others usable and report the missing area `(not supported)` rather than
  erroring the whole module. Selection is per operation with a stable
  operation/capability name, selected backend, API, and version in the
  capability report; fail closed when an area's API is absent.
- **Secrets never enter requests, plans, hashes, logs, or MCP args.** The SMTP
  password, SMS provider auth token/credentials, and webhook secret/bearer token
  use the existing `credential_ref: env:NAME` mechanism, resolved only at apply
  time and absent from the request, plan, approval hash, result, and logs (the
  contract in `architecture-contracts.md` → Secrets and identity, as user
  password changes and WI-041 DDNS `passwd` already do). Push device tokens and
  any account tokens are never surfaced by display models.
- **Redirecting or silencing alerts is high risk; benign transport tweaks are
  medium.** Changing the recipient set, disabling a channel, or turning off rule
  rows that carry security-relevant events (account login, storage/SMART health,
  security advisories) can hide security-critical notifications from the admin —
  classify those mutations **high risk**. Pure transport changes (SMTP
  host/port/TLS/from-name that keep the same recipients and enabled state) are
  medium. State the direction-dependent classification in the plan summary.
- **Patch-only + postcondition.** Follow the module pattern: plan records and
  hashes the complete observed state of the touched scope, apply rejects a
  changed state, merges the patch into a freshly read config, and **re-reads to
  verify the requested fields took effect** — DSM silently ignores some fields
  (the recurring lesson: WI-041 relay's `set_relay_enable` → `set_misc_config`
  correction). Omitted fields are never sent and never silently reset. An empty
  patch is rejected by the application layer, not sent (WI-051 lesson: empty
  `set` can be a silent DSM no-op success).
- **Webhook and rules set semantics need impl-time confirmation.** Whether
  webhook providers are id-keyed with server-assigned ids, and whether the rule
  `set` takes the full grid or a sparse patch, must be confirmed with one
  authorized, fully reverted live round-trip before those scopes ship.

## Acceptance criteria

- [ ] Slice A: `notification capabilities|mail|sms|push|webhook|rules` (CLI) and
      the matching `get_*` MCP tools return normalized state with semantic field
      names; a decode fails (rather than returning empty success) on a malformed
      shape.
- [ ] No secret leak: unit tests assert the decoded state never carries the SMTP
      password, SMS auth token, webhook secret, or push device tokens; a live
      `--json` grep confirms their absence.
- [ ] Independent gating: each channel selects its own backend and is reported
      `(not supported)` when its API/version is absent (webhook on older DSM,
      etc.) without disabling the other channels.
- [ ] Slice A live verification on the DSM 7.3 lab: read every present channel
      and the rule matrix with no secret leak; unconfigured channels report a
      clean disabled/empty state.
- [ ] Slice B (first write): `mail` SMTP config via guarded hash-bound
      plan/apply, with a request-capture test proving `credential_ref` resolution
      (the password appears only on the wire, never in plan/hash/logs) and a
      postcondition re-read; recipient/disable-direction changes classified high,
      transport-only medium.
- [ ] Test send: `notification test <channel>` CLI command and MCP tool exist,
      are excluded from the read-only gateway, and are not gated behind
      plan/apply; a live test email is sent and received on the lab.
- [ ] plan/apply MCP tools are excluded from the read-only gateway
      (`internal/mcpserver/read_only.go`); the module registers via
      `internal/runtime/manager.go`.
- [ ] Slice B live verification on the DSM 7.3 lab (authorized, fully reverted):
      a mail config field round-trip through plan/apply with postcondition proof;
      remaining scopes (sms/push/webhook/rules) documented as pending if their
      throwaway resources are unavailable.

## Verification

- Decoder + request-capture unit tests; `go test ./... -count=1`, `go vet ./...`.
- Live read on an explicitly configured NAS (authenticated session reads); live
  reverted writes require explicit per-session authorization. Sending a test
  message is an outbound side effect and requires the operator to confirm a
  disposable recipient before it is run live.
- Source of truth for fields: `webapi-Core` notification handlers +
  `SYNO.Core.Notification.*` conf files and the DSM Admin Center
  `NotificationApp` JS on codesearch (branch matching the lab DSM), each field
  confirmed with a throwaway `DSMCTL_DUMP` probe.

## Coordination

- Parallel group C. Shares the Control Panel domain/facade registry with the
  other Control Panel modules; new operation package under
  `internal/synology/operations/notification`, new domain under
  `internal/domain/notification`. Shares `internal/mcpserver/server.go`,
  `internal/mcpserver/read_only.go`, and `internal/runtime/manager.go` with any
  concurrent module work — check for active parallel edits before claiming.
- No overlap with the External Access module (WI-041) beyond the shared facade;
  note that Synology push relay depends on account binding, which WI-041 owns as
  a non-goal, so keep push-relay handling read + toggle only.
