---
id: WI-109
title: Return protocol session expiry after Gateway restart
status: done
priority: P1
owner: ""
depends_on: [WI-033]
parallel_group: G
touches:
  - internal/gateway/server.go
  - internal/gateway/server_test.go
---

# WI-109 — Return protocol session expiry after Gateway restart

## Outcome

An MCP client holding a stateful session ID from before a Gateway restart
receives the protocol-defined HTTP 404 session-missing response and can
reinitialize, while attempts to attach a different bearer token to a live
session remain forbidden.

## Scope

- Distinguish an unknown or expired session binding from a live binding owned
  by another MCP token.
- Return HTTP 404 for the former and HTTP 403 for the latter.
- Preserve bounded session binding, audit, and cross-token isolation.

## Acceptance criteria

- [x] An unknown post-restart MCP session receives HTTP 404
      `session_not_found`.
- [x] A live session presented with another bearer-token identity still
      receives HTTP 403 `session_token_mismatch`.
- [x] The connected Codex MCP client can recover after the upgraded Gateway
      restarts.
- [x] `go test ./...`, `go vet ./...`, and SPK validation pass.

## Verification

- `go test ./internal/gateway -count=1`
- `go test ./... -count=1`
- `go vet ./...`
- Live Gateway upgrade followed by repeated `list_nas` and `get_system_info`
  calls from the pre-upgrade MCP connection.

## Handoff

- Live verification after `7.3.2-33` found that the pre-upgrade Codex MCP
  connection received HTTP 403 `session_token_mismatch` indefinitely. The
  Gateway's in-memory SDK session and token binding were both gone after
  restart, so this was an expired session, not a cross-token attachment.
- MCP Streamable HTTP requires HTTP 404 when a server-side session no longer
  exists; the Go SDK maps that response to `ErrSessionMissing`.
- The binding result now distinguishes authorized, missing, and token-mismatch
  states. HTTP boundary tests cover both protocol 404 and cross-token 403.
- After upgrading to `7.3.2-34`, the pre-upgrade Codex connection recovered on
  its first `list_nas` call. Subsequent parallel `get_system_info` calls reached
  all three NAS targets successfully.
- Full Go tests, `go vet`, diff checks, SPK validation, package health, and the
  external HTTPS health endpoint all passed.
