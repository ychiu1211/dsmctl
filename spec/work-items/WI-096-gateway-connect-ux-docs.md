---
id: WI-096
title: Gateway connect experience — subpath proxy docs and plaintext-transport warning
status: proposed
priority: P2
owner: ""
depends_on: []
parallel_group: G
touches:
  - deploy/linux/nginx.conf.example
  - docs/gateway.md
  - internal/gateway/admin/ui.go
---

# WI-096 — Gateway connect experience: subpath proxy docs and plaintext-transport warning

## Provenance

Design-review follow-up (2026-07-22 adversarial review of the remote-MCP connect
surface, re-validated against `cc8d160`). Both findings still hold on current
main. This is the operability slice — the confusion a real operator hit when a
gateway published at `http://<nas>/dsmctl/mcp` showed an `http` URL and it was
unclear whether a token had to be pre-created.

## Outcome

An operator putting the gateway behind a reverse proxy under a path prefix has a
documented, copyable recipe, and an operator viewing the admin page over plain
HTTP is told, at the point of need, that the bearer token will travel
unencrypted unless TLS terminates upstream.

## Scope

- Add a subpath reverse-proxy recipe. The SPK path is already covered
  (`deploy/synology/spk/conf/resource` emits `X-Forwarded-Prefix: /dsmctl` and
  `server.go` `adminRootRedirect` preserves it), but the generic proxy path is
  not: `deploy/linux/nginx.conf.example` mounts at a bare `location /` with no
  `X-Forwarded-Prefix`. Add a commented subpath variant (a `location /dsmctl/`
  block that forwards `X-Forwarded-Prefix /dsmctl` alongside the existing
  `X-Forwarded-Proto`; the generic deployment pins the external host with
  `--admin-public-url` and deliberately rewrites the upstream `Host` to
  loopback), and a short "Mounting under a subpath"
  note in `docs/gateway.md` that names `X-Forwarded-Prefix` as the required
  header and states that `--admin-public-url` intentionally rejects a path (the
  origin and the prefix are configured separately). Recommend pinning the HTTPS
  public origin in reverse-proxy production.
- Add a plaintext-transport warning in the admin UI. `mcpEndpoint()`
  (`internal/gateway/admin/ui.go`, JS helper ~line 620) builds the endpoint from
  `location.origin`, so it reflects whatever scheme the admin is browsing. When
  the computed endpoint is an `http://` origin that is **not** loopback
  (`127.0.0.1`/`localhost`/`::1`), render an inline warning next to the access
  wizard / issued-token dialog that the bearer token is sent unencrypted and the
  endpoint should be fronted by TLS. The documented loopback default
  (`http://127.0.0.1:18765/mcp`) is exempt.

## Non-goals

- Native in-process TLS, or refusing to start on a non-loopback plaintext bind
  (that startup-warning behaviour is in WI-099).
- Changing how `externalBase()` derives scheme/host/prefix or gating forwarded
  headers on trusted proxies (WI-098).
- Any change to token scopes, lifetimes, or defaults (WI-097).

## Design constraints

- Documentation and a client-side UI annotation only; no change to admission,
  the compatibility selector, or the application/facade boundary.
- The warning is presentational and must not block token creation or leak any
  secret; it keys purely off the computed endpoint scheme/host.
- Keep the loopback-http default unwarned so the standard local deployment is
  not noisy.

## Acceptance criteria

- [ ] `deploy/linux/nginx.conf.example` contains a subpath variant that forwards
      `X-Forwarded-Prefix` and the existing proto header while preserving the
      documented upstream Host rewrite, and
      `docs/gateway.md` documents the subpath requirement and the
      origin-vs-prefix split.
- [ ] The admin access wizard / issued-token dialog shows a transport warning
      when the MCP endpoint is non-loopback `http://`, and shows nothing extra
      for `https://` or loopback `http://`.
- [ ] No new server-side behaviour; existing gateway tests stay green.

## Verification

- Render the admin page over `http://<non-loopback>/dsmctl/` and confirm the
  warning appears; over `https://` and over `http://127.0.0.1:18765/` confirm it
  does not.
- Follow the new nginx recipe on a subpath mount and confirm the advertised
  OAuth metadata and `/admin/` redirect carry the `/dsmctl` prefix.

## Coordination

- `internal/gateway/admin/ui.go` is actively edited by the gateway-UI stream
  (WI-045/WI-047/WI-095); coordinate to avoid clobbering wizard markup.
- Pairs with WI-098 (forwarded-header trust) and WI-099 (TLS posture); this item
  is docs/UI only and can land independently.

## Handoff

Fill this only when pausing incomplete work.
