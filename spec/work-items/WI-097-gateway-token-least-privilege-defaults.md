---
id: WI-097
title: Gateway MCP-token least-privilege defaults and lifetime cap
status: proposed
priority: P1
owner: ""
depends_on: []
parallel_group: G
touches:
  - internal/gateway/admin/ui.go
  - internal/gateway/oauth/handler.go
  - internal/gateway/state/policy.go
---

# WI-097 — Gateway MCP-token least-privilege defaults and lifetime cap

## Provenance

Design-review follow-up (2026-07-22 adversarial review, re-validated against
`cc8d160`). All three observed behaviours still exist on current main. The
manual Full-access/365-day defaults and advanced no-expiry choice are also
explicitly accepted in `spec/mcp-power-user-connection-design.md`; changing
them is therefore a product-policy revision, not an implementation-only fix.
The target-role OAuth grant remains an implementation defect regardless of the
default-authority decision.

## Outcome

If the least-privilege direction is accepted, creating an MCP credential
defaults to the least privilege that is useful, and unbounded or perpetual
lifetime is either rejected or represented as a separate, explicit, auditable
exception. Over-grant becomes a deliberate opt-in rather than the one-click
default.

## Required product decision

Before this item can move to `ready`, explicitly supersede or reaffirm the
Full-access/365-day/no-expiry decisions in
`spec/mcp-power-user-connection-design.md`. Record:

- the manual default preset, lifetime, and NAS preselection policy;
- the OAuth initial scope challenge and consent policy;
- the maximum server-accepted lifetime and whether an explicit perpetual
  credential remains supported.

If the existing power-user defaults are reaffirmed, narrow this item to the
unambiguous server-side issues: destination-only profile exclusion, explicit
OAuth NAS consent, and any separately accepted lifetime bound.

## Scope

- **Manual-token wizard defaults** (`internal/gateway/admin/ui.go`,
  `openAccessWizard()` ~line 692). Today it hardcodes `tokenExpiry = 365`, the
  `authorityPreset = full` (which `applyAuthorityPreset()` maps to all four
  scopes including `nas.apply`), and `renderTokenNASChoices()` renders every
  non-target NAS checkbox pre-checked. Flip these to least privilege: default the
  preset to `observer` (`nas.read` only), default the lifetime to a short window
  (e.g. 30 days), and render NAS checkboxes unchecked so each NAS is opted in
  deliberately. (The `authorityPreset` dropdown and target-role skip added by the
  recent rework stay.)
- **OAuth grant defaults** (`internal/gateway/oauth/handler.go`).
  `ScopeChallenge()` (~line 146) tells normal MCP clients to request
  `defaultScopeString` (all four scopes, ~line 35), protected-resource metadata
  advertises all four (~line 156), and `normalizeScopes` (~line 606) substitutes
  all four when a client sends no `scope`. Changing only `normalizeScopes` is
  insufficient because current MCP clients treat the `WWW-Authenticate` scope
  as authoritative. If the least-privilege direction is accepted, make the
  initial challenge and protected-resource metadata request `nas.read`, change
  the scope-less fallback to the same value, and keep explicit requests for
  `nas.plan`/`nas.apply` possible. Define and test how a client later obtains a
  broader grant instead of relying on an undocumented UI retry.
  `validateAuthorizationRequest` (~lines 433–446) unconditionally builds the NAS
  allowlist from every profile (and, unlike the manual path, does **not** skip
  `role: target` profiles). Exclude target-role profiles, and add a mechanism to
  scope the granted NAS set per authorization (prefer an administrator selection
  at the consent step; use a protocol parameter only if it has a defined,
  validated contract) instead of always granting all profiles.
- **Server-side lifetime cap** (`internal/gateway/state/policy.go`,
  `normalizeMCPTokenInput` ~line 736). Today any future `ExpiresAt` is accepted
  and a nil `ExpiresAt` means never-expires. Add a `MaxMCPTokenLifetime` and
  reject `ExpiresAt` beyond `now+cap`; either reject a nil `ExpiresAt` or auto-set
  it to `now+cap` unless an explicit "no expiry" flag is set, so a perpetual
  token is always deliberate and auditable. Mirror the existing approval-TTL cap
  pattern (`policy.go` ~lines 358–359).

## Non-goals

- Removing the ability to create a broad/long-lived token entirely — advanced
  operators may still opt in if the required product decision retains it.
- Scope/approval-model semantics (immutability, high-risk approval binding) —
  unchanged; see the model already documented in `docs/gateway.md`.
- Any transport, forwarded-header, or docs change (WI-096/WI-098/WI-099).

## Design constraints

- The server-side lifetime policy is authoritative: it must reject an over-long
  lifetime regardless of the client/UI. If perpetual credentials remain
  supported, an explicit server-side input must distinguish that choice from an
  omitted expiry; the UI default alone is never the guarantee.
- Preserve the credential-list states (never-used/used/expired/revoked) and the
  digest-only storage; this item changes defaults and bounds, not storage.
- Keep scopes/allowlist immutable after creation (changing authority still means
  issuing a new credential).

## Acceptance criteria

- [ ] The superseding product decision is recorded in
      `spec/mcp-power-user-connection-design.md` and reflected in
      `docs/gateway.md` before implementation is marked complete.
- [ ] If least privilege is accepted, opening the manual-token wizard shows
      `nas.read`, a short bounded lifetime, and no NAS pre-selected; submission
      requires the operator to choose a NAS for a `nas.*` grant. Unit/UI
      assertions cover the defaults and validation.
- [ ] A normal MCP authorization initiated from the server's
      `WWW-Authenticate` challenge yields the decided initial scope (not merely
      a hand-built scope-less request); the protected-resource metadata and
      fallback agree. Explicitly requesting `nas.plan`/`nas.apply` still works.
- [ ] Target-role profiles are always excluded from OAuth grants, and the
      consent step grants only the explicitly selected managed profiles.
- [ ] `normalizeMCPTokenInput` rejects an `ExpiresAt` beyond the cap and rejects
      (or explicitly gates) a perpetual token; a table test covers the boundary.
- [ ] Existing gateway/state and admin tests stay green; new behaviour is tested.

## Verification

- Create a token via the UI accepting all defaults; confirm scopes/lifetime/NAS.
- Drive OAuth from the real unauthenticated MCP challenge, with an omitted
  `scope`, and with an explicit broader `scope`; confirm the granted scopes and
  that target NAS are excluded.
- `go test ./internal/gateway/... -count=1`.

## Coordination

- `internal/gateway/admin/ui.go` and `oauth/handler.go` are actively edited by
  the gateway stream (WI-045/WI-048/WI-091/WI-092/WI-095); rebase and re-verify
  line references before editing.
- Product decision is blocking, not a late implementation touchpoint: update
  the accepted power-user design before claiming this item.

## Handoff

Fill this only when pausing incomplete work.
