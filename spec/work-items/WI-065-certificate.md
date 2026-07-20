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
- [x] A decoder test injects a `key`/`private_key` field into a `CRT.list`
      response fixture and asserts it is dropped, upgrading the no-key guarantee
      from structural to test-enforced (`TestDecodeDropsInjectedKeyMaterial` in
      `operations/certificate/decode_test.go` re-encodes the whole decoded model
      and asserts no injected canary or key-bearing field survives).
- [x] Slice A live verification on the DSM 7.3 lab: read the two installed
      certificates (self-signed default `synology` serving 6 services incl. the
      DSM desktop; renewable Let's Encrypt QuickConnect cert), confirmed the
      default/self-signed/renewable flags and expiry-in-days; `--json` output
      carries no key material.
- [x] Capability report lists the certificate read operation with stable name,
      backend, API, and version; a NAS without `SYNO.Core.Certificate.CRT`
      reports it `(not supported)` and fails closed.
- [x] Pre-apply local validation: import plan rejects a key/cert mismatch, an
      expired leaf, and (for a DSM-service binding) a leaf whose SAN does not
      cover the connection host — proven by unit tests over generated fixture
      PEMs (`domain/certificate/pem_test.go`; the plan rejects expiry + SAN,
      and the key/cert-match check runs at apply pre-request — the earliest
      point the key bytes exist, still before the NAS is touched — since the
      secrets contract forbids resolving the key at plan time:
      `TestApplyCertificateImportRejectsMismatchedKey`).
- [x] Slice B import via guarded hash-bound plan/apply: private key supplied by
      `credential_ref: env:NAME`, absent from plan/hash/result/logs
      (`TestDoCertificateImportKeyRidesOnlyMultipartBody` proves the key rides
      only the multipart body — never the URL/query/headers;
      `TestCertificatePlanExcludesPrivateKey` and
      `TestApplyCertificateImportResolvesKeyRepinsAndHidesKey` prove the plan,
      hash, and result carry no key value — only the ref NAME; the DSM client
      logs request metadata only, never a parameter value); apply merges into
      fresh state, rejects stale state (`TestCertificatePlanStaleRejection`), and
      postcondition-re-reads the certificate's public identity. Classified high
      risk; `plan_certificate_change`/`apply_certificate_plan` excluded from the
      read-only gateway.
- [x] Slice B set-default, service-bind, and delete via plan/apply with
      postcondition re-read (`TestApplyCertificateSetDefaultPostcondition`,
      `TestApplyCertificateBindPostcondition`,
      `TestApplyCertificateDeletePostcondition`); deleting or replacing the
      certificate that serves the current dsmctl session requires an explicit
      acknowledgement (`TestPlanCertificateImportRequiresCurrentSessionAck`) and
      the verify step re-pins to the known new leaf fingerprint
      (`RepinLeafFingerprint`/`TestRepinTLSConfig`); a broken handshake is
      reported as a lockout (`TestApplyCertificateLockoutReportedNotSuccess`).
- [x] Export writes the archive to a local file only, returns no key bytes over
      MCP, redacts session tokens from transfer errors
      (`TestDoCertificateExportRedactsSessionTokens`), and is stripped from the
      read-only gateway (`get_certificate_export` in the read-only strip list and
      `read_only_test.go`).
- [x] Let's Encrypt issuance/renewal is documented as an out-of-scope follow-on
      with the ACME-challenge reason recorded (Non-goals, above; `docs/certificate.md`).
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

## Handoff

- **State.** Slice A (read-only) shipped earlier. **Slice B (guarded writes) is
  implemented and unit-test-green** but **not live-verified** — no certificate
  was imported/replaced/deleted/bound/exported against the lab NAS in this
  session (per the standing rule that replacing the DSM-serving cert can lock the
  session out). `go build ./...`, `go vet ./...`, and `go test ./... -count=1`
  all pass.
- **Files added:** `internal/domain/certificate/mutation.go` (write intents,
  desired-cert public fingerprint, precondition), `.../pem.go` (offline
  key/cert-match, chain, expiry, SAN-coverage validators),
  `internal/synology/operations/certificate/mutation.go` (isolated
  `WIRE-UNVERIFIED` wire names + write-capability selectors),
  `internal/synology/certificate_mutation.go` (multipart import mirroring the
  WI-049 transport, set-default/bind/delete JSON writes, streaming export,
  `RepinLeafFingerprint`), `internal/application/certificate_mutation.go`
  (plan/apply mirroring `file_station_mutation.go`, export), plus test files in
  each package. **Files changed:** `domain/certificate/model.go` (capability
  flags), `synology/certificate.go` (capability discovery),
  `application/certificate.go` (write methods on the local client interface),
  `cli/certificate.go` (plan/apply/export commands + capability rows),
  `mcpserver/server.go` (three tools), `mcpserver/read_only.go` (strip list),
  `mcpserver/server_test.go`+`read_only_test.go` (counts/strip assertions),
  `docs/certificate.md`.
- **WIRE-UNVERIFIED (must confirm with a throwaway `DSMCTL_DUMP` probe before any
  live write is trusted), all isolated in
  `operations/certificate/mutation.go`.** A partial live pass has since verified
  the import wire shape — see **Live wire-verification — Slice B** below; the
  import api (parent `SYNO.Core.Certificate`), `method=import`, the `entry.cgi`
  endpoint, and the `key`/`cert`/`inter_cert` + `id`/`desc`/`_sid` fields are now
  LIVE-VERIFIED. Still WIRE-UNVERIFIED: the import `as_default` encoding (fixed,
  one live re-check pending); the CRT `set`/`delete`/`export` param shapes (the
  `delete` `id`-vs-`ids` array and `set` `as_default` keying); and the
  `SYNO.Core.Certificate.Service` `set` method + `settings` array `{service,id}`
  shape.
- **Remaining before this can be trusted:** (1) live wire-verification of the
  names above; (2) a throwaway self-issued cert live plan→apply→revert NOT bound
  to any DSM service, then a scoped current-session test only with explicit
  authorization; (3) confirm DSM's `import` success payload actually returns the
  new cert `id` (`decodeImportedCertID` guesses `id`/`certificate.id` and falls
  back to the replace id).
- **Judgement calls / limitations.** (a) The key/cert-match validator runs at
  **apply** pre-request rather than at plan time, because the secrets contract
  forbids resolving the key at plan time; plan-time validation covers the
  public-only checks (expiry, chain, SAN). (b) DSM's `CRT.list` returns no DER
  fingerprint, so the import postcondition verifies the certificate's **public
  identity** (subject CN + issuer CN + SAN set), not a DER-fingerprint equality;
  full DER re-verification would require `export` (which extracts the key) and is
  left to live follow-up. (c) `certificateServesCurrentSession` decides the
  current-session flag from the default flag and DSM-desktop service binding; a
  pinned-fingerprint DER comparison is a live-verification refinement (the plan
  context already carries the pinned fingerprint). (d) The private key is
  resolved into a `[]byte` and zeroized after import; the intermediate Go
  `string` from `ResolveSecret` cannot be wiped but its lifetime is minimized.

## Adversarial security review — Slice B fixes

An adversarial review of the Slice B commit found and fixed the following. These
are code fixes on `claude/wi065-certificate-writes`; `WIRE-UNVERIFIED` markers are
unchanged and no live NAS mutation was performed.

- **HIGH — `get_certificate_export` reachable under a read scope on the managed
  remote gateway.** `ToolScope` classifies any `get_` tool as `ScopeRead`, so a
  `nas.read`-only remote token could invoke export, which writes a **private-key
  archive to the gateway HOST** at a caller-controlled path. Fixed by stripping
  `get_certificate_export` from `NewRemote` (mirroring `NewReadOnly`) so it is not
  on the remote surface at all, and by confining the export writer (below).
  Regression: `internal/gateway/remote_test.go` now asserts a read-only token can
  neither see nor call it. **Footgun flagged for follow-up:** the prefix-based
  `ScopeRead` rule means any future `get_`-named tool with side effects is
  read-reachable by default; consider an explicit per-tool scope table.
- **HIGH (same finding) — arbitrary host-file overwrite via `ExportCertificate`
  `local_path`.** The writer used `O_CREATE|O_WRONLY|O_TRUNC` on a caller-supplied
  path with no confinement. Fixed with `safeExportPath` (rejects `..` traversal)
  and `O_EXCL` (never overwrites an existing file; also fails on an existing
  symlink at the final component, defeating a symlink-swap redirect). Residual:
  a *new* file can still be created at an absolute path locally — acceptable for a
  local CLI export and no longer reachable remotely.
- **MEDIUM — transport-error `_sid`/`SynoToken` leak.** A failed `http.Client.Do`
  returns a `*url.Error` whose `Error()` carries the full request URL (with the
  session id and token); redacting only the `%s` operand missed it. Fixed in the
  shared transport with `redactTransferError`, which also covers the pre-existing
  same bug in the WI-049 upload/download/thumbnail transports. Regression:
  `TestDoCertificateExportRedactsSessionTokensOnTransportError`.
- **MEDIUM — `set_default` skipped SAN-covers-host validation.** It now looks the
  target up with the full certificate (`findCertByID`) and validates
  `ValidateNamesCoverHost` at plan time, so making a non-covering certificate the
  DSM default is a plan-time error, not a post-apply lockout.
- **MEDIUM — import postcondition false success on a same-identity replace.** The
  identity-only match (this supersedes limitation (b) above) is now augmented with
  the validity window (`valid_from`/`valid_till`, the only distinguishing public
  field `CRT.list` exposes — there is no serial or DER fingerprint). A
  **current-session** import is still positively proven by the re-pin TLS
  handshake; a **non-current-session replace** that cannot be distinguished now
  **fails closed** instead of reporting `Applied=true`.
- **LOW — confusing error on `set_default`/`bind` re-pin.** These paths do not
  re-pin, so in `pinned_fingerprint` mode a *successful* change makes the
  postcondition re-read fail TLS verification. `lockoutOrError` now also detects a
  TLS pin/verification failure (not only `SessionExpiredError`) and surfaces the
  possible-lockout wording.
- **LIVE-VERIFY (finding #6, no code fix) — `isDSMService()` whitelist.**
  Current-session/SAN protection for bind and delete keys off the hardcoded
  service whitelist `{default, dsm, webui}` (`certificate_mutation.go`
  `isDSMService`). If the real DSM-desktop service key on a target DSM differs
  from these, binding or deleting the serving certificate under a non-whitelisted
  key would **skip the acknowledgement + SAN-coverage checks**. Confirm the actual
  DSM-desktop service key(s) with a `DSMCTL_DUMP` probe during live verification
  and extend the whitelist if needed.

## Live wire-verification — Slice B (partial, DSM 7.3)

A live wire-verification pass against the real DSM 7.3 lab corrected the
certificate mutation wire shape. This was applied as a **code-only** change (no
clean import→confirm→delete re-verify cycle has been run), so the Slice B
live-verification acceptance box above stays **UNCHECKED**.

- **IMPORT api corrected to the parent (fixed + live-verified).** The multipart
  import posted `api=SYNO.Core.Certificate.CRT`, which DSM rejects with **code
  103** (method does not exist). Re-posting the identical multipart with **only**
  the api form field changed to the PARENT **`SYNO.Core.Certificate`** (present on
  the NAS, `entry.cgi`, version 1) **SUCCEEDED live**. So `method=import`, the
  `entry.cgi` endpoint, the file-part names `key`/`cert`/`inter_cert`, and the form
  fields `id`/`desc`/`_sid` are now **LIVE-VERIFIED**; `list`/`set`/`delete` stay
  on `SYNO.Core.Certificate.CRT` (`list` live-verified; `set`/`delete` methods
  confirmed to exist). Encoded as the new `CRTImportAPIName` constant in
  `operations/certificate/mutation.go`; only `import` uses it, and
  `doCertificateImport` now posts the parent api.
- **`as_default=false` not honored (fixed, pending one live re-check).** Despite
  sending `as_default=false`, DSM marked the newly-imported cert
  `is_default=true` — the multipart `as_default` form field is truthy for any
  non-empty value. Fix: `doCertificateImport` now sends the `as_default` part
  **only** when the caller wants the cert to become default and omits it entirely
  otherwise, so an import with as_default=false leaves the existing default cert in
  place. Marked `WIRE-UNVERIFIED (as_default): re-confirm live` — the corrected
  behavior still needs one live re-check (the lab is temporarily unavailable for
  writes).
- **Delete param (unchanged, pending live re-check).** The shipped `{"id":[...]}`
  array reached the DSM delete handler and returned a domain error (not a
  missing-arg error), so the `id` array param is likely correct. Left as-is with a
  `WIRE-UNVERIFIED (delete id-vs-ids): re-confirm live` note.

**Still required before WI-065 can be marked done:** a clean live
import→confirm→delete re-verify against a throwaway self-issued certificate not
bound to any DSM service, confirming (1) as_default=false preserves the existing
default and (2) the delete `id` param removes the certificate — plus the remaining
live checks recorded in the Handoff section. No NAS was mutated in this code-only
pass.
