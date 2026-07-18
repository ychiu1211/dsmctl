---
id: WI-016
title: Enforce remote authorization, approval, and audit
status: done
priority: P0
owner: "gateway-policy"
depends_on: [WI-014, WI-015]
parallel_group: F
touches:
  - internal/gateway
  - internal/application
  - internal/mcpserver
  - cmd/dsmctl-gateway
  - docs/gateway.md
---

# WI-016 - Enforce remote authorization, approval, and audit

## Outcome

Every remote MCP request has an authenticated caller, an explicit NAS
allowlist and scope decision, and a secret-free audit record. Read-only tokens
cannot plan or apply. High-risk remote applies execute only after a DSM or
local gateway administrator creates a matching, short-lived, single-use
approval outside the MCP conversation.

## Scope

- Create, list metadata for, revoke, expire, and rotate random high-entropy MCP
  bearer tokens; store only token digests.
- Add per-token NAS allowlists and independent `nas.read`, `nas.plan`,
  `nas.apply`, and `nas.admin` scopes. New tokens default to read-only.
- Authenticate before MCP session handling, attach a stable gateway principal
  to context, and rate-limit by token identity.
- Filter `list_nas`, credential status, fleet summaries, and error details by
  the caller's target allowlist.
- Enforce tool/use-case policy before application execution and recheck remote
  apply admission at the gateway application boundary.
- Add approval records bound to plan hash, NAS profile and revision, requesting
  token, approving administrator identity, creation/expiry times, and single-use
  state. Default lifetime is 10 minutes.
- Atomically consume approval when admitting a high-risk apply. A consumed
  approval is never restored after DSM, stale-state, or postcondition failure.
- Add immutable audit events for admin and MCP activity, plus bounded retention
  and query/export of redacted records.
- Preserve local CLI and stdio plan/apply behavior; remote policy is additive
  and may not weaken existing hash, precondition, protected-resource, or
  postcondition checks.

## Non-goals

- OIDC/OAuth authorization-server discovery, refresh tokens, dynamic client
  registration, multi-owner tenancy, or organization roles.
- Approval through an MCP tool, chat message, returned plan hash, or a caller
  controlled header.
- Batch/fan-out mutation or one approval covering multiple NAS profiles.
- Recording raw DSM requests/responses, secrets, authorization headers,
  cookies, SynoTokens, SIDs, OTPs, or encrypted payloads.

## Design constraints

- Follow `spec/gateway-deployment.md` and the repository mutation-safety
  contract. Remote authorization supplements plan/apply; it does not replace
  canonical plans, stable IDs, current-state fingerprints, or verification.
- Tool annotations are not authorization. Denial must occur in enforceable
  gateway policy using the authenticated principal.
- Plan authorization and apply authorization are evaluated separately. A token
  losing scope, target access, or validity after planning cannot apply.
- Approval consumption and apply admission are atomic with respect to retries.
  Duplicate HTTP/MCP delivery cannot execute the same approval twice.
- Audit failure for a mutating request fails the operation closed. Read-only
  audit backpressure follows an explicit bounded policy and never leaks data.
- Error messages reveal no hidden profile names or target metadata outside the
  caller's allowlist.

## Acceptance criteria

- [x] Missing, malformed, expired, and revoked credentials are rejected before
      MCP initialization or tool execution.
- [x] A new token can read only its allowlisted NAS profiles and cannot call
      plan, apply, or admin use cases.
- [x] Plan and apply scopes can be granted independently and are re-evaluated
      on every request.
- [x] A high-risk apply without an exact unexpired approval is denied before
      any DSM mutation method can run.
- [x] Approval is bound to plan hash, NAS profile revision, requesting token,
      and administrator; mismatches, expiry, replay, and concurrent duplicates
      fail closed.
- [x] A failed/stale/postcondition apply still consumes its admitted approval.
- [x] Medium-risk remote applies follow the documented scope policy and retain
      all existing plan/hash/precondition checks.
- [x] Audit events cover token lifecycle, profile/credential administration,
      plan, approval, apply, denial, and outcome with correlation IDs.
- [x] Automated secret-canary tests prove that audit/log/error/MCP output omit
      passwords, OTPs, trusted-device IDs, bearer tokens, cookies, SynoTokens,
      SIDs, master keys, and ciphertext payloads.
- [x] Local CLI and stdio plan/apply tests retain their existing behavior.

## Verification

- `go test ./... -count=1` and `go vet ./...`.
- Authorization table tests cover every MCP tool and scope combination.
- Concurrency tests race duplicate applies against one approval and assert that
  the fake mutation backend executes at most once.
- Audit tests simulate storage failure and retention cleanup.
- All apply verification uses fakes and captured requests. No live disruptive
  DSM mutation is authorized by this work item.

## Coordination

Depends on WI-014 transport identity plumbing and WI-015 persistent state. It
touches high-contention MCP/application files; begin only after coordinating
with active management work such as WI-013.

## Handoff

Fill this only when pausing incomplete work.

## Completion notes

- Gateway state schema 2 stores MCP token metadata plus SHA-256 digests only,
  approval records, and an immutable closed-schema audit stream. Audit
  retention is bounded to 10,000 events and 30 days; schema-1 migration creates
  and verifies a pre-migration backup.
- Managed HTTP authentication runs before MCP initialization, attaches a stable
  token principal, and rate-limits each token identity to 120 requests per
  minute. All 49 MCP tools are classified and filtered by independent
  `nas.read`, `nas.plan`, `nas.apply`, and `nas.admin` scopes.
- NAS allowlists are enforced before application execution. The application
  apply boundary then rechecks active token state, scope, allowlist, profile
  revision, and risk. High-risk approvals are atomically consumed with the
  mandatory admission audit before application precondition reads; low- and
  medium-risk applies require `nas.apply` and keep every existing canonical
  plan/hash/stable-ID/precondition/postcondition guard.
- `/admin/` and its API manage token create/list/rotate/revoke/expiry,
  out-of-band ten-minute approvals, and redacted audit query/JSONL export.
  Mutating admin requests persist a start audit record before executing and
  fail closed when audit storage is unavailable.
- `go test ./... -count=1` and `go vet ./...` pass. Tests cover every MCP tool
  authorization class, hidden-target filtering, expired/revoked tokens,
  duplicate approval races, failed-apply consumption, audit failure/retention,
  schema migration, and secret canaries. No live DSM mutation was run.
- Docker Desktop built and exercised `dsmctl-gateway:wi016` as
  `linux/amd64`, non-root `10001:10001`. Bootstrap, profile/token persistence,
  pre-initialize denial, authenticated initialize, restart without bootstrap,
  and absence of plaintext tokens in `gateway.db` all passed. Image ID:
  `sha256:91e12501ac4dc935fa4a4455ae3eb105079a6a4639ededa4664d1d18febcc6a5`.
