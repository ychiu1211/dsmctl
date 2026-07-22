# Synology x86_64 package

The offline SPK embeds the same `linux/amd64` gateway image distributed for
generic Linux. It supports DSM 7.2.1-69057 or newer on x86_64 only and requires
Container Manager 1432+ plus Web Station. DSM 7.3 may expose a separate
`DockerEngine` runtime provider, but that does not replace the
`ContainerManager` package dependency that supplies the Package Center and
`docker-project` integration used by this SPK. Do not run both providers at
once; stop `DockerEngine` before starting the required `ContainerManager`.
See
`deploy/synology/SUPPORTED.md` for the tested-release matrix.

The SPK package version follows the shared dsmctl compatibility train. The
current `7.3.2-14` means DSM compatibility train 7.3.2, dsmctl build 14; it does
not change the separate DSM 7.2.1 host-installation minimum above.

## Runtime boundary

Synology's official `docker-project` resource worker preloads
`image.tar.gz`, creates/recreates the Compose project, and starts/stops/removes
it with the package lifecycle. The package does not mount the Docker socket and
the gateway never controls Container Manager. Synology's findhost protocol
uses LAN-scoped UDP broadcast, so the Synology Compose adapter uses host
networking to see the NAS LAN interfaces. The Gateway HTTP server still binds
only host `127.0.0.1:18765`. A package-owned authentication bridge binds only
`127.0.0.1:18766`, and Web Station routes the HTTPS `/dsmctl` portal to that
bridge. The bridge starts DSM's existing Web Login authorization-code + PKCE
flow, exchanges the one-time code with the host over loopback, requires exact
membership in DSM's `administrators` group, and sends the container only a
short-lived signed login assertion. The non-root
container has all capabilities dropped and cannot configure host networking.
The package does not modify firewall, router, QuickConnect, VPN, DNS, or
certificate settings.

DSM acquires the `docker-project` resource before running `postinst`. The SPK
therefore writes the dynamic-UID Compose `.env` into the staged payload during
`preinst`, referring to DSM's stable package FHS links. The worker can create
the container once DSM creates those links; `postinst` then creates or
migrates the package-private key and the restart policy brings the gateway to
healthy state.

Upgrades deliberately set the worker's `force_recreate` build parameter to
`false`. A changed immutable image reference still updates the container, but
Container Manager keeps the existing per-container profile while doing so.
Forcing recreation removed that profile during an upgrade on DSM 7.3 and the
immediately following package start failed with `container ... profile should
exist`; Package Center then offered Repair even though installation had
finished. The Synology Compose adapter also overrides the image health check
to use the SPK's real host-network listener at `127.0.0.1:18765` rather than the
generic image's port 8080.

The container runs as the dynamically resolved package UID/GID with a
read-only root filesystem, all capabilities dropped, no-new-privileges,
resource limits, and a bounded tmpfs. State is mounted from `SYNOPKG_PKGVAR` at
`/data`. Package home is mounted read-only at `/run/secrets`. It contains the
package-private 32-byte vault master key (`master.key`) and a distinct 32-byte
DSM assertion key (`dsm-sso.key`). The host bridge and container share only the
assertion key; it cannot decrypt the credential database. Do not loosen either
key's permissions.

The Synology project deliberately omits Compose `cpus` and `pids_limit`
settings because older supported x86_64 DSM kernels, including DS3018xs on DSM
7.3-81168, do not expose the corresponding cgroup controllers. Requiring those
settings prevents Container Manager from creating the container. The memory
and tmpfs bounds remain enforced; generic Linux Compose keeps CPU and process
limits on hosts that support them.

The bridge reuses the same `SYNO.API.Auth` `webui` code-grant implementation as
NAS profile Web Login: PKCE state stays server-side, DSM sends the one-time code
to the verified opener origin, and the bridge completes the Noise IK exchange
through the host's loopback HTTP listener. It checks effective
`administrators` membership, discards the resulting DSM session material, and
sends the container only a 30-second audience-bound, one-time assertion for
creating a Gateway session. DSM cookies, passwords, SIDs, SynoTokens, and Noise
keys never enter the container. This integration authorizes Gateway
administration only; it does not create an implicit Host NAS profile or expose
DSM credentials to MCP operations.

## Gateway administration

On a fresh SPK installation, open `/dsmctl/admin` and choose **DSM Web Login**.
The Gateway opens DSM's HTTPS sign-in page in a separate window; DSM handles
password, 2FA, passkey, and approve-sign-in, then returns a one-time code to the
opener. The bridge detects the configured DSM HTTP/HTTPS management ports
(defaults `5000`/`5001`) from the host web-server configuration. Only current
members of DSM's `administrators` group are accepted. No unauthenticated setup endpoint exists
in this mode, so installation does not create or display a bootstrap password.
The resulting Gateway session is HttpOnly/SameSite and is independent from the
DSM browser and code-exchange sessions. Logging out of DSM does not silently
log out dsmctl; use dsmctl's own logout or session-revocation controls. A new
dsmctl login always performs a fresh DSM Web Login and administrator check.

An authenticated DSM administrator may optionally enable an independent local
Gateway username/password from the administrator panel. Its password is stored
only as an Argon2id verifier. Once enabled, the login page offers both methods;
local access remains available when DSM authentication is unavailable. Existing
installations that already have a local administrator keep that account during
upgrade and gain DSM Web Login alongside it. Treat the local fallback as a
recovery credential, store it in a password manager, and leave it disabled when
it is not needed.

Gateway administrator access, by either method, does not grant access to the
Host NAS or any other NAS. Every managed NAS still requires its own profile and
authentication. Generic container deployments do not have the host bridge and
continue to require local first-run administrator setup.

The installed package contributes a `dsmctl Gateway` entry to DSM's main menu
and makes Package Center's **Open** action launch `/dsmctl/admin`. Both surfaces
use the same four-tile dsmctl brand mark as the browser favicon. DSM installs
main-menu entries from package metadata, so pin or drag the entry to the DSM
desktop after installation if a persistent desktop tile is desired.

Web Station owns the package portal shortcut and opens the NAS-local
`/dsmctl/` alias without hard-coding a NAS address. The Gateway root handler
preserves the validated `X-Forwarded-Prefix` and redirects that request to
`/dsmctl/admin/`; the service icon is registered through Web Station as well as
the DSM application metadata so the portal does not fall back to its generic
WWW mark.

The Gateway portal is served by Web Station on the NAS web ports, not DSM's
administration ports. Use `https://NAS_ADDRESS/dsmctl/admin/`; do not append
`:5001`. Likewise, `http://NAS_ADDRESS/dsmctl/admin/` uses the Web Station HTTP
endpoint, while `http://NAS_ADDRESS:5000/dsmctl/admin/` is not a Gateway URL.

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
confirm the SHA-256 fingerprint after reviewing every CA, hostname, and
validity warning reported by the Gateway. This permits an explicitly approved
LAN IP that is absent from the certificate SAN. The Gateway observes the
fingerprint itself; it does not ask for manual entry or an up-front TLS mode.

## Lifecycle, recovery, and logs

Package status follows the gateway container's local health. A managed NAS
being offline is application health data and does not mark the package stopped.
Before upgrade, the scripts copy `gateway.db`, `master.key`, and `dsm-sso.key` to
`SYNOPKG_PKGVAR/backups/pre-upgrade-*`; a database migration also creates an
adjacent pre-schema backup and fails closed without replacing recoverable
state.

The credential vault is not stored in the Docker image or writable container
layer. Recovery uses the three host-mounted files below:

- `/var/packages/dsmctl-gateway/var/gateway.db` contains configuration,
  encrypted NAS credentials and sessions, administrator state, tokens, and
  audit data.
- `/var/packages/dsmctl-gateway/home/master.key` is the separate 32-byte key
  required to decrypt that database.
- `/var/packages/dsmctl-gateway/home/dsm-sso.key` authenticates only the
  short-lived host-to-container DSM assertions. Restoring it preserves current
  delegated sessions; it cannot decrypt `gateway.db`.

Back up the database and keys as one point-in-time set. The safest manual
procedure is to stop `dsmctl Gateway` in Package Center, create an encrypted
archive, and then start the package again. For example, after replacing the
destination with a real shared-folder path:

```sh
sudo synopkg stop dsmctl-gateway
backup_file="/volume1/your-backup-share/dsmctl-gateway-$(date +%Y%m%d-%H%M%S).tgz.enc"
umask 077
sudo tar -C /var/packages/dsmctl-gateway -czf - \
  var/gateway.db home/master.key home/dsm-sso.key | \
  openssl enc -aes-256-cbc -pbkdf2 -salt -out "$backup_file"
openssl enc -d -aes-256-cbc -pbkdf2 -in "$backup_file" | tar -tzf -
sudo synopkg start dsmctl-gateway
```

`openssl` prompts for a backup passphrase; keep that passphrase separately.
The verification command should list exactly `var/gateway.db`,
`home/master.key`, and `home/dsm-sso.key`. Do not start the package until the
archive exists, has a non-zero size, and that listing succeeds. A plain archive
is acceptable only when the destination itself is encrypted and tightly
access-controlled. The in-place `pre-upgrade-*` copies are useful for rollback
but are not disaster-recovery backups because they remain on the same NAS
volume.

Restore only while the package is stopped. Decrypt the archive, put all files
back at the exact paths above, preserve their package ownership and mode 0600,
and then start the package. Never combine a database from one backup with a
master key from another. Test a backup by listing the encrypted archive and,
ideally, restoring it on a disposable installation before relying on it. A
missing `dsm-sso.key` can be regenerated, but all existing DSM-backed Gateway
sessions are then invalid and users must sign in again.

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
  deploy/synology/build-spk.sh 7.3.2-14 dsmctl-gateway:release dist
deploy/synology/validate-spk.sh dist/dsmctl-gateway-7.3.2-14-x86_64.spk
sha256sum -c dist/SHA256SUMS
```

The release workflow is triggered by the matching `dsmctl-v7.3.2-14` tag (or an
explicit `7.3.2-14` dispatch input) and refuses a version that differs from the
source version or the container image label.

Package installation and model certification must be done on real Synology
hardware; structure validation or ordinary Docker Engine is not a substitute.

For repeatable SSH deployment to a test NAS, use the validated artifact and
the same Package Center backend every time (replace `nas` with the SSH config
alias):

```sh
scp -O dist/dsmctl-gateway-7.3.2-14-x86_64.spk \
  nas:/tmp/dsmctl-gateway-7.3.2-14-x86_64.spk
ssh nas 'sha256sum /tmp/dsmctl-gateway-7.3.2-14-x86_64.spk'
ssh nas 'sudo env PATH=/usr/syno/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin \
  /usr/syno/bin/synopkg install \
  /tmp/dsmctl-gateway-7.3.2-14-x86_64.spk'
ssh nas 'sudo env PATH=/usr/syno/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin \
  /usr/syno/bin/synopkg status dsmctl-gateway'
```

Compare the remote SHA-256 with `dist/SHA256SUMS` before installing. `scp -O`
uses the legacy SSH copy protocol for NAS SSH services that do not expose an
SFTP subsystem. Supplying DSM's normal `PATH` is important when sudo uses a
restricted secure path: Container Manager's status script invokes
`synosystemctl` by name, and omitting `/usr/syno/bin` can falsely report that
the dependency is stopped. A successful upgrade must finish as
`installed_and_started`; do not include Repair in the release procedure.
