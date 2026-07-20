---
id: WI-064
title: Sign and verify release artifacts with attested provenance and SBOM
status: deferred
priority: P2
owner: ""
depends_on:
  - WI-017
parallel_group: E
touches:
  - .github/workflows/gateway-release.yml
  - deploy/synology/build-spk.sh
  - deploy/verify-release.sh
  - docs/release-verification.md
  - docs/release-policy.md
---

# WI-064 — Sign and verify release artifacts with attested provenance and SBOM

> **Decision (2026-07-20): not signing for now — deferred.** Release-artifact
> signing/attestation is not being pursued at this time; the signing
> root-of-trust question is set aside rather than answered. Revisit when release
> signing is prioritized. The scope below is preserved for that future pickup.

## Outcome

A release consumer can cryptographically verify that a downloaded gateway image,
`x86_64` SPK, and generic Compose bundle were built by this project's release
pipeline and were not altered in transit — including on a Synology NAS that
installs the SPK fully offline. The pipeline emits detached signatures over the
release checksums, a signed provenance attestation cryptographically bound to
the artifact digests, and a signed/attested SBOM, plus a single verification
tool and a published trust anchor that make "is this the real release?" a
mechanical, offline-capable check.

## Scope

- Sign the existing `SHA256SUMS` manifest that WI-017's release job already
  produces, so a verifier authenticates the whole artifact set through one
  signature plus the per-file hashes.
- Replace the hand-rolled, unsigned in-toto/SLSA JSON currently written inline
  in `gateway-release.yml` with a proper signed provenance attestation
  (DSSE-wrapped) whose `subject` digests match the shipped image, SPK, and
  Compose bundle.
- Produce a signed or attested form of the syft SPDX SBOM that WI-017 already
  generates, bound to the image digest it describes.
- Ship one `deploy/verify-release.sh` consumer tool that, given a downloaded
  artifact directory and the published trust anchor, verifies the signature over
  `SHA256SUMS`, verifies every listed file hash, verifies the provenance
  attestation subjects, and reports a single pass/fail — and that can run
  offline for the SPK path (no live transparency-log or CA lookup required at
  verify time).
- Publish the trust anchor (public key or the exact keyless verifier identity
  and issuer) in-repo and document a `docs/release-policy.md` covering signing
  identity, key custody location, rotation, and revocation.
- Document the end-to-end verification procedure for both the generic Linux and
  Synology consumers in `docs/release-verification.md`.

## Non-goals

- Building, packaging, or shipping the image, SPK, or Compose bundle, or the
  reproducible-build determinism check — WI-017 owns all of that and this item
  layers on top of its emitted artifacts.
- Regenerating checksums or the SBOM content (WI-017 already emits `SHA256SUMS`
  and the SPDX SBOM); this item signs/attests them, it does not recompute them.
- Signing the DSM package for Synology Package Center's own submission/trust
  process (external release process, explicitly out of scope in WI-017).
- Runtime signature enforcement inside the gateway, admin UI, or SPK install
  scripts. Verification is a documented pre-install consumer step, not an
  automatic gate the package performs on itself.
- Any change to DSM operation ownership, plan/apply, secrets handling, or the
  container image contents.
- In-toto SLSA Level 3 hermetic-builder attestation or a hosted transparency
  service; only build-provenance binding of digests to this repo's pipeline is
  in scope.

## Design constraints

- Preserve the portability and deployment boundary in
  `architecture-contracts.md` and `gateway-deployment.md`: signing and
  verification are release-pipeline and operator-tooling concerns. The container
  image, application layer, and DSM facade are unchanged; no signing key,
  private material, or verification logic is embedded in the running gateway or
  in any DSM request path.
- No private signing key or its passphrase enters the repository, the image, the
  SPK, `/data`, plans, logs, or any artifact. Only public trust material is
  committed or shipped. This mirrors the secrets rules in
  `architecture-contracts.md`.
- The SPK is installed offline (`gateway-deployment.md`: install must not reach a
  registry). The chosen scheme's SPK verification path must therefore succeed
  with only the published public trust anchor and material bundled alongside the
  artifacts — no mandatory online CA/transparency-log fetch at verify time.
- Signing must not break WI-017's reproducibility: the deterministic artifact
  bytes (image, SPK, Compose) remain byte-identical across two builds. Detached
  signatures and attestations sit beside the artifacts; they do not modify the
  signed bytes.
- `deploy/verify-release.sh` depends only on tooling the release notes name and
  fails closed: a missing signature, a hash mismatch, an unbound provenance
  subject, or an untrusted signer identity is a non-zero exit with a clear
  reason, never a silent pass.

## Product decision required (blocks `ready`)

Choose the release signing root of trust and its offline-verifiable scheme:

1. Keyless OIDC (cosign + GitHub Actions identity, Rekor inclusion) versus a
   long-lived published key (cosign key, minisign, or GPG). The SPK's mandatory
   offline install means keyless verification must ship a bundled Rekor/inclusion
   proof or a long-lived key must be used for that path.
2. Where the public key / verifier identity is published and its custody,
   rotation, and revocation home.
3. Whether this GitHub repository is public, which gates keyless-OIDC
   availability.

Once decided, the remaining work (DSSE-wrapping provenance, signing
`SHA256SUMS`, attesting the SBOM, the offline verify tool, and the docs) is
directly implementable.

## Acceptance criteria

- [ ] The release job publishes a detached signature over `SHA256SUMS`, and
      `deploy/verify-release.sh` exits non-zero when either the signature or any
      listed file hash does not match, and zero when both do.
- [ ] The provenance artifact is a signed attestation whose subject digests
      equal the actual `sha256` of the shipped image, SPK, and Compose bundle;
      the previous unsigned inline JSON is removed from `gateway-release.yml`.
- [ ] The SBOM is shipped in a signed or attested form bound to the image
      digest, and verification of that binding is part of `verify-release.sh`.
- [ ] Verifying the SPK, its image, and its checksums with only the published
      trust anchor and artifacts in a directory succeeds with no network access
      (validated by running `verify-release.sh` with networking disabled).
- [ ] Tampering with any released byte (flip one byte in the SPK, the SBOM, or
      `SHA256SUMS`) makes `verify-release.sh` fail with a message identifying the
      failing artifact.
- [ ] A signature produced by a key or identity other than the published trust
      anchor is rejected.
- [ ] `docs/release-verification.md` gives copy-pasteable generic-Linux and
      Synology verification steps, and `docs/release-policy.md` records the
      signing identity, key custody location, rotation, and revocation policy.
- [ ] `go test ./... -count=1`, `go vet ./...`, and `git diff --check` pass, and
      WI-017's two-build byte-identical image and SPK checks still pass unchanged.

## Verification

- Run the release workflow (or its extracted signing steps) on a tag build and
  confirm `SHA256SUMS`, the detached signature, the signed provenance
  attestation, and the attested SBOM all appear in `dist/`.
- Positive path: `deploy/verify-release.sh dist/ <trust-anchor>` exits 0.
- Negative paths: corrupt each of the SPK, SBOM, and `SHA256SUMS` in a scratch
  copy and confirm distinct non-zero failures; sign with a throwaway key/identity
  and confirm rejection.
- Offline path: run the SPK verification with networking disabled and confirm it
  still passes (no dependency on Rekor/Fulcio/OCSP at verify time).
- No live DSM NAS interaction is required or authorized for this item; it is
  entirely release-pipeline and local-tooling work with fixture artifacts.

## Coordination

- Depends on WI-017, which owns `deploy/synology/build-spk.sh`,
  `deploy/synology/validate-spk.sh`, and `.github/workflows/gateway-release.yml`
  and already emits the reproducible artifacts, `SHA256SUMS`, the SPDX SBOM, and
  the (to-be-replaced) unsigned provenance JSON. This item edits the release
  workflow's tail and adds new files; sequence after or coordinate closely with
  the WI-017 owner (`synology-distribution`) so both do not edit
  `gateway-release.yml` concurrently. WI-017's acceptance criterion covering
  "checksums, SBOM, embedded image digest" stays with WI-017; this item adds the
  signing/attestation/verification layer above it.
- Sibling WI-010 decompositions (structured DSM errors, observability, CI
  matrix) touch different files and do not overlap this item's release-signing
  surface.

## Handoff

Fill this only when pausing incomplete work.
