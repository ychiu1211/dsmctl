---
id: WI-080
title: Universal Search index management module
status: proposed
priority: P3
owner: ""
depends_on: [WI-019, WI-022]
parallel_group: C
touches:
  - internal/domain/universalsearch
  - internal/synology/operations/universalsearch
  - internal/synology/universalsearch.go
  - internal/runtime/manager.go
  - internal/application/universalsearch.go
  - internal/cli/universalsearch.go
  - internal/mcpserver/server.go
  - docs/universal-search.md
---

# WI-080 — Universal Search index management module

## Outcome

A CLI user or MCP agent can read the Synology Universal Search index
configuration — the list of indexed folders and the current index status — and,
through the hash-bound plan/apply contract, manage the index: add or remove an
indexed folder and trigger a re-index/rebuild. This is a focused, typed module
in the sense of [WI-006](WI-006-control-panel-modules.md) — it exposes named
index operations (list folders, index status, add folder, remove folder,
reindex), never a generic `set key=value` proxy over the Universal Search
configuration. It is package-version gated on the installed Universal Search
package exactly like the Photos (WI-030), Surveillance (WI-034), and Download
Station (WI-043) modules.

The API family, methods, field names, package id, and version baselines below
are the author's best current knowledge and **must be live-verified at
implementation time** against the lab with a throwaway `DSMCTL_DUMP` probe
before any code depends on them — the standing policy is that source-doc and
UI-derived API names are often stale (see [[dsm-webapi-live-verify-fields]]).
In particular the **API namespace itself is unconfirmed**: Universal Search is
expected to live under `SYNO.Finder.*` (the package's internal name is Finder /
`SynoFinder`), most likely `SYNO.Finder.FileIndex.*`, but it may instead be
`SYNO.Core.FileIndexing` or a mix; confirm the real namespace, the indexed-folder
method set, and the reindex entry point before slicing the operations.

## Scope

Sliced read-first, then guarded write, so the read slice can ship independently.

### Slice A — read-only (independently shippable)

- **Indexed folder list** — expected `SYNO.Finder.FileIndex.Folder` (or
  `.List`) `list`: each indexed folder's stable identifier, share/path,
  enabled state, and (if the API returns it) per-folder index status and item
  count. Decode tolerantly; a size/count returned as a quoted string is handled
  as `flexInt64` (the recurring DSM quirk).
- **Index status** — expected `SYNO.Finder.FileIndex.Status` (or a `status` /
  `get_status` method on the folder API): overall daemon/index state
  (idle / indexing / paused), progress, queued/pending item counts, and last
  index time. The precise API + shape is the first thing to live-verify.
- **Capabilities + package gating** — the module reports itself supported only
  when the Universal Search package (expected package id `SynoFinder`, version
  baseline to be confirmed) is installed at/above the tested baseline and its
  index daemon is reachable; otherwise it fails closed with package evidence
  (installed / version / running), mirroring WI-043.
- CLI reads (`universal-search capabilities|folders|status`, alias to be
  chosen — e.g. `usearch`) and the matching `get_universal_search_*` MCP read
  tools, returning normalized state.

### Slice B — guarded write (plan/apply, hash-bound)

- **Add an indexed folder** — expected `SYNO.Finder.FileIndex.Folder` `add`
  (fields — share/path, recursive/enable flags — to be live-verified). Plan
  records and hashes the full observed indexed-folder list; apply rejects a
  changed list, issues the add, and re-reads to confirm the folder is present.
- **Remove an indexed folder** — expected `SYNO.Finder.FileIndex.Folder`
  `delete` (keyed by the stable folder id). Same hash-bound plan/apply; apply
  re-reads to confirm the folder is absent. Removing a folder discards its
  accumulated search index (derived data, rebuildable by re-adding + reindex) —
  see risk classification.
- **Reindex / rebuild** — expected `SYNO.Finder.FileIndex.Reindex` or a
  `reindex` / `rebuild` method on the folder/status API, scoped either to a
  single folder or the whole index (whichever the API supports — verify). This
  is a long-running background task: the apply postcondition verifies the
  reindex was **accepted and started** (status transitions to indexing/queued),
  not that it completed.

## Non-goals

- **General Universal Search settings** — search result limits, thumbnail/preview
  behavior, which content sources are searchable (Drive/Note Station/Mail/photo
  metadata), and the search-scope preferences. This WI manages the file **index**
  (folders + status + reindex), not the search-experience configuration.
- **Running search queries** (`SYNO.Finder.Search.*` or equivalent). dsmctl
  manages the index; it is not a search client.
- **Media indexing / the media-server thumbnail index**
  (`SYNO.Core.FileIndexing` / `synoindexd` for photos/music/video), which is a
  separate subsystem from Universal Search even if the namespaces look adjacent —
  do not conflate them; confirm which daemon each API drives before wiring.
- **Indexed-folder ACL / permission changes.** Adding a folder to the index
  does not change its share permissions; this module never touches
  `SYNO.Core.Share` or file-station ACLs.
- **Pausing/resuming or throttling the index daemon** as a standing schedule,
  and any global enable/disable of Universal Search itself (that is
  package-lifecycle, covered by the package module WI-019/WI-029).

## Design constraints

- **Focused typed operations, not a settings proxy.** Each capability is a
  named index operation with its own stable operation name in the capability
  report (selected backend, API, version); there is no generic mutation command
  or MCP tool over Universal Search config (WI-006 / mutation-safety contract).
- **Package-gated, fail-closed.** Every variant matches
  `PackageVersionRange(SynoFinder, <baseline>, ∞)` plus the API version (real id
  and baseline to be confirmed live). A NAS without the package, or below
  baseline, reports the module unsupported with evidence; a NAS with the package
  installed but the index daemon not running returns an actionable
  "installed but not running" error rather than an empty successful state.
- **Per-operation backend selection.** Folder-list, status, add, remove, and
  reindex each select their own backend by advertised API/version
  (compatibility framework); a missing status API must not disable folder-list,
  and vice versa. Older package versions may omit per-folder status fields —
  degrade to the fields present, do not error the whole read.
- **Hash-bound plan/apply with postcondition re-read.** Slice-B mutations follow
  the module pattern: plan records and hashes the complete observed indexed-folder
  list (and, for reindex, the observed status), apply rejects stale state,
  performs the typed operation, and re-reads to verify the field actually took
  effect — DSM silently ignores some fields, so the postcondition is mandatory.
  Ownership is patch-only for add/remove (the folder set is mutated by one entry,
  never wholesale replaced); unspecified folders are never touched.
- **Reindex postcondition is "started", not "finished".** Because reindexing is
  a background job that can run for hours, apply cannot block on completion; it
  verifies the index status transitioned into an indexing/queued state (job
  accepted) and returns that as the postcondition. Re-run the read to observe
  progress. Idempotence: a reindex requested while already indexing must be
  reported as such, not double-queued blindly.
- **Risk classification.** No index operation changes external exposure or
  security posture, and none delete user files, so none are high on those
  grounds. Reindex is **load-heavy but not destructive** → medium (it can
  saturate CPU/IO on the NAS for an extended period — say so in the plan
  summary). Removing an indexed folder discards that folder's accumulated
  search index (derived, rebuildable) → medium, flagged as the highest-consequence
  operation with an explicit "search index for this folder will be discarded"
  note. Adding a folder → low (it only schedules new indexing work). If live
  verification shows the DSM delete also removes anything beyond the derived
  index, re-classify remove as high.
- **Secrets never enter requests, plans, hashes, logs, or MCP args.** This
  module has no first-class secret today, but indexed folders can include
  encrypted shared folders that must be mounted to index. If adding such a folder
  requires a mount/encryption key, it uses `credential_ref: env:NAME` resolved
  only at apply time — the key value never enters the request, plan, hash,
  result, or logs (secrets-and-identity contract). No folder-index response
  field is a secret, but decode conservatively and never surface tokens/SIDs.

## Acceptance criteria

- [ ] Live-verify first: the real Universal Search API namespace, the
      indexed-folder list/add/delete method set, the index-status API+shape, the
      reindex entry point, and the package id + version baseline are confirmed on
      the lab with a throwaway `DSMCTL_DUMP` probe; the spec's API guesses are
      corrected in code and docs to match what the NAS actually returns.
- [ ] Slice A: `universal-search capabilities|folders|status` (CLI) and the
      matching `get_universal_search_*` MCP tools return normalized state with
      package evidence (installed / version / running).
- [ ] Package-gating: reads and selection fail closed without the package and
      below baseline; capabilities carry installed/version/running; an
      installed-but-daemon-down NAS returns an actionable error, not empty
      success.
- [ ] Per-operation selection: folder-list and status select independently; a
      NAS missing the status API still lists folders (and vice versa); older
      versions degrade to available fields without erroring.
- [ ] Decoder + composition unit tests: folder-list entry (incl. a count/size
      returned as a quoted string), status shape, and malformed-shape rejection
      (decoders never silently return empty successful state).
- [ ] Slice A live verification on the DSM 7.3 lab: read capabilities, the
      indexed-folder list, and index status against the installed Universal
      Search package (installed via dsmctl guarded install if not present).
- [ ] Slice B — add/remove indexed folder via hash-bound plan/apply: plan
      records+hashes the full observed folder list, apply rejects stale state and
      re-reads to confirm the folder is present/absent; add classified low,
      remove classified medium with an explicit index-discard note; request-capture
      test pins the wire fields; read-only gateway excludes the plan/apply tools.
- [ ] Slice B — reindex/rebuild via hash-bound plan/apply: classified medium
      (load-heavy, non-destructive); apply postcondition verifies the reindex was
      accepted/started (status → indexing/queued), not completion; a reindex
      requested while already indexing is reported, not blindly double-queued.
- [ ] Slice B live verification on the DSM 7.3 lab (authorized, fully reverted):
      add a throwaway indexed folder → confirm present → trigger reindex →
      confirm status transition → remove the folder → confirm absent, with the
      plan hash rejecting a mid-flight folder-list change.
- [ ] Read-only gateway exclusion: `plan_universal_search_*` / `apply_universal_search_*`
      are absent from the read-only tool set; only the `get_*` reads are exposed.

## Verification

- Decoder + request-capture unit tests; `go test ./... -count=1`, `go vet ./...`.
- Live reads on the DSM 7.3 lab against the installed Universal Search package
  (install via `dsmctl package install <SynoFinder|real-id>` for this work if the
  lab does not already have it — confirm the real package id first).
- Live reverted write requires explicit per-session authorization: add/remove use
  a throwaway test share so no real indexed folder is disturbed, and any reindex
  is triggered only with permission because it loads the NAS; all mutations are
  fully reverted (folder removed, index left as found).
- Source of truth for fields: the Universal Search package's WebAPI conf +
  handlers on codesearch (search the `SynoFinder` / Finder package for
  `SYNO.Finder.FileIndex` definitions) — treated as a starting hypothesis and
  reconciled against the live `DSMCTL_DUMP` probe, never trusted blind.

## Coordination

- Package-scoped module (parallel group C) alongside Photos (WI-030),
  Surveillance (WI-034), Download Station (WI-043), and the Drive Admin modules;
  new packages under `internal/domain/universalsearch` and
  `internal/synology/operations/universalsearch`, a facade
  `internal/synology/universalsearch.go`, application layer, thin CLI, and thin
  MCP tools reusing the same application methods. No overlap with the External
  Access (WI-041) or file-services modules beyond the shared compatibility
  framework and runtime manager registration.
- Disambiguate against any future media-indexing / `SYNO.Core.FileIndexing`
  work item so the two index subsystems do not collide on operation names.
