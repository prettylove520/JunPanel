#!/usr/bin/env bash
set -Eeuo pipefail
umask 027
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
BASE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
PORT="${JUNPANEL_PORT:-8787}"
if [[ -z "${JUNPANEL_PORT:-}" && -f /opt/junpanel/port && ! -L /opt/junpanel/port ]]; then PORT="$(cat /opt/junpanel/port)"; fi
MODE="${1:-install}"
die() { printf '\n[未完成] %s\n' "$*" >&2; return 1; }
say() { printf '[JunPanel] %s\n' "$*"; }
[[ "$MODE" == install || "$MODE" == --check ]] || die '用法：sudo bash install.sh [--check]'
[[ $EUID -eq 0 ]] || die '请使用 sudo bash install.sh'
[[ "$PORT" =~ ^[0-9]{4,5}$ ]] && ((10#$PORT >= 1024 && 10#$PORT <= 65535)) || die 'JUNPANEL_PORT 必须为 1024–65535'
PORT="$((10#$PORT))"
[[ -f /etc/debian_version ]] || die '安装器面向 Debian 系统'
[[ -d /run/systemd/system ]] || die '需要 systemd 系统服务'
for command in systemctl getent useradd groupadd runuser install sha256sum ss; do command -v "$command" >/dev/null || die "缺少系统命令：$command"; done
case "$(uname -m)" in x86_64) ARCH=amd64;; aarch64|arm64) ARCH=arm64;; *) die '仅提供 Linux amd64 / arm64 预编译程序';; esac
BIN="$BASE/bin/junpanel-linux-$ARCH"
[[ -f "$BIN" && ! -L "$BIN" && -f "$BASE/bin/SHA256SUMS" ]] || die '请解压包含 bin 目录的完整安装包'
(cd "$BASE/bin" && sha256sum -c SHA256SUMS --status) || die '程序校验和不符，请重新获取完整安装包'
command -v docker >/dev/null && docker info >/dev/null 2>&1 || die '请先准备现有 Docker Engine；安装器不替换 Docker'
docker compose version >/dev/null 2>&1 || die '未检测到 Docker Compose 插件'
[[ -x /usr/bin/caddy && -f /etc/caddy/Caddyfile && ! -L /etc/caddy/Caddyfile ]] || die '需要 /usr/bin/caddy 与普通文件 /etc/caddy/Caddyfile'
systemctl is-active --quiet caddy.service || die '现有 caddy.service 未运行'
[[ "$(systemctl show caddy.service -p User --value)" == caddy ]] || die '需要以 caddy 用户运行的标准系统服务'
[[ "$(getent passwd caddy | cut -d: -f6)" == /var/lib/caddy ]] || die 'Caddy 用户主目录经过定制，请先手动适配'
CADDY_START="$(systemctl show caddy.service -p ExecStart --value)"
[[ "$CADDY_START" == *'/usr/bin/caddy'* && "$CADDY_START" == *'--config /etc/caddy/Caddyfile'* ]] || die 'Caddy 使用了自定义启动配置；请先按文档手动适配'
[[ -z "$(systemctl show caddy.service -p EnvironmentFiles --value)" ]] || die 'Caddy 使用 EnvironmentFile，请先为代理适配相同环境后手动安装'
grep -Fq '{$' /etc/caddy/Caddyfile && die '主 Caddyfile 含环境变量占位，请先按文档手动适配'
runuser -u caddy -- /usr/bin/caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1 || die '现有 Caddy 配置未通过校验，未更改文件'
for dir in /opt/junpanel /var/lib/junpanel-web /var/lib/junpanel-agent /srv/junpanel /etc/caddy/junpanel.d; do
  [[ ! -L "$dir" ]] || die "$dir 是符号链接，拒绝安装"
done
if [[ -e /opt/junpanel && ! -f /opt/junpanel/.junpanel-install ]]; then die '/opt/junpanel 已有其它内容，拒绝覆盖'; fi
if [[ ! -f /opt/junpanel/.junpanel-install ]]; then
  for dir in /var/lib/junpanel-web /var/lib/junpanel-agent /srv/junpanel /etc/caddy/junpanel.d; do [[ ! -e "$dir" ]] || die "$dir 已存在，请检查是否为旧安装或其它数据"; done
  for unit in junpanel-web.service junpanel-agent.service; do [[ "$(systemctl show "$unit" -p LoadState --value)" == not-found ]] || die "$unit 已存在，拒绝覆盖"; done
  getent passwd junpanel-web >/dev/null && die 'junpanel-web 账号已存在但无安装标记，拒绝接管'
  getent group junpanel >/dev/null && die 'junpanel 组已存在但无安装标记，拒绝接管'
fi
if ! systemctl is-active --quiet junpanel-web.service && [[ -n "$(ss -H -ltn "sport = :$PORT")" ]]; then die "本机端口 $PORT 已被占用，可设置 JUNPANEL_PORT"; fi
say "检查通过 · $(docker compose version --short) · 架构 $ARCH · 本机端口 $PORT"
[[ "$MODE" != --check ]] || exit 0

ROLLBACK=''
BIN_OLD=''
WEB_OLD=''
AGENT_OLD=''
WAS_WEB=0
WAS_AGENT=0
systemctl is-active --quiet junpanel-web.service && WAS_WEB=1
systemctl is-active --quiet junpanel-agent.service && WAS_AGENT=1
if [[ -f /opt/junpanel/junpanel ]]; then BIN_OLD="$(mktemp /opt/junpanel/.previous.XXXXXX)"; cp -p /opt/junpanel/junpanel "$BIN_OLD"; fi
if [[ -f /etc/systemd/system/junpanel-web.service ]]; then WEB_OLD="$(mktemp /etc/systemd/system/.junpanel-web.XXXXXX)"; cp -p /etc/systemd/system/junpanel-web.service "$WEB_OLD"; fi
if [[ -f /etc/systemd/system/junpanel-agent.service ]]; then AGENT_OLD="$(mktemp /etc/systemd/system/.junpanel-agent.XXXXXX)"; cp -p /etc/systemd/system/junpanel-agent.service "$AGENT_OLD"; fi
on_error() {
  local code=$?
  trap - ERR
  printf '\n[未完成] 安装中断，正在恢复本次改动。数据目录会保留。\n' >&2
  systemctl stop junpanel-web.service junpanel-agent.service >/dev/null 2>&1 || true
  if [[ -n "$ROLLBACK" && -f "$ROLLBACK" ]]; then cp -p -- "$ROLLBACK" /etc/caddy/Caddyfile; runuser -u caddy -- /usr/bin/caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1 && systemctl reload caddy.service || printf 'Caddy 恢复未完成，请立即通过 SSH 检查。\n' >&2; rm -f -- "$ROLLBACK"; fi
  if [[ -n "$BIN_OLD" && -f "$BIN_OLD" ]]; then mv -- "$BIN_OLD" /opt/junpanel/junpanel; fi
  if [[ -n "$WEB_OLD" && -f "$WEB_OLD" ]]; then mv -- "$WEB_OLD" /etc/systemd/system/junpanel-web.service; else rm -f /etc/systemd/system/junpanel-web.service; fi
  if [[ -n "$AGENT_OLD" && -f "$AGENT_OLD" ]]; then mv -- "$AGENT_OLD" /etc/systemd/system/junpanel-agent.service; else rm -f /etc/systemd/system/junpanel-agent.service; fi
  systemctl daemon-reload || true
  if ((WAS_AGENT)); then systemctl start junpanel-agent.service || true; fi
  if ((WAS_WEB)); then systemctl start junpanel-web.service || true; fi
  exit "$code"
}
trap on_error ERR
getent group junpanel >/dev/null || groupadd --system junpanel
getent passwd junpanel-web >/dev/null || useradd --system --user-group --home-dir /var/lib/junpanel-web --shell /usr/sbin/nologin junpanel-web
install -d -m 0755 -o root -g root /opt/junpanel /srv/junpanel /srv/junpanel/sites /etc/caddy/junpanel.d
install -d -m 0700 -o junpanel-web -g junpanel-web /var/lib/junpanel-web
install -d -m 0700 -o root -g root /var/lib/junpanel-agent /srv/junpanel/.trash
systemctl stop junpanel-web.service junpanel-agent.service >/dev/null 2>&1 || true
install -m 0755 -o root -g root "$BIN" /opt/junpanel/junpanel.next
mv /opt/junpanel/junpanel.next /opt/junpanel/junpanel
printf 'JunPanel 0.1.3\n' > /opt/junpanel/.junpanel-install
chmod 0644 /opt/junpanel/.junpanel-install
if [[ ! -e /var/lib/junpanel-web/account.json ]]; then runuser -u junpanel-web -- /opt/junpanel/junpanel setup --data /var/lib/junpanel-web; fi
[[ -f /var/lib/junpanel-web/account.json && ! -L /var/lib/junpanel-web/account.json ]] || die '管理员配置异常'
chown junpanel-web:junpanel-web /var/lib/junpanel-web/account.json
chmod 0600 /var/lib/junpanel-web/account.json

cat > /etc/systemd/system/junpanel-agent.service <<'UNIT'
[Unit]
Description=JunPanel local management agent
After=docker.service caddy.service
Requires=docker.service
[Service]
Type=simple
User=root
Group=junpanel
ExecStart=/opt/junpanel/junpanel agent
Restart=on-failure
RestartSec=3
RuntimeDirectory=junpanel
RuntimeDirectoryMode=0750
UMask=0022
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=/var/lib/junpanel-agent /srv/junpanel /etc/caddy/junpanel.d /run/junpanel -/var/lib/caddy
ProtectHome=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
MemoryMax=384M
TasksMax=96
[Install]
WantedBy=multi-user.target
UNIT
cat > /etc/systemd/system/junpanel-web.service <<UNIT
[Unit]
Description=JunPanel Chinese server workbench
After=junpanel-agent.service
Requires=junpanel-agent.service
[Service]
Type=simple
User=junpanel-web
Group=junpanel-web
SupplementaryGroups=junpanel
ExecStart=/opt/junpanel/junpanel server --port $PORT
Restart=on-failure
RestartSec=3
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/junpanel-web
InaccessiblePaths=-/var/run/docker.sock -/run/docker.sock -/var/lib/junpanel-agent -/etc/shadow
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
CapabilityBoundingSet=
LockPersonality=true
RestrictSUIDSGID=true
MemoryMax=192M
TasksMax=48
[Install]
WantedBy=multi-user.target
UNIT
chmod 0644 /etc/systemd/system/junpanel-{web,agent}.service
if ! grep -Fxq 'import /etc/caddy/junpanel.d/*.caddy' /etc/caddy/Caddyfile; then
  ROLLBACK="$(mktemp /etc/caddy/.junpanel-install.XXXXXX)"
  cp -p /etc/caddy/Caddyfile "$ROLLBACK"
  printf '\n# JunPanel managed sites\nimport /etc/caddy/junpanel.d/*.caddy\n' >> /etc/caddy/Caddyfile
  runuser -u caddy -- /usr/bin/caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
  systemctl reload caddy.service
fi
systemctl daemon-reload
systemctl start junpanel-agent.service junpanel-web.service
systemctl is-active --quiet junpanel-agent.service
systemctl is-active --quiet junpanel-web.service
READY=0
for attempt in {1..20}; do
  if [[ -S /run/junpanel/agent.sock ]] && (exec 3<>"/dev/tcp/127.0.0.1/$PORT"; printf 'GET /api/meta HTTP/1.1\r\nHost: 127.0.0.1:%s\r\nConnection: close\r\n\r\n' "$PORT" >&3; IFS= read -r reply <&3; [[ "$reply" == 'HTTP/1.1 200'* ]]) 2>/dev/null; then READY=1; break; fi
  sleep 0.5
done
((READY)) || die '面板未就绪，请查看 junpanel-agent / junpanel-web 服务日志'
systemctl enable junpanel-agent.service junpanel-web.service >/dev/null
printf '%s\n' "$PORT" > /opt/junpanel/port
chmod 0644 /opt/junpanel/port
for temp in "$ROLLBACK" "$BIN_OLD" "$WEB_OLD" "$AGENT_OLD"; do [[ -z "$temp" ]] || rm -f -- "$temp"; done
trap - ERR
say '安装完成。现有 SSH、Fail2ban、防火墙、Swap 和安全更新均未配置变更。'
printf '\n在你的电脑上建立 SSH 隧道（保持本窗口运行）：\n  ssh -N -o ExitOnForwardFailure=yes -L 127.0.0.1:%s:127.0.0.1:%s 你的SSH用户@服务器IP\n\n浏览器打开：http://127.0.0.1:%s\n账号：admin（即 admin）\n忘记面板密码：sudo -u junpanel-web /opt/junpanel/junpanel password\n' "$PORT" "$PORT" "$PORT"
