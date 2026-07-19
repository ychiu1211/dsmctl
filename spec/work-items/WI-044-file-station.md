---
id: WI-044
title: FileStation module (full read/write)
status: in_progress
priority: P1
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/filestation
  - internal/synology/operations/filestation
  - internal/synology/operations/filestationmutation
  - internal/synology/filestation.go
  - internal/synology/client.go
  - internal/synology/system.go
  - internal/runtime/manager.go
  - internal/application/file_station.go
  - internal/cli/file.go
  - internal/cli/root.go
  - internal/mcpserver/server.go
  - internal/mcpserver/read_only.go
  - docs/file-station.md
---

# WI-044 — FileStation module (full read/write)

## Outcome

A CLI user or MCP agent can browse, search, inspect, transfer, and manage the
actual files on a NAS through the core `SYNO.FileStation.*` WebAPI — not only
read-only. Reads follow the Download Station module shape; every NAS mutation
rides the hash-bound plan/apply mutation-safety contract; two new binary
transports (streaming multipart upload and streaming download) are added to the
otherwise text-only WebAPI client.

FileStation is a **core DSM surface** (discovered via `SYNO.API.Info`), so there
is no installed-package evidence or gate.

Per the owner's decision, **MCP exposes everything** (all reads, all plan/apply
mutations, and upload/download). The remote HTTP gateway stays **read-only**: all
FileStation write and transfer tools are stripped from `NewReadOnly`.

## Scope and phasing

Delivered in phases; each phase compiles, is unit-tested, and is live-verified on
the DSM 7.3 lab against unique `dsmctl-e2e-*` scratch paths (per AGENTS.md — never
touch existing user data).

1. **Reads + capabilities** — `Info.get`, `List.list_share/list/getinfo`,
   `Search.start/list/clean`, `DirSize.start/status/stop`, `MD5.start/status`,
   `VirtualFolder.list`, `CheckPermission.write`. Async reads start a task, poll
   to completion (tolerating the DSM start/status registration race), and clean
   up. CLI `file capabilities|info|shares|ls|stat|search|du|md5|virtual-folders|check-permission`;
   MCP `get_filestation_*`.
2. **Binary transport + transfer** — streaming multipart `Upload.upload`
   (plan/apply, binds the local file size+sha256) and streaming `Download.download`
   (exempt: reads the NAS, writes local disk). New `streamingClient`,
   `uploadLocked`/`downloadLocked`, `lockedExecutor` transfer methods. Optional
   `Thumb.get`.
3. **Low-risk mutations** — `CreateFolder.create`, `Rename.rename`,
   `CopyMove` copy (first async mutation), `Favorite.*` (direct, exempt).
4. **Destructive/async** — `CopyMove` move, `Delete.start/status` (high risk;
   strict child-count/size fingerprint for replay safety).
5. **Archives** — `Compress.start/status`, `Extract.start/status/list`.
6. **Sharing links + tasks** — `Sharing.create/edit/delete/clear_invalid/list`
   (create is high risk: public exposure) and `BackgroundTask.list/clear_finished`.

## Write-safety model

- Mutations extend the share plan/apply pattern (`ChangePrecondition`) to a
  multi-path `FilePrecondition` compared by fingerprint + plan hash. Apply
  re-plans, rejects stale state, performs the typed operation (polling async
  tasks to completion), and verifies the postcondition.
- Exemptions (the contract's "local-only and reversible" carve-out): **Download**
  (no NAS mutation) and **Favorites** (per-user bookmarks) are direct.
- Destructive `move`/`delete` and public `sharing-link create` are high risk;
  remote apply requires the existing single-use approval.
- Archive/sharing-link passwords use `env:NAME` credential references resolved at
  apply.

## Non-goals

- Advanced sharing-link options beyond create/edit/delete/list/clear_invalid.
- FileStation settings not exposed by the documented API.
- Recursive client-side directory upload/download orchestration (single files and
  DSM-native folder download only).

## Acceptance criteria

- [x] Phase 1: `file capabilities|info|shares|ls|stat|search|du|md5|virtual-folders|check-permission`
      (CLI) and `get_filestation_*` (MCP) return normalized state; decoders are
      tolerant (numbers-as-strings, `additional` sub-resources) and reject
      malformed shapes; async reads poll to completion.
- [x] Phase 1 live verification on DSM 7.3 lab: all seven read backends selected;
      real listings decode `real_path`/size/owner/time/perm; `du` and `md5`
      complete (start/status race tolerated); `check-permission` is non-mutating.
- [~] Phase 2: binary transport added to the WebAPI client (streaming multipart
      upload + streaming download; timeout-less streaming client preserving pinned
      TLS; no JSON body cap on downloads). **Download shipped** (CLI `file get`
      with atomic `.part` rename; MCP `get_filestation_file_content` base64/capped)
      and **live byte-verified** (NAS-side MD5 == local MD5). **Upload engine
      built + unit-tested** (multipart field order locked: parameters before the
      file part; session-retry on seekable sources); its user-facing `file put`
      lands with the plan/apply mutation infra (Phase 3) and is live-verified once
      Delete (Phase 4) enables scratch cleanup.
- [x] Phase 3–6: create/rename/copy/move/delete/compress/extract/upload and
      sharelink create/delete via plan/apply (multi-path `FilePrecondition`
      compared by fingerprint + hash; upload binds local size+sha256); favorites
      direct. CLI verb subcommands (`mkdir/rename/cp/mv/rm/compress/extract/put/
      share-link/favorite/tasks`) + generic `file plan`/`file apply --approve`.
      MCP `plan_filestation_change`/`apply_filestation_plan` (all actions incl.
      sharelinks) + `get_filestation_favorites`/`_sharing_links`/`_background_tasks`;
      the read-only gateway strips `plan_/apply_filestation_*` and
      `get_filestation_file_content`. **Live-verified end-to-end** on scratch paths:
      mkdir → upload (byte-perfect) → cp → rename → mv → compress → extract →
      recursive delete → cleanup confirmed; favorites add/list/remove; sharing-link
      create/list/delete; staleness rejection (stale plan refused).

## Verification

- Unit: decoder tolerance + malformed rejection; async poll composition;
  precondition/fingerprint/hash + staleness (phases 3+); multipart body order and
  download content-type branch (phase 2).
- `go build ./...`, `go vet ./...`, `go test ./... -count=1`.
- Live on the DSM 7.3 lab (`10.17.36.235`), reads against real shares and
  mutations against `dsmctl-e2e-*` scratch paths only, verified then cleaned up.

## Coordination

New packages under `internal/domain/filestation`,
`internal/synology/operations/filestation`, and (phases 3+)
`internal/synology/operations/filestationmutation`. Parallel group C alongside the
other Control Panel / module work. Binary transport touches
`internal/synology/client.go` and `internal/runtime/manager.go`; coordinate with
any concurrent client-core change.

## Handoff

- Phase 1 complete and live-verified on the lab (default profile `lab`, DSM 7.3,
  host `Derek_3018xs`). All reads shipped: domain models, operation variants +
  tolerant decoders, façade, application methods, CLI `file` command tree, and ten
  `get_filestation_*` MCP tools (golden tool count updated 93 → 103; the five
  non-`get_` read tools were named with the `get_` prefix so the remote read-scope
  classifier recognizes them). `pollTask` tolerates the DirSize/MD5 start→status
  "no such task" (code 599) registration race for a bounded number of early
  attempts.
- **All phases complete and live-verified on the DSM 7.3 lab** (default profile
  `lab`, host `Derek_3018xs`). Full read + write FileStation module shipped:
  reads, streaming download/upload transport, the file-tree mutation surface,
  favorites, sharing links, and background-task list, across CLI and MCP.
- Key implementation notes for future maintainers:
  - Mutations reuse the share plan/apply pattern via a multi-path
    `FilePrecondition` compared by `Fingerprint` + plan `Hash` (slices are not
    `==`-comparable). `observePath` treats a DSM getinfo **stub entry with an
    empty name** as "absent" — that is the only absence signal getinfo gives.
  - Async ops (copy/move/delete/compress/extract) start a taskid and poll
    `status` to `finished`. `pollTask` polls fast initially then relaxes and
    tolerates the code-599 start/status race. `DirSize`/`MD5` additionally
    **restart** the whole start→status sequence (`asyncRestartRounds`) because a
    trivially small target can complete and be freed before its first status read
    — `du` on a tiny folder is therefore ~5/6 reliable; larger targets are solid.
  - Binary transport (`filestation_transport.go`): `streamingClient` drops the
    30s timeout and JSON body cap while keeping pinned TLS; upload sends every
    parameter field before the file part (locked by an httptest test).
  - MCP tool count is 109 (golden test in `server_test.go`). New read tools use
    the `get_` prefix so the remote read-scope classifier (`ToolScope`) recognizes
    them; `plan_/apply_filestation_*` and `get_filestation_file_content` are
    stripped from the read-only gateway.
- Possible follow-ons (not required by this WI): `Sharing.edit`/`clear_invalid`,
  `Thumb.get`, `BackgroundTask.clear_finished`, and a synchronous `DirSize` path
  for trivially small folders.
