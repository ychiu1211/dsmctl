# System logs

`dsmctl` reads the DSM system log (Log Center) through a read-only module shared
by the CLI and MCP server. It never writes, rotates, or clears logs.

```console
dsmctl log capabilities --nas office
dsmctl log list --nas office
dsmctl log list --nas office --type connection --limit 50
dsmctl log list --nas office --keyword cache --level error --json
```

Each entry is normalized to a stable shape: `time`, `level` (`info`, `warn`, or
`error`), `type` (the canonical DSM category such as `system`, `connection`, or
`fileTransfer`), `who`, and `message`. The list result also carries the
whole-log severity counts DSM reports for the current filter (`total`,
`info_count`, `warn_count`, `error_count`).

## Filters

- `--type` selects a DSM log category. DSM defaults to `system` when omitted;
  other categories are `connection`, `package`, and `fileTransfer`. Each
  category is a separate log; there is no combined "all categories" view.
- `--keyword` is a case-insensitive substring filter applied by DSM.
- `--from` / `--to` bound the entry time. Each accepts a local timestamp
  (`2006-01-02` or `2006-01-02 15:04:05`) or a raw Unix-seconds integer. DSM
  filters by time only when `--from` is set; `--to` is an optional inclusive
  upper bound. A bare date resolves to local midnight, so `--to 2026-07-18`
  ends at the start of the 18th — pass a time to include part of a day.
- `--limit` / `--offset` page the newest-first result; `--limit` is bounded.
- `--level` (`info`, `warn`, `error`) filters severity. DSM exposes no stable
  server-side severity filter, so this is applied by dsmctl to the retrieved
  page: it narrows the returned entries but the counts still describe the full
  keyword/type/time match. To find, for example, the most recent errors, narrow
  with `--from`/`--type` or widen `--limit` so the errors fall inside the
  retrieved window. (DSM stores severities as `info`, `warn`, and `err`; the
  latter is normalized to `error`.)

## Compatibility

The module selects `SYNO.Core.SyslogClient.Log` v1 (`log.read`) and reports it in
`log capabilities` and `nas capabilities`. A DSM without the API makes only this
module unsupported; other modules are unaffected.

## MCP

MCP exposes the same contract through `get_log_capabilities` and `get_logs`
(read-only annotations). `get_logs` accepts `nas`, `limit`, `offset`, `keyword`,
`log_type`, `level`, and the `from`/`to` time bounds (a local timestamp or Unix
seconds), and returns the normalized entries plus severity counts.
