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
