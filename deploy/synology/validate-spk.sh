#!/usr/bin/env bash
set -euo pipefail

spk="${1:?usage: validate-spk.sh PACKAGE.spk}"
for command in tar gzip grep base64; do
  command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }
done
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
tar -xf "$spk" -C "$work"

for required in INFO package.tgz conf/resource conf/privilege conf/PKG_DEPS scripts/start-stop-status PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG; do
  test -e "$work/$required" || { echo "missing SPK entry: $required" >&2; exit 1; }
done
grep -qx 'arch="x86_64"' "$work/INFO"
grep -qx 'os_min_ver="7.2.1-69057"' "$work/INFO"
grep -q '^pkg_min_ver=1432$' "$work/conf/PKG_DEPS"
grep -q '"run-as": "package"' "$work/conf/privilege"
grep -q '"preload-image": "image.tar.gz"' "$work/conf/resource"
grep -q '"path": "project"' "$work/conf/resource"
grep -q '"proxy_target": "http://127.0.0.1:18765"' "$work/conf/resource"
grep -q '"name": "Host"' "$work/conf/resource"
! grep -R -q '/var/run/docker.sock\|/run/docker.sock' "$work"
! grep -R -q 'platform.key\|platform-assertion\|dsmctl-synology-auth\|synology-auth.pid' "$work"

mkdir "$work/package"
tar -xzf "$work/package.tgz" -C "$work/package"
for required in image.tar.gz image-metadata.json project/compose.yaml ui/config; do
  test -e "$work/package/$required" || { echo "missing package.tgz entry: $required" >&2; exit 1; }
done
gzip -t "$work/package/image.tar.gz"
grep -q '127.0.0.1:18765:8080' "$work/package/project/compose.yaml"
grep -q 'read_only: true' "$work/package/project/compose.yaml"
grep -q 'no-new-privileges:true' "$work/package/project/compose.yaml"
grep -q 'cap_drop:' "$work/package/project/compose.yaml"
! grep -q 'bootstrap\|platform.key\|platform-assertion\|synology-auth' "$work/package/project/compose.yaml"
grep -q '"allUsers": true' "$work/package/ui/config"
printf 'Validated offline x86_64 SPK structure and security controls: %s\n' "$spk"
