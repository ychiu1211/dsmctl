# dsmctl specifications

This directory is the coordination source of truth for planned work. It is
intentionally separate from `docs/`: specs describe work that is not complete,
while docs describe behavior that users can rely on today.

## Current baseline

The following slices are implemented and should be treated as existing
contracts unless a work item explicitly changes them:

- Multiple named NAS profiles with independent sessions.
- Password, DSM OTP, and trusted-device authentication without secrets in
  configuration or MCP inputs.
- Operation-scoped DSM compatibility routing.
- System information and read-only disk, storage-pool, RAID, and volume state.
- Guarded storage-pool and volume create/expand/delete through a shared
  hash-bound plan/apply contract.
- Guarded local user and group CRUD.
- Guarded memberships, user/group quotas, and application privileges.
- Guarded shared-folder CRUD and normalized user/group permissions.
- One shared application layer and WebAPI facade used by both CLI and MCP.
- Hash-bound plan/apply with current-state preconditions and postcondition
  verification for existing mutations.
- A managed remote MCP Server gateway: persistent encrypted NAS profiles, a
  local one-hour-setup administrator with browser sessions, scoped MCP bearer
  tokens with NAS allowlists, out-of-band single-use high-risk approvals, and
  bounded redacted audit (WI-014/015/016/032/033/035/037).

## How to navigate

- [roadmap.md](roadmap.md): priority, dependency graph, and backlog status.
- [architecture-contracts.md](architecture-contracts.md): invariants every
  implementation must preserve.
- [gateway-deployment.md](gateway-deployment.md): approved architecture and
  delivery plan for the portable amd64 gateway, generic Linux container, and
  Synology x86_64 package.
- [mcp-power-user-connection-design.md](mcp-power-user-connection-design.md):
  accepted private power-user defaults, client identity semantics, target
  connection flow, and the prioritized implementation gap register.
- [gateway-connect-security-review.md](gateway-connect-security-review.md):
  2026-07-22 adversarial review of the remote-MCP connect surface — verdict,
  the connect-surface properties that must be preserved, and the gaps that
  became WI-096..099.
- [agent-workflow.md](agent-workflow.md): how agents claim, update, and hand off
  work without colliding.
- [work-items](work-items): executable specs with scope and acceptance criteria.
- [work-item template](work-items/_template.md): required format for new items.

## Status vocabulary

| Status | Meaning |
| --- | --- |
| `proposed` | Direction exists, but decisions or dependencies are incomplete. |
| `ready` | An agent can implement it without a product decision. |
| `in_progress` | Claimed by one owner in the work-item metadata. |
| `blocked` | Cannot progress until the named dependency or decision is resolved. |
| `done` | Acceptance criteria pass and user-facing docs are updated. |

The roadmap table and the metadata in each work-item file must agree. When they
do not, the individual work item is authoritative and the roadmap should be
repaired in the same change.

## Safety default

Read-only API discovery and inventory checks may run against an explicitly
configured test NAS. Account/share live mutations may only use unique
`dsmctl-e2e-*` resources and must verify UID, GID, or UUID before cleanup.

Storage-pool, volume, SAN target/mapping, encrypted-share, WORM, network,
firewall, and other potentially disruptive mutations must not be tested on a
live NAS without new, explicit authorization for that exact test. Unit fixtures
and request-capture tests are the default for those operations.

The configured test NAS is explicitly authorized for disposable LUN create and
delete integration tests. The test must create a uniquely named
`dsmctl-e2e-lun-*` LUN, capture its stable DSM LUN ID immediately after create,
and refuse deletion unless the current stable ID matches. It must not modify or
delete an existing LUN, target, or mapping, and must not map the temporary LUN.
