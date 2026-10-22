#!/usr/bin/env bash
# Checks that the version and SHA-256 pinned in quick-install.sh are those of the published
# release: the checksum must match the downloaded package and the release's SHA256SUMS.
set -Eeuo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
repo="${GITHUB_REPOSITORY:-prettylove520/JunPanel}"
version="$(sed -n 's/^  local version=\([0-9][0-9.]*\)$/\1/p' quick-install.sh)"
expected="$(sed -n 's/^  local expected=\([0-9a-f]\{64\}\)$/\1/p' quick-install.sh)"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && "$expected" =~ ^[0-9a-f]{64}$ ]] || { echo "pin lines missing or malformed" >&2; exit 1; }
base="https://github.com/$repo/releases/download/v$version"
dir="$(mktemp -d)"
trap 'rm -rf -- "$dir"' EXIT
curl -fsSL -o "$dir/package.tgz" "$base/JunPanel-$version-linux.tar.gz"
curl -fsSL -o "$dir/SHA256SUMS" "$base/JunPanel-$version-SHA256SUMS.txt"
actual="$(sha256sum "$dir/package.tgz" | cut -d' ' -f1)"
listed="$(awk -v f="JunPanel-$version-linux.tar.gz" '$2 == f {print $1}' "$dir/SHA256SUMS")"
echo "version $version"
echo "pinned  $expected"
echo "package $actual"
echo "listed  $listed"
[[ "$actual" == "$expected" && "$listed" == "$expected" ]] || { echo "pinned checksum does not match the published release" >&2; exit 1; }
echo "pin verified"
