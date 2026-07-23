#!/usr/bin/env bash
set -euo pipefail

spk="${1:?usage: validate-spk.sh PACKAGE.spk}"
for command in tar gzip grep cmp; do
  command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }
done
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
tar -tf "$spk" > "$work/spk.entries"
! grep -q '^\./' "$work/spk.entries" || {
  echo "SPK entries must be rooted at INFO/package.tgz rather than ./" >&2
  exit 1
}
test "$(sed -n '1p' "$work/spk.entries")" = "INFO" || {
  echo "INFO must be the first SPK entry" >&2
  exit 1
}
tar -xf "$spk" -C "$work"

for required in INFO package.tgz conf/resource conf/privilege conf/PKG_DEPS scripts/start-stop-status PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG; do
  test -e "$work/$required" || { echo "missing SPK entry: $required" >&2; exit 1; }
done
grep -qx 'arch="x86_64"' "$work/INFO"
grep -qx 'os_min_ver="7.2.1-69057"' "$work/INFO"
grep -qx 'adminprotocol="https"' "$work/INFO"
grep -qx 'adminport="443"' "$work/INFO"
grep -qx 'adminurl="dsmctl/"' "$work/INFO"
grep -q '^\[ContainerManager\]$' "$work/conf/PKG_DEPS"
grep -q '^pkg_min_ver=1432$' "$work/conf/PKG_DEPS"
grep -q '^\[WebStation\]$' "$work/conf/PKG_DEPS"
grep -q '"run-as": "package"' "$work/conf/privilege"
grep -q '"preload-image": "image.tar.gz"' "$work/conf/resource"
grep -q '"path": "project"' "$work/conf/resource"
grep -q '"force_recreate": false' "$work/conf/resource"
! grep -q '"force_recreate": true' "$work/conf/resource" || {
  echo "Synology upgrades must preserve the existing Container Manager profile" >&2
  exit 1
}
grep -q 'uname -m' "$work/scripts/preinst"
! grep -q 'SYNOPKG_DSM_ARCH' "$work/scripts/preinst" || {
  echo "preinst must inspect the CPU ISA, not DSM's platform label" >&2
  exit 1
}
grep -q 'staged_target=.*package' "$work/scripts/preinst"
grep -q 'staged_target/project/.env' "$work/scripts/preinst"
grep -q 'DSMCTL_DATA_DIR=/var/packages/\$pkg/var' "$work/scripts/preinst"
grep -q 'DSMCTL_SECRET_DIR=/var/packages/\$pkg/home' "$work/scripts/preinst"
grep -q '"proxy_target": "http://127.0.0.1:18766"' "$work/conf/resource"
grep -q '"icon": "ui/images/dsmctl_{0}.png"' "$work/conf/resource"
grep -q '"name": "Host"' "$work/conf/resource"
! grep -R -q '/var/run/docker.sock\|/run/docker.sock' "$work"

mkdir "$work/package"
tar -xzf "$work/package.tgz" -C "$work/package"
for required in image.tar.gz image-metadata.json project/compose.yaml bin/dsmctl-synology-auth ui/config \
  ui/images/dsmctl_16.png ui/images/dsmctl_24.png \
  ui/images/dsmctl_32.png ui/images/dsmctl_48.png \
  ui/images/dsmctl_64.png ui/images/dsmctl_72.png \
  ui/images/dsmctl_256.png; do
  test -e "$work/package/$required" || { echo "missing package.tgz entry: $required" >&2; exit 1; }
done
tar -tvzf "$work/package.tgz" | grep -Eq '^-rwxr-xr-x .* \./bin/dsmctl-synology-auth$' || {
  echo "Synology authentication bridge must have archive mode 0755" >&2
  exit 1
}
gzip -t "$work/package/image.tar.gz"
grep -q 'network_mode: host' "$work/package/project/compose.yaml"
grep -q -- '--listen=127.0.0.1:18765' "$work/package/project/compose.yaml"
grep -q -- '--administrator-mode=dsm' "$work/package/project/compose.yaml"
grep -q -- '--platform-assertion-key-file=/run/secrets/dsm-sso.key' "$work/package/project/compose.yaml"
grep -q -- '--trusted-proxies=127.0.0.0/8' "$work/package/project/compose.yaml"
grep -q 'healthcheck:' "$work/package/project/compose.yaml"
grep -q 'http://127.0.0.1:18765/healthz' "$work/package/project/compose.yaml"
! grep -q 'http://127.0.0.1:8080/healthz' "$work/package/project/compose.yaml" || {
  echo "Synology healthcheck must use the host-network loopback listener" >&2
  exit 1
}
! grep -q '127.0.0.1:18765:8080' "$work/package/project/compose.yaml" || {
  echo "Synology host networking must use a loopback listener, not a published port" >&2
  exit 1
}
grep -q 'read_only: true' "$work/package/project/compose.yaml"
grep -q 'no-new-privileges:true' "$work/package/project/compose.yaml"
grep -q 'cap_drop:' "$work/package/project/compose.yaml"
grep -q 'mem_limit: 256m' "$work/package/project/compose.yaml"
grep -q 'tmpfs:' "$work/package/project/compose.yaml"
grep -q '/tmp:size=16m' "$work/package/project/compose.yaml"
grep -q 'source: \${DSMCTL_SECRET_DIR}' "$work/package/project/compose.yaml"
grep -q 'target: /run/secrets' "$work/package/project/compose.yaml"
! grep -q '^[[:space:]]*cpus:' "$work/package/project/compose.yaml" || {
  echo "Synology compose must not require an unavailable CPU CFS controller" >&2
  exit 1
}
! grep -q '^[[:space:]]*pids_limit:' "$work/package/project/compose.yaml" || {
  echo "Synology compose must not require an unavailable PIDs controller" >&2
  exit 1
}
! grep -q 'bootstrap\|platform.key' "$work/package/project/compose.yaml"
grep -q '"allUsers": false' "$work/package/ui/config"
grep -q '"icon": "images/dsmctl_{0}.png"' "$work/package/ui/config"
grep -q '"url": "/dsmctl/"' "$work/package/ui/config"
grep -q 'attempts.*-lt 60' "$work/scripts/start-stop-status"
grep -q 'dsmctl-synology-auth' "$work/scripts/start-stop-status"
grep -q '127.0.0.1:18766' "$work/scripts/start-stop-status"
grep -q -- '--dsm-http-port=' "$work/scripts/start-stop-status"
grep -q -- '--dsm-https-port=' "$work/scripts/start-stop-status"
grep -q 'dsm-sso.key' "$work/scripts/postinst"
grep -q 'dsm-sso.key' "$work/scripts/preupgrade"
grep -q 'dsm-sso.key' "$work/scripts/postupgrade"
cmp "$work/PACKAGE_ICON.PNG" "$work/package/ui/images/dsmctl_64.png"
cmp "$work/PACKAGE_ICON_256.PNG" "$work/package/ui/images/dsmctl_256.png"
printf 'Validated offline x86_64 SPK structure and security controls: %s\n' "$spk"
