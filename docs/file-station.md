# FileStation

A module for the core Synology FileStation WebAPI (`SYNO.FileStation.*`). Because
FileStation is a built-in DSM surface discovered through `SYNO.API.Info`, there is
no package to install or gate on. The module is being delivered in phases; this
document tracks what has shipped.

## Reads (shipped)

```console
dsmctl file capabilities --nas lab
dsmctl file info --nas lab
dsmctl file shares --nas lab
dsmctl file ls /home --nas lab
dsmctl file stat /home/.viminfo --nas lab --json
dsmctl file search /home --pattern "*.txt" --nas lab
dsmctl file du /home --nas lab
dsmctl file md5 /home/.viminfo --nas lab
dsmctl file virtual-folders --nas lab
dsmctl file check-permission /home --filename note.txt --nas lab
```

- **`capabilities`** reports which reads are available and the DSM API
  version-specific backend selected for each (`SYNO.FileStation.Info` v2,
  `List` v2, `Search` v2, `DirSize` v2, `MD5` v2, `VirtualFolder` v2,
  `CheckPermission` v3 on DSM 7.3).
- **`info`** reads `SYNO.FileStation.Info` (`get`): host name, manager flag,
  whether public sharing is supported, and the supported virtual mount protocols.
- **`shares`** reads `SYNO.FileStation.List` (`list_share`): the shared folders
  visible to the session, each with its path, real volume path, owner,
  timestamps, and permission summary. `--writable` limits to writable shares.
- **`ls`** reads `SYNO.FileStation.List` (`list`) for one folder, with optional
  `--pattern`, `--type file|dir`, `--sort-by`, and paging. Each entry carries its
  real volume path, size, owner, timestamps, and permission summary.
- **`stat`** reads `SYNO.FileStation.List` (`getinfo`) for one or more explicit
  paths.
- **`search`** runs `SYNO.FileStation.Search`: it starts a background search,
  polls `list` until DSM reports it finished, and cleans the task up. `--pattern`,
  `--ext`, `--type`, and `--no-recursive` refine it.
- **`du`** runs `SYNO.FileStation.DirSize` (`start`/`status`/`stop`) to compute
  aggregate size and file/directory counts.
- **`md5`** runs `SYNO.FileStation.MD5` (`start`/`status`) to compute a file's
  MD5 digest.
- **`virtual-folders`** reads `SYNO.FileStation.VirtualFolder` (`list`): mounted
  remote CIFS/NFS folders.
- **`check-permission`** runs `SYNO.FileStation.CheckPermission` (`write`), a
  **non-mutating** probe of whether the session may create a file at a path.
  Supply `--filename` to probe creating a specific file inside a folder.

The asynchronous reads (`search`, `du`, `md5`) start a task and poll it to
completion. DSM briefly reports "no such task" (code 599) between a task's `start`
and its registration; the poller tolerates that transient error for a bounded
number of early attempts.

MCP exposes the same reads through `get_filestation_capabilities`,
`get_filestation_info`, `get_filestation_shares`, `get_filestation_directory`,
`get_filestation_entry_info`, `get_filestation_search`,
`get_filestation_directory_size`, `get_filestation_md5`,
`get_filestation_virtual_folders`, and `get_filestation_write_permission`. All are
read-only.

Field shapes are live-verified on DSM 7.3.

## Download (shipped)

```console
dsmctl file get /home/report.pdf --nas lab            # -> ./report.pdf
dsmctl file get /home/report.pdf -o /tmp/r.pdf --nas lab
```

`file get` streams a file from the NAS to local disk through
`SYNO.FileStation.Download`, writing to a sibling `.part` file and renaming on
success so an interrupted transfer never leaves a partial file. Downloading reads
the NAS and writes a local file the user names; it does not mutate the NAS, so it
is not gated behind plan/apply. The download is bounded only by the caller's
context (not the 30-second timeout that fits JSON calls) and is never buffered in
memory.

MCP exposes `get_filestation_file_content`, which returns a file's content
base64-encoded (refused above an 8 MiB inline limit; stream larger files with the
CLI). It is read-only with respect to the NAS.

## Writes (shipped)

Every NAS mutation goes through the repo's hash-bound **plan/apply** contract. The
verb subcommands plan, print the plan (risk, warnings, hash), and apply in one
process when `--yes` is passed (the terminal user is the approver); the generic
`file plan` / `file apply --approve <hash>` pair supports scripting and MCP
parity.

```console
dsmctl file put ./report.pdf /home/docs --yes        # upload (guarded)
dsmctl file mkdir /home docs --yes
dsmctl file rename /home/docs/a.txt b.txt --yes
dsmctl file cp /home/docs/b.txt /home/backup --yes
dsmctl file mv /home/docs/b.txt /home/backup --yes   # high risk (removes source)
dsmctl file rm /home/backup/b.txt --yes              # high risk (permanent)
dsmctl file compress /home/out.zip /home/docs --yes
dsmctl file extract /home/out.zip /home/restored --yes
```

- Each plan binds a **multi-path fingerprint** (the observed existence, type,
  size, and mtime of every source, and the destination): apply re-plans and
  rejects a stale plan if any of that changed or a destination appeared. Upload
  additionally binds the local file's size and SHA-256.
- **Upload** streams a local file to the NAS (multipart, no memory buffering).
  **Move** and **delete** are marked high risk (delete is permanent and
  recursive — no recycle bin). Async operations (copy, move, delete, compress,
  extract) start a DSM task and are polled to completion, and the postcondition
  is verified (created paths present, moved/deleted sources absent).
- Archive and sharing-link passwords use `env:NAME` credential references
  resolved only at apply time — secrets never enter a plan, log, or MCP argument.

### Sharing links (public URLs)

```console
dsmctl file share-link create /home/report.pdf --yes   # high risk (public URL)
dsmctl file share-link list
dsmctl file share-link edit <link-id> --expire 2026-08-01 --yes
dsmctl file share-link clear-invalid --yes
dsmctl file share-link delete <link-id> --yes
```

Creating a link publishes the path to an anonymous public URL, so it is high
risk. `list` shows each link's id, path, public URL, and password protection.
`edit` changes the expiry and/or the password (`--password-ref env:NAME`) of an
existing link and verifies the new expiry after apply; `clear-invalid` removes
every expired or broken link in one guarded action. One DSM behavior the
mutations account for (live-verified): the Sharing API silently ignores bare
string parameter values — an unquoted `date_expired` leaves the link unchanged
— so every string parameter is sent as a JSON string literal.

### Favorites and background tasks

```console
dsmctl file favorite add /home/docs --name Docs   # per-user, reversible (direct)
dsmctl file favorite list
dsmctl file favorite remove /home/docs
dsmctl file tasks                                  # in-progress background tasks
```

Favorites are per-user sidebar bookmarks — reversible and local to your account,
so they are direct commands rather than plan/apply.

## MCP

MCP exposes the full surface: all reads, `get_filestation_file_content` (base64,
8 MiB cap), `get_filestation_favorites`, `get_filestation_sharing_links`,
`get_filestation_background_tasks`, and the `plan_filestation_change` /
`apply_filestation_plan` pair (create_folder, rename, copy, move, delete,
compress, extract, upload, sharelink_create, sharelink_delete). The **read-only
gateway** strips every write and the content-transfer tool. See
[WI-049](../spec/work-items/WI-049-file-station.md).
