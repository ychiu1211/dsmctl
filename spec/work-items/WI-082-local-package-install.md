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
  - internal/synology/packagecenter.go
  - internal/synology/client.go
  - internal/application/package_center.go
  - internal/cli/package.go
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
  (+ `localinstall_test.go`), client + application + CLI wiring, and a
  `docs/package-center.md` update.

> This is a **tracking stub**. The implementation is being authored as
> **uncommitted WIP** on branch `claude/spk-install-cli-ed5ef5` (checked out in
> the `weblogin-protocol-exploration-cfd633` worktree): untracked
> `localinstall.go` + `_test.go` plus edits to `package_center.go`,
> `package.go`, `client.go`, `packagecenter.go`, and the docs. Fold this stub
> into the fuller spec when it is committed.

## Non-goals

- Online catalog install/update (already shipped as WI-029).
- Package trust-level / signature-policy changes.

## Design constraints

- Guarded, hash-bound plan/apply like every package mutation; the uploaded `.spk`
  is a local file reference, and install remains high risk (runs package code).
- Reuses the WI-029 install/status polling rather than duplicating it.

## Acceptance criteria

- [ ] Local `.spk` install via `upload` → `install` with `task_id`/`path`,
      reusing the WI-029 status machinery; CLI + MCP; live-verified on the lab.
- [ ] `plan_/apply_package_*` (local-install variant) excluded appropriately from
      the read-only gateway.

## Verification

- `go test ./... -count=1`, `go vet ./...`; live install of a throwaway `.spk`
  on the DSM lab with a revert.

## Coordination

- Sibling of [WI-029](WI-029-package-install-update.md); parallel group C. Shares
  `internal/synology/client.go` multipart transport and the Package Center
  application/CLI surface — coordinate with any concurrent Package Center change.

## Handoff

Genuinely in progress as uncommitted WIP on `claude/spk-install-cli-ed5ef5` as of
2026-07-20; had no WI number and no roadmap row before this item. Assign an owner,
finish, and commit under WI-082.
