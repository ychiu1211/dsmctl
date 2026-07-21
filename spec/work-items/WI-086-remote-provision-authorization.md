---
id: WI-086
title: Remote-provision authorization (nas.provision scope)
status: in_progress
owner: "claude"
depends_on: [WI-016, WI-045]
parallel_group: F
touches:
  - spec/gateway-deployment.md
  - spec/architecture-contracts.md
  - internal/remotepolicy
  - internal/mcpserver/remote_policy.go
  - internal/mcpserver/read_only.go
  - internal/gateway/state (approvals)
  - internal/gateway/admin (approval UI)
---

# WI-086 — Remote-provision authorization amendment (un-enrolled target)

## Outcome

The remote MCP authorization model is extended so a scoped client can request
provisioning of a NAS that is **not yet an enrolled profile**, under a new
`nas.provision` scope and an approval record that can bind to a not-yet-enrolled
target. This unblocks the remote-MCP half of WI-083 (and, if chosen, WI-085)
without weakening the single-owner, human-gated, no-secrets-over-MCP contracts.
It exists because the user chose (2026-07-21) to make provisioning
remote-MCP-callable, which the shipped model cannot express.

## Shipped model (2026-07-21): provision an existing credential-less profile

The implemented v1 sidesteps the un-enrolled-target problem entirely: remote
provisioning targets a profile the local administrator has **already added**
(URL + pinned TLS) but which holds **no credentials yet**. That makes the target
a normal enrolled profile with a revision, inside the token's allowlist, so the
existing allowlist/target machinery applies unchanged. The only genuinely new
authorization primitive is the distinct `nas.provision` scope. Implemented:

- `remotepolicy.ScopeProvision = "nas.provision"`; `ToolScope` maps the
  `provision_` prefix to it (not a sub-privilege of `nas.apply`); new tokens
  never receive it by default.
- `provision_nas` MCP tool over the shared `Service.ProvisionNAS` operation
  (same operation the CLI/Admin-UI use). It refuses a profile that already holds
  a stored credential, so a grant cannot re-provision or hijack a set-up NAS; the
  generated password is stored in the vault and **never** returned (result has no
  password field), logged, or accepted as input.
- `NewReadOnly` strips `provision_nas`; the tools/list scope filter hides it from
  tokens without `nas.provision`; the middleware requires the explicit `nas`
  argument and the allowlist, like every other targeted tool.
- The managed gateway installs a vault provision sink
  (`admin.NewVaultProvisionSink`) on its Service so `provision_nas` persists into
  the encrypted vault; the developer read-only gateway never exposes the tool.

Unit-verified (scope classification, read-only stripping, omitted-nas rejection,
`ProvisionNAS` happy path + re-provision guard + missing sink/profile). Live
verification against a fresh NAS + a remote MCP client is still pending.

### Un-enrolled target shipped too (2026-07-21): `provision_discovered_nas`

The fully-remote path — a client discovers a fresh device and provisions it with
no pre-existing profile — is now implemented as a second tool
`provision_discovered_nas`, also under `nas.provision`, with these safety bounds:

- **LAN/VPN only**: the application layer refuses any target whose host is not a
  private/loopback/link-local address (a hostname is resolved and every address
  must be LAN-scoped), so a generated password can never be sent to an arbitrary
  public host.
- **Trust-on-first-use**: a factory-fresh device is self-signed and there is no
  human to confirm a fingerprint, so the operation trusts and pins the exact
  certificate it observes on first contact (`tlstrust.Observe`), creates a pinned
  profile through the sink's `CreateProvisionProfile`, then provisions.
- **Scope-gated, allowlist-exempt**: a discovered device is outside every
  allowlist by construction, so the tool is authorized by the `nas.provision`
  scope alone (the middleware special-cases it: no `AuthorizeRemoteTarget`, no
  profile/revision) and audited by url. It is refused if a profile of that name
  already exists (use the enrolled path) and the new profile is **never** added
  to any token's allowlist (WI-038 principle).
- Stripped from the read-only developer gateway; the generated password is stored
  in the vault and never returned, exactly like the enrolled path.

Unit-verified (LAN/plain-http rejection, TOFU profile creation + provision
against a loopback TLS server, existing-name and missing-creator refusal,
scope-required/allowlist-exempt middleware admission). Live verification against a
real factory-fresh NAS + a remote MCP client is still pending, and the LAN-TOFU
posture (a MITM on the LAN during the fresh-setup window) is an accepted,
documented bootstrap risk for a single-owner LAN/VPN gateway.

The conflicts below documented why a per-address provision allowlist was
considered; the shipped model instead bounds the un-enrolled path by LAN scope +
the rarely-granted `nas.provision` scope + fresh-setup-only reachability.

## The three conflicts (deferred un-enrolled-target model)

The shipped remote model (gateway-deployment.md; WI-016) blocks remote
provisioning three ways, all of which this item must amend deliberately:

1. **Scope classification** — `internal/mcpserver/remote_policy.go` `ToolScope()`
   classifies tools purely by name prefix (`get_`/`plan_`/`apply_`/`list_nas`/
   `discover_lan_devices`). A `provision_*` tool matches none and is
   default-denied and unlisted. Add a `nas.provision` scope and its prefix
   mapping; it is **not** a sub-privilege of `nas.apply` (it mints a new
   credential and targets a device reached via `lan.discover`, not an enrolled,
   allowlisted profile). New tokens never receive it by default.
2. **Read-only stripping** — `internal/mcpserver/read_only.go` `NewReadOnly()`
   `RemoveTools()` strips every mutating/discovery tool; decide whether the
   developer read-only gateway strips provisioning too (it should).
3. **Un-enrolled target + approval binding** — the remote middleware requires an
   explicit `nas` resolved by `AuthorizeRemoteTarget` against existing profiles
   and the token allowlist, and the WI-016 high-risk approval binds to
   `{plan hash, NAS profile + revision, admin id, token id}`. A freshly
   discovered device is none of those. Define how a provision request names a
   not-yet-enrolled target (e.g. by discovered serial/MAC/address), how the
   allowlist applies (a provision allowlist is not a profile allowlist), and how
   the approval record binds to a target that has no profile/revision yet —
   consuming the approval atomically with profile creation.

## Scope

- Amend `spec/gateway-deployment.md` (Remote MCP authorization and approval) and
  `spec/architecture-contracts.md` (mutation safety / secrets) to define the
  `nas.provision` scope, the un-enrolled-target naming and allowlist model, and
  the provision approval record.
- Implement the scope, its classification, read-only stripping, and the approval
  binding for a not-yet-enrolled target.
- Keep the generated password out of MCP entirely (results carry only the
  created profile name + credential-status booleans; retrieval stays the WI-084
  reveal).

## Non-goals

- The provisioning behavior itself (WI-083) or OS install (WI-085); this item is
  the authorization/contract layer they depend on for the remote surface.
- Relaxing any other gateway non-goal (no fan-out mutation, no multi-tenant
  owners, no delegated identity).

## Design constraints

- Gateway administration proper (creating the local admin, token lifecycle)
  remains non-MCP. `nas.provision` authorizes creating the **NAS's** first admin
  via a scoped token + out-of-band approval, which is distinct from Gateway
  administration and must be documented as such.
- The plaintext-over-setup-channel property (WI-083 protocol) is a
  secret-creation-over-remote concern; the amendment must state the transport
  posture (e.g. provisioning only over trusted LAN/VPN reachability).

## Acceptance criteria

- [ ] `spec/gateway-deployment.md` and `architecture-contracts.md` define the
  `nas.provision` scope, un-enrolled-target model, and provision approval record.
- [ ] A token without `nas.provision` cannot invoke provisioning; a token with
  it can only provision within its provision allowlist and only with a valid,
  single-use approval.
- [ ] The approval binds to and is consumed atomically with the new profile's
  creation; a failed provision does not leave a reusable approval.
- [ ] The read-only developer gateway strips provisioning tools.
- [ ] No password crosses the MCP boundary in any provision path.

## Verification

- remotepolicy + mcpserver unit tests for scope classification, default-deny,
  allowlist filtering, and read-only stripping.
- gateway/state tests for the un-enrolled-target approval lifecycle.

## Coordination

Blocks the remote-MCP surface of WI-083 (and WI-085 if remote install is
chosen). Touches the same policy/approval files as WI-016/WI-038; coordinate
before editing `internal/remotepolicy` and `internal/mcpserver`.
