# Drive Admin

Setting up Drive from scratch? Follow the end-to-end
[Drive setup playbook](drive-playbook.md); this page documents the module
itself.

The Drive Admin module manages functionality provided by the **Synology Drive
Server package** (`SynologyDrive`), not by DSM itself. It is the first consumer
of dsmctl's package-scoped operation selection: Drive's WebAPI behavior follows
the installed package release rather than the DSM release, so before every
command dsmctl re-reads the installed-package inventory, verifies the package
and its version, and routes to the operation variant whose package-version
range matches. Two NAS on the same DSM build with different Drive versions can
therefore select different backends, and a Drive release older than the
verified baseline fails closed instead of receiving untested requests.

The module covers the Admin Console reads — service status, active
connections, team folders, and Drive server logs — plus two guarded writes:
the server database configuration (vmtouch pair, WI-031) and team-folder
enable/disable/versioning (WI-050). Everything else stays deferred (see
[Deferred operations](#deferred-operations)).

## Capabilities and package evidence

```console
dsmctl drive admin capabilities --nas office
```

Reports, per operation, whether a verified backend was selected, plus the
installed-package evidence used for selection: whether `SynologyDrive` is
installed, the observed version, and whether the package service was running.
The compatibility report's `packages` list and each selection reason carry the
same evidence, so an unsupported module is diagnosable from the output alone:

- package not installed → every operation unsupported with
  "package SynologyDrive is not installed";
- package below the verified baseline (3.0) → unsupported with the observed
  version and required range;
- package installed but stopped → operations stay selected, reads fail with
  guidance to start the package through a Package Center lifecycle plan.

MCP exposes the same result through `get_drive_admin_capabilities`.

## Reads

```console
dsmctl drive admin status --nas office
dsmctl drive admin connections --nas office --json
dsmctl drive admin team-folders --nas office
dsmctl drive admin log list --nas office --limit 50
dsmctl drive admin log list --nas office --username alice --keyword report --from "2026-07-01" --to "2026-07-17"
```

- `status` returns the Drive service status as reported by the package
  (lowercased, for example `enabled`) plus the package evidence observed with
  this exact call.
- `connections` lists active Drive client sessions with the fields the Drive
  server actually reports (WI-054): the session id (the target for a guarded
  kick), device name, client type, address, status, client version, location,
  device UUID, relay flag, and login/last-auth times. Sessions are not
  attributed to an account name by the API.
- `team-folders` lists shared folders from the admin team-folder view: the
  name, whether each is enabled as a Drive team folder, Drive's share status,
  the share type, and — for enabled team folders — the versioning settings
  (kept versions, rotation policy `fifo`/`smart`, retention days). Drive
  reports versioning fields as `"-"` on disabled folders; they surface as
  absent. Drive's home entry appears as `homes/mydrive_home`.
- `log list` reads Drive server logs. Keyword, username, team-folder scope,
  offset, and the Unix-seconds/`"2006-01-02 15:04:05"` time range are applied
  by Drive; the page size is bounded (default 100, maximum 1000). Drive stores
  log text as a numeric event code plus substitution fields rather than a
  rendered message, so entries surface the structured fields: time, username,
  client type, IP address, event code, path, and team folder.

Observability reads round out the overview page (WI-053):

```console
dsmctl drive admin summary --nas office
dsmctl drive admin db-usage --nas office
dsmctl drive admin top-files --nas office --ranking-by download --period-days 7 --limit 20
dsmctl drive admin activation --nas office
```

- `summary` counts active connections by client family (desktop, mobile,
  ShareSync) via `SYNO.SynologyDrive.Connection` `summary` v2.
- `db-usage` reports Drive's cached storage breakdown (version repository,
  database, Synology Office documents, all bytes) and when the cache was
  calculated (`SYNO.SynologyDrive.DBUsage` `get`); triggering a recalculation
  is deferred.
- `top-files` ranks the most accessed files from Drive's access log
  (`SYNO.SynologyDrive.Dashboard` `top_access_files`), optionally by preview
  or download activity only.
- `users` lists the accounts allowed to use Drive (`--type local|domain|ldap`)
  with whether Drive has materialized each account and DSM account context.
  **Who may use Drive is the DSM application privilege**
  (`SYNO.SDS.Drive.Application`) — grant or revoke it through the account
  module's guarded `application_privilege` change; denying it removes the
  account from this view immediately (live-verified). Drive's own
  `Privilege.set` is deliberately not exposed: a Drive-side disable does not
  stick while the application privilege still allows the account.
- `files` browses one Drive view — a team folder (`--team-folder`) or your My
  Drive — **including removed entries** (the rescue perspective; hide them
  with `--exclude-removed`), with per-node size, version count, and
  modification time. `file-versions --path <path>` lists a node's stored
  versions (time, size, content hash, storing client). Together they answer
  "what got deleted and which version do I want back"; the restore write is a
  planned follow-up (WI-057 defers `Node.Restore`).
- `activation` reports whether the package completed its online activation
  (registration against the NAS serial). An unactivated Drive still serves
  clients — verified live — so this is informational; performing the
  activation requires the Admin Console's online activation-code exchange and
  stays deferred.

MCP tools: `get_drive_admin_status`, `get_drive_admin_connections`,
`get_drive_admin_team_folders`, `get_drive_admin_logs`,
`get_drive_connection_summary`, `get_drive_db_usage`, `get_drive_top_files`,
`get_drive_activation`, `get_drive_users`.

## Team folders (guarded write)

Team-folder changes go through the standard hash-bound plan/apply contract,
bound to the observed team-folder entry and package-gated like every other
Drive operation. One plan changes one folder.

```console
echo '{"action":"enable","name":"team-data","max_versions":8,"version_policy":"smart","retention_days":0}' \
  | dsmctl drive admin team-folders plan --nas office -o enable.plan.json
dsmctl drive admin team-folders apply -f enable.plan.json --approve <hash>
```

- `enable` activates a shared folder as a team folder. `max_versions`
  (0..32) is required because DSM refuses the enable without it; 0 turns
  versioning off. While versioning is on, an explicit `version_policy`
  (`fifo` rotates the earliest version, `smart` is Intelliversioning) is
  required so the stored policy never depends on server defaults;
  `retention_days` (0..120) defaults to 0 (keep versions until rotated).
  Drive indexes the folder after enabling, which takes time and space on
  large folders.
- `disable` deactivates the team folder. **High risk and destructive**:
  Drive deletes its team-folder database including stored file versions.
  Files in the shared folder itself are not removed.
- `set_versioning` patches `max_versions`, `version_policy`, and/or
  `retention_days` on an enabled team folder; omitted fields keep their
  current values (DSM merges them server-side). Reducing kept versions,
  turning versioning off, or tightening retention prunes stored versions and
  is high risk.

The Drive home entry (`homes/mydrive*`) accepts only `set_versioning`: it
patches the global My Drive versioning DSM fans out to **every user home**,
so those plans are always high risk. Enabling or disabling the home entry is
rejected (My Drive follows the DSM home service), and the `surveillance`
share is rejected because Drive silently ignores it. Drive answers `Share.set` with an empty success even when
it skips an ineligible share, so apply verifies the postcondition by
re-reading the team-folder list (with bounded retries while Drive converges)
and returns an explicit not-yet-confirmed error instead of a false success.

MCP: `plan_drive_team_folder_change` and `apply_drive_team_folder_plan`; the
read-only gateway strips both.

## Connections (guarded kick)

Disconnect one client session through plan/apply, targeted by the session id
from the connections read:

```console
dsmctl drive admin connections kick --session <session_id> --nas office -o kick.plan.json
dsmctl drive admin connections apply -f kick.plan.json --approve <hash>
```

The plan binds to the observed connection entry (a session that reconnected
invalidates it), and apply verifies the session left the list. The kicked
client must authenticate again to resume syncing; files already synced stay
on the device, and dsmctl never sends Drive's remote data-wipe companion
field. The delete request shape is verified against the Drive server source;
the surrounding contract is live-verified (see WI-054 for the limits). MCP:
`plan_drive_connection_kick` / `apply_drive_connection_kick_plan`.

## Deferred operations

Index management, the end-user file API
(`SYNO.SynologyDrive.Files`), sharing links, labels, watermark/download
restrictions (Advanced Features), node locking, and ShareSync are out of
scope for this slice.

## DSM backends (verified live on Drive 4.0.3-27892)

API names, versions, request shapes, and response fields were verified against
the configured lab NAS (read-only) with Synology Drive Server **4.0.3-27892**
installed, guided by `SYNO.API.Info` discovery, the package's own Admin
Console assets, and the Drive server source's WebAPI registry
(`synosyncfolder` `server/ui-web/webapi/admin-console/SYNO.SynologyDrive.py`
and `handlers/log/list.cpp`, whose release branches confirm the per-package-
version API surface):

- Status: `SYNO.SynologyDrive` `get_status` v1. The service state is
  `enable_status`; QuickConnect relay fields stay unmodeled.
- Connections: `SYNO.SynologyDrive.Connection` `list` v1 (target advertises
  v1-2; the v1 shape is the verified baseline).
- Team folders: `SYNO.SynologyDrive.Share` `list` v1. The request is rejected
  (error 120) without paging and a valid sort column, so the backend always
  sends `offset`/`limit` with `sort_by: share_name`. Items expose `share_name`,
  the `share_enable` activation flag, `share_status`, `share_type`, and — for
  enabled entries — `rotate_cnt`/`rotate_policy`/`rotate_days` (reported as
  `"-"` otherwise).
- Team-folder set: `SYNO.SynologyDrive.Share` `set` v1 (POST, admin-only; the
  target also advertises v2 with identical parameters). The `share` parameter
  is a JSON array of per-share objects; dsmctl sends exactly one. An entry
  with `share_enable` routes to the enable/disable path (enable requires
  `rotate_cnt`; disable removes Drive's view database), and an entry without
  it is a versioning-only patch merged from the stored settings. Confirmed in
  the Drive server source (`handlers/share/set.cpp`, `handlers/share/list.cpp`
  on the 4.0/4.1 release branches) and live-verified on the lab target.
- Logs: `SYNO.SynologyDrive.Log` `list` v1. `target` is required: the
  all-scopes view is `share_type: all` with `target: user`, and one team
  folder is `share_type: share` with an `@`-prefixed shared-folder name.
  `log_type` is Drive's numeric event-code array filter (sent empty), and
  `keyword`, `username`, `offset`, `limit`, `datefrom`, and `dateto` are
  applied by Drive. Entries are template-coded (numeric `type` plus `s*`/`p*`
  substitution slots).

Every variant additionally requires `SynologyDrive >= 3.0` through the
package-version matcher (see
[the compatibility guide](compatibility.md#package-scoped-operations)).
Response decoders are defensive: a malformed envelope or an unrecognized list
shape returns an explicit decode error naming the available fields instead of
silently returning an empty state — this is exactly how the initial field
assumptions were corrected during live verification. Confirm the selected
backends on any target with `dsmctl drive admin capabilities`.
