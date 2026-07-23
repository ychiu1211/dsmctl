---
id: WI-032
title: Replace platform administration with a portable local administrator
status: done
priority: P0
owner: ""
depends_on: [WI-015, WI-016]
parallel_group: G
touches:
  - internal/gateway/admin
  - internal/gateway/state
  - internal/gateway/platformauth
  - internal/synologyauth
  - cmd/dsmctl-gateway
  - cmd/dsmctl-synology-auth
  - deploy/linux
  - deploy/synology
  - docs/gateway.md
  - docs/synology-package.md
  - spec/gateway-deployment.md
---

# WI-032 - Replace platform administration with a portable local administrator

## Outcome

The platform-neutral gateway presents first-run local setup until the first
administrator is created, creates its own username and password, and thereafter
uses browser sessions for administration on generic amd64 Linux. WI-091
supersedes only the fresh Synology SPK entry path with DSM-delegated login and
an optional local fallback. The NAS hosting an SPK receives no implicit
profile, credential, or privilege.

## Scope

- Replace the generic bootstrap bearer token with a local administrator model;
  WI-091 later adds an explicit DSM-delegated SPK entry path without changing
  the generic Linux setup contract.
- When no administrator exists, keep administrator creation available across
  elapsed time and process restarts; the first successful transactional
  creation permanently closes setup for that initialized state.
- Store a normalized administrator username and an Argon2id password verifier;
  never store or return the password.
- Add login, logout, current-session, password-change, and revoke-other-session
  flows. Browser session secrets are random, stored only as digests, bounded,
  expiring, and sent through HttpOnly/SameSite cookies.
- Require same-origin JSON requests plus a non-simple request header for every
  state-changing admin API, and rate-limit setup and login attempts in memory.
- Keep profile management, per-NAS DSM web login, MCP tokens, approvals, and
  audit behind the local administrator session. These identities remain
  independent and non-transitive.
- Keep the shared image free of host-NAS assumptions. The optional signed
  platform assertion interface added by WI-091 is enabled only by the explicit
  Synology deployment adapter.
- Remove bootstrap-secret creation and mounts from generic Linux and Synology
  deployment assets.
- Update the state schema transactionally. Empty pre-release schema-3 states
  may return to uninitialized; a schema-3 state containing profiles, MCP
  tokens, or other managed data must fail closed with explicit reset/migration
  guidance rather than silently expose a fresh setup endpoint.
- Make the uninitialized/initialized UI states explicit. An
  initialized unauthenticated gateway shows the ordinary login page; it cannot
  infer whether the viewer was the original installer.

## Non-goals

- Setup codes, emailed recovery, password-reset questions, OIDC, DSM SSO,
  multiple administrator accounts, roles, or multi-tenant ownership.
- Detecting or automatically trusting the NAS that hosts the container.
- Automatically discovering, adding, or authenticating any NAS.
- Treating RFC1918 source addresses, proxy headers, or the first browser as a
  durable identity proof.

## Design constraints

- The persistent first-run endpoint is an explicit LAN/VPN single-owner product tradeoff. A
  caller that wins the first setup race controls an otherwise empty gateway;
  it receives no DSM credential or NAS authority. The UI and documentation
  explain that an unexpected login page requires an operator-controlled data
  reset before enrolling a NAS.
- Setup availability follows persistent uninitialized state rather than a
  process-local timer. Elapsed time and restart do not close it while no local
  administrator exists.
- Administrator creation, password change, and session issuance are atomic.
  Password change revokes every other administrator session.
- Password verification uses bounded Argon2id parameters suitable for the
  packaged memory limit, constant-time comparison, and a dummy verification
  path for unknown usernames.
- Cookies are Secure whenever the external request is HTTPS. Production docs
  continue to require a trusted TLS reverse proxy and loopback-only backend.
- The gateway image contains no DSM paths, commands, authentication calls, or
  host-NAS assumptions. `localhost` continues to mean the container itself.
- Existing CLI/MCP/application/runtime/Synology-operation boundaries and every
  plan/apply, remote-scope, approval, and audit contract remain unchanged.

## Acceptance criteria

- [x] A fresh gateway permits exactly one administrator creation without a
      setup code, DSM session, platform header, or time deadline.
- [x] Setup remains available across elapsed time and process restart only
      while the database remains uninitialized.
- [x] Concurrent setup requests produce one administrator, one session, and no
      partial or overwritten account state.
- [x] Password plaintext and browser session tokens cannot be found in the
      database, backup, logs, audit output, or API responses.
- [x] Valid login creates an expiring HttpOnly/SameSite session; invalid login,
      expired/revoked sessions, CSRF-like simple cross-origin requests, and
      bounded rate-limit overflow fail closed.
- [x] Logout, password change, and revoke-other-sessions have the documented
      effect, including revocation across gateway restart.
- [x] Profile, credential, MCP-token, approval, and audit APIs accept the local
      admin session and no longer accept legacy admin bearer tokens or DSM
      platform assertions.
- [x] Generic Linux uses first-run local setup without a bootstrap or platform
      key; the same image supports WI-091's explicit DSM adapter when selected.
- [x] The host NAS is absent after initialization and can be used only after
      explicit profile creation plus that profile's own DSM Web Login.
- [x] Schema migration and pre-migration backup are tested; non-empty legacy
      platform/token-admin state never silently opens unauthenticated setup.
- [x] `go test ./... -count=1`, `go vet ./...`, amd64 image build, generic
      Docker lifecycle smoke, SPK validation, and offline-image validation pass.
- [x] User documentation explains persistent first-run setup, trusted-network
      restriction, unexpected initialized state, reset consequences, login
      sessions, and explicit host-NAS enrollment.

## Verification

- Unit tests inject time, randomness, password-hash parameters, and concurrent
  requests; persisted-byte and captured-log tests use secret canaries.
- HTTP tests exercise setup, login, cookie/session lifecycle, origin/header
  enforcement, rate limiting, and every existing admin API authorization path.
- `go test ./... -count=1` and `go vet ./...`.
- Build and run the same `linux/amd64` image through the generic hardened
  Compose configuration before and after restart.
- Build and validate the offline x86_64 SPK locally. Real DSM lifecycle
  certification remains part of WI-017 and performs no disruptive NAS mutation.

## Coordination

This intentionally replaces the bootstrap-token portion of WI-015 and the DSM
platform-authentication portion of WI-017. The current session owns both this
item and the WI-017 implementation; WI-017 hardware certification/completion is
paused until this item passes so it does not certify the superseded adapter.

## Handoff

Fill this only when pausing incomplete work.

## Completion notes

- State schema 4 stores one normalized local administrator and an Argon2id
  verifier. Random 12-hour browser-session secrets are stored only as SHA-256
  digests; session count and password hashing concurrency are bounded.
- The admin HTTP surface now provides persistent first-run setup, login/logout,
  password change, and session revocation with Secure/HttpOnly/SameSite cookies,
  same-origin JSON mutation checks, and in-memory setup/login rate limiting.
- The gateway and UI no longer accept bootstrap bearer tokens or caller-supplied
  identity headers. WI-091's signed DSM assertion is an explicit SPK-only login
  adapter; generic Linux remains local-setup only. Every NAS, including the host
  NAS, requires an explicit profile and that NAS's own DSM Web Login.
- `go test ./... -count=1`, `go vet ./...`, and `git diff --check` pass. Two
  `linux/amd64` builds were identical at image ID
  `sha256:23bc4034b70d97d347ca87dfe0fa193bddfa5d1dba190bcd73207318bf5fa1d6`.
  The hardened Docker smoke test passed setup, readiness, cookie security,
  secret non-disclosure, and session persistence across restart.
- Two deterministic offline SPK builds were byte-identical at SHA-256
  `9d576f03f350fa9950eaffaef3cf010bf71144f2de5f11ff19080bf0cff45186`;
  the embedded image archive SHA-256 was
  `acc85497bf8a13688129b98ffe314dae190c28270f82722971cd4c0d4ab5b88b`,
  and the offline x86_64 SPK structure/security validator passed. Real DSM
  lifecycle and portal certification remains explicitly owned by WI-017.
