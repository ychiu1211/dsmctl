---
id: WI-060
title: Structured DSM error taxonomy across CLI and MCP
status: in_progress
priority: P1
owner: ""
depends_on: []
parallel_group: E
touches:
  - internal/synology/errors.go
  - internal/synology/client.go
  - internal/application/service.go
  - internal/cli/root.go
  - cmd/dsmctl/main.go
  - internal/mcpserver/server.go
  - docs/
---

# WI-060 — Structured DSM error taxonomy across CLI and MCP

## Outcome

Every failure that originates from a DSM API carries a stable, typed category so
that scripts and MCP clients can react programmatically instead of string-matching
opaque messages. The same underlying DSM error surfaces as a documented CLI exit
code with a human message, and as machine-readable MCP structured error content
with the same category string. Transient DSM failures on read-only calls are
retried with bounded backoff; mutations are never silently retried. No rendered
error ever contains a SID, SynoToken, password, or OTP.

> Status (2026-07-20): the taxonomy + CLI exit-code half of this outcome is
> **shipped**; the two clauses describing *machine-readable MCP structured error
> content with the category string* and *transient read-only retry with backoff*
> are the **deferred** follow-on target, not yet implemented
> (`CategoryTransient`/`CategoryRateLimit` are defined but not yet produced by any
> classifier, and no MCP error middleware injects the category). See the split
> Acceptance criteria and the Handoff for the exact boundary.

## Scope

- Define a closed, stable set of DSM error categories:
  `auth`, `permission`, `not-found`, `conflict`, `rate-limit`, `transient`,
  `unsupported`, `invalid-input`, and `unknown` (fallback). The string spellings
  are part of the contract and must not change without a new work item.
- Add a table-driven classifier that maps `APIError.Code` (and the existing
  session/OTP conditions already recognized in `internal/synology/errors.go`) to
  a category. Reuse, do not duplicate, the current predicates: session
  `106/107/119`, OTP challenge `403/406`, invalid OTP `404`. Classify the
  documented DSM common codes at least: `101` invalid-input, `102`/`103`
  not-found (API/method absent), `104` unsupported (version), `105`/`108`
  permission, `119` auth, `120` invalid-input; auth-domain codes `400-407` as
  `auth`/`permission` per their DSM meaning. Unknown codes fall back to
  `unknown`.
- Expose the category from the transport `APIError` (e.g. a `Category()` method)
  and via a package-level classifier `synology.Classify(err) Category` that works
  after the error has been wrapped with `%w` by the application layer.
- Preserve the category through `internal/application` wrapping. The existing
  `authenticationError` helper and any `fmt.Errorf("NAS %q: %w", ...)` wrapping
  must keep the typed error reachable via `errors.As`/`Classify`.
- CLI: replace the single hard-coded `os.Exit(1)` in `cmd/dsmctl/main.go` with a
  documented category-to-exit-code map computed by a helper in `internal/cli`.
  Distinct, stable, non-overlapping exit codes per category; `0` success; a
  generic non-DSM failure keeps a single reserved code. Document the codes in
  `docs/` and reference them from command help or the README.
- MCP: tool error returns include a machine-readable `category` field (stable
  string) plus a human message in the structured error content, so a model or
  client can branch without parsing prose. The existing `SessionExpiredError`
  and OTP guidance messages are preserved and map to `auth`.
- Retry/backoff: read-only DSM calls whose error classifies as `transient` or
  `rate-limit` are retried with bounded exponential backoff (fixed max attempts
  and total time budget, jittered). Plan/apply and any mutating call are never
  auto-retried. Retry honors context cancellation.
- Secret hygiene: the HTTP non-2xx path and every category message must route
  through the existing redaction (endpoint `Redacted()`), and never embed the
  SID, SynoToken, password, OTP, or raw request body.

## Non-goals

- Observability/structured logging, the CI test matrix, packaging, and release
  policy (the other WI-010 themes). Those remain separate items.
- Changing operation-variant selection or compatibility routing (owned by the
  DONE WI-044). This item only classifies a runtime `unsupported` error; it does
  not decide which backend to call.
- Introducing a generic raw WebAPI error passthrough or exposing DSM request
  field names in the taxonomy.
- Retrying mutations, or adding retry to interactive auth challenges.

## Design constraints

- Categories are domain-level semantics, not DSM field names, per the dependency
  and secrets contracts in `architecture-contracts.md`.
- The classifier lives in `internal/synology` (the facade), not in CLI or MCP;
  CLI and MCP remain thin adapters that only translate the category to an exit
  code or structured field.
- The category set is closed and exhaustive: adding or renaming a category is a
  breaking contract change requiring a new work item.
- Retry policy must default to safe: only demonstrably idempotent read paths are
  eligible, and the eligibility is a property of the call site, not inferred from
  the HTTP verb (all DSM calls are POST).
- Secrets rule from `architecture-contracts.md` is a hard invariant: a test must
  prove SIDs/tokens never appear in any rendered error string.

## Acceptance criteria

Core taxonomy + CLI exit codes shipped 2026-07-20 (commit on main):

- [x] A `Category` type with the nine fixed string values exists in
      `internal/synology` (`category.go`), and a unit test asserts each spelling
      and exhaustiveness.
- [x] `APIError.Category()` and `synology.Classify(err)` return the correct
      category for a table of DSM codes covering every mapped code, including
      `106/107/119` (session→auth), `403/406/404` (OTP→auth), `102/103`
      (not-found), `104` (unsupported), `105/402/407` (permission),
      `101/114/120` (invalid-input), and the `unknown` fallback.
- [x] `synology.Classify` returns the correct category after the error is wrapped
      with `fmt.Errorf(... %w ...)` (single and double wrap) and recognizes
      `SessionExpiredError` / `OTPRequiredError` as auth; unit-tested.
- [x] `cmd/dsmctl/main.go` exits with the documented, category-specific code for
      each category and `0` on success (`internal/cli/exitcode.go`); a test
      asserts the map is total over the taxonomy and codes are distinct, and the
      human stderr line is prefixed with the category (`FormatError`).
- [x] A unit test asserts the rendered `APIError` message carries no
      SID/SynoToken/password/OTP; the transfer-URL redaction (download/upload/
      thumbnail) is separately guarded by WI-049's `redactTransferURL` test.
- [x] `docs/errors.md` documents the category set and the exit-code table; the
      roadmap row is updated.

Deferred to a follow-on (a coherent second slice; noted in Handoff):

- [ ] Each MCP tool error result carries a stable `category` field in structured
      content — needs a central error-middleware over all ~146 tool handlers, not
      146 per-tool edits.
- [ ] HTTP-level transient/rate-limit typing (timeouts, 5xx, resets, 429) and
      bounded retry of read-only calls with backoff — needs HTTP-error
      classification in the request path plus call-site retry-eligibility
      threading (no current DSM app-code maps to transient/rate-limit, so retry
      is inert until HTTP errors are typed).

## Verification

- `go test ./internal/synology/... ./internal/application/... ./internal/cli/... ./internal/mcpserver/...`
  and `go vet ./...` before handoff (Go toolchain at `C:\Program Files\Go\bin`).
- All new behavior is exercised with unit fixtures / fake transports; no live
  NAS mutation is required. Read-only classification may optionally be spot-checked
  against the lab NAS by inducing a known error (e.g. a not-found path), but this
  is not required and must not mutate state.
- Retry timing tests must use an injectable clock or a very small backoff budget
  so the suite stays fast and deterministic.

## Coordination

- `internal/application/service.go`, `internal/mcpserver/server.go`, and
  `internal/cli/root.go` are high-contention files (see `agent-workflow.md`);
  coordinate before editing if another item is active on them. The change to
  each is additive (wrapping-preservation, a structured field, an exit-code
  helper) and should be a small, isolated diff.
- Prefer a prerequisite commit that introduces the `Category` type and classifier
  in `internal/synology` before the CLI/MCP adapter wiring, so parallel items can
  build on the taxonomy without cherry-picking adapter changes.
- Do not restate or contradict WI-044's compatibility-selection semantics; only
  classify the runtime `unsupported` error.

## Handoff

2026-07-20: the taxonomy foundation and CLI exit-code surface shipped
(`internal/synology/category.go`, `internal/cli/exitcode.go`,
`cmd/dsmctl/main.go`, `docs/errors.md`, with `category_test.go` and
`exitcode_test.go`). Two pieces remain, deliberately split into a follow-on
because each needs a design decision rather than more of the same wiring:

1. **MCP structured `category` field.** The SDK turns a handler's returned Go
   error into an error result with just the message. Surfacing the category on
   every tool needs one central wrapper around `mcp.AddTool` (or an interceptor)
   so all ~146 tools gain the field uniformly — not 146 edits. `synology.Classify`
   is ready to supply the value.
2. **Transient retry.** No DSM app-code currently classifies as `transient` or
   `rate-limit`; those only arise from HTTP-level failures (timeout, 5xx, reset,
   429) that the request path does not yet type. The retry loop is only
   meaningful once `requestLocked` classifies those into `CategoryTransient` /
   `CategoryRateLimit`, and the retry must be gated on a call-site read-only flag
   (never the HTTP verb — all DSM calls are POST). Use an injectable clock.
