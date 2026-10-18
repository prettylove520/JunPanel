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
  if [[ "$mode" == --print ]]; then
    ip="${2:-}"; port="${3:-8787}"
    valid_ip "$ip" || die '请输入规范的 IPv4 地址。'
    [[ "$port" =~ ^[0-9]{4,5}$ ]] && ((10#$port >= 1024 && 10#$port <= 65535)) || die '端口需在 1024–65535。'
    render "$ip" "$((10#$port))"
    exit
  fi
  [[ $EUID -eq 0 ]] || die '请使用 sudo bash public-access.sh 公网IPv4，或以 root 执行。'
  [[ "$mode" == --disable ]] || { ip="$mode"; valid_ip "$ip" || die '用法：sudo bash public-access.sh 服务器公网IPv4（关闭直连使用 --disable）。'; }
  for command in systemctl runuser mktemp chmod grep cp mv rm dpkg; do command -v "$command" >/dev/null || die "缺少系统工具：$command"; done
  [[ -f /opt/junpanel/.junpanel-install && ! -L /opt/junpanel && -f /opt/junpanel/port && ! -L /opt/junpanel/port ]] || die '请先完成 JunPanel 安装。'
  [[ -d /etc/caddy/junpanel.d && ! -L /etc/caddy/junpanel.d && -f /etc/caddy/Caddyfile && ! -L /etc/caddy/Caddyfile ]] || die 'Caddy 配置目录异常。'
  grep -Fxq 'import /etc/caddy/junpanel.d/*.caddy' /etc/caddy/Caddyfile || die '主 Caddyfile 缺少 JunPanel import，请先完成安装。'
  [[ -x /usr/bin/caddy ]] || die '未找到 /usr/bin/caddy。'
  systemctl is-active --quiet caddy.service || die 'Caddy 服务未运行。'
  local target=/etc/caddy/junpanel.d/panel-access.caddy backup='' temp='' changed=0 had_previous=0
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
  elif ((had_previous == 0)); then
    printf '[JunPanel] 未配置公网直连，无需更改。\n'; exit
  fi
  cleanup() {
    local code=$?
    trap - EXIT
    if ((code && changed)); then
      printf '\n[JunPanel] 配置失败，正在恢复原入口。\n' >&2
      if ((had_previous)); then cp -p -- "$backup" "$target"; else rm -f -- "$target"; fi
      if ! runuser -u caddy -- /usr/bin/caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1 || ! systemctl reload caddy.service; then
        printf '[JunPanel] Caddy 恢复未完成，请通过 SSH 检查服务日志。\n' >&2
      fi
    fi
    [[ -z "$backup" ]] || rm -f -- "$backup"
    [[ -z "$temp" ]] || rm -f -- "$temp"
    exit "$code"
  }
  trap cleanup EXIT
  if ((had_previous)); then backup="$(mktemp /etc/caddy/.junpanel-public-old.XXXXXXXX)"; cp -p -- "$target" "$backup"; fi
  if [[ "$mode" == --disable ]]; then
    changed=1; rm -f -- "$target"
  else
    temp="$(mktemp /etc/caddy/.junpanel-public-new.XXXXXXXX)"
    render "$ip" "$port" > "$temp"
    chmod 0644 "$temp"
    changed=1; mv -- "$temp" "$target"; temp=''
  fi
  runuser -u caddy -- /usr/bin/caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
  systemctl reload caddy.service
  changed=0
  if [[ "$mode" == --disable ]]; then
    printf '\n[JunPanel] 公网直连已关闭，仍可使用原有 SSH 隧道访问。\n'; exit
  fi
  printf '\n[JunPanel] HTTPS 直连入口已配置：https://%s\nCaddy 正在申请 IP 证书，稍后直接在浏览器打开上方地址，无需 SSH 隧道。\n' "$ip"
  if curl --noproxy '*' --fail --silent --show-error --connect-timeout 5 --max-time 15 "https://$ip/api/meta" >/dev/null 2>&1; then
    printf '本机 HTTPS 证书与面板响应检查通过；外部网络是否可达仍取决于现有网络设置。\n'
  else
    printf '证书签发或外部入口尚未就绪。公网 80/443 需可达；可查看：sudo journalctl -u caddy -n 80 --no-pager\n'
  fi
  printf '使用原有面板账号和密码登录。建议在“账号与安全”开启两步验证。\n'
)

main "$@"
