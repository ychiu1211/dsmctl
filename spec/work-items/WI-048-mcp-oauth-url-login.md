---
id: WI-048
title: Add MCP OAuth URL login
status: done
priority: P0
owner: ""
depends_on: [WI-045]
parallel_group: G
touches:
  - cmd/dsmctl-gateway/main.go
  - internal/gateway/server.go
  - internal/gateway/oauth/
  - internal/gateway/state/
  - internal/gateway/admin/ui.go
  - docs/gateway.md
  - docs/gateway-admin-guide.md
  - spec/mcp-power-user-connection-design.md
  - spec/roadmap.md
---

# WI-048 — Add MCP OAuth URL login

## Outcome

A supported HTTP MCP client can connect using only the gateway's MCP URL,
complete a browser authorization-code flow by authenticating as the existing
local Gateway administrator, and receive an audience-bound MCP credential
without manually copying a bearer token. The existing manual-token path
remains available for headless and legacy clients.

## Scope

- Publish prefix-aware OAuth Protected Resource Metadata and authorization
  server metadata and reference the resource metadata in MCP 401 challenges.
- Support public-client registration for URL-only clients, exact redirect-URI
  validation, authorization code with S256 PKCE, and the OAuth `resource`
  parameter.
- Show a browser login/consent page that identifies the client, redirect host,
  requested scopes, and every NAS granted to the client.
- Issue short-lived access tokens and rotated persistent refresh tokens using
  digest-only storage. Bind OAuth grants to this gateway's canonical MCP URL.
- Record OAuth client/grant lifecycle in the existing redacted audit surface.
- Keep manual access-token creation and explain both connection paths in the
  MCP Access UI and operator documentation.

## Non-goals

- No public SaaS, multi-user identity provider, third-party OIDC federation,
  DSM-account login, token exchange, device-code grant, or client-credentials
  grant.
- No Client ID Metadata Document fetching; dynamic client registration is
  provided for URL-only compatibility without introducing an SSRF surface.
- No change to NAS plan/apply, per-token NAS allowlists, or high-risk approval.

## Design constraints

- OAuth is enabled only for the managed gateway and uses the existing local
  administrator as resource owner; Admin cookies are never MCP credentials.
- Redirect URIs are registered before authorization and compared exactly.
  Loopback HTTP and non-loopback HTTPS are accepted; fragments and embedded
  credentials are rejected.
- Authorization codes are single-use, short-lived, bounded in memory, and
  bound to client, redirect URI, resource, scope, and S256 PKCE challenge.
- Access, refresh, authorization-code, administrator, DSM, and vault secrets
  never enter logs, audit details, URLs, display models, or persistent
  plaintext.
- OAuth does not weaken the existing remote policy: every NAS remains an
  explicit allowlist entry and Full access still means exactly `nas.read`,
  `nas.plan`, `nas.apply`, and `lan.discover`.

## Acceptance criteria

- [x] An unauthenticated `/mcp` request returns 401 with a prefix-correct
      `resource_metadata` challenge.
- [x] Protected-resource and authorization-server metadata validate against
      the MCP 2025-11-25 authorization requirements used by the flow.
- [x] A dynamically registered public client completes authorization code +
      S256 PKCE and receives an access token that authenticates `/mcp`.
- [x] Invalid client IDs, redirect mismatches, resource mismatches, reused or
      expired codes, and bad PKCE verifiers fail closed without redirects to
      untrusted locations.
- [x] Refresh rotates both access and refresh credentials; reuse, expiry,
      administrator revocation, and manual token lifecycle changes fail closed.
- [x] Consent clearly shows client, redirect, scopes, NAS grants, and the
      full-access/high-risk approval boundary.
- [x] Manual-token creation remains functional and the UI/docs distinguish
      browser URL login from manual configuration.
- [x] `go test ./...`, `go vet ./...`, and `git diff --check` pass.

## Verification

- Unit tests for metadata, redirect/resource validation, PKCE, code one-time
  use, login rate limiting, token/refresh rotation, prefix derivation, audit,
  and manual-token regression.
- Managed-gateway HTTP integration test from 401 discovery through an
  authenticated MCP initialize request. No live DSM mutation is authorized or
  required.

Completed 2026-07-19:

- `TestManagedMCPCompletesOfficialOAuthURLLogin` uses the official Go MCP SDK
  to exercise 401 discovery, authorization-server metadata, DCR, browser
  authorization, S256 PKCE, token exchange, and authenticated MCP initialize.
- OAuth/state tests cover prefix derivation, exact redirect/resource binding,
  untrusted-origin rejection, one-time codes, access/refresh rotation,
  absolute refresh expiry, refresh reuse, and administrative invalidation.
- `go test ./...`, `go vet ./...`, and `git diff --check` pass. The rebuilt
  managed Gateway also returned healthy/ready, valid metadata, and the expected
  OAuth 401 challenge on `127.0.0.1:18765`; English and Traditional Chinese MCP
  Access views were checked in the in-app browser.
- Follow-up copy review separates navigation (`Set up MCP access`), standard
  OAuth (`Connect with the MCP URL`), and manual credential generation
  (`Create manual token` / `Generate token and configuration`). Static UI
  regression checks and browser checks cover all five supported locales.

## Coordination

WI-017 is still active in parallel group G and overlaps gateway packaging and
documentation. This item keeps deployment file changes out of scope and
preserves current WI-017 worktree edits.

## Handoff

Completed. The local managed Gateway is running the rebuilt binary on port
18765. No live DSM mutation or real OAuth client registration was performed.
