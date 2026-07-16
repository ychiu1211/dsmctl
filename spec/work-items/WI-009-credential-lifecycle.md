---
id: WI-009
title: Credential status, removal, and trusted-device rotation
status: done
priority: P2
owner: ""
depends_on: []
parallel_group: D
touches:
  - internal/credentials
  - internal/runtime/manager.go
  - internal/synology/client.go
  - internal/application
  - internal/cli
  - internal/mcpserver/server.go
  - cmd/dsmctl-mcp/main.go
  - README.md
  - docs/credentials.md
---

# WI-009 — Credential status, removal, and trusted-device rotation

## Outcome

A user can see, per NAS profile, whether a password and a DSM trusted-device
credential are stored in the OS credential store, which password environment
variable applies and whether it is set, remove stored credentials, and rotate
the trusted device — without any secret value ever being displayed. An MCP
client can read the same boolean/name-only status to diagnose authentication
failures without transporting secrets.

## Scope

- `dsmctl auth status`: per-profile credential presence, password environment
  variable name and set/unset state, and in-process session state.
- `dsmctl auth logout`: remove the stored password and/or trusted-device
  credential; default scope is both. An explicitly named profile does not
  need to exist in the configuration, so credentials orphaned by an earlier
  `nas remove` stay removable.
- `dsmctl auth rotate-device`: delete the stored trusted device first, then
  re-authenticate with the stored password or environment fallback so DSM
  issues a fresh trusted-device credential through the normal OTP flow.
- `nas remove` best-effort deletes both stored credentials for the removed
  profile; `--keep-credentials` opts out.
- One read-only MCP tool `get_auth_status` sharing the same application
  result model.
- Credential-store additions: existence probes and idempotent deletes that
  never return secret material to callers.

## Non-goals

- MCP tools that accept passwords or OTP values, or that mutate credentials.
- Server-side DSM trusted-device revocation (`SYNO.Core.TrustedDevice`);
  old device entries are revoked manually in DSM Personal > Security.
- Cross-process session invalidation: removing local credentials cannot log
  out another running dsmctl or MCP process.
- Credential export or display of any secret value.

## Design constraints

- Credential removal is local-only and reversible by running `auth login`
  again, so per the mutation-safety contract it does not use plan/apply.
- Passwords, device IDs, and device names are authentication material and
  never enter results, logs, or MCP outputs; status reports booleans plus
  the environment variable name only.
- `auth status` and `get_auth_status` are fully offline: they must not
  resolve passwords or contact a NAS. In-process session state comes from a
  lock-protected accessor that reports only whether a client exists and
  whether it holds a session ID.
- A keyring probe failure surfaces as a per-profile error string instead of
  failing the whole status listing.

## Acceptance criteria

- [x] `auth status` reports every configured profile with password stored,
  trusted device stored, environment variable name, and set state; secrets
  and device names are absent from table and JSON output.
- [x] `auth logout` removes exactly the requested scope, reports what was
  removed versus not stored, and works for profile names no longer in the
  configuration.
- [x] `auth rotate-device` deletes the stored device before authenticating,
  stores the new device ID on success, and leaves an actionable recovery
  path when login fails after deletion.
- [x] `nas remove` cleans both credentials by default and honors
  `--keep-credentials`.
- [x] `get_auth_status` is read-only annotated, never contacts the NAS, and
  returns the shared application result model.
- [x] Documentation states that running processes keep in-memory credentials
  until exit and that a set password environment variable still enables
  non-interactive login after logout.

## Verification

- `go test ./... -count=1` and `go vet ./...`; unit tests use the in-memory
  keyring fake and never touch the real OS credential store.
- `auth status` may run against the configured test NAS host offline (no DSM
  contact) at any time.
- `rotate-device` performs a real DSM login and registers a new trusted
  device server-side; live verification is manual-only and out of automated
  tests.

## Coordination

Touches `internal/application` and `internal/mcpserver/server.go`, which are
shared with other management items; no other item is in progress in parallel
group D.

## Completion record

- Completed end to end on 2026-07-17. CLI adds `auth status`, `auth logout`,
  and `auth rotate-device`; `nas remove` now cleans stored credentials with
  a `--keep-credentials` opt-out; MCP adds the offline read-only
  `get_auth_status` tool over the shared application result model.
- Session introspection uses `synology.Client.HasSession` and
  `runtime.Manager.SessionInfo`, which never resolve credentials and never
  contact DSM.
- Verified with `go test ./... -count=1` and `go vet ./...` using the
  in-memory keyring fake, plus an offline `dsmctl auth status` run against
  the local configuration. No live DSM mutation or rotation was performed;
  `rotate-device` live verification remains manual per the policy above.
