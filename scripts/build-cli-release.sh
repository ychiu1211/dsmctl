#!/usr/bin/env bash
set -euo pipefail

usage() {
	echo "usage: build-cli-release.sh VERSION OUTPUT_DIR" >&2
	exit 2
}

[[ $# -eq 2 ]] || usage
version="$1"
output_dir="$2"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-[1-9][0-9]*$ ]] || {
	echo "Version must be DSM_MAJOR.DSM_MINOR.DSM_PATCH-DSMCTL_BUILD" >&2
	exit 2
}

for command in go; do
	command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
source_epoch="${SOURCE_DATE_EPOCH:-0}"
revision="${REVISION:-$(git -C "$repo_root" rev-parse HEAD 2>/dev/null || echo unknown)}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$output_dir"
output_dir="$(cd "$output_dir" && pwd)"

source_version="$(cd "$repo_root" && go run ./cmd/dsmctl --version | awk '{print $NF}')"
[[ "$source_version" == "$version" ]] || {
	echo "Requested version $version does not match source version $source_version" >&2
	exit 1
}

ldflags="-s -w -buildid= -X github.com/derekvery666/dsmctl/internal/buildinfo.Version=$version -X github.com/derekvery666/dsmctl/internal/buildinfo.Revision=$revision"

build_target() {
	local goos="$1"
	local goarch="$2"
	local format="$3"
	local archive="$4"
	local suffix=""
	if [[ "$goos" == "windows" ]]; then
		suffix=".exe"
	fi

	local stage="$work/$goos-$goarch"
	mkdir -p "$stage"
	cp "$repo_root/LICENSE" "$stage/LICENSE"
	cp "$repo_root/deploy/release/README.txt" "$stage/README.txt"
	(
		cd "$repo_root"
		CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
			-trimpath -buildvcs=false -ldflags="$ldflags" \
			-o "$stage/dsmctl$suffix" ./cmd/dsmctl
	)
	if [[ "$goos" != "windows" ]]; then
		chmod 0755 "$stage/dsmctl$suffix"
	fi
	go version -m "$stage/dsmctl$suffix" | grep -Fq 'github.com/derekvery666/dsmctl'

	(
		cd "$repo_root"
		go run ./scripts/release_archive.go \
			-format "$format" \
			-root "$stage" \
			-output "$output_dir/$archive" \
			-epoch "$source_epoch"
	)
}

build_target linux amd64 tar.gz dsmctl-linux-amd64.tar.gz
build_target windows amd64 zip dsmctl-windows-amd64.zip

printf 'Built CLI release archives for dsmctl %s in %s\n' "$version" "$output_dir"
