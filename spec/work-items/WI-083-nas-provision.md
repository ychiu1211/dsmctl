---
id: WI-083
title: Provision a fresh Synology NAS with a generated, human-recoverable admin
status: in_progress
priority: P2
owner: ""
depends_on: []
parallel_group: G
touches:
  - internal/config/config.go
  - internal/credentials/generate.go
  - internal/credentials/reference.go
  - internal/credentials/secure_store.go
  - internal/clipboard/clipboard.go
  - internal/cli/tty.go
  - internal/cli/provision.go
  - internal/cli/reveal.go
  - internal/cli/auth.go
  - internal/cli/root.go
  - internal/provision/provision.go
  - .claude/skills/nas-provision/SKILL.md
---

# WI-083 — Provision a fresh Synology NAS with a generated, human-recoverable admin

## Outcome

`dsmctl provision <name> --admin-user <u> --url https://<ip>:5001` takes a Synology NAS in its
DSM first-run setup window to a working, logged-in-able DSM: it creates the first administrator
(username operator-chosen), applies the update policy and privacy defaults, finishes the setup
wizard, and hardens the box. The administrator password is generated locally, stored in the OS
credential store, and never printed. It is retrievable only by a human at a terminal via
`dsmctl auth reveal-password`; no model and no MCP tool can obtain it. Day-to-day the operator
logs in to DSM in a browser, or `dsmctl` uses the stored password for automation. Verified live
on a DS918+ (DSM 7.3.1-86009).

## Scope

- Sessionless, pre-authentication provisioning against the DSM setup window: log in as the
  built-in `admin` (empty password during setup) to obtain the `_SSID` session, then a
  sequential stop-on-error `SYNO.Entry.Request` compound — `SYNO.Core.User create` +
  `SYNO.Core.Group.Member add administrators`.
- Postcondition login as the new admin; store the generated password in the OS keyring first.
- `CompleteSetup`: DSM update policy (`SYNO.Core.Upgrade.Setting` + `SYNO.Core.Package.Setting`,
  default security hotfixes), analytics opt-out (`SYNO.Core.DataCollect`,
  `SYNO.ActiveInsight.Setting`), and `SYNO.Core.QuickStart.Info hide_welcome` to finish the wizard.
- `Harden` (best-effort): auto-block, server name, scramble + disable the built-in `admin`.
- `--finish-only`: run just the post-account wizard steps against an already-created admin,
  logging in with the stored password.
- Human-only `dsmctl auth reveal-password`: gated by isatty **and** a typed NAS-name
  confirmation read from stdin; clipboard sink with auto-clear, or `--stdout`.
- `credentials.GeneratePassword` (crypto/rand, ≥16, unambiguous alphabet), keyring-only
  `RevealPassword`, and a `mem:` `MemoryReferenceResolver` so a generated secret flows to a
  request builder by reference, never as a plan/argument/log value.
- Runtime password resolution is keyring-first (`SecureStore.Password`, environment fallback),
  so a provisioned password authenticates every `dsmctl` command, not only reveal / `--finish-only`.
- Flexible NAS identity in config: `Profile.Identity{Serial,MAC,MACs,Model}` + ordered
  `Profile.Endpoints[]`, backward compatible (legacy `url` synthesizes an endpoint).

## Non-goals

- **No MCP tool that returns, provisions, or reveals a password** (deferred; the assistant must
  hand off to a human terminal).
- Serial-keyed credential storage (survives profile rename) — deferred; credentials stay
  keyed by profile name, which already survives IP/DDNS change.
- `dsmctl auth open` SSO session-injecting proxy — deferred.
- Runtime endpoint failover / serial verification / findhost self-heal — the config model is in
  place but the resolver is not yet wired.
- DSM (`.pat`) install as a `dsmctl` subcommand — the recovery `webman/install.cgi` flow is
  reverse-verified and documented in the skill but driven with `curl` for now.
- Synology Account, QuickConnect, and recommended-package install — left to the operator.

## Design constraints

- Provision bypasses the authenticated compatibility client entirely (no session, no advertised
  API model); it mirrors `internal/weblogin` with raw HTTP to the setup WebAPI via a
  cookie-jar client, and refuses a non-https target so the password is never cleartext.
- The generated/stored password is plaintext only inside the local process and the OS keyring.
  `RevealPassword` is keyring-only (no env fallback) and is not an `application.Service` method,
  so `mcp.AddTool` cannot expose it. The reveal command's stdin-confirmation gate is what makes
  it human-only: the Claude Code Bash tool is a pty and passes isatty, so isatty alone is
  insufficient (see the repo memory note).
- Only a **successful** first-admin creation consumes the setup window; failures are retryable.
- New `Profile` fields are `omitempty`; `Normalize` migrates a legacy `url`, so pre-existing
  config files round-trip unchanged.

## Acceptance criteria

- [x] `dsmctl provision ds918 --admin-user testuser --url https://…:5001 --insecure-skip-tls-verify`
      on a setup-window NAS creates the admin, stores a generated password, finishes the wizard,
      and prints a reveal hint but never the value. (Live-verified on a DS918+.)
- [x] `dsmctl auth reveal-password --nas <name>` copies the password to the clipboard for a human
      and **refuses** a bare pty, a pipe, and a redirect (three vectors, exit 1, no value emitted).
- [x] `dsmctl provision <name> --finish-only` completes the wizard on an existing admin using the
      stored password.
- [x] `dsmctl system info --nas <name>` authenticates with the provisioned keyring password
      (keyring-first runtime resolution; live-verified on the DS918+).
- [ ] No registered MCP tool name contains `reveal` or `password` (guard test — pending).
- [ ] Old config files round-trip; the endpoint resolver honors identity/failover (pending Phase 3).

## Verification

- Unit: `go test ./internal/config/... ./internal/credentials/... ./internal/provision/...`
  (config identity round-trip, `GeneratePassword` policy/length, keyring-only `RevealPassword`,
  the `SYNO.Entry.Request` compound wire format via `httptest`, has_fail handling). Full
  `go test ./...` is green (Go at `C:\Program Files\Go\bin`).
- The reveal-password gate is verified by injecting a non-terminal stdin (refusal) and a pipe.
- Live-mutation policy: provisioning runs only against a genuinely fresh / setup-window NAS,
  never in default `go test ./...`. The `webman` install and `SYNO.Core.QuickStart` /
  `SYNO.Entry.Request` request shapes are pinned from a real DSM 7.3.1 pass (see the skill).

## Coordination

- Composes with WI-081 (TLS trust) on `internal/cli/auth.go`; relates to WI-008/WI-009
  (vault-managed secrets, retrievable only via a human-gated reveal, never MCP).
- `internal/config/config.go` schema additions are `omitempty` and coordinate with any
  in-flight profile-shape work.
- The companion operational guide is `.claude/skills/nas-provision/SKILL.md`.

## Handoff

Shipped and merged to main. Deferred, in priority order: (1) Phase 3 endpoint resolver + serial
verify + findhost self-heal; (2) `dsmctl auth open` SSO handoff; (3) MCP no-password guard test;
(4) a `dsmctl` subcommand for the recovery `.pat` install; (5) serial-keyed credential migration.
