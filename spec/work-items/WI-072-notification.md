---
id: WI-072
title: Notification settings module
status: in_progress
priority: P2
owner: "claude (dsm-notification-setup worktree)"
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
  - docs/notification.md
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

**Scope addition (2026-07-20, product request):** besides the settings surface,
the module must read the **notification history** — the per-user DSM desktop
notification feed (the bell) — so an operator or MCP agent can ask "does this
system have problems?" and get the actual delivered notifications (storage
failures, package errors, security events) with level/date filters. The history
and the per-app desktop-notification toggles are additional independent read
areas alongside the WI-006-style settings channels.

## Live-verified wire map (DSM 7.3-81168 lab, 2026-07-20)

Every read below was probed against the lab with a throwaway authenticated
probe (per [[dsm-webapi-live-verify-fields]]); write methods were confirmed
from the DSM 7.3 `NotificationVueBundle.js` / `DSMNotify*.js` sources served by
the NAS itself and remain WIRE-UNVERIFIED until Slice B runs a reverted
round-trip.

Read (live-verified response shapes):

- `SYNO.Core.Notification.Mail.Conf` v2 `get` → `{enable_mail, enable_oauth,
  in_use[], profiles[], send_welcome_mail, sender_mail, sender_name,
  smtp_auth{enable,user}, smtp_info{port,server,ssl,verifyCert},
  subject_prefix}`. **No password field in the response.** v1 differs only in
  `mail[]` instead of `profiles[]`.
- `SYNO.Core.Notification.Push.Mail` v2 `get` → `{enable_mail, mail[],
  subject_prefix, template_id}` (Synology-relay email mode).
- `SYNO.Core.Notification.Push.Conf` v1 `get` → `{mobile_enable, msn_*,
  skype_*}` (msn/skype are dead legacy fields; only `mobile_enable` is
  meaningful).
- `SYNO.Core.Notification.Push.Mobile` v2 `list` → `{count, list[]}` (paired
  devices; UI distinguishes device vs browser by `app_version` presence).
- `SYNO.Core.Notification.Push.Webhook.Provider` v2 `list` → `{count, list[]}`.
- `SYNO.Core.Notification.SMS.Conf` v2 `get` → `{api_id, enable_sms,
  msg_interval, phone_info[{code,num,prefix}], provider_name, sender, user}`
  (no password field). `SYNO.Core.Notification.SMS.Provider` v2 `list` →
  `provider_info[{provider_id, provider_name, req_method, param_used{...},
  template, ...}]`; the `template` URL may embed user credentials on custom
  providers and is never decoded. Note: DSM 7.3's Notification UI no longer
  offers SMS, but the API family is still served.
- `SYNO.Core.Notification.CMS.Conf` v2 `get` → `{available_templates[],
  cms_enable, join_dsm_cms, template_id}` (read deferred; CMS is out of the
  first slice).
- `SYNO.Core.Notification.Advance.FilterSettings` v2 (and v1) `list` →
  `{<profile>: [{appid, format, group, level, name, source, tag, title,
  warnPercent}]}`; default profile key is `All` (257 events on the lab).
  `level` ∈ NOTIFICATION_INFO/WARN/ERROR. Passing `format=<channel>` does not
  change the entry count on the lab; per-channel enablement semantics are a
  Slice B question. `SYNO.Core.Notification.Advance.Variables` v1 `get` →
  `{company_name, http_url}`; `.Advance.CustomizedData` v1 `get` →
  `{subject, content, default_subject, default_content}`.
- **History (desktop bell):** `SYNO.Core.DSMNotify` v1, single method `notify`
  multiplexed by `action`:
  - `action=load` with optional `offset`, `limit`, `level`
    (NOTIFICATION_ERROR etc.), `dateFrom`, `dateTo` (epoch seconds), `sortBy`
    (`time`), `sortDir` (`ASC`/`DESC`) → `{items[], total, newestMsgTime}`.
    Item: `{notifyId, time (epoch), level, title (string key such as
    StgMgrMountSSDROCacheFail), msg [JSON-encoded map of %VAR% → value],
    className (source app id), tag, hasMail, mailType, isEncoded, ...}`.
  - `action=loadHaveNtAppList` → `{items[{app, appids[], category,
    category_enu, dsmnotify: "on"|"off"}], total}` — the per-category desktop
    notification toggles.
  - `SYNO.Core.DSMNotify.Strings` v1 `get` `{pkgName:"", lang}` → map keyed by
    notification key: `{<key>: {level, title, msg}}` with `%VAR%` placeholder
    templates (~110 KB for enu). History rendering = template + item var map;
    nested `table:section:key` indirections stay unresolved.

Write methods confirmed in the DSM 7.3 UI source (Slice B, WIRE-UNVERIFIED):
`Mail.Conf set`, `Mail send_test` / `refresh_token` (OAuth), `Mail.Profile.Conf
create/set/delete (profile_id)`, `Push send_test {target_id_list}`,
`Push.Mobile set {settings[]} / unpair {target_id_list}`,
`Push.Webhook.Provider create/set/delete/send_test (profile_id)`,
`Advance.WarningPercentage set {warn_type, warn_percent}`, and history
mutation `DSMNotify notify action=delete_by_ids {notifyIds[]} /
action=apply {clean:"all"}`. LINE pairing (`Notification.Line get_bot_info`)
is deferred with the webhook wizard.

## Scope

Sliced read-first, then guarded write, so the read slice can ship independently.

### Slice A — read-only

Each area is a separate compatibility boundary with its own decoder and
capability entry (APIs and shapes per the live-verified wire map above):

- **Email (SMTP):** `SYNO.Core.Notification.Mail.Conf` `get` (v2 preferred,
  v1 fallback), normalized to `{enabled, oauth_enabled, sender_name,
  sender_mail, subject_prefix, welcome_mail_enabled, smtp {server, port, ssl,
  verify_cert, auth_enabled, auth_user}, recipients}`; enriched with the
  Synology-relay mode from `SYNO.Core.Notification.Push.Mail` `get`
  `{relay_enabled, relay_recipients, relay_subject_prefix}` when available.
  The SMTP password is **never** decoded (and DSM's `get` does not return it).
- **SMS:** `SYNO.Core.Notification.SMS.Conf` `get` → `{enabled, provider,
  phones, interval}` (the provider account id / api user are auth material and
  are not decoded, per the original non-goal); provider catalog from
  `SYNO.Core.Notification.SMS.Provider` `list` reduced to `{id, name, method,
  required params}` — the credential-bearing `template` URL is never decoded.
- **Push:** `SYNO.Core.Notification.Push.Conf` `get` → `{mobile_enabled}`
  (legacy msn/skype fields ignored); paired devices via
  `SYNO.Core.Notification.Push.Mobile` `list`, decoded tolerantly (the lab has
  no paired device). Per-device push tokens are never surfaced.
- **Webhook:** `SYNO.Core.Notification.Push.Webhook.Provider` `list` →
  configured providers, decoded tolerantly (lab list is empty); any
  token/secret-bearing field is never decoded.
- **Rule matrix:** `SYNO.Core.Notification.Advance.FilterSettings` `list`
  (v2/v1) → per-profile event catalog `{name, tag, title, group, level,
  source, app id, warn percent}` with the profile name (`All` by default)
  preserved. Per-channel enablement semantics stay a Slice B question; the
  read reports the event catalog DSM actually serves.
- **Desktop notification settings:** `SYNO.Core.DSMNotify` `notify`
  `action=loadHaveNtAppList` → per-category desktop toggles
  `{category, app ids, enabled}`.
- **History (the user-visible notification feed):** `SYNO.Core.DSMNotify`
  `notify` `action=load` with `{offset, limit, level, from, to}` filters →
  `{total, newest_time, entries[]}`; each entry carries the raw key, source
  app, level, time, and a **rendered title/message** produced by substituting
  the item's `%VAR%` map into the `SYNO.Core.DSMNotify.Strings` templates
  (requested per query language, default `enu`). Unresolvable nested
  `table:section:key` references are left as-is rather than guessed.
- **CMS notification relay** (`SYNO.Core.Notification.CMS.Conf`) is deferred:
  it only matters on CMS-managed fleets and needs one to verify.

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

- [x] Slice A: `notification capabilities|mail|sms|push|webhook|rules|desktop|history`
      (CLI) and the matching `get_notification_*` MCP tools return normalized
      state with semantic field names; a decode fails (rather than returning
      empty success) on a malformed shape. **Done** (decoder tests reject
      missing `enable_mail`/`total`/profileless rules).
- [x] History: `notification history` supports offset/limit paging plus level
      and date-range filters applied by DSM, and renders human-readable
      title/message text from the DSM string templates alongside the raw
      notification key, so an agent can answer "does this system currently have
      problems?" from the same feed the DSM desktop bell shows. **Live-verified
      2026-07-21** on the lab: `--level error` narrowed Total 128 → 6 and
      `--from 2026-07-18` narrowed Total → 2 server-side; offset paging and
      `--lang cht` rendering confirmed.
- [x] No secret leak: unit tests assert the decoded state never carries the SMTP
      password, SMS auth token, webhook secret, or push device tokens; a live
      `--json` grep confirms their absence (only the SMS provider catalog's
      *parameter names* `api_id`/`api_key` appear, by design; no values, URLs,
      or `@@` templates).
- [x] Independent gating: each channel selects its own backend and is reported
      `(not supported)` when its API/version is absent (webhook on older DSM,
      etc.) without disabling the other channels (unit-tested with a
      DSMNotify-only target: history/desktop supported, mail unsupported).
- [x] Slice A live verification on the DSM 7.3 lab (7.3-81168, 2026-07-21):
      all seven areas supported and read; unconfigured channels (mail, push,
      webhook, SMS) report clean disabled/empty state; rules serve profile
      `All` with 257 events; history surfaces the lab's real problems
      (Surveillance Station dependency install failures, SSD read-only cache
      mount failures).
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

## Progress

- **Slice A (read-only, all seven areas + history) shipped and live-verified
  2026-07-21** on the DSM 7.3-81168 lab. `go test ./... -count=1` and
  `go vet ./...` clean; 15 decoder/request-capture unit tests in
  `internal/synology/operations/notification`. CLI `dsmctl notification
  capabilities|mail|push|webhook|sms|rules|desktop|history`, MCP
  `get_notification_*` (8 tools, read-only annotations, prefix-scoped
  `nas.read`, included in the read-only gateway). User docs in
  `docs/notification.md`.
- Known rendering limitation (documented): variable values that are
  themselves DSM string-table references (`dsm:volume:volume`,
  `pkgmgr:fail_op_install_pkg`) are substituted verbatim, not resolved
  through the UIString tables.
- **Slice B (writes + test send) not started.** The wire map above records
  the UI-source method names; every write remains WIRE-UNVERIFIED until a
  reverted live round-trip. No temporary lab resources were left behind by
  Slice A (read-only probes; the throwaway probe session logged out).

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
