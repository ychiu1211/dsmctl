---
id: WI-079
title: KMIP key management
status: proposed
priority: P3
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/kmip
  - internal/synology/operations/kmip
  - internal/synology/kmip.go
  - internal/runtime/manager.go
  - internal/application/kmip.go
  - internal/cli/kmip.go
  - internal/mcpserver/server.go
  - docs/control-panel.md
---

# WI-079 — KMIP key management

## Outcome

A CLI user or MCP agent can read the Control Panel → Security → KMIP surface —
whether this NAS acts as a KMIP client (escrowing encrypted-share keys to an
external KMIP server) and/or as the local KMIP server (holding keys for other
Synology devices), including connection status and the certificate identity in
use — and, through the hash-bound plan/apply contract, change the KMIP
client/server configuration under strict guardrails. This is a focused Control
Panel module in the sense of [WI-006](WI-006-control-panel-modules.md): one
typed module for the KMIP setting area, never a generic `set key=value` proxy.

The API map, versions, and set fields below are the author's best current
knowledge and **must be treated as unverified**. The standing policy applies:
DSM WebAPI conf/source and mobile-client field names are frequently stale, so
every API family, version, method, and field named here is **to be
live-verified at implementation time** against the lab with a throwaway
`DSMCTL_DUMP` probe before any of it is trusted
(see [[dsm-webapi-live-verify-fields]]).

## Scope

Sliced read-first, then guarded write, so the read slice ships independently.

### Slice A — read-only (to be live-verified)

- **KMIP role/status.** Likely `SYNO.Core.KMIP.*` — probe `SYNO.API.Info` for
  the exact family. Expected sub-families (all to be confirmed):
  - `SYNO.Core.KMIP.Server` — local-server state: enabled, listening
    port, bound certificate, and how many escrowed key objects it holds.
  - `SYNO.Core.KMIP.Client` — external-server config the NAS points at:
    server address, port, the server's expected identity, the client
    certificate/CA in use, and last-connection/health status.
  - A read/info method (`get` / `query` / `list`) per sub-family returning the
    normalized state above. If DSM exposes only a single combined KMIP config
    API rather than split client/server families, model both roles from it.
- **Escrowed key inventory (identifiers only).** If a list method exists
  (e.g. a KMIP object/managed-key list), surface only non-secret identifiers
  and metadata — object id/UID, owner device, share association, create time,
  state — and **never** any key bytes or wrapping material.
- **Certificate binding (read).** The certificate/CA fingerprint or friendly
  name currently bound to the KMIP client and server. Report the binding, not
  the key material.

### Slice B — guarded write (plan/apply, hash-bound)

- **Enable/disable the local KMIP server** — turn the NAS's KMIP server on or
  off and set its listening port and bound certificate.
- **Configure/clear the KMIP client** — point the NAS at (or detach it from) an
  external KMIP server: server address, port, expected server identity, and the
  client certificate/CA to authenticate with.
- **Certificate binding (write)** — bind or rotate the certificate used for the
  KMIP client and/or server TLS identity, referencing an already-installed
  certificate by name/id (the certificate itself is installed out of band).

## Non-goals

- **Encrypted shared-folder key operations.** Creating, mounting, unmounting,
  re-keying, or escrowing an individual encrypted share's key is the
  encrypted-share domain (WI-008), not this module. This WI configures *where*
  and *whether* keys are managed via KMIP; it does not enrol or migrate a
  share's key into KMIP.
- **Installing or issuing certificates.** Importing, generating, or signing the
  client/server certificate and CA belongs to the certificate-management module.
  This WI binds an already-installed certificate by reference and does not create
  key/cert material.
- **Migrating escrowed keys between key managers**, exporting escrowed key
  material, or any destructive purge of the KMIP object store. Deleting escrowed
  keys can permanently orphan encrypted data and is deliberately excluded here.
- **Standing up KMIP against a non-Synology / third-party KMIP appliance beyond
  the fields DSM's own client exposes.** dsmctl configures DSM's KMIP client;
  it is not a general-purpose KMIP protocol implementation.

## Design constraints

- **Independent compatibility boundary, fail-closed.** KMIP is a distinct API
  family with its own `SYNO.API.Info` advertisement. A NAS/DSM build without it
  (feature absent, or the security package/edition that gates it not present)
  must report the module `(not supported)` and never fabricate an empty
  "no KMIP configured" success. Per-operation backend selection with a stable
  operation/capability name (`kmip.read`, and the Slice-B mutation names) and
  fail-closed behaviour when the API is absent, per the compatibility framework.
- **Secrets never enter requests/plans/hashes/logs/MCP args.** The KMIP client
  authenticates with a **certificate private key** (PEM), and connecting may
  require a server password or pre-shared secret. All such material — private
  keys especially, plus any KMIP server credential or wrapping key — uses the
  existing `credential_ref: env:NAME` mechanism, resolved only at apply time and
  provably absent from the request body, the plan, the approval hash, the
  result, and all logs. Escrowed key bytes and object wrapping material are
  never surfaced by any display model; the read slice returns identifiers and
  status only.
- **Every write is HIGH risk — it changes security posture and can make
  encrypted shares unmountable.** Repointing or detaching the KMIP client,
  disabling the local KMIP server, or rotating the bound certificate can sever a
  NAS from the key manager its encrypted shares depend on, so those shares fail
  to mount at boot — an availability failure equivalent to locking the operator
  out of their own data. There is no low- or medium-risk KMIP toggle; classify
  **all** Slice-B mutations high. The read-only gateway must exclude the KMIP
  plan/apply tools.
- **Patch + postcondition, ownership stated.** Follow the module pattern: plan
  records and hashes the complete observed KMIP state, apply rejects a changed
  state, merges the patch into a freshly read config, performs the typed
  operation, and re-reads to verify the requested fields actually took effect
  (DSM silently ignores some fields — the recurring lesson). State explicitly
  whether each mutation is patch-only or full-desired-state; unspecified fields
  must never be silently reset — a partial KMIP write that blanks the server
  address or certificate would break key access.
- **Write shapes require an authorized, reverted live probe.** KMIP client
  connect requires a reachable KMIP server and a valid certificate identity, so
  the exact `set`/`connect`/`test` field symmetry, whether the client "test
  connection" is a separate method, and whether server enable needs a companion
  certificate-bind call cannot be confirmed from source alone. Slice B must not
  ship until confirmed by one explicitly authorized, fully reverted live
  configure/detach cycle against a disposable KMIP endpoint — this module's
  siblings have already caught wrong wire methods from stale conf
  (e.g. WI-041's relay `set_relay_enable` → `set_misc_config` correction).

## Acceptance criteria

- [ ] Slice A: `kmip capabilities|status` (CLI) and the matching `get_kmip_*`
      MCP tool(s) return normalized client/server role, connection status, and
      certificate binding; escrowed-key output (if any) is identifiers/metadata
      only, and a unit test asserts no key bytes, private-key material, or KMIP
      credential ever appear in the decoded state or `--json` output.
- [ ] The exact `SYNO.Core.KMIP.*` family, versions, methods, and read fields
      are confirmed with a throwaway `DSMCTL_DUMP` probe on the lab and recorded
      in the memory map before the decoder is finalized; malformed shapes error
      rather than returning an empty success.
- [ ] Compatibility: `kmip.read` selects its own backend and reports a stable
      operation/API/version; on a NAS/DSM without KMIP the module reports
      `(not supported)` and does not disable or error any other Control Panel
      module.
- [ ] Slice B: KMIP client configure/detach and local-server enable/disable via
      guarded hash-bound plan/apply, each with a request-capture test and a
      postcondition re-read; every mutation classified HIGH risk; the read-only
      gateway excludes the plan/apply tools.
- [ ] Certificate private keys and any KMIP server credential are supplied only
      via `credential_ref: env:NAME`; a secret-hygiene test proves the resolved
      secret is absent from the request, plan, approval hash, result, and logs.
- [ ] Slice B live verification (explicitly authorized, fully reverted): a
      client connect→detach (or server enable→disable) round-trip through
      plan/apply against a disposable KMIP endpoint, with postcondition proof
      and the corrected wire method recorded.
- [ ] CLI (`kmip …`) and MCP tools use the same application query/result and
      plan/apply methods; no generic KMIP `set key=value` surface exists.

## Verification

- Decoder + request-capture unit tests; `go test ./... -count=1`,
  `go vet ./...`. Unit tests use fixtures and the in-memory keyring fake and
  never touch real key material.
- Live read is allowed against the explicitly configured lab NAS once the KMIP
  family is confirmed present. Live write requires explicit per-session
  authorization and a disposable KMIP endpoint, and must be fully reverted —
  a misapplied KMIP change can render encrypted shares unmountable.
- Source of truth for fields (to cross-check, not to trust): the DSM WebAPI
  conf/handlers for `SYNO.Core.KMIP.*` and the Control Panel Security KMIP tab
  JS on codesearch; confirm every field against the lab before shipping.

## Coordination

- Shares the Control Panel facade and application/MCP registration surface with
  the other parallel-group-C modules (`internal/synology/controlpanel.go`,
  `internal/application`, `internal/mcpserver/server.go`). New operation package
  under `internal/synology/operations/kmip` and domain model under
  `internal/domain/kmip`; no overlap with the file-service, time, or
  external-access modules beyond the shared facade and tool registry.
- **WI-008 (encrypted-share keys)** is the primary coordination point: KMIP is
  the escrow backend for encrypted shared folders, so the two modules must agree
  on ownership — WI-008 owns per-share key lifecycle, WI-079 owns the KMIP
  client/server configuration those keys are escrowed through. Sequence WI-079's
  live write verification with WI-008 to avoid stranding a real encrypted share.
- Certificate binding depends on a certificate already installed by the
  certificate-management module; this WI references it by name/id and does not
  own certificate install/issue.
