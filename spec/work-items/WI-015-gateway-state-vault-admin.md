---
id: WI-015
title: Add persistent gateway profiles, vault, and administration
status: ready
priority: P0
owner: ""
depends_on: [WI-014]
parallel_group: F
touches:
  - internal/gateway
  - internal/config
  - internal/credentials
  - internal/runtime/manager.go
  - internal/application
  - cmd/dsmctl-gateway
  - docs/gateway.md
---

# WI-015 - Add persistent gateway profiles, vault, and administration

## Outcome

An authenticated gateway administrator can add, test, update, remove, and
authenticate multiple NAS profiles without restarting the process. Passwords,
OTPs, and trusted-device credentials are stored through a package-neutral
encrypted vault, and profile changes safely evict stale DSM clients.

## Scope

- Add a versioned transactional embedded state repository under `/data`; use a
  pure-Go implementation that preserves `CGO_ENABLED=0` builds.
- Persist NAS profiles, revisions, secret metadata, token/bootstrap metadata,
  schema version, migration records, and health summaries.
- Read a 32-byte master key from `/run/secrets/master.key` and encrypt password
  and trusted-device payloads with AES-256-GCM, unique random nonces, and
  authenticated profile/type metadata.
- Add `vault:<id>` apply-time secret references for the gateway while retaining
  existing `env:NAME` behavior for CLI automation.
- Add an admin application/API for profile list/add/update/remove/default,
  DNS/TCP/TLS/DSM connection tests, password/OTP enrollment, credential status,
  removal, and trusted-device rotation.
- Add one-time generic-Linux bootstrap from `/run/secrets/bootstrap`; bootstrap
  becomes permanently invalid after the first administrator is established.
- Add profile revisions and runtime invalidation. URL, username, TLS policy, or
  credential changes close and remove the old cached client.
- Support `system_ca` and `pinned_fingerprint` TLS modes and explicit
  fingerprint confirmation. Production gateway mode has no skip-verify path.
- Add transactional schema migration with pre-migration backup and fail-closed
  readiness on migration or key errors.

## Non-goals

- DSM browser-session authentication and SPK-specific paths; WI-017 supplies
  the Synology administration adapter and mounts.
- OIDC, multi-owner tenancy, password recovery, portable secret export, or
  automatic NAS discovery.
- Remote MCP tool scopes, apply approvals, and full audit policy; WI-016 owns
  them.
- Changing current CLI config/keyring behavior or migrating a desktop user's
  OS keyring into the gateway automatically.

## Design constraints

- Follow `spec/gateway-deployment.md`. The repository and vault are gateway
  infrastructure; Synology operation packages remain unaware of them.
- Never store the master key in the database or normal `/data` backup.
- Password and OTP values exist only for the bounded enrollment transaction.
  They never enter MCP inputs, plans, display models, errors, or logs.
- A profile update and runtime eviction must be ordered so no new request can
  acquire the old client after the updated revision is committed.
- Removing a profile closes its session and deletes its credentials by default;
  retained credentials require an explicit administrator choice and remain
  addressable for later cleanup.
- Plans remain bound to profile name and revision. Apply rejects a missing or
  materially changed profile even when its NAS name was recreated.

## Acceptance criteria

- [ ] Profile CRUD and default selection take effect without process restart.
- [ ] URL, username, TLS, or credential changes evict and close the old client;
      unrelated NAS sessions remain active.
- [ ] At least 32 profiles can be stored and listed deterministically.
- [ ] Password and trusted-device values are encrypted at rest with distinct
      nonces and cannot be found in the database, backups, logs, or API output.
- [ ] Missing, malformed, or wrong master keys prevent readiness and do not
      overwrite existing encrypted data.
- [ ] The first generic-Linux bootstrap succeeds exactly once; replay and
      absent-bootstrap attempts fail closed.
- [ ] Password plus OTP enrollment stores a trusted device through the existing
      authentication flow without exposing either secret.
- [ ] `vault:<id>` resolves only at apply time and is unavailable to MCP result
      encoding or plan hashing.
- [ ] System-CA and pinned-fingerprint TLS modes are tested, including explicit
      mismatch and certificate-rotation failures.
- [ ] A failed schema migration leaves the original database and backup
      recoverable and keeps `/readyz` false.
- [ ] Existing CLI config and OS-keyring tests continue to pass.

## Verification

- `go test ./... -count=1` and `go vet ./...`.
- Repository tests cover concurrent reads/updates, migration rollback, profile
  revision compare-and-swap, and client eviction races.
- Vault tests use temporary files and fixture keys; they scan persisted bytes
  and captured logs for known plaintext secrets.
- TLS tests use local test servers and generated certificates.
- Optional live verification is limited to authentication and read-only system
  information on explicitly configured NAS profiles. No live DSM mutation is
  authorized.

## Coordination

Depends on WI-014's gateway lifecycle. It also touches `internal/application`
and `internal/runtime/manager.go`; coordinate with any active management work
before changing shared constructors or service interfaces.

## Handoff

Fill this only when pausing incomplete work.
