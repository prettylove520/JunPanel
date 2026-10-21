#!/usr/bin/env bash
# Domain access: name validation, generated config, keeping an existing entry, and (when Caddy is
# installed) a real Caddy proxying to the demo panel with a locally issued certificate.
set -Eeuo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
source ./public-access.sh
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

for good in panel.example.org a.b.co sub.panel.ljaxm.com xn--fiqs8s.example.cn a-b.c-d.io a1.b2.c3d; do
  valid_domain "$good" || fail "valid domain rejected: $good"
done
for bad in localhost panel panel. .panel.com panel..com -panel.com panel-.com Panel.com panel.com/ panel.com:8443 https://panel.com 1.2.3.4 8.8.8.8 panel.123 '*.panel.com' 'pa nel.com' $'panel.com\nimport x' 'panel.com;x' "$(printf 'a%.0s' {1..64}).com" "$(printf 'a.%.0s' {1..130})com"; do
  if valid_domain "$bad"; then fail "invalid domain accepted: $bad"; fi
done
for reserved in panel.local panel.localhost a.internal host.lan x.home a.test a.example a.invalid x.arpa example.com www.example.org a.example.net; do
  reserved_domain "$reserved" || fail "reserved domain not recognised: $reserved"
done
for usable in panel.ljaxm.com panel.example.cn mytest.com; do
  if reserved_domain "$usable"; then fail "ordinary domain treated as reserved: $usable"; fi
done

# The generated entry: every origin rule names the domain, there is no IP-only TLS block, and the
# IP form is unchanged.
domain_cfg="$(render panel.ljaxm.com 8787)"
[[ "$(grep -c 'not header Origin https://panel.ljaxm.com$' <<< "$domain_cfg")" == 3 ]] || fail 'domain origin rules'
grep -q '^https://panel.ljaxm.com {$' <<< "$domain_cfg" || fail 'domain site line'
if grep -Eq 'shortlived|issuer acme|tls \{' <<< "$domain_cfg"; then fail 'domain entry carries the IP certificate settings'; fi
grep -q 'header_down Set-Cookie "^(.\*)\$" "\$1; Secure"' <<< "$domain_cfg" || fail 'domain cookie Secure rule'
grep -q 'header_up Origin http://127.0.0.1:8787' <<< "$domain_cfg" || fail 'domain origin rewrite'
ip_cfg="$(render 8.8.4.4 8787)"
grep -q 'profile shortlived' <<< "$ip_cfg" || fail 'IP entry lost its short-lived certificate profile'
[[ "$(grep -c 'not header Origin https://8.8.4.4$' <<< "$ip_cfg")" == 3 ]] || fail 'IP origin rules'

# An existing entry, domain or IP, is kept by a re-run that names nothing.
work="$(mktemp -d)"; trap 'rm -rf -- "$work"' EXIT
printf '%s\n' "$domain_cfg" > "$work/domain.caddy"
printf '%s\n' "$ip_cfg" > "$work/ip.caddy"
[[ "$(configured_address_from "$work/domain.caddy")" == panel.ljaxm.com ]] || fail 'existing domain entry not detected'
[[ "$(configured_address_from "$work/ip.caddy")" == 8.8.4.4 ]] || fail 'existing IP entry not detected'
printf 'https://10.0.0.5 {\n}\nhttps://-bad- {\n}\n' > "$work/junk.caddy"
if configured_address_from "$work/junk.caddy" >/dev/null; then fail 'private address / malformed name accepted from an existing entry'; fi
ln -s "$work/domain.caddy" "$work/link.caddy"
if configured_address_from "$work/link.caddy" >/dev/null; then fail 'symlinked entry followed'; fi
if configured_address_from "$work/missing.caddy" >/dev/null; then fail 'missing entry reported'; fi
# A domain already served by another site block must be noticed, whatever the spelling.
printf '# comment about panel.ljaxm.com\n{\n    email a@b.c\n}\nblog.ljaxm.com, https://www.ljaxm.com:8443 {\n    respond "x"\n}\n\nhttp://Panel.LJAXM.com {\n    respond "y"\n}\n' > "$work/main.caddy"
printf 'git.ljaxm.com {\n    respond "z"\n}\n' > "$work/other.caddy"
domain_in_use panel.ljaxm.com "$work/main.caddy" "$work/other.caddy" >/dev/null || fail 'http:// spelling and mixed case not noticed'
[[ "$(domain_in_use www.ljaxm.com "$work/main.caddy")" == "$work/main.caddy" ]] || fail 'second address / scheme and port not noticed'
domain_in_use blog.ljaxm.com "$work/main.caddy" >/dev/null || fail 'first address of a list not noticed'
[[ "$(domain_in_use git.ljaxm.com "$work/main.caddy" "$work/other.caddy")" == "$work/other.caddy" ]] || fail 'second file not searched'
if domain_in_use ljaxm.com "$work/main.caddy" "$work/other.caddy" >/dev/null; then fail 'parent domain reported as in use'; fi
if domain_in_use email "$work/main.caddy" >/dev/null; then fail 'global option or comment mistaken for a site'; fi
if domain_in_use panel.ljaxm.com "$work/missing.caddy" "$work/link.caddy" >/dev/null; then fail 'missing / symlinked file read'; fi
# An explicit public IP still wins over an existing entry.
JUNPANEL_PUBLIC_IP=1.1.1.1 detect_ip | grep -qx 1.1.1.1 || fail 'explicit IP did not win over the existing entry'

# Which entries exist after a run: naming one adds or replaces it and keeps the other.
plan() { plan_entries "$@"; }
expect_plan() { local want="$1"; shift; local got; got="$(plan "$@")"; [[ "$got" == "$want" ]] || fail "plan $*: expected '$want', got '$got'"; }
#            expected          cmd    only cur_ip   cur_domain want_ip  want_domain
expect_plan '8.8.4.4 -'        set    0    ''       ''         8.8.4.4  ''
expect_plan '8.8.4.4 p.x.com'  set    0    8.8.4.4  ''         ''       p.x.com
expect_plan '8.8.4.4 p.x.com'  set    0    ''       p.x.com    8.8.4.4  ''
expect_plan '8.8.4.4 p.y.com'  set    0    8.8.4.4  p.x.com    ''       p.y.com
expect_plan '1.1.1.1 p.x.com'  set    0    8.8.4.4  p.x.com    1.1.1.1  ''
expect_plan '8.8.4.4 p.x.com'  set    0    8.8.4.4  p.x.com    ''       ''
expect_plan '8.8.4.4 p.x.com'  set    0    8.8.4.4  p.x.com    ''       p.x.com
expect_plan '- p.x.com'        set    1    8.8.4.4  ''         ''       p.x.com
expect_plan '8.8.4.4 -'        set    1    ''       p.x.com    8.8.4.4  ''
expect_plan '8.8.4.4 p.y.com'  set    1    8.8.4.4  p.x.com    8.8.4.4  p.y.com
expect_plan '8.8.4.4 -'        remove-domain 0 8.8.4.4 p.x.com  ''       ''
expect_plan '- p.x.com'        remove-ip     0 8.8.4.4 p.x.com  ''       ''
expect_plan '- -'              disable       0 8.8.4.4 p.x.com  ''       ''
expect_plan '- -'              remove-domain 0 ''      p.x.com  ''       ''

# Both entries in one file: the domain first, one header, each block names its own host.
both_cfg="$(render_all panel.ljaxm.com 8.8.4.4 8787)"
[[ "$(grep -c '^# JunPanel public HTTPS access' <<< "$both_cfg")" == 1 ]] || fail 'header repeated or missing'
[[ "$(grep -n '^https://' <<< "$both_cfg" | cut -d: -f2- | tr '\n' '|')" == 'https://panel.ljaxm.com {|https://8.8.4.4 {|' ]] || fail 'entries not domain-then-IP'
[[ "$(grep -c 'not header Origin https://panel.ljaxm.com$' <<< "$both_cfg")" == 3 && "$(grep -c 'not header Origin https://8.8.4.4$' <<< "$both_cfg")" == 3 ]] || fail 'origin rules of the two entries'
[[ "$(grep -c 'profile shortlived' <<< "$both_cfg")" == 1 ]] || fail 'only the IP entry carries the short-lived profile'
[[ "$(render_all '' 8.8.4.4 8787)" == "$ip_cfg" ]] || fail 'IP-only file differs from the single-entry output'
[[ "$(render_all panel.ljaxm.com '' 8787)" == "$domain_cfg" ]] || fail 'domain-only file differs from the single-entry output'
printf '%s\n' "$both_cfg" > "$work/both.caddy"
[[ "$(configured_entries_from "$work/both.caddy" | tr '\n' '|')" == 'domain panel.ljaxm.com|ip 8.8.4.4|' ]] || fail 'both entries not read back'
[[ "$(configured_address_from "$work/both.caddy")" == panel.ljaxm.com ]] || fail 'primary address of a two-entry file'

# The script's own argument handling, before it needs root or Caddy.
out="$(bash public-access.sh --print Panel.LJAXM.com 8787)"; grep -q '^https://panel.ljaxm.com {$' <<< "$out" || fail '--print did not lower-case the name'
if bash public-access.sh --print 'bad name' 8787 >/dev/null 2>&1; then fail '--print accepted a bad name'; fi
printf 'Domain access: name validation, generated entry, IP entry unchanged, existing entry kept PASS\n'

# Everything below needs a real Caddy.
if ! command -v caddy >/dev/null; then
  [[ "${JUNPANEL_CADDY_TEST:-}" != 1 ]] || fail 'JUNPANEL_CADDY_TEST=1 but caddy is not installed'
  printf 'Caddy not installed: skipped the proxy test\n'; exit 0
fi
command -v go >/dev/null || fail 'go is needed to build the demo panel'
panel_port=18601; https_port=18643
go build -o "$work/jp" .
"$work/jp" server --demo --port "$panel_port" >"$work/panel.log" 2>&1 & panel=$!
render panel.ljaxm.com "$panel_port" > "$work/panel-access.caddy"
cat > "$work/Caddyfile" <<CADDYFILE
{
    local_certs
    skip_install_trust
    admin off
    http_port 18680
    https_port $https_port
}
import $work/panel-access.caddy
CADDYFILE
caddy validate --config "$work/Caddyfile" --adapter caddyfile >/dev/null 2>&1 || fail 'caddy rejects the generated entry'
XDG_DATA_HOME="$work/data" XDG_CONFIG_HOME="$work/cfg" caddy run --config "$work/Caddyfile" --adapter caddyfile >"$work/caddy.log" 2>&1 & proxy=$!
trap 'kill "$panel" "$proxy" 2>/dev/null || true; rm -rf -- "$work"' EXIT
root="$work/data/caddy/pki/authorities/local/root.crt"
site=(curl --noproxy '*' --silent --show-error --max-time 5 --resolve "panel.ljaxm.com:$https_port:127.0.0.1" --cacert "$root")
url="https://panel.ljaxm.com:$https_port"
# A browser on port 443 sends an Origin without a port; the test listens elsewhere, so say it explicitly.
origin=https://panel.ljaxm.com
ready=0
for _ in {1..40}; do
  if [[ -f "$root" ]] && "${site[@]}" --fail "$url/api/meta" >"$work/meta.json" 2>/dev/null; then ready=1; break; fi
  sleep 0.5
done
((ready)) || { cat "$work/caddy.log" >&2; fail 'the domain entry did not come up over HTTPS with a valid certificate'; }
grep -q '"version"' "$work/meta.json" || fail 'panel did not answer through the domain entry'
page="$("${site[@]}" "$url/")" || fail 'page request through the domain entry failed'
grep -q '<meta name="junpanel-version"' <<< "$page" || fail 'page not served through the domain entry'
login='{"user":"demo","password":"JunPanelPreview2026!"}'
code="$("${site[@]}" -o "$work/login.out" -D "$work/login.hdr" -w '%{http_code}' -H 'Content-Type: application/json' -H "Origin: $origin" -d "$login" "$url/api/login")"
[[ "$code" == 200 ]] || fail "login through the domain entry: $code"
grep -i '^set-cookie:' "$work/login.hdr" | grep -qi 'secure' || fail 'session cookie is not Secure'
code="$("${site[@]}" -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -H 'Origin: https://evil.invalid' -d "$login" "$url/api/login")"
[[ "$code" == 403 ]] || fail "foreign Origin on a write accepted: $code"
code="$("${site[@]}" -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d "$login" "$url/api/login")"
[[ "$code" == 403 ]] || fail "write without Origin accepted: $code"
code="$("${site[@]}" -o /dev/null -w '%{http_code}' -H 'Origin: https://evil.invalid' "$url/api/meta")"
[[ "$code" == 403 ]] || fail "foreign Origin on a read accepted: $code"
# Nothing answers on the bare IP any more: there is no certificate for it.
if curl --noproxy '*' --silent --max-time 5 --insecure -o /dev/null "https://127.0.0.1:$https_port/api/meta"; then fail 'the IP address still answers'; fi
printf 'Domain entry through a real Caddy: valid certificate, panel reachable, Secure cookie, foreign/missing Origin refused, no IP access PASS\n'

# Both entries at once, in one Caddy: the domain and the IP each work and refuse the other's origin.
kill "$proxy" 2>/dev/null || true; wait "$proxy" 2>/dev/null || true
render_all panel.ljaxm.com 8.8.4.4 "$panel_port" > "$work/both.caddy"
# Caddy older than 2.10 has no short-lived profile; drop the IP entry's own tls settings for this test.
awk 'skip && /^    }$/ {skip=0; next} /^    tls \{$/ {skip=1} !skip' "$work/both.caddy" > "$work/both-local.caddy"
if grep -q 'shortlived' "$work/both-local.caddy"; then fail 'test helper did not strip the IP certificate settings'; fi
cat > "$work/Caddyfile.both" <<CADDYFILE
{
    local_certs
    skip_install_trust
    admin off
    default_sni 8.8.4.4
    http_port 18681
    https_port 18644
}
import $work/both-local.caddy
CADDYFILE
caddy validate --config "$work/Caddyfile.both" --adapter caddyfile >/dev/null 2>&1 || fail 'caddy rejects the two-entry file'
XDG_DATA_HOME="$work/data2" XDG_CONFIG_HOME="$work/cfg2" caddy run --config "$work/Caddyfile.both" --adapter caddyfile >"$work/caddy2.log" 2>&1 & proxy=$!
trap 'kill "$panel" "$proxy" 2>/dev/null || true; rm -rf -- "$work"' EXIT
root2="$work/data2/caddy/pki/authorities/local/root.crt"
byname=(curl --noproxy '*' --silent --show-error --max-time 5 --resolve "panel.ljaxm.com:18644:127.0.0.1" --cacert "$root2")
byip=(curl --noproxy '*' --silent --show-error --max-time 5 --resolve "8.8.4.4:18644:127.0.0.1" --cacert "$root2")
ready=0
for _ in {1..40}; do
  if [[ -f "$root2" ]] && "${byname[@]}" --fail -o /dev/null "https://panel.ljaxm.com:18644/api/meta" 2>/dev/null && "${byip[@]}" --fail -o /dev/null "https://8.8.4.4:18644/api/meta" 2>/dev/null; then ready=1; break; fi
  sleep 0.5
done
((ready)) || { cat "$work/caddy2.log" >&2; fail 'the two entries did not both come up over HTTPS with valid certificates'; }
for pair in "byname|https://panel.ljaxm.com|https://8.8.4.4" "byip|https://8.8.4.4|https://panel.ljaxm.com"; do
  IFS='|' read -r via origin other <<< "$pair"
  declare -n client="$via"
  url="$origin:18644"
  code="$("${client[@]}" -o /dev/null -D "$work/hdr" -w '%{http_code}' -H 'Content-Type: application/json' -H "Origin: $origin" -d "$login" "$url/api/login")"
  [[ "$code" == 200 ]] || fail "login through $origin: $code"
  grep -i '^set-cookie:' "$work/hdr" | grep -qi secure || fail "cookie not Secure through $origin"
  code="$("${client[@]}" -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -H "Origin: $other" -d "$login" "$url/api/login")"
  [[ "$code" == 403 ]] || fail "$origin accepted the other entry's origin: $code"
  code="$("${client[@]}" -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d "$login" "$url/api/login")"
  [[ "$code" == 403 ]] || fail "$origin accepted a write without Origin: $code"
done
printf 'Domain and IP entries together in one Caddy: both valid over HTTPS, Secure cookie, each refuses the other origin and a missing origin PASS\n'
