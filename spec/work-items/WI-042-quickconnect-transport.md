---
id: WI-042
title: QuickConnect ID as a dsmctl connection target
status: proposed
priority: P3
owner: ""
depends_on: []
parallel_group: H
touches:
  - internal/config/config.go
  - internal/runtime/manager.go
  - internal/synology/quickconnectresolve
  - internal/cli/nas.go
  - docs/quickconnect-transport.md
---

# WI-042 — QuickConnect ID as a dsmctl connection target

## Outcome

dsmctl can reach a NAS given only its QuickConnect ID (for example
`derekchiu3018`) instead of a directly routable `https://host:port` URL, by
resolving the ID to a reachable endpoint before the existing web-login and
client machinery runs. This is a **connection-layer** capability, distinct from
the read/write *management* of QuickConnect settings in
[WI-041](WI-041-external-access.md): WI-041 configures QuickConnect on a NAS you
can already reach; WI-042 is about reaching a NAS through it.

## Background — the resolution protocol

Authoritative client-side spec:
`git.synology.inc/synology/libsynoclientconn/docs_src/qc-server-protocol.md`
("single source of truth for the QuickConnect Control Server HTTPS API";
server side lives in `CloudService-Control`). Reference client implementations:
`SurveillanceStationClient/lib/ConnectionFinder/QuickConnect.cpp`,
`{OrangeDrive,presto,MangoDrive-Client}/…/autoconn.cpp`,
`Android-Lib/…/syhttp/relay`, `android-sylibx-holepunch`,
`Windows-SynoUtils/SynoRelay`, and `NoteStationClipper/…/quickconnect.ts`.

Three coordinator calls, all `POST https://<host>/Serv.php` with a JSON body:

1. **Tier-0 `get_site_list`** (`{version:1, command:"get_site_list"}` to
   `global.quickconnect.to` — `.cn` for the China region) → the region control
   sites to query next.
2. **Tier-1 `get_server_info`** (`{version:1, command:"get_server_info",
   id:"<ServiceId, e.g. dsm_https>", serverID:"<qc-id>"}`) → candidate
   connection addresses for the NAS: `server.external.ip`/`ipv6` (WAN), LAN
   interface addresses, `server.fqdn`, `server.ddns` / top-level `ddns`
   (e.g. `my.synology.me`), `smartdns`, the service `port`, and a
   `udp_punch_port` (5353) for hole-punch.
3. **Tier-2 `request_tunnel`** (same shape, `command:"request_tunnel"`) → a
   relay region the client uses to build the relay `host:port`; used only when
   every direct candidate fails.

Connection candidate ordering (doc §4): try **Direct** candidates first — LAN,
then WAN/external, then DDNS/FQDN, then smartdns — and fall back to **Relay**
(and hole-punch) only when Direct fails.

## Scope

Sliced so the tractable Direct path can ship without the relay/tunnel client.

### Slice A — Direct resolution

- A `quickconnectresolve` package implementing Tier-0/Tier-1 (`get_site_list`,
  `get_server_info`) against the coordinator, returning the ordered Direct
  candidate endpoints (LAN → WAN → DDNS/FQDN → smartdns) with the service port.
- Config: a profile may specify a QuickConnect ID and region instead of a `url`
  (for example a `quickconnect_id` field, or a `qc://<id>` URL scheme the
  resolver expands). Exactly one of `url` / `quickconnect_id` is required.
- Runtime integration: the manager resolves the ID to a base URL once, probes
  the ordered candidates for reachability, and hands the first reachable
  `https://host:port` to the **unchanged** existing web-login + client + session
  machinery.

### Slice B — Relay fallback (stretch, likely its own WI)

- `request_tunnel` + a relay client that tunnels TLS to the NAS through the
  Synology relay, plus optional UDP hole-punch (`udp_punch_port`). This is a
  substantial transport component (NAT traversal, relay framing) and should be
  re-scoped after Slice A.

## Non-goals

- Managing QuickConnect settings (that is WI-041).
- The relay/hole-punch transport in the first slice (Slice B / follow-up WI).
- Non-DSM QuickConnect services (only the DSM control endpoint is targeted).
- Bundling the resolver into the read-only gateway's remote surface without a
  separate security review (it makes outbound calls to a third-party coordinator
  and would let a remote caller trigger them).

## Design constraints

- **A third party enters the connection bootstrap.** Resolving an ID contacts
  Synology's coordinator (`global.quickconnect.to`), disclosing the QC ID and
  the client's IP to an external service. dsmctl's current model assumes a
  direct endpoint the operator controls; this must be an explicit opt-in per
  profile, never a silent fallback, and it fails closed when the coordinator is
  unreachable (offline/air-gapped deployments simply cannot use it).
- **TLS identity must stay verifiable.** Direct candidates present the NAS's
  certificate for `<id>.<domain>` (relay tunnels TLS end-to-end to the NAS).
  Chain verification against the resolved hostname is the default; the
  pinned-fingerprint TLS mode (now the default, commit `0a4539a`) interacts
  awkwardly with a dynamically chosen endpoint and needs an explicit story
  (pin the NAS leaf regardless of which candidate connected, or require standard
  verification for QC profiles).
- **The protocol is source-only and unversioned publicly.** Model the request
  and response shapes from `libsynoclientconn` and live-verify the resolution
  against a real QC ID before trusting them; treat coordinator responses as
  untrusted input (validate candidate addresses, never follow an address family
  or port dsmctl did not expect).
- **No secret in a URL/query.** The QC ID and any resolution parameters go in the
  POST body, matching the reference clients; the ID is not a secret but the
  resolution must not leak session material.
- **Reuse, don't fork.** After resolution yields a base URL, nothing downstream
  changes: the same `weblogin`, `synology.Client`, and session store are used.

## Acceptance criteria

- [ ] `quickconnectresolve` performs Tier-0/Tier-1 and returns ordered Direct
      candidates with the service port; coordinator responses are validated.
- [ ] A profile can be configured with a QuickConnect ID + region; the manager
      resolves it to a reachable base URL and the existing login/client path runs
      unchanged.
- [ ] Opt-in and fail-closed: no silent coordinator calls; a resolution failure
      is a clear, actionable error; the TLS-verification story for QC endpoints
      is decided and enforced.
- [ ] Live verification: resolve a real QC ID (the lab's `derekchiu3018`) to its
      Direct candidates and complete a `system info` read over the resolved
      endpoint.
- [ ] Relay/hole-punch explicitly out of this slice, tracked for a follow-up.

## Verification

- Unit tests with recorded coordinator fixtures (validate parsing + candidate
  ordering + rejection of malformed/unsafe addresses).
- Live resolution against the lab's QuickConnect ID `derekchiu3018` (region
  `tw`), then a read over the resolved endpoint. No mutation, so no special
  write authorization is required.

## Coordination

- Independent of WI-041 (different layer). Touches the connection bootstrap
  (`internal/config`, `internal/runtime`) rather than a Control Panel module;
  new group H alongside LAN discovery, which is the other session-less
  connectivity feature.
