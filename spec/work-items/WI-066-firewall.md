---
id: WI-066
title: Firewall rules
status: proposed
priority: P1
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/firewall
  - internal/synology/operations/firewall
  - internal/synology/firewall.go
  - internal/runtime/manager.go
  - internal/application/firewall.go
  - internal/cli/firewall.go
  - internal/mcpserver/server.go
  - internal/mcpserver/read_only.go
  - docs/firewall.md
---

# WI-066 — Firewall rules

## Outcome

A CLI user or MCP agent can read the Control Panel → Security → Firewall surface
— whether the firewall is enabled, the firewall profiles, each profile's ordered
rule list, the per-network-adapter profile binding, and the default (no-match)
policy — and, through the hash-bound plan/apply contract, change the firewall rule
set and default policy under a mandatory self-lockout safety guard. This is a
focused Control Panel module in the sense of
[WI-006](WI-006-control-panel-modules.md): one typed module for the firewall
setting area, never a generic `set key=value` or raw-API proxy. There is no
"toggle rule N" convenience path that bypasses the guarded contract.

Firewall is a **core DSM surface** (discovered via `SYNO.API.Info`), so there is
no installed-package evidence or gate; absence of the API family is reported
`(not supported)` and fails closed rather than erroring adjacent modules.

The API map, method names, rule field names, and rule-list ownership semantics
below are the author's best current knowledge and **MUST be treated as
to-be-live-verified at implementation time**. The standing policy is that
source-doc and mobile/desktop-client field names are frequently stale; confirm
every name and shape against the lab with a throwaway read-only `DSMCTL_DUMP`
probe before trusting it, and re-read after any write
(see [[dsm-webapi-live-verify-fields]], [[dsm-webapi-string-param-quoting]]).

## Scope

Sliced read-first, then guarded write, so the read slice ships independently.

### Slice A — read-only (independently shippable)

Likely API family `SYNO.Core.Security.Firewall` and its sub-APIs — **all names
and versions to be live-verified**:

- **Firewall status + adapter policy** — `SYNO.Core.Security.Firewall` `get`:
  global enabled flag, and per-network-adapter (interface) binding of adapter →
  profile plus each adapter's default (no-match) policy (`allow`/`deny`/`drop`).
- **Profiles** — `SYNO.Core.Security.Firewall.Profile` `list` (a profile is a
  named rule group; DSM ships one default profile): `{id, name, is_default}`.
- **Rules per profile** — `SYNO.Core.Security.Firewall.Rules` `load`/`get`: the
  **ordered** rule list for a profile. Per-rule fields (best-knowledge, verify):
  `{index/priority, enabled, policy(allow|deny|drop), proto(tcp|udp|all),
  ip_ver(ipv4|ipv6), src_type(all|ip|subnet|range|geoip), src_value,
  port_type(all|builtin_service_set|custom), ports, name}`. Built-in service
  sets (e.g. the DSM management ports, SMB, etc.) are referenced by name and
  must be normalized to a stable domain name, not the raw DSM token.
- **Source-IP catalogs (read view)** — the GeoIP/country catalog used by geoip
  rules and the built-in application/service port-set catalog (likely
  `SYNO.Core.Security.Firewall.GeoIP` / `.Country` / a service-set list), so a
  reader can resolve rule references to human names.

### Slice B — guarded write (plan/apply, hash-bound; VERY HIGH risk)

Exactly the mutations named in the area brief, all through the mutation-safety
contract:

- **Rule create / reorder / enable-disable / delete** within a profile, likely
  `SYNO.Core.Security.Firewall.Rules` `set`. DSM's WebUI submits the **entire
  ordered rule array** for a profile in one `set`, so create/delete/reorder/
  enable are almost certainly expressed as full-ruleset replacement rather than
  incremental verbs — the ownership model is therefore **full desired state for
  the target profile's rule list** (verify at impl time). Reorder = the array's
  order; enable/disable = the per-rule `enabled` flag.
- **Default-policy set + firewall enable/disable + adapter binding** —
  `SYNO.Core.Security.Firewall` `set`: the global enable flag and each adapter's
  default no-match policy and bound profile.

## Non-goals

- **Auto Block / DoS protection / account-access protection.** These live under
  other `SYNO.Core.Security.*` families (e.g. AutoBlock, DoS) and are separate
  security posture surfaces; each is its own future WI, not folded in here.
- **Firewall rule import/export** (`.conf` upload/download of a saved rule set)
  and **GeoIP database updates**.
- **Profile create/rename/delete.** Slice B manages rules and policy *within*
  the existing profile set and the adapter bindings; profile lifecycle
  management is deferred to a follow-on once rule writes are proven safe.
- **Any generic "raw firewall set" command** or a convenience toggle that
  bypasses plan/apply — prohibited by the mutation-safety contract.

## Design constraints

- **VERY HIGH risk — a wrong rule can permanently lock the admin out of DSM.**
  Enabling the firewall with a default-deny and no matching allow rule, deleting
  or disabling the rule that permits the management port, reordering a deny above
  the management allow, or setting an adapter's default policy to deny/drop can
  each drop the current management session and leave DSM unreachable over the
  network. **Every Slice-B mutation is classified high risk**; there is no
  medium-risk firewall write. Remote apply additionally requires the existing
  single-use approval (WI-016).
- **Mandatory self-lockout simulation guard (fail closed).** dsmctl MUST NOT
  rely on DSM to protect the session. Apply computes the *effective verdict* of
  the proposed rule set for the management tuple and **refuses the apply unless
  the result provably ALLOWS it**:
  - The tuple is `(source, port, proto=tcp)` where `port` is the DSM management
    port dsmctl is actually connected over and `source` is the client address as
    seen by the NAS. The source is determined by a client-IP echo/connected-
    session read (API to be live-verified, e.g. `SYNO.Core.CurrentConnection`
    or the connected-session list); when it cannot be determined (NAT/relay),
    the operator MUST supply a `--keep-reachable <cidr>` that the guard treats
    as the source, and the guard fails closed if neither is available.
  - A local rule-evaluation function mirrors DSM's **first-match, then adapter
    default** semantics (top-down over the enabled, ordered rules, on the
    adapter carrying the management traffic) and must be unit-tested against
    DSM's real precedence. If the verdict is anything other than `allow`, apply
    is rejected before any write.
- **Preserve a revert path.** The plan records the complete prior state — the
  full prior ordered rule list for every touched profile plus the prior global
  enable and per-adapter policy/binding — and both the plan and the apply result
  surface it, so an operator on a console/LAN path can restore. Implement an
  **armed auto-revert**: after a successful write, require a re-confirmation
  within a bounded timeout; if it does not arrive, dsmctl re-submits the prior
  state. (The pre-apply simulation guarantees the session itself survives; the
  armed revert covers a rule that is subtly wrong but not self-locking.)
- **Patch semantics stated explicitly, per the contract.** Because a profile's
  rule `set` is full-ruleset replacement, the plan captures and hashes the
  **complete observed rule list** (order included) for each touched profile; a
  create/reorder/enable/delete is expressed as the resulting full ordered array.
  Unspecified rules in an untouched profile are never rewritten. Apply rejects a
  changed observed fingerprint (someone edited the firewall out-of-band), merges
  the intent into a freshly read rule set, writes, and **re-reads to verify the
  rules and default policy actually took effect** — DSM silently ignores some
  fields, and array/string parameters have historically needed JSON-literal
  encoding rather than bare form values ([[dsm-webapi-string-param-quoting]]).
- **No secrets, but session-load-bearing data.** Firewall configuration carries
  no passwords, keys, PEM, or tokens, so no `credential_ref` is required; the
  standing rule still holds that no SID/SynoToken enters plans, hashes, logs, or
  MCP arguments. The management source IP/CIDR used by the guard is not a secret
  but is safety-critical: it is derived at apply time and recorded in the plan
  as the reachability assertion, never silently defaulted.
- **Rule-list ownership + `set` shape need impl-time confirmation.** Whether
  `Rules.set` replaces the whole profile array (expected) or accepts incremental
  edits, whether reorder is index-driven, and the exact `src_type`/`port_type`
  token vocabulary must be confirmed with one authorized, fully-reverted live
  create/reorder/delete against a **non-management** rule before Slice B ships.
- **Per-operation compatibility + capabilities.** Each read and each write is an
  independently selectable operation with a stable name, selected backend, API,
  and version in the capability report; a missing firewall API fails closed and
  does not disable other Control Panel modules.

## Acceptance criteria

- [ ] Slice A: `firewall capabilities|status|profiles|rules` (CLI) and the
      matching `get_firewall_*` MCP tools return normalized state — global
      enabled flag, profiles, each profile's ordered rule list with normalized
      policy/proto/source/port fields, per-adapter default policy and profile
      binding — with tolerant decoders that reject malformed shapes rather than
      returning an empty successful state.
- [ ] Slice A independent gating: the firewall API family selects its own backend
      by advertised version; when absent it reports `(not supported)` and fails
      closed without disabling adjacent Control Panel modules.
- [ ] Slice A live verification on the DSM 7.3 lab (read-only): status, the
      default profile's rules, adapter bindings, and the GeoIP/service-set
      catalogs decode against the real NAS with no SID/SynoToken leak.
- [ ] Self-lockout guard implemented and unit-tested: a rule-evaluation function
      reproduces DSM first-match + adapter-default precedence; apply is **refused
      fail-closed** for any proposed state whose effective verdict for the
      management tuple is not `allow`, and refused when the source cannot be
      determined and no `--keep-reachable` CIDR is supplied.
- [ ] Slice B: rule create/reorder/enable/delete and default-policy/enable/
      adapter-binding set via hash-bound plan/apply — plan captures and hashes
      the complete prior ordered rule set + global/adapter policy, apply rejects
      stale state, writes full desired state, re-reads to verify, and preserves
      the prior state for revert (armed auto-revert on missing re-confirmation).
- [ ] All Slice-B mutations classified high risk; the read-only gateway strips
      `plan_firewall_change` / `apply_firewall_plan`; remote apply requires the
      existing single-use approval.
- [ ] Slice B live verification on the DSM 7.3 lab (authorized, fully reverted):
      a **non-management** allow rule created → reordered → disabled → deleted
      through plan/apply with postcondition re-read; a deliberately self-locking
      proposal (default-deny with the management allow removed) is refused by the
      guard and never sent to DSM; final state matches the pre-test snapshot.

## Verification

- Unit: decoder tolerance + malformed rejection; the rule-evaluation/precedence
  function against a table of DSM-observed verdicts; precondition fingerprint +
  plan hash + staleness rejection; request-capture asserting full-ruleset `set`
  body shape and JSON-literal array/string encoding; a test proving the guard
  refuses a self-locking proposal and one proving fail-closed with no source.
- `go build ./...`, `go vet ./...`, `go test ./... -count=1`.
- Live reads allowed against the DSM 7.3 lab. Live writes require explicit
  per-session authorization and operate **only** on a throwaway non-management
  rule (never the rule permitting the connecting management port), captured
  before and fully reverted after; the self-lockout guard is exercised with a
  proposal that is confirmed rejected before any write.
- Source of truth for fields (all still requiring the live re-read):
  `SYNO.Core.Security.Firewall*` conf + handlers on codesearch and the Admin
  Center firewall UI module; confirm on the lab per
  [[dsm-webapi-live-verify-fields]].

## Coordination

- Parallel group C, alongside the other Control Panel / security modules; shares
  the module registry and facade pattern established by
  [WI-006](WI-006-control-panel-modules.md). New read operations under
  `internal/synology/operations/firewall`; following the FileStation precedent
  ([WI-049](WI-049-file-station.md)) the guarded writes may live in a sibling
  `internal/synology/operations/firewallmutation` package — decide at impl time.
- The self-lockout guard needs the connecting management port/protocol and the
  client source address; coordinate with `internal/runtime/manager.go` (which
  owns the session/target) and any concurrent connection-layer work
  (e.g. [WI-042](WI-042-quickconnect-transport.md)) so the source/port used by
  the guard reflects the real transport, not an assumed direct URL.
- Adjacent-but-separate security surfaces (Auto Block, DoS protection, account
  protection) are their own WIs; do not fold them into this module.

## Handoff

Fill this only when pausing incomplete work.
