# MCP power-user connection and identity design

Status: accepted product direction; OAuth URL login shipped by WI-048 and
remaining connection gaps stay tracked below.

This document defines how a trusted operator connects MCP clients to a private
dsmctl MCP Server. It is a design specification, not a claim that every flow
described below is implemented today. Current behavior that users may rely on
remains documented in [`docs/gateway.md`](../docs/gateway.md).

## Product position

dsmctl MCP Server is a private, single-owner power-user appliance:

- it is installed and administered by one trusted owner;
- it is reachable only from host loopback, a trusted LAN, or a VPN, with HTTPS
  termination at a trusted reverse proxy for non-loopback access;
- its clients are operator-controlled AI applications, IDEs, and automation,
  not anonymous Internet users;
- one owner may connect several client installations and should issue one
  credential per installation;
- it is not a public SaaS, multi-tenant service, organization identity
  provider, or delegated DSM-user portal.

The product should optimize for completing real NAS administration work with
low setup friction. The default client credential therefore exposes every MCP
capability, while NAS targets, credential lifetime, interactive execution
consent, and high-risk approval remain explicit controls.

## Decision summary

1. The default connection preset is **Full access** with `nas.read`,
   `nas.plan`, `nas.apply`, and `lan.discover`.
2. The Agent/MCP Host is the primary execution-consent boundary: it lets the
   owner inspect the intended call and asks every time before invoking an
   `apply_*` tool.
3. A connection must explicitly select at least one NAS when it includes a
   `nas.*` scope. There is no implicit all-NAS grant and no remote default-NAS
   fallback.
4. The connection wizard defaults to a 365-day credential. No-expiry remains
   an explicit advanced choice with a durable-access warning.
5. A bearer token identifies a client installation by possession. It does not
   prove which human is operating that client at request time.
6. The primary connection flow is standards-based MCP OAuth: paste the MCP URL
   into a compatible client and authenticate in the Gateway browser page.
   Manually configured client access tokens remain a fallback for headless and
   legacy clients.
7. Gateway administration, MCP authorization, and the downstream DSM account
   stay independent. None of these identities is silently inherited by
   another layer.
8. Agent confirmation is user-facing consent, not server-verifiable
   authorization. Plan/apply validation remains authoritative, and high-risk
   apply still requires a separate short-lived Admin UI approval.

## Default authority model

### Presets

| Preset | Scopes | Intended use |
| --- | --- | --- |
| **Full access (default)** | `nas.read`, `nas.plan`, `nas.apply`, `lan.discover` | A trusted AI client expected to use the complete dsmctl MCP surface, with sensitive execution intercepted by its Agent/MCP Host. |
| NAS operator | `nas.read`, `nas.plan`, `nas.apply` | Full management of reviewed NAS profiles without observing the local network. |
| Planner | `nas.read`, `nas.plan` | A client that may prepare plans but must hand them to another client for apply. |
| Observer | `nas.read` | Dashboards, inventory, troubleshooting, and low-trust integrations. |
| Custom | Any valid combination | Advanced policy construction; the UI explains unusual combinations. |

The wizard presents presets in user language first. Raw scope checkboxes move
under an Advanced disclosure. The default is visibly described as:

> Use every dsmctl MCP capability, including local-network discovery. Your
> Agent asks before each apply. High-risk changes also require a separate
> approval in this Admin UI.

### Why `nas.apply` is in the default

Power users connect dsmctl because they want an agent to finish a task, not
only report what could be done. `nas.apply` does not bypass the architecture's
mutation controls: the client still needs a canonical plan and approval hash;
the application rereads state, rejects stale plans, protects built-in
resources, performs a typed operation, and verifies the postcondition. A
remote high-risk apply additionally needs a short-lived, single-use approval
created outside the MCP conversation.

Low- and medium-risk applies do not require that out-of-band approval. The
wizard must say this plainly before creating a Full access connection and must
configure the selected Agent/MCP Host to ask before every `apply_*` invocation
where that client supports such a policy. The larger default is an intentional
trust decision for this private product, not an assertion that every mutation
is harmless.

### Why `lan.discover` is in the default

`lan.discover` is targetless and can reveal devices that are not present in
the NAS allowlist. That broader observation surface is intentional in the Full
access preset: this product is installed by a trusted owner on a private
LAN/VPN for power-user operation, and discovery is part of the complete MCP
capability set the owner asked to make available. The review step must state
this consequence explicitly.

Its enforcement remains independent from `nas.*`. Selecting Full access does
not make discovered devices manageable, implicitly grant every stored profile,
or remove the requirement to review at least one NAS allowlist entry. Owners
who do not want LAN observation can select NAS operator or Custom.

### Target and lifetime defaults

- The owner must select one or more existing NAS profiles. If exactly one
  profile exists, the wizard may preselect it but still displays the grant in
  the review step.
- Selecting no NAS is valid only for a discovery-only custom connection. The
  current behavior of silently creating a `nas.*` token that can reach no NAS
  is replaced by an actionable validation error.
- The default lifetime is 365 days. Thirty- and ninety-day choices remain,
  while no-expiry moves to Advanced and states that revocation becomes the
  only credential-lifetime control.
- Authority and lifetime are independent. A power-user default is not a reason
  to issue one shared, permanent token to every client.

## Identity model

### Three independent identity boundaries

| Boundary | Current proof | What the server can assert | What it cannot assert |
| --- | --- | --- | --- |
| Gateway administrator | Local username/password and an expiring browser-session cookie | The initialized owner authenticated to the Admin UI and authorized an administrative action. | That an MCP request is currently being driven by the same human. |
| MCP client | Possession of one random `dsmctl_mcp_*` bearer token | The request belongs to the token ID/name, scopes, NAS allowlist, expiry, and revocation state stored for that connection. | The physical person at the keyboard, an e-mail address, or a DSM user identity. |
| DSM executor | The encrypted session or credential stored for one NAS profile | DSM accepted the configured profile account for the downstream operation. | The identity of the human or MCP client that originated the request. |

The current principal is constructed from token ID, token name, scopes, and
NAS allowlist in
[`internal/gateway/state/policy.go`](../internal/gateway/state/policy.go). The
gateway authenticates it before MCP initialization in
[`internal/gateway/server.go`](../internal/gateway/server.go). Client-supplied
names, IP addresses, proxy identity headers, DSM cookies, and Admin UI cookies
must never become MCP authority.

### What “this is me” means in version 1

In the private single-owner model, “me” means:

1. the owner authenticated to the Admin UI when creating the connection; and
2. a particular client installation later proved possession of the credential
   issued for that connection.

It is not continuous human authentication. Anyone who copies the bearer token
has the same authority. The UI must call it a **client access token**, not a
personal identity, and display this warning before revealing it:

> This token represents this MCP client. Anyone who possesses it receives the
> same NAS and operation permissions. Store it in the client's secret storage
> and do not share it between devices.

One token is issued per client installation. Recommended connection names are
human-readable and specific, for example `Codex · Deryc desktop` or
`Home Assistant · NAS host`. The immutable token ID remains the audit identity;
the name is descriptive metadata, not proof.

The token record should also retain `client_type` and `created_by` metadata so
the Admin UI can explain provenance. These fields improve accountability but
do not change the possession-based authentication claim.

### Downstream accountability

dsmctl Audit can attribute a request to a client token and, after the proposed
metadata addition, to the administrator who created that connection. DSM will
still record the DSM account stored on the selected NAS profile. If several
humans share the gateway and one DSM profile, end-to-end human attribution is
not available. Per-human Gateway accounts and per-human DSM credential
bindings are a future multi-user product, not part of this design.

## Target connection experience

The overview action is **Set up MCP access** because it navigates to a page
with two distinct paths. That page presents **Connect with the MCP URL** for
standard OAuth and **Create manual token** only for headless or legacy clients.
No navigation action claims to create a credential, and no manual-token action
is labeled as if the Gateway initiates a client connection.

### Entry conditions

The wizard is enabled after at least one NAS profile has a stored credential
and a successful connection test. If no NAS is ready, it links directly to the
missing profile or sign-in step instead of allowing the owner to create a
nonfunctional connection.

### Wizard

1. **Choose client** — Codex, Claude, VS Code, or Generic Streamable HTTP.
   Each choice declares whether the tested client version accepts custom
   Authorization headers or requires MCP OAuth.
2. **Name connection** — prefill a client/device-specific name; explain that
   this is the audit identity for that client installation.
3. **Choose NAS access** — use a searchable checkbox list, not a Ctrl/Cmd
   multi-select. Preselect the sole profile only when the review remains
   explicit.
4. **Choose authority** — preselect Full access. Show that it includes all four
   scopes, explain that discovery can observe devices outside the NAS
   allowlist, and state that the Agent asks before apply.
5. **Choose lifetime** — default 365 days; shorter and advanced no-expiry
   options remain available.
6. **Review** — show client, full external endpoint, selected NAS profiles,
   scopes, expiry, and the bearer-possession warning.
7. **Issue and configure** — create the token only after review. Reveal it once
   in a modal with Copy token, Copy endpoint, Copy complete configuration, and
   client-specific instructions. The secret is not persisted in browser
   storage and is cleared when the modal is dismissed or the page reloads.
8. **Verify first use** — show `Waiting for <client> to connect`. A real MCP
   authentication updates the token's `last_used_at`; the page then changes to
   `Connected` with the first/last-use time. A separate optional in-browser
   token check may validate the credential but must not be presented as proof
   that the external client is configured.

### External endpoint derivation

The UI must show and copy an absolute external MCP URL. It cannot hard-code
`/mcp` because the Synology alias exposes `/dsmctl/mcp` while local Compose
normally exposes `/mcp`.

The browser can derive the sibling endpoint from its validated location:

```text
http://127.0.0.1:18765/admin/  -> http://127.0.0.1:18765/mcp
https://nas.example/dsmctl/admin -> https://nas.example/dsmctl/mcp
```

The derivation preserves scheme, authority, and every path segment before the
terminal `/admin`. It never uses a query parameter or unvalidated
client-supplied Host/Forwarded value as authority. Deployment tests cover
loopback, a root reverse proxy, and the Synology `/dsmctl` prefix.

### Configuration handoff

The Generic view explains that the transport is Streamable HTTP and shows the
two invariant values:

```text
URL: https://nas.example/dsmctl/mcp
Authorization: Bearer dsmctl_mcp_<secret>
```

Client tabs provide versioned, tested configuration rather than pretending one
JSON schema works everywhere. A conceptual configuration is:

```json
{
  "name": "dsmctl",
  "transport": "streamable-http",
  "url": "https://nas.example/dsmctl/mcp",
  "headers": {
    "Authorization": "Bearer dsmctl_mcp_<secret>"
  }
}
```

If a selected client accepts only a URL and standard OAuth, the wizard stops
before token issuance and says that the current release is not compatible. It
must not ask the user to improvise an unsupported header field.

### Agent-side execution consent

Broad authorization at issuance is paired with interactive consent at
execution. The connection recipe should configure the selected Agent/MCP Host
to use the following behavior where the client supports per-tool policy:

| Tool class | Default Agent/MCP Host behavior | Server-side boundary |
| --- | --- | --- |
| Read, list, and get | Proceed under normal host policy; no mutation confirmation is required. | Token validity, `nas.read`, and the explicit NAS allowlist. |
| `discover_*` | Proceed after the host's normal permission notice; the connection review already discloses LAN observation. | Token validity and `lan.discover`; discovered devices gain no NAS authority. |
| `plan_*` | Proceed and display the resulting plan; planning does not mutate DSM. | Token validity, `nas.plan`, NAS allowlist, and canonical plan construction. |
| `apply_*` at low or medium risk | **Always ask** before sending the call. Show the target, plan summary, and material tool inputs. A denial means no apply request is sent. | Token validity, `nas.apply`, NAS allowlist, plan hash, state revalidation, typed operation, and postcondition checks. |
| `apply_*` at high risk | **Always ask**, then direct the owner through the separate Admin UI approval. | All apply checks plus an exact, short-lived, single-use, out-of-band approval. |

The MCP server already labels read-only tools and mutation tools with
`ToolAnnotations` in
[`internal/mcpserver/server.go`](../internal/mcpserver/server.go), with coverage
in [`internal/mcpserver/server_test.go`](../internal/mcpserver/server_test.go).
Those annotations help a host choose a confirmation UX, but the MCP
specification defines them as hints. They are not cryptographic authority, and
the server cannot prove that a host actually displayed or received a user
confirmation. Server policy therefore never accepts an Agent prompt, model
statement, annotation, or client-side "approved" field in place of its own
checks.

The wizard provides tested client-specific instructions for setting
`apply_*` tools to Always ask. If a supported client cannot enforce that
policy, the wizard must show a prominent warning and require explicit owner
acknowledgment; it must not claim that the server can supply the missing
client-side prompt. This consent model follows the MCP requirements and
guidance for host-controlled user authorization and sensitive tool calls:

- [MCP specification: user consent and control](https://modelcontextprotocol.io/specification/2025-11-25)
- [MCP tools: human in the loop](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)
- [MCP schema: tool annotations are hints](https://modelcontextprotocol.io/specification/2025-11-25/schema)

## Current gap register

Priority meanings: P0 blocks a normal first connection, P1 materially weakens
clarity, safety, or interoperability, and P2 is cleanup after the primary flow
works.

| ID | Priority | Current evidence | Intended correction | Completion signal |
| --- | --- | --- | --- | --- |
| GAP-CONN-001 | P0 | The overview and MCP access page show only `/mcp` in [`internal/gateway/admin/ui.go`](../internal/gateway/admin/ui.go), while the Synology package requires `https://NAS/dsmctl/mcp` in [`docs/synology-package.md`](../docs/synology-package.md). | Derive and display the absolute external sibling endpoint, preserving a reverse-proxy prefix. | Browser tests prove correct copy values for local root, custom origin, and `/dsmctl` paths. |
| GAP-CONN-002 | P0 | The primary UI action is token creation; it has no client selection, configuration snippet, endpoint copy, or ordered next step. | Replace the primary flow with the eight-step Connect MCP client wizard and retain raw token management as Advanced. | A new owner can reach a first authenticated `list_nas` call using only UI instructions. |
| GAP-CONN-003 | P0 | Only `nas.read` is checked by default even though the product targets trusted power users. | Add named presets and select Full access (`nas.read + nas.plan + nas.apply + lan.discover`) by default. | Created default connections contain exactly all four supported scopes; authorization-table tests remain green. |
| GAP-CONN-004 | P0 | `renderIssuedToken` writes one text block; there are no dedicated secret-copy or complete-config actions. | Use a one-time modal with Copy token, Copy endpoint, and Copy configuration actions plus explicit secret-storage copy. | UI tests prove the token is shown once, never persisted, and absent after dismissal/reload. |
| GAP-CONN-005 | P1 | The NAS allowlist is a browser multi-select requiring Ctrl/Cmd, and an empty selection creates a token that cannot use `nas.*` tools. | Use searchable checkboxes and require at least one NAS for presets containing `nas.*`. | Keyboard, mobile-width, one-profile, multi-profile, and empty-selection tests pass. |
| GAP-CONN-006 | P1 | The issued-token table exposes last use, but the creation flow never waits for or celebrates the first real client request. | Add waiting, connected, expired, revoked, and never-used connection states driven by server metadata. | An authenticated initialize changes the wizard result to Connected without exposing the token. |
| GAP-ID-001 | P0 | The UI does not clearly say that bearer possession identifies a client rather than a human. | Rename the concept to client connection/access token and show the possession warning before issue. | All locales carry the same identity claim; no copy states that a token proves a human or DSM identity. |
| GAP-ID-002 | P1 | Token state stores name and ID but not client type or creating administrator; audit tables emphasize raw actor IDs. | Store immutable `client_type` and `created_by`, resolve safe display names in Admin UI, and retain token ID as the authority key. | Connection detail and audit views show owner-readable provenance without accepting display metadata as authority. |
| GAP-CONSENT-001 | P0 | Mutation tools already carry destructive annotations, but the Admin connection flow neither configures nor verifies an Agent/MCP Host policy that asks before `apply_*`. | Add client-specific setup that sets `apply_*` to Always ask where supported, displays target/plan/material inputs, and warns when the selected client cannot enforce it. Never treat the prompt or annotation as server authority. | Every advertised supported-client recipe is tested to intercept apply before the request is sent; unsupported confirmation behavior requires an explicit warning and owner acknowledgment. |
| GAP-LIFE-001 | P1 | No expiry is the current first option, which silently couples high default authority to permanent lifetime. | Default to 365 days; keep no-expiry in Advanced with a durable-access warning. | Default creation has an expiry; explicit no-expiry remains supported and audited. |
| GAP-COMPAT-001 | P1 | Closed by WI-048: the managed Gateway publishes OAuth Protected Resource Metadata, authorization-server discovery, DCR, authorization code with S256 PKCE, and rotating refresh tokens. | Keep URL login as the recommended path and manual tokens as the explicit headless/legacy fallback. | A dynamically registered URL-only client can authorize and authenticate `/mcp`; manual Bearer configuration remains functional. |
| GAP-DOC-001 | P1 | Current docs explain the header but do not provide one authoritative per-client setup flow; the checked-in MCP access screenshot still shows the superseded `nas.admin` label. | Make the wizard the source of truth, add versioned client recipes, and refresh localized screenshots after implementation. | Documentation and screenshots match `lan.discover`, the external endpoint, presets, and the tested client matrix. |
| GAP-TEST-001 | P1 | Existing authorization tests validate policy, but there is no end-to-end test from UI-issued secret through an actual Streamable HTTP client setup and first-use state. | Add managed-gateway integration coverage for issue, initialize, tool listing/filtering, last-use update, rotate, expire, and revoke. | The full flow passes on root-path Compose and prefixed Synology-proxy fixtures without live DSM mutation. |

## OAuth interoperability track

The private token flow is intentionally simple and remains useful for
clients that allow a custom Authorization header. It is not the standard
browser authorization experience expected by clients that accept only a
remote MCP URL.

The current official MCP authorization framework is optional at the protocol
level. When it is used for an authenticated HTTP MCP server, the protected
server acts as an OAuth resource server. The framework defines Protected
Resource Metadata, authorization-server discovery, authorization code with
PKCE, resource/audience binding, and bearer access-token validation:

- [MCP Authorization, protocol revision 2025-11-25](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)
- [Connecting to remote MCP servers](https://modelcontextprotocol.io/docs/develop/connect-remote-servers)
- [Streamable HTTP transport](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)

WI-048 implements an embedded authorization server for the managed Gateway.
It uses the existing local administrator as the resource owner without
introducing a multi-user account system. The flow:

1. expose OAuth Protected Resource Metadata at the prefix-correct well-known
   path and reference it in authentication challenges where appropriate;
2. expose OAuth authorization-server metadata or supported OIDC discovery;
3. use authorization code with S256 PKCE and exact redirect-URI validation;
4. show client identity, redirect destination, requested preset/scopes, NAS
   grants, and lifetime on a browser consent page;
5. issue short-lived, audience-bound access tokens and securely rotated refresh
   tokens;
6. audit immutable owner subject, client ID, grant, and token identity without
   logging credentials;
7. keep the Admin UI cookie unusable at `/mcp` and keep MCP access tokens
   unusable at `/admin`.

OAuth improves client interoperability and lets the gateway associate a
request with the authorized owner subject and client as part of a standard
grant. It still does not prove the physical human remained present for every
request, make the downstream DSM account per-human, or replace plan/apply and
high-risk approval.

## Security invariants

The power-user default must not weaken these existing contracts:

- `/admin` authority is never an MCP scope.
- Every remote NAS-scoped call names its target explicitly and passes the
  token's NAS allowlist.
- `list_nas` and safe metadata are filtered to the caller's allowlist.
- `lan.discover` stays separate from `nas.*` and the NAS allowlist.
- Every mutation uses the typed application plan/apply boundary; no generic DSM
  mutation or raw WebAPI tool is introduced.
- Apply revalidates token status, scope, NAS grant, profile revision, plan hash,
  stable identity, observed state, and postcondition as applicable.
- High-risk remote apply still consumes an exact, short-lived, single-use,
  out-of-band approval bound to the requesting token and NAS revision.
- Token values, Authorization headers, Admin cookies, DSM sessions, passwords,
  OTPs, master keys, ciphertext, and request bodies remain absent from display,
  logs, audit, and persistent plaintext.
- Token digests remain the stored verifier. Rotation invalidates the old value
  immediately; expiry and revocation are checked on every request.
- The backend remains loopback-only at the deployment boundary. Non-loopback
  client access uses a trusted HTTPS reverse proxy, LAN, or VPN; no design here
  authorizes direct Internet publication.
- The server never trusts a caller-provided display name, user header, source
  IP, or model statement as identity or authority.

## Implementation order

### Slice 1 – Connection usability (P0)

Implement the external endpoint derivation, Full access preset, NAS checkbox
selector, one-time secret modal, copyable client configuration, Agent-side
Always ask setup, and first-use status. This closes GAP-CONN-001 through
GAP-CONN-006, GAP-ID-001, and GAP-CONSENT-001 without changing the enforced
server policy model.

### Slice 2 – Connection provenance and lifecycle

Add client type/creator metadata, connection-centric tables and audit labels,
the 365-day default, complete integration coverage, and refreshed user docs.
This closes GAP-ID-002, GAP-LIFE-001, GAP-DOC-001, and GAP-TEST-001.

### Slice 3 – Standard remote authorization

Implemented by WI-048. The OAuth interoperability track closes
GAP-COMPAT-001. Manual client access tokens remain available for scripts and
power users that prefer explicit secret configuration.

## Success criteria

- A first-time owner can add/authenticate one NAS and complete a real MCP
  `list_nas` call without consulting repository documentation.
- The copied URL is correct on local Compose, a root reverse proxy, and the
  Synology `/dsmctl` alias.
- The default connection receives all four scopes, while `nas.*` operations
  remain limited to the reviewed NAS profiles.
- Every advertised Agent/MCP Host intercepts `apply_*`, shows the intended
  target and action, and asks before sending it; high-risk apply additionally
  requires Admin UI approval.
- The owner can answer, from the connection and Audit views, which client
  credential acted, who created it, which NAS it could reach, what authority it
  had, when it was first/last used, and whether it is still valid.
- No UI or documentation claims that a manual bearer token proves a human or
  DSM user identity.
- Every advertised MCP client/configuration pair is exercised by a repeatable
  compatibility check, and unsupported OAuth-only clients fail with guidance
  before a token is issued.
