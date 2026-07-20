---
id: WI-081
title: Automatic TLS trust enrollment
status: in_progress
owner: ""
priority: P2
depends_on: [WI-032, WI-048]
parallel_group: G
touches:
  - internal/tlstrust
  - internal/cli/auth.go
  - internal/gateway/admin/handler.go
  - internal/gateway/admin/ui.go
  - internal/runtime/manager.go
  - .github/workflows/ci.yml
  - docs/
---

# WI-081 — Automatic TLS trust enrollment

## Outcome

Establishing trust in a NAS server certificate (the pin dsmctl rides for its own
transport) is a guided, first-connection enrollment step rather than a manual
out-of-band fingerprint paste, for both the CLI `auth` path and the Gateway
administration UI — without weakening the existing pin-on-first-use guarantee or
silently trusting a changed leaf.

Every NAS web-login flow attempts normal system-CA and hostname verification
before credentials or a one-time login code can be sent. A user is asked about
pinning only after dsmctl has observed a parseable certificate and displayed
every CA, hostname, and validity warning; the user never has to choose a TLS
mode or manually enter a fingerprint before connecting.

## Scope

- A dedicated `internal/tlstrust` package owning trust decisions (parse, pin,
  compare fingerprints, persist), factored out of the ad-hoc pinning previously
  spread across `internal/config`, `internal/runtime/manager.go`, and
  `internal/gateway/admin`. It performs one shared, credential-free TLS preflight
  that verifies the system chain, hostname, and validity period and returns
  structured observed-certificate information plus warnings when normal
  verification fails.
- Preserve `system_ca` and `pinned_fingerprint` as internal connection policies,
  while removing their manual selection and fingerprint entry from the main
  Gateway Admin NAS workflow.
- Before Gateway Web Login or password/OTP enrollment, probe the NAS. Return a
  structured trust challenge for an untrusted certificate or changed pin and
  require an authenticated administrator confirmation bound to the current
  profile revision and a freshly observed fingerprint
  (`internal/gateway/admin/{handler,ui,handler_test}.go`).
- Before CLI `auth login`, perform the same preflight. On a pinnable
  verification failure, show the observed certificate and ask interactively
  whether to persist its SHA-256 fingerprint for that profile before opening the
  browser (`internal/cli/auth.go`, `internal/cli/auth_tls_test.go`).
- Runtime manager and HTTP transport consume the shared trust package for pin
  enforcement (`internal/runtime/manager.go`).
- Keep existing pins fail-closed. Certificate rotation requires a new explicit
  confirmation and never silently replaces a stored fingerprint.
- Update operator documentation and focused tests using local TLS fixtures.

## Non-goals

- Installing a private CA into an operating-system or browser trust store.
- Suppressing the browser's own warning when it directly opens a self-signed
  DSM Web Login page; the stored pin authenticates dsmctl/Gateway traffic only.
- Treating missing/unparseable certificates, TLS protocol or cryptographic
  handshake failures, or general network failures as a trust-on-first-use
  opportunity. CA, hostname, and validity failures remain pinnable after their
  exact warnings are shown.
- Removing the existing CLI-only explicit insecure test profile compatibility
  path, or changing HTTP profile support in this item.
- Automatic trust of a *changed* fingerprint without acknowledgement; ACME or
  automatic CA-store enrollment.
- Any live DSM mutation or distribution/hardware certification.

## Design constraints

- Must preserve the pin-on-first-use / current-transport protection already in
  the architecture contract: a changed server leaf is never silently re-trusted.
- No DSM password, OTP, session, one-time login code, or SynoToken may be sent
  before TLS preflight succeeds or the observed fingerprint is confirmed.
- The server derives the candidate fingerprint from its own TLS handshake. A
  browser-supplied fingerprint is only confirmation input and must match a
  fresh observation before the profile is updated.
- Trust confirmation is local-administrator state, not an MCP tool, caller
  header, or agent-controlled authority. MCP operations continue to fail closed
  on TLS errors and cannot approve a new certificate.
- URL or host changes reset the managed profile to `system_ca`; they never carry
  an old endpoint's pin forward.
- Trust material and fingerprints follow the secrets/redaction contract; no
  `_sid`/`SynoToken`/one-time code appears in logs or error output.
- Existing WI-015 vault, revision/invalidation, secret-redaction, and production
  no-skip-verify contracts remain unchanged.

## Acceptance criteria

- [x] System-trusted NAS certificates proceed without a TLS question and remain
      valid across ordinary CA-issued certificate renewals.
- [x] An unknown-issuer certificate produces an observed SHA-256 fingerprint and
      certificate identity/validity details before any authentication request;
      explicit confirmation persists and enforces the pin.
- [x] Expired, not-yet-valid, and hostname-mismatched certificates show their
      exact validation warnings and remain explicitly pinnable, including a NAS
      reached only by an IP address absent from the certificate SAN.
- [x] A pinned certificate mismatch fails closed, shows old and newly observed
      fingerprints to the administrator, and updates only after a fresh,
      revision-bound confirmation.
- [x] Gateway Web Login and password/OTP enrollment enforce server-side TLS
      preflight even if the browser UI is bypassed (no HTTP request or secret
      reaches DSM before the certificate is trusted).
- [x] CLI `auth login` performs the same preflight and stores a confirmed pin;
      declining or non-interactive input opens no browser and stores no secret.
- [x] Trust decisions live in `internal/tlstrust` and are consumed by the CLI
      auth path, the Gateway admin handler, and the runtime manager transport.
- [x] Focused TLS tests, `go build ./...`, `go vet ./...`, and
      `go test ./... -count=1` pass with local generated TLS fixtures only.

## Verification

- `go test ./internal/tlstrust ./internal/runtime ./internal/cli ./internal/gateway/admin -count=1`.
- `go build ./...`, `go vet ./...`, and `go test ./... -count=1`.
- TLS tests use local generated certificates and local HTTP test servers only.
- No live DSM authentication or mutation is authorized by this item.

## Coordination

- **Number collision resolved here.** The in-flight work was mis-filed as
  `WI-051`, which is already shipped/`done` as the Synology Office settings
  module ([WI-051](WI-051-office-admin.md)). This item, **WI-081**, is its
  canonical number; the mis-numbered
  `spec/work-items/WI-051-automatic-tls-trust-enrollment.md` draft was folded in
  here and removed.
- WI-017 owns packaging and real Synology certification and must certify this
  final enrollment flow after WI-081 completes. This item changes the embedded
  Admin UI and operator TLS wording but does not edit deployment lifecycle or SPK
  assets.
- Overlaps the gateway/auth transport pinning code (group G); coordinate with any
  concurrent change to `internal/runtime/manager.go` and `internal/gateway/admin`.

## Handoff

Completed on branch `claude/tls-trust-enrollment-wi081`, rebased onto
`origin/main`. The preserved WIP (originally authored ~33 commits behind
`origin/main` and mis-numbered WI-051) was rebased, renumbered to WI-081, and
finished: the mis-numbered `WI-051-automatic-tls-trust-enrollment.md` file is
removed, and the roadmap carries only the canonical WI-081 row. `go build`,
`go vet`, and `go test ./... -count=1` are green.
