---
id: WI-098
title: Gateway forwarded-header trust — gate X-Forwarded-* on trusted proxies
status: proposed
priority: P1
owner: ""
depends_on: []
parallel_group: G
touches:
  - internal/gateway/server.go
  - internal/gateway/oauth/handler.go
  - internal/gateway/admin/auth_http.go
  - cmd/dsmctl-gateway/main.go
---

# WI-098 — Gateway forwarded-header trust: gate X-Forwarded-* on trusted proxies

## Provenance

Design-review follow-up (2026-07-22 adversarial review, re-validated against
`cc8d160`). All three findings still hold; none were fixed by the recent gateway
rework. Severity is low-to-medium: **not** a third-party exploit in the current
loopback-bind + nginx topology (metadata is per-request and only poisons the
caller's own response), but a real defense-in-depth gap and an inconsistency with
the codebase's own trusted-proxy handling.

## Outcome

The gateway derives its advertised origin (OAuth issuer, authorization/token/
registration endpoints, protected-resource `resource`, the `WWW-Authenticate`
`resource_metadata` URL, and the admin cookie path/redirect) from forwarded
headers **only** when the request comes from a configured trusted proxy, exactly
as request-log client-IP extraction already does — and never from a direct or
untrusted peer.

## Scope

- **Single choke point for forwarded-header trust.** `clientIP()`
  (`internal/gateway/server.go` ~line 566) already gates `X-Forwarded-For` on
  `addressTrusted(remote, TrustedProxies)` (~line 589, keyed off
  `Options.TrustedProxies`). The origin-deriving paths do **not**: `externalBase()`
  (`internal/gateway/oauth/handler.go` ~lines 541–561) and `externalOrigin()`
  (`internal/gateway/admin/auth_http.go` ~lines 94–110) read
  `X-Forwarded-Proto`/`-Host`/`-Prefix` ungated. Add a server-level middleware
  (alongside `correlateAndLog`, `server.go` ~line 522) that strips these three
  headers whenever `!addressTrusted(remoteAddr, trustedProxies)`, so both
  `externalBase` and `externalOrigin` naturally fall back to `req.TLS + req.Host`
  for untrusted peers and honour the headers only behind a real proxy. This
  requires plumbing the parsed `trustedProxies` to the point where the middleware
  runs (it already exists for `clientIP`).
- **Validate a trusted X-Forwarded-Host against AllowedHosts.**
  `protectHostAndOrigin` (`server.go` ~lines 423–450) validates only
  `normalizeHost(req.Host)` against `allowedHosts`; a forwarded host that a
  trusted proxy legitimately sets is never checked. When a trusted proxy sets
  `X-Forwarded-Host`, validate it against the same `allowedHosts` set before it
  is allowed to define the external origin, so the allowlist actually constrains
  the advertised hostname.
- **Do not append X-Forwarded-Prefix when the origin is pinned.** In
  `externalBase()` the prefix read/append (~lines 557–561) sits **outside** the
  `if origin == ""` block, so even with `--admin-public-url` set (which pins
  scheme+host; a path is rejected at `handler.go` ~line 101) a caller-supplied
  `X-Forwarded-Prefix` is still appended. Move the prefix logic inside the
  `origin == ""` branch (pinned origin returned as-is), and give operators who
  need a prefix under a pinned origin a first-class way to express it (either
  relax the pinned-URL parser to accept a path segment used as the prefix, or a
  separate `--admin-public-prefix`). The second unconditional prefix consumer,
  `adminRootRedirect` (`server.go` ~lines 184–188), must be covered by the same
  trusted-proxy gate.

## Non-goals

- Requiring `--admin-public-url` in all deployments (it stays optional; gating
  the headers is what closes the gap). Recommending it for reverse-proxy
  production is a docs note in WI-096.
- TLS termination / non-loopback startup warning (WI-099).
- Token defaults, scopes, and lifetimes (WI-097).

## Design constraints

- Reuse the existing `addressTrusted`/`TrustedProxies` machinery — do not add a
  second forwarded-header parser.
- With no trusted proxies configured, behaviour for a direct client is
  `req.TLS + req.Host` (the safe default); the change must not break the
  documented loopback + nginx deployment where nginx is the trusted proxy.
- Purely request-path hardening; the application/facade boundary and the
  compatibility selector are untouched.

## Acceptance criteria

- [ ] `X-Forwarded-Proto`/`-Host`/`-Prefix` from a non-trusted peer are ignored;
      `externalBase()`/`externalOrigin()` fall back to `req.TLS + req.Host`. A
      test drives a request with spoofed forwarded headers from an untrusted
      remote and asserts the advertised origin ignores them.
- [ ] A trusted proxy's `X-Forwarded-Host` is honoured only if it is in
      `allowedHosts`; a non-allowlisted forwarded host is rejected or falls back.
- [ ] With `--admin-public-url` set, a caller-supplied `X-Forwarded-Prefix` does
      not alter the advertised endpoints.
- [ ] Existing gateway request/host tests stay green; new behaviour is tested for
      both the OAuth and admin origin paths.

## Verification

- Unit tests for `externalBase`/`externalOrigin` with trusted vs untrusted
  `RemoteAddr` and spoofed headers.
- Confirm the documented nginx deployment (nginx in `TrustedProxies`) still
  advertises the correct external origin and `/dsmctl` prefix.
- No live DSM interaction required.

## Coordination

- `server.go`, `oauth/handler.go`, and `admin/auth_http.go` are shared with the
  active gateway stream (WI-091/WI-092 add DSM-delegated login on these paths);
  coordinate and re-verify line references before editing.
- Complements WI-096. WI-099 consumes the trusted-proxy/client-IP plumbing from
  this item and therefore depends on WI-098 as a complete batch, although its
  unrelated clock and state subchanges can be developed separately.

## Handoff

Fill this only when pausing incomplete work.
