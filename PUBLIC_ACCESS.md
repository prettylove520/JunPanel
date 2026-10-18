# 使用服务器 IP 直接访问 JunPanel

已安装 JunPanel 0.1.1 后，在服务器的 SSH 窗口执行一次，把最后的参数换成服务器的公网 IPv4：

```bash
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash -s -- 服务器公网IPv4
```

之后直接打开 `https://服务器公网IPv4`，使用原有面板账号和密码登录，不需要再建立 SSH 隧道。root 用户也可使用 `bash` 代替 `sudo bash`。本脚本不安装面板；如果尚未安装，先执行 README 中的一条命令安装。

## 前置条件

- 已安装并运行 JunPanel；本机 `/api/meta` 能正常响应。
- 使用现有标准 systemd Caddy 服务，Caddy 版本至少 2.10.1。可用 `caddy version` 查看版本。脚本不安装或替换 Caddy。
- IP 必须是此服务器拥有的公网 IPv4。当前脚本不处理 IPv6 或域名。
- 公网 80/443 必须能到达现有 Caddy；脚本不添加防火墙规则。若 IP 的 HTTPS 地址已有其它网站，先检查地址冲突。
- IP 证书使用 Let's Encrypt `shortlived` 配置，由 Caddy 自动续签。签发依赖外部网络和 CA；“Caddy 已重载”不等于证书已签发。

依据：[Let's Encrypt IP 证书说明](https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability/)、[Caddy tls 文档](https://caddyserver.com/docs/caddyfile/directives/tls)、[Caddy 官方 IP 证书支持记录](https://github.com/caddyserver/caddy/issues/7118)。

脚本只写入 `/etc/caddy/junpanel.d/panel-access.caddy`，保留主配置及其它站点，校验后重载。配置失败时尝试恢复原入口；不要同时编辑 Caddy 配置。它不更改 SSH、Fail2ban、防火墙、应用容器或备份设置。

## 访问保护

公网入口的登录页任何人都可访问，必须使用原有管理员密码；建议开启面板两步验证。原有密码哈希、会话、登录锁定与 CSRF 检查保持生效。当前登录锁定为全局共享，可能被外部失败登录触发。

0.1.1 后端仅接受本机 Host / Origin，因此本配置先在 Caddy 中拒绝不匹配、缺失的写请求或 WebSocket Origin，再适配到本机接口；公网会话 Cookie 增加 Secure 属性。不要移除这些检查或改成公网 HTTP 传输登录密码。后端仍只监听本机，0.1.1 界面中的本机连接标签描述的是后端连接方式。

如果服务器位于 NAT / 反向代理之后，IP 证书选择还可能需要 Caddy 的 `default_sni` 定制；本脚本不修改全局配置。此类环境应先人工检查，不能保证直接适配。

## 关闭入口与排查

关闭公网直连，保留面板和已有网站：

```bash
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash -s -- --disable
```

重新运行带 IP 的命令可更改直连 IP。更改面板后端端口后，也需重新运行该命令。卸载面板前建议先关闭公网入口。

证书尚未就绪、入口无法打开时，在服务器查看：

```bash
caddy version
sudo systemctl status junpanel-web caddy --no-pager
sudo journalctl -u caddy -n 80 --no-pager
```

浏览器访问必须使用 `https://`。证书未就绪时请根据日志排查，不要忽略浏览器证书警告。

仅预览配置，不修改服务器：

```bash
bash public-access.sh --print 203.0.113.10 8787
```

上面的文档示例 IP 不能用于实际证书签发。

## 本次验证

2026-10-18，在 Windows 本机使用官方 Caddy 2.11.7 与实际 JunPanel 0.1.1 演示程序完成：

- 生成的 IP HTTPS 配置通过 Caddy adapt，确认使用生产 ACME 地址和 shortlived 配置。
- 将相同路由用于仅本机 HTTP 测试，检查实际代理登录、会话读取、CSRF 拒绝、退出与失效。
- 检查公网响应 Cookie 带 Secure、HttpOnly、SameSite=Strict。
- 检查外部 Origin、缺失 Origin 的写请求 / WebSocket、HTTP Origin 和跨站读取被拒绝。
- 检查 IPv4 参数与配置注入拒绝，以及 Bash 语法。

本次未申请真实公网证书，未执行 Debian 上的 systemd 重载 / 失败恢复，也未修改用户的远程服务器。证书签发与服务器部署仍需在实际环境验证。
