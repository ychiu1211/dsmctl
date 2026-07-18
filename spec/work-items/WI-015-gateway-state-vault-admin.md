---
id: WI-015
title: Add persistent gateway profiles, vault, and administration
status: in_progress
priority: P0
owner: "gateway-admin"
depends_on: [WI-014]
parallel_group: F
touches:
  - internal/gateway
  - internal/config
  - internal/credentials
  - internal/runtime/manager.go
  - internal/weblogin
  - internal/application
  - cmd/dsmctl-gateway
  - docs/gateway.md
---

# WI-015 - Add persistent gateway profiles, vault, and administration

## Outcome

An authenticated gateway administrator can add, test, update, remove, and
authenticate multiple NAS profiles without restarting the process. Web-login
sessions, passwords, OTPs, and trusted-device credentials are stored through a
package-neutral encrypted vault, and profile changes safely evict stale DSM
clients.

## Scope

- Add a versioned transactional embedded state repository under `/data`; use a
  pure-Go implementation that preserves `CGO_ENABLED=0` builds.
- Persist NAS profiles, revisions, secret metadata, token/bootstrap metadata,
  schema version, migration records, and health summaries.
- Read a 32-byte master key from `/run/secrets/master.key` and encrypt
  password, trusted-device, and web-login session payloads (DSM SID/SynoToken
  plus the durable Noise resume keys) with AES-256-GCM, unique random nonces,
  and authenticated profile/type metadata.
- Wire a vault-backed `credentials.SessionStore` into the gateway's runtime
  manager through the existing `runtime.WithSessionStore` seam: the manager
  prefers a stored web-login session over password resolution, renews it
  headlessly through the existing Noise_KK resume, and persists rotated
  SID/SynoToken values back to the vault.
- Add `vault:<id>` apply-time secret references for the gateway while retaining
  existing `env:NAME` behavior for CLI automation.
- Add an admin application/API for profile list/add/update/remove/default,
  DNS/TCP/TLS/DSM connection tests, web-login session enrollment, password/OTP
  enrollment, credential status, removal, and trusted-device rotation.
- Web-login enrollment keeps the gateway as the code-exchange client: the admin
  API issues the PKCE challenge and performs the Noise_IK exchange; the
  authenticated admin page opens the NAS's own sign-in page and relays only the
  one-time code, taking the role the CLI loopback relay plays today. Passwords,
  OTPs, and passkeys stay in the administrator's browser at the NAS.
- Add one-time generic-Linux bootstrap from `/run/secrets/bootstrap`; bootstrap
  becomes permanently invalid after the first administrator is established.
- Add profile revisions and runtime invalidation. URL, username, TLS policy, or
  credential changes close and remove the old cached client.
- Support `system_ca` and `pinned_fingerprint` TLS modes and explicit
  fingerprint confirmation. Production gateway mode has no skip-verify path.
- Add transactional schema migration with pre-migration backup and fail-closed
  readiness on migration or key errors.

## Non-goals

- Authenticating the gateway administrator through a DSM browser session, and
  SPK-specific paths; WI-017 supplies the Synology administration adapter and
  mounts. Enrolling NAS profile sessions via DSM web login is in scope above.
- OIDC, multi-owner tenancy, password recovery, portable secret export, or
  automatic NAS discovery.
- Remote MCP tool scopes, apply approvals, and full audit policy; WI-016 owns
  them.
- Changing current CLI config/keyring behavior, migrating a desktop user's OS
  keyring into the gateway automatically, or any gateway code path that reads
  an OS keyring — even co-located with a CLI install. `dsmctl auth login`
  state stays CLI-only; sessions enter the gateway only through its own
  enrollment.

## Design constraints

- Follow `spec/gateway-deployment.md`. The repository and vault are gateway
  infrastructure; Synology operation packages remain unaware of them.
- Decision: gateway profiles obtain DSM sessions from the encrypted vault (or
  the retained `env:NAME` password fallback for automation accounts) — never
  from the desktop OS keyring, which the portability boundary forbids in the
  container. Password-env-only operation remains documented as the pre-WI-015
  development bridge, not the end state, because 2FA-enforced and passkey
  accounts cannot authenticate headlessly by password.
- Never store the master key in the database or normal `/data` backup.
- Password and OTP values exist only for the bounded enrollment transaction.
  Stored session material (SID, SynoToken, Noise resume private keys) is
  password-equivalent. None of these ever enter MCP inputs, plans, display
  models, errors, logs, or admin API responses; session status surfaces only
  the existing non-secret `SessionMeta` projection.
- A profile update and runtime eviction must be ordered so no new request can
  acquire the old client after the updated revision is committed.
- A headless session resume rewrites the stored session payload in place; it
  must not advance the profile revision or evict the cached client.
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
- [ ] Password, trusted-device, and session values are encrypted at rest with
      distinct nonces and cannot be found in the database, backups, logs, or
      API output.
- [ ] Missing, malformed, or wrong master keys prevent readiness and do not
      overwrite existing encrypted data.
- [ ] The first generic-Linux bootstrap succeeds exactly once; replay and
      absent-bootstrap attempts fail closed.
- [ ] Password plus OTP enrollment stores a trusted device through the existing
      authentication flow without exposing either secret.
- [ ] Web-login enrollment through the admin flow stores a session that serves
      MCP reads with no `DSMCTL_PASSWORD_*` variable set, and the session
      survives a gateway restart.
- [ ] An expired vaulted session renews through Noise_KK resume without a
      browser or password; the rotated SID/SynoToken is persisted and the
      cached client is not evicted.
- [ ] The gateway has no OS-keyring code path; sessions resolve only through
      the vault store or environment passwords.
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
- Verify early against a live DSM that the one-time code relayed by the admin
  browser can be exchanged from a different host (the gateway) and that the
  sign-in page accepts a non-loopback `opener` origin; if DSM binds either to
  the browser's host, record the limitation and enroll that NAS through the
  password/OTP path instead.
- Optional live verification is limited to authentication and read-only system
  information on explicitly configured NAS profiles. No live DSM mutation is
  authorized.

## Coordination

Depends on WI-014's gateway lifecycle. It also touches `internal/application`
and `internal/runtime/manager.go`; coordinate with any active management work
before changing shared constructors or service interfaces.

## Handoff

Fill this only when pausing incomplete work.
