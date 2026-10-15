#!/usr/bin/env bash
set -euo pipefail
[[ $EUID -eq 0 ]] || { printf '请使用 sudo bash uninstall.sh\n' >&2; exit 1; }
[[ -f /opt/junpanel/.junpanel-install && ! -L /opt/junpanel ]] || { printf '未找到 JunPanel 安装标记，未做更改。\n' >&2; exit 1; }
systemctl disable --now junpanel-web.service junpanel-agent.service
rm -f /etc/systemd/system/junpanel-web.service /etc/systemd/system/junpanel-agent.service
rm -f /opt/junpanel/junpanel
systemctl daemon-reload
printf 'JunPanel 服务与程序已移除。\n账号、数据库凭据、应用数据卷、站点文件和 Caddy 片段均保留，网站与应用继续运行。\n如需恢复面板，可重新执行完整安装包中的 install.sh。\n'
