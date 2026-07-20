---
id: WI-069
title: Network interfaces, bonding and routing
status: proposed
priority: P2
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/network
  - internal/synology/operations/network
  - internal/synology/network.go
  - internal/synology/compatibility_report.go
  - internal/runtime/manager.go
  - internal/application/network.go
  - internal/cli/network.go
  - internal/cli/root.go
  - internal/mcpserver/server.go
  - internal/mcpserver/read_only.go
  - docs/network.md
---

# WI-069 — Network interfaces, bonding and routing

## Outcome

A CLI user or MCP agent can read the Control Panel → Network surface — general
settings (hostname, DNS servers, default gateway, IPv6), per-NIC interface
configuration (IP/DHCP/netmask/MTU/jumbo/link status), link-aggregation bonds,
static routes, and (best-effort) traffic-control shaping — and, through the
hash-bound plan/apply contract, change the connectivity-affecting settings under
strict guardrails. This is a focused module in the sense of
[WI-006](WI-006-control-panel-modules.md): one typed module per DSM setting
area, never a generic `set key=value` proxy.

Network is a **core DSM surface** (discovered via `SYNO.API.Info`), so there is
no installed-package gate; each network area is still its own independent
compatibility boundary.

**This module carries an unusually high blast radius.** The interface that
carries dsmctl's own management session runs over one of the very NICs, bonds,
gateways, and routes this module edits. A wrong write can sever the session,
strand the NAS, and — for a routed/remote NAS — require physical or LAN-console
recovery. The session-severing safety model below is the defining constraint of
this work item, not a footnote.

## Scope

Sliced read-first, then guarded write, so the read slice ships independently.

### Slice A — read-only (independently shippable)

Every area selects its backend independently and reports `(not supported)`
rather than erroring the module when its API/version is absent (fail-closed).

- **General** — `SYNO.Core.Network` `get`: hostname, default gateway (IPv4 and
  IPv6), configured DNS nameservers, IPv6 enable/mode, and DHCP-supplied-DNS
  flag. Volatile/transient fields are omitted from the domain state.
- **Interfaces** — `SYNO.Core.Network.Interface` `list`/`get`: per NIC the
  logical name (`eth0`, `bond0`, `ovs_eth0`, …), MAC, IPv4 address/netmask/
  per-interface gateway, DHCP-vs-static mode, MTU (incl. jumbo-frame 9000),
  IPv6 address/prefix/mode, and link status (connected, negotiated speed,
  duplex). Read-only.
- **Bonds (link aggregation)** — `SYNO.Core.Network.Bond` `list`/`get`: each
  bond's member NICs (slaves), bonding mode (e.g. active-backup, balance-XOR,
  802.3ad/LACP, adaptive load balancing), and status. Read-only.
- **Static routes** — `SYNO.Core.Network.Router` `list` (likely; the family may
  instead be `SYNO.Core.Network.Route`/`.StaticRoute` — verify): destination
  network, netmask/prefix, gateway, egress interface, metric, and address
  family (IPv4/IPv6).
- **Traffic control (best-effort read)** — `SYNO.Core.Network.TrafficControl`
  (family may be `.Bandwidth`/`.QoS` — verify) per-interface up/down limits and
  per-service rules, exposed as a fifth independent read area. If the API is
  absent it reports `(not supported)` and does not affect the other four areas.

CLI `network capabilities|general|interfaces|bonds|routes|traffic-control`; MCP
`get_network_*`. All reads are side-effect-free.

### Slice B — guarded write (plan/apply, hash-bound, all high risk)

Every mutation rides the hash-bound plan/apply contract: plan records **and
hashes the complete observed state** of the affected area (all interfaces / all
bonds / the full route table / the general block — not just the touched field),
plus the NAS profile, canonical intent, stable resource id (e.g. `ifname`,
`bond` name, route key), observed-state fingerprint, risk, and approval hash.
Apply revalidates the plan, **re-reads and rejects stale state**, merges the
patch into a **freshly read** config, performs the typed operation, and — where
the session survives — **re-reads to verify the requested fields actually took
effect**, because DSM silently ignores some fields (the recurring lesson across
these modules).

Ownership is **patch-only**: omitted fields preserve their current value and are
never silently reset (critical here — an interface `set` that drops MTU or IPv6
config would be a connectivity change).

- **General** — `SYNO.Core.Network` `set`: hostname, DNS nameservers, default
  gateway. (Default-gateway change is session-severing when the session is
  routed through that gateway — see safety model.)
- **Interface config** — `SYNO.Core.Network.Interface` `set`: static IP /
  netmask / per-interface gateway, DHCP↔static mode, MTU / jumbo frames, IPv6
  address/mode.
- **Bond create / delete / mode** — `SYNO.Core.Network.Bond`
  `create`/`delete`/`set`: form or dissolve a bond, change member NICs, change
  bonding mode.
- **Static-route CRUD** — `SYNO.Core.Network.Router` `create`/`set`/`delete`
  keyed by the route's stable identifier.

CLI verb subcommands under `network` (`set-general`, `set-interface`,
`bond-create|bond-delete|bond-set`, `route-add|route-set|route-del`) plus generic
`network plan` / `network apply --approve`. MCP `plan_network_change` /
`apply_network_plan`. The read-only remote gateway **strips both** plan and
apply tools (see Design constraints).

## Session-severing safety model

This is the load-bearing part of the WI.

- **Identify the management interface.** The runtime/session manager knows the
  address dsmctl connected on. At plan time the module resolves which
  interface(s), bond, default gateway, and route serve that address (the NIC
  whose IP matches the session peer, plus the default-route egress). This
  resolved set is the **protected path**.
- **Ambiguous connection ⇒ treat everything as protected.** If the session
  arrived via hostname, DDNS, QuickConnect/relay, or any NATed/routed path where
  the on-NAS egress cannot be resolved unambiguously, the module fails closed and
  treats **all** interface/gateway/bond writes as session-severing.
- **Refuse protected-path mutations by default.** A plan that would change the IP,
  netmask, DHCP mode, MTU, or IPv6 config of the management interface; change or
  remove the default gateway the session uses; enslave the management NIC into a
  bond or delete a bond it belongs to; or delete/alter the route the session
  rides — is **rejected at plan time** unless an explicit override intent is
  present (CLI `--allow-session-loss`; MCP an explicit boolean intent field,
  never a default).
- **Override requires a revert story.** DSM's own web UI applies network changes
  behind a confirm-or-rollback timer ("keep these settings?" with an auto-revert
  countdown). The override apply MUST bind to that mechanism: use DSM's
  confirm/rollback method + timeout parameter if one exists (to be live-verified;
  candidate is a `SYNO.Core.Network` confirm/apply-with-timeout method), so the
  NAS auto-restores the prior config if dsmctl cannot reconnect and confirm.
  Where DSM exposes no such mechanism for the affected field, the override apply
  is a documented two-step: apply, then a **separate reconnection + confirmation
  apply** within a bounded window; if reconnection fails the operator is told the
  exact prior values to restore out-of-band. The override apply must persist the
  pre-change snapshot (the plan's hashed observed state) so recovery is exact.
- **The postcondition contract is explicitly relaxed for protected-path writes.**
  The standard "apply re-reads over the same session to verify" guarantee assumes
  the session survives; a management-interface change may kill it mid-apply. Such
  applies are documented as best-effort with the auto-revert timer as the real
  safety net, and success is confirmed only after re-establishing a session and
  reading back the new state. This exception is spelled out so no future
  maintainer assumes the normal postcondition ran.
- **Everything is high risk.** There is no "medium" network toggle here. Even a
  DNS-only or hostname-only change is classified high (it changes name resolution
  / service identity and network posture). Destructive and connectivity-severing
  changes (bond delete, gateway change, protected-path interface change) are the
  highest tier and, on the remote gateway, would require the existing single-use
  approval — except the gateway strips these tools entirely (below).

## Non-goals

- **Traffic-control / QoS writes.** Read-only in this WI; shaping rules are a
  large distinct surface — defer writes to a follow-up.
- **PPPoE, VPN client/server, IPv6 tunnels, and Wi-Fi/dongle interfaces**
  (`SYNO.Core.Network.PPPoE`, `SYNO.Core.Network.VPN.*`, `SYNO.Core.Network.
  IPv6Tunnel`, wireless). These carry auth secrets and their own lifecycles;
  out of scope here (see secrets note below for how they would be handled).
- **Firewall, port forwarding / router pairing, and DDNS/QuickConnect** — those
  live in the firewall module and in [WI-041](WI-041-external-access.md).
- **Network diagnostics** (ping/traceroute/`SYNO.Core.Network.Bond.Status`
  live counters, throughput graphs) beyond the static status fields in Slice A.
- **A generic `network set key=value` command or any raw WebAPI mutation proxy.**

## Design constraints

- **Independent compatibility boundaries.** General, Interface, Bond, Router, and
  Traffic-Control are separate API families and separate failure boundaries.
  A NAS with a single NIC (no bonding) must still read/write interfaces and
  routes; a missing bond or traffic-control API reports `(not supported)` and
  never disables the others. Selection is per operation; every operation appears
  in the capability report with a stable name, selected backend, API, and
  version.
- **API details are best-current-knowledge and MUST be live-verified.** All
  family names, versions, method names, and field names above (`SYNO.Core.
  Network`, `.Interface`, `.Bond`, `.Router`/`.Route`, `.TrafficControl`, the
  bonding-mode enum, the DHCP/MTU/IPv6 field names, and any confirm/rollback
  method) are the author's best knowledge and are **to be live-verified at
  implementation time**. Per standing policy, source-doc and mobile/Admin-Center
  client field names are frequently stale — confirm each against the lab with a
  throwaway `DSMCTL_DUMP` read probe before trusting it, and keep the
  postcondition re-read to catch silently-ignored fields.
- **Secrets never enter requests, plans, hashes, results, logs, or MCP args.**
  This module's Slice B has no password fields, but any interface-auth material
  brought in later (PPPoE username/password, 802.1X credentials, Wi-Fi PSK) MUST
  use the existing `credential_ref: env:NAME` mechanism, resolved only at apply
  time and absent from the request, plan, hash, result, and logs — mirroring the
  DDNS `passwd` handling in WI-041. MACs and IPs are configuration, not secrets,
  and may appear in state.
- **Patch-only ownership, enforced.** An interface `set` merges into a freshly
  read interface record; MTU, IPv6, and gateway fields the caller did not name
  are preserved. A test must prove an untouched field is not reset.
- **Read-only gateway excludes plan/apply.** `plan_network_change` and
  `apply_network_plan` are stripped from `NewReadOnly`; a remote read-only fleet
  caller can inspect network state but can never reconfigure a NAS's
  connectivity. Fan-out mutation stays prohibited; read fan-out (if any) stays
  opt-in and bounded.
- **Decoders are strict.** They normalize DSM shapes (numbers-as-strings,
  `additional` sub-resources, per-interface IPv6 arrays) and return an error for
  malformed shapes rather than silently returning empty successful state.

## Acceptance criteria

- [ ] Slice A: `network capabilities|general|interfaces|bonds|routes|traffic-control`
      (CLI) and `get_network_*` (MCP) return normalized state; each area selects
      its own backend and reports `(not supported)` independently; decoders
      reject malformed shapes.
- [ ] Slice A live-verified on the DSM 7.3 lab (read-only): general (hostname,
      DNS, default gateway, IPv6), all NICs with IP/DHCP/MTU/link status, any
      bonds with mode + members, the static-route table, and traffic-control if
      present — with the exact `SYNO.Core.Network.*` families/versions/fields
      confirmed via a throwaway `DSMCTL_DUMP` probe and recorded in the memory
      map.
- [ ] The plan captures and hashes the **complete** affected-area state; apply
      re-reads, rejects a stale plan, merges patch-only into a fresh read, and
      (session permitting) re-reads to verify fields took effect.
- [ ] Management-interface detection works: the module resolves the
      session-carrying NIC/gateway/route, and an ambiguous (hostname/relay/NATed)
      connection is treated as fully protected.
- [ ] A plan that mutates the protected path is **refused at plan time** without
      the explicit override; with the override it binds to DSM's confirm/rollback
      auto-revert (or the documented two-step reconnect-confirm) and persists the
      exact pre-change snapshot for recovery.
- [ ] All Slice B mutations are classified high risk; protected-path,
      bond-delete, and gateway changes are the highest tier; the read-only
      gateway strips `plan_network_change` / `apply_network_plan` (asserted by a
      gateway test).
- [ ] Request-capture tests lock every enabled DSM mutation wire shape
      (interface set, general set, bond create/delete/set, route CRUD); a test
      proves patch-only preserves an untouched field.
- [ ] No live network mutation runs without explicit per-session authorization;
      any authorized live write is performed on a **non-management** NIC/route
      only and fully reverted, with the session-severing paths exercised only in
      refuse/override-gating unit tests unless a physically recoverable target is
      explicitly provided.

## Verification

- Unit: strict decoders + malformed rejection; management-interface resolution
  (including the ambiguous-connection fail-closed case); precondition /
  fingerprint / hash + staleness; patch-only preservation; request-capture for
  every mutation shape; read-only-gateway strip assertion.
- `go build ./...`, `go vet ./...`, `go test ./... -count=1`, CLI and MCP builds
  (MCP golden tool-count updated for the new `get_network_*` +
  `plan_/apply_network_*` tools, with the `get_` prefix so the remote read-scope
  classifier recognizes the reads).
- Live reads on the configured DSM 7.3 lab. Live writes require explicit
  per-session authorization and are restricted to a non-management interface or
  a disposable static route, fully reverted; the field/method map must be
  live-confirmed (throwaway `DSMCTL_DUMP`) before any write ships, because these
  WebAPI definitions have been wrong before.

## Coordination

New packages under `internal/domain/network` and
`internal/synology/operations/network`, with facade `internal/synology/network.go`
and application `internal/application/network.go`; parallel group C alongside the
other Control Panel modules. Touches the shared adapter surface
(`internal/runtime/manager.go`, `internal/synology/compatibility_report.go`,
`internal/cli/root.go`, `internal/mcpserver/server.go`,
`internal/mcpserver/read_only.go`) — coordinate with any concurrent module adding
to the capability report, CLI root, or MCP tool registry. Overlaps in spirit with
the firewall and external-access ([WI-041](WI-041-external-access.md)) modules
(all touch network exposure) but shares no operation ownership with them.
Management-interface resolution depends on the session manager knowing the
connected address; coordinate if the connection layer changes.
