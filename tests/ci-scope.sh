#!/usr/bin/env bash
# Decides how much CI a pull request needs. A change that only touches documentation, release
# notes, images, the licence, the two pin lines of quick-install.sh or the version string in main.go
# cannot change what the program does, so it gets the short checks; anything else (and every push to main) gets the full set.
#
# Past the short checks, two more switches keep the slow Docker jobs for the changes that can break them:
#   docker=false  every changed file is a document, an image or part of the web interface (web/), so
#                 nothing the Docker tests exercise can have changed;
#   apps=false    no file the app templates depend on changed (the catalogue, guides, settings, backup,
#                 Docker or stack code, dependencies, this workflow or its scripts).
# A push to main always runs everything.
#
# usage: ci-scope.sh <event> <base> <head>   prints light, pin, docker and apps as "name=<bool>"
set -Eeuo pipefail
event="${1:-}" base="${2:-}" head="${3:-}"
light=false pin=false ver=false docker=true apps=true
if [[ "$event" == pull_request && -n "$base" && -n "$head" ]]; then
  mapfile -t files < <(git diff --name-only "$base...$head")
  if ((${#files[@]} > 0)); then
    light=true docker=false apps=false
    for f in "${files[@]}"; do
      case "$f" in
        apps.go | apps_test.go | appguides.go | appsettings*.go | backup.go | docker*.go | stacks.go | go.mod | go.sum | install.sh | .github/workflows/* | tests/*) light=false docker=true apps=true ;;
        main.go) ver=true ;;
        *.go) light=false docker=true ;;
        web/*) light=false ;;
        quick-install.sh) pin=true ;;
        *.md | LICENSE | .github/release-notes/*) ;;
        */*) light=false ;;
        *.png) ;;
        *) light=false ;;
      esac
    done
    if [[ "$ver" == true ]]; then
      # main.go may change only its version string, e.g. var version = "1.5.6".
      while IFS= read -r line; do
        [[ "$line" =~ ^[+-]var\ version\ =\ \"[0-9]+\.[0-9]+\.[0-9]+\"$ ]] || { light=false; docker=true; }
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
echo "docker=$docker"
echo "apps=$apps"
