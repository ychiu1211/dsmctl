---
id: WI-041
title: External Access module (Synology Account, QuickConnect, DDNS)
status: in_progress
priority: P2
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/externalaccess
  - internal/synology/operations/externalaccess
  - internal/synology/externalaccess.go
  - internal/runtime/manager.go
  - internal/application/external_access.go
  - internal/cli/external_access.go
  - internal/mcpserver/server.go
  - docs/control-panel.md
---

# WI-041 — External Access module (Synology Account, QuickConnect, DDNS)

## Outcome

A CLI user or MCP agent can read the Control Panel → External Access surface —
Synology Account binding status, QuickConnect state, DDNS records, and the
router/port-forwarding view — and, through the hash-bound plan/apply contract,
change the exposure-changing settings under guardrails. This is a focused
Control Panel module in the sense of [WI-006](WI-006-control-panel-modules.md):
one typed module per DSM setting area, never a generic `set key=value` proxy.

The API map, versions, live-read shapes, and authoritative set fields below were
established against the lab (DS3018xs, DSM 7.3-81168): reads were live-verified
with a throwaway read-only probe; set fields were taken from the DSM WebAPI conf
and C++ handlers on codesearch (`webapi-QuickConnect`, `webapi-DDNS`,
`webapi-MyDSCenter`) and still need the standard impl-time confirmation
(see [[dsm-webapi-live-verify-fields]] / [[dsm-external-access-webapi-map]]).

## Scope

Sliced read-first, then guarded write, so the read slice can ship independently.

### Slice A — read-only (all live-verified)

- **Synology Account (MyDS):** `SYNO.Core.MyDSCenter` v2 `query` →
  `{account, activated, is_logged_in}`; `SYNO.Core.Package.MyDS` v1 `get` →
  `{myds_id, serial, ds_major/minor/build}`. `auth_key` is an account token and
  is **never** decoded into the state model.
- **QuickConnect:** `SYNO.Core.QuickConnect` v2 `get` →
  `{enabled, server_alias, region, domain, ddns_domain, server_id,
  myds_account}`; v3 `get_misc_config` → `{relay_enabled}`; v1 `status` →
  `{status, alias_status}`. `SYNO.Core.QuickConnect.Permission` v1 `get` →
  per-service exposure (`mobile_apps`, `cloudstation`, `file_sharing`,
  `dsm_portal`). `SYNO.Core.QuickConnect.Upnp` v1 `get` →
  `{enabled, force_enabled, whitelist}`.
- **DDNS:** `SYNO.Core.DDNS.Record` v1 `list` → `{records, next_update_time}`;
  `SYNO.Core.DDNS.Provider` v1 `list` (provider catalog);
  `SYNO.Core.DDNS.ExtIP` v2 `list` → detected WAN IP(s).
- **Router / port forwarding (read view):** `SYNO.Core.PortForwarding.Rules` v1
  `load`, `SYNO.Core.PortForwarding.RouterConf` v1 `get`.

### Slice B — guarded write (plan/apply, hash-bound)

- **DDNS record CRUD** — `SYNO.Core.DDNS.Record` `create` / `set` / `delete`
  (keyed by `id`). Authoritative fields (from `webapi-DDNS/src/webapi-DDNS.h`):
  `provider`, `hostname`, `username`, `passwd`, `enable`, `heartbeat`, `ipv6`.
- **QuickConnect enable / alias / region / relay** — `SYNO.Core.QuickConnect`
  `set` (`enabled`, `server_alias`, `region`), `set_server_alias`
  (`server_alias`, `region`), and v3 `set_relay_enable` (`relay_enabled`).
- **QuickConnect per-service exposure** — `SYNO.Core.QuickConnect.Permission`
  `set` (toggle which services are reachable externally).

## Non-goals

- **QuickConnect ID as a dsmctl *connection target*.** Reaching a NAS from
  outside *by* its QuickConnect ID (rather than a direct URL) requires
  implementing Synology's client-side resolution + relay handshake, not a
  Control-Panel config call. It is a connection-layer feature and belongs in its
  own work item — see "QuickConnect as transport" below. This WI only *manages*
  QuickConnect settings on an already-reachable NAS.
- **Binding / unbinding a Synology Account** (`SYNO.Core.MyDSCenter.Login` /
  `.Logout` / `.Purchase`). These are account-lifecycle actions with side effects
  well beyond a settings patch; deferred.
- **Registering a `*.synology.me` hostname** (`SYNO.Core.DDNS.Synology`
  `register_hostname`) and third-party-provider account provisioning; DDNS record
  management assumes the provider identity already exists.
- **Port-forwarding rule writes and router pairing**
  (`SYNO.Core.PortForwarding.Rules` set, `RouterConf` set, UPnP/NAT-PMP setup).
  Read-only in this WI; writing them reconfigures the user's router.
- **Certificate management** (`SYNO.Core.Certificate.*`), reverse proxy
  (`SYNO.Core.AppPortal.ReverseProxy`), and the DSM external-web setting
  (`SYNO.Core.Web.DSM.External`).

## Design constraints

- **Independent compatibility boundaries.** Synology Account, QuickConnect, and
  DDNS are separate API families and separate failure boundaries: a NAS missing
  one (e.g. QuickConnect disabled at the account level) must leave the others
  usable, reported `(not supported)` rather than erroring the whole module.
  QuickConnect's own version split is real — v1/v2 differ in `get` shape
  (`myds_account` appears at v2), and relay lives only at v3.
- **Secrets never enter requests/plans/logs.** A DDNS record's `passwd` and
  QuickConnect enable's account password must use the existing
  `credential_ref: env:NAME` mechanism (as user-password changes do), resolved at
  apply time and absent from the request, plan, hash, result, and logs.
  `auth_key`, `server_id`, and account tokens are never surfaced by display
  models.
- **Every write is high risk — it changes external exposure.** Enabling
  QuickConnect, enabling relay, adding a DDNS record, or broadening
  QuickConnect.Permission all make the NAS (more) reachable from the public
  internet; disabling them drops remote clients. Classify all Slice-B mutations
  high; there is no "medium" externally-facing toggle here.
- **Patch + postcondition.** Follow the module pattern: plan records and hashes
  the complete current state, apply rejects a changed state, merges the patch
  into a freshly read config, and re-reads to verify the requested fields
  actually took effect (DSM silently ignores some fields — the recurring lesson).
- **DDNS record set semantics need impl-time confirmation.** The lab has no
  configured record, so the record entry shape (`create` vs `set` field
  symmetry, whether `id` is server-assigned) must be confirmed with one
  authorized, fully-reverted live create/delete before Slice B ships.

## Acceptance criteria

- [x] Slice A: `external-access capabilities|account|quickconnect|ddns` (CLI) and
      the matching `get_*` MCP tools return normalized state; `auth_key` and
      account tokens are provably absent from all output (unit test asserts the
      decoded account never carries the token; live `--json` grep confirms it).
- [x] Independent gating: each area selects its own backend, and QuickConnect
      relay/status/permission are skipped (null) when their versioned APIs are
      absent; a missing area's API does not disable the others.
- [x] Slice A live verification on the DSM 7.3 lab: read account, QuickConnect
      (enabled, id `derekchiu3018`, relay on, connected, four exposed services),
      DDNS (no records, WAN address), and port forwarding (no router, no rules)
      with no token leak.
- [x] Port-forwarding read view (`SYNO.Core.PortForwarding.Rules` load,
      `RouterConf` get) added to the read module as a fourth independent area.
- [x] Slice B (first write): QuickConnect **relay** toggle via guarded
      hash-bound plan/apply, with request-capture test and postcondition re-read;
      classified high risk; read-only gateway excludes the plan/apply tools.
- [x] Slice B live verification on the DSM 7.3 lab (authorized, fully reverted):
      relay off→on round-trip through plan/apply with postcondition proof; the
      live apply caught the wrong wire method (`set_relay_enable` → the real
      `set_misc_config`, the symmetric setter of `get_misc_config`).
- [ ] Remaining Slice B: DDNS record create/delete (registers a real external
      hostname; needs a throwaway test hostname) and QuickConnect enable/alias,
      with `credential_ref` for the DDNS `passwd` / account password.

## Verification

- Decoder + request-capture unit tests; `go test ./... -count=1`, `go vet ./...`.
- Live read now (unauthenticated `SYNO.API.Info` + authenticated session reads);
  live reverted write requires explicit per-session authorization.
- Source of truth for fields: `webapi-QuickConnect/conf/SYNO.Core.QuickConnect.py`
  + `src/`, `webapi-DDNS/conf/SYNO.Core.DDNS.py` + `src/webapi-DDNS.{cpp,h}`
  (branch `DSM7-3-new`), `webapi-MyDSCenter`.

## Coordination

- Shares `internal/domain/controlpanel` and `internal/synology/controlpanel.go`
  with the other Control Panel modules (parallel group C). New operation package
  under `internal/synology/operations/externalaccess`; no overlap with
  file-services or time modules beyond the shared facade.

## Related: "QuickConnect as transport" (separate future WI)

Distinct from this module: letting dsmctl *connect* to a NAS given only a
QuickConnect ID. The authoritative client-side spec is
`libsynoclientconn/docs_src/qc-server-protocol.md` ("single source of truth for
the QuickConnect Control Server HTTPS API"). Flow:

1. `get_site_list` / `get_server_info` — POST `{version:1, command:"get_server_info",
   id, serverID:<qc-id>}` to `https://global.quickconnect.to/Serv.php`, then to
   the region site (`https://<site>/Serv.php`).
2. Response yields connection candidates: `server.external.ip[/ipv6]` (WAN),
   `server.fqdn`, `server.ddns` / top-level `ddns` (e.g. `my.synology.me`),
   `smartdns`, and LAN interface addresses.
3. Client tries direct candidates, else `request_tunnel` → relay region → builds
   the relay `host:port` (hole-punch fallback:
   `android-sylibx-holepunch`).

This is a non-trivial connection-layer capability (new resolver + relay client),
not a Control-Panel setting, and should be scoped on its own if pursued.
