# Portable gateway and Synology x86_64 deployment

This specification defines how the existing dsmctl application becomes a
long-running, remotely accessible MCP gateway without coupling the core image
to Synology DSM. Synology is the first packaged deployment adapter; the same
container image must also run under ordinary Docker Engine or Podman on an
amd64 Linux host.

The user-visible product name is `dsmctl MCP Server` (WI-035). This
specification keeps the `gateway` term for the transport, policy, and
deployment layer it defines; the two names refer to the same artifact.

## Decision summary

- The product is a single-owner gateway that manages multiple reachable
  Synology NAS profiles through the existing application and runtime layers.
- The only supported container platform is `linux/amd64`.
- The Synology package supports only DSM `7.2.1-69057` or newer on `x86_64`
  models with Container Manager `1432` or newer.
- The Synology SPK preloads a pinned amd64 image and uses the official
  `docker-project` resource worker. Installation does not require a registry.
- Generic Linux uses the same image with a separate Compose file.
- MCP uses Streamable HTTP. Version 1 is stateless at the MCP transport layer
  and prefers JSON responses; per-NAS DSM clients and sessions remain stateful.
- The gateway is LAN/VPN oriented. Automatic Internet exposure, QuickConnect,
  port forwarding, and public multi-tenant hosting are out of scope.
- New remote credentials receive read-only access by default. Planning,
  applying, and LAN discovery are separate scopes; gateway administration is
  never an MCP scope.
- High-risk applies require a short-lived, single-use approval recorded
  outside the MCP conversation. Echoing a plan hash is not operator approval.

## Goals

1. Install one gateway and manage multiple independently authenticated NAS
   profiles, including the NAS hosting the package when explicitly configured.
2. Keep the core image portable across Synology and ordinary amd64 Linux.
3. Preserve the existing CLI/MCP/application/DSM compatibility boundaries.
4. Keep passwords, OTPs, trusted-device IDs, DSM sessions and their resume
   keys, tokens, and master keys out of MCP arguments, plans, display models,
   and logs.
5. Make install, start, stop, reboot, upgrade, backup, and uninstall behavior
   deterministic and testable.
6. Fail closed when authentication, authorization, TLS trust, approval, or
   profile state cannot be established.

## Non-goals

- ARM, arm64, armv7, i686, or multi-architecture images and packages.
- DSM releases older than `7.2.1-69057`, models without Container Manager, or
  a native-binary SPK fallback.
- Kubernetes, high availability, active/passive gateway failover, or shared
  state between gateway instances.
- Public SaaS, multiple tenant owners, delegated DSM-user tenancy, or an
  organization-wide identity model.
- Automatic NAS discovery, QuickConnect integration, VPN configuration,
  router configuration, or certificate issuance.
- Fan-out mutation. Read-only fleet inventory may be added with bounded
  concurrency and partial results; every mutation remains scoped to one NAS
  and one plan.
- A raw DSM WebAPI proxy or a generic mutation tool.
- OIDC in version 1. The authentication boundary must be extensible so OIDC
  can be added without changing application use cases or MCP tool schemas.

## Architecture

```text
Generic Docker Compose --------+
                                +--> dsmctl gateway container
Synology x86_64 SPK ------------+          |
  - Container Manager project              +--> HTTP auth/policy
  - DSM reverse proxy                       +--> MCP adapter
                                           +--> admin application
                                           +--> existing application layer
                                                       |
                                                       +--> runtime/session manager
                                                                    |
                                                                    +--> NAS A
                                                                    +--> NAS B
                                                                    +--> NAS C
```

The gateway container owns HTTP transport, local administrator identity,
authorization, profile administration, persistent state, and audit records. It
reuses the existing application layer for DSM behavior. Deployment adapters
own process, port, TLS termination, and persistent mounts; they never identify
the gateway administrator or construct DSM WebAPI requests.

## Portability boundary

The container image must not execute or require:

- `/usr/syno/*`, `authenticate.cgi`, `synopkg`, or any DSM command;
- `SYNOPKG_*` environment variables or `/var/packages/*` paths;
- the Docker socket, Container Manager APIs, or host-network access;
- an OS desktop keyring or D-Bus Secret Service.

The image exposes only stable container paths and interfaces:

```text
/data                    persistent database and audit data
/run/secrets/master.key  read-only 32-byte vault key
/tmp                     bounded tmpfs
/mcp                     Streamable HTTP MCP endpoint
/admin                   administration API/UI endpoint
/healthz                 process liveness
/readyz                  state-store and policy readiness
```

Platform-specific configuration is supplied by flags, environment variables,
mounted secrets, Compose, or the SPK wrapper. The generic image starts with no
implicit knowledge of a local NAS. A NAS hosting the container is configured
through its reachable LAN address or DNS name; `localhost` always means the
container itself.

## Runtime and MCP transport

- Add a dedicated long-running gateway entry point; keep the current stdio MCP
  entry point supported for local clients.
- Serve one Streamable HTTP endpoint at `/mcp`.
- Version 1 uses stateless MCP sessions and JSON responses because current
  tools do not require server-to-client requests or durable SSE streams.
- DSM clients remain lazily created and cached per profile. Calls to one NAS
  retain the existing session serialization; calls to different profiles may
  run concurrently.
- Default fleet limits are 32 configured NAS profiles and 8 concurrent
  cross-profile operations. Per-profile timeouts remain configurable, with a
  server-enforced maximum of 120 seconds.
- The backend listens on the container interface. Deployment adapters publish
  it only on host loopback; TLS termination and externally reachable routing
  happen in a trusted reverse proxy.
- Enforce body limits, header timeouts, idle timeouts, graceful shutdown,
  Origin/Host validation, rate limits, and redacted structured logging.
- Liveness must not contact DSM. Readiness verifies local state, key access,
  and policy initialization but must not require every NAS to be reachable.

## Persistent profiles and runtime invalidation

Profiles are no longer a process-start snapshot. A transactional repository
stores versioned profile state and supports list, add, update, remove, select
default, and connection test operations.

A profile contains at least:

```json
{
  "name": "office",
  "url": "https://nas.example.com:5001",
  "username": "automation",
  "timeout_seconds": 30,
  "tls_mode": "system_ca",
  "certificate_fingerprint": ""
}
```

Stored TLS modes are `system_ca` and `pinned_fingerprint`; they are connection
state, not an up-front administrator choice. Enrollment always attempts system
CA, hostname, and validity verification first. CA, hostname, and validity
failures produce a structured challenge containing their warnings and the
server-observed fingerprint; an administrator may explicitly pin that exact
leaf, including for a LAN IP absent from the certificate SAN. Confirmation is
bound to the current profile revision and a fresh server-side observation
before `pinned_fingerprint` is persisted. Pin mismatch and certificate rotation
fail closed. Missing/unparseable certificates, TLS protocol or cryptographic
handshake failures, and network failures cannot be pinned. An insecure mode
may exist only behind an explicit development build/runtime flag and is never
enabled by the Synology package.

Changing URL, username, TLS policy, or credential binding atomically advances
the profile revision and evicts the old runtime client. Removing a profile
closes its session and removes credentials unless the administrator explicitly
chooses to retain them. A plan remains bound to its NAS profile and observed
state; apply rejects a removed or materially changed profile.

## State and secret storage

- Use a versioned transactional embedded database under `/data`. The selected
  implementation must build with `CGO_ENABLED=0`; SQLite through a pure-Go
  driver is the preferred default unless an implementation spike proves a
  smaller transactional store better satisfies migrations and audit queries.
- Store profiles, secret metadata, token hashes, approvals, audit events, and
  schema version in the database.
- Store password, trusted-device, and DSM web-login session payloads (SID,
  SynoToken, and the durable Noise resume keys) as AES-256-GCM ciphertext with
  a unique random nonce and authenticated metadata binding secret type and
  profile identity.
- Prefer a stored web-login session for a profile and renew it headlessly
  through DSM session resume; passwords remain for automation accounts. The
  desktop OS keyring used by the CLI is never read by the gateway.
- Read the vault key from `/run/secrets/master.key`; never store it in the
  database or ordinary `/data` backup. Generic Linux supplies the file. The
  Synology wrapper generates it in the package-private home directory and
  mounts it read-only.
- Replace daemon reliance on `env:NAME` with opaque `vault:<id>` references for
  apply-time secrets. Retain environment references for CLI automation.
- Database backup excludes the master key and plaintext secrets. A portable
  secret export, if later added, requires a user-provided wrapping passphrase
  and is outside version 1.
- Schema migrations are forward-only during upgrade, transactional, and
  backed up before modification. A failed migration prevents readiness.

## Administration and enrollment

Administration is separate from MCP authorization.

- Every deployment uses the same local administrator. While no administrator
  exists, each process start opens setup for one hour; expiry requires restart
  of the still-uninitialized process. The first transactional setup creates a
  normalized username, an Argon2id password verifier, and an expiring browser
  session, then permanently closes setup for that initialized state.
- Administration uses random browser sessions stored only as digests and sent
  through HttpOnly/SameSite cookies. Mutations require same-origin JSON plus a
  non-simple request header. Login/setup attempts are bounded in memory.
- DSM browser sessions, DSM groups, forwarded identity headers, and the NAS
  hosting the container do not authorize Gateway administration.
- The admin API supports profile CRUD, connection testing, web-login session
  enrollment (the administrator signs in at the NAS's own page, the browser
  relays the one-time code, and the gateway performs the code exchange and
  stores the session), password/OTP enrollment, observed TLS fingerprint confirmation,
  credential removal/rotation, token lifecycle, approvals, audit queries, and
  safe backup status.
- Password and OTP submissions are accepted only by authenticated admin
  endpoints, kept only for the enrollment transaction, and never accepted by
  MCP tools.
- A stored NAS password may be revealed to the local administrator (WI-084):
  the reveal endpoint re-verifies the administrator password, is rate limited,
  emits a `credential.reveal` audit event naming the NAS, and returns the
  value once without persisting it anywhere else. Reveal is part of local
  administration and is never reachable through MCP authorization.
- A DSM authentication adapter failure must fail closed; the package must not
  expose an unauthenticated fallback admin UI.

## Remote MCP authorization and approval

MCP access tokens are random high-entropy bearer credentials. Only their
digests are stored. A token has an optional expiry, revocation state,
last-used time, a NAS allowlist, and independent scopes:

```text
nas.read
nas.plan
nas.apply
lan.discover
```

`lan.discover` (renamed from the pre-release `nas.admin`; WI-038) admits only
LAN device discovery. Its distinct prefix is deliberate: discovery reveals
devices outside the caller's NAS allowlist, so it sits outside the
allowlist-filtered `nas.*` family and must never become a container for other
privileges. Gateway administration is performed only through the local
administrator session and is never grantable to an MCP token.

- New tokens default to `nas.read` only.
- Allowlist entries are validated against existing profiles at issue time.
  Deleting a NAS profile removes its name from every token allowlist in the
  same transaction (WI-038); re-creating a profile under a deleted name never
  silently restores prior remote access.
- Remote tool calls that operate on a NAS name their target explicitly. An
  omitted target is an error, never a default-profile fallback, so changing a
  default cannot silently retarget a remote caller (WI-038). Default-profile
  resolution remains a local CLI convenience.
- `list_nas` and every result are filtered by the caller's NAS allowlist.
- Tool registration annotations remain hints; policy enforcement happens
  before application execution and apply policy is rechecked at the gateway
  application boundary.
- Rate-limit identity is the token ID, not a client-supplied name or IP alone.
- Token values, authorization headers, and cookies are always redacted.

High-risk remote applies require an approval record containing the plan hash,
NAS profile and revision, approving local administrator identity, requesting token ID, expiry,
and a single-use marker. Default approval lifetime is 10 minutes. Consumption
and apply admission are atomic. Failed stale-state or postcondition checks do
not make an old approval reusable. Local CLI/stdio behavior remains governed
by the existing plan/apply contract and is not silently changed by HTTP policy.

To remove manual transcription, the administration page also lists redacted
pending high-risk plan requests containing the plan summary, risk, and binding
fields (WI-038). These records are advisory: they never approve anything, MCP
clients cannot see or create approvals through them, and the approval record
above remains the only admission authority.

### Remote provisioning (WI-086)

Remote provisioning of a fresh NAS is admitted under a distinct
`nas.provision` scope. The shipped v1 targets a profile the local administrator
**already added** (URL + pinned TLS) but that holds no credentials yet, so the
existing allowlist and per-profile machinery apply unchanged; the tool refuses a
profile that already has a stored credential, and the generated password is
stored in the vault and never returned over MCP. `nas.provision` is never a
sub-privilege of `nas.apply` and is never granted by default; the developer
read-only gateway strips the tool.

A **truly un-enrolled target** (a discovered device with no profile yet) is
provisioned by the `provision_discovered_nas` tool, also under `nas.provision`.
Because a discovered device is outside every allowlist, it is authorized by the
scope alone and bounded instead by: the target host must be a
private/loopback/link-local (LAN/VPN) address; the certificate is trusted on
first contact and pinned; a name collision with an existing profile is refused;
and the newly created profile is never added to any token's allowlist. This is a
LAN/VPN bootstrap that sends a generated password to the device, so the scope is
granted only to a trusted provisioning client. The conflicts below document the
finer-grained per-address-allowlist model that was considered instead:

- A distinct `nas.provision` scope, never granted by default and never a
  sub-privilege of `nas.apply` (it mints a new credential and targets a device
  reached through `lan.discover`, not an allowlisted profile). The read-only
  developer gateway strips provisioning tools.
- The target is named by its discovered identity (serial/MAC/reachable
  address), filtered by a provision allowlist that is separate from the profile
  allowlist. Provisioning is confined to LAN/VPN-reachable targets.
- The provision approval record binds to that discovered identity and is
  consumed atomically with creation of the new profile; a failed provision
  leaves no reusable approval. Creating the NAS's first administrator through a
  scoped token plus out-of-band approval is distinct from Gateway
  administration, which stays non-MCP.
- The generated administrator password never crosses the MCP boundary: results
  carry only the new profile name and credential-status booleans, and retrieval
  stays the human-gated reveal. The setup channel's plaintext-password property
  further restricts provisioning to trusted reachability.

## Audit and observability

Audit records contain timestamp, request/correlation ID, remote token ID or
local administrator identity, NAS profile, tool/use case, action, risk, plan hash when
applicable, stable resource identifier when available, outcome, and normalized
error class. They never contain request secrets, raw authorization material,
SynoTokens, SIDs, OTPs, or encrypted payloads.

Audit retention is fixed at 10,000 events and 30 days.
Operational logs are separate from immutable audit events. Health and admin
status report the gateway version, schema version, profile health summaries,
and selected deployment mode without disclosing secret presence beyond the
existing boolean credential-status model.

## Container hardening

The shipped Compose definitions must enforce or document equivalent controls:

- non-root UID/GID;
- read-only root filesystem;
- `cap_drop: [ALL]` and `no-new-privileges`;
- no Docker socket and no host network;
- only `/data` writable and `/tmp` as bounded tmpfs;
- CPU, memory, PID, and log-rotation limits;
- loopback-only host port publication;
- pinned image identity and health check.

The image contains one statically linked amd64 Go server in a minimal runtime
base. It contains no shell or package manager unless a documented operational
requirement justifies one.

## Deployment adapters

### Generic amd64 Linux

- Provide a Compose file and operator guide for Docker Engine and a compatible
  Podman invocation.
- The operator creates persistent data and secret files with documented UID
  ownership, supplies TLS/reverse proxy separately, and explicitly configures
  each NAS.
- No platform SSO is assumed; the one-hour first-run page creates the local
  administrator credential.

### Synology x86_64

- `arch="x86_64"` and `os_min_ver="7.2.1-69057"`.
- Declare Container Manager `>=1432` through `PKG_DEPS`.
- Bundle the exact `linux/amd64` image as `image.tar.gz`; installation and
  ordinary start do not pull from a registry.
- Use the official `docker-project` resource worker to preload, create,
  recreate on upgrade, start, stop, and remove the project.
- Persist the database in package data and the master key in package-private
  home. Upgrade preserves both. Uninstall presents an explicit retain/delete
  choice and never silently deletes retained secrets.
- Resolve the DSM package user's dynamic UID/GID into Compose/runtime ownership
  so the long-running container remains non-root while package data and secret
  files remain unreadable to other users. Never solve an ownership mismatch by
  making the master key world-readable.
- Publish the backend to host loopback only and register the DSM portal/reverse
  proxy without automatically opening a firewall or router port.
- Route the same local-administrator UI through the DSM portal and keep its
  browser session separate from MCP bearer credentials and every NAS session.

## Release and verification matrix

The release pipeline produces one amd64 container image, one generic Compose
bundle, and one x86_64 SPK. It records checksums, dependency versions, an SBOM,
and the embedded image digest.

Minimum verification:

- Generic Linux: current supported Docker Engine on an amd64 Linux host.
- Synology: at least one Intel and one AMD x86_64 model.
- DSM: `7.2.1-69057`, `7.2.2-72806`, and each newer DSM release claimed by the
  package metadata/release notes.
- Lifecycle: offline install, first start, stop/start, NAS reboot, package
  upgrade, failed migration, retained-data uninstall, and full-delete uninstall.
- Security: no external backend port, no Docker socket/capabilities, denied
  Origin/Host, expired/revoked tokens, NAS allowlist filtering, read-only token
  denial, single-use approval, redacted logs, and missing-key fail-closed.
- Multi-NAS: local-host NAS by LAN name plus two remote profiles, independent
  sessions, one unreachable NAS without global failure, and profile-change
  client eviction.

No storage, volume, SAN, network, firewall, encrypted-share, or other
disruptive DSM live mutation is authorized by this specification. Remote apply
tests use application fakes unless a separate exact live test is explicitly
approved under the repository safety policy.

## Delivery plan

1. WI-014 establishes the portable HTTP gateway and hardened generic container.
2. WI-015 adds transactional profiles, vault storage, administration, and
   runtime invalidation.
3. WI-016 adds scoped remote authorization, out-of-band approval, and audit.
4. WI-017 packages the completed gateway for generic Linux and Synology
   x86_64, including offline image preload and portal wiring.
5. WI-032 replaces the pre-release bootstrap/platform-auth split with the same
   one-hour local-administrator setup and browser session model everywhere.
6. WI-033, WI-035, and WI-037 delivered the redesigned multilingual
   administration shell and design tokens; WI-038 streamlines the approval,
   token-lifecycle, enrollment, and audit-review flows on top of the shipped
   WI-016 policy model before WI-017 certifies the final UI.

The production Synology package depends on all of the items above. Intermediate
developer builds may expose read-only tools on generic Linux, but must be
clearly labeled unsupported and must not enable remote apply.

## External references

- Synology Docker Project Worker:
  <https://help.synology.com/developer-guide/resource_acquisition/docker-project.html>
- Synology package dependency configuration:
  <https://help.synology.com/developer-guide/synology_package/pkgdeps.html>
- MCP Streamable HTTP transport:
  <https://modelcontextprotocol.io/specification/2025-11-25/basic/transports>
- MCP HTTP authorization:
  <https://modelcontextprotocol.io/specification/2025-03-26/basic/authorization>
