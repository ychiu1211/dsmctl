---
id: WI-082
title: Local (offline) package .spk install
status: in_progress
owner: ""
priority: P2
depends_on: [WI-019, WI-029]
parallel_group: C
touches:
  - internal/synology/operations/packagecenter/localinstall.go
  - internal/synology/operations/packagecenter/localinstall_test.go
  - internal/synology/packagecenter.go
  - internal/synology/filestation_transport.go
  - internal/synology/package_upload_test.go
  - internal/application/package_center.go
  - internal/application/package_center_test.go
  - internal/cli/package.go
  - internal/mcpserver/server.go
  - internal/mcpserver/read_only.go
  - internal/mcpserver/server_test.go
  - docs/package-center.md
---

# WI-082 — Local (offline) package .spk install

## Outcome

A CLI user (and, where the gateway allows writes, an MCP agent) can install a
locally supplied `.spk` file into Package Center without the NAS reaching the
online catalog — complementing the online install/update path shipped in
[WI-029](WI-029-package-install-update.md).

## Scope

- Manual local-file install flow: `SYNO.Core.Package.Installation` `upload`
  (multipart, reusing the streaming transport) → `install` keyed by the returned
  `task_id`/`path` (not a catalog `url`), then reuse the existing online
  install/status polling machinery. See [[dsm-manual-spk-install-api]].
- New operation `internal/synology/operations/packagecenter/localinstall.go`
  (+ `localinstall_test.go`), client + application + CLI + MCP wiring, and a
  `docs/package-center.md` update.
- The upload reuses the existing streaming multipart transport that backs the
  FileStation upload (WI-049), extracted as a shared `doMultipartUpload` in
  `internal/synology/filestation_transport.go`, rather than adding a second
  multipart helper to `client.go`. The client.go multipart helper the early WIP
  drafted was dropped in favor of this reuse.

## Non-goals

- Online catalog install/update (already shipped as WI-029).
- Package trust-level / signature-policy changes.

## Design constraints

- Guarded, hash-bound plan/apply like every package mutation; the uploaded `.spk`
  is a local file reference, and install remains high risk (runs package code).
- Reuses the WI-029 install/status polling rather than duplicating it.

## Acceptance criteria

- [x] Local `.spk` install via `upload` → `install` keyed by the returned
      `task_id` (falling back to `path`), reusing the WI-029 install-status +
      inventory-confirmation machinery (`awaitPackageInstalledLocked`, now shared
      by the online and local paths). An already-installed package is upgraded and
      confirmed against the uploaded package's version.
- [x] Hash-bound plan/apply bound to the exact `.spk` content (byte size +
      SHA-256); apply refuses a file that changed after planning. CLI
      (`package install --spk`) and MCP (`plan_package_local_install` /
      `apply_package_local_install_plan`).
- [x] The local-install `plan_`/`apply_` MCP tools are stripped from the
      read-only gateway alongside the other package plan/apply tools (guarded by
      `TestNewReadOnlyOmitsPlanAndApplyTools`).
- [ ] Live-verified on the DSM lab (install of a throwaway `.spk` with a revert).
      **Deferred**: a live install runs package code on the NAS and requires
      explicit user authorization, so it is not performed here — see Handoff.

## Verification

- `go build ./...`, `go vet ./...`, `go test ./... -count=1` all green.
- Request-capture / fake-transport unit tests cover the upload multipart field
  order and file part (`TestPackageUploadMultipartContract`), the
  `task_id`-vs-`path` install request contract and install/upgrade method
  selection (`TestLocalInstallRequestContract`), the upload-cleanup contract
  (`TestUploadCleanupContract`), the upload response decode
  (`TestDecodeUploadResult`), and the hash-bound plan/apply file binding
  (`TestPackageLocalInstallPlanApplyBindsFileContent`).
- Still pending: live install of a throwaway `.spk` on the DSM lab with a revert
  (requires explicit authorization).

## Coordination

- Sibling of [WI-029](WI-029-package-install-update.md); parallel group C. Shares
  `internal/synology/client.go` multipart transport and the Package Center
  application/CLI surface — coordinate with any concurrent Package Center change.

## Handoff

Implemented and committed on `claude/spk-install-cli-ed5ef5`, rebased onto the
current `origin/main`. The early WIP (based on the WI-039 era) was brought up to
date: conflicts in `client.go`, `packagecenter.go`, `package_center.go`,
`package_center_test.go`, and the docs were resolved by keeping main's
infrastructure (WI-060's `HTTPError`/retry loop; the online install + guarded
update) and re-applying the local-install additions on top. The WIP's duplicate
in-memory multipart helper in `client.go` was dropped; the upload now reuses the
WI-049 streaming transport (`doMultipartUpload`). `awaitPackageInstalledLocked`
was generalized with an `expectVersion` parameter so the online and local paths
share one poll-and-confirm loop.

**Remaining before this can be marked done:** a live install of a throwaway
`.spk` on the DSM lab, with a revert. That runs package code on the NAS and is a
guarded mutation, so it needs explicit user authorization and was intentionally
not performed. Everything else (build/vet/test, unit + request-capture coverage,
read-only gateway stripping) is complete.
