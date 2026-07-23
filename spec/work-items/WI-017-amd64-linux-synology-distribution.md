---
id: WI-017
title: Ship generic Linux and Synology x86_64 distributions
status: in_progress
priority: P1
owner: "codex"
depends_on: [WI-014, WI-015, WI-016, WI-032, WI-033, WI-035, WI-037, WI-038, WI-081]
parallel_group: G
touches:
  - .githooks/pre-commit
  - deploy/container
  - deploy/linux
  - deploy/synology
  - deploy/release
  - .github/workflows
  - scripts/build-cli-release.sh
  - scripts/release_archive.go
  - scripts/validate-release-assets.sh
  - scripts/install.sh
  - scripts/install.ps1
  - docs/gateway.md
  - docs/synology-package.md
  - docs/public-release-plan.md
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
  `7.2.1-69057`, and a `ContainerManager>=1432` package dependency. A separate
  `DockerEngine` package does not satisfy the `docker-project` worker boundary.
- Bundle the exact image as `image.tar.gz` and use Synology's `docker-project`
  worker to preload, create/recreate, start, stop, and remove the project.
- Mount package persistent data at `/data`, mount the package-private master
  key read-only at `/run/secrets/master.key`, and use a bounded tmpfs.
- Resolve the DSM package user's dynamic UID/GID into the Compose project so
  the non-root container can read its private keys and write package data
  without widening host file permissions.
- Bind the container HTTP server only to host loopback and register a DSM
  portal/reverse proxy. Do not automatically modify DSM firewall, router, VPN,
  QuickConnect, or certificate settings.
- Use host networking only in the Synology Compose adapter so the non-root
  Gateway can send and receive LAN-scoped findhost UDP broadcast. Do not add
  network capabilities or expose the HTTP listener on a LAN address.
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
- [x] The DSM portal reaches admin and MCP endpoints through TLS/reverse proxy;
      the backend port is not reachable directly from another LAN host.
- [ ] The DSM portal serves DSM Web Login by default, optionally supports an
      explicitly configured local fallback, and preserves the shared
      profile/vault/token models without exposing generic first-run setup.
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
WI-032 supersedes this item's DSM platform-authentication design. WI-037 has
completed the final tokenized MCP Server presentation and validated the shared
`linux/amd64` image locally. WI-038 adds the guided approval, token-lifecycle,
and enrollment flows. WI-081 replaces manual TLS-mode selection with verified
trust enrollment; certify the administration UI only after it lands. Real
Synology hardware certification remains before the distribution item can
complete.

Continuation 2026-07-23: the user explicitly asked Codex to finish public
distribution after confirming that the DSM deliverable must be a downloadable
SPK rather than a `go install` target. No other active agent is attached to
this workspace task, so Codex is continuing the existing WI-017 instead of
creating a competing release workflow. The user subsequently narrowed public
publication to CLI archives and the DSM SPK only. The workflow still builds the
Gateway image internally because the offline SPK bundles it, but does not
publish standalone MCP, image, Compose, or GHCR artifacts. This does not claim
the outstanding hardware/lifecycle matrix.

## Handoff

The Intel DSM 7.3 hardware path now installs and runs through the official
workers. Broader hardware/lifecycle certification remains before this item can
truthfully move to `done`.

- Last known good state: `dsmctl-gateway` 7.3.2-4 is installed and running on
  DS3018xs `192.0.2.235`, DSM 7.3-81168, with Container Manager 24.0.2-1606.
  DSM acquires `docker-project` before `postinst`, so `preinst` writes the
  dynamic package UID/GID and stable FHS paths into the staged Compose `.env`.
  Package home is mounted read-only at `/run/secrets`; `postinst` creates or
  migrates `master.key`. DS3018xs lacks CPU CFS and PIDs cgroup controllers, so
  the Synology Compose omits those unsupported settings while retaining the
  enforced 256 MiB memory limit and 16 MiB `/tmp` tmpfs. The Synology adapter
  uses host networking so findhost UDP broadcast sees the physical LAN, while
  the HTTP listener remains restricted to host `127.0.0.1:18765` with a
  non-root user, all capabilities dropped, no-new-privileges, and no Docker
  socket.
- Verification: the 7.3.2-4 image ID is
  `sha256:91956123260915b7aed45339a87cefce351d20950e9f3993c3c4263dd31b0092`.
  Two fixed-input 7.3.2-4 SPK builds were byte-identical at SHA-256
  `486480005264b9552b43b2d4e30e817833956cc1d6576d2b996884fb2118fc12`,
  and both passed `deploy/synology/validate-spk.sh` plus every release
  checksum. Package Center completed the 7.3.2-3 to 7.3.2-4 upgrade and the
  upgrade wizard confirmed that state and private-key recovery copies were
  written. DSM 7.3's package status still reports stopped after its final
  system stop trigger even though Package Center exposes Open, loopback health
  is successful, and the replacement container owns its listener. Container
  Manager reports `dsmctl-gateway` as `running(1)` and Docker reports `healthy`,
  dynamic user `241224:241224`, read-only rootfs, memory `268435456`, cap-drop
  `ALL`, no-new-privileges, and the bounded tmpfs. Local loopback and HTTPS
  `/dsmctl/healthz` return `{"status":"ok"}`, `/dsmctl/` returns a prefix-safe
  307 to `/dsmctl/admin/`, `/dsmctl/admin` returns 200, and
  unauthenticated `/dsmctl/mcp` reaches the gateway and returns the expected
  401 Bearer challenge. Database and key inodes survived stop/start and
  upgrade; the 32-byte 0600 master-key SHA-256 remained unchanged. Final local
  verification passed `go build -o bin/dsmctl-current.exe ./cmd/dsmctl`,
  `go test ./... -count=1`, `go vet ./...`, shell syntax, and
  `git diff --check`.
- LAN discovery: the installed Gateway's Add NAS wizard completed a live
  findhost scan from the container and displayed 141 Synology devices across
  the lab LAN broadcast domain. The pre-fix bridge-network container could not
  reach this broadcast domain. Desktop access to `192.0.2.235:18765` still fails while
  NAS loopback and the Web Station HTTPS `/dsmctl/healthz` route both return
  `{"status":"ok"}`, proving the discovery fix did not expose the backend
  listener on the LAN.
- Administration UI: the empty NAS Profile list now matches the Passwords
  empty-state spacing. The corrected descendant selector applies 40 px top and
  bottom padding through the intermediate `#profiles` list container; live
  browser inspection measured the empty state at 220 px high with both paddings
  present, and the action no longer touches the panel edge.
- Desktop integration: the UI config declares Synology's required
  `images/dsmctl_{0}.png` icon template and ships 16, 24, 32, 48, 64, 72, and
  256 pixel variants of the canonical four-tile dsmctl mark. Package Center's
  64/256 pixel icons use the same bytes. The Web Service resource also points
  at `ui/images/dsmctl_{0}.png`; DSM copied all seven variants into the live
  WebService shortcut directory, and the live 72-pixel file's SHA-256 exactly
  matches the packaged icon. The main-menu entry is present. Its portal root
  now redirects to the existing initialized Gateway login, proving the vault
  state survived the upgrade. Local verification passed the full Go test
  suite, `go vet`, shell syntax, JSON parsing, release checksums, and
  `git diff --check`.
- Blocker: NAS reboot, retain-data uninstall/reinstall, explicit delete-data
  uninstall, DSM 7.2.1/7.2.2, and AMD x86_64 hardware remain unverified. No
  reboot or destructive uninstall was performed on the shared lab NAS.
- Temporary resources: none. Task Scheduler diagnostic task ID 3, diagnostic
  logs, uploaded `/tmp` SPKs/Compose file, and the temporary root SSH key were
  removed. The installed/running package and its intended pre-upgrade recovery
  copies remain. Local Docker image tags and reproducible `dist/` artifacts
  remain for follow-up and contain no gateway secrets.

Public-distribution continuation verified 2026-07-23:

- The single tag workflow builds deterministic Windows/Linux amd64 `dsmctl`
  archives, the offline DSM SPK, checksum-verifying installers, Apache-2.0,
  checksums, and support metadata. A manual dispatch is build-only;
  `dsmctl-vX.Y.Z-N` publishes a GitHub prerelease, downloads every published
  asset, and re-runs the complete validator.
- Standalone `dsmctl-mcp`, Gateway image, Compose, and GHCR publication are
  intentionally outside this release. The image remains an internal build
  input embedded in the offline SPK.
- The release archiver unit tests and pinned `actionlint` passed. Two local
  Windows/Linux CLI archive builds were byte-identical and contained only the
  `dsmctl` executable, README, and Apache-2.0 text. The local `linux/amd64` scratch
  image built successfully with the full license inside it. Two SPKs from that
  image were byte-identical at SHA-256
  `f91618a43e470eaf8f26fed793b5c27f648d47969c754582ec358df4e518426a`;
  SPK structure/security validation and all generated checksums passed.
- Temporary verification archives and the `wi017-verify` image tag were
  removed. The versioned local `dsmctl-gateway:7.3.2-18` image remains as the
  intended build result. No NAS connection, install, or mutation was made.
- Draft PR #2 carries the public release implementation on
  `codex/public-release`. Both push/PR CI matrices passed. After narrowing the
  public scope, build-only Actions run `29984509190` at commit `033f248`
  passed the complete CLI/SPK pipeline, including two deterministic SPK builds,
  the Compose persistence smoke, exact eight-file asset validation, and
  artifact upload; tag-only GitHub prerelease steps were correctly skipped.
- Remaining publication gate: review and merge PR #2, push
  `dsmctl-v7.3.2-18`, and verify the public CLI/SPK prerelease links. WI-017
  still cannot become `done` until its hardware/lifecycle acceptance matrix
  passes.
