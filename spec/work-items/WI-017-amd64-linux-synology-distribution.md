---
id: WI-017
title: Ship generic Linux and Synology x86_64 distributions
status: in_progress
priority: P1
owner: "synology-distribution"
depends_on: [WI-014, WI-015, WI-016]
parallel_group: G
touches:
  - deploy/container
  - deploy/linux
  - deploy/synology
  - .github/workflows
  - docs/gateway.md
  - docs/synology-package.md
  - README.md
---

# WI-017 - Ship generic Linux and Synology x86_64 distributions

## Outcome

The same pinned `linux/amd64` gateway image can be deployed with ordinary
Docker/Podman on an amd64 Linux host or installed offline as an x86_64 Synology
SPK. Both deployments preserve state across restart and upgrade, expose only a
secured reverse-proxy endpoint, and pass the same gateway behavior suite.

## Scope

- Produce one reproducible `linux/amd64`, `CGO_ENABLED=0` image with version,
  revision, SBOM, checksum, and provenance metadata.
- Provide generic Docker Compose, documented Podman invocation, persistent
  directory/UID setup, master-key and bootstrap-secret creation, reverse-proxy
  requirements, backup guidance, upgrade, and uninstall instructions.
- Build an x86_64-only SPK with `arch="x86_64"`, minimum DSM
  `7.2.1-69057`, and a `ContainerManager>=1432` package dependency.
- Bundle the exact image as `image.tar.gz` and use Synology's `docker-project`
  worker to preload, create/recreate, start, stop, and remove the project.
- Mount package persistent data at `/data`, mount the package-private master
  key read-only at `/run/secrets/master.key`, mount a separate platform
  assertion key when Synology authentication is enabled, and use a bounded
  tmpfs.
- Resolve the DSM package user's dynamic UID/GID into the Compose project so
  the non-root container can read its private keys and write package data
  without widening host file permissions.
- Publish the container port only to host loopback and register a DSM
  portal/reverse proxy. Do not automatically modify DSM firewall, router, VPN,
  QuickConnect, or certificate settings.
- Add a Synology admin-auth adapter that validates the DSM session outside the
  core image, confirms administrator/application privilege, and sends a
  short-lived signed identity assertion to the admin API.
- Add Package Center lifecycle/status, install/upgrade/uninstall messaging,
  data retention choice, state migration backup, and actionable log paths.
- Create release and install/upgrade test automation plus an explicit supported
  model/DSM matrix.

## Non-goals

- ARM or multi-architecture images/SPKs, native SPK fallback, DSM before
  7.2.1, or models without Container Manager.
- Pulling the production image from a registry during SPK install or start.
- Public Package Center acceptance in the first completion of this item;
  submission artifacts may be prepared, but external review is a later release
  process.
- Bundled public TLS, automatic DNS, port forwarding, VPN, QuickConnect, or
  Internet exposure.
- OIDC, high availability, or migration between two live gateway instances.

## Design constraints

- Follow `spec/gateway-deployment.md`. The core image remains byte-identical
  between generic Linux and Synology delivery.
- The SPK uses the official resource worker and never mounts the Docker socket
  or shells out from the gateway to control Container Manager.
- DSM authentication remains outside the image. Forwarded identity must be
  integrity-protected, short-lived, audience-bound, and accepted only from the
  private adapter path; a raw username header is invalid.
- Synology mode disables the generic bootstrap/local-admin path and uses a
  dedicated platform assertion key, not the vault master key. UID/GID mismatch
  must not be worked around by running the gateway as root or making key files
  group/world-readable.
- Package start/stop/status reflects the Container Manager project without
  treating an unreachable managed NAS as a dead package.
- Upgrade preserves database and master key, backs up state before migration,
  and fails closed without replacing recoverable data.
- Uninstall distinguishes removing runtime artifacts from deleting persistent
  credentials. Deletion requires an explicit user choice and is documented as
  irreversible unless a backup exists.

## Acceptance criteria

- [ ] One image digest passes the same behavior tests under generic Docker and
      Synology Container Manager.
- [ ] Generic Compose and documented Podman deployment start as non-root with
      read-only rootfs, dropped capabilities, no-new-privileges, bounded tmpfs,
      no Docker socket, and loopback-only publication.
- [ ] The SPK refuses unsupported DSM/architecture/dependency combinations and
      installs on a supported x86_64 DSM with no registry access.
- [ ] Package start, stop, status, NAS reboot, and restart correctly follow the
      Docker project while managed-NAS outages remain application health data.
- [ ] The DSM portal reaches admin and MCP endpoints through TLS/reverse proxy;
      the backend port is not reachable directly from another LAN host.
- [ ] DSM admin authentication produces a verified signed identity assertion;
      unauthenticated, non-admin, expired, replayed, wrong-audience, and forged
      assertions fail closed.
- [ ] Generic bootstrap and Synology DSM administration both manage the same
      profile/vault/token models without image-specific branches.
- [ ] Upgrade preserves profiles, encrypted secrets, tokens, audit data, and
      master key and successfully applies a tested schema migration.
- [ ] Retain-data uninstall leaves documented recoverable state; delete-data
      uninstall removes the exact package data/key targets after explicit
      confirmation.
- [ ] Release artifacts include SPK/image/Compose checksums, SBOM, embedded
      image digest, supported DSM/model matrix, and security configuration.
- [x] Installation and user documentation covers multi-NAS routing, why
      container `localhost` is not the host NAS, TLS pinning, backup limits,
      token rotation, approvals, and recovery from a missing key.

## Verification

- `go test ./... -count=1` and `go vet ./...`.
- Reproducible amd64 image and SPK build in CI; inspect image config and Compose
  security controls.
- Generic amd64 Linux smoke/upgrade test with Docker Engine; documented Podman
  smoke test.
- Synology tests on at least one Intel and one AMD x86_64 model, covering DSM
  `7.2.1-69057`, `7.2.2-72806`, and every newer release claimed supported.
- Offline SPK install, reboot, upgrade, retained-data uninstall, and full-delete
  uninstall tests.
- Multi-NAS test uses read-only operations against the host NAS by LAN name and
  two remote profiles. No disruptive live DSM mutation is authorized.

## Coordination

Depends on the completed gateway, state/vault, and authorization work. Most
files are new deployment assets, but release documentation and workflows may
overlap WI-010 reliability/release hardening if that item is later specified.

## Handoff

Implementation and local verification are complete; real Synology hardware
certification remains before this item can truthfully move to `done`.

- Last known good state: the core supports irreversible platform-admin mode,
  short-lived audience-bound replay-protected assertions, and the external
  loopback DSM auth adapter. Generic Linux assets, deterministic offline SPK
  assets, lifecycle scripts, release workflow, supported matrix, and user docs
  are present.
- Verification: `go test ./... -count=1` and `go vet ./...` pass. The same
  `linux/amd64` image passed generic bootstrap/restart and platform-mode
  no-bootstrap/restart Docker lifecycles. Two fixed-input builds produced image
  ID `sha256:8a762b0ec480204d85ece83826ca1845d706f43c7d6ed78e0f3a3efd39e05055`;
  SPKs built independently from those images were byte-identical at SHA-256
  `6bb0cc4181cbb37e1cdfbe1757852f45ce1fc5ebf91efd9fa0c7c7f3a16b4124`.
  Offline image load, Compose parsing, SPK structure/security validation,
  shell/JSON syntax, icon dimensions, mounted-key non-disclosure, and container
  security inspection passed.
- Blocker: install/start/stop/reboot/portal/upgrade/uninstall and behavior tests
  still require authorized real Intel and AMD x86_64 Synology systems across
  the DSM versions claimed in `deploy/synology/SUPPORTED.md`. No ordinary
  Docker result is recorded as a Container Manager or DSM portal pass.
- Temporary resources: none. Test containers and temporary state/artifact
  directories were removed; local Docker test image tags remain available for
  follow-up without containing gateway secrets.
