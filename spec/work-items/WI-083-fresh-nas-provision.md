---
id: WI-083
title: Fresh-NAS first-admin provision (local admin + remote MCP)
status: in_progress
owner: "claude"
depends_on: [WI-084, WI-023, WI-086]
parallel_group: H
touches:
  - internal/provision (new; portable core)
  - internal/application
  - internal/cli
  - internal/gateway/admin (wizard + handler)
  - internal/gateway/state (vault glue)
  - internal/mcpserver/server.go
  - internal/remotepolicy
---

# WI-083 — Fresh-NAS first-admin provision (local admin + remote MCP)

## Outcome

From the CLI, the Gateway Admin UI, and (per the WI-086 amendment) a scoped
remote MCP tool, dsmctl can take a NAS that has finished DSM OS installation
but has no administrator, create the first administrator, and hand the
ready-to-manage NAS to the normal profile/enrollment flow. The administrator
password is **generated in the application layer** and stored in the active
credential store (OS keyring for the CLI; the gateway AES-256-GCM vault for the
Admin UI / remote flows) bound to a newly created NAS profile. The password is
never displayed, returned over MCP, logged, or placed in a plan or audit
record; retrieval afterward is exclusively the WI-084 human-gated reveal.

## Product decisions (2026-07-21)

The user chose the maximal scope on both open forks:

- **Scope**: provisioning covers first-admin creation on an already-installed
  fresh-setup NAS (this item) **and** DSM OS installation from a `.pat` image on
  never-installed hardware (carved out to **WI-085**, which feeds this item).
- **Surface**: provisioning is reachable from local administration (CLI +
  Admin UI wizard) **and** as a scoped remote MCP capability. The remote path
  is contract-breaking under the shipped single-owner model and is gated behind
  the **WI-086** authorization amendment (new `nas.provision` scope +
  un-enrolled-target approval binding); it must not ship before WI-086.

## Reuse, don't duplicate: one operation, thin adapters

This item **reuses** the existing engine and follows the standard boundary
(`architecture-contracts.md`: CLI and MCP are thin adapters over the shared
application layer; the facade owns DSM protocol). It must **not** create a
second parallel implementation.

A working first-admin engine already exists on branch
`claude/nas-setup-workflow-0952c6` (live-verified on a DS918+ / DSM 7.3.1) and
is already split correctly for reuse:

- `internal/provision/provision.go` is a **portable protocol core** with no CLI
  coupling: `EstablishSetupSession`, `CreateFirstAdmin` (the
  `SYNO.Entry.Request` sequential compound), `Harden`, `Login`, `CompleteSetup`,
  over `Target{BaseURL, HTTPClient}` + `AdminRequest{Username, Password}`. Keep
  this package essentially as-is — it is the operation core both adapters share.
- `internal/cli/provision.go` is the CLI adapter; `internal/credentials`
  `GeneratePassword` mints the password.

The one thing the branch never did is lift the flow into the shared application
layer, so it is CLI-only and keyring-only. The reuse plan:

1. **Add an application-layer operation** — e.g. `Service.ProvisionFirstAdmin`
   — that orchestrates the existing `internal/provision` core:
   generate password → `CreateFirstAdmin`/`CompleteSetup` → store via the
   injected credential store → `CreateProfile` → verification login. This is the
   single place the behavior lives.
2. **Refactor the CLI adapter to call the Service**, not `internal/provision`
   directly, so CLI and MCP share one code path (no duplication).
3. **Deployment-specific storage is the only fork**, handled behind the existing
   `CredentialStore` abstraction (`WithCredentialStore`): the CLI stores in the
   OS keyring (`SavePassword`); the gateway stores in the vault via
   `EnrollPasswordForAccount(name, createdProfile.Revision, adminUser,
   generatedPassword, device)` — NOT `SavePassword` (that leaves
   `record.Username` empty, so the reveal's account field is blank; the revision
   guard also closes the create→enroll race), wrapped in
   `manager.MutateProfile` like the `passwordEnrollment` handler.
4. **Add thin adapters over the Service**: a `dsmctl provision` CLI command
   (already exists — just re-point it), an Admin UI wizard step, and a scoped
   MCP tool (gated by WI-086). All three call `Service.ProvisionFirstAdmin`.
5. Rebase onto this branch's WI-084 store/reveal foundation (the two branches
   diverged from an older `main`; keep the gateway-vault + Admin UI reveal base).

## Protocol notes (live-verified once; re-verify before shipping)

- The setup page auto-logs-in the built-in `admin` account with an empty
  password; the session is carried by `_SSID` with **no SynoToken**.
- First-admin creation is a `SYNO.Entry.Request` **sequential, stop-on-error
  compound**: `SYNO.Core.User.create` then `SYNO.Core.Group.Member` add to
  `administrators` (plus optional built-in-admin hardening/scramble-expire).
- The setup channel sends the password as a plaintext form field even over
  https; provision must refuse plain-http targets unless explicitly acknowledged.
- Error signals: DSM `119` = no setup session (not fresh, or session lapsed);
  `103` = setup already completed. Both fail closed.

## Scope

- Detect a fresh-setup NAS (see the fresh-setup detection open question below;
  optionally sourced from WI-023 discovery / after a WI-085 OS install).
- Create the first administrator with a generated high-entropy password.
- Persist via the WI-084 storage path bound to a new NAS profile, then run the
  standard verification login.
- CLI may also prompt for an operator-chosen password; that path also stores
  rather than displays.
- Guarded plan/apply; provisioning is high risk. Remote invocation requires the
  WI-086 approval flow.

## Non-goals

- DSM OS installation from a `.pat` image (that is **WI-085**; this item begins
  once DSM is installed and in fresh-setup state).
- Returning any password over MCP, in plans, results, logs, or audit.
- Fan-out / multi-NAS provisioning in one call.

## Design constraints

- WI-008 decision: secret material is vault-managed, retrievable only via the
  WI-084 human-gated reveal, never over MCP.
- WI-063: fresh-setup endpoints get their own operation variants with DSM
  version ranges; out-of-range fails closed.
- Live verification requires a factory-fresh (or reset) NAS; the protocol notes
  above must be re-verified on the target DSM train before `status: ready`.

## Progress (2026-07-21)

The **shared operation + CLI adapter** slice is implemented on this branch,
unit-verified (`go build/vet/test ./...` green):

- Imported the portable `internal/provision` core and
  `credentials.GeneratePassword` from `claude/nas-setup-workflow` (no CLI/config
  coupling; builds as-is).
- Added the shared application operation `Service.ProvisionFirstAdmin` (+
  `FinishSetup`) behind a `Provisioner` seam (default forwards to
  `internal/provision`) and a `ProvisionSink` so the generated password reaches
  only the store, never the caller/result — unit-tested with fakes for step
  order, plain-http refusal, create-failure, sink-failure, and best-effort
  warnings.
- Added the thin `dsmctl provision` CLI adapter (pin-on-first-use TLS, cookie-jar
  target, keyring sink writing password + config profile). `--finish-only` reuses
  `FinishSetup`.
- Added the **gateway Admin-UI adapter**: `POST /admin/api/profiles/{name}/provision`
  calls the same `Service.ProvisionFirstAdmin` with a **vault sink**
  (`EnrollPasswordForAccount` inside `MutateProfile`, no trusted device), audited
  as `profile.provision`, returning no password; and an Admin UI "Provision fresh
  NAS" row action + dialog (5 languages). The provisioned password lands in the
  vault and is retrievable through the WI-084 reveal — unit-verified end to end
  (sink → `StoredPassword`).

Still pending: the remote MCP tool (WI-086), online OS install (WI-085), and one
**live fresh-NAS verification**.

## Acceptance criteria

- [x] The provisioning sequence is a single application-layer operation reused by
  thin adapters (CLI shipped; gateway/MCP adapters call the same operation),
  not a duplicate implementation.
- [x] CLI: `dsmctl provision <name> --admin-user … --url https://…` generates the
  password, drives the setup sequence, and stores the password (keyring) +
  profile without ever printing it (unit-verified; user confirmed a live CLI run).
- [x] Gateway Admin UI: a credential-less profile can be provisioned from the web
  page; the generated password is stored in the vault (never returned) and is
  revealable through the WI-084 human-gated reveal (unit-verified sink →
  StoredPassword; audited `profile.provision`).
- [ ] Provision (Admin UI) succeeds against a live fresh NAS end to end; the
  resulting profile authenticates through the normal runtime path.
- [ ] The generated password exists only in the credential store; MCP results,
  CLI output, logs, and audit contain no secret material; it is revealable only
  through the WI-084 reveal.
- [ ] Already-provisioned (103) and lapsed-setup (119) targets fail closed with
  actionable messages.
- [ ] Plain-http provision requires an explicit acknowledgment flag.
- [ ] Remote MCP provisioning is admitted only under the WI-086 scope + approval
  model, or is absent if WI-086 is not yet delivered.

## Verification

- Unit fixtures for the compound request/response shapes; request-capture tests
  for both error signals; gateway vault-glue tests (CreateProfile →
  EnrollPasswordForAccount → reveal).
- One live end-to-end pass on a disposable fresh NAS (explicit authorization
  required for that exact test).

## Coordination

Depends on WI-084 (store/reveal), WI-023 (discovery), WI-086 (remote authz
amendment). Feeds from WI-085 (.pat OS install). Shares
`internal/gateway/admin/ui.go` + `handler.go` with the WI-084 UI work already on
this branch. Reconcile with the `claude/nas-setup-workflow-0952c6` branch before
re-implementing.

## Open questions

- Fresh-setup **detection**: findhost reports both a configured NAS and an
  installed-but-no-admin NAS as `ready` (it cannot tell them apart). Deciding
  whether to probe each discovered device with an `admin`/empty setup login
  breaches the WI-023 "unauthenticated, NAS-independent discovery" carve-out —
  resolve whether detection is a provision-time step or an opt-in discovery
  enrichment adding a `not_configured` state.
- Whether the Admin UI ready-gate treats a `not_configured` device as
  actionable (offer provision) or keeps it disabled like `not_installed`.
