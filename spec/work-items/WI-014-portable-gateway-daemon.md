---
id: WI-014
title: Establish the portable HTTP gateway daemon
status: in_progress
priority: P0
owner: "gateway"
depends_on: []
parallel_group: F
touches:
  - cmd/dsmctl-gateway
  - internal/gateway
  - internal/mcpserver
  - internal/application
  - internal/runtime
  - deploy/container
  - docs/gateway.md
---

# WI-014 - Establish the portable HTTP gateway daemon

## Outcome

An amd64 Linux host can run the existing dsmctl MCP tools through a hardened,
platform-neutral Streamable HTTP gateway container while the current stdio
server continues to work. MCP transport sessions are stateless, but each NAS
profile retains its independent DSM client and session.

## Scope

- Add a dedicated `dsmctl-gateway` long-running entry point without folding
  HTTP process concerns into the Synology facade or existing CLI.
- Serve `/mcp`, `/healthz`, and `/readyz`; reserve `/admin` for WI-015.
- Use the SDK Streamable HTTP handler in stateless/JSON-response mode.
- Add HTTP server limits, graceful shutdown, request correlation, redacted
  structured logs, Origin/Host protection, and configurable trusted proxies.
- Add a gateway-local concurrency limiter: 32 configured profiles maximum and
  8 concurrent cross-profile operations by default.
- Build one `linux/amd64`, `CGO_ENABLED=0` container image with a non-root user,
  read-only root filesystem support, bounded tmpfs, no shell requirement, and
  health check.
- Provide a development Compose file that publishes only to host loopback and
  mounts `/data` plus read-only secret placeholders.
- Document that the core image contains no DSM paths, commands, SSO, package
  lifecycle, Container Manager calls, or Docker socket access.

## Non-goals

- Dynamic profile mutation, persistent database migrations, package vault, or
  browser administration; those are WI-015.
- Remote apply authorization, approval records, and audit retention; those are
  WI-016. Developer HTTP builds expose read-only tools only.
- Synology SPK construction, DSM reverse proxy registration, or offline image
  preload; those are WI-017.
- SSE resumption, server-to-client MCP requests, durable MCP sessions, OIDC,
  ARM images, or a multi-architecture manifest.

## Design constraints

- Follow `spec/gateway-deployment.md` and preserve the shared
  CLI/MCP/application/runtime/Synology dependency direction.
- HTTP authentication and policy wrap MCP execution; MCP handlers remain thin
  and never build DSM requests.
- Liveness is local-only. Readiness may inspect configuration and local state
  but does not contact all configured NAS devices.
- Default HTTP mode fails closed when required remote authentication policy is
  absent. An explicit development read-only mode may use a generated local
  credential but may not register apply tools.
- Per-NAS session serialization and one retry after DSM session expiry remain
  owned by the runtime and Synology client.

## Acceptance criteria

- [ ] `dsmctl-gateway` starts and stops cleanly and the existing
      `dsmctl-mcp` stdio tests continue to pass unchanged.
- [ ] `/mcp` successfully initializes and calls `list_nas` and a read-only DSM
      fake through Streamable HTTP in stateless mode.
- [ ] The server rejects invalid Host/Origin, oversized requests, disallowed
      content types, and requests exceeding concurrency limits.
- [ ] Shutdown stops accepting new requests, drains bounded in-flight work,
      and closes all cached DSM sessions.
- [ ] `/healthz` does not contact DSM; `/readyz` fails when required local
      configuration or secret mounts are invalid.
- [ ] A `CGO_ENABLED=0`, `linux/amd64` image builds and runs as non-root with a
      read-only root filesystem and no Docker socket.
- [ ] The development Compose file binds the backend to `127.0.0.1` only.
- [ ] No image or runtime code refers to `/usr/syno`, `/var/packages`,
      `SYNOPKG_*`, `authenticate.cgi`, or Container Manager.
- [ ] Gateway documentation explains MCP-stateless versus DSM-stateful
      sessions and states that HTTP developer mode is read-only.

## Verification

- `go test ./... -count=1` and `go vet ./...`.
- HTTP handler tests use `httptest` and fake application/runtime dependencies.
- Build and inspect the amd64 image; run it with read-only rootfs, all
  capabilities dropped, no-new-privileges, and loopback-only publication.
- No live DSM mutation is permitted. Optional read-only tests may contact an
  explicitly configured NAS.

## Coordination

This item touches `internal/mcpserver` and `internal/application`, which also
appear in WI-013. WI-013 is complete as of commit `4303e36`, and its completion
metadata is recorded in `0aa0b7e`; there is no remaining active overlap. Prefer
new gateway packages and entry points over restructuring existing management
tools.

## Handoff

Fill this only when pausing incomplete work.
