---
id: WI-014
title: Establish the portable HTTP gateway daemon
status: done
priority: P0
owner: ""
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

- [x] `dsmctl-gateway` starts and stops cleanly and the existing
      `dsmctl-mcp` stdio tests continue to pass unchanged.
- [x] `/mcp` successfully initializes and calls `list_nas` and a read-only DSM
      fake through Streamable HTTP in stateless mode.
- [x] The server rejects invalid Host/Origin, oversized requests, disallowed
      content types, and requests exceeding concurrency limits.
- [x] Shutdown stops accepting new requests, drains bounded in-flight work,
      and closes all cached DSM sessions.
- [x] `/healthz` does not contact DSM; `/readyz` fails when required local
      configuration or secret mounts are invalid.
- [x] A `CGO_ENABLED=0`, `linux/amd64` image builds and runs as non-root with a
      read-only root filesystem and no Docker socket.
- [x] The development Compose file binds the backend to `127.0.0.1` only.
- [x] No image or runtime code refers to `/usr/syno`, `/var/packages`,
      `SYNOPKG_*`, `authenticate.cgi`, or Container Manager.
- [x] Gateway documentation explains MCP-stateless versus DSM-stateful
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

## Completion record

- Completed on 2026-07-17 in implementation commit `ae1deb3`; this completion
  metadata records the subsequently executed container verification.
- `go test ./... -count=1` and `go vet ./...` passed. Focused Streamable HTTP
  tests initialized MCP, called `list_nas`, and called `get_system_info`
  against an in-process fake DSM.
- Docker Desktop 4.82.0 / Engine 29.6.1 on WSL 2.7.10 built image
  `sha256:13a8cf79bbfb3e2351812c84a553b99dc58f4c1edbf15a7c5115bc4823006845`
  from `deploy/container/Dockerfile` for `linux/amd64`. The scratch image was
  4,362,856 bytes, configured as UID/GID `10001:10001`, and had the expected
  binary-only entrypoint and health check.
- A real container ran healthy with a read-only root filesystem, all
  capabilities dropped, `no-new-privileges`, bounded tmpfs/process/memory/CPU,
  bridge networking, read-only config/secret mounts, and publication only on
  `127.0.0.1:18765`. Inspect showed no Docker socket mount; the process ran as
  UID 10001 and `/bin/sh` was absent.
- Runtime probes returned health/readiness 200, missing auth 401, invalid
  Host/Origin 403, invalid content type 415, MCP protocol `2025-06-18`, no
  `plan_*` or `apply_*` tools, and a successful empty `list_nas` result.
  SIGTERM shutdown completed with exit code 0 and no OOM. The test container
  and mounted test config/token were removed; no live DSM call or mutation was
  performed. The locally built `dsmctl-gateway:wi014` image remains available
  for inspection.
