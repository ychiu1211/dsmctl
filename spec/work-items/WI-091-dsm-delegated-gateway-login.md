---
id: WI-091
title: Add DSM-delegated Gateway administrator login
status: in_progress
priority: P0
owner: "codex"
depends_on: [WI-032]
parallel_group: G
touches:
  - cmd/dsmctl-gateway
  - cmd/dsmctl-synology-auth
  - internal/gateway/admin
  - internal/gateway/oauth
  - internal/gateway/platformauth
  - internal/gateway/state
  - internal/synologyauth
  - deploy/synology
  - docs/gateway.md
  - docs/synology-package.md
  - spec/architecture-contracts.md
  - spec/gateway-deployment.md
---

# WI-091 - Add DSM-delegated Gateway administrator login

## Outcome

A Synology SPK installation lets a currently authenticated DSM administrator
open the Gateway through DSM Web Login without first creating a separate
Gateway account. The administrator may later configure one independent local
Gateway username/password as an explicit fallback. A generic container has no
DSM trust source and keeps the existing mandatory one-hour local-administrator
setup before any administration is possible.

## Scope

- Add an optional, platform-neutral signed-administrator assertion interface
  to the Gateway and enable it only when an explicit assertion key is supplied.
- Add a Synology host-side loopback bridge which reuses the existing NAS Web
  Login authorization-code + PKCE + Noise exchange, verifies the returned
  account's membership in the DSM `administrators` group, strips
  caller-supplied identity headers, and signs a short-lived audience-bound
  login assertion for the loopback-only Gateway backend.
- Verify the DSM assertion once while creating an independent Gateway browser
  session. DSM and dsmctl are separate sites with separate logout/session
  lifecycles; a new dsmctl login always repeats DSM Web Login and the group
  check.
- On a fresh SPK, disable the unauthenticated one-hour local setup endpoint and
  present DSM Web Login as the only entry method. After DSM login, allow an
  administrator to configure the single local fallback account.
- On an upgraded SPK that already has a local administrator, preserve it and
  offer both DSM Web Login and local username/password login.
- On generic Linux, preserve the existing setup window, local login, cookie,
  rate-limit, origin, and readiness behavior with no DSM-specific dependency.
- Identify audit actors and session metadata as `dsm:<subject>` or
  `local:<username>` without storing DSM cookies, SIDs, SynoTokens, Noise keys,
  or assertion values.
- Permit DSM Web Login on the MCP OAuth authorization page; local
  username/password authorization is shown only after a local account exists.
- Generate and preserve a distinct 32-byte DSM assertion key in package-private
  storage, mount it read-only into the container, and include it in upgrade
  recovery copies without mixing it with the vault master key.

## Non-goals

- Authorizing ordinary DSM users. Only effective members of the DSM
  `administrators` group are Gateway administrators.
- Sending a DSM password, OTP, cookie, SID, SynoToken, or group list to the
  container or persisting it in Gateway state.
- Treating forwarded username headers, source IP, the desktop shortcut, or the
  host NAS profile as proof of identity.
- Adding DSM-specific paths, commands, or package variables to the core
  container image.
- Multiple local fallback accounts, per-DSM-user Gateway roles, OIDC, SAML, or
  Internet-facing multi-tenant administration.

## Design constraints

- This item is an explicit approved exception to WI-032's identical-admin-mode
  contract. The core remains portable: it verifies an abstract signed
  assertion; only the SPK bridge executes DSM commands and interprets DSM
  group membership.
- The public DSM reverse proxy targets only the loopback bridge, while the
  Gateway backend remains loopback-only. The bridge removes every incoming
  assertion header before optionally adding its own.
- Assertions use a random ID, an audience, issued/expiry times, an
  administrator claim, HMAC-SHA-256, a maximum one-minute lifetime, and
  bounded replay detection. Unknown claims/providers fail closed.
- DSM-backed Gateway browser sessions remain digest-only HttpOnly/SameSite
  cookies and are independent after the login assertion is consumed. Local
  sessions never become valid merely because DSM Web Login is unavailable.
- The SPK's local fallback account is optional. Generic Linux readiness still
  requires it; SPK readiness may instead be satisfied by a configured DSM
  assertion verifier.
- Password reveal/export keeps its independent human-gated reauthentication
  rules. A deployment with no local fallback password must not silently treat
  an ambient DSM session as password reauthentication.

## Acceptance criteria

- [x] Fresh generic container exposes only the existing local setup flow and
      cannot accept DSM assertions or use DSM login.
- [ ] Fresh SPK exposes only DSM Web Login; unauthenticated local setup and
      local password login are unavailable until a signed-in DSM administrator
      explicitly configures the fallback account.
- [ ] DSM administrators can enter the Admin UI and authorize an MCP OAuth
      client without sending DSM credentials to the container.
- [ ] Non-administrators, missing/expired Web Login codes, forged/replayed/wrong-
      audience assertions, non-loopback bridge callers, and identity mismatch
      all fail closed.
- [x] After a valid login assertion is consumed, the DSM-backed Gateway session
      works independently and requires its own logout or revocation.
- [ ] After local fallback setup, DSM and local login both work and produce
      distinguishable audit actors; logout and session revocation work for both.
- [ ] Existing SPK local administrators survive upgrade and gain DSM login;
      generic Linux behavior and existing local sessions remain compatible.
- [x] The assertion key is exact-length, private, preserved across upgrade,
      absent from ordinary database backups/logs/responses, and deletion follows
      the existing explicit package-data deletion choice.
- [x] Focused state/admin/OAuth/bridge tests, `go test ./... -count=1`,
      `go vet ./...`, deterministic image/SPK builds, and offline validation pass.
- [ ] Live DSM verification covers admin/non-admin/session-expiry behavior,
      fresh-or-reset login state, local fallback enablement, HTTPS portal login,
      package restart, and upgrade without performing DSM configuration
      mutations.

## Verification

- Unit tests inject time, assertion keys, DSM validators, and session records;
  request-capture tests prove cookie/assertion/header redaction and replay denial.
- Reuse `internal/weblogin`'s live-tested `SYNO.API.Auth` `webui` code grant,
  PKCE state binding, opener origin check, and Noise IK exchange.
- `go test ./... -count=1`, `go vet ./...`, deterministic image/SPK build and
  `deploy/synology/validate-spk.sh`.
- DSM live tests are authentication/read-only lifecycle tests. They do not
  authorize storage, network, firewall, account, or other DSM mutations.

## Coordination

WI-017 is still in progress only for broader distribution certification; its
implemented SPK assets are an integration surface, not a prerequisite, so it
is deliberately coordination rather than `depends_on`. This item overlaps
WI-017 in Synology packaging and WI-084 in the Admin UI/state files.
Preserve WI-017's current package, host-network, icon, and release changes.
Do not change WI-084's NAS credential-store/reveal semantics; the local
fallback availability is only surfaced so those independent human gates can
continue to fail closed.

## Decision (2026-07-22)

The initial cookie-CGI design was rejected during live verification: the
Web Station portal and DSM management UI are different origins, so DSM cookies
are correctly absent from `/dsmctl` requests. The product owner clarified that
DSM and dsmctl are independent sites and that Gateway login should reuse the
already implemented NAS Web Login code-grant flow. The SPK therefore performs
that server-side code exchange and creates an independent Gateway session;
there is no cross-origin cookie polling and DSM logout does not implicitly
revoke an already-created dsmctl session.

## Handoff

- The Synology Compose adapter now pins `--administrator-mode=dsm`. Mode
  validation requires the platform assertion key and fails startup instead of
  silently falling back to generic one-hour local-administrator setup. `auto`
  remains backward compatible for existing deployment commands by selecting
  DSM mode when the assertion key flag is present; generic deployments without
  that key remain local-setup mode.
- Last known good state: release `7.3.2-14` reuses the existing DSM `webui`
  authorization-code + PKCE + Noise IK exchange, validates the returned
  subject against the host `administrators` group, consumes a short-lived
  platform assertion once, and then uses an independent Gateway session. The
  Admin UI registers its `postMessage` listener before navigating the DSM
  popup, preventing an already-authenticated DSM from returning the code before
  the listener exists. The explanatory text now states that DSM and Gateway
  sessions are independent.
- Verification: `go test ./... -count=1`, `go vet ./...`, embedded Admin UI
  JavaScript parsing, and `git diff --check` pass. Two fixed-input SPKs under
  `dist/wi091-14-a` and `dist/wi091-14-b` are byte-identical and pass offline
  validation. Their SHA-256 is
  `4c47181e1b9e7ca50610276798bc8a9fc0c75fd41f75c2e58c65ba40f6785aa7`;
  the image ID is
  `sha256:3d483fd1a268b0e1a9063d9903ad93db071d2f90dcfd8900cb608e84108f4124`.
  The lab NAS is running `7.3.2-14`; both health endpoints return OK.
  Live DSM Web Login completed with an administrator account, and the Gateway
  session remained
  authenticated after the host bridge/package was restarted and the page was
  reloaded. The two existing NAS profiles remained intact.
- Remaining work: the MCP OAuth authorization page still needs the same
  code-grant flow instead of the obsolete request-cookie validator. Fresh/reset
  SPK behavior, non-administrator denial, optional local fallback enablement,
  session expiry/revocation, and generic-container upgrade compatibility still
  require the remaining acceptance tests before this item can be completed.
- NAS compatibility note: this DSM build's Container Manager status script
  invokes `synosystemctl` without an absolute path, but Package Center runs the
  status hook with a sanitized PATH. The one affected line now uses
  `/usr/syno/bin/synosystemctl`; the untouched vendor original remains at
  `/var/packages/ContainerManager/scripts/start-stop-status.dsmctl-backup-20260722`.
  Without this compatibility fix Package Center falsely reports Container
  Manager as stopped and refuses dependent SPK upgrades.
