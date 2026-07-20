---
id: WI-065
title: Certificate management
status: in_progress
priority: P1
owner: "claude"
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/certificate
  - internal/synology/operations/certificate
  - internal/synology/certificate.go
  - internal/synology/client.go
  - internal/runtime/manager.go
  - internal/application/certificate.go
  - internal/cli/certificate.go
  - internal/cli/root.go
  - internal/mcpserver/server.go
  - internal/mcpserver/read_only.go
  - docs/certificate.md
---

# WI-065 — Certificate management

## Outcome

A CLI user or MCP agent can read the Control Panel → Security → Certificate
surface — the installed certificates, their subjects, issuers, SANs, expiry, and
which DSM service each one serves — and, through the hash-bound plan/apply
contract, import a certificate + private key (+ intermediate chain), set the
default certificate, bind a certificate to a service, and delete a certificate,
all under guardrails. This is a focused Control Panel module in the sense of
[WI-006](WI-006-control-panel-modules.md): one typed module for the certificate
setting area, never a generic `set key=value` proxy over `SYNO.Core.Certificate.*`.

Certificate replacement is the single highest-consequence Control Panel write in
dsmctl so far: replacing or deleting the certificate the DSM desktop presents can
break admin TLS — including the very connection dsmctl rides — so every mutation
here is high risk and the module carries a current-session protection policy in
the spirit of the architecture contract's built-in/current-principal rule.

The API families named below are the author's best knowledge from the DSM
certificate UI and WebAPI conventions and **must be live-verified at
implementation time** with a throwaway read-only `DSMCTL_DUMP` probe before any
code trusts them — the standing policy is that source-doc / mobile-client field
and method names are frequently stale (see [[dsm-webapi-live-verify-fields]]).
Name the family precisely: `SYNO.Core.Certificate.CRT` (list / import / export /
set / delete), `SYNO.Core.Certificate.Service` (service→certificate binding), and
`SYNO.Core.Certificate.LetsEncrypt` (ACME issue/renew — a non-goal, see below).

## Scope

Sliced read-first, then guarded write, so the read slice ships independently.

### Slice A — read-only (independently shippable)

- **Installed certificates** — `SYNO.Core.Certificate.CRT` `list` →
  normalized per-cert state: stable `id`, description, `is_default`,
  `self_signed`, `renewable`, subject (CN + org), issuer, the SAN list, key
  algorithm/size, and `valid_from` / `valid_till` with a locally computed
  days-to-expiry. The list returns **public certificate metadata only** — no
  private-key material — and the decoder must reject any shape that would carry
  key bytes into the domain model.
- **Service bindings** — `SYNO.Core.Certificate.Service` `list` → the set of
  DSM services (DSM desktop, FTPS, WebDAV, SMTP/mail relay, reverse-proxy
  vhosts, package services, etc.) and the certificate `id` each is bound to,
  joined back to the certificate list so a read shows "cert X serves DSM +
  FTPS". Exact method name (`list` vs `get`) and the service-key vocabulary are
  **to be live-verified**.
- **Capabilities** — each operation reports a stable name, selected backend,
  API, and version; the module fails closed and reports `(not supported)` when
  `SYNO.Core.Certificate.*` is absent, without disabling other Control Panel
  modules.

Export is deliberately **not** in Slice A even though it does not mutate the NAS
— see the export note under Design constraints; it exfiltrates private-key
material and is treated as a guarded, gateway-stripped local transfer.

### Slice B — guarded write (plan/apply, hash-bound)

- **Import a certificate bundle** — `SYNO.Core.Certificate.CRT` `import`,
  a **multipart** upload (reusing the streaming multipart transport added in
  [WI-049](WI-049-file-station.md), `internal/synology/client.go`) carrying the
  private key, leaf certificate, and optional intermediate chain as file parts,
  plus `id` (empty = new, set = replace), `desc`, and `as_default`. The likely
  field names are `key` / `cert` / `inter_cert` and the endpoint may be a
  dedicated cgi rather than `entry.cgi` — **all to be live-verified**; the
  documented private-key field name is exactly the kind of detail that is
  frequently wrong.
- **Set default certificate** — `SYNO.Core.Certificate.CRT` `set`
  (`as_default`, keyed by `id`) — which certificate DSM presents by default.
- **Bind a service to a certificate** — `SYNO.Core.Certificate.Service` `set`,
  mapping a service key to a certificate `id`.
- **Delete a certificate** — `SYNO.Core.Certificate.CRT` `delete` (keyed by
  `id`) — destructive; deleting a bound or default certificate breaks the
  services that depend on it.

Every Slice-B operation is **high risk**. There is no low-risk write in this
module.

## Non-goals

- **Let's Encrypt / ACME issuance and renewal**
  (`SYNO.Core.Certificate.LetsEncrypt` `create` / `renew`). Reason: issuance is
  not a settings patch — it drives an external CA handshake with an HTTP-01 or
  DNS-01 challenge (needing port 80 reachability or DNS provider control),
  is subject to CA rate limits, and can partially fail leaving a pending order.
  That is a challenge-orchestration capability, not a Control-Panel write, and
  belongs in its own work item. This module *manages* certificates that already
  exist (imported or previously issued); it can set/bind/delete an existing LE
  certificate but does not obtain or renew one.
- **Self-signed generation / renew-in-place** (`CRT` create/renew of a
  DSM-generated cert). Deferred; the primary flow is bring-your-own cert via
  import.
- **KMIP / centralized key management** (`SYNO.Core.Certificate.KMIP` or the
  Certificate → Settings KMIP tab). Its key material has the same secrets
  handling as private keys and warrants its own scoped WI.
- **CSR generation and export of a signing request.**
- **Anything that ships certificate private-key bytes back to a caller** — no
  MCP tool returns key material; export (below) writes only to local disk.

## Design constraints

- **Focused, typed module — never a raw `SYNO.Core.Certificate` proxy.**
  Per [WI-006](WI-006-control-panel-modules.md), the surface is a small set of
  intents (import, set-default, bind, delete), not a generic passthrough of
  arbitrary CRT/Service fields.
- **Private keys are secrets and never enter requests, plans, hashes, logs, or
  MCP arguments.** The imported private key (PEM) is supplied by
  `credential_ref: env:NAME`, resolved to bytes **only at apply time**, streamed
  as the multipart key part, and zeroized after; it never touches the plan file,
  the approval hash, the result, or any log line (see the secrets contract and
  [WI-009](WI-009-credential-lifecycle.md)). The **leaf and intermediate
  certificates are public** and may be recorded — the plan fingerprints the
  *desired* certificate by its locally parsed public fields (subject, SAN,
  issuer, serial, not-before/not-after, and the SHA-256 fingerprint of the DER),
  plus the *name* of the key's `credential_ref` (never its value).
- **Pre-apply local validation, before the NAS is touched.** Parse the supplied
  cert and key locally and verify: (1) the private key mathematically matches
  the leaf certificate's public key; (2) the intermediate(s) chain to the leaf;
  (3) `not_after` is in the future; and (4) the leaf's SAN/CN covers the
  profile's connection host when the target binding is the DSM service. A
  mismatch or an expired/uncovering cert is a plan-time error, not a silent
  apply that bricks TLS.
- **Current-session / DSM-service protection policy.** dsmctl pins the DSM
  server certificate for its own transport (per the lab TLS-pinning setup).
  Replacing the certificate bound to the DSM service **changes the leaf dsmctl
  is pinned to**, so the post-apply re-read cannot ride the old pinned
  connection. The apply must anticipate this: it knows the new leaf's fingerprint
  locally (from the imported PEM), so the verify step re-pins to the expected new
  fingerprint (or falls back to CA validation) rather than treating the pinning
  break as an apply failure — and a broken-and-not-recoverable handshake is
  reported as a lockout, not a success. Replacing/deleting the certificate that
  serves the current session requires an explicit acknowledgement, analogous to
  the built-in/current-principal protection in the mutation-safety contract.
- **Patch + postcondition (the recurring lesson).** Plan records and hashes the
  complete observed certificate + binding state; apply rejects a changed state,
  performs the typed operation, and **re-reads** `CRT list` + `Service list` to
  verify the requested certificate is present with the expected fingerprint and
  that the intended default/bindings actually took effect — DSM silently ignores
  some fields, and a certificate operation that "succeeds" but leaves the old
  cert bound is exactly the failure mode this catches.
- **Export exfiltrates a private key.** `SYNO.Core.Certificate.CRT` `export`
  returns an archive that **contains the private key PEM**. It is read-only with
  respect to the NAS but produces secret material, so it is modeled like a
  FileStation download: it writes only to a caller-named local file, never
  returns key bytes over MCP (no base64 payload), redacts `_sid`/`SynoToken`
  from any transfer error (the `redactTransferURL` lesson from WI-049), and is
  **stripped from the read-only remote gateway**. Flag it plainly in help text as
  extracting private-key material.
- **Independent compatibility boundary, fail-closed.** `CRT`, `Service`, and
  `LetsEncrypt` are selected per operation; a NAS advertising `CRT` but not
  `Service` (or vice versa) reports the missing area `(not supported)` without
  erroring the module. When no certificate API is advertised at all, the module
  reports unsupported and performs no calls.

## Acceptance criteria

- [x] Slice A: `certificate capabilities|list` (CLI) and the `get_certificates`
      / `get_certificate_capabilities` MCP tools return normalized state — certs
      with subject/issuer/SAN/expiry/days-remaining/default flag and the bound
      services. The no-private-key property is currently **structural** — the
      decoder is a public-field whitelist (`operations/certificate/decode.go`) and
      the domain model has no key-bearing field (`domain/certificate/model.go`) —
      not yet enforced by a dedicated key-injection test (see the unchecked item
      below). (Bindings are inline in `CRT.list`, so no separate `services`
      read/`SYNO.Core.Certificate.Service` call is needed — that API's `list` is
      code 103 on the lab; the per-cert `services[]` array is authoritative.)
- [ ] A decoder test injects a `key`/`private_key` field into a `CRT.list`
      response fixture and asserts it is dropped, upgrading the no-key guarantee
      from structural to test-enforced (currently only asserted by a comment in
      `operation_test.go`).
- [x] Slice A live verification on the DSM 7.3 lab: read the two installed
      certificates (self-signed default `synology` serving 6 services incl. the
      DSM desktop; renewable Let's Encrypt QuickConnect cert), confirmed the
      default/self-signed/renewable flags and expiry-in-days; `--json` output
      carries no key material.
- [x] Capability report lists the certificate read operation with stable name,
      backend, API, and version; a NAS without `SYNO.Core.Certificate.CRT`
      reports it `(not supported)` and fails closed.
- [ ] Pre-apply local validation: import plan rejects a key/cert mismatch, an
      expired leaf, and (for a DSM-service binding) a leaf whose SAN does not
      cover the connection host — proven by unit tests over fixture PEMs.
- [ ] Slice B import via guarded hash-bound plan/apply: private key supplied by
      `credential_ref: env:NAME`, absent from plan/hash/result/logs
      (request-capture + log-scrub tests); apply merges into fresh state,
      rejects stale state, and postcondition-re-reads the new fingerprint.
      Classified high risk; `plan_/apply_certificate_*` excluded from the
      read-only gateway.
- [ ] Slice B set-default, service-bind, and delete via plan/apply with
      postcondition re-read; deleting or replacing the certificate that serves
      the current dsmctl session requires an explicit acknowledgement and the
      verify step re-pins to the known new leaf fingerprint.
- [ ] Export writes the archive to a local file only, returns no key bytes over
      MCP, redacts session tokens from transfer errors, and is stripped from the
      read-only gateway.
- [ ] Let's Encrypt issuance/renewal is documented as an out-of-scope follow-on
      with the ACME-challenge reason recorded.
- [ ] Slice B live verification on the DSM 7.3 lab is performed **only** against
      a throwaway, self-issued test certificate that is not bound to the DSM
      service, with a full revert — the DSM-serving cert is never replaced during
      testing without explicit per-session authorization.

## Verification

- Unit: decoder tolerance + malformed/key-bearing rejection; local
  key/cert-match, chain, expiry, and SAN-coverage validators over fixture PEMs;
  request-capture proving the private key rides only the multipart body and
  never the plan/hash/log; precondition fingerprint + staleness rejection;
  export transfer-URL redaction and read-only-gateway stripping.
- `go build ./...`, `go vet ./...`, `go test ./... -count=1`.
- Live reads on the DSM 7.3 lab against the real certificate store. **Live
  writes require explicit per-session authorization** and use a disposable
  self-issued test certificate not bound to any service; replacing the
  DSM-serving certificate is out of bounds for routine verification because a
  bad apply can lock the session out (this module's own protection policy).
  Field/method/endpoint names for `import` (especially the `key`/`cert`/
  `inter_cert` multipart field names and the cgi path), `set` default, and
  `Service` `set` are confirmed with a throwaway `DSMCTL_DUMP` probe before the
  writes ship — do not trust the source docs.

## Coordination

- New packages under `internal/domain/certificate` and
  `internal/synology/operations/certificate`; parallel group C alongside the
  other Control Panel / module work, depends on the module pattern from
  [WI-006](WI-006-control-panel-modules.md).
- Reuses the streaming multipart transport and the `redactTransferURL` /
  content-tool gateway-stripping conventions established by
  [WI-049](WI-049-file-station.md) in `internal/synology/client.go` — coordinate
  with any concurrent client-core change.
- Reuses the `credential_ref: env:NAME` mechanism from
  [WI-009](WI-009-credential-lifecycle.md) for the private key; coordinate if the
  credential-resolution interface changes.
- Certificate management was explicitly listed as a non-goal of the External
  Access module ([WI-041](WI-041-external-access.md)); this WI is that deferred
  surface. No file overlap beyond the shared facade and MCP server registration.
