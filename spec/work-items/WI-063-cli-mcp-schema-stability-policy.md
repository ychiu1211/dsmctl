---
id: WI-063
title: CLI/MCP schema-stability and release policy
status: proposed
priority: P1
owner: ""
depends_on:
  - WI-044
parallel_group: E
touches:
  - CHANGELOG.md
  - docs/stability.md
  - README.md
  - internal/mcpserver
  - internal/mcpserver/testdata
  - cmd/dsmctl
  - .github/workflows/ci.yml
  - spec/roadmap.md
---

# WI-063 — CLI/MCP schema-stability and release policy

## Outcome

A third-party integrator can build against dsmctl's CLI command/flag surface and
MCP tool schemas and know, from a written policy and an enforced changelog, when
those surfaces change and how long a deprecated surface remains before removal.
The policy is the milestone M4 "operational standard": it states explicitly what
is guaranteed, what is experimental, and how a breaking change is announced —
given that the WI-044 release number (`DSM_train-build`) tracks the certified DSM
feature train and therefore cannot by itself signal a CLI/MCP schema break.

## Scope

- A stability policy document (`docs/stability.md`) that defines:
  - The covered surface set: MCP tool names, MCP tool input/output field names
    and types, CLI command names, CLI flag names, and the `--json` output
    contract. (Human-readable CLI prose and internal package APIs are named as
    out of scope.)
  - Stability tiers: `stable` (change only through the deprecation process) and
    `experimental` (may change within a release with a changelog note).
  - The deprecation process: announce in the changelog, keep the old surface for
    the compatibility window, and emit a deprecation notice when the deprecated
    surface is used, before removal.
  - How the WI-044 DSM-train release number relates to schema stability, and the
    chosen mechanism (D2 below) for signalling a breaking change.
- A `CHANGELOG.md` following Keep a Changelog, with an `Added` / `Changed` /
  `Deprecated` / `Removed` / `Fixed` grouping and an explicit `Breaking` callout
  for any covered-surface break, seeded with the current `7.3.2-1` release.
- A machine-checkable drift guard: a Go test that enumerates the registered MCP
  tools (name + input schema) into a committed golden file and fails when the
  set drifts, so a schema change cannot merge without regenerating the golden
  and adding a changelog entry. An equivalent golden over the CLI command/flag
  inventory (walking the Cobra command tree).
- A CI step that runs the drift guard and (advisory) checks that a diff touching
  `internal/mcpserver` tool registration or `cmd/dsmctl` command/flag definitions
  also updates `CHANGELOG.md`.
- Contributor guidance in `spec/agent-workflow.md` (or the README release
  section) linking the deprecation process to the drift guard.

## Non-goals

- Changing the release-version number scheme or its parsing/reporting; WI-044
  owns `DSM_MAJOR.DSM_MINOR.DSM_PATCH-DSMCTL_BUILD` and is done.
- Changing packaging artifacts, the amd64 image/SPK, or the DSM support matrix;
  WI-017 owns those.
- Structured DSM error taxonomy, observability/metrics, and the CI test matrix —
  the other WI-010 themes, tracked separately.
- Runtime schema negotiation or versioned MCP endpoints. This item guarantees
  and documents stability; it does not serve multiple schema versions at once.
- A frozen guarantee over every existing tool. Newly landed and clearly
  in-flux surfaces may be marked `experimental` rather than forced to `stable`.

## Design constraints

- The golden files derive from the same tool registration the server uses
  (`internal/mcpserver`) and the same Cobra tree the CLI uses (`cmd/dsmctl`); the
  guard must not maintain a hand-written parallel list that can silently diverge.
- The drift guard captures field names and types but must not embed secrets or
  environment-specific values; secrets never appear in tool arguments per the
  architecture secrets contract, so the schema is safe to commit.
- The read-only gateway strips write tools (WI-014/016); the MCP golden must be
  taken over the full stdio tool set so gateway filtering is a documented subset,
  not a source of spurious drift.
- The policy states a support scope, not a certification claim: it must not imply
  an operation works on any DSM build beyond what its own work item verified.

## Product decisions required (blocking `ready`)

- **D1 — compatibility window. RESOLVED (2026-07-20): no fixed removal window.**
  The **operation is the stable abstraction**; DSM-version differences are absorbed
  by per-variant version ranges (the WI-044 model — e.g. `op X`: 7.1/7.2 → impl a,
  7.3 → impl b). Support is unbounded forward/backward: an operation is not aged
  out on a timer — its abstract surface persists while its variant set is extended.
  A DSM version with no matching variant is reported **out-of-range (fail-closed)**
  in the capability report, never silently broken or removed. (Original question:
  the minimum time / number of trains a `stable` surface must remain after
  deprecation before removal — moot under this model; removal is a deliberate,
  changelog-signalled act, not a schedule.)
- **D2 — breaking-change signal.** Because the release number is DSM-train-derived
  and cannot act as a semver major, decide the signal: (a) add an independent
  monotonic schema/contract version surfaced in `--version` and
  `get_capabilities` and bumped on any covered-surface break, or (b) rely on the
  changelog `Breaking` section plus the deprecation window with no numeric
  signal.
- **D3 — coverage boundary.** Confirm exactly which surfaces are `stable`:
  whether the plan JSON structure and plan-hash inputs, capability report shapes,
  and exit codes are covered, and whether only `--json` output is guaranteed
  while human-readable text is not.

## Acceptance criteria

- [ ] `docs/stability.md` exists and states the covered surface set, the
      `stable`/`experimental` tiers, the deprecation process, the resolved D1
      window, and the resolved D2 breaking-change signal.
- [ ] `CHANGELOG.md` exists, follows Keep a Changelog grouping with an explicit
      `Breaking` callout, and has a populated `7.3.2-1` entry.
- [ ] A Go test enumerates registered MCP tools into a committed golden and fails
      when a tool is added, removed, renamed, or an input field name/type
      changes without the golden being regenerated.
- [ ] A Go test walks the CLI command/flag tree into a committed golden and fails
      on command/flag drift without regeneration.
- [ ] If D2 resolves to (a): `--version` and `get_capabilities` report the schema
      version, and a test asserts it is present and well-formed.
- [ ] CI runs the drift guards; a diff that changes MCP tool registration or CLI
      commands/flags without touching `CHANGELOG.md` is flagged.
- [ ] `go test ./...` and `go vet ./...` pass; the drift goldens regenerate
      deterministically (documented regenerate command, stable ordering).
- [ ] The roadmap row and README release section link to the stability policy.

## Verification

- `go test ./... -count=1` including the two new golden tests.
- `go vet ./...`.
- Regenerate goldens (documented `-update` flag or make target), confirm no diff
  on a clean tree, then confirm a deliberate throwaway tool/flag rename makes the
  guard fail.
- Build and run `--version` and, if D2(a) is chosen, inspect the schema version
  in `get_capabilities` output.
- No live DSM call or mutation is required; all verification is local.

## Coordination

- Depends on WI-044 (done) for the release-number policy this builds on.
- Overlaps WI-017 (in_progress) only in that a release must satisfy the changelog
  and deprecation obligations before shipping; it does not touch the image, SPK,
  or support matrix files.
- Sibling WI-010 themes (structured DSM errors, observability, CI matrix) are
  decomposed separately; keep the `Changed`/`Fixed` changelog groups available
  for them but do not implement them here.
- Touches `internal/mcpserver` tool registration and `cmd/dsmctl` — coordinate
  with any in-flight module additions (group C) so the golden is regenerated
  once at merge rather than fought over.

## Handoff

Fill this only when pausing incomplete work.
