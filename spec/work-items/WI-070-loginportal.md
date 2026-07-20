---
id: WI-070
title: Login Portal and reverse proxy
status: proposed
priority: P2
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/loginportal
  - internal/synology/operations/loginportal
  - internal/synology/loginportal.go
  - internal/runtime/manager.go
  - internal/application/loginportal.go
  - internal/cli/loginportal.go
  - internal/cli/root.go
  - internal/mcpserver/server.go
  - internal/mcpserver/read_only.go
  - docs/control-panel.md
---

# WI-070 — Login Portal and reverse proxy

## Outcome

A CLI user or MCP agent can read the Control Panel → Login Portal surface — the
DSM access settings (HTTP/HTTPS ports, HTTP→HTTPS redirect, HSTS, HTTP/2,
customized domain), the per-application portals (alias / port), and the
reverse-proxy rules — and, through the hash-bound plan/apply contract, change
those settings under guardrails. This is a focused Control Panel module in the
sense of [WI-006](WI-006-control-panel-modules.md): one typed module per DSM
setting area with stable semantic field names, never a generic `set key=value`
proxy over `SYNO.Core.Web.DSM`.

Login Portal is a **core DSM surface** (discovered via `SYNO.API.Info`), so there
is no installed-package gate. It is, however, the single most dangerous
Control-Panel area dsmctl touches: the DSM tab's own settings decide how the
administrator — and dsmctl itself, over its web-login session — reaches DSM.

The API families named below are the author's best current knowledge. **Every
API name, version, method, and field in this spec is to be live-verified at
implementation time** against the lab with a throwaway `DSMCTL_DUMP` read probe
before it is trusted — the standing policy is that source-doc and mobile-client
field names are frequently stale, and this module has zero margin for a wrong
field (see [[dsm-webapi-live-verify-fields]]).

## Scope

Sliced read-first, then guarded write, so the read slice ships independently.

### Slice A — read-only

Three independent areas, each its own compatibility/failure boundary:

- **DSM access (Login Portal → DSM tab).** Likely `SYNO.Core.Web.DSM` `get`
  (version to-be-live-verified, expected v1/v2), normalized into stable field
  names: HTTP port, HTTPS port, HTTPS enabled, HTTP→HTTPS force-redirect, HSTS
  enabled (+ max-age / include-subdomains if present), HTTP/2 enabled, and the
  DSM portal customization (alias-based vs port-based access to DSM). The
  customized-domain / external-domain settings may live on a sibling family
  (`SYNO.Core.Web.DSM.External` or similar) — confirm at impl time and fold into
  the same normalized state, reported `(not supported)` independently if absent.
- **Application portals (Login Portal → Applications tab).** Likely
  `SYNO.Core.AppPortal` `list` / `get` → per-app entries with stable names: app
  id + title, portal alias (path portal), portal HTTP/HTTPS port (port portal),
  enabled, and per-app HTTP→HTTPS redirect.
- **Reverse-proxy rules (Login Portal → Advanced tab).** Likely
  `SYNO.Core.AppPortal.ReverseProxy` `list` / `get` → each rule with a
  server-assigned stable id (uuid), description, frontend (protocol, hostname,
  port, HSTS/HTTP2 flags, referenced certificate id — id only, never key
  material), backend (protocol, hostname, port), and custom / proxy headers
  (e.g. WebSocket, `X-Forwarded-*`).

### Slice B — guarded write (plan/apply, hash-bound)

Every write here changes how services (including DSM) are reached, so all of
Slice B rides the hash-bound plan/apply contract: plan records and hashes the
complete observed state, apply rejects a changed state, merges the patch into a
freshly read config, and re-reads to verify the requested fields actually took
effect (DSM silently ignores some fields — the recurring lesson across this
codebase, e.g. WI-041's `set_relay_enable` → `set_misc_config` and WI-049's
JSON-literal string quoting).

- **DSM access settings** — `SYNO.Core.Web.DSM` `set` (patch-only ownership):
  ports, HTTPS enable, HTTP→HTTPS redirect, HSTS, HTTP/2, customized domain.
- **Application portal enable / alias / port** — `SYNO.Core.AppPortal`
  `set` / `edit`, keyed by app id.
- **Reverse-proxy rule CRUD** — `SYNO.Core.AppPortal.ReverseProxy`
  `create` / `set` / `delete`, keyed by the rule uuid.

## Non-goals

- **Certificate management and certificate→service assignment**
  (`SYNO.Core.Certificate.*`). A reverse-proxy rule or the DSM HTTPS setting may
  *reference* a certificate id, but issuing, importing, assigning, or renewing
  certificates (and any ACME/Let's Encrypt provisioning for a proxied hostname)
  is a separate module. Private-key/PEM material never enters this module.
- **The External Access surface** — Synology Account, QuickConnect, DDNS, and
  port-forwarding — which is [WI-041](WI-041-external-access.md). This WI manages
  the reverse-proxy / DSM-portal half of "how the NAS is reached"; WI-041 owns
  the account/relay/DDNS half. They must not both write the same fields.
- **Firewall, auto-block, account-protection, and DoS protection**
  (`SYNO.Core.Security.*`) — a separate security module.
- **Login-portal branding / customization** (logo, background, login-page theme)
  and **SSO / SAML / LDAP / domain sign-in** integration.
- A generic `login-portal set key=value` command over `SYNO.Core.Web.DSM`.

## Design constraints

- **Focused module, not a raw proxy.** Per [WI-006](WI-006-control-panel-modules.md),
  the domain model exposes stable semantic names (HTTPS redirect, HSTS, portal
  alias, frontend/backend) and validated intents, not the raw DSM request keys.
- **Self-lockout is the defining hazard — classify every DSM-access write HIGH
  risk.** Changing the HTTP/HTTPS port, forcing HTTP→HTTPS redirect, disabling
  HTTP, or repointing the customized domain can sever the very session dsmctl
  and the administrator use to reach DSM (dsmctl's web-login session rides one of
  these ports; see [[dsm-weblogin-protocol]]). This is exactly the contract's
  "built-in or current-session principal requires an explicit protection policy":
  the plan must surface the current transport (scheme/host/port dsmctl is
  connected on) and the concrete reachability change; apply must **refuse to
  disable or move the port/scheme carrying the current session** unless an
  explicit override is given, and must re-read on the (expected) new endpoint to
  confirm DSM is still reachable, failing loudly if not.
- **Reverse-proxy and portal writes are HIGH risk — they change external
  exposure.** A new reverse-proxy rule or an enabled application portal can
  publish an internal service to the public internet; classify all Slice-B
  mutations HIGH. There is no low/medium externally-facing toggle here. Remote
  apply requires the existing single-use approval (per the remote-gateway
  contract).
- **Independent compatibility boundaries, fail-closed.** DSM-access, AppPortal,
  and ReverseProxy are three separate API families with three separate version
  splits. Each selects its own backend and is reported `(not supported)` when its
  API is absent, without disabling the other two. Capability reporting lists a
  stable operation name, selected backend, API, and version per area.
- **Reverse-proxy rule identity + whole-set fingerprint.** Rules are keyed by the
  server-assigned uuid. Because `create` returns a new uuid and concurrent DSM
  edits reorder/replace rules, the plan/apply precondition must fingerprint the
  **complete observed rule set** (not just the target rule) so a stale plan is
  rejected — mirroring WI-049's multi-path `FilePrecondition`.
- **Patch-only ownership.** DSM-access and app-portal writes are patches:
  unspecified fields are read fresh and preserved, never silently reset (contract
  "Unspecified fields must never be silently reset"). Reverse-proxy `set`
  replaces a single identified rule as full desired state; other rules are left
  untouched.
- **Secrets never enter requests/plans/hashes/logs/MCP args.** Certificate
  private keys and KMIP material are out of scope and never surfaced (id only).
  Any secret-bearing reverse-proxy header value (e.g. an injected auth token) or
  basic-auth credential must use the existing `credential_ref: env:NAME`
  mechanism, resolved at apply time and absent from the request, plan, hash,
  result, and logs. SIDs/SynoTokens in endpoint URLs must be redacted from any
  error surfaced by this module (as WI-049 fixed for transfer URLs).

## Acceptance criteria

- [ ] Slice A: `control-panel login-portal capabilities|dsm|applications|reverse-proxy`
      (CLI) and `get_login_portal_*` (MCP) return normalized state; decoders
      reject malformed shapes and never silently return an empty successful
      state; each of the three areas selects its own backend and a missing area
      is reported `(not supported)` without disabling the others.
- [ ] Slice A: no secret/identity leak — certificate key material is never
      present (id only), and SID/SynoToken never appear in output or errors; a
      unit test and a live `--json` grep confirm it.
- [ ] Slice A live-verified on the DSM 7.3 lab: read DSM ports / redirect / HSTS
      / HTTP2 / domain, the application-portal list, and the reverse-proxy rule
      list, with the correct backends selected.
- [ ] Slice B (DSM access): `SYNO.Core.Web.DSM` `set` via hash-bound plan/apply,
      patch-only, classified HIGH risk, with a request-capture test and a
      postcondition re-read; the self-lockout guardrail refuses to sever the
      current-session transport without an explicit override and re-reads on the
      expected new endpoint to confirm reachability.
- [ ] Slice B (portals + reverse proxy): application-portal enable/alias/port and
      reverse-proxy rule create/set/delete via plan/apply, keyed by app id / rule
      uuid, fingerprinting the whole rule set; classified HIGH risk;
      `credential_ref` used for any secret header value.
- [ ] The read-only remote gateway excludes `plan_login_portal_change` /
      `apply_login_portal_plan` (asserted by a `read_only` test); MCP golden
      tool-count updated.
- [ ] Slice B live-verified on the DSM 7.3 lab as a fully reverted round-trip
      (authorized per-session; performed with an out-of-band recovery path — SSH
      or console — available), with postcondition proof.

## Verification

- Unit: strict decoders (tolerant of numbers-as-strings / `additional`
  sub-resources, rejecting malformed shapes); request-capture tests for each
  `set`/`create`/`delete`; precondition fingerprint + plan-hash + staleness
  rejection; the self-lockout guardrail (a plan that would move/disable the
  current-session port is refused without override).
- `go build ./...`, `go vet ./...`, `go test ./... -count=1`.
- Live reads allowed on the explicitly configured DSM 7.3 lab. **Live writes
  require explicit per-session authorization** and must be run only with an
  out-of-band recovery path available (these settings can change how DSM is
  reached), and must be reverted. Confirm the actual API/version/method/field
  names first with a throwaway `DSMCTL_DUMP` probe — do not ship any write on the
  strength of the source docs alone.
- Source of truth to reconcile against the live probe: codesearch
  `webapi`/Admin-Center definitions for `SYNO.Core.Web.DSM`,
  `SYNO.Core.AppPortal`, and `SYNO.Core.AppPortal.ReverseProxy`, and the DSM
  Admin Center Login Portal (DSM / Applications / Advanced) tab JS.

## Coordination

- Shares the Control Panel facade (`internal/synology/loginportal.go` alongside
  the group-C modules) and registers CLI under the existing `control-panel`
  command tree (`internal/cli/root.go`) plus MCP tools in
  `internal/mcpserver/server.go`. New operation package under
  `internal/synology/operations/loginportal`; new domain under
  `internal/domain/loginportal`; application layer
  `internal/application/loginportal.go`.
- **Overlaps [WI-041](WI-041-external-access.md).** Both describe "how the NAS is
  reached." WI-041 owns Synology Account / QuickConnect / DDNS / port-forwarding;
  this WI owns the DSM portal, application portals, and reverse proxy. Neither
  writes the other's fields; if the customized-domain setting turns out to be
  shared between `SYNO.Core.Web.DSM.External` and DDNS, coordinate ownership
  before either ships a write.
- Depends on the [WI-006](WI-006-control-panel-modules.md) module pattern and the
  established hash-bound plan/apply contract (as extended for multi-resource
  fingerprints in [WI-049](WI-049-file-station.md)). The certificate module (if
  scoped later) owns cert lifecycle; this module only references certificate ids.
