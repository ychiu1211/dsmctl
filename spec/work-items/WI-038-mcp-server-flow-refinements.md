---
id: WI-038
title: Streamline MCP Server approval and administration flows
status: done
priority: P1
owner: ""
depends_on: [WI-037]
parallel_group: G
touches:
  - internal/gateway/admin
  - internal/gateway/state
  - internal/mcpserver
  - internal/application
  - internal/remotepolicy
  - docs/gateway.md
  - docs/gateway-admin-guide.md
  - spec/roadmap.md
  - spec/work-items/WI-017-amd64-linux-synology-distribution.md
---

# WI-038 - Streamline MCP Server approval and administration flows

## Outcome

The highest-friction administration flows become guided, low-transcription
experiences without weakening any WI-016 authorization, out-of-band approval,
or secret-handling contract. An administrator approving a high-risk apply sees
what they are approving and no longer hand-copies a 64-character hash, a token
ID, and a revision number across three views inside a ten-minute window.
Remote calls always name their target NAS explicitly, profiles are editable
and honest about which DSM account is actually signed in, tokens gain a
visible lifecycle, NAS deletion cannot silently re-grant access later, the
LAN-discovery scope carries an honest name, and DSM enrollment secrets are
collected through masked forms.

## Scope

### High-risk approval flow

- Record a pending approval request when a remote `plan_*` call returns a
  high-risk plan to an authenticated MCP token. The record contains only
  closed, non-secret scalar fields: plan hash, NAS profile name and revision,
  requesting token ID, tool/use-case name, risk, stable resource identifier
  when available, the plan's canonical summary text, and creation time.
- The Approvals view lists open requests newest-first with the plan summary,
  risk, NAS, and requesting token resolved to its display name. One click
  creates exactly the existing approval record bound to the request's recorded
  fields and audit-logs the action; a dismiss action removes the request
  without side effects.
- Requests deduplicate by plan hash and requesting token (re-planning
  refreshes the timestamp), are bounded to a fixed count of 50 with
  oldest-first eviction, expire after at most 24 hours, and are removed when a
  matching approval is created. The TTL is the only other cleanup trigger;
  stale rows for revoked tokens or changed profiles are harmless because
  admission re-checks everything.
- Keep the manual entry path as fallback, upgraded to structured input: the
  NAS field becomes a selector over existing profiles, the requesting-token
  field becomes a selector over active tokens, and the plan-hash field
  validates 64-hex before submit. The profile-revision input is removed
  entirely: the manual path captures the selected profile's current revision
  server-side at creation time. Machine-known consistency data is never
  human-typed.
- Approval expiry displays remaining time, computed when the view renders;
  no live timer is required. No separate approval-revocation mechanism is
  added: a mistakenly created approval is stopped by revoking or rotating the
  requesting token, an existing control that apply admission already
  re-checks. Documentation states this escape hatch explicitly.

### Explicit remote NAS targeting

- Remote MCP tool calls that operate on a NAS must name it explicitly. An
  omitted `nas` argument is rejected with a clear error instead of resolving
  to the default profile, so changing a default can never silently retarget a
  remote caller. `list_nas`, `discover_lan_devices`, and the targetless
  `get_auth_status` form remain valid without a target.
- The managed administration UI drops the set-default action and default
  marker. Default-profile resolution remains a local CLI/stdio convenience
  and its behavior there is unchanged.

### MCP token lifecycle

- Token creation offers an optional expiry with presets (for example 30, 90,
  365 days); the default stays no expiry, because long-lived automation is
  the primary use of this LAN-oriented service and silent future breakage is
  worse than an aging permanent token. Accountability comes from visibility
  instead: the issued-token table shows creation time, expiry, and last-used,
  and the existing expire endpoint becomes reachable from the UI.
- Each scope checkbox carries a one-line localized description of what it
  permits. `nas.apply` is visually marked as allowing changes to the NAS,
  distinct from the read/plan scopes.
- The NAS allowlist input becomes a multi-select over existing profiles
  instead of free text. Server-side validation is unchanged.
- Deleting a NAS profile removes its name from every token allowlist in the
  same transaction and audit-logs the change. Re-creating a profile under a
  deleted name therefore requires deliberately re-granting each token.
- Token scopes and allowlists remain immutable after creation apart from the
  deletion cleanup above; changing authority still means issuing a new token
  or rotating. Document this explicitly. The token ID is displayed and
  copyable separately from the token name.

### Scope rename: `nas.admin` becomes `lan.discover`

- Rename the scope everywhere it appears: the policy constant, the tool
  authorization table, token creation validation, UI scope chips and helper
  copy, and user documentation. The scope still admits only
  `discover_lan_devices`; its meaning and enforcement do not change.
- The schema migration rewrites `nas.admin` to `lan.discover` in every stored
  token. After migration, `nas.admin` is rejected as token-creation input;
  no alias or dual acceptance remains.
- The distinct prefix is deliberate and documented: discovery reveals devices
  outside the caller's NAS allowlist, so the scope sits outside the
  allowlist-filtered `nas.*` family, and gateway administration is never an
  MCP scope.

### NAS profile management and enrollment

- Profile creation collects connection identity only: name, DSM URL, and TLS
  policy (with fingerprint when pinned). The DSM account field moves to
  password/OTP enrollment, which accepts the account together with the
  credentials and updates the stored profile username in the same
  transaction. Web Login continues to store the actually signed-in account;
  the profile list shows the enrolled account for stored sessions and
  passwords so a mismatch is visible instead of silent.
- Profiles become editable from the UI (URL, TLS policy and fingerprint,
  timeout) through the existing update API with expected-revision conflict
  handling. The name is displayed as permanent and is not editable; creation
  marks it as unchangeable.
- Replace `window.prompt` collection of DSM passwords and OTPs with a masked
  in-page modal using proper form semantics (password input type, no
  autocomplete persistence, submit/cancel). The modal states that successful
  password/OTP enrollment registers a trusted device on that NAS. Secrets
  remain bounded to the enrollment transaction exactly as today.
- After profile creation, surface the required next step (Web Login or
  password/OTP) instead of leaving it implicit in a row of table actions.
  Popup-blocked and relay-origin failures during Web Login produce actionable
  messages that steer to the password/OTP fallback.

### Administrator and form clarity

- The administrator password-change form requires new-password confirmation,
  matching first-run setup, because no recovery path exists by design.
- The minimum administrator password length is enforced and described as 12
  characters (runes), not bytes; existing stored verifiers are unaffected.
- Every form marks required fields; placeholders show example values rather
  than role descriptions; first-run setup warns operators to keep the
  uninitialized endpoint within the trusted deployment network.

### Audit review

- Render audit events as a filterable table (time, actor, action, tool/NAS,
  outcome) backed by the existing `after`/`limit`/`actor_id`/`action` query
  parameters. The raw JSON view may remain as a secondary presentation.
- Export downloads every retained event, not only the newest page: the
  export path streams or pages through the full bounded store in chronological
  order, and the UI states the export scope. View and export ordering are
  documented.

## Non-goals

- Approval through an MCP tool, chat message, returned plan hash, or a
  caller-controlled header. The WI-016 prohibition is unchanged; this item
  only reduces transcription for the human administrator.
- Push, webhook, or e-mail notification of pending approvals; refresh/polling
  in the administration page is sufficient.
- Storing full plan payloads, request bodies, DSM responses, or any secret
  material in approval-request records.
- Changing plan hashing, approval binding fields, the ten-minute approval
  TTL, single-use consumption, or local CLI/stdio behavior.
- A separate approval-revocation mechanism; revoking the requesting token is
  the documented brake for a mistaken approval.
- Exposing credential retention on profile deletion or orphan-secret
  management in the UI. Retention has no completing use case in the UI (a
  retained secret cannot re-attach to a recreated profile), so the deletion
  dialog keeps the single delete-everything path and the WI-015 API contract
  stands unchanged.
- Adding new discovery capabilities or widening `lan.discover` beyond the
  existing `discover_lan_devices` tool.
- Renaming existing profiles; the profile name remains a permanent
  identifier.
- Configurable audit retention (fixed at 10,000 events / 30 days by
  decision).
- Multiple administrators, roles, OIDC, or a dark theme.

## Design constraints

- Every WI-016 contract holds: an approval remains bound to plan hash, NAS
  profile revision, requesting token, and approving administrator; expires
  after at most ten minutes; and is atomically consumed exactly once.
  Mandatory audit writes still fail mutations closed.
- Approval-request records are advisory UI state. Their absence, expiry, or
  eviction must not block the manual approval path, and their presence must
  not bypass or pre-compute any admission check.
- Requests reveal nothing an administrator cannot already see: they are
  derived from the requesting token's own plan result plus admin-visible
  metadata. MCP responses and `list_nas` filtering are unchanged; MCP clients
  cannot list, create, or consume requests.
- Record requests from typed plan data at the gateway application/policy
  boundary, not by re-parsing rendered MCP tool text.
- Explicit targeting applies to remote principals only. Local CLI and stdio
  resolution of an omitted NAS to the default profile is unchanged, and the
  state repository may retain its default-selection operation for CLI
  compatibility; no remote code path consults it.
- The new state bucket and the stored-scope rename ship in one forward-only
  transactional schema migration with the existing pre-migration backup and
  fail-closed readiness behavior.
- Adding an expiry default must not retroactively expire existing stored
  tokens.
- Password/OTP enrollment updates the profile username through the same
  ordered revision/eviction path as any profile change; a mid-enrollment
  profile change fails closed on the revision check.
- The embedded page stays offline (no external assets, existing CSP), keeps
  stable element IDs or deliberately updates them with matching tests, and
  covers all five WI-035 locales for every new string, including localizing
  the Audit navigation label and unifying apply/approval terminology per
  locale.
- Secret canaries continue to prove that passwords, OTPs, bearer tokens,
  cookies, SynoTokens, SIDs, master keys, and ciphertext never appear in
  approval requests, audit output, logs, or API responses.

## Acceptance criteria

- [x] A remote high-risk `plan_*` result creates exactly one pending approval
      request with NAS, revision, requesting token, tool, risk, and summary;
      duplicates refresh rather than accumulate; bounds and TTL are enforced.
- [x] One-click approval creates a standard approval bound to the request's
      recorded fields, audit-logs it, and removes the request; dismissing a
      request has no other effect; local/stdio plans never create requests.
- [x] The manual approval form offers profile and token selectors, has no
      revision input, captures the selected profile's current revision
      server-side, and rejects malformed hashes client- and server-side.
- [x] Remote target-scoped tools reject an omitted `nas` argument with a
      clear error; `list_nas`, `discover_lan_devices`, and targetless
      `get_auth_status` still work; CLI/stdio default resolution is
      unchanged; the managed UI no longer offers a set-default action.
- [x] Token creation offers an optional expiry (default: none); the table
      shows creation time, expiry, and last-used plus a separately copyable
      token ID; an existing token can be expired from the UI; each scope
      shows a localized description and `nas.apply` is visually marked as
      permitting changes.
- [x] The allowlist input only offers existing profiles; deleting a profile
      transactionally removes it from every token allowlist and audit-logs
      the cleanup; a recreated same-name profile is not remotely accessible
      through any pre-existing token.
- [x] After migration, a token stored with `nas.admin` authorizes
      `discover_lan_devices` through `lan.discover`, the UI and tool table
      show only the new name, and creating a token with `nas.admin` is
      rejected.
- [x] Profile creation collects name, URL, and TLS only; password/OTP
      enrollment collects the DSM account with the credentials and updates
      the stored username transactionally; the profile list shows the
      actually enrolled account; profiles are editable (URL, TLS, timeout)
      with revision-conflict handling and a permanent, non-editable name.
- [x] Password/OTP enrollment uses a masked modal that discloses
      trusted-device registration; no `window.prompt` remains for secret
      entry; enrollment secrets stay out of state, logs, and audit.
- [x] Administrator password change requires a matching confirmation entry;
      the minimum length is enforced and described as 12 characters; forms
      mark required fields; approval expiry shows remaining time.
- [x] The audit view is a filterable table backed by the existing query
      parameters, and export returns every retained event in documented
      order even when more than 1,000 events are stored.
- [x] All five locales cover every new string with English fallback,
      including a localized Audit navigation label; empty states remain
      purposeful; no external asset is introduced.
- [x] `go test ./... -count=1`, `go vet ./...`, the amd64 Docker build, and an
      isolated-container browser walkthrough of the changed flows pass.

## Verification

### Results (2026-07-19)

- `go test ./... -count=1` — passed with Go 1.26.5 on windows/amd64.
- `go vet ./...` — passed with Go 1.26.5 on windows/amd64.
- `docker build --platform linux/amd64 -f deploy/container/Dockerfile -t dsmctl:wi-038 .`
  — passed with Docker Server 29.6.1.
- Isolated-container browser walkthrough — passed against the amd64 image on
  `127.0.0.1:18766` with a read-only root filesystem, tmpfs state, all Linux
  capabilities dropped, and `no-new-privileges`. Verified first-run setup,
  profile create/edit and masked password/OTP dialogs, token scopes and expiry,
  approval/manual fallback, audit filters/export copy, password confirmation,
  all five locale catalogs, CSP/offline assets, and the toast overlay fix. The
  container and its tmpfs state were removed after verification.
- All test traffic used fakes, captured requests, or the isolated local
  container. No live DSM mutation or DSM version certification was performed.

- Unit tests for request lifecycle: creation on high-risk plan, deduplication,
  bounds/TTL eviction, removal on approval and on token revocation, and a race
  between one-click approval and manual creation for the same plan hash.
- Authorization table tests covering an omitted `nas` argument for every
  remote target-scoped tool and the unchanged targetless tools.
- Authorization tests proving MCP tokens cannot read or mutate approval
  requests and that admission checks are byte-identical with and without a
  request record.
- Export tests seeding more than 1,000 audit events and asserting complete,
  ordered output; enrollment tests asserting the account/username update
  shares the revision/eviction path.
- Rendered-UI assertions for the new controls, locale catalogs, stable IDs,
  and absence of `window.prompt` secret collection; secret-canary scans over
  persisted bytes, logs, and API output.
- Migration tests covering the new schema version, scope rewrite,
  pre-migration backup, and fail-closed readiness on migration error.
- All verification uses fakes and captured requests. No live DSM mutation is
  authorized by this work item.

## Coordination

WI-017 certifies the final administration UI on real Synology hardware and now
depends on this item; land it before that certification. The explicit remote
NAS targeting change and the scope rename are pre-distribution compatibility
breaks by deliberate decision; both must be reflected in `docs/gateway.md` and
the Traditional Chinese operator guide in the same change. Touches the
high-contention `internal/mcpserver` and `internal/application` files —
coordinate with any active management work item before changing shared
middleware or service seams.

## Handoff

Fill this only when pausing incomplete work.
