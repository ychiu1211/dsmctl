# Portable amd64 MCP gateway

`dsmctl-gateway` exposes the existing application layer over stateless MCP
Streamable HTTP. The image is a platform-neutral `linux/amd64` image: it runs
under Docker Engine, Podman, or Synology Container Manager without changing
the binary. The Synology SPK is a deployment wrapper around this same image,
not a separate DSM-specific build. Production Linux instructions live in
`deploy/linux/README.md`; the offline package is documented in
`docs/synology-package.md`.

The managed gateway stores up to 32 NAS profiles in a transactional embedded
database and exposes an authenticated administration page at `/admin/`.
Profiles, credentials, MCP tokens, and authorization changes take effect
without restarting the process. Managed mode exposes the complete MCP tool
surface through per-token NAS allowlists and independent `nas.read`,
`nas.plan`, `nas.apply`, and `lan.discover` scopes. The static WI-014 developer
mode remains read-only.

Generic managed deployments require the local first-run administrator flow
described below. The Synology SPK adds a loopback host bridge that reuses
dsmctl's DSM Web Login authorization-code, PKCE, and Noise exchange, checks the
returned account's effective `administrators` membership, and gives the core
gateway a short-lived signed login assertion instead of a DSM cookie, SID, or
password. The core then creates its own independent Gateway browser session. A
fresh SPK therefore starts in DSM-Web-Login-only mode; a DSM-authenticated
administrator can later enable the same local login as an optional fallback.
The SPK pins `--administrator-mode=dsm`; if its assertion key is absent or
invalid, the Gateway refuses to start instead of exposing generic first-run
account creation. Supplying a platform assertion key directly is reserved for
managed deployment adapters and is not a way to bypass generic-container setup.

## Session model

MCP transport requests are stateless and return JSON. The gateway does not
issue or rely on a durable MCP session ID and does not open a standalone SSE
stream. DSM connectivity is intentionally different: the existing runtime
manager lazily keeps one client and authenticated DSM session per configured
NAS profile. Calls to different NAS profiles may run concurrently, while the
Synology client continues to serialize authentication and retry a request once
after an expired DSM session.

Stopping the process drains bounded in-flight HTTP requests and then closes
all cached DSM sessions.

## Managed Compose startup

The checked-in Compose project publishes the gateway only on
`127.0.0.1:18765`. Prepare its local files from the repository root:

```console
cd deploy/container
mkdir -p data secrets
openssl rand 32 > secrets/master.key
chmod 700 data secrets
chmod 600 secrets/master.key
sudo chown -R 10001:10001 data secrets
docker compose up --build
```

Open `http://127.0.0.1:18765/admin/` and create the local Gateway administrator
username/password. Setup remains available until the first account is created,
including across process restarts; keep it on loopback or a trusted deployment
network until then. The first successful setup closes the endpoint transactionally and
creates an expiring HttpOnly/SameSite browser session; the password is stored
only as an Argon2id verifier. There is no setup code or long-lived
administrator bearer token.

Administrator passwords contain at least 8 Unicode characters; choosing a
strong password is the operator's responsibility. Later password changes
require the new password twice because there is no recovery path. A forgotten
administrator password cannot be recovered or reset: delete the gateway state
data and reinstall the deployment, then run first-time setup again — every NAS
profile, token, and audit record is removed. The login page states the same
recovery path.

An initialized unauthenticated browser sees the normal login page. The Gateway
cannot determine whether that viewer was the installer. If the first visit is
unexpectedly initialized, reset the empty deployment state before enrolling a
NAS. Resetting a used state deletes its NAS sessions and MCP credentials.

The administration page identifies the product as **dsmctl MCP Server** and
shows the absolute, deployment-prefix-aware `/mcp` client endpoint. Its
overview reports NAS, client-connection, and approval status. The NAS, MCP
access, approval, Audit, and administrator views are list- and state-first:
creation, reauthentication, manual fallback, filtering, and password forms
open only when requested. English,
Traditional Chinese, Simplified Chinese, Japanese, and German are built in.
The Traditional Chinese step-by-step operator guide is available in
[`gateway-admin-guide.md`](gateway-admin-guide.md).
The page initially uses the saved locale or browser preference, and the locale
selector is available before and after login. Only the locale identifier is
stored in browser-local storage; it is not authentication state. On narrow
screens navigation and tables scroll independently. The embedded page uses no
CDN, remote font, translation service, or other external rendering asset.

The shared three-step NAS wizard first searches the gateway host's local
broadcast domain or accepts a manually entered IP, DNS name, or DSM URL. A
discovered device pre-fills its hostname and representative IPv4 address, but
the administrator still confirms the URL because findhost does not advertise
a reliable custom DSM port. Editing and incomplete setup reuse the connection
step; signing in again reuses the authentication step and can navigate back to
the same connection fields.

The page does not ask the administrator to choose a TLS mode or type a
fingerprint. Every profile starts with system-CA, hostname, and validity
verification. Immediately before Web Login, password/OTP enrollment, or
connection diagnostics, the Gateway performs a credential-free TLS handshake.
If normal verification fails because of CA, hostname, or validity policy, the
page shows every warning plus the identity, validity period, and SHA-256
fingerprint actually observed by the Gateway, then asks whether to trust and
pin that exact certificate. This deliberately supports a NAS reached only by a
LAN IP missing from its certificate SAN, but only after explicit administrator
consent. A missing/unparseable certificate or TLS protocol, cryptographic
handshake, or network failure cannot be pinned. A pin authenticates exactly
one observed leaf certificate in place of CA, hostname, and validity checks, so certificate
rotation fails closed and shows the old and newly observed fingerprints before
an administrator may replace it. CA-issued certificates stay on system trust
and continue validating across ordinary renewals. Changing a profile URL clears
an old endpoint's pin and starts system verification again. These policies
remain stored internally as `system_ca` and `pinned_fingerprint`.

The DSM account is deliberately not guessed during profile creation.
Password/OTP enrollment collects the account together with masked
credentials and commits the verified account and encrypted credentials in one
revision-checked transaction. Web Login likewise records the account that DSM
actually signed in. Profiles can later edit URL and timeout, but not their name
or a manually selected TLS policy. A profile without credentials exposes a primary
**Complete setup** action; after credentials are stored, reauthentication
becomes a deliberate row-menu action. A stored credential is not presented as
a live health result. **Connection diagnostics** reports DNS, TCP, TLS/HTTP,
and DSM authentication as named stages. Production managed mode has no
skip-verification option. Sign in
through the NAS's own DSM page (the gateway stores the resulting SID,
SynoToken, and Noise resume keys), or use the bounded password/OTP enrollment
for an automation account. Web sessions resume headlessly and survive gateway
restarts. The container never reads the host's desktop OS keyring.

For a profile whose password is stored (from password/OTP enrollment), the
row menu adds a **Reveal stored password** action. It opens a dialog that
requires the administrator to re-enter their own administrator password; on
success the gateway returns the stored DSM account password once, with a copy
button, and clears it when the dialog closes. The reveal endpoint is rate
limited per source, writes a `credential.reveal` audit event naming the NAS
(never the value), and is part of local administration only — it is never an
MCP tool and cannot be reached with a bearer token. This is the sole way the
vault surfaces a stored password, implementing the vault-managed,
human-gated-reveal policy.

The NAS page also offers an **Export credentials** action that applies the same
gate at bulk scope. It opens a dialog requiring the administrator to re-enter
their own administrator password, then downloads one CSV
(`dsmctl-nas-credentials.csv`) with a row per NAS profile — `name`, `host`,
`url`, `account`, `password` — where a profile with no stored account or
password exports an empty field. Like the single reveal it reads only
vault-enrolled passwords (never the `DSMCTL_PASSWORD_*` environment fallback),
is rate limited per source, and is audited as its own `credential.export`
action recording only the profile and stored-password counts, never a value.
The download is the only place the passwords appear; they are never rendered
into the page.

The stored Gateway pin protects Gateway-to-NAS traffic. Because the Web Login
popup connects the administrator's browser directly to DSM, that browser may
still display its own self-signed-certificate warning unless its trust store
also trusts the certificate.

The relay is tested against the DSM protocol locally. If a particular DSM
release refuses a non-loopback `opener` origin, use password/OTP enrollment for
that NAS until its browser-origin behavior is verified and supported.

For a custom host name or LAN address, add it to `DSMCTL_ALLOWED_HOSTS` and add
the exact browser origin to `DSMCTL_ALLOWED_ORIGINS` before starting Compose.
If a reverse proxy changes the public origin used by the browser, pass
`--admin-public-url=https://gateway.example` as well.

The MCP URL is `http://127.0.0.1:18765/mcp` for the default local deployment.
The administration page displays and copies the absolute URL, including a
reverse-proxy path prefix when present. The recommended connection path is to
paste only that URL into an OAuth-capable MCP client. The client discovers the
Gateway authorization endpoints, opens a browser page, and the owner approves
the connection with the existing Gateway administrator username and password.
This login is local to the Gateway; it does not expose or replace any stored
DSM credential. OAuth access tokens expire after one hour and are renewed with
a rotating refresh token whose maximum lifetime is 365 days.

For headless automation and clients that cannot perform browser OAuth, create
a manual token on the MCP Access page and send it as
`Authorization: Bearer <token>`. Manual plaintext is shown only at creation or
rotation; the database stores its SHA-256 digest. OAuth access and refresh
secrets are likewise stored only as digests. Missing, malformed, expired, and
revoked credentials are rejected before MCP initialization, and request limits
are tracked independently by credential identity. `/healthz` is local process
liveness and never contacts DSM. `/readyz` verifies the state schema,
established admin, and mounted master key; it does not poll the NAS fleet.

Every remote NAS-scoped tool call must include an explicit `nas` argument. The
gateway never resolves an omitted remote target through the default profile;
`list_nas`, `discover_lan_devices`, and the targetless `get_auth_status` form
are the only targetless exceptions. Local CLI and stdio calls retain default
profile resolution.

`nas.read` exposes only read tools and filters profile/fleet/credential views to
the token's NAS allowlist. `nas.plan` and `nas.apply` are independent: a token
may prepare plans without applying, or apply a previously delivered canonical
plan without gaining general read access. `lan.discover` admits only
`discover_lan_devices`. Its distinct prefix is deliberate: discovery may reveal
devices outside the configured NAS allowlist, while gateway administration is
never an MCP scope. Every request re-evaluates token status, scope, and target.

The manual-token API may set no expiry or an explicit expiry. Both URL login
and the power-user manual-token UI grant all currently configured NAS profiles
and all four scopes by default; every NAS remains an explicit allowlist entry.
This broad default does not bypass agent-side confirmation before apply, and
high-risk plans still require Gateway approval. The manual UI defaults to a
365-day lifetime and also offers 30 days, 90 days, or no expiry as an advanced
choice. The credential list distinguishes never used, used, expired, and
revoked credentials. OAuth credentials can be expired or revoked; their
rotating refresh path is invalidated at the same time. Manual credentials can
also be rotated. Scopes and NAS allowlists are immutable after creation;
changing authority means issuing a new client credential. Deleting a NAS
transactionally removes that name from every token allowlist, so recreating the
same profile name never restores old access.

Low- and medium-risk remote plans require `nas.apply` and retain the existing
plan hash, profile revision, stable-ID, precondition, protected-resource, and
postcondition checks. High-risk plans additionally require a matching approval
created out of band on `/admin/`. It is bound to one plan hash, NAS profile
revision, requesting token, and local administrator, expires after at most ten
minutes, and is atomically consumed once before application precondition reads.
A stale or failed apply never restores a consumed approval.

Successful remote high-risk `plan_*` calls create a bounded, 24-hour pending
request containing only the plan hash, NAS/revision, requesting token, tool,
risk, optional stable resource ID, and canonical summary. The Approvals page
can approve that request in one click or dismiss it. Requests deduplicate per
plan hash and token, keep at most 50 entries, and never affect admission checks.
The manual fallback selects an existing profile and active token, validates the
64-hex plan hash, and captures the current profile revision server-side. If an
approval was created by mistake, revoke or rotate the requesting token; apply
admission rechecks token status before consuming the approval.

The administration page displays newest events first in a table. Time, actor,
and action filters open on demand, and the active filter is summarized above
the list. JSONL export contains every retained event in chronological order,
regardless of the visible filters. Records use a closed
scalar schema and never include request
bodies, authorization headers, passwords, OTPs, trusted-device values,
SynoTokens, SIDs, cookies, master keys, or encrypted vault payloads. Retention
is bounded to 10,000 events and 30 days. A mandatory audit write failure blocks
admin mutations and remote apply admission before DSM mutation.

To put a trusted reverse proxy in front of the loopback listener, explicitly
set:

- `DSMCTL_ALLOWED_HOSTS` to the HTTP host names accepted by the backend;
- `DSMCTL_ALLOWED_ORIGINS` to exact browser origins, if browser MCP clients are
  used; requests without an `Origin` header remain valid for non-browser MCP
  clients;
- `DSMCTL_TRUSTED_PROXIES` to proxy CIDR ranges whose `X-Forwarded-For` value
  may be used for request logging.

TLS termination belongs at that proxy. Do not publish the development gateway
directly to the Internet.

## Direct binary startup

The same executable can run on an ordinary amd64 Linux host:

```console
dsmctl-gateway \
  --listen=127.0.0.1:18765 \
  --state=/srv/dsmctl/gateway.db \
  --master-key-file=/run/secrets/master.key \
  --allowed-hosts=localhost,127.0.0.1
```

Omit `--state` only to retain the WI-014 static-config development mode.
Managed startup fails closed for a missing, malformed, or wrong master key.
Until a local administrator is created, MCP remains unavailable while the
health and setup page stay reachable. At most 32 NAS profiles are accepted,
per-profile timeouts are capped at 120 seconds, and at most 8 MCP requests run
concurrently by default.

## State, backup, and secret references

`gateway.db` uses bbolt transactions and a versioned schema. A pre-migration
backup is created beside the database before forward migration; migration or
key validation failure keeps readiness false. Passwords, trusted-device IDs,
web-login sessions, and apply secrets are encrypted independently with
AES-256-GCM and authenticated profile/type metadata. The master key is never
copied into the database or its backup.

The pre-release schema-3 bearer/platform administrator modes are not carried
forward as authority. An empty schema-3 state returns to first-run setup. If it
already contains profiles, encrypted secrets, MCP tokens, or approvals, schema
4 writes a pre-migration backup and refuses to expose a fresh setup endpoint;
the operator must deliberately reset that pre-release state or restore and
migrate it with a version that still understands the old credential.

Schema 5 adds pending approval requests and transactionally rewrites every
stored `nas.admin` scope to `lan.discover`. After migration `nas.admin` is
invalid input; there is no alias or dual-acceptance window.

Schema 6 adds public OAuth client registrations and digest-only rotating
refresh-token records. Authorization codes remain short-lived and in memory;
access tokens continue to use the existing digest-only MCP-token record and
authorization policy.

The admin API can create opaque `vault:<id>` apply-time references. Only the
application's apply-time resolver can decrypt those values; MCP results and
plan hashing see only the reference. Removing a NAS deletes its credentials by
default. With explicit credential retention, the API lists orphan metadata so
the administrator can later delete it.

Two admin endpoints deliberately return plaintext, both requiring the
administrator to re-enter their own administrator password.
`POST /admin/api/profiles/{name}/credentials/reveal` gives the signed-in
administrator browser session one profile's DSM account and its vault-enrolled
password (the NAS list's Copy account / Copy password actions).
`POST /admin/api/credentials/export` returns a CSV of every profile's `name`,
`host`, `url`, `account`, and vault-enrolled `password` (empty when none is
stored), audited as `credential.export`. Both answer only vault-enrolled
passwords, never consult the `DSMCTL_PASSWORD_*` environment fallback, never
return web-login session material or apply secrets, and are rate limited per
source; reveal answers 404 when no password is enrolled. MCP bearer tokens
cannot call any `/admin/api/` route, so no secret is reachable over `/mcp`.

## Container security and portability

The image is built with `CGO_ENABLED=0` for `linux/amd64`, contains a single
static executable and CA roots, runs as numeric UID/GID `10001`, and requires
no shell. The Compose project uses a read-only root filesystem, a 16 MiB
`/tmp` tmpfs, drops every Linux capability, enables `no-new-privileges`, and
applies process, memory, CPU, and log bounds.

Only `/data` and `/run/secrets` are mounted. The image has no Docker socket and
does not use host networking. It contains no `/usr/syno` or `/var/packages`
integration, `SYNOPKG_*` handling, DSM `authenticate.cgi` calls, Synology
package lifecycle logic, or Container Manager control calls. Synology only
wraps the same image with package lifecycle and loopback portal routing.
