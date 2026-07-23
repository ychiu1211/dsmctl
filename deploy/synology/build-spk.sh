#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: build-spk.sh PACKAGE_VERSION IMAGE_REFERENCE OUTPUT_DIR" >&2
  exit 2
}

[[ $# -eq 3 ]] || usage
package_version="$1"
image_reference="$2"
output_dir="$3"
[[ "$package_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-[1-9][0-9]*$ ]] || {
  echo "Package version must be DSM_MAJOR.DSM_MINOR.DSM_PATCH-DSMCTL_BUILD (for example 7.3.2-1)" >&2
  exit 2
}

for command in docker go tar gzip sed sha256sum; do
  command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
template="$script_dir/spk"
source_epoch="${SOURCE_DATE_EPOCH:-0}"
revision="${REVISION:-$(git -C "$repo_root" rev-parse HEAD 2>/dev/null || echo unknown)}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
stage="$work/stage"
package_stage="$work/package"
mkdir -p "$stage" "$package_stage" "$output_dir"

os="$(docker image inspect "$image_reference" --format '{{.Os}}')"
architecture="$(docker image inspect "$image_reference" --format '{{.Architecture}}')"
image_id="$(docker image inspect "$image_reference" --format '{{.Id}}')"
image_version="$(docker image inspect "$image_reference" --format '{{index .Config.Labels "org.opencontainers.image.version"}}')"
[[ "$os/$architecture" == "linux/amd64" ]] || { echo "image must be linux/amd64, got $os/$architecture" >&2; exit 1; }
[[ "$image_version" == "$package_version" ]] || { echo "image version must be $package_version, got $image_version" >&2; exit 1; }

cp -R "$template/conf" "$template/scripts" "$template/WIZARD_UIFILES" "$stage/"
sed "s/__VERSION__/$package_version/g" "$template/INFO.template" > "$stage/INFO"
cp "$template/package/ui/images/dsmctl_64.png" "$stage/PACKAGE_ICON.PNG"
cp "$template/package/ui/images/dsmctl_256.png" "$stage/PACKAGE_ICON_256.PNG"
cp "$repo_root/LICENSE" "$stage/LICENSE"

cp -R "$template/package/." "$package_stage/"
sed "s/__VERSION__/$package_version/g" "$template/package/project/compose.yaml.template" > "$package_stage/project/compose.yaml"
rm "$package_stage/project/compose.yaml.template"
mkdir -p "$package_stage/bin"
(
  cd "$repo_root"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false \
    -ldflags="-s -w -buildid=" \
    -o "$package_stage/bin/dsmctl-synology-auth" ./cmd/dsmctl-synology-auth
)
docker tag "$image_reference" "dsmctl-gateway:$package_version"
docker save -o "$work/image.raw.tar" "dsmctl-gateway:$package_version"
mkdir "$work/image"
tar -xf "$work/image.raw.tar" -C "$work/image"
tar --sort=name --mtime="@$source_epoch" --owner=0 --group=0 --numeric-owner -C "$work/image" -cf - . | gzip -n > "$package_stage/image.tar.gz"
cat > "$package_stage/image-metadata.json" <<EOF
{"image_id":"$image_id","image_reference":"dsmctl-gateway:$package_version","platform":"linux/amd64","revision":"$revision","version":"$package_version"}
EOF

chmod 0755 "$package_stage/bin/dsmctl-synology-auth" "$stage/scripts/"*
# NTFS does not persist a Unix executable bit, so record the host bridge mode
# explicitly instead of relying on the build host's filesystem metadata.
tar --sort=name --mtime="@$source_epoch" --owner=0 --group=0 --numeric-owner \
  --exclude='./bin/dsmctl-synology-auth' -C "$package_stage" -cf "$work/package.raw.tar" .
tar --mtime="@$source_epoch" --owner=0 --group=0 --numeric-owner --mode=0755 \
  -C "$package_stage" -rf "$work/package.raw.tar" ./bin/dsmctl-synology-auth
gzip -n < "$work/package.raw.tar" > "$stage/package.tgz"
spk="$output_dir/dsmctl-gateway-$package_version-x86_64.spk"
tar --sort=name --mtime="@$source_epoch" --owner=0 --group=0 --numeric-owner -C "$stage" -cf "$spk" \
  INFO LICENSE PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG WIZARD_UIFILES conf package.tgz scripts

cp "$package_stage/image.tar.gz" "$output_dir/dsmctl-gateway-$package_version-image.tar.gz"
cp "$repo_root/deploy/linux/compose.yaml" "$output_dir/compose.yaml"
cat > "$output_dir/release-metadata.json" <<EOF
{"image_id":"$image_id","platform":"linux/amd64","revision":"$revision","spk":"$(basename "$spk")","version":"$package_version"}
EOF
(
  cd "$output_dir"
  sha256sum "$(basename "$spk")" "dsmctl-gateway-$package_version-image.tar.gz" compose.yaml release-metadata.json > SHA256SUMS
)
printf 'Built %s from %s (%s).\n' "$spk" "$image_reference" "$image_id"
