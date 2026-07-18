# Synology x86_64 package

The offline SPK embeds the same `linux/amd64` gateway image distributed for
generic Linux. It supports DSM 7.2.1-69057 or newer on x86_64 only and requires
Container Manager 1432+ plus Web Station. See
`deploy/synology/SUPPORTED.md` for the tested-release matrix.

## Runtime boundary

Synology's official `docker-project` resource worker preloads
`image.tar.gz`, creates/recreates the Compose project, and starts/stops/removes
it with the package lifecycle. The package does not mount the Docker socket and
the gateway never controls Container Manager. The container publishes port
8080 only as host `127.0.0.1:18765`; a package-owned DSM authentication adapter
listens only on `127.0.0.1:18766`; Web Station registers the HTTPS `/dsmctl`
portal. The package does not modify firewall, router, QuickConnect, VPN, DNS,
or certificate settings.

The container runs as the dynamically resolved package UID/GID with a
read-only root filesystem, all capabilities dropped, no-new-privileges,
resource limits, and a bounded tmpfs. State is mounted from
`SYNOPKG_PKGVAR` at `/data`. The 32-byte vault key and separate 32-byte DSM
assertion key are package-private files under `SYNOPKG_PKGHOME/secrets`, mounted
read-only. Do not loosen their permissions.

## DSM administration

Open `/dsmctl/admin` through the DSM HTTPS origin. The adapter accepts requests
only from the loopback portal, passes the DSM cookie to Synology's documented
`authenticate.cgi`, and requires membership in the DSM `administrators` group.
The desktop application is also admin-only (`allUsers: false`). The adapter
strips any caller-supplied identity header and signs a fresh 45-second HMAC
assertion. The core requires the exact audience, administrator bit, issuance
window and one-time `jti`; forged, expired, replayed, wrong-audience, and raw
username headers fail closed. Synology mode permanently disables the generic
bootstrap and local administrator token endpoints.

MCP clients connect to `https://NAS/dsmctl/mcp` using scoped bearer tokens
created in the admin page. DSM cookies do not authorize MCP. One installation
can dynamically hold up to 32 NAS profiles, including the host NAS and remote
NAS systems. For the host NAS use its LAN DNS name/address—container
`localhost` points back to the container. Prefer a trusted certificate or
explicitly confirm and pin its SHA-256 fingerprint.

## Lifecycle, recovery, and logs

Package status requires both the host authentication adapter and gateway
container health. A managed NAS being offline is application health data and
does not mark the package stopped. Before upgrade, the scripts copy
`gateway.db`, `master.key`, and `platform.key` to
`SYNOPKG_PKGVAR/backups/pre-upgrade-*`; the database migration also creates an
adjacent pre-schema backup and fails closed without replacing recoverable
state.

The uninstall wizard retains state and keys by default, allowing reinstall of
the same package identity. Full deletion requires selecting the destructive
option and typing `DELETE`; it removes only the exact package var/home
contents. This is irreversible without a backup containing both state and the
original keys.

Logs are available at `/var/log/packages/dsmctl-gateway.log`,
`/var/log/synopkg.log`, `SYNOPKG_PKGVAR/synology-auth.log`, and the Container
Manager project log. A missing/wrong master key prevents startup by design;
restore the matched database and keys together rather than generating a new
key over existing state.

## Build and validate

Build the immutable amd64 image with fixed `VERSION`, `REVISION`, `CREATED`,
and `--provenance=false` (the release workflow emits external BuildKit and
in-toto provenance metadata), then on Linux run:

```sh
SOURCE_DATE_EPOCH="$(git show -s --format=%ct)" \
  deploy/synology/build-spk.sh 0.1.0-1 dsmctl-gateway:release dist
deploy/synology/validate-spk.sh dist/dsmctl-gateway-0.1.0-1-x86_64.spk
sha256sum -c dist/SHA256SUMS
```

Package installation and model certification must be done on real Synology
hardware; structure validation or an ordinary Docker Engine is not a substitute.
