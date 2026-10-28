#!/usr/bin/env bash
# Exercises ci-scope.sh against small throwaway repositories.
set -Eeuo pipefail
here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"; trap 'rm -rf -- "$tmp"' EXIT
# The workflow exports EVENT for the real run; the scenarios use their own SCOPE_EVENT so it cannot leak in.
cd "$tmp"
git init -q . && git config user.email t@example.com && git config user.name t
printf '  local version=1.0.0\n  local expected=%064d\n  local public_ref=abc\nmain() { :; }\n' 0 > quick-install.sh
echo "# doc" > README.md; printf 'package main
var version = "1.0.0"
func f() {}
' > main.go; echo lic > LICENSE; echo img > logo.png; mkdir -p .github/release-notes tests
git add -A && git commit -qm base && base="$(git rev-parse HEAD)"
fail=0
check() { # name, expected light, pin, docker, apps
  local out; out="$(bash "$here/ci-scope.sh" "${SCOPE_EVENT:-pull_request}" "$base" "$(git rev-parse HEAD)")"
  if [[ "$out" != $'light='"$2"$'\n'pin="$3"$'\n'docker="$4"$'\n'apps="$5" ]]; then echo "FAIL $1: $(echo "$out" | tr '\n' ' ')"; fail=1; else echo "ok   $1"; fi
}
reset() { git reset -q --hard "$base"; }
sed -i 's/^  local version=.*/  local version=1.0.1/; s/^  local expected=.*/  local expected=1111111111111111111111111111111111111111111111111111111111111111/' quick-install.sh
git commit -qam pin; check "pin lines only" true true false false; reset
echo more >> README.md; echo n > .github/release-notes/v1.md; git add -A; git commit -qm docs; check "docs and release notes" true false false false; reset
sed -i 's/^main() { :; }/main() { echo changed; }/' quick-install.sh; git commit -qam logic; check "logic change in quick-install.sh" false true false false; reset
sed -i 's/^  local version=.*/  local version=1.0.1/' quick-install.sh; sed -i 's/^  local public_ref=.*/  local public_ref=zzz/' quick-install.sh; git commit -qam mixed; check "pin plus another line" false true false false; reset
echo "package main // x" > main.go; sed -i 's/^  local version=.*/  local version=1.0.1/' quick-install.sh; git commit -qam code; check "code and pin" false true true false; reset
sed -i 's/^var version = .*/var version = "1.0.1"/' main.go; mkdir -p .github/release-notes; echo n > .github/release-notes/v1.md; git add -A; git commit -qm bump; check "version bump with notes" true false false false; reset
sed -i 's/^var version = .*/var version = "1.0.1"/; s/^func f() {}/func f() { println(1) }/' main.go; git commit -qam bumpcode; check "version bump plus a code change" false false true false; reset
sed -i 's/^var version = .*/var version = "latest"/' main.go; git commit -qam badver; check "version set to a non-version" false false true false; reset
echo x >> LICENSE; echo y >> logo.png; git add -A; git commit -qm assets; check "licence and image" true false false false; reset
mkdir -p web; echo z > web/a.png; git add -A; git commit -qm webpng; check "an image inside web/" false false false false; reset
echo y > tests/new.sh; git add -A; git commit -qm test; check "a new test script" false false true true; reset
git commit -q --allow-empty -m empty; check "no changed files" false false true true; reset
sed -i 's/^  local version=.*/  local version=1.0.1/' quick-install.sh; git commit -qam pin; SCOPE_EVENT=push check "push to main" false false true true; reset
mkdir -p web; echo z > web/app.js; git add -A; git commit -qm webjs; check "web interface only" false false false false; reset
echo x >> README.md; mkdir -p web; echo z > web/app.js; git add -A; git commit -qm webdocs; check "web interface and docs" false false false false; reset
echo "package main" > extra.go; git add -A; git commit -qm gofile; check "a Go file" false false true false; reset
echo "package main" > apps.go; git add -A; git commit -qm appsgo; check "the app catalogue" false false true true; reset
exit $fail
