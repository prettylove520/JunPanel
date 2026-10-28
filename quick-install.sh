#!/usr/bin/env bash
# Verify downloads, install JunPanel, configure HTTPS and print a login link.
set -Eeuo pipefail

main() (
  set -Eeuo pipefail
  umask 077
  export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
  local version=1.5.16
  local expected=6f5ecf0db677e83a503fa012a9ecc9b54a53b8e43344f029df5a1421a30a98b8
  local public_ref=dfca8704170deed924ae00f0e0609c7e3b637670
  local public_hash=a10426f7c68355a7807fcecb147137ecdff9e2e57f0edd4c64122ea1e813bc2b
  local archive="JunPanel-$version-linux.tar.gz"
  local url="https://github.com/prettylove520/JunPanel/releases/download/v$version/$archive"
  local public_url="https://raw.githubusercontent.com/prettylove520/JunPanel/$public_ref/public-access.sh"
  local access="${JUNPANEL_ACCESS:-public}" public_ip='' public_result=1
  local workdir='' status=0 panel_user=admin panel_password='' line=''
  fail() { printf '\n[JunPanel] %s\n' "$*" >&2; exit 1; }
  cleanup() {
    status=$?
    trap - EXIT
    if [[ -n "$workdir" && -d "$workdir" && ! -L "$workdir" && "${workdir##*/}" == junpanel-install.* ]]; then rm -rf -- "$workdir"; fi
    if ((status)); then printf '\n[JunPanel] 安装未完成，请把上面的报错发来排查（不要发送密码）。\n  面板安装器失败时会自动恢复本次改动并保留数据；已装好的 Docker / Caddy 不会被卸载，修复后直接重跑同一条命令即可。\n' >&2; fi
    exit "$status"
  }
  # Suppress the packaged installer's SSH footer; print the selected access method below.
  install_output() {
    local text='' legacy_footer=0
    while IFS= read -r text || [[ -n "$text" ]]; do
      if [[ "$text" == '在你的电脑上建立 SSH 隧道'* ]]; then legacy_footer=1; fi
      if ((legacy_footer == 0)); then printf '%s\n' "$text"; fi
    done
  }
  # Installs Docker and Caddy from their official apt repositories when the server has neither. It only
  # adds what is missing and never touches an existing Docker or Caddy; the panel's own installer
  # (install.sh) still runs all of its checks afterwards.
  write_prepare() {
    cat > "$1" <<'PREPARE'
#!/usr/bin/env bash
set -Eeuo pipefail
umask 022
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export DEBIAN_FRONTEND=noninteractive
say() { printf '[JunPanel] %s\n' "$*"; }
die() { printf '[JunPanel] %s\n' "$*" >&2; exit 1; }
[[ -d /run/systemd/system ]] || die '需要 systemd 系统，无法自动准备运行环境。'
. /etc/os-release
distro="${ID:-}" codename="${VERSION_CODENAME:-}"
need_docker=0 need_compose=0 need_caddy=0
command -v docker >/dev/null || need_docker=1
if ((need_docker == 0)) && ! docker compose version >/dev/null 2>&1; then need_compose=1; fi
[[ -x /usr/bin/caddy ]] || need_caddy=1
if ((need_docker + need_compose + need_caddy == 0)); then
  say 'Docker、Compose 与 Caddy 均已就绪，无需额外安装。'
  exit 0
fi
case "$distro" in debian|ubuntu) ;; *) die "自动安装依赖仅支持 Debian / Ubuntu（当前 ${distro:-未知}）。请手动安装 Docker 与 Caddy 后重跑，或设置 JUNPANEL_AUTO_DEPS=0。";; esac
[[ -n "$codename" ]] || die '无法识别系统版本代号，请手动安装 Docker 与 Caddy。'
fetch_key() { curl --fail --location --silent --show-error --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 60 --retry 2 --output "$2" "$1"; chmod 0644 "$2"; }
say '更新软件包索引…'
apt-get update -qq
apt-get install -y -qq ca-certificates curl >/dev/null
install -d -m 0755 /etc/apt/keyrings
if ((need_docker)); then
  say '安装 Docker Engine 与 Compose 插件（Docker 官方源）…'
  fetch_key "https://download.docker.com/linux/$distro/gpg" /etc/apt/keyrings/docker.asc
  printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/%s %s stable\n' "$(dpkg --print-architecture)" "$distro" "$codename" > /etc/apt/sources.list.d/docker.list
  apt-get update -qq
  apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-compose-plugin >/dev/null || die 'Docker 安装失败，请查看上方报错。'
elif ((need_compose)); then
  say '已有 Docker，补装 Compose 插件…'
  apt-get install -y -qq docker-compose-plugin >/dev/null 2>&1 || apt-get install -y -qq docker-compose-v2 >/dev/null 2>&1 || die '已有 Docker 但缺少 Compose 插件，且自动安装失败。请手动安装 docker-compose-plugin 后重跑。'
fi
if ((need_docker + need_compose)); then
  systemctl enable --now docker >/dev/null 2>&1 || true
  for _ in {1..20}; do docker info >/dev/null 2>&1 && break; sleep 1; done
  docker info >/dev/null 2>&1 || die 'Docker 已安装但服务未能启动，请执行 systemctl status docker 查看原因。'
  docker compose version >/dev/null 2>&1 || die 'Docker Compose 插件不可用，请手动安装后重跑。'
fi
if ((need_caddy)); then
  for port in 80 443; do
    if [[ -n "$(ss -H -ltnp "sport = :$port" 2>/dev/null)" ]]; then
      die "端口 $port 已被其它程序占用，Caddy 无法启动。请先停用占用者（ss -ltnp 'sport = :$port' 可查看），或自行安装 Caddy 后重跑。"
    fi
  done
  say '安装 Caddy（Caddy 官方源）…'
  fetch_key https://dl.cloudsmith.io/public/caddy/stable/gpg.key /etc/apt/keyrings/caddy-stable.asc
  printf 'deb [signed-by=/etc/apt/keyrings/caddy-stable.asc] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main\n' > /etc/apt/sources.list.d/caddy-stable.list
  apt-get update -qq
  apt-get install -y -qq caddy >/dev/null || die 'Caddy 安装失败，请查看上方报错。'
  systemctl enable --now caddy.service >/dev/null 2>&1 || true
  for _ in {1..20}; do systemctl is-active --quiet caddy.service && break; sleep 1; done
  systemctl is-active --quiet caddy.service || die 'Caddy 已安装但服务未能启动，请执行 journalctl -u caddy -n 50 --no-pager 查看原因。'
fi
say '运行环境已准备好。'
PREPARE
  }
  trap cleanup EXIT
  [[ "$access" == public || "$access" == local ]] || fail 'JUNPANEL_ACCESS 只能为 public 或 local。'
  [[ "$(uname -s)" == Linux ]] || fail '请在 Debian 服务器的 SSH 窗口执行此命令。'
  case "$(uname -m)" in x86_64|aarch64|arm64) ;; *) fail '目前支持 amd64 / arm64 架构。';; esac
  [[ -f /etc/debian_version ]] || fail '此安装器面向 Debian 系统。'
  for command in curl tar sha256sum mktemp rm bash tee; do command -v "$command" >/dev/null || fail "缺少 $command，请先安装该系统工具。"; done
  local -a runner=()
  if ((EUID != 0)); then command -v sudo >/dev/null || fail '请使用 root 登录后重新执行。'; runner=(sudo); fi
  if [[ -n "${JUNPANEL_PORT:-}" ]]; then runner+=(env "JUNPANEL_PORT=$JUNPANEL_PORT"); fi
  workdir="$(mktemp -d /tmp/junpanel-install.XXXXXXXX)"
  [[ -d "$workdir" && ! -L "$workdir" ]] || fail '无法创建临时安装目录。'
  printf '\n  JunPanel · 中文服务器工作台\n\n[1/5] 下载安装包 v%s 与 HTTPS 入口配置器…\n' "$version"
  curl --fail --location --show-error --silent --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 300 --retry 2 --output "$workdir/$archive" "$url"
  curl --fail --location --show-error --silent --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 60 --retry 2 --output "$workdir/public-access.sh" "$public_url"
  printf '[2/5] 自动验证下载文件…\n'
  (cd -- "$workdir" && printf '%s  %s\n%s  public-access.sh\n' "$expected" "$archive" "$public_hash" | sha256sum --check --status) || fail '下载文件校验失败，未执行安装。请重新运行安装命令。'
  if [[ "$access" == public ]]; then
    if public_ip="$(bash "$workdir/public-access.sh" --detect 2>"$workdir/ip-detection.log")"; then
      if [[ "$public_ip" =~ ^[0-9.]+$ ]]; then printf '[JunPanel] 自动识别到服务器公网 IP：%s\n' "$public_ip"; else printf '[JunPanel] 沿用已配置的域名入口：%s\n' "$public_ip"; fi
    else
      public_ip=''
      printf '[JunPanel] 未识别到唯一公网 IPv4，将先安装面板，不生成未经确认的公网地址。\n'
    fi
  fi
  tar --extract --gzip --file "$workdir/$archive" --directory "$workdir" --no-same-owner
  [[ -f "$workdir/junpanel/install.sh" && ! -L "$workdir/junpanel/install.sh" ]] || fail '安装包缺少安装器。'
  if [[ "${JUNPANEL_AUTO_DEPS:-1}" != 0 ]]; then
    printf '[3/5] 准备运行环境（缺少 Docker 或 Caddy 时自动安装，普通用户可能需要输入 sudo 密码）…\n'
    write_prepare "$workdir/prepare.sh"
    "${runner[@]}" bash "$workdir/prepare.sh"
  else
    printf '[3/5] 已跳过自动准备运行环境（JUNPANEL_AUTO_DEPS=0）。\n'
  fi
  printf '[4/5] 检查环境并安装面板…\n'
  "${runner[@]}" bash "$workdir/junpanel/install.sh" | tee "$workdir/install.log" | install_output
  while IFS= read -r line; do
    case "$line" in '管理员：'*) panel_user="${line#管理员：}";; '初始密码：'*) panel_password="${line#初始密码：}";; esac
  done < "$workdir/install.log"
  if [[ "$access" == public && -n "$public_ip" ]]; then
    printf '[5/5] 自动配置 HTTPS 登录入口…\n'
    if "${runner[@]}" bash "$workdir/public-access.sh" "$public_ip"; then public_result=0; else public_result=$?; fi
  elif [[ "$access" == local ]]; then
    printf '[5/5] 配置本机访问模式…\n'
    if "${runner[@]}" bash "$workdir/public-access.sh" --disable; then public_result=0; else public_result=$?; fi
  else
    printf '[5/5] 未配置公网入口：无法自动识别公网 IPv4。\n'
  fi
  printf '\n==================================================\n  JunPanel 面板安装完成\n  账号：%s\n' "$panel_user"
  if [[ -n "$panel_password" ]]; then printf '  密码：%s\n' "$panel_password"; else printf '  密码：沿用原有面板密码（本次未重置）\n'; fi
  if [[ "$access" == public && -n "$public_ip" ]]; then
    if ((public_result == 0)); then
      printf '  登录链接：https://%s\n  状态：HTTPS 检查通过，可使用上方链接登录\n==================================================\n\n以后直接打开上方链接，无需建立 SSH 隧道。\n' "$public_ip"
    elif ((public_result == 2)); then
      printf '  登录链接（准备中）：https://%s\n  状态：证书或入口尚未就绪，当前不能确认可登录\n==================================================\n\n请查看上方提示，或执行：sudo journalctl -u caddy -n 80 --no-pager\n' "$public_ip"
    else
      printf '  公网入口：配置未完成，请查看上方报错\n==================================================\n'
    fi
    exit
  fi
  local client_ip='' client_port='' server_ip='' server_port='' ssh_user='' ssh_host='' panel_port=8787
  if [[ -f /opt/junpanel/port && ! -L /opt/junpanel/port ]]; then read -r panel_port < /opt/junpanel/port; elif [[ -n "${JUNPANEL_PORT:-}" ]]; then panel_port="$JUNPANEL_PORT"; fi
  printf '  本机入口：http://127.0.0.1:%s\n==================================================\n' "$panel_port"
  if [[ "$access" == local && "$public_result" != 0 ]]; then printf '\n公网入口关闭未完成，请查看上方报错确认访问状态。\n'; fi
  if [[ "$access" == public ]]; then printf '\n公网 IP 未识别；NAT、仅内网或多公网 IP 环境需要手动指定。可设置 JUNPANEL_PUBLIC_IP 后重跑安装器，账号与数据会保留。\n'; fi
  read -r client_ip client_port server_ip server_port <<< "${SSH_CONNECTION:-}"
  ssh_user="${SUDO_USER:-${USER:-root}}"
  if [[ "$ssh_user" =~ ^[a-zA-Z_][a-zA-Z0-9_-]*$ && "$server_ip" =~ ^[0-9a-fA-F:.]+$ && "$server_port" =~ ^[0-9]+$ && "$panel_port" =~ ^[0-9]{4,5}$ ]] && ((10#$panel_port >= 1024 && 10#$panel_port <= 65535)); then
    panel_port=$((10#$panel_port)); ssh_host="$server_ip"; [[ "$ssh_host" != *:* ]] || ssh_host="[$ssh_host]"
    printf '\n如使用本机入口，在电脑终端执行并保持窗口运行：\n  ssh -N -o ExitOnForwardFailure=yes -p %s -L 127.0.0.1:%s:127.0.0.1:%s %s@%s\n然后打开：http://127.0.0.1:%s\n' "$server_port" "$panel_port" "$panel_port" "$ssh_user" "$ssh_host" "$panel_port"
  fi
)

main "$@"
