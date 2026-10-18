#!/usr/bin/env bash
set -Eeuo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
source ./public-access.sh
file_preview="$(bash ./public-access.sh --print 8.8.8.8 8789)"
pipe_preview="$(cat ./public-access.sh | bash -s -- --print 8.8.8.8 8789)"
[[ "$file_preview" == "$pipe_preview" && "$pipe_preview" == *'8.8.8.8'* ]]
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/junpanel-tls-defaults.XXXXXXXX")"
cleanup_test() { [[ -d "$test_dir" && ! -L "$test_dir" && "${test_dir##*/}" == junpanel-tls-defaults.* ]] && rm -rf -- "$test_dir"; }
trap cleanup_test EXIT
printf '# existing\nhttp://127.0.0.1:8080 {\n respond "ready"\n}\nimport /etc/caddy/kids-english.caddy\n' > "$test_dir/root"
rewrite_root "$test_dir/root" 8.8.8.8 enable > "$test_dir/first"
grep -Fxq '    default_sni 8.8.8.8 # JunPanel IP TLS default' "$test_dir/first"
grep -Fxq 'import /etc/caddy/kids-english.caddy' "$test_dir/first"
rewrite_root "$test_dir/first" 8.8.8.8 enable > "$test_dir/repeat"
cmp -s "$test_dir/first" "$test_dir/repeat"
rewrite_root "$test_dir/first" 9.9.9.9 enable > "$test_dir/change"
[[ "$(grep -c 'default_sni' "$test_dir/change")" == 1 ]]
grep -q 'default_sni 9.9.9.9 ' "$test_dir/change"
rewrite_root "$test_dir/change" '' disable > "$test_dir/disabled"
if grep -q 'default_sni' "$test_dir/disabled"; then exit 1; fi
grep -Fxq 'import /etc/caddy/kids-english.caddy' "$test_dir/disabled"
printf '{\n email admin@example.com\n}\nexample.com { respond "ok" }\n' > "$test_dir/global"
rewrite_root "$test_dir/global" 8.8.8.8 enable > "$test_dir/with-global"
[[ "$(head -n 1 "$test_dir/with-global")" == '{' ]]
grep -Fxq ' email admin@example.com' "$test_dir/with-global"
printf '{\n default_sni existing.example.com\n}\n' > "$test_dir/conflict"
if rewrite_root "$test_dir/conflict" 8.8.8.8 enable > "$test_dir/rejected"; then printf 'Conflicting default_sni was overwritten\n' >&2; exit 1; fi
printf '{\n default_sni 8.8.8.8\n}\n' > "$test_dir/same"
rewrite_root "$test_dir/same" 8.8.8.8 enable > "$test_dir/unchanged"
cmp -s "$test_dir/same" "$test_dir/unchanged"
printf 'TLS defaults: existing sites/global options, repeat/change/disable, and conflict preservation PASS\n'
