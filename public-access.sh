#!/usr/bin/env bash
# Optional IPv4 HTTPS access for JunPanel 0.1.1 through the existing Caddy service.
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
detect_ip() {
  local candidate='' line='' client_ip='' client_port='' server_ip='' server_port='' token='' previous=''
  if [[ -n "${JUNPANEL_PUBLIC_IP:-}" ]]; then
    public_ip "$JUNPANEL_PUBLIC_IP" || return 1
    printf '%s\n' "$JUNPANEL_PUBLIC_IP"; return
  fi
  # Preserve a previously configured public entry across installer updates.
  if [[ -f /etc/caddy/junpanel.d/panel-access.caddy && ! -L /etc/caddy/junpanel.d/panel-access.caddy ]]; then
    while IFS= read -r line; do
      if [[ "$line" =~ ^https://([0-9.]+)[[:space:]]\{ ]]; then
        candidate="${BASH_REMATCH[1]}"
        if public_ip "$candidate"; then printf '%s\n' "$candidate"; return; fi
      fi
    done < /etc/caddy/junpanel.d/panel-access.caddy
  fi
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
  local ip="$1" port="$2"
  cat <<CADDY
# JunPanel public HTTPS access - managed by public-access.sh
https://$ip {
    tls {
        issuer acme {
            dir https://acme-v02.api.letsencrypt.org/directory
            profile shortlived
        }
    }
    route {
        @foreign_origin {
            header Origin *
            not header Origin https://$ip
        }
        respond @foreign_origin "Forbidden origin" 403
        @unsafe_origin {
            not method GET HEAD
            not header Origin https://$ip
        }
        respond @unsafe_origin "Origin required" 403
        @websocket_origin {
            header Upgrade websocket
            not header Origin https://$ip
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
  local mode="${1:-}" ip='' port=8787
  die() { printf '\n[JunPanel] %s\n' "$*" >&2; exit 1; }
  if [[ "$mode" == --detect ]]; then
    detect_ip || die '未检测到唯一可用的公网 IPv4；可设置 JUNPANEL_PUBLIC_IP，或显式传入服务器公网 IPv4。'
    exit
  fi
  if [[ "$mode" == --print ]]; then
    ip="${2:-}"; port="${3:-8787}"
    valid_ip "$ip" || die '请输入规范的 IPv4 地址。'
    [[ "$port" =~ ^[0-9]{4,5}$ ]] && ((10#$port >= 1024 && 10#$port <= 65535)) || die '端口需在 1024–65535。'
    render "$ip" "$((10#$port))"
    exit
  fi
  [[ $EUID -eq 0 ]] || die '请使用 sudo bash public-access.sh，或以 root 执行。'
  if [[ "$mode" != --disable ]]; then
    if [[ -n "$mode" ]]; then ip="$mode"; else ip="$(detect_ip)" || die '未能自动识别公网 IPv4，请显式传入服务器公网 IPv4。'; fi
    public_ip "$ip" || die '需要服务器真实的公网 IPv4，不能使用内网、回环或文档示例地址。'
    printf '[JunPanel] 已识别管理入口 IP：%s\n' "$ip"
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
    dpkg --compare-versions "$caddy_version" ge 2.10.1 || die 'IP 公信证书需要 Caddy 2.10.1 或更新版本；此脚本不会替换现有 Caddy。'
    systemctl is-active --quiet junpanel-web.service || die 'JunPanel 网页服务未运行，请先检查安装结果。'
    read -r port < /opt/junpanel/port
    [[ "$port" =~ ^[0-9]{4,5}$ ]] && ((10#$port >= 1024 && 10#$port <= 65535)) || die '已保存的面板端口无效。'
    port=$((10#$port))
    curl --noproxy '*' --fail --silent --show-error --max-time 5 "http://127.0.0.1:$port/api/meta" >/dev/null || die '本机面板尚未就绪。'
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
  local attempt response=''
  for attempt in {1..12}; do
    if response="$(curl --noproxy '*' --fail --silent --show-error --connect-timeout 3 --max-time 4 "https://$ip/api/meta" 2>/dev/null)" && [[ "$response" == *'"version":'* ]]; then
      printf '\n  登录链接：https://%s\n  状态：HTTPS 证书与面板响应检查通过\n  账号：使用现有面板账号\n\n以后直接在浏览器打开上方链接，无需 SSH 隧道。\n' "$ip"
      exit
    fi
    ((attempt == 12)) || sleep 1
  done
  printf '\n  登录链接（准备中）：https://%s\n  面板已安装，但 HTTPS 证书或入口尚未就绪，当前不能确认可登录。\n\n公网 80/443 需可达；请查看：sudo journalctl -u caddy -n 80 --no-pager\n' "$ip"
  exit 2
)

if [[ "${BASH_SOURCE[0]:-$0}" == "$0" ]]; then main "$@"; fi
