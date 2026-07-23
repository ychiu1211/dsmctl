---
id: WI-099
title: Gateway OAuth and TLS-posture hardening
status: proposed
priority: P2
owner: ""
depends_on: [WI-098]
parallel_group: G
touches:
  - internal/gateway/state/oauth.go
  - internal/gateway/state/types.go
  - internal/gateway/oauth/handler.go
  - internal/gateway/state/policy.go
  - cmd/dsmctl-gateway/main.go
---

# WI-099 — Gateway OAuth and TLS-posture hardening

## Provenance

Design-review follow-up (2026-07-22 adversarial review, re-validated against
`cc8d160`). All five findings still hold; none exploitable as a direct
third-party compromise in the loopback + nginx deployment, but each is a
standards-alignment or robustness gap worth closing as a batch.

## Outcome

Dynamic client registration cannot be weaponised to lock out onboarding, the
registration rate-limit works behind a reverse proxy, refresh-token theft is
detectable, an operator publishing plaintext on a non-loopback address is warned,
and the approval path uses the same injectable clock as its siblings.

## Scope

1. **DCR exhaustion + no admin reclamation.** `RegisterOAuthClient`
   (`internal/gateway/state/oauth.go` ~line 92) enforces a hard cap
   `MaxOAuthClients = 128` (`state/types.go` ~line 26); `OAuthClient` records have
   no expiry or last-used stamp, and the only repository methods are register and
   get-by-id, so an anonymous burst of 128 registrations permanently blocks new
   clients with DB-surgery the only recovery. Add `ListOAuthClients` +
   `DeleteOAuthClient` (repository + an admin endpoint/UI), and age out clients
   with no live access/refresh token (a TTL / last-used stamp pruned before the
   cap bites).
2. **Registration rate-limit collapses behind the proxy.** The `/oauth/register`
   limiter keys on `remoteKey()` (`oauth/handler.go` ~line 729, used at ~line 185;
   the local-login limiter at ~line 270 shares it), which uses `req.RemoteAddr` —
   always `127.0.0.1` behind nginx, so the intended per-IP 30/min becomes one
   global bucket. Plumb the parsed `trustedProxies` into `gatewayoauth.Options`
   and reuse `server.go`'s `clientIP()` inside `remoteKey`, keying on the real
   client IP when the peer is a trusted proxy and falling back to `RemoteAddr`
   otherwise. (Depends on / shares the plumbing added by WI-098.)
3. **Refresh-token reuse detection.** `RefreshOAuthTokenSet`
   (`state/oauth.go` ~line 175; rotation ~lines 200–233) rotates correctly
   (old digest deleted ~line 223) but a replay of a superseded token just returns
   `ErrOAuthUnauthorized` (~lines 204–206) without revoking the live family. Add a
   stable lineage/family id seeded at `IssueOAuthTokenSet` and carried across
   rotations; on a well-formed refresh whose digest is absent but whose lineage is
   still active, revoke the family (reuse `deleteOAuthRefreshTokensForMCPToken`
   ~line 243), per OAuth 2.1 / refresh-reuse BCP. The absolute 365-day TTL already
   bounds worst-case damage.
4. **Non-loopback plaintext startup warning.** The gateway only ever serves plain
   HTTP (`cmd/dsmctl-gateway/main.go` ~line 217 `net.Listen("tcp")`, ~line 233
   `Serve`); nothing warns when `--listen` is a non-loopback address. After the
   listener binds, inspect `listener.Addr()`; if the bound IP is not loopback and
   no `https` `--admin-public-url` (or an explicit behind-TLS acknowledgement) is
   set, emit a prominent `logger.Warn` that traffic is unencrypted. Optionally
   accept `--tls-cert`/`--tls-key` for direct `ServeTLS`; the minimum is the
   warning. (In-process TLS stays optional — proxy termination remains the model.)
5. **Clock-seam consistency.** `AdmitRemoteApply` (`state/policy.go` ~line 594)
   uses `time.Now().UTC()` while every other approval path uses the injectable
   `r.now()` seam. Switch it to `r.now().UTC()` and add a frozen-clock regression
   test for approval expiry/consumption. (`appendAuditTx` ~line 642 and the
   `pruneAudit` cutoff ~line 665 also use `time.Now()` — a lower-priority
   follow-up, note but not required here.)

## Non-goals

- Making DCR authenticated (it stays open per the MCP OAuth model); the fix is
  reclamation + aging, not closing registration.
- Mandatory in-process TLS (proxy termination stays the deployment model).
- Forwarded-header origin trust (WI-098) and token-grant defaults (WI-097),
  though item 2 reuses WI-098's trusted-proxy plumbing.

## Design constraints

- Reuse existing machinery: the `TrustedProxies`/`clientIP` helper (item 2), the
  approval-TTL cap pattern, and `deleteOAuthRefreshTokensForMCPToken` (item 3).
- Digest-only credential storage, the stateless MCP transport, and the
  bbolt-transactional state model are unchanged; item 1/3 add fields and methods,
  not a new store.
- Bounded and GC'd: any lineage tombstone or last-used stamp must be pruned on
  revocation/expiry so it cannot grow unbounded.

## Acceptance criteria

- [ ] An admin can list and delete OAuth client registrations, and inert clients
      age out before the 128 cap can be exhausted; a test proves a full-cap state
      is recoverable without DB surgery.
- [ ] Behind a configured trusted proxy, the register/login rate-limit keys on the
      real client IP (distinct buckets per client); a test covers proxied vs
      direct.
- [ ] Replaying a rotated refresh token revokes the live token family; a test
      covers rotate → replay → family-revoked.
- [ ] Binding `--listen` to a non-loopback address with no https public URL emits
      a startup warning; loopback stays silent.
- [ ] `AdmitRemoteApply` uses `r.now()`; a frozen-clock test locks approval expiry
      semantics. Existing gateway tests stay green.

## Verification

- `go test ./internal/gateway/... -count=1`.
- Manual: fill the client cap, confirm admin prune restores onboarding; bind a
  non-loopback listener and confirm the warning.
- No live DSM interaction required.

## Coordination

- Item 2 shares trusted-proxy plumbing with WI-098; land WI-098 first or
  co-develop the shared `Options.TrustedProxies` wiring.
- `state/oauth.go`, `oauth/handler.go`, and `main.go` are shared with the active
  gateway stream; re-verify line references before editing.

## Handoff

Fill this only when pausing incomplete work.
