---
id: WI-062
title: CI test matrix, live-test gate, and DSM compatibility evidence record
status: done
priority: P1
owner: ""
depends_on: []
parallel_group: E
touches:
  - .github/workflows/ci.yml
  - docs/compatibility.md
  - docs/testing.md
  - integration
---

# WI-062 — CI test matrix, live-test gate, and DSM compatibility evidence record

## Outcome

Every push and pull request is gated by unit and request-capture tests that run
on more than one operating system, destructive live mutations can never execute
in CI, and the DSM builds and package versions each module was live-verified
against are recorded in the repository instead of only in agent memory.

## Scope

- Extend `.github/workflows/ci.yml` so `go build`, `go vet`, and `go test ./...`
  run on an OS matrix of at least `ubuntu-latest` and `windows-latest`. The full
  unit plus request-capture suite is the default gate on every trigger.
- Keep the Docker/gateway cross-compile, image build, and hardened container
  smoke test running only on Linux (they depend on Docker and `linux/amd64`).
- Add an explicit CI guard asserting the live-test environment variables
  (`DSMCTL_LIVE_NAS`, `DSMCTL_LIVE_CONFIG`, `DSMCTL_MCP_BINARY`,
  `DSMCTL_LIVE_MUTATIONS`, `DSMCTL_LIVE_SAN_MUTATIONS`) are unset, so the
  `integration/` live tests always `t.Skip` and no live or destructive mutation
  can run in CI.
- Add a `docs/testing.md` that states the default gate, the environment-variable
  contract for opt-in live/integration tests, the `dsmctl-e2e-*` unique-resource
  and stable-ID-verified cleanup rule, and the requirement that storage/SAN/
  encrypted-share/WORM/network/firewall mutations need explicit per-test
  authorization (mirroring `AGENTS.md` and the `spec/README.md` safety default).
- Add an in-repo DSM compatibility evidence record (a table in
  `docs/compatibility.md` or a file it links) listing, per module or operation
  group, the exact DSM build and any relevant package version it was
  live-verified against, and define the convention for adding a new row when an
  operation is live-verified.

## Non-goals

- Redefining the release version string or the compatibility train (owned by
  WI-044, done). This item does not touch version parsing, ordering, or the
  `X.Y.Z-N` policy.
- Changing the amd64 image, the Synology SPK, or the gateway container smoke
  test (owned by in-progress WI-017).
- Adding new live or destructive tests, or running any live NAS test in CI.
- Structured DSM error semantics, runtime observability/metrics, and packaging
  policy — separate siblings split from the WI-010 parent theme.
- Backfilling every historical live-verification result; the record ships with a
  defined format and the currently known entries, and grows as operations are
  verified.

## Design constraints

- The application/facade boundary and operation-scoped compatibility rules in
  `architecture-contracts.md` are unchanged; this item touches only CI
  configuration, test gating, and documentation.
- CI must pass with no NAS reachable: the default gate is unit and
  request-capture tests only, and the `integration/` package must continue to
  skip cleanly when the live environment variables are unset.
- OS-specific steps must be conditioned: Docker and `linux/amd64` steps run only
  on the Linux matrix leg so the Windows leg does not attempt them.
- The evidence record is documentation, not a runtime gate. It records observed
  live-verification history and must not be read by the compatibility selector
  or used to widen operation support beyond advertised APIs and verified
  release/package evidence.
- The live-test policy documentation must not weaken the existing safety
  default; it restates it in one authoritative place.

## Acceptance criteria

- [x] CI runs `go build`, `go vet`, and `go test ./...` on both `ubuntu-latest`
      and `windows-latest` via a `strategy.matrix.os` on the `test` job.
- [x] With no live environment variables set, `go test ./...` passes and every
      NAS-dependent `integration/` live test reports skipped, on both matrix
      legs; a dedicated step runs `go test ./integration -run Live -v` to prove
      it (the three NAS tests skip; the offline `TestLiveLUNCleanupCandidateFaultPath`
      unit test passes without touching a NAS).
- [x] The `Guard against live-test environment variables` step fails the run if
      any of `DSMCTL_LIVE_NAS`, `DSMCTL_LIVE_CONFIG`, `DSMCTL_MCP_BINARY`,
      `DSMCTL_LIVE_MUTATIONS`, or `DSMCTL_LIVE_SAN_MUTATIONS` is set.
- [x] The Docker cross-compile, image build, and hardened gateway smoke test
      moved verbatim into a Linux-only `gateway-image` job (content unchanged).
- [x] The shared-source version-consistency check still runs (on both legs of
      the `test` job).
- [x] `docs/testing.md` documents the default gate, the live-test
      environment-variable contract, the `dsmctl-e2e-*` unique-resource and
      stable-ID-verified cleanup rule, and the explicit-authorization requirement
      for disruptive mutations.
- [x] `docs/compatibility.md` gained a "Live-verification evidence record" table
      with nine populated rows (DSM build plus relevant package version) and a
      stated "Adding a row" convention.

## Verification

- `go test ./... -count=1` and `go vet ./...` on Linux and Windows.
- Confirm `go test ./integration -run Live -v` reports skipped with no live
  environment variables set.
- Inspect a CI run and confirm both OS legs pass and the live-env guard step
  runs; confirm Docker/gateway steps execute only on Linux.
- No live DSM call or mutation is performed by this item.

## Coordination

- `.github/workflows/ci.yml` is shared with WI-044 (done; added the
  version-consistency step) and in-progress WI-017 (owns the image/SPK and
  container smoke-test steps). Coordinate with the WI-017 owner before editing
  the workflow; keep all image/SPK/smoke-test steps unchanged and Linux-only.
- This is the CI/live-test-policy slice of the WI-010 parent theme. The
  structured-error, observability, and packaging slices are separate items;
  align the `docs/testing.md` structure with them if they land concurrently.
- `docs/compatibility.md` overlaps the compatibility documentation touched by
  WI-044; this item only appends the evidence record and does not alter the
  version-train or selector text.

## Handoff

Fill this only when pausing incomplete work.
