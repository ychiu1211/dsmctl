---
id: WI-081
title: Automatic TLS trust enrollment
status: in_progress
owner: ""
priority: P2
depends_on: [WI-032, WI-048]
parallel_group: G
touches:
  - internal/tlstrust
  - internal/cli/auth.go
  - internal/gateway/admin/handler.go
  - internal/gateway/admin/ui.go
  - internal/runtime/manager.go
  - .github/workflows/ci.yml
  - docs/
---

# WI-081 — Automatic TLS trust enrollment

## Outcome

Establishing trust in a NAS server certificate (the pin dsmctl rides for its own
transport) is a guided, first-connection enrollment step rather than a manual
out-of-band fingerprint paste, for both the CLI `auth` path and the Gateway
administration UI — without weakening the existing pin-on-first-use guarantee or
silently trusting a changed leaf.

## Scope

- A dedicated `internal/tlstrust` package owning trust decisions (parse, pin,
  compare fingerprints, persist), factored out of the ad-hoc pinning currently
  spread across `internal/config`, `internal/runtime/manager.go`, and
  `internal/gateway/admin`.
- CLI `auth` enrollment wiring (`internal/cli/auth.go`, `auth_tls_test.go`).
- Gateway admin handler + UI surfacing the enrollment/trust state
  (`internal/gateway/admin/{handler,ui,handler_test}.go`).
- Runtime manager consumes the shared trust package
  (`internal/runtime/manager.go`).

> This is a **tracking stub**. The design and implementation are being authored
> as **uncommitted WIP** in the local `main` worktree
> (`C:/Users/deryc/Projects/dsmctl`, ~17 files, +488/-60). Fold this stub into
> the fuller spec drafted there when it is committed.

## Non-goals

- To be filled from the in-flight design (likely: CA-store trust, ACME, or any
  automatic trust of a *changed* fingerprint without acknowledgement).

## Design constraints

- Must preserve the pin-on-first-use / current-transport protection already in
  the architecture contract: a changed server leaf is never silently re-trusted.
- Trust material and fingerprints follow the secrets/redaction contract.

## Acceptance criteria

- [ ] To be authored from the in-flight design (`internal/tlstrust` unit tests,
      CLI enrollment test, admin-handler test all pass).

## Verification

- `go test ./... -count=1`, `go vet ./...`.

## Coordination

- **Number collision resolved here.** The in-flight work was mis-filed as
  `WI-051`, which is already shipped/`done` as the Synology Office settings
  module ([WI-051](WI-051-office-admin.md)). This item, **WI-081**, is its
  canonical number; the mis-numbered `spec/work-items/WI-051-automatic-tls-trust-enrollment.md`
  draft in the local `main` worktree must be renamed to `WI-081-...` (and its
  front-matter `status: done` corrected to `in_progress`) before it is committed.
- Overlaps the gateway/auth transport pinning code (group G); coordinate with any
  concurrent change to `internal/runtime/manager.go` and `internal/gateway/admin`.

## Handoff

Genuinely in progress as uncommitted WIP in the local `main` worktree as of
2026-07-20; not previously reflected in the roadmap. That worktree's `main`
checkout was ~33 commits behind `origin/main`, which is why it could not see
that WI-051 was already taken. Rebase onto `origin/main`, renumber to WI-081,
then commit.
