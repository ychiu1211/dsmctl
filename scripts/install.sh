#!/usr/bin/env bash
set -euo pipefail

repository="derekvery666/dsmctl"
version=""
prefix="${HOME}/.local/bin"

usage() {
	cat <<'EOF'
Install dsmctl from a checksum-verified GitHub Release archive.

Usage: install.sh [options]

Options:
  --version VERSION  Install a specific version such as 7.3.2-18. Required
                     for prereleases; omitted means the latest stable release.
  --prefix DIR       Install directory (default: $HOME/.local/bin).
  -h, --help         Show this help.
EOF
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--version)
			[[ $# -ge 2 ]] || { echo "--version requires a value" >&2; exit 2; }
			version="$2"
			shift 2
			;;
		--prefix)
			[[ $# -ge 2 ]] || { echo "--prefix requires a value" >&2; exit 2; }
			prefix="$2"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "unknown option: $1" >&2
			usage >&2
			exit 2
			;;
	esac
done

for command in awk curl install mktemp tar uname; do
	command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }
done

if [[ -n "$version" ]]; then
	version="${version#dsmctl-v}"
	version="${version#v}"
	[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-[1-9][0-9]*$ ]] || {
		echo "Invalid version: $version" >&2
		exit 2
	}
	tag="dsmctl-v$version"
fi

case "$(uname -s)/$(uname -m)" in
	Linux/x86_64|Linux/amd64)
		asset="dsmctl-linux-amd64.tar.gz"
		;;
	*)
		echo "This preview installer currently supports Linux amd64 only." >&2
		exit 1
		;;
esac

if [[ -z "$version" ]]; then
	latest_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$repository/releases/latest")"
	tag="${latest_url##*/}"
	[[ "$tag" == dsmctl-v* ]] || { echo "Unable to resolve the latest stable dsmctl release." >&2; exit 1; }
	version="${tag#dsmctl-v}"
fi

download_base="https://github.com/$repository/releases/download/$tag"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
curl -fL --retry 3 --output "$work/$asset" "$download_base/$asset"
curl -fL --retry 3 --output "$work/SHA256SUMS" "$download_base/SHA256SUMS"

expected="$(awk -v asset="$asset" '$2 == asset || $2 == "*" asset { print tolower($1) }' "$work/SHA256SUMS")"
[[ "$expected" =~ ^[0-9a-f]{64}$ ]] || { echo "No unique checksum found for $asset" >&2; exit 1; }
if command -v sha256sum >/dev/null; then
	actual="$(sha256sum "$work/$asset" | awk '{print tolower($1)}')"
elif command -v shasum >/dev/null; then
	actual="$(shasum -a 256 "$work/$asset" | awk '{print tolower($1)}')"
else
	echo "A SHA-256 tool (sha256sum or shasum) is required." >&2
	exit 1
fi
[[ "$actual" == "$expected" ]] || { echo "Checksum mismatch for $asset" >&2; exit 1; }

mkdir "$work/archive"
expected_contents=$'LICENSE\nREADME.txt\ndsmctl'
actual_contents="$(tar -tzf "$work/$asset" | LC_ALL=C sort)"
[[ "$actual_contents" == "$expected_contents" ]] || {
	echo "Release archive contains unexpected files; refusing extraction." >&2
	exit 1
}
tar -xzf "$work/$asset" -C "$work/archive"
[[ -f "$work/archive/dsmctl" ]] || { echo "Archive is missing dsmctl" >&2; exit 1; }

install -d "$prefix"
install -m 0755 "$work/archive/dsmctl" "$prefix/dsmctl"
installed=("$prefix/dsmctl")

printf 'Installed checksum-verified dsmctl %s:\n' "$version"
printf '  %s\n' "${installed[@]}"
case ":$PATH:" in
	*":$prefix:"*) ;;
	*) printf 'Add %s to PATH before invoking dsmctl.\n' "$prefix" ;;
esac
