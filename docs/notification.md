# Notification

`dsmctl` reads the DSM Control Panel → Notification surface and the per-user
desktop notification feed (the DSM bell) through a read-only module shared by
the CLI and MCP server. This slice never changes notification settings, never
sends a message, and never deletes or marks notifications. No secret material
is ever decoded: the SMTP password, SMS provider account/API key and send-URL
templates, webhook URLs/secrets, and per-device push tokens stay on the NAS.

```console
dsmctl notification capabilities --nas office
dsmctl notification mail --nas office
dsmctl notification push --nas office
dsmctl notification webhook --nas office
dsmctl notification sms --nas office --json
dsmctl notification rules --nas office --group Storage
dsmctl notification desktop --nas office
dsmctl notification history --nas office --limit 20
dsmctl notification history --nas office --level error --from "2026-07-01" --json
```

## Settings areas

Each area is an independent compatibility boundary: a NAS missing one API
reports that area `(not supported)` without disabling the others.

- **mail** — the email channel: whether custom-SMTP email is enabled, the SMTP
  server/port/SSL/certificate-verification/auth-user transport settings, the
  sender, subject prefix, welcome-mail flag, and recipients, plus the
  Synology-relay email mode (send through Synology's servers) when that API is
  present. DSM's own `get` does not return the SMTP password and the decoder
  never reads one.
- **push** — whether mobile push is enabled and the paired push targets
  (mobile apps vs browser registrations). Device push tokens are never
  surfaced.
- **webhook** — configured webhook providers (`profile_id`, name, kind,
  enabled). The webhook URL and any token are never surfaced.
- **sms** — whether SMS is enabled, the selected provider, recipient phone
  numbers, minimum message interval, and the provider catalog (each provider's
  id, HTTP method, and which credential parameters it requires — never the
  values or the credential-bearing send-URL template). DSM 7.3's UI no longer
  offers SMS but still serves the API.
- **rules** — the notification event catalog per profile (DSM's built-in
  profile is `All`): every event key with its group, normalized severity
  (`info`/`warn`/`error`), localized title, source application, and warning
  threshold. Per-event × per-channel enablement is a future write-slice
  concern; this read reports the catalog DSM serves.
- **desktop** — the signed-in user's per-category DSM desktop notification
  toggles (which categories may show desktop notifications).

## Notification history

`notification history` reads the same feed the DSM desktop bell shows — the
notifications DSM actually delivered — newest first. This is the quickest way
to ask "does this system currently have problems?": storage failures, package
errors, security events, and system warnings all land here.

- `--level` (`info`, `warn`, `error`) is applied **server-side** by DSM, so
  `Total matching` reflects the whole filtered feed.
- `--from` / `--to` bound the delivery time, also applied by DSM. Each accepts
  a local timestamp (`2006-01-02` or `2006-01-02 15:04:05`) or Unix seconds.
- `--limit` / `--offset` page the newest-first result; `--limit` is bounded.
- `--lang` selects the DSM string-table language for the rendered text
  (default `enu`; `cht` for Traditional Chinese, etc.).

Each entry carries the raw DSM event key (for example
`StgMgrMountSSDROCacheFail`), the source application, the normalized severity,
the delivery time (formatted plus Unix seconds), and a **rendered title and
message**: dsmctl fetches DSM's notification string templates and substitutes
the entry's `%VAR%` values, producing the same text DSM shows. A value that is
itself a reference into another DSM string table (such as
`dsm:volume:volume`) is left as-is rather than guessed; the raw variables are
included in the JSON output. When the string-table API is unavailable the
entries still return with their raw keys and variables.

## Compatibility

Backends selected on DSM 7.3 (reported by `notification capabilities` and
`nas capabilities`):

| Area    | Capability                  | DSM API                                           |
| ------- | --------------------------- | ------------------------------------------------- |
| mail    | `notification.mail.read`    | `SYNO.Core.Notification.Mail.Conf` v2 (v1 fallback) + `SYNO.Core.Notification.Push.Mail` |
| push    | `notification.push.read`    | `SYNO.Core.Notification.Push.Conf` v1 + `SYNO.Core.Notification.Push.Mobile` |
| webhook | `notification.webhook.read` | `SYNO.Core.Notification.Push.Webhook.Provider` v2 |
| sms     | `notification.sms.read`     | `SYNO.Core.Notification.SMS.Conf` v2 + `SYNO.Core.Notification.SMS.Provider` |
| rules   | `notification.rules.read`   | `SYNO.Core.Notification.Advance.FilterSettings` v2 (v1 fallback) |
| desktop | `notification.desktop.read` | `SYNO.Core.DSMNotify` v1 (`loadHaveNtAppList`)    |
| history | `notification.history.read` | `SYNO.Core.DSMNotify` v1 (`load`) + `SYNO.Core.DSMNotify.Strings` |

## MCP

MCP exposes the same contract through `get_notification_capabilities`,
`get_notification_mail`, `get_notification_push`, `get_notification_webhook`,
`get_notification_sms`, `get_notification_rules`, `get_notification_desktop`,
and `get_notification_history` (all read-only annotations, included in the
read-only gateway). `get_notification_history` accepts `nas`, `limit`,
`offset`, `level`, `from`/`to` time bounds, and `lang`, and returns the
rendered entries plus the total match count — an agent health-check is one
`get_notification_history` call with `level: "error"`.

## Not yet implemented (write slice)

Channel configuration changes, per-event rule changes, sending a test
message, and history deletion/clearing are Slice B of
[WI-072](../spec/work-items/WI-072-notification.md) and are not exposed yet.
