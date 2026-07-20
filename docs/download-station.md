# Download Station

A read-only module for the Synology Download Station package, package-version
gated on the installed `DownloadStation` package like the Photos and Surveillance
modules. The service/task/statistic reads use the stable, publicly-documented
legacy `SYNO.DownloadStation.*` API (each served from its own CGI path, resolved
from the discovered API registry); the full detailed settings read uses the
newer `SYNO.DownloadStation2.Settings.*` API generation (all on `entry.cgi`).

```console
dsmctl download capabilities --nas office
dsmctl download service --nas office --json
dsmctl download tasks --nas office
dsmctl download statistics --nas office
dsmctl download settings --nas office
```

- **`capabilities`** reports the installed package evidence (installed, version,
  running) and which reads are available, each selected independently. A NAS
  without Download Station — or below the verified 3.0 baseline — fails closed.
- **`service`** reads `SYNO.DownloadStation.Info` (`getinfo` + `getconfig`) and
  `SYNO.DownloadStation.Schedule` (`getconfig`): version, manager flag, default
  destination, eMule and auto-unzip switches, per-protocol (BT/eMule/FTP/HTTP/NZB)
  rate limits in KB/s (0 = unlimited), and the bandwidth schedule.
- **`tasks`** reads `SYNO.DownloadStation.Task` (`list`): each task's id, type,
  title, size, status, destination, and live transfer speed. Task entries are
  decoded tolerantly (a size or speed returned as a quoted string is handled)
  because the verification NAS had no task to populate the list.
- **`statistics`** reads `SYNO.DownloadStation.Statistic` (`getinfo`): the
  aggregate download and upload speed in bytes/s.
- **`settings`** composes the `SYNO.DownloadStation2.Settings.*` reads into the
  full detailed configuration: BitTorrent (TCP/DHT ports, DHT, port forwarding,
  preview, encryption, rate limits, max peers, seeding), eMule, FTP/HTTP, NZB,
  automatic extraction, destination/watch-folder, RSS refresh interval, and the
  bandwidth scheduler (with DSM's raw 168-slot weekly bitmap). The NZB password
  and archive-extraction passwords are never surfaced — only a
  `password_configured` flag is.

MCP exposes the same reads through `get_download_station_capabilities`,
`get_download_station_service`, `get_download_station_tasks`,
`get_download_station_statistics`, and `get_download_station_settings`. All are
read-only.

## Guarded task control

Download tasks are created and controlled through the same hash-bound plan/apply
contract as the other modules. One request performs exactly one action —
`create` (with `uris` and an optional `destination`), `pause` / `resume` /
`delete` (with `task_ids`), or `edit` (with `task_ids` and a required
`destination` to re-target the tasks):

```console
echo '{"action":"create","uris":["https://example.com/file.iso"],"destination":"Share"}' \
  | dsmctl download tasks plan --nas office -o task.plan.json
dsmctl download tasks apply --nas office -f task.plan.json --approve <hash-from-plan>

echo '{"action":"pause","task_ids":["dbid_5"]}' | dsmctl download tasks plan --nas office -o pause.plan.json
dsmctl download tasks apply --nas office -f pause.plan.json --approve <hash>
```

A control plan binds to the **stable identity** of the target tasks (id, title,
type — and for `edit` the observed destination) — not their volatile transfer
progress — so an apply fails cleanly if a target has since disappeared, while a
download progressing does not invalidate the plan. Apply verifies the
postcondition afterward: `create` confirms a task with a matching uri exists,
`pause`/`resume` confirm the paused state, `delete` confirms the task is gone,
and `edit` confirms each task reports the new destination. Per-task failures in
DSM's response are surfaced, never silently dropped. `create` and `resume` make
the NAS fetch external content and `delete` removes the task, so those are
**high** risk; `pause` and `edit` are medium (editing moves the task's data to
the new destination). `edit` uses the legacy Task API method `edit`, which
exists from **version 2** (v1 returns error 103), so it is a separately gated
operation (`download.task.edit`) that fails closed on a NAS advertising only
Task v1. Task rename is not a Download Station feature. MCP exposes
`plan_download_station_task_change` and `apply_download_station_task_plan`
(excluded from the read-only gateway).

## Guarded settings write

Settings are changed through the same plan/apply contract. A change carries
exactly one patch-only group patch. The writable groups are:

- **BitTorrent** — ports, DHT, port forwarding, preview, encryption, rate
  limits, max peers, seeding.
- **FTP/HTTP** — max download rate and per-task connection limit.
- **RSS** — feed refresh interval.
- **Location** — default download destination and the torrent/NZB watch folder.
- **Scheduler** — the alternative-rate weekly schedule, scheduled rates, and max
  simultaneous tasks.
- **Global** — download volume and the eMule / auto-extract service toggles.
- **Auto-extraction** — the per-user archive-extraction preferences (enable,
  create subfolder, delete archive, overwrite, local vs fixed destination).
- **NZB** — the Usenet news-server settings (server, port, username, auth, SSL,
  PAR2 repair, connections per download, max download rate).

  Auto-extraction and NZB are applied as **partial sets** — only the patched
  fields are sent, so the passwords the read never returns are left untouched
  by default. Passwords change only through **credential references**: the NZB
  patch takes `password_ref` (e.g. `env:MY_NZB_PASSWORD`) and auto-extraction
  takes `passwords_ref` resolving to a newline-separated password list. The
  reference — never the value — is what enters the plan; it resolves only while
  an approved plan is applied. `clear_password` / `clear_passwords` remove the
  stored secrets explicitly (an empty environment variable is rejected by the
  resolver). The auto-extraction postcondition re-checks the
  `password_configured` flag; the NZB API returns no password indicator, so
  that write is confirmed by DSM's success response only.

```console
echo '{"bt":{"max_upload_rate":15,"enable_preview":false}}' | dsmctl download settings plan --nas office -o bt.plan.json
dsmctl download settings apply --nas office -f bt.plan.json --approve <hash>
```

Because the DSM `set` is a full-object replace, apply reads the complete target
group, merges the patch, and submits the whole object so an unspecified field is
never reset; the plan binds to the complete observed group and apply verifies
each changed field. Enabling BitTorrent port forwarding opens the BT port on the
router (external exposure) and is high risk; other changes are medium. MCP
exposes `plan_download_station_settings_change` and
`apply_download_station_settings_plan` (excluded from the read-only gateway).

Three DSM behaviors the guard accounts for. An **empty string form value is
dropped** by the DownloadStation2 JSON-request parser (live-verified: sending
`username=` left the stored name untouched, caught by the postcondition), so
dsmctl encodes an empty string as the JSON literal `""`, which does clear the
field. The scheduler `schedule` is a
168-character weekly bitmap that must be sent as a quoted JSON string (an
all-digit value otherwise parses as a number and DSM rejects it). The location
default destination is a **per-user share binding**: DSM applies it but provides
no API to clear it back to unset, so a set can only re-point it to another share
— treat it as irreversible. DSM also returns `(null)` for an unset watch folder,
which the reader normalizes to empty so a subsequent set does not echo the
sentinel back and fail path validation.

Field shapes and set semantics are live-verified on Download Station 4.1.2,
including the task edit round-trip and both password writes (set via
credential reference, then cleared). Still out of scope: the eMule group
(enabling it starts the eMule service), BT/eMule search, RSS feed management,
and eMule server management — see
[WI-043](../spec/work-items/WI-043-download-station.md).
