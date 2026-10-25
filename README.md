# JunPanel

![JunPanel](logo-preview.png)

[![CI](https://github.com/prettylove520/JunPanel/actions/workflows/ci.yml/badge.svg)](https://github.com/prettylove520/JunPanel/actions/workflows/ci.yml) · [下载 v1.4.2](https://github.com/prettylove520/JunPanel/releases/tag/v1.4.2) · [MIT 许可证](LICENSE)

中文优先的自托管服务器工作台。独立编写的 Go 后端与原生 HTML/CSS/JavaScript 界面，采用石墨色与薄荷绿设计，支持深浅主题和手机布局。

品牌素材：[矢量标识](web/logo.svg) · [标识预览](logo-preview.png)

界面参照 1Panel 的布局，视觉沿用常见服务器面板的结构（左侧菜单、首页四个圆环仪表、卡片和表格），细节参照 Vercel、Linear：一套中性灰阶、近黑色操作按钮、只用一种靛蓝表示选中/链接/图表，状态以“圆点 + 文字”显示；左侧一级菜单（概览、应用商店、网站、数据库、容器、文件、计划任务、备份、告警、日志、面板设置），同一模块下的页面用顶部标签切换；支持浅色与深色主题（默认跟随系统），小屏幕自动收起菜单。概览与网站列表同时显示面板网站和原有 Caddy 域名（只读，按域名统计）。除主配置外，还会从运行中的系统 Caddy 进程发现其他配置文件，显示配置路径、PID 和读取结果；相同域名去重，其他实例不会被面板重载。读取的是磁盘配置，并非运行时管理 API 快照；使用 `--resume`、自定义适配器或被隐藏的进程不在自动发现范围内；Docker 列表包含系统 Docker 中运行和已停止的全部容器。直接运行的程序和 systemd 服务不属于 Docker 容器，“面板安装应用”只统计通过 JunPanel 安装的应用。读取失败会显示错误，不当作零项。 界面字体（Geist、Geist Mono，中文为思源黑体 Noto Sans SC，均为 SIL OFL 授权，见 `web/fonts/LICENSE.txt`）随程序内置，不访问外部字体服务；中文字体按 Unicode 分片，页面只下载实际用到的字。应用商店显示各应用自己的 Logo（来源与授权见 `third-party/app-icons-NOTICE.txt`），仅用于标识应用。

界面预览（模拟数据）：[概览](preview.png) · [应用商店](app-center.png)

产品方向参考 KunPanel 的模块覆盖和操作便利性；本项目没有使用 KunPanel 源码、模板、图标、页面或品牌资源，也不是它的改名版本。

## 一条命令安装

安装包已包含 **Linux amd64 / arm64 预编译程序**。服务器无需安装 Go、Node、Python、npm 或构建前端，也不需要再部署一组面板容器。

在 Debian 服务器的 SSH 窗口执行下面这一条命令（root 或有 sudo 权限的用户均可）：

```bash
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/quick-install.sh | bash
```

自动完成下载、校验、解压、环境检查、面板安装、**公网 IPv4 识别和 HTTPS 管理入口配置**。结束时集中显示账号、随机密码和登录链接：证书及面板响应检查通过后可直接打开 `https://服务器IP`，无需再填 IP 或执行另一条配置命令。公网登录页会公开，仍需管理员认证；建议开启两步验证。HTTPS 入口要求现有 Caddy 至少 2.10.1、公网 80/443 可达。

在线安装器固定下载 v1.4.2 安装包，并从固定 Git 提交下载 HTTPS 配置器；两者都使用脚本内固定的 SHA-256 校验，失败不会执行安装。已有安装可重跑同一命令，沿用账号、数据与保存的端口；不会重置原密码。请先等待后台任务结束。

IP 识别不访问第三方 IP 查询服务：优先保留已配置的公网入口，再读取当前 SSH 连接、本机路由和网卡。NAT、仅 IPv6、仅内网或多地址无法确定时会明确提示，不把私网 IP 当成公网链接。证书尚未就绪会标注“准备中”，不会提示已经可登录。

如果只希望使用 SSH 隧道，显式选择本机模式：

```bash
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/quick-install.sh | JUNPANEL_ACCESS=local bash
```

如需先查看安装脚本，可在浏览器打开 [quick-install.sh](https://github.com/prettylove520/JunPanel/blob/main/quick-install.sh)。网络无法连接 GitHub 时可使用下面的手动安装方式。

<details>
<summary>手动下载或离线安装</summary>

1. 在服务器的 SSH 窗口下载安装包并核对校验和：

   ```bash
   curl -fL -o JunPanel-1.4.2-linux.tar.gz https://github.com/prettylove520/JunPanel/releases/download/v1.4.2/JunPanel-1.4.2-linux.tar.gz
   curl -fL -o JunPanel-1.4.2-SHA256SUMS.txt https://github.com/prettylove520/JunPanel/releases/download/v1.4.2/JunPanel-1.4.2-SHA256SUMS.txt
   sha256sum --ignore-missing -c JunPanel-1.4.2-SHA256SUMS.txt
   ```

   如果服务器无法连接 GitHub，可从[版本下载页](https://github.com/prettylove520/JunPanel/releases/tag/v1.4.2)下载到电脑，再通过 `scp` 上传。

2. 在服务器的 SSH 窗口执行：

   ```bash
   tar -xzf JunPanel-1.4.2-linux.tar.gz && sudo bash junpanel/install.sh
   ```

   安装器自动选择架构、验证程序校验和、检查现有服务、创建随机面板密码并启动两个 systemd 服务。记下终端显示的 `admin` 账号与随机密码。**新安装没有默认密码**。

</details>

### 打开面板

**在线安装完成后，直接打开终端最后显示的“登录链接”**，使用同一处显示的账号与密码登录。无需手填 IP 或重复配置。

**已有旧安装，只配置访问入口：** 在服务器执行下面这一条命令，自动识别 IP、配置 HTTPS 并显示登录链接：

```bash
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash
```

随后使用脚本显示的链接和原有面板账号登录。管理员可在设置中修改密码，最低 8 个字符。支持现有 0.1.1 安装，使用系统 Caddy 申请和续签 IP 证书；要求 **Caddy 2.10.1 或更新版本、公网 80/443 可达**。默认用服务器 IP 访问，不需要域名；也可以加上你自己的域名（例如 `panel.example.com`，由 Caddy 自动申请和续期证书），IP 和域名可以同时使用。证书签发需要时间；脚本会报告入口是否已就绪，失败可查看其提示的日志。[使用域名、直连说明与关闭方式](PUBLIC_ACCESS.md)

<details>
<summary>方式二：通过 SSH 隧道访问（无需公开面板）</summary>

安装完后，在**你电脑上**另开一个终端，粘贴安装器显示的 SSH 连接命令并保持运行。在线安装器会根据当前 SSH 连接填写地址、用户与端口；若通过跳板机或内网连接，请改用你平时能登录服务器的地址。手动填写示例：

   ```powershell
   ssh -N -o ExitOnForwardFailure=yes -L 127.0.0.1:8787:127.0.0.1:8787 你的SSH用户@服务器IP
   ```

用你现有的 SSH 密码登录，然后在浏览器打开 [http://127.0.0.1:8787](http://127.0.0.1:8787)。面板登录后可修改面板密码并开启两步验证。

</details>

在线安装默认配置公网 HTTPS 管理入口；选择 `JUNPANEL_ACCESS=local` 会关闭面板专用的公网入口并保留本机访问。完整安装包内的手动 `install.sh` 仍只安装本机服务。SSH 密码登录方式保持原样。

你的现有 SSH 密码登录方式、22 端口、Fail2ban、nftables、防火墙策略、Swap、自动安全更新和 Docker 日志限制不会由安装器配置或修改。备份和重启可在“计划任务”页按时间表自动执行，面板本身不创建其它定时任务。

安装器会给现有 `/etc/caddy/Caddyfile` 末尾追加一条 `import /etc/caddy/junpanel.d/*.caddy`，校验后重载；保留原有站点块。安装或变更失败时会尝试恢复本次修改，不提供对外部并发编辑的事务保证。安装过程中请勿同时编辑 Caddyfile。

## 使用范围

| 模块 | 本版实际支持 |
| --- | --- |
| 概览 | 网站、应用、容器与告警计数；负载、CPU、内存、根分区的状态仪表；CPU、内存、上下行流量趋势（5 秒采样、内存保留 120 点，约 10 分钟）；系统信息、Caddy/Docker 状态、网站与最近任务 ；**监控历史**：除实时图外可切换 1 小时 / 6 小时 / 24 小时 / 7 天，每分钟记录一次平均值（CPU、内存、磁盘、流量），保存在代理私有目录，重启不丢，超过 7 天自动清理 |
| 网站 | 创建、编辑、启停、移除静态网站、反向代理及跳转（301/302，可保留路径）；多域名；域名冲突检查；高级设置：密码访问（bcrypt）、IP 白名单、关闭压缩；Caddy 校验与重载；失败回退；现有域名只读概览，并显示每个网站是否在运行（由面板向本机 Caddy 询问，不访问外网）和每个域名当前解析到哪个 IP（指向本机、其它服务器或没有解析）；“证书”页列出 Caddy 证书与到期时间 |
| 应用商店 | 独立编写的 23 种模板：网站与协作（WordPress、Nextcloud）、效率工具（Vaultwarden、Memos、Trilium Notes、linkding、FreshRSS、Excalidraw）、影音（Jellyfin、Navidrome）、开发工具（Gitea、code-server、Stirling-PDF、draw.io、IT Tools）、运维（Uptime Kuma、Gotify、ntfy）、数据库（PostgreSQL、MySQL、Redis）、AI（n8n、Open WebUI）；安装、启停、重启、重新部署、**升级**（先自动备份，再拉取新镜像并重建）、卸载保留数据、恢复部署、绑定域名、**应用设置**（改本机端口、关闭 Vaultwarden/Open WebUI/Gitea 的新用户注册、添加环境变量，保存后自动重新部署；面板生成的数据库密码不可改，密码类变量不回显） |
| 容器 | 现有容器列表与状态、启停重启、最近日志；“编排”页新建和编辑 Compose 编排（保存前逐项安全检查），部署、拉取更新、启停、停用、删除，并分组展示其它 Compose 项目；“镜像”页列表、拉取及非强制删除；“网络”“卷”页列表、创建和非强制删除 |
| 文件 | 限定 `/srv/junpanel`；浏览、新建文件夹/文本、上传、下载、UTF-8 编辑、版本冲突检查、重命名/移动、回收站（恢复、永久删除、清空，30 天自动清理）；上传与下载均为流式，单文件上传上限 512 MiB |
| 数据库 | 应用商店安装的 PostgreSQL/MySQL/WordPress 内部 MariaDB；执行 SQL、创建数据库、备份（流式导出压缩 SQL）、从备份恢复到原库或新库；Redis 的 INFO/DBSIZE/PING；再次验证面板密码后查看连接凭据 |
| 备份 | 手动备份静态网站文件、有数据的应用、数据库 SQL；列表、下载、校验、删除；网站、应用和数据库都可一键恢复（恢复前自动创建“恢复前备份”） ；**异地备份**：在“备份”页设置 WebDAV（坚果云、Nextcloud 等）或 S3 兼容存储（AWS、Cloudflare R2、阿里云 OSS、MinIO），可测试连接，新备份（含计划任务产生的）自动上传，也可对单份备份手动上传；上传失败不影响本地备份，会在列表里标出原因；密钥只存放在代理私有目录且不回显（暂不支持 SFTP） |
| 计划任务 | 按时间表自动备份（网站、应用、数据库）或重启应用；每天、每周、每隔 N 小时；保留最近 N 份；立即运行、停用、编辑；最近结果可见 |
| 告警 | 磁盘空间、证书到期、容器意外停止、Docker 与 Caddy 服务状态、网站无法访问（连续约 2 分钟打不开才告警）；面板内显示当前告警和事件，并可推送到 Webhook（通用 JSON、企业微信、钉钉、飞书机器人）；不发送邮件 |
| 版本升级（面板设置 → 版本升级） | 检查新版本（默认每 6 小时，可选每小时到每周，也可关闭；手动检查会提示结果）；有新版本时在面板内校验并一键升级（带进度条，已登录即可，不再重复输入密码）；安装失败自动回滚 |
| 容器终端（容器 → 终端） | 运行中容器的 `/bin/sh` 基础行终端、输入输出、Ctrl+C、断开；一次性连接票据与并发限制 |
| 日志 | “服务日志”页：直接看 Caddy、面板管理代理、网页服务、Docker、面板升级的最近日志，可按级别筛选、搜索、自动刷新；“任务”页：应用/镜像/容器/编排任务结果与有界输出；“操作日志”：谁在何时、从哪个 IP 做了什么修改及结果（只记操作对象，不记密码、SQL、文件或编排内容）；“登录日志”：登录成功、失败、限速和退出 |
| 公网入口（面板设置 → 公网入口） | 在面板内设置、更换或移除面板的域名和 IP 入口，可同时保留两者；需再次输入密码；应用前检查域名解析，失败自动恢复 |
| 账号安全（面板设置 → 安全） | 单管理员、密码哈希、按来源 IP 登录限速、服务器端会话、闲置/绝对过期、CSRF、同源校验、TOTP 两步验证（动态码一次性使用） |

本版不提供全盘文件管理、宿主机 Web Shell、特权或宿主机网络的 Compose 编排、自定义 Caddy 配置片段、现有外部数据库纳管、上传任意 SQL 文件导入、Redis 任意键编辑或整套生产级数据库迁移工具。GitHub 自动部署、多用户授权和更多应用模板可在此结构上扩展，当前界面没有占位操作入口。

## 备份与恢复

在左侧 **备份** 页创建，作为后台任务运行，进度见“任务日志”。备份保存在服务器的 `/var/lib/junpanel-agent/backups`（root 私有），每份带 SHA-256，下载和恢复前都会校验，文件被改动会拒绝使用。**备份与应用数据在同一块磁盘，不是异地备份，请下载后另行保存。** 创建备份前会检查磁盘至少有 512 MiB 空闲；写入失败会删除不完整的文件。

| 类型 | 内容 | 恢复 |
| --- | --- | --- |
| 网站文件 | 静态网站目录（`.tar.gz`），不含符号链接 | 解出到临时目录后整体切换，原目录被替换 |
| 应用数据 | 应用的数据目录（WordPress 为网站文件和 MariaDB 两处），`.tar.gz` | 移除容器与数据卷，重建后写回数据，再启动 |
| 数据库 | 单个数据库的 SQL（`.sql.gz`），不限 8 MiB | 恢复到原数据库或填写的新库名：目标库已存在时先备份，再删除重建并在一个事务（PostgreSQL）中导入 |

- **应用备份会短暂停止应用**，以得到一致的数据；完成后自动启动，原来就是停止的则保持停止。这是物理拷贝，恢复要求同一应用、同一镜像主版本。
- **恢复会覆盖现有数据。** 开始前会自动创建一份“恢复前备份”，恢复中途失败时用它回滚；任务输出和应用状态会给出它的编号。应用恢复期间该应用处于“进行中”，不能同时执行其它操作。
- 备份不包含应用凭据，也不包含 Compose 配置，只能恢复到面板里仍存在的同一个应用。服务器重装后需要先在面板中以同样的标识重新安装应用（数据库密码由初始化数据决定，请保存好原来的连接信息）。
- 也可以用原生工具手动导入（PostgreSQL / MySQL，`容器` 为容器名，先解压）：

```bash
gunzip -c database-blog-20261020-101500.sql.gz | docker exec -i -e PGPASSWORD=密码 容器 psql -U junpanel -d 库名
gunzip -c database-blog-20261020-101500.sql.gz | docker exec -i -e MYSQL_PWD=密码 容器 mysql -uroot 库名
```

## 计划任务

在左侧 **计划任务** 页创建。只有两类内置任务，**不能执行任意命令**：定时备份（网站文件、应用数据、数据库 SQL，规则同上）和定时重启应用。

- **时间**：每天、每周某天，或每隔 1–168 小时。每天和每周的时间按服务器本地时区计算（页面上会显示时区，例如 `CST +0800`）；间隔从每次开始运行时算起。夏令时切换日不会漏跑或重复跑。
- **补跑**：到点时面板或服务器停机，恢复后只补跑一次，不会把错过的每一次都补上。刚启动的 20 秒内不会开始任务。
- **重试**：目标应用正忙，或后台任务已满（最多同时 2 个）时，每 10 分钟重试一次，最多 3 次，然后等下一个时间点。目标已被删除、被保护等永久性错误不重试，结果显示在列表中。
- **保留份数**：定时备份完成后，只清理**这个计划任务**创建的备份，保留最近 N 份（1–60，默认 7）。手动备份和“恢复前备份”不受影响，也不会被清理。删除计划任务不会删除它已创建的备份，之后这些备份不再自动清理。
- **同一任务不会重叠**：上一次还没结束就到点，这次会被跳过并记录。应用类备份仍会短暂停止应用，建议安排在夜间；最多 50 个计划任务。
- 管理代理重启时仍在运行的任务会被标记为“失败（被中断）”，请核对结果；任务输出可在“任务日志”查看，记录保存在代理内存中，重启后清空。
- 计划保存在 `/var/lib/junpanel-agent/schedules.json`（root 私有）。该文件损坏时管理代理会拒绝启动，而不是静默丢弃计划。

## 告警

左侧 **告警** 页。面板每 30 秒检查一次（启动后约 45 秒开始），当前告警显示在该页、概览页的横幅和侧栏角标上；配置了 Webhook 才会推送。四类检查可分别开关：

| 检查 | 触发条件 | 恢复 |
| --- | --- | --- |
| 磁盘空间 | 系统根分区、数据目录所在分区已用比例达到阈值（默认 90%，可设 50–99%）；同一分区只提醒一次 | 降到阈值以下 3 个百分点，并连续 2 次检查正常 |
| 证书到期 | Caddy 为**仍在使用的域名**保存的证书，剩余时间不足提前天数（默认 14 天，1–60）或已过期。有效期不足 30 天的短期证书（例如 Caddy 为 IP 地址申请的 6 天证书）只在最后 1/5 有效期内提醒，避免一直告警 | 证书续期后 |
| 容器停止 | 上一次检查时在运行、现在已停止且退出码非 0；或处于反复重启、dead 状态。一次性（`oneoff`）容器不检查，退出码为 0 的正常退出不提醒 | 容器重新运行并连续 2 次检查正常 |
| 服务 | Docker 连续 3 次无法访问；`caddy.service` 连续 2 次不是 active | 恢复后连续 2 次检查正常 |

- **面板自己的操作不会误报**：在面板里停止、重启容器或项目，应用的停止/重启/卸载/备份/恢复期间，都不会提醒。**在服务器上手动执行 `docker stop` 会提醒一次**（退出码通常为 137 或 143）。
- **不会刷屏**：同一时刻的多条通知合并成一条；未解决的告警每隔 12 小时（1–168 小时）提醒一次；刚恢复 30 分钟内又出现的，只在面板记录、不再推送。Webhook 发送失败时每 5 分钟重试，最多 3 次，之后放弃并在页面显示结果。通知状态保存在 `/var/lib/junpanel-agent/alerts-state.json`，重启管理代理不会重复推送已有的告警，代理停机期间容器挂掉也能在恢复后发现。
- **Webhook**：支持 `http://` 和 `https://`（本机地址也可以，例如面板里装的 Uptime Kuma）。地址常含密钥，保存后不再显示，也不写入日志；不跟随重定向；企业微信、钉钉、飞书返回的错误码会被识别为失败。钉钉请使用“自定义关键词”并包含 `JunPanel`，暂不支持“加签”。**不支持邮件。**
- **证书来源**：读取 Caddy 数据目录下的 `certificates`（默认 `/var/lib/caddy/.local/share/caddy`，可用 `--caddy-data` 修改），再用 Caddy 配置里的域名过滤掉已不使用的旧证书；读不到域名列表时检查全部证书。页面的“证书”列表同样来自这里；证书检查每 10 分钟刷新一次。
- 页面上的“发送测试通知”会用已保存的地址发一条消息；“立即检查”马上执行一次检查。

## 面板内升级

左侧 **面板设置 → 版本升级** 页显示当前版本和最新版本，有新版本时概览页和侧栏会提示。点击“升级到 vX”并确认即可升级，已登录的管理员无需再次输入密码，弹窗会显示进度条；也可以继续使用一键安装命令。**这个功能从包含它的版本开始提供，更早的版本需要先用一键安装命令升级一次。**

- **版本和校验值只有一个来源**：管理代理读取 GitHub 上 `main` 分支 `quick-install.sh` 里固定的版本号和安装包 SHA-256，也就是一键安装命令信任的同一个位置。这个固定值只在新版本发布并核对完成后才会合并，所以面板看到的新版本一定是可以安装的。请求里不能指定版本或下载地址。
- **升级过程**：下载安装包（HTTPS，最多跟随 5 次重定向，不会降到 HTTP）→ 校验 SHA-256 → 安全解包（拒绝路径穿越、链接和设备文件，大小有上限）→ 运行包内程序确认版本一致 → 用 `systemd-run` 在代理之外启动安装包里的 `install.sh`。任何一步失败都不会改动现有安装；安装器自己失败时会回滚，沿用它原有的恢复逻辑。
- **会重启**：面板和管理代理会停止约 1 分钟，现有应用容器和 Caddy 不受影响；登录会话、限速记录和内存里的任务日志会清空；账号、数据、备份、计划任务和告警设置保留。升级完成后页面会自动重新加载；从 v0.1.11 起，其它已打开的面板页面在下一次请求时发现版本变化，也会自动刷新一次，不会继续运行旧界面。
- **有任务在运行时不能升级**：备份、应用部署等后台任务、正在操作的应用、正在运行的计划任务都会阻止升级，页面会列出是哪一项；降级和重装同一版本也不允许。
- **只更新程序，不重新配置公网入口**：升级只运行 `install.sh`，不会重新运行公网 HTTPS 配置。如果发布说明提示需要更新公网入口配置，请重新执行一键安装命令。
- **升级日志**：安装器的输出保存在 `/var/lib/junpanel-agent/upgrade/upgrade.log`，升级页会显示最近的内容。升级超过 15 分钟没有结果会被标记为失败。
- **不满足条件时**：服务器没有 `systemd-run` 时按钮不可用，页面会说明并提示改用一键安装命令。可以在页面上关闭自动检查更新（检查只读取 GitHub 上公开的文件，不发送你的服务器信息）。

## 从应用到域名

1. **应用商店 → 全部应用 → 安装**，填名称、标识和空闲的本机端口。
2. 等待任务完成，在“已安装”中确认容器状态。应用冷启动与数据库初始化可能晚于容器启动；请结合日志确认就绪。
3. 将你的域名解析到服务器，再点应用上的 **绑定域名**。上游会预填 `http://127.0.0.1:应用端口`。
4. 保存后由系统 Caddy 申请证书并提供 HTTPS。DNS 解析及公网 80/443 的可达性需自行具备，面板不会添加防火墙规则。

创建静态网站后点击“文件”上传网页或编辑 `index.html`。移除站点只移除配置，静态文件保留。此处的静态目录并不是应用容器数据卷；WordPress 等应用数据存放在 Docker 命名卷中。

应用模板端口仅绑定 `127.0.0.1`。数据库连接信息中的 `127.0.0.1` 指的是服务器：电脑端数据库客户端也需通过 SSH 转发数据库端口。WordPress 的 MariaDB 不发布宿主机端口，使用项目内部网络。

## 安装检查、改端口和更新

仅做安装前检查，不写服务和配置：

```bash
sudo bash junpanel/install.sh --check
```

换用空闲面板端口，例如 8877：

```bash
sudo JUNPANEL_PORT=8877 bash junpanel/install.sh
```

SSH 转发两端及浏览器地址也都改用 8877。后续重新执行安装器会沿用已保存的端口；仅当显式设置 `JUNPANEL_PORT` 时改端口。

更新时上传新完整安装包，解压到另一个目录，再执行其中的 `install.sh`。安装器识别自己的安装标记，保留账号、站点登记、应用凭据和数据，不重装应用镜像。更新会短暂重启面板，关闭面板终端与内存会话/任务日志，不停止现有应用和 Caddy。请等后台任务结束后更新。

“重新部署”使用 `docker compose up --detach --no-build --pull missing`，不主动更新已存在的镜像；模板采用版本系列标签，初次拉取的具体补丁版本由镜像仓库决定。生产环境如需固定 digest，可自行修改 `apps.go` 中的模板后重新构建。

## 常用维护

```bash
# 两个面板服务的状态与最近日志
sudo systemctl status junpanel-agent junpanel-web --no-pager
sudo journalctl -u junpanel-agent -u junpanel-web -n 100 --no-pager

# 重置面板密码，同时关闭面板两步验证并使旧会话失效
# 此命令只改变面板账号，不改变 SSH 密码。
sudo -u junpanel-web /opt/junpanel/junpanel password

# 仅停止面板，现有应用和 Caddy 继续运行
sudo systemctl disable --now junpanel-web junpanel-agent

# 再次开启
sudo systemctl enable --now junpanel-agent junpanel-web
```

登录日志记录结果与来源 IP，例如 `login result=failed ip=203.0.113.5`，可供 Fail2ban 等工具使用（面板不会自行配置 Fail2ban）。过滤规则示例：

```ini
# /etc/fail2ban/filter.d/junpanel.conf
[Definition]
failregex = login result=(failed|throttled) ip=<HOST>$
journalmatch = _SYSTEMD_UNIT=junpanel-web.service
```

请勿把重置密码命令的输出粘贴到公开日志或反馈中。重置应以上面指定的 `junpanel-web` 用户执行，以保持认证文件归属正确。

移除面板程序与服务（保留应用、站点和数据，可再次安装）：

```bash
sudo bash junpanel/uninstall.sh
```

容器保护标签为 `junpanel.protected=true`，会阻止容器/项目/应用操作、数据库执行和终端进入。要解除保护，请通过你原有的 Compose 配置与 SSH 维护流程重建容器标签。

## 路径与权限

| 路径 | 用途 |
| --- | --- |
| `/opt/junpanel/junpanel` | 一个预编译程序，分别运行网页服务和本机管理代理 |
| `/var/lib/junpanel-web/account.json` | 仅网页服务可读写的账号哈希、TOTP 密钥和版本标记 |
| `/var/lib/junpanel-agent` | root 私有登记文件、应用 Compose 配置和数据库凭据 |
| `/run/junpanel/agent.sock` | root:junpanel、0660 的本机控制通道 |
| `/srv/junpanel/sites` | 面板静态网站目录，网页文件供 Caddy 读取 |
| `/var/lib/junpanel-agent/alerts.json`、`alerts-state.json` | 告警设置（含 Webhook 地址）与当前告警、事件历史，0600 |
| `/var/lib/junpanel-agent/upgrade.json`、`upgrade/` | 更新检查结果与升级记录、下载和解包的临时文件、升级日志，root 私有 |
| `/var/lib/junpanel-agent/schedules.json` | 计划任务及最近一次运行结果 |
| `/var/lib/junpanel-agent/backups` | root 私有的备份文件，每份附带记录与 SHA-256 |
| `/srv/junpanel/.trash` | root 私有回收站，项目移入 30 天后自动永久删除 |
| `/etc/caddy/junpanel.d/managed.caddy` | 面板拥有的站点片段；请通过网站模块编辑 |
| `/etc/caddy/junpanel.d/panel-access.caddy` | 面板自己的公网入口，由 `public-access.sh` 或“面板设置 → 公网入口”管理 |
| `/var/lib/junpanel-agent/stacks`、`/srv/junpanel/stacks` | 编排的 Compose 定义（root 私有）与编排的数据目录（相对路径指向这里） |
| `/var/lib/junpanel-web/audit.log` | 操作与登录日志，0600，超过 2 MiB 时轮换一份 |

网页服务使用 `junpanel-web` 低权限用户；它不加入 docker 组，不直接挂载或访问 Docker socket。独立 root 代理连接系统 Docker，并执行范围固定的管理接口。**代理仍拥有 root 与 Docker 的高权限，网页服务被攻破后也可能滥用代理允许的能力；这一拆分不是强沙箱，也不使 Docker 管理变成低权限操作。** 面板管理员等同于服务器管理员信任级别。

网页程序固定监听 `127.0.0.1`；在线安装器使用 `public-access.sh` 通过现有系统 Caddy 提供公网 IPv4 HTTPS 入口，校验原始 Origin 后适配本机接口，并给会话 Cookie 添加 Secure；请使用这套完整配置，而非仅增加一条普通反向代理。可显式选择本机模式并通过 SSH 隧道访问。公网入口启用后任何人都可到达登录页，仍必须通过管理员认证；登录失败按来源 IP 分别锁定，单个来源无法把管理员锁在门外。

Cookie 是 HttpOnly、SameSite=Strict，令牌仅存服务器内存，界面不将登录凭据写入 localStorage。公网访问使用 HTTPS 和 Secure Cookie；SSH 隧道访问使用本机 HTTP，远程链路由 SSH 加密。请在可信电脑上使用面板。

文件操作使用 Go `os.Root` 限定路径，拒绝 `..` 与逃逸符号链接。它不是针对宿主机 root 的隔离机制，不能防御管理员在目录内预置特殊挂载点或硬链接。容器终端也可能经现有容器的高权限/宿主机挂载影响主机，请只进入可信容器。

会话闲置 30 分钟、最长 8 小时；后台轮询不延长闲置期。同一来源 IP（IPv6 按 /64 计）10 分钟内验证失败 5 次即锁定该来源 10 分钟；全部来源 10 分钟内累计失败 200 次时，暂停最近 30 天内未成功登录过的来源。来源地址取自本机 Caddy 写入的 `X-Forwarded-For`，直连时取连接地址。状态存于网页进程内存，重启后清空。TOTP 支持前后各一个 30 秒时间窗口，需要服务器时钟准确；每个动态码只能使用一次，同一时间窗口内再次登录或验证需等待下一个动态码。终端最多并发 2 个，连接最长 30 分钟；客户端无活动时约 5 分钟后断开。已有终端连接在账号变更时不即时中止，应同时重启面板服务以立即关闭。

## 当前限制与验证状态

- 管理界面中文优先，个别容器/数据库的原生命令输出保留原文。
- 任务最多并行 2 个、保留 80 项，输出保留末尾 128 KiB；任务和指标历史不写入长期数据库。代理重启后会提示核对被中断的应用操作。
- 文本编辑 1 MiB，文件单个上传最大 512 MiB（流式写入，读取超时 30 分钟），下载不限大小，目录最多 5000 项。回收站保留在本机，不是备份；移入 30 天后自动永久删除，也可在回收站中手动永久删除或清空。
- 数据库查询结果上限 8 MiB，过大时拒绝，避免把截断结果当成完整结果。数据库备份与恢复是流式的，不受此限；整站迁移请用原生工具。
- 编排只允许面板能检查的写法：不允许 `privileged`、宿主机网络/PID/IPC 命名空间、额外权限（少数无害权限除外）、设备、GPU、`build`、`env_file`，以及挂载 `/srv/junpanel` 以外的目录；检查在 `nobody` 用户下由 `docker compose config` 规范化后进行，部署的就是检查过的内容。
- 现有 Compose 项目按标签识别，整组操作逐个执行，不重新解释外部 Compose 文件或保证依赖顺序；遇到失败会停止并提示部分完成。一次性容器不参与整组操作。
- 安装器面向 Debian + 标准 systemd `caddy.service`、`caddy` 用户（home 为 `/var/lib/caddy`）、`/usr/bin/caddy`、`/etc/caddy/Caddyfile`、现有 Docker/Compose。自定义 Caddy 环境文件、主配置中的环境变量、非标准路径会拒绝自动安装；其它导入文件中的环境变量同样需要人工核对后，为代理及 Caddy 校验命令配置一致的环境。
- Caddy 管理 API 必须可由现有 `systemctl reload caddy` 正常使用；`admin off` 等定制需先自行处理。安装前可先确认现有 reload 能工作。
- Docker REST 客户端支持 API 1.41–1.56 的协商区间，超出支持范围时明确报错；用户所给 Compose 版本由安装器直接检测，不安装替换版本。
- 已完成 Go 自动测试、静态检查、前端语法检查、浏览器演示交互和响应式检查，以及 Linux 双架构交叉编译。详见 `VALIDATION.md`。**当前本地开发环境没有 Debian/systemd/Docker 守护进程，未进行真实服务器安装、容器启动、数据库初始化、PTY 或真实证书签发联调，也没有部署到你的远程服务器。**

## 自行开发

Go 1.26 或更新版本；浏览器资源直接嵌入程序，不需要前端依赖安装。只有两个通用第三方 Go 模块：`creack/pty` 与 `gorilla/websocket`。

```bash
go test ./...
go vet ./...
bash build.sh

# 本机安全演示：仅模拟数据，所有服务器变更均被拒绝
go run . server --demo --port 8789
```

打开 http://127.0.0.1:8789，演示账号 `demo`、演示密码 `JunPanelPreview2026!`。演示密码只在 `--demo` 生效，正式服务不识别该账号。

主要代码：`web/` 为界面，`webserver.go`/`auth.go` 为认证与代理，`agent.go` 为本机路由，`sites.go` 为 Caddy 事务，`apps.go` 为独立模板，`files.go` 为限定目录文件管理，`docker.go`/`database.go`/`terminal.go` 为容器相关能力。

接口实现依据官方文档：[Caddy 配置导入](https://caddyserver.com/docs/caddyfile/directives/import)、[配置校验与重载](https://caddyserver.com/docs/command-line)、[Caddy 官方 systemd 单元](https://github.com/caddyserver/dist/blob/master/init/caddy.service)、[Docker Compose up](https://docs.docker.com/reference/cli/docker/compose/up/)。模板镜像来自 [WordPress](https://hub.docker.com/_/wordpress)、[Gitea](https://docs.gitea.com/installation/install-with-docker/)、[Uptime Kuma](https://github.com/louislam/uptime-kuma)、[PostgreSQL](https://hub.docker.com/_/postgres)、[MySQL](https://hub.docker.com/_/mysql)、[Redis](https://hub.docker.com/_/redis) 的官方发布来源。

JunPanel 原创代码使用 MIT 许可证；通用依赖及 Go 运行时许可证见 `third-party/`。安装包不包含应用镜像，使用时由你的 Docker 拉取。
