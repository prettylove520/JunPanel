#!/usr/bin/env bash
# Optional HTTPS access for JunPanel through the existing Caddy service: by public IPv4, or by domain name.
set -Eeuo pipefail

valid_ip() {
  local ip="$1" part
  [[ "$ip" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || return 1
  local -a parts
  IFS=. read -ra parts <<< "$ip"
  for part in "${parts[@]}"; do
    [[ "$part" == 0 || "$part" != 0* ]] && ((10#$part <= 255)) || return 1
  done
}
valid_domain() {
  local name="$1"
  ((${#name} <= 253)) || return 1
  [[ "$name" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$ ]] || return 1
  # An all-numeric last label would make this an IP-looking name, not a domain.
  [[ ! "${name##*.}" =~ ^[0-9]+$ ]]
}
# Names no public CA will issue a certificate for.
reserved_domain() {
  case "$1" in
    *.local|*.localhost|*.internal|*.lan|*.home|*.test|*.example|*.invalid|*.arpa|example.com|example.net|example.org|*.example.com|*.example.net|*.example.org) return 0;;
  esac
  return 1
}
public_ip() {
  valid_ip "$1" || return 1
  local a b c d
  IFS=. read -r a b c d <<< "$1"
  ((a != 0 && a != 10 && a != 127 && a < 224)) || return 1
  ! ((a == 100 && b >= 64 && b <= 127)) || return 1
  ! ((a == 169 && b == 254)) || return 1
  ! ((a == 172 && b >= 16 && b <= 31)) || return 1
  ! ((a == 192 && (b == 168 || (b == 0 && (c == 0 || c == 2))))) || return 1
  ! ((a == 198 && (b == 18 || b == 19 || (b == 51 && c == 100)))) || return 1
  ! ((a == 203 && b == 0 && c == 113)) || return 1
}
# Is this name already a site address in one of the given Caddy files? Prints the first file.
# Caddy would not refuse two blocks for one host (it treats "x.com" and "https://x.com" as
# different addresses), it would just let one of them shadow the other.
domain_in_use() {
  local host="$1" file='' line='' token=''
  local -a tokens=()
  shift
  for file in "$@"; do
    [[ -f "$file" && ! -L "$file" ]] || continue
    while IFS= read -r line; do
      [[ "$line" =~ ^[^[:space:]#{}] ]] || continue
      line="${line%%\{*}"
      read -ra tokens <<< "${line//,/ }"
      for token in "${tokens[@]}"; do
        token="${token#https://}"; token="${token#http://}"; token="${token%%[:/]*}"; token="${token,,}"
        if [[ "$token" == "$host" ]]; then printf '%s\n' "$file"; return 0; fi
      done
    done < "$file"
  done
  return 1
}
# The access address already configured: a public IPv4 or a domain name.
configured_address() { configured_address_from /etc/caddy/junpanel.d/panel-access.caddy; }
configured_address_from() {
  local file="$1" line='' candidate=''
  [[ -f "$file" && ! -L "$file" ]] || return 1
  while IFS= read -r line; do
    if [[ "$line" =~ ^https://([0-9a-z.-]+)[[:space:]]\{ ]]; then
      candidate="${BASH_REMATCH[1]}"
      if public_ip "$candidate" || valid_domain "$candidate"; then printf '%s\n' "$candidate"; return; fi
    fi
  done < "$file"
  return 1
}
detect_ip() {
  if [[ -n "${JUNPANEL_PUBLIC_IP:-}" ]]; then
    public_ip "$JUNPANEL_PUBLIC_IP" || return 1
    printf '%s\n' "$JUNPANEL_PUBLIC_IP"; return
  fi
  # Preserve a previously configured public entry across installer updates.
  configured_address && return
  detect_server_ip
}
# This server's own public IPv4, from the SSH connection, the routing table or its interfaces.
detect_server_ip() {
  local candidate='' line='' client_ip='' client_port='' server_ip='' server_port='' token='' previous=''
  read -r client_ip client_port server_ip server_port <<< "${SSH_CONNECTION:-}"
  if public_ip "$server_ip"; then printf '%s\n' "$server_ip"; return; fi
  command -v ip >/dev/null || return 1
  # A route lookup reads the kernel routing table; it does not contact this address.
  line="$(ip -4 route get 1.1.1.1 2>/dev/null)" || line=''
  local -a tokens=()
  read -ra tokens <<< "$line"
  for token in "${tokens[@]}"; do
    if [[ "$previous" == src ]] && public_ip "$token"; then printf '%s\n' "$token"; return; fi
    previous="$token"
  done
  local -a found=()
  while IFS= read -r line; do
    tokens=(); read -ra tokens <<< "$line"; previous=''
    for token in "${tokens[@]}"; do
      if [[ "$previous" == inet ]]; then
        candidate="${token%%/*}"
        if public_ip "$candidate" && [[ " ${found[*]} " != *" $candidate "* ]]; then found+=("$candidate"); fi
      fi
      previous="$token"
    done
  done < <(ip -4 -o address show scope global 2>/dev/null)
  ((${#found[@]} == 1)) || return 1
  printf '%s\n' "${found[0]}"
}
rewrite_root() {
  local file="$1" address="$2" action="$3"
  awk -v address="$address" -v action="$action" '
    { lines[NR]=$0
      if ($0 ~ /^[[:space:]]*default_sni[[:space:]]+/) {
        if ($0 ~ /# JunPanel IP TLS default[[:space:]]*$/) owned=1
        else { split($0, fields, /[[:space:]]+/); value=$0; sub(/^[[:space:]]*default_sni[[:space:]]+/, "", value); sub(/[[:space:]#].*$/, "", value); if (value != address) conflict=1; else existing=1 }
      }
    }
    END {
      if (action == "enable" && conflict) exit 42
      insert=(action == "enable" && !owned && !existing)
      for (i=1; i<=NR; i++) {
        line=lines[i]
        if (insert && line !~ /^[[:space:]]*(#.*)?$/) {
          if (line ~ /^[[:space:]]*\{[[:space:]]*(#.*)?$/) {
            print line; print "    default_sni " address " # JunPanel IP TLS default"; insert=0; continue
          }
          print "{"; print "    default_sni " address " # JunPanel IP TLS default"; print "}"; print ""; insert=0
        }
        if (line ~ /^[[:space:]]*default_sni[[:space:]]+/ && line ~ /# JunPanel IP TLS default[[:space:]]*$/) {
          if (action == "enable") print "    default_sni " address " # JunPanel IP TLS default"
        } else print line
      }
      if (insert) { print "{"; print "    default_sni " address " # JunPanel IP TLS default"; print "}" }
    }
  ' "$file"
}
render() {
  local host="$1" port="$2" tls=''
  if public_ip "$host" || ! valid_domain "$host"; then
    tls='    tls {
        issuer acme {
            dir https://acme-v02.api.letsencrypt.org/directory
            profile shortlived
        }
    }
'
  fi
  cat <<CADDY
# JunPanel public HTTPS access - managed by public-access.sh
https://$host {
${tls}    route {
        @foreign_origin {
            header Origin *
            not header Origin https://$host
        }
        respond @foreign_origin "Forbidden origin" 403
        @unsafe_origin {
            not method GET HEAD
            not header Origin https://$host
        }
        respond @unsafe_origin "Origin required" 403
        @websocket_origin {
            header Upgrade websocket
            not header Origin https://$host
        }
        respond @websocket_origin "WebSocket origin required" 403
        reverse_proxy 127.0.0.1:$port {
            # Validate the public Origin above before adapting to the 0.1.1 local API.
            header_up Host 127.0.0.1:$port
            header_up Origin http://127.0.0.1:$port
            header_down Set-Cookie "^(.*)\$" "\$1; Secure"
        }
    }
}
CADDY
}
main() (
  set -Eeuo pipefail
  umask 027
  export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
  local mode="${1:-}" ip='' port=8787 kind=ip want=any
  die() { printf '\n[JunPanel] %s\n' "$*" >&2; exit 1; }
  # --domain NAME only accepts a domain; a bare argument may be either an IP or a domain.
  if [[ "$mode" == --domain ]]; then want=domain; mode="${2:-}"; [[ -n "$mode" ]] || die '用法：public-access.sh --domain 你的域名，例如 panel.example.com'; fi
  [[ "$mode" == -* ]] || { mode="${mode,,}"; mode="${mode%.}"; }
  if [[ "$mode" == --detect ]]; then
    detect_ip || die '未检测到唯一可用的公网 IPv4；可设置 JUNPANEL_PUBLIC_IP，或显式传入服务器公网 IPv4。'
    exit
  fi
  if [[ "$mode" == --print ]]; then
    ip="${2:-}"; port="${3:-8787}"; ip="${ip,,}"
    valid_ip "$ip" || valid_domain "$ip" || die '请输入规范的 IPv4 地址或域名。'
    [[ "$port" =~ ^[0-9]{4,5}$ ]] && ((10#$port >= 1024 && 10#$port <= 65535)) || die '端口需在 1024–65535。'
    render "$ip" "$((10#$port))"
    exit
  fi
  [[ $EUID -eq 0 ]] || die '请使用 sudo bash public-access.sh，或以 root 执行。'
  if [[ "$mode" != --disable ]]; then
    if [[ -n "$mode" ]]; then ip="$mode"; else ip="$(detect_ip)" || die '未能自动识别公网 IPv4，请显式传入服务器公网 IPv4，或用 --domain 指定域名。'; fi
    if valid_domain "$ip"; then
      kind=domain
      reserved_domain "$ip" && die "$ip 是保留或示例域名，公共证书机构不会为它签发证书，请使用你自己的域名。"
    else
      [[ "$want" == any ]] || die "“$ip”不是有效的域名。请填写小写字母、数字和连字符组成的域名，例如 panel.example.com；中文域名请先转换为 Punycode，不要带 https:// 或端口。"
      valid_ip "$ip" || die "“$ip”既不是公网 IPv4，也不是有效的域名。"
      public_ip "$ip" || die '需要服务器真实的公网 IPv4，不能使用内网、回环或文档示例地址。'
    fi
    if [[ "$kind" == domain ]]; then printf '[JunPanel] 管理入口域名：%s\n' "$ip"; else printf '[JunPanel] 已识别管理入口 IP：%s\n' "$ip"; fi
  fi
  for command in systemctl runuser mktemp chmod grep cp mv rm dpkg awk cmp; do command -v "$command" >/dev/null || die "缺少系统工具：$command"; done
  [[ -f /opt/junpanel/.junpanel-install && ! -L /opt/junpanel && -f /opt/junpanel/port && ! -L /opt/junpanel/port ]] || die '请先完成 JunPanel 安装。'
  [[ -d /etc/caddy/junpanel.d && ! -L /etc/caddy/junpanel.d && -f /etc/caddy/Caddyfile && ! -L /etc/caddy/Caddyfile ]] || die 'Caddy 配置目录异常。'
  grep -Fxq 'import /etc/caddy/junpanel.d/*.caddy' /etc/caddy/Caddyfile || die '主 Caddyfile 缺少 JunPanel import，请先完成安装。'
  [[ -x /usr/bin/caddy ]] || die '未找到 /usr/bin/caddy。'
  systemctl is-active --quiet caddy.service || die 'Caddy 服务未运行。'
  local target=/etc/caddy/junpanel.d/panel-access.caddy backup='' temp='' changed=0 had_previous=0
  local root_backup='' root_temp='' root_action=enable
  [[ ! -L "$target" ]] || die '入口配置是符号链接，拒绝覆盖。'
  if [[ -e "$target" ]]; then
    [[ -f "$target" ]] && grep -Fxq '# JunPanel public HTTPS access - managed by public-access.sh' "$target" || die '同名配置不属于 JunPanel，拒绝覆盖。'
    had_previous=1
  fi
  if [[ "$mode" != --disable ]]; then
    command -v curl >/dev/null || die '缺少 curl。'
    local caddy_version
    caddy_version="$(/usr/bin/caddy version)"; caddy_version="${caddy_version%% *}"; caddy_version="${caddy_version#v}"
    if [[ "$kind" == ip ]]; then dpkg --compare-versions "$caddy_version" ge 2.10.1 || die 'IP 公信证书需要 Caddy 2.10.1 或更新版本；此脚本不会替换现有 Caddy。'; fi
    systemctl is-active --quiet junpanel-web.service || die 'JunPanel 网页服务未运行，请先检查安装结果。'
    read -r port < /opt/junpanel/port
    [[ "$port" =~ ^[0-9]{4,5}$ ]] && ((10#$port >= 1024 && 10#$port <= 65535)) || die '已保存的面板端口无效。'
    port=$((10#$port))
    curl --noproxy '*' --fail --silent --show-error --max-time 5 "http://127.0.0.1:$port/api/meta" >/dev/null || die '本机面板尚未就绪。'
    if [[ "$kind" == domain ]]; then
      local used='' fragment=''
      local -a others=()
      for fragment in /etc/caddy/junpanel.d/*.caddy; do [[ "$fragment" == "$target" ]] || others+=("$fragment"); done
      used="$(domain_in_use "$ip" /etc/caddy/Caddyfile "${others[@]}")" && die "域名 $ip 已经在 $used 中配置为另一个站点，不能再作为面板入口。请换一个域名（例如它的子域名），或先把那个站点移除。未做任何更改。"
    fi
    if [[ "$kind" == domain && "${JUNPANEL_SKIP_DNS_CHECK:-}" != 1 ]]; then
      # Check DNS before touching anything: switching to a domain that does not point here would
      # remove the working IP entry and leave no public way in until it is fixed.
      local resolved='' server='' v6=''
      resolved="$(getent ahostsv4 "$ip" 2>/dev/null | awk '{print $1}' | sort -u)" || resolved=''
      [[ -n "$resolved" ]] || die "域名 $ip 还没有解析到任何 IPv4 地址。请先在域名服务商处添加一条 A 记录指向这台服务器，等待生效后重试（可用 getent hosts $ip 查看）。未做任何更改。"
      if server="$(detect_server_ip)"; then
        grep -Fxq -- "$server" <<< "$resolved" || die "域名 $ip 当前解析到 $(tr '\n' ' ' <<< "$resolved" | sed 's/ $//')，不是这台服务器的 $server。请把 A 记录改为 $server 并等待生效；如果使用了 Cloudflare 等代理，请先改为“仅 DNS”。未做任何更改。若确认解析无误（例如服务器在 NAT 之后），可设置 JUNPANEL_SKIP_DNS_CHECK=1 后重试。"
      else
        printf '[JunPanel] 无法确认这台服务器的公网 IP，已跳过解析比对；域名解析到：%s\n' "$(tr '\n' ' ' <<< "$resolved" | sed 's/ $//')"
      fi
      v6="$(getent ahostsv6 "$ip" 2>/dev/null | awk '$1 !~ /^::ffff:/ {print $1}' | sort -u | head -1)" || v6=''
      [[ -z "$v6" ]] || printf '[JunPanel] 注意：域名还有 AAAA 记录（%s）。证书机构可能通过 IPv6 验证，请确认这台服务器的 IPv6 也能到达 Caddy，否则请删除 AAAA 记录。\n' "$v6"
    fi
  elif ((had_previous == 0)) && ! grep -Eq '^[[:space:]]*default_sni[[:space:]].*# JunPanel IP TLS default[[:space:]]*$' /etc/caddy/Caddyfile; then
    printf '[JunPanel] 未配置公网直连，无需更改。\n'; exit
  fi
  cleanup() {
    local code=$?
    trap - EXIT
    if ((code && changed)); then
      printf '\n[JunPanel] 配置失败，正在恢复原入口。\n' >&2
      if ((had_previous)); then cp -p -- "$backup" "$target"; else rm -f -- "$target"; fi
      if [[ -n "$root_backup" && -f "$root_backup" ]]; then cp -p -- "$root_backup" /etc/caddy/Caddyfile; fi
      if ! runuser -u caddy -- /usr/bin/caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1 || ! systemctl reload caddy.service; then
        printf '[JunPanel] Caddy 恢复未完成，请通过 SSH 检查服务日志。\n' >&2
      fi
    fi
    [[ -z "$backup" ]] || rm -f -- "$backup"
    [[ -z "$temp" ]] || rm -f -- "$temp"
    [[ -z "$root_backup" ]] || rm -f -- "$root_backup"
    [[ -z "$root_temp" ]] || rm -f -- "$root_temp"
    exit "$code"
  }
  trap cleanup EXIT
  root_backup="$(mktemp /etc/caddy/.junpanel-root-old.XXXXXXXX)"
  cp -p -- /etc/caddy/Caddyfile "$root_backup"
  if ((had_previous)); then backup="$(mktemp /etc/caddy/.junpanel-public-old.XXXXXXXX)"; cp -p -- "$target" "$backup"; fi
  if [[ "$mode" == --disable ]]; then
    root_action=disable; changed=1; rm -f -- "$target"
  else
    # Only the IP certificate needs a default server name; a domain does not, so remove ours.
    [[ "$kind" == ip ]] || root_action=disable
    temp="$(mktemp /etc/caddy/.junpanel-public-new.XXXXXXXX)"
    render "$ip" "$port" > "$temp"
    chmod 0644 "$temp"
    changed=1; mv -- "$temp" "$target"; temp=''
  fi
  root_temp="$(mktemp /etc/caddy/.junpanel-root-new.XXXXXXXX)"
  cp -p -- /etc/caddy/Caddyfile "$root_temp"
  rewrite_root /etc/caddy/Caddyfile "$ip" "$root_action" > "$root_temp" || die '主配置已有不同的 default_sni，已保留原设置；请先人工核对。'
  runuser -u caddy -- /usr/bin/caddy validate --config "$root_temp" --adapter caddyfile
  if ! cmp -s -- /etc/caddy/Caddyfile "$root_temp"; then mv -- "$root_temp" /etc/caddy/Caddyfile; root_temp=''; fi
  systemctl reload caddy.service
  changed=0
  if [[ "$mode" == --disable ]]; then
    printf '\n[JunPanel] 公网直连已关闭，仍可使用原有 SSH 隧道访问。\n'; exit
  fi
  printf '\n[JunPanel] 正在准备 HTTPS 登录入口，首次签发证书最多等待约一分钟…\n'
  local attempt response='' tries=12 pause=1
  local -a resolve=()
  # For a domain, ask this machine's Caddy directly, so the check does not depend on DNS caching
  # or on reaching our own public address from inside.
  if [[ "$kind" == domain ]]; then tries=30; pause=2; resolve=(--resolve "$ip:443:127.0.0.1"); fi
  for ((attempt = 1; attempt <= tries; attempt++)); do
    if response="$(curl --noproxy '*' "${resolve[@]}" --fail --silent --show-error --connect-timeout 3 --max-time 4 "https://$ip/api/meta" 2>/dev/null)" && [[ "$response" == *'"version":'* ]]; then
      printf '\n  登录链接：https://%s\n  状态：HTTPS 证书与面板响应检查通过\n  账号：使用现有面板账号\n\n以后直接在浏览器打开上方链接，无需 SSH 隧道。\n' "$ip"
      if [[ "$kind" == domain ]]; then printf '  原来的 IP 入口已停用，只能通过这个域名访问。\n'; fi
      exit
    fi
    ((attempt == tries)) || sleep "$pause"
  done
  printf '\n  登录链接（准备中）：https://%s\n  面板已安装，但 HTTPS 证书或入口尚未就绪，当前不能确认可登录。\n\n公网 80/443 需可达；请查看：sudo journalctl -u caddy -n 80 --no-pager\n' "$ip"
  if [[ "$kind" == domain ]]; then printf '如需改回 IP 入口：重新执行本脚本并传入服务器公网 IPv4。\n'; fi
  exit 2
)

if [[ "${BASH_SOURCE[0]:-$0}" == "$0" ]]; then main "$@"; fi
