---
id: WI-045
title: Define the power-user MCP connection and identity design
status: done
priority: P1
owner: ""
depends_on: [WI-038]
parallel_group: G
touches:
  - spec/README.md
  - spec/mcp-power-user-connection-design.md
  - spec/roadmap.md
---

# WI-045 – Define the power-user MCP connection and identity design

## Outcome

The product has one explicit design for connecting trusted power-user MCP
clients to a private LAN/VPN deployment. The design records the larger default
scope preset, explains what identity a bearer token does and does not prove,
and turns the current connection and interoperability deficiencies into a
prioritized implementation backlog.

## Scope

- State the private, single-owner, power-user product assumptions.
- Define recommended permission presets and select the default preset.
- Define Agent/MCP Host interception for actual apply execution without
  confusing client-side consent with server-side authorization.
- Specify the end-to-end Admin UI flow from NAS enrollment through the first
  successful MCP request.
- Distinguish Gateway administrator, MCP client credential, and downstream DSM
  execution identity.
- Inventory current UI, endpoint, configuration, audit, documentation, and
  standards-interoperability gaps with priorities and acceptance direction.
- Preserve the existing plan/apply, high-risk approval, NAS allowlist, secret
  storage, and deployment trust boundaries.

## Non-goals

- No runtime, UI, token-schema, permission-default, deployment, or MCP protocol
  implementation in this item.
- No public SaaS, multi-tenant, organization-role, or per-user DSM credential
  design.
- No claim that OAuth or OIDC is already implemented.

## Acceptance criteria

- [x] The design names Full access as the default power-user preset, includes
      every supported MCP scope, and explains each one's consequence.
- [x] The design requires the Agent/MCP Host to ask before every `apply_*`
      call and preserves the server as the authoritative policy boundary.
- [x] The current possession-based token identity and all three identity
      boundaries are explicit.
- [x] The intended connection wizard includes an externally correct MCP URL,
      client-specific configuration handoff, one-time secret handling, and a
      first-use verification state.
- [x] Every observed gap has a stable ID, priority, current evidence, intended
      correction, and completion signal.
- [x] OAuth interoperability is separated from the near-term private PAT flow
      and grounded in the current official MCP authorization model.
- [x] Existing security invariants that a larger default scope must not weaken
      are enumerated.

## Verification

- Cross-check the design against `spec/gateway-deployment.md`,
  `docs/gateway.md`, `docs/synology-package.md`, the embedded Admin UI, gateway
  authentication middleware, and persistent token principal construction.
- Check every local file link and every official MCP reference in the design.
- Run `git diff --check`.

## Coordination

WI-017 continues distribution certification in the same parallel group. This
item edits only specifications and does not change its packaging or runtime
surface. A later Admin UI implementation item will overlap WI-017-facing
documentation and must coordinate before starting.

## Completion notes

- Added `spec/mcp-power-user-connection-design.md` and linked it from the spec
  index. The accepted default is Full access (`nas.read`, `nas.plan`,
  `nas.apply`, and `lan.discover`) over an explicitly reviewed NAS allowlist.
- Paired broad credential authority with Agent/MCP Host execution consent:
  every `apply_*` is configured to Always ask and display its intended action,
  while server-side plan/apply validation and separate high-risk Admin UI
  approval remain authoritative.
- Specified the complete connection wizard, prefix-correct absolute endpoint,
  client-specific configuration handoff, one-time secret behavior, real
  first-use state, 365-day default lifetime, and advanced no-expiry option.
- Defined the independent Gateway administrator, possession-based MCP client,
  and downstream DSM execution identities, including exactly what “this is
  me” can and cannot mean in the single-owner release.
- Recorded thirteen P0/P1 gaps covering connection handoff, defaults,
  Agent-side execution consent, identity, lifecycle, standards compatibility,
  documentation, and integration tests, each with evidence, correction, and a
  completion signal.
- Kept manual client access tokens as the near-term private power-user path and
  separated a future standards-based OAuth resource/authorization-server
  track, referenced to the official MCP 2025-11-25 specification.
- Verified every repository-relative link resolves and `git diff --check`
  passes for the WI-045 files. No runtime behavior, server process, token, or
  NAS state was changed.

## Handoff

Fill this only when pausing incomplete work.
