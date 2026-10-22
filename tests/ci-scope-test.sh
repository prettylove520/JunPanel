#!/usr/bin/env bash
# Exercises ci-scope.sh against small throwaway repositories.
set -Eeuo pipefail
here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"; trap 'rm -rf -- "$tmp"' EXIT
cd "$tmp"
git init -q . && git config user.email t@example.com && git config user.name t
printf '  local version=1.0.0\n  local expected=%064d\n  local public_ref=abc\nmain() { :; }\n' 0 > quick-install.sh
echo "# doc" > README.md; echo "package main" > main.go; mkdir -p .github/release-notes tests
git add -A && git commit -qm base && base="$(git rev-parse HEAD)"
fail=0
check() { # name, expected-light, expected-pin
  local out; out="$(bash "$here/ci-scope.sh" "${EVENT:-pull_request}" "$base" "$(git rev-parse HEAD)")"
  if [[ "$out" != $'light='"$2"$'\n'pin="$3" ]]; then echo "FAIL $1: $(echo "$out" | tr '\n' ' ')"; fail=1; else echo "ok   $1"; fi
}
reset() { git reset -q --hard "$base"; }
sed -i 's/^  local version=.*/  local version=1.0.1/; s/^  local expected=.*/  local expected=1111111111111111111111111111111111111111111111111111111111111111/' quick-install.sh
git commit -qam pin; check "pin lines only" true true; reset
echo more >> README.md; echo n > .github/release-notes/v1.md; git add -A; git commit -qm docs; check "docs and release notes" true false; reset
sed -i 's/^main() { :; }/main() { echo changed; }/' quick-install.sh; git commit -qam logic; check "logic change in quick-install.sh" false true; reset
sed -i 's/^  local version=.*/  local version=1.0.1/' quick-install.sh; sed -i 's/^  local public_ref=.*/  local public_ref=zzz/' quick-install.sh; git commit -qam mixed; check "pin plus another line" false true; reset
echo "package main // x" > main.go; sed -i 's/^  local version=.*/  local version=1.0.1/' quick-install.sh; git commit -qam code; check "code and pin" false true; reset
echo y > tests/new.sh; git add -A; git commit -qm test; check "a new test script" false false; reset
git commit -q --allow-empty -m empty; check "no changed files" false false; reset
sed -i 's/^  local version=.*/  local version=1.0.1/' quick-install.sh; git commit -qam pin; EVENT=push check "push to main" false false; reset
exit $fail
