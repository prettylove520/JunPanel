#!/usr/bin/env bash
# Build the Linux release files into dist/ (override with DIST_DIR). Used by the
# Release workflow and runnable locally: bash scripts/package-release.sh 0.1.5
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
fail() { printf '[打包] %s\n' "$*" >&2; exit 1; }
version="${1:-}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail '用法：bash scripts/package-release.sh 版本号（例如 0.1.5）'
grep -Fxq "var version = \"$version\"" main.go || fail "main.go 中的 version 不是 $version，请先更新版本号"
[[ -z "$(git status --porcelain --untracked-files=no)" ]] || fail '存在未提交的改动，请先提交'
dist="${DIST_DIR:-dist}"
mtime="${RELEASE_MTIME:-$(git log -1 --format=%cI)}"
archive="JunPanel-$version-linux.tar.gz"
source="JunPanel-$version-source.zip"
sums="JunPanel-$version-SHA256SUMS.txt"

bash build.sh
rm -rf -- "$dist"
mkdir -p -- "$dist"
stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT
mkdir -p "$stage/junpanel/bin"
# The online installer pins this archive's hash, so it is not part of the archive.
git ls-files -z -- . ':!quick-install.sh' | xargs -0 cp --parents -t "$stage/junpanel"
cp bin/junpanel-linux-amd64 bin/junpanel-linux-arm64 bin/SHA256SUMS "$stage/junpanel/bin/"
find "$stage/junpanel" -type d -exec chmod 755 {} +
find "$stage/junpanel" -type f -exec chmod 644 {} +
find "$stage/junpanel" -type f \( -name '*.sh' -o -name 'junpanel-linux-*' \) -exec chmod 755 {} +
tar --sort=name --mtime="$mtime" --owner=0 --group=0 --numeric-owner --format=gnu -C "$stage" -cf - junpanel | gzip -n -9 > "$dist/$archive"
git archive --format=zip --prefix="JunPanel-$version/" -o "$dist/$source" HEAD
(cd "$dist" && sha256sum "$archive" "$source" > "$sums" && cat "$sums")
