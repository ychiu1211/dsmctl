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
8080 only as host `127.0.0.1:18765`; Web Station routes the HTTPS `/dsmctl`
portal directly to that loopback endpoint. The package does not modify
firewall, router, QuickConnect, VPN, DNS, or certificate settings.

The container runs as the dynamically resolved package UID/GID with a
read-only root filesystem, all capabilities dropped, no-new-privileges,
resource limits, and a bounded tmpfs. State is mounted from `SYNOPKG_PKGVAR` at
`/data`. The only mounted secret is the package-private 32-byte vault master key
under `SYNOPKG_PKGHOME/secrets/master.key`. Do not loosen its permissions.

There is no DSM authentication adapter, `authenticate.cgi` call, platform
assertion, bootstrap secret, or implicit Host NAS identity. The container
cannot and does not determine which NAS is hosting it.

## Gateway administration

Open `/dsmctl/admin` within one hour after the first start and create the local
Gateway administrator username/password. The password is stored only as an
Argon2id verifier. Setup closes after the first successful transaction. If the
Gateway remains uninitialized and the hour expires, restart the package to
open a new one-hour window.

Later visits use the ordinary Gateway login page and an expiring
HttpOnly/SameSite browser session. DSM users, DSM administrator-group
membership, and DSM cookies do not grant Gateway access. Likewise, Gateway
administrator login does not grant access to the Host NAS or any other NAS.

If the first visit unexpectedly shows an initialized login page when nobody
created the account, do not enroll a NAS. Uninstall with the explicit
delete-data choice (or otherwise remove only this package's empty persistent
state) and reinstall. Once NAS credentials exist, deleting state destroys
those encrypted sessions and requires a matching backup to recover them.

MCP clients connect to `https://NAS/dsmctl/mcp` using separately scoped bearer
tokens created in the Gateway admin page. One installation can hold up to 32
NAS profiles. Every NAS, including the NAS hosting the SPK, must be explicitly
added with its reachable LAN DNS name/address and authenticated through that
profile's own DSM Web Login. Container `localhost` points back to the container
and is never a Host NAS shortcut. Prefer a trusted certificate or explicitly
confirm and pin its SHA-256 fingerprint.

## Lifecycle, recovery, and logs

Package status follows the gateway container's local health. A managed NAS
being offline is application health data and does not mark the package stopped.
Before upgrade, the scripts copy `gateway.db` and `master.key` to
`SYNOPKG_PKGVAR/backups/pre-upgrade-*`; a database migration also creates an
adjacent pre-schema backup and fails closed without replacing recoverable
state.

The uninstall wizard retains state and key by default, allowing reinstall of
the same package identity. Full deletion requires selecting the destructive
option and typing `DELETE`; it removes only the exact package var/home
contents. This is irreversible without a backup containing both state and the
original master key.

Logs are available at `/var/log/packages/dsmctl-gateway.log`,
`/var/log/synopkg.log`, and the Container Manager project log. A missing/wrong
master key prevents startup by design; restore the matched database and key
together rather than generating a new key over existing state.

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
hardware; structure validation or ordinary Docker Engine is not a substitute.
