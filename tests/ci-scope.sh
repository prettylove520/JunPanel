#!/usr/bin/env bash
# Decides how much CI a pull request needs. A change that only touches documentation, release
# notes, images, the licence, the two pin lines of quick-install.sh or the version string in main.go
# cannot change what the program does, so it gets the short checks; anything else (and every push to main) gets the full set.
#
# usage: ci-scope.sh <event> <base> <head>   prints "light=<bool>" and "pin=<bool>"
set -Eeuo pipefail
event="${1:-}" base="${2:-}" head="${3:-}"
light=false pin=false ver=false
if [[ "$event" == pull_request && -n "$base" && -n "$head" ]]; then
  mapfile -t files < <(git diff --name-only "$base...$head")
  if ((${#files[@]} > 0)); then
    light=true
    for f in "${files[@]}"; do
      case "$f" in
        quick-install.sh) pin=true ;;
        main.go) ver=true ;;
        *.md | LICENSE | .github/release-notes/*) ;;
        */*) light=false ;;
        *.png) ;;
        *) light=false ;;
      esac
    done
    if [[ "$ver" == true ]]; then
      # main.go may change only its version string, e.g. var version = "1.5.6".
      while IFS= read -r line; do
        [[ "$line" =~ ^[+-]var\ version\ =\ \"[0-9]+\.[0-9]+\.[0-9]+\"$ ]] || light=false
      done < <(git diff -U0 "$base...$head" -- main.go | grep -E '^[+-]' | grep -vE '^(\+\+\+|---) ')
    fi
    if [[ "$pin" == true ]]; then
      # Only the version and checksum lines may differ.
      while IFS= read -r line; do
        [[ "$line" =~ ^[+-]\ \ local\ (version|expected)=[0-9a-f.]+$ ]] || light=false
      done < <(git diff -U0 "$base...$head" -- quick-install.sh | grep -E '^[+-]' | grep -vE '^(\+\+\+|---) ')
    fi
  fi
fi
echo "light=$light"
echo "pin=$pin"
