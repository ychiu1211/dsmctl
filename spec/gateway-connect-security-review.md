# Gateway connect-surface security & design review (2026-07-22)

An adversarial design review of the dsmctl remote-MCP **connect surface** — the
gateway's bearer admission, OAuth 2.1 flow, public-URL/scheme derivation, token
model, TLS posture, scope/approval model, and connect UX. It reviews the
implementation of the accepted design in
[mcp-power-user-connection-design.md](mcp-power-user-connection-design.md) and
[gateway-deployment.md](gateway-deployment.md); it does not change that design.

This is a review record: it states the verdict, the properties that must be
preserved, and the gaps that became work items. It is not itself a contract —
the contracts remain in `architecture-contracts.md`.

## Verdict

**The connect-surface design is sound.** No high-severity or third-party-
exploitable issue was found. The review produced **13 confirmed, still-current
gaps**, all hardening / secure-by-default / operability — none of them a direct
compromise in the documented loopback-bind + reverse-proxy deployment. They are
tracked as [WI-096](work-items/WI-096-gateway-connect-ux-docs.md),
[WI-097](work-items/WI-097-gateway-token-least-privilege-defaults.md),
[WI-098](work-items/WI-098-gateway-forwarded-header-trust.md), and
[WI-099](work-items/WI-099-gateway-oauth-tls-hardening.md).

## Method

- Six review dimensions (forwarded-header trust, OAuth correctness, token model,
  TLS posture, scope/approval, connect UX), each read against the actual code.
- Every candidate finding was **adversarially verified** against the real
  control flow — a verifier tried to refute it and default to "not a real issue"
  unless the code genuinely permitted the failure. This knocked every initial
  "medium/high" claim down to low/medium and eliminated the false positives.
- The surviving findings were then **re-validated against current `main`
  (`cc8d160`)** after the gateway UI/auth rework, to confirm they still hold and
  to capture current file:line. All 13 still held; none had been fixed.

## Confirmed-correct properties (preserve these)

These controls were checked and found correct. Future work must not regress
them; a change here needs its own review.

**OAuth flow**
- PKCE **S256 is mandatory** at `/authorize` and verified (constant-time) at
  token exchange; missing or `plain` challenges are rejected.
- `redirect_uri` is validated by **exact match against the registered set at
  both authorize and token time**.
- Authorization codes are short-lived (~5 min), **single-use**, digest-keyed, and
  bounded; held in memory only.
- The token endpoint is **public-client only** (PKCE-secured); it does not accept
  confidential-client secrets.
- Consent-form CSRF is defended by a same-origin check + mandatory admin
  credentials + PKCE + a form-action CSP.

**Token model**
- The static developer bearer path uses a **constant-time full-digest compare**;
  OAuth/manual tokens authenticate via SHA-256-digest-keyed lookup (timing-safe
  by construction).
- **Digest-only storage** for all bearer material (access, refresh, auth codes,
  static bearer). The master key is never copied into the DB or its backup.
- Access ~1h / refresh ≤365d with rotation that **preserves the token id and
  never extends the absolute grant**; revocation/expiry take effect on the next
  request and kill the refresh path.

**Scope & approval**
- Scope-to-tool authorization is **fail-closed on both `tools/list` and
  `tools/call`**, and every NAS-scoped call requires an explicit `nas` argument.
- High-risk approval binding is complete and single-use (**plan hash + profile
  revision + token + admin, ≤10 min**), consumed **race-safe in one bbolt
  transaction**, and the mandatory admission audit commits in that same
  transaction before the DSM precondition read.
- A client-supplied `plan.Risk` **cannot be downgraded** to skip the high-risk
  gate (Risk is a hashed plan field); another token cannot consume a plan's
  approval.

**Transport / origin**
- With `--admin-public-url` set, the configured origin is used verbatim and
  forwarded headers are ignored.
- Forwarded host/prefix are sanitized against CRLF/path-injection and the scheme
  is constrained; metadata/challenge responses are `Cache-Control: no-store`
  (no shared-cache poisoning).
- The OAuth `http` redirect_uri is restricted to loopback, so codes are never
  delivered in cleartext to a remote host; the admin cookie's `Secure` flag
  follows the derived scheme.
- The unauthenticated `/mcp` `401` emits a `WWW-Authenticate` challenge carrying
  `resource_metadata` + `scope`, enabling standard OAuth discovery.

## Gaps → work items

All severities are the post-verification value; locations are current on
`cc8d160`.

### WI-096 — connect UX & subpath docs (P2)
- **Subpath reverse-proxy is undocumented for generic nginx.** The SPK path
  forwards `X-Forwarded-Prefix`, but `deploy/linux/nginx.conf.example` mounts at
  bare `location /` and no doc names the header. (`oauth/handler.go` `externalBase`
  ~557.) *low / partial.*
- **The displayed `http` MCP endpoint is unexplained.** `mcpEndpoint()`
  (`admin/ui.go` ~620) reflects the browsing scheme with no warning that a bearer
  token over `http` travels in cleartext. *low.*

### WI-097 — token least-privilege defaults (P1)
- **Manual-token wizard defaults over-grant:** 365-day + `full` preset (incl.
  `nas.apply`) + all NAS pre-checked, one click. (`admin/ui.go` ~692.) *medium.*
- **OAuth scope-less default = all four scopes, and the grant always includes
  every NAS** (target-role profiles not excluded). (`oauth/handler.go`
  `normalizeScopes` ~606, `validateAuthorizationRequest` ~433–446.) *medium.*
- **No server-side max token lifetime; perpetual tokens allowed.**
  (`state/policy.go` `normalizeMCPTokenInput` ~736.) *medium.*

### WI-098 — forwarded-header trust gating (P1)
- **`X-Forwarded-Proto/Host/Prefix` are trusted ungated on `TrustedProxies`**,
  inconsistent with `clientIP()` which gates `X-Forwarded-For`. Feeds the
  advertised OAuth/admin origin. (`oauth/handler.go` `externalBase` ~541–561;
  `admin/auth_http.go` `externalOrigin` ~94–110; ref `server.go` `clientIP` ~566.)
  *medium — not third-party-exploitable in loopback+nginx (per-request stateless
  metadata), a consistency/defense-in-depth gap.*
- **`X-Forwarded-Host` bypasses the `AllowedHosts` allowlist.** (`server.go`
  `protectHostAndOrigin` ~423–450.) *medium.*
- **`X-Forwarded-Prefix` is appended even when `--admin-public-url` pins the
  origin.** (`oauth/handler.go` ~557–561.) *low.*

### WI-099 — OAuth & TLS-posture hardening (P2)
- **Anonymous DCR can exhaust the 128-client cap** with no admin list/prune.
  (`state/oauth.go` `RegisterOAuthClient` ~92; `state/types.go` `MaxOAuthClients`
  ~26.) *medium.*
- **Registration rate-limit keys on `RemoteAddr`** → one global bucket behind the
  proxy. (`oauth/handler.go` `remoteKey` ~729.) *medium.*
- **Refresh rotation lacks reuse detection** (a replayed superseded token doesn't
  revoke the family). (`state/oauth.go` `RefreshOAuthTokenSet` ~175.) *low.*
- **No warning on a non-loopback plaintext bind.** (`cmd/dsmctl-gateway/main.go`
  ~217/233.) *low.*
- **`AdmitRemoteApply` uses `time.Now()` instead of the `r.now()` clock seam.**
  (`state/policy.go` ~594.) *low, testability only.*

## Extending this record

When a listed gap is implemented, mark its work item `done` and leave this record
as the historical rationale — do not delete the "preserve these" section, which
future connect-surface changes must respect. A new connect-surface review appends
a new dated section rather than rewriting this one.
