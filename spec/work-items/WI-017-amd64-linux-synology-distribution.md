---
id: WI-017
title: Ship generic Linux and Synology x86_64 distributions
status: in_progress
priority: P1
owner: "synology-distribution"
depends_on: [WI-014, WI-015, WI-016, WI-024]
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
  directory/UID setup, master-key creation, reverse-proxy
  requirements, backup guidance, upgrade, and uninstall instructions.
- Build an x86_64-only SPK with `arch="x86_64"`, minimum DSM
  `7.2.1-69057`, and a `ContainerManager>=1432` package dependency.
- Bundle the exact image as `image.tar.gz` and use Synology's `docker-project`
  worker to preload, create/recreate, start, stop, and remove the project.
- Mount package persistent data at `/data`, mount the package-private master
  key read-only at `/run/secrets/master.key`, and use a bounded tmpfs.
- Resolve the DSM package user's dynamic UID/GID into the Compose project so
  the non-root container can read its private keys and write package data
  without widening host file permissions.
- Publish the container port only to host loopback and register a DSM
  portal/reverse proxy. Do not automatically modify DSM firewall, router, VPN,
  QuickConnect, or certificate settings.
- Route the portable local-administrator setup/login UI through the DSM portal
  without deriving any Gateway identity or NAS credential from the host DSM.
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
- Gateway local-administrator setup/login is identical in generic Linux and
  Synology. DSM sessions, groups, forwarded identity headers, and the host NAS
  are never Gateway administrator authority. UID/GID mismatch must not be
  worked around by running the gateway as root or making key files
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
- [ ] The DSM portal serves the same one-hour local-account setup, login Cookie,
      profile/vault/token models, and security behavior as generic Linux with
      no image-specific branch or host-DSM authentication adapter.
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
WI-024 supersedes this item's DSM platform-authentication design. Real hardware
certification and completion of this item are paused until WI-024 replaces the
adapter and revalidates the shared image/SPK artifacts.

## Handoff

Implementation and local verification are complete; real Synology hardware
certification remains before this item can truthfully move to `done`.

- Last known good state: WI-024 is complete. Generic Linux and Synology use the
  same portable local-administrator setup/login flow and exact image. The SPK
  owns only package lifecycle, master-key creation, offline image/project
  resources, and loopback DSM portal wiring; there is no DSM authentication
  adapter, platform assertion key, bootstrap secret, or implicit host-NAS
  profile. Deterministic offline SPK assets, lifecycle scripts, release
  workflow, supported matrix, and user documentation are present.
- Verification: `go test ./... -count=1`, `go vet ./...`, and
  `git diff --check` pass. Two fixed-input `linux/amd64` builds were identical
  at image ID
  `sha256:23bc4034b70d97d347ca87dfe0fa193bddfa5d1dba190bcd73207318bf5fa1d6`.
  The hardened generic Docker lifecycle passed local setup, readiness, Cookie
  controls, secret non-disclosure, and administrator-session persistence across
  restart. Two SPK builds were byte-identical at SHA-256
  `9d576f03f350fa9950eaffaef3cf010bf71144f2de5f11ff19080bf0cff45186`;
  the offline x86_64 SPK structure/security validator passed with embedded
  image archive SHA-256
  `acc85497bf8a13688129b98ffe314dae190c28270f82722971cd4c0d4ab5b88b`.
  Compose parsing, shell/JSON syntax, icon dimensions, mounted-key
  non-disclosure, and container security inspection also pass locally.
- Blocker: install/start/stop/reboot/portal/upgrade/uninstall and behavior tests
  still require authorized real Intel and AMD x86_64 Synology systems across
  the DSM versions claimed in `deploy/synology/SUPPORTED.md`. No ordinary
  Docker result is recorded as a Container Manager or DSM portal pass.
- Temporary resources: none. Test containers and temporary state/artifact
  directories were removed; local Docker test image tags remain available for
  follow-up without containing gateway secrets.
