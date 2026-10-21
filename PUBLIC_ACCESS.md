# 公网访问 JunPanel：服务器 IP 或你自己的域名

面板有两种公网入口，可以只用其中一种，也可以**同时使用**，由同一个脚本管理：

- **服务器 IP**：新版在线安装器自动配置，见下文。
- **你自己的域名**：见下一节。配置域名**不会**影响已有的 IP 入口，两个地址都可以登录。

## 用域名访问

先做三件事：

1. 在域名服务商处添加一条 **A 记录**，把你要用的名字（例如 `panel.example.com`）指向这台服务器的公网 IPv4。
2. 确认服务器的 80 和 443 端口对外开放（用 IP 能打开面板，说明已经开放）。
3. 如果域名在 Cloudflare 等服务上，先把这条记录设为“仅 DNS”（灰色云朵），不要开代理；证书由服务器上的 Caddy 向 Let's Encrypt 申请，需要请求直接到达服务器。

然后在服务器的 SSH 窗口执行（把域名换成你的）：

```bash
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash -s -- --domain panel.example.com
```

脚本会：

- 先检查域名格式，并**在改动任何配置之前**检查它是否已经解析到这台服务器、有没有被 Caddy 里的其它站点占用（包括面板“网站”页创建的站点）。任何一项不满足都会直接停下，原有入口保持不变。
- 写入域名入口，由 Caddy 自动申请并续期证书；校验整份配置后才重载，失败会自动还原。
- 检查本机 Caddy 上这个域名的证书与面板响应，通过后显示登录链接。已有的 IP 入口保持不变；证书还没签发好时显示“准备中”，可以稍等后查看日志。

之后重新执行一键安装命令会原样保留已配置的域名和 IP 入口。已经是面板入口的域名不会被再次检查解析，所以域名经过线路机等中转、解析与服务器本机地址不一致时，重跑安装命令也不会被拦住。

### 调整入口

指定哪一种，就只改哪一种，另一种保持不变：

```bash
# 换成另一个域名（IP 入口保留）
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash -s -- --domain 新域名
# 加上或更换 IP 入口（域名入口保留）；IP 入口需要 Caddy 2.10.1 或更新版本
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash -s -- 服务器公网IPv4
# 一条命令同时设置两个
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash -s -- --domain 你的域名 服务器公网IPv4
# 只保留所指定的一种，停用另一种：加 --only
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash -s -- --domain 你的域名 --only
# 单独停用一种，或全部关闭
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash -s -- --remove-domain
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash -s -- --remove-ip
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash -s -- --disable
```

域名要求：小写字母、数字和连字符组成，至少两段（`panel.example.com`），不带 `https://` 和端口；中文域名请先转换为 Punycode（`xn--…`）；`example.com`、`.local`、`.test` 等保留名称无法签发证书，会被拒绝。如果域名还有 AAAA（IPv6）记录，请确认服务器的 IPv6 也能到达 Caddy，否则证书机构可能通过 IPv6 验证失败。服务器在 NAT 之后、解析与服务器本机地址不一致时，确认无误后可设置 `JUNPANEL_SKIP_DNS_CHECK=1` 跳过解析比对。

域名入口使用与 IP 入口相同的保护：Caddy 先拒绝来源不匹配或缺失的写请求和 WebSocket，再改写为本机请求；登录 Cookie 带 Secure。常规 Let's Encrypt 证书不要求 Caddy 2.10；IP 入口的短期证书才需要。

## 使用服务器 IP 直接访问

新版在线安装器已自动识别 IP、配置 HTTPS，并在最后显示账号、密码和登录链接。正常安装不需要再执行本文的入口配置命令。

已有 JunPanel 0.1.1，仅需启用或重新检查入口时，在服务器的 SSH 窗口执行一次：

```bash
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash
```

脚本会自动识别服务器公网 IPv4，配置入口并等待 HTTPS 检查，随后显示实际登录链接。使用原有面板账号和密码登录，不需要再建立 SSH 隧道。root 用户也可使用 `bash` 代替 `sudo bash`。本脚本不安装面板；如果尚未安装，先执行 README 中的一条命令安装。

## 自动识别与安装结果

识别顺序为：显式指定的 `JUNPANEL_PUBLIC_IP`、已配置的公网入口、当前 SSH 连接的服务器地址、默认路由源地址、唯一的公网网卡地址。路由查询只读取本机路由表，不向查询地址发送数据；不调用外部 IP 查询服务。内网、回环、链路本地、共享地址、文档示例和多播地址不会作为自动登录地址。

多个公网地址无法确定、NAT 或仅内网环境下，不会随意选择一个 IP。可显式传入已知公网 IPv4：

```bash
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash -s -- 服务器公网IPv4
```

脚本最多等待约一分钟检查 HTTPS。只有证书有效且面板 `/api/meta` 正常响应时才提示检查通过；仍在签发或网络不通时显示“登录链接（准备中）”并返回状态码 2，保留已经校验并加载的 Caddy 配置供后续重试。在线安装器会保留该状态，不把它当成可立即登录。

在线安装器最后汇总账号、初始密码和真实登录链接；已有账号不会重置密码。安装输出临时保存在私有临时目录用于汇总密码，结束时删除，不写入公开日志或长期密码文件。

## 前置条件

- 已安装并运行 JunPanel；本机 `/api/meta` 能正常响应。
- 使用现有标准 systemd Caddy 服务，Caddy 版本至少 2.10.1。可用 `caddy version` 查看版本。脚本不安装或替换 Caddy。
- IP 入口：IP 必须是此服务器拥有的公网 IPv4，不处理 IPv6；域名入口见上一节。
- 公网 80/443 必须能到达现有 Caddy；脚本不添加防火墙规则。若 IP 的 HTTPS 地址已有其它网站，先检查地址冲突。
- IP 证书使用 Let's Encrypt `shortlived` 配置，由 Caddy 自动续签。签发依赖外部网络和 CA；“Caddy 已重载”不等于证书已签发。

依据：[Let's Encrypt IP 证书说明](https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability/)、[Caddy tls 文档](https://caddyserver.com/docs/caddyfile/directives/tls)、[Caddy 官方 IP 证书支持记录](https://github.com/caddyserver/caddy/issues/7118)。

脚本管理 `/etc/caddy/junpanel.d/panel-access.caddy`，并按需在主 Caddyfile 的全局选项中补入带 `# JunPanel IP TLS default` 标记的 `default_sni`，让没有发送 SNI 的 IP 浏览器连接能选择对应证书。保留其它站点与全局选项，校验整份候选配置后才替换主文件并重载；失败时尝试恢复主文件与入口片段。若已有不同且不属于面板的 `default_sni`，拒绝自动覆盖。不要同时编辑 Caddy 配置。脚本不更改 SSH、Fail2ban、防火墙、应用容器或备份设置。

## 访问保护

公网入口的登录页任何人都可访问，必须使用原有管理员密码；建议开启面板两步验证。原有密码哈希、会话、登录锁定与 CSRF 检查保持生效。登录锁定按来源 IP 分别计算：Caddy 会用真实客户端地址覆盖外部传入的 `X-Forwarded-For`，面板据此识别来源，并在日志中记录 `ip=`。

0.1.1 后端仅接受本机 Host / Origin，因此本配置先在 Caddy 中拒绝不匹配、缺失的写请求或 WebSocket Origin，再适配到本机接口；公网会话 Cookie 增加 Secure 属性。不要移除这些检查或改成公网 HTTP 传输登录密码。后端仍只监听本机，0.1.1 界面中的本机连接标签描述的是后端连接方式。

IP 浏览器连接通常不发送 SNI；本机测试已复现没有默认名称时的证书选择失败，脚本现在自动配置 `default_sni`。NAT 环境仍可能需要显式指定正确公网 IP，并确保入站连接最终到达此 Caddy。复杂导入的全局选项或自定义 TLS 策略仍需人工检查；脚本会校验并拒绝不兼容配置。

## 关闭入口与排查

关闭公网直连，保留面板和已有网站：

```bash
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash -s -- --disable
```

重新运行带 IP 的命令可更改直连 IP，域名入口保留；不传地址时沿用已配置的全部公网入口。更改面板后端端口后，也需重新运行入口配置。卸载面板前建议先关闭公网入口。

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
- 使用可重复的 Bash 测试检查 SSH / 路由 / 网卡识别、地址歧义、非公网地址排除及显式覆盖；测试保存在 `tests/ip-detection.sh` 并由 Linux CI 执行。
- 使用实际 Caddy TLS 握手复现无 SNI 的 IP 证书选择失败；由生产脚本生成 `default_sni` 修复后，无 SNI 和 IP SNI 均通过证书验证，同端口的既有域名 TLS 仍正常。测试使用本机专用证书与显式测试信任，不安装系统根证书、不申请公网证书。
- `tests/tls-defaults.sh` 检查现有站点与全局选项保留、重复运行、更换 IP、关闭时移除面板拥有的默认项、拒绝覆盖其它默认名称。
- 用隔离的下载与安装替身检查在线安装器的就绪、已有账号、证书准备中、配置失败、无法识别公网 IP、本机模式、安装失败、两个文件校验失败和下载失败；这些是流程模拟，不是真实服务器部署。

本次未申请真实公网证书，未执行 Debian 上的 systemd 重载 / 失败恢复，也未修改用户的远程服务器。证书签发与服务器部署仍需在实际环境验证。
