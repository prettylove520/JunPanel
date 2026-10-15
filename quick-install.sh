#!/usr/bin/env bash
# JunPanel online installer: download completely, verify, then install.
set -Eeuo pipefail

main() (
  set -Eeuo pipefail
  umask 077
  export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
  local version=0.1.1
  local expected=d2d34100204c2f6d930939ad234253be4a8126f1d01d6de02e1ba0a0a8b9e302
  local archive="JunPanel-$version-linux.tar.gz"
  local url="https://github.com/prettylove520/JunPanel/releases/download/v$version/$archive"
  local workdir='' status=0
  fail() { printf '\n[JunPanel] %s\n' "$*" >&2; exit 1; }
  cleanup() {
    status=$?
    trap - EXIT
    if [[ -n "$workdir" && -d "$workdir" && ! -L "$workdir" && "${workdir##*/}" == junpanel-install.* ]]; then
      rm -rf -- "$workdir"
    fi
    if ((status)); then printf '\n[JunPanel] 安装未完成，请把上面的报错发来排查（不要发送密码）。\n' >&2; fi
    exit "$status"
  }
  trap cleanup EXIT
  [[ "$(uname -s)" == Linux ]] || fail '请在 Debian 服务器的 SSH 窗口执行此命令。'
  case "$(uname -m)" in x86_64|aarch64|arm64) ;; *) fail '目前支持 amd64 / arm64 架构。';; esac
  [[ -f /etc/debian_version ]] || fail '此安装器面向 Debian 系统。'
  for command in curl tar sha256sum mktemp rm bash; do
    command -v "$command" >/dev/null || fail "缺少 $command，请先安装该系统工具。"
  done
  if ((EUID != 0)); then command -v sudo >/dev/null || fail '请使用 root 登录后重新执行。'; fi
  workdir="$(mktemp -d /tmp/junpanel-install.XXXXXXXX)"
  [[ -d "$workdir" && ! -L "$workdir" ]] || fail '无法创建临时安装目录。'
  printf '\n  JunPanel · 中文服务器工作台\n\n[1/3] 下载安装包 v%s…\n' "$version"
  curl --fail --location --show-error --silent --proto '=https' --proto-redir '=https' \
    --connect-timeout 15 --max-time 300 --retry 2 --output "$workdir/$archive" "$url"
  printf '[2/3] 自动验证下载文件…\n'
  (cd -- "$workdir" && printf '%s  %s\n' "$expected" "$archive" | sha256sum --check --status) \
    || fail '下载文件校验失败，未执行安装。请重新运行安装命令。'
  tar --extract --gzip --file "$workdir/$archive" --directory "$workdir" --no-same-owner
  [[ -f "$workdir/junpanel/install.sh" && ! -L "$workdir/junpanel/install.sh" ]] || fail '安装包缺少安装器。'
  printf '[3/3] 检查环境并安装（普通用户可能需要输入 sudo 密码）…\n'
  if ((EUID == 0)); then
    bash "$workdir/junpanel/install.sh"
  elif [[ -n "${JUNPANEL_PORT:-}" ]]; then
    sudo env "JUNPANEL_PORT=$JUNPANEL_PORT" bash "$workdir/junpanel/install.sh"
  else
    sudo bash "$workdir/junpanel/install.sh"
  fi
  printf '\n[JunPanel] 安装完成。请记下上方随机面板密码。\n'
  # SSH_CONNECTION supplies the server address without a public-IP lookup service.
  local client_ip client_port server_ip server_port ssh_user ssh_host
  read -r client_ip client_port server_ip server_port <<< "${SSH_CONNECTION:-}"
  ssh_user="${SUDO_USER:-${USER:-root}}"
  if [[ "$ssh_user" =~ ^[a-zA-Z_][a-zA-Z0-9_-]*$ && "$server_ip" =~ ^[0-9a-fA-F:.]+$ && "$server_port" =~ ^[0-9]+$ ]]; then
    ssh_host="$server_ip"
    [[ "$ssh_host" != *:* ]] || ssh_host="[$ssh_host]"
    local panel_port=8787
    if [[ -f /opt/junpanel/port && ! -L /opt/junpanel/port ]]; then
      read -r panel_port < /opt/junpanel/port
    elif [[ -n "${JUNPANEL_PORT:-}" ]]; then panel_port="$JUNPANEL_PORT"; fi
    if [[ "$panel_port" =~ ^[0-9]{4,5}$ ]] && ((10#$panel_port >= 1024 && 10#$panel_port <= 65535)); then
      panel_port=$((10#$panel_port))
      printf '\n在你电脑的终端粘贴以下命令，并保持窗口运行：\n\n  ssh -N -o ExitOnForwardFailure=yes -p %s -L 127.0.0.1:%s:127.0.0.1:%s %s@%s\n\n然后打开：http://127.0.0.1:%s\n' "$server_port" "$panel_port" "$panel_port" "$ssh_user" "$ssh_host" "$panel_port"
    fi
  fi
)

main "$@"
