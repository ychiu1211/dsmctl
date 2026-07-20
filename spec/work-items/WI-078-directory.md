---
id: WI-078
title: Directory services (LDAP and Active Directory)
status: proposed
priority: P2
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/directory
  - internal/synology/operations/directory
  - internal/synology/directory.go
  - internal/runtime/manager.go
  - internal/application/directory.go
  - internal/cli/directory.go
  - internal/mcpserver/server.go
  - docs/control-panel.md
---

# WI-078 — Directory services (LDAP and Active Directory)

## Outcome

A CLI user or MCP agent can read the Control Panel → Domain/LDAP surface —
whether the NAS is joined to an Active Directory domain or bound to an LDAP
server, the directory connection health, the sync schedule, and the synced
domain user/group lists — and, through the hash-bound plan/apply contract,
join/leave an AD domain or bind/unbind an LDAP server under high-risk
guardrails. This is a focused Control Panel module in the sense of
[WI-006](WI-006-control-panel-modules.md): one typed module per DSM setting
area, never a generic `set key=value` proxy.

The API families, methods, and fields named below are the author's current
best knowledge from the DSM Domain/LDAP UI behavior and the `SYNO.Core.Directory.*`
naming convention. Per the standing policy, **every API specific here is to be
live-verified at implementation time** with a throwaway `DSMCTL_DUMP` probe
against the lab before it is trusted — source-doc and mobile-client field names
are routinely stale (see [[dsm-webapi-live-verify-fields]]). The lab NAS may not
be domain-joined, so both the read shapes and the join/leave field symmetry must
be established live before Slice B ships.

## Scope

Sliced read-first, then guarded write, so the read slice can ship independently.

### Slice A — read-only (to be live-verified)

- **Directory mode / AD status:** likely `SYNO.Core.Directory.Domain` `get`
  (or `status`) → normalized `{joined, domain_fqdn, netbios/workgroup, dns_server,
  domain_controller, connection_status, ou, last_sync}`. Volatile connection
  detail is fine to surface but the model reports a stable `mode` enum
  (`ad` / `ldap` / `none`).
- **LDAP client status:** likely `SYNO.Core.Directory.LDAP` `get` →
  `{bound, server_address, base_dn, bind_dn, encryption, profile,
  connection_status}`. Never surface the bind password or any secret.
- **Synced domain user/group lists (read):** likely
  `SYNO.Core.Directory.Domain.User` / `.Group` `list`, or a domain-scoped query
  on `SYNO.Core.User` / `SYNO.Core.Group` — **which one is real is explicitly
  to be live-verified**. Paginated, read-only, entries clearly marked
  domain-sourced; no password hashes, Kerberos keytab, or machine-account
  material in the model.
- **Sync schedule (read):** likely `SYNO.Core.Directory.Domain.Schedule` `get`
  → `{enabled, schedule}` for the periodic domain user/group sync.
- **capabilities:** each area (Domain, LDAP, domain-directory read) reports its
  own stable operation name, selected backend, API, and version; a missing API
  is reported `(not supported)`, fail-closed, never faked as empty state.

### Slice B — guarded write (plan/apply, hash-bound)

- **AD domain join** — `SYNO.Core.Directory.Domain` `join`. Best-known fields
  (to be live-verified): `domain` (FQDN), `workgroup`/NetBIOS, `dns` (server),
  admin `username`, admin `password`, optional `ou` (organizational unit),
  server description, and DSM's join options (e.g. DNS update, nsswitch). The
  domain admin bind password is supplied via `credential_ref: env:NAME` — it
  never enters the request payload, plan, hash, result, or logs; it is resolved
  only at apply time.
- **AD domain leave** — `SYNO.Core.Directory.Domain` `leave`. May require
  re-supplying domain admin credentials via `credential_ref`. High risk: leaving
  revokes every domain login on the NAS.
- **LDAP bind** — `SYNO.Core.Directory.LDAP` `bind` / `set`. Fields (to be
  live-verified): `server_address`, `base_dn`, `bind_dn`, bind `password`
  (`credential_ref`), `encryption` (none/StartTLS/LDAPS), `profile`
  (Synology/standard/other). **LDAP unbind** — the symmetric disable.

## Non-goals

- **Directory Server (being an AD or LDAP server).** The DSM *Directory Server*
  and *LDAP Server* packages — hosting a domain, provisioning domain
  users/groups/DNS zones, SSO issuance (`SYNO.ActiveDirectory.*` /
  `SYNO.LDAP.Server.*` or their package APIs) — are a large, separate management
  domain with their own lifecycle and belong in their own work item. This module
  is strictly a *directory client*: it joins/binds the NAS to an existing
  directory, it does not run one. Deferred with reason.
- **Per-domain-user/group privilege assignment.** Granting a synced domain
  principal share/app access is account and effective-access territory
  ([WI-007](WI-007-effective-access-explanation.md), the share/ACL modules);
  this WI owns only join/bind and the read of the synced list, not authorization
  of those principals.
- **Editing individual synced domain users/groups.** They are owned by the
  domain controller and are read-only here.
- **SSO / SAML / domain-based single sign-on**, Kerberos keytab and
  certificate/CA trust management for the directory, and the local DSM user
  database (owned by the account module).

## Design constraints

- **Independent compatibility boundaries.** AD (`SYNO.Core.Directory.Domain`)
  and LDAP (`SYNO.Core.Directory.LDAP`) are separate API families and separate
  failure boundaries even though a NAS is normally in at most one mode. A NAS in
  neither mode reports `mode: none` for both cleanly; an absent API for one is
  reported `(not supported)` and must not disable reading the other or the
  capabilities surface. Per-operation backend selection, fail-closed when the
  API is absent.
- **Secrets never enter requests/plans/hashes/logs/MCP args.** The AD domain
  admin bind password and the LDAP bind password use the existing
  `credential_ref: env:NAME` mechanism (as user-password changes do), resolved
  at apply time and absent from the request, plan, approval hash, result, and
  logs. Synced-user password hashes, Kerberos keytab bytes, and machine-account
  secrets are never surfaced by any display model.
- **Every write is high risk — it changes authentication for the whole NAS.**
  Joining a domain remaps identity/UID-GID and can grant domain administrators
  access to the NAS; leaving a domain revokes all domain logins and can **lock
  out an administrator whose only account is a domain account**; binding/unbinding
  LDAP is equivalent. Classify all Slice-B mutations high; there is no "medium"
  directory toggle here.
- **Admin-lockout guard.** A `leave`/`unbind` that would remove the current
  principal's only remaining authentication path must be refused, or gated behind
  an explicit high-risk override, analogous to the built-in/current-session
  principal protection in the secrets-and-identity contract. The plan must state
  when the acting principal is domain-sourced.
- **Async join + postcondition.** Domain join/leave is asynchronous — DSM
  returns before the machine account/trust settles and status transitions
  through a `joining`/`leaving` phase. Follow the module pattern: plan records
  and hashes the complete current directory state; apply rejects a changed
  state, performs the typed join/leave/bind, then **polls status to a terminal
  `joined`/`bound`/`failed` state and re-reads `mode`** to verify the change took
  effect (accepting only the call's initial `OK` is insufficient, and DSM
  silently ignores some fields — the recurring lesson).
- **Mode switch is explicit.** Joining AD while LDAP-bound (or the reverse) is a
  mode transition, not an additive patch; the plan must surface the `none→ad`,
  `none→ldap`, or cross-mode transition as its canonical intent.
- **Field/shape confirmation before Slice B.** Because the lab may not be
  domain-joined, the join/leave and bind/unbind field shapes and the domain
  user/group read API must be confirmed with a `DSMCTL_DUMP` probe and, for
  Slice B, one authorized, fully-reverted join/bind against a **throwaway test
  domain / LDAP server** before the writes ship. Never exercise a write against
  the lab's real authentication.

## Acceptance criteria

- [ ] Slice A: `directory capabilities|status|users|groups` (CLI) and the
      matching `get_*` MCP tools return normalized state (mode `ad`/`ldap`/`none`,
      connection health, domain/LDAP identity); bind passwords, hashes, and
      keytab material are provably absent from all output (unit test asserts the
      decoded state never carries a secret; live `--json` grep confirms it).
- [ ] Independent gating: Domain and LDAP each select their own backend; a
      missing/absent API is reported `(not supported)` without disabling the
      other area or capabilities.
- [ ] Slice A live read verified against the lab via a throwaway `DSMCTL_DUMP`
      probe; the real API family, versions, and field names are recorded (or the
      lab's not-joined/absent state is documented) — the speculative names above
      are replaced with live-verified ones.
- [ ] Domain user/group list read is paginated and read-only; synced entries are
      clearly marked domain-sourced and carry no password/keytab material.
- [ ] Slice B: AD `join` + `leave` and LDAP `bind` + `unbind` via hash-bound
      plan/apply; bind passwords via `credential_ref: env:NAME` (absent from
      request, plan, hash, and logs); classified high risk; the read-only gateway
      excludes the plan/apply tools.
- [ ] Apply postcondition polls the async join/leave to a terminal state and
      re-reads `mode`; stale-state rejection is tested (plan hash mismatch after
      an out-of-band change).
- [ ] Admin-lockout guard: a `leave`/`unbind` that would strip the current
      principal's only authentication path is refused or requires an explicit
      high-risk override; covered by a unit test with a domain-sourced acting
      principal.
- [ ] Slice B live verification (authorized, fully reverted) against a throwaway
      test domain / LDAP server, with the postcondition proof, before the writes
      are enabled by default. Any wire-method/field correction found live is
      recorded in the WebAPI map memory.

## Verification

- Decoder + request-capture unit tests; `go test ./... -count=1`, `go vet ./...`.
- Live read via an authenticated session plus a throwaway `DSMCTL_DUMP` probe to
  fix the API family and field names; live join/leave/bind requires explicit
  per-session authorization and a throwaway test domain/LDAP server — never the
  lab's real authentication path.
- Source of truth for fields: codesearch WebAPI Directory conf/handlers for
  `SYNO.Core.Directory.Domain`, `SYNO.Core.Directory.LDAP`, and the
  domain-directory read/sync-schedule APIs (current DSM7 branch). Live
  verification overrides any stale source; the confirmed map is recorded in a
  `dsm-directory-webapi-map` memory note on completion.

## Coordination

- Shares the `internal/domain/controlpanel` facade/registry pattern and
  `internal/synology/controlpanel.go` with the other Control Panel modules
  (parallel group C). New operation package under
  `internal/synology/operations/directory`; module registration in
  `internal/runtime/manager.go`; thin CLI in `internal/cli/directory.go` and thin
  MCP tools in `internal/mcpserver/server.go` using the same application methods;
  user docs in `docs/control-panel.md`. No overlap with other Control Panel
  modules beyond the shared facade.
- Conceptual overlap with the account module ([WI-009](WI-009-credential-lifecycle.md))
  and effective-access ([WI-007](WI-007-effective-access-explanation.md)): synced
  domain users/groups surface there for authorization, while this WI owns only
  the directory join/bind and the read of the synced list. Coordinate so domain
  principals are not double-managed — this module never assigns their privileges.

## Handoff

Fill this only when pausing incomplete work.
