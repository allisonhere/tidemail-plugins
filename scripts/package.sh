#!/usr/bin/env bash
# Build one plugin for one platform and pack it the way TideMail installs it:
#   scripts/package.sh <id> <goos> <goarch> <outdir>
# The tarball is flat: plugin.toml, the executable named by the manifest's
# command, and the README. Its name is <id>-<version>-<goos>-<goarch>.tar.gz.
set -euo pipefail

id=$1 goos=$2 goarch=$3 out=$4
dir="plugins/$id"
version=$(sed -n 's/^version *= *"\(.*\)"/\1/p' "$dir/plugin.toml" | head -1)
command=$(sed -n 's/^command *= *"\(.*\)"/\1/p' "$dir/plugin.toml" | head -1)
binary=$(basename "$command")
[ -n "$version" ] && [ -n "$binary" ] || { echo "$dir/plugin.toml needs version and command" >&2; exit 1; }

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
(cd "$dir" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -buildvcs=false -trimpath -o "$stage/$binary" .)
cp "$dir/plugin.toml" "$stage/"
[ -f "$dir/README.md" ] && cp "$dir/README.md" "$stage/"

mkdir -p "$out"
tarball="$out/$id-$version-$goos-$goarch.tar.gz"
tar -czf "$tarball" -C "$stage" --owner=0 --group=0 --numeric-owner plugin.toml "$binary" $([ -f "$stage/README.md" ] && echo README.md)
echo "$tarball"
