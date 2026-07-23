# Generic amd64 Linux deployment

This deployment runs the same `linux/amd64` image embedded in the Synology
package. Use Docker Engine with Compose v2, or Podman on an amd64 Linux host.
The published port is loopback-only; an HTTPS reverse proxy is required.

## Install with Docker Compose

1. Copy `.env.example` to `.env` and replace `DSMCTL_IMAGE` with the release's
   exact `repository@sha256:...` value. Verify `SHA256SUMS` before use.
2. Run `sudo ./setup.sh`. It creates only the persistent directory and the
   32-byte vault master key; there is no bootstrap or DSM identity secret.
3. Run `docker compose --env-file .env -f compose.yaml up -d`.
4. Configure an existing TLS reverse proxy to
   `http://127.0.0.1:${DSMCTL_PORT}/`. Rewrite the upstream `Host` to
   `127.0.0.1`, set `X-Forwarded-Proto https`, and do not publish the backend
   port to the LAN. `nginx.conf.example` shows the required boundary.
5. Open `${DSMCTL_PUBLIC_ORIGIN}/admin` and create the local Gateway
   administrator username/password. Setup remains available until the first
   account is created; keep the backend loopback-only and the reverse proxy
   restricted to a trusted deployment network until then.

After initialization, the page shows the ordinary login form and issues an
expiring HttpOnly/SameSite browser session. It never returns or asks the user
to store a long-lived administrator bearer token. If the first visit
unexpectedly shows a login form even though nobody initialized this instance,
stop it and explicitly remove the empty data directory before trying again.
Removing a used data directory destroys every NAS session and MCP token.

The Compose file runs with the selected non-root UID/GID, a read-only root
filesystem, all capabilities dropped, `no-new-privileges`, a 16 MiB tmpfs,
resource limits, and no Docker socket.

## Podman

Create the directories and master key with `setup.sh`, then run the immutable
digest with equivalent controls:

```sh
podman run -d --name dsmctl-gateway --replace --restart=unless-stopped \
  --user 10001:10001 --read-only --cap-drop=all \
  --security-opt=no-new-privileges --pids-limit=128 --memory=256m --cpus=1 \
  --tmpfs /tmp:rw,size=16m,mode=1777,uid=10001,gid=10001 \
  -p 127.0.0.1:18765:8080 \
  -v /opt/dsmctl/data:/data:rw \
  -v /opt/dsmctl/secrets:/run/secrets:ro \
  "$DSMCTL_IMAGE" \
  --listen=0.0.0.0:8080 --state=/data/gateway.db \
  --master-key-file=/run/secrets/master.key \
  --admin-public-url="$DSMCTL_PUBLIC_ORIGIN" \
  --allowed-hosts=127.0.0.1,localhost \
  --allowed-origins="$DSMCTL_PUBLIC_ORIGIN"
```

Rootless Podman requires host directories owned by the container-visible UID;
use `podman unshare chown` when user namespaces remap it.

## Upgrade, backup, and removal

Before upgrade, stop the service and run `sudo ./backup.sh PATH.tar.gz`. A
usable recovery set contains both `gateway.db` and the exact `master.key`;
neither one is useful alone. Update only `DSMCTL_IMAGE` to the new published
digest, verify checksums/SBOM/provenance, then run `docker compose pull` and
`docker compose up -d`. Schema migration creates an adjacent pre-migration DB
backup and fails closed on error.

`docker compose down` removes runtime objects and retains data. To delete all
credentials, first make a verified backup, then explicitly remove only the
configured data and secret directories. Losing `master.key` makes encrypted
NAS credentials unrecoverable.

For a NAS managed by this gateway, do not enter `localhost`: inside the
container it means the gateway container. Use the NAS LAN DNS name/address,
and let the Gateway perform normal certificate verification automatically. If
CA, hostname, or validity verification fails, review every warning and
explicitly confirm the SHA-256 fingerprint observed by the Gateway. This also
supports a LAN IP absent from the certificate SAN; there is no up-front
TLS-mode choice. The NAS
hosting Docker receives no special access and must also be explicitly added
and authenticated through its own DSM Web Login. A single gateway supports up
to 32 profiles; MCP tokens can be limited by scope and NAS allowlist, and
high-risk applies require an exact, short-lived approval.
