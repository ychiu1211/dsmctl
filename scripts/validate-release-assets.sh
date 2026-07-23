#!/usr/bin/env bash
set -euo pipefail

usage() {
	echo "usage: validate-release-assets.sh VERSION DIST_DIR" >&2
	exit 2
}

[[ $# -eq 2 ]] || usage
version="$1"
dist_dir="$2"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-[1-9][0-9]*$ ]] || usage

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
dist_dir="$(cd "$dist_dir" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
for command in cmp find go grep sha256sum sort tar unzip; do
	command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }
done

expected_assets=(
	LICENSE
	SHA256SUMS
	SUPPORTED.md
	dsmctl-gateway-${version}-x86_64.spk
	dsmctl-linux-amd64.tar.gz
	dsmctl-windows-amd64.zip
	install.ps1
	install.sh
)

for asset in "${expected_assets[@]}"; do
	test -f "$dist_dir/$asset" || { echo "missing release asset: $asset" >&2; exit 1; }
done

actual_assets="$(find "$dist_dir" -mindepth 1 -maxdepth 1 -type f -printf '%f\n' | LC_ALL=C sort)"
expected_listing="$(printf '%s\n' "${expected_assets[@]}" | LC_ALL=C sort)"
[[ "$actual_assets" == "$expected_listing" ]] || {
	echo "release directory contains missing or unexpected files" >&2
	diff -u <(printf '%s\n' "$expected_listing") <(printf '%s\n' "$actual_assets") || true
	exit 1
}

expected_unix=$'LICENSE\nREADME.txt\ndsmctl'
actual_unix="$(tar -tzf "$dist_dir/dsmctl-linux-amd64.tar.gz" | LC_ALL=C sort)"
[[ "$actual_unix" == "$expected_unix" ]] || {
	echo "unexpected Linux archive contents" >&2
	exit 1
}

expected_windows=$'LICENSE\nREADME.txt\ndsmctl.exe'
actual_windows="$(unzip -Z1 "$dist_dir/dsmctl-windows-amd64.zip" | LC_ALL=C sort)"
[[ "$actual_windows" == "$expected_windows" ]] || {
	echo "unexpected Windows archive contents" >&2
	exit 1
}

tar -xzf "$dist_dir/dsmctl-linux-amd64.tar.gz" -C "$work"
dsmctl_buildinfo="$(go version -m "$work/dsmctl")"
grep -Fq 'github.com/derekvery666/dsmctl' <<<"$dsmctl_buildinfo" || {
	echo "dsmctl binary does not contain the public Go module path" >&2
	exit 1
}
if [[ "$(uname -s)/$(uname -m)" == "Linux/x86_64" ]]; then
	dsmctl_version="$("$work/dsmctl" --version)"
	[[ "$dsmctl_version" == "dsmctl version $version" ]] || {
		echo "unexpected dsmctl version output: $dsmctl_version" >&2
		exit 1
	}
fi

cmp "$repo_root/LICENSE" "$dist_dir/LICENSE"
"$repo_root/deploy/synology/validate-spk.sh" "$dist_dir/dsmctl-gateway-$version-x86_64.spk"
(
	cd "$dist_dir"
	sha256sum -c SHA256SUMS
)

printf 'Validated complete dsmctl %s release asset set: %s\n' "$version" "$dist_dir"
