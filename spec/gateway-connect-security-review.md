# Gateway connect-surface security and design review (2026-07-22)

Status: accepted review record; implementation follow-ups remain tracked by
WI-096 through WI-099.

This is an adversarial review of the dsmctl remote-MCP connect surface: bearer
admission, OAuth, public URL derivation, token lifecycle, TLS posture, remote
scope/approval policy, and connection UX. It reviews the accepted direction in
[mcp-power-user-connection-design.md](mcp-power-user-connection-design.md) and
[gateway-deployment.md](gateway-deployment.md). It does not itself supersede a
product decision or the contracts in
[architecture-contracts.md](architecture-contracts.md).

Review date: 2026-07-22; source and standards re-validation: 2026-07-23.

Implementation baseline: `cc8d160` (`Polish gateway login and credential UI`).
The repository HEAD before this record landed was `94d1a37`; its diff from
`cc8d160` changed only roadmap/work-item files, so the reviewed runtime code was
identical.

## Verdict

Within the documented product and deployment model, the connection design has
no confirmed high-severity vulnerability. The authorization-code flow,
credential storage, remote authorization boundary, and high-risk approval
transaction have the right primary controls. Thirteen grouped observations
remain open, with re-validated severities from low to medium. They are mapped to
[WI-096](work-items/WI-096-gateway-connect-ux-docs.md),
[WI-097](work-items/WI-097-gateway-token-least-privilege-defaults.md),
[WI-098](work-items/WI-098-gateway-forwarded-header-trust.md), and
[WI-099](work-items/WI-099-gateway-oauth-tls-hardening.md).

This conclusion is conditional on the supported topology: the gateway binds to
loopback by default, and every non-loopback deployment terminates HTTPS at a
trusted reverse proxy on a private LAN or VPN. Publishing the HTTP listener on
an untrusted network is outside that model. In such a deployment the two
plaintext-transport observations below become high impact because bearer
tokens and administrator credentials can be intercepted.

The number 13 follows the work-item grouping used by the review. Some entries
contain more than one closely related code observation; it is not a count of 13
independent vulnerabilities.

## Method

The review used six adversarial dimensions:

1. **OAuth protocol and browser flow** — discovery, dynamic registration,
   redirect handling, PKCE, authorization-code exchange, resource binding,
   refresh rotation, and browser-origin checks.
2. **Credential lifecycle and storage** — entropy, plaintext lifetime,
   digest-only persistence, expiry, revocation, rotation, bounded state, and
   recovery paths.
3. **Remote authorization** — authentication before MCP initialization,
   scope-to-tool mapping, explicit NAS targeting, allowlist enforcement, and
   destination-only profile behavior.
4. **Mutation and approval safety** — plan binding, profile revision, token
   identity, approval expiry, concurrent consumption, mandatory audit, and
   retry behavior.
5. **HTTP, proxy, and transport boundary** — Host/Origin validation,
   forwarded-header trust, prefix derivation, cookie attributes, loopback
   exceptions, and TLS termination assumptions.
6. **Deployment and operator UX** — copied endpoint correctness, subpath proxy
   recipes, unsafe-transport warnings, rate-limit identity, and administrative
   reclamation.

For each candidate issue, the review formed an adversarial claim, traced the
complete request path, checked the enforcing transaction or middleware, and
looked for a focused regression test. Claims that did not survive that process
were discarded. Every retained observation was then checked again against the
implementation baseline and current official OAuth/MCP security guidance.

The standards references used for the re-validation are:

- [MCP Authorization, 2025-11-25](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)
- [RFC 7636: Proof Key for Code Exchange](https://www.rfc-editor.org/rfc/rfc7636.html)
- [RFC 9700: Best Current Practice for OAuth 2.0 Security](https://www.rfc-editor.org/rfc/rfc9700.html)
- [RFC 9728: OAuth 2.0 Protected Resource Metadata](https://www.rfc-editor.org/rfc/rfc9728.html)

## Confirmed-correct properties to preserve

The following controls are correct. Future work MUST preserve their security
properties unless a new, explicit security decision supersedes this record.
They are not open findings and do not need to be re-litigated in WI-096 through
WI-099.

| Control | Verified behavior and source |
| --- | --- |
| PKCE is mandatory and downgrade-resistant | Authorization accepts only `code_challenge_method=S256` and a valid 43–128 character challenge; token exchange recomputes SHA-256 and compares the challenge in constant time. Metadata advertises only `S256`. `internal/gateway/oauth/handler.go:174`, `internal/gateway/oauth/handler.go:426`, `internal/gateway/oauth/handler.go:647`, `internal/gateway/oauth/handler.go:659`. |
| Redirect URIs are bound twice with exact values | The authorization endpoint requires the request value to be one of the registered strings. The token endpoint requires the value to equal the value stored with that authorization code. `internal/gateway/oauth/handler.go:420`, `internal/gateway/oauth/handler.go:346`. |
| Authorization codes are short-lived and single-use | Codes have a five-minute TTL, are stored only by SHA-256 digest in memory, are bounded to 256 pending entries, and are deleted before the exchange result is returned. `internal/gateway/oauth/handler.go:33`, `internal/gateway/oauth/handler.go:451`, `internal/gateway/oauth/handler.go:474`; replay coverage is in `internal/gateway/oauth/handler_test.go:138`. |
| The embedded token endpoint is public-client only | Requests carrying client authentication are rejected; authorization requires PKCE and the browser consent POST must pass the same-origin check before administrator authentication. `internal/gateway/oauth/handler.go:220`, `internal/gateway/oauth/handler.go:324`. |
| OAuth grants are resource-bound and discoverable | Authorization validates the canonical MCP resource; code exchange checks the same resource; refresh records bind both client and resource. Unauthenticated MCP requests advertise `resource_metadata` and scope in `WWW-Authenticate`. `internal/gateway/oauth/handler.go:423`, `internal/gateway/oauth/handler.go:350`, `internal/gateway/state/oauth.go:147`, `internal/gateway/state/oauth.go:204`, `internal/gateway/server.go:298`. |
| Persisted managed bearer secrets are digest-only | Manual/OAuth access tokens and refresh tokens persist SHA-256 digests, not raw secrets. Managed-token authentication performs a digest-index lookup. The fixed development bearer and PKCE challenge use constant-time digest comparison. These are separate guarantees; managed bbolt lookup is not described as a constant-time comparison, and the developer token file is outside this digest-only managed-state claim. `internal/gateway/state/policy.go:251`, `internal/gateway/state/policy.go:283`, `internal/gateway/state/oauth.go:141`, `internal/gateway/state/oauth.go:175`, `internal/gateway/server.go:277`. |
| OAuth access and refresh lifetimes remain bounded | OAuth access tokens last one hour; refresh records have an absolute 365-day expiry that rotation does not extend. Rotation preserves the MCP token identity, and revoking/expiring the access record deletes its refresh path. `internal/gateway/state/types.go:27`, `internal/gateway/state/oauth.go:173`, `internal/gateway/state/policy.go:241`. Refresh replay-family detection is still F-11. |
| Scope-to-tool admission fails closed | Every listed remote tool needs a known mapping and a held scope; an unknown mapping is hidden from `tools/list` and denied on `tools/call`. Targeted calls also require an explicit NAS and pass the token allowlist. `internal/mcpserver/remote_policy.go:33`, `internal/mcpserver/remote_policy.go:90`, `internal/mcpserver/remote_policy.go:112`, `internal/mcpserver/remote_policy.go:148`. |
| High-risk approval is exact, race-safe, and single-use | Admission rechecks active token, `nas.apply`, NAS allowlist, profile revision, plan hash, approval expiry, requesting token, and administrator identity in one bbolt write transaction. The same transaction consumes the approval and writes mandatory audit before application precondition reads. `internal/gateway/state/policy.go:581`, `internal/gateway/state/policy.go:610`, `internal/gateway/state/policy.go:986`; 16-way concurrency coverage is in `internal/gateway/state/policy_test.go:301`. |
| OAuth and JSON responses are non-cacheable | The OAuth handler sets `Cache-Control: no-store` before routing metadata, authorization, registration, and token requests; shared JSON responses do the same. `internal/gateway/oauth/handler.go:120`, `internal/gateway/server.go:270`. |
| Plain HTTP redirect URIs are loopback-only | Registered redirects must use HTTPS, except exact `localhost` or a loopback IP over HTTP; credentials and fragments are rejected. `internal/gateway/state/oauth.go:314`. |
| Administrator browser mutations retain origin and cookie defenses | Mutations require the private browser-request header, JSON, and an exact external Origin. Browser sessions are HttpOnly and SameSite Strict, with `Secure` derived from the external HTTPS origin. `internal/gateway/admin/auth_http.go:79`, `internal/gateway/admin/auth_http.go:117`. Forwarded-origin trust is still subject to F-06 through F-08. |

## Open observations

Severity is assessed for the supported loopback plus trusted-TLS-proxy model.
“Policy decision” means the observed behavior is real, but changing it would
reverse an accepted product decision rather than repair an unambiguous defect.

| ID | Severity | Re-validated observation | Disposition |
| --- | --- | --- | --- |
| F-01 | Low | The generic nginx example has only a root `location /` and no `X-Forwarded-Prefix`; the generic deployment docs do not explain the origin/prefix split. `deploy/linux/nginx.conf.example:9`. | WI-096: add a copyable subpath recipe and documentation. |
| F-02 | Medium | The Admin UI derives the MCP endpoint from browser location but does not warn when it is non-loopback `http://`. `internal/gateway/admin/ui.go:620`, `internal/gateway/admin/ui.go:692`. | WI-096: show a non-blocking warning at the access and issued-token surfaces. |
| F-03 | Medium, policy decision | Manual-token defaults are 365 days, Full access, and every managed NAS preselected. The behavior is confirmed at `internal/gateway/admin/ui.go:450`, `internal/gateway/admin/ui.go:691`, and `internal/gateway/admin/ui.go:692`. It is also explicitly accepted by `spec/mcp-power-user-connection-design.md`, so it is not correct to call this an implementation bug without superseding that decision. | WI-097 remains proposed until the product default is explicitly changed; server enforcement must not depend on a UI-only change. |
| F-04 | Medium | OAuth advertises/challenges with all four scopes, substitutes all four when `scope` is omitted, and grants every profile, including destination-only profiles. `internal/gateway/oauth/handler.go:35`, `internal/gateway/oauth/handler.go:146`, `internal/gateway/oauth/handler.go:156`, `internal/gateway/oauth/handler.go:433`, `internal/gateway/oauth/handler.go:606`. Changing only `normalizeScopes` would not change normal clients that follow the authoritative `WWW-Authenticate` scope or protected-resource metadata. | WI-097: decide the initial OAuth authority model; if least privilege is chosen, update the initial challenge/protected-resource metadata and broader-grant behavior as well as the fallback, add per-grant NAS selection, and always exclude destination-only profiles. |
| F-05 | Medium, policy decision | `normalizeMCPTokenInput` accepts any future expiry and also accepts `nil` as never-expiring. `internal/gateway/state/policy.go:736`. The accepted power-user design deliberately exposes no-expiry as an advanced choice, so a hard cap or prohibition needs an explicit superseding decision. | WI-097: add an authoritative lifetime policy after deciding whether explicit perpetual credentials remain supported. |
| F-06 | Medium | `X-Forwarded-Proto`, `X-Forwarded-Host`, and `X-Forwarded-Prefix` influence OAuth/admin external URLs, cookie path, and redirects without checking `TrustedProxies`, although `X-Forwarded-For` is gated. `internal/gateway/oauth/handler.go:541`, `internal/gateway/admin/auth_http.go:94`, `internal/gateway/admin/auth_http.go:137`, `internal/gateway/server.go:174`, `internal/gateway/server.go:566`. | WI-098: strip or ignore origin/prefix forwarding headers from untrusted peers at one middleware choke point. |
| F-07 | Medium | Host protection validates only `req.Host`; a trusted forwarded host that defines the public origin is not checked against `AllowedHosts`. `internal/gateway/server.go:423`, `internal/gateway/oauth/handler.go:551`, `internal/gateway/admin/auth_http.go:105`. | WI-098: validate an honored forwarded host through the same normalized allowlist. |
| F-08 | Low | `externalBase` appends a forwarded prefix even when `--admin-public-url` pins the origin, so a request header can still alter pinned endpoint URLs. `internal/gateway/oauth/handler.go:541`. | WI-098: keep pinned public configuration authoritative and provide an explicit configured prefix if needed. |
| F-09 | Medium, availability | Dynamic registration stops permanently at 128 records and the repository/admin surface has no list, delete, last-used, or aging path. `internal/gateway/state/types.go:26`, `internal/gateway/state/oauth.go:68`, `internal/gateway/state/oauth.go:100`. | WI-099 after WI-098: add admin reclamation and bounded aging/pruning. |
| F-10 | Low, availability | OAuth register/login throttles key only on `RemoteAddr`; behind nginx all callers share the proxy address and therefore one bucket. `internal/gateway/oauth/handler.go:185`, `internal/gateway/oauth/handler.go:270`, `internal/gateway/oauth/handler.go:729`. | WI-099 after WI-098: reuse trusted-proxy client-IP derivation without trusting spoofed forwarding headers. |
| F-11 | Medium | Refresh tokens rotate and the prior digest is deleted, but replay of a superseded token is indistinguishable from an arbitrary invalid token and does not revoke the live family. `internal/gateway/state/oauth.go:175`. RFC 9700 requires public-client refresh replay detection by sender constraint or rotation with retained relationship information. | WI-099: retain a bounded family/tombstone signal and revoke the live family on replay. |
| F-12 | Medium | The process always calls plain `Serve` and emits no warning when the listener is non-loopback without a declared HTTPS public origin. `cmd/dsmctl-gateway/main.go:217`, `cmd/dsmctl-gateway/main.go:233`. | WI-099: warn prominently (or require an explicit TLS-termination acknowledgement); loopback remains the local-development exception. |
| F-13 | Low | `AdmitRemoteApply` uses wall-clock `time.Now()` instead of the repository's injectable `r.now()` seam, unlike adjacent approval operations. `internal/gateway/state/policy.go:594`. This is a determinism/testability defect, not a demonstrated production bypass. | WI-099: use `r.now().UTC()` and freeze the clock in the regression test. |

## Standards and product-policy notes

- The current MCP authorization specification says authorization-server
  endpoints are HTTPS and treats scope challenges as authoritative. The
  repository's loopback HTTP endpoint remains a deliberate local-development
  exception, not a claim of literal standards compliance. Non-loopback OAuth
  endpoints MUST be published through HTTPS.
- RFC 9700 permits a native `localhost` redirect registration to vary its port.
  dsmctl currently requires the complete registered redirect string, including
  port, at both endpoints. That is stricter and safe for dynamically registered
  clients, though it may be less interoperable with clients that expect the
  native-app port exception.
- The accepted power-user design currently chooses Full access and 365 days as
  manual defaults. F-03 and the lifetime policy in F-05 must not silently
  overwrite that decision. If WI-097 is accepted, update
  `spec/mcp-power-user-connection-design.md`, `docs/gateway.md`, the UI, server
  policy, OAuth challenge/protected-resource metadata, and tests together.

## Extending and re-validating this record

When a listed gap is implemented, mark its work item `done` and leave this
record as the historical rationale. A new connect-surface review appends a new
dated section instead of erasing the earlier evidence. Re-run the review when
OAuth, bearer persistence, tool scopes, NAS roles, approval transactions,
forwarded-header/public-URL behavior, or the supported MCP authorization
protocol version changes.

At minimum, re-run:

```console
go test ./internal/gateway/... ./internal/mcpserver/... -count=1
go vet ./internal/gateway/... ./internal/mcpserver/...
```
