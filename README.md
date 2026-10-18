# JunPanel

![JunPanel](logo-preview.png)

[![CI](https://github.com/prettylove520/JunPanel/actions/workflows/ci.yml/badge.svg)](https://github.com/prettylove520/JunPanel/actions/workflows/ci.yml) · [下载 v0.1.1](https://github.com/prettylove520/JunPanel/releases/tag/v0.1.1) · [MIT 许可证](LICENSE)

中文优先的自托管服务器工作台。独立编写的 Go 后端与原生 HTML/CSS/JavaScript 界面，采用石墨色与薄荷绿设计，支持深浅主题和手机布局。

品牌素材：[矢量标识](web/logo.svg) · [标识预览](logo-preview.png)

界面预览（模拟数据）：[服务器总览](preview.png) · [应用中心](app-center.png)

产品方向参考 KunPanel 的模块覆盖和操作便利性；本项目没有使用 KunPanel 源码、模板、图标、页面或品牌资源，也不是它的改名版本。

## 一条命令安装

安装包已包含 **Linux amd64 / arm64 预编译程序**。服务器无需安装 Go、Node、Python、npm 或构建前端，也不需要再部署一组面板容器。

在 Debian 服务器的 SSH 窗口执行下面这一条命令（root 或有 sudo 权限的用户均可）：

```bash
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/quick-install.sh | bash
```

自动完成下载、校验、解压、环境检查、面板安装、**公网 IPv4 识别和 HTTPS 管理入口配置**。结束时集中显示账号、随机密码和登录链接：证书及面板响应检查通过后可直接打开 `https://服务器IP`，无需再填 IP 或执行另一条配置命令。公网登录页会公开，仍需管理员认证；建议开启两步验证。HTTPS 入口要求现有 Caddy 至少 2.10.1、公网 80/443 可达。

在线安装器固定下载 v0.1.1 安装包，并从固定 Git 提交下载 HTTPS 配置器；两者都使用脚本内固定的 SHA-256 校验，失败不会执行安装。已有安装可重跑同一命令，沿用账号、数据与保存的端口；不会重置原密码。请先等待后台任务结束。

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
   curl -fL -o JunPanel-0.1.1-linux.tar.gz https://github.com/prettylove520/JunPanel/releases/download/v0.1.1/JunPanel-0.1.1-linux.tar.gz
   curl -fL -o JunPanel-0.1.1-SHA256SUMS.txt https://github.com/prettylove520/JunPanel/releases/download/v0.1.1/JunPanel-0.1.1-SHA256SUMS.txt
   sha256sum --ignore-missing -c JunPanel-0.1.1-SHA256SUMS.txt
   ```

   如果服务器无法连接 GitHub，可从[版本下载页](https://github.com/prettylove520/JunPanel/releases/tag/v0.1.1)下载到电脑，再通过 `scp` 上传。

2. 在服务器的 SSH 窗口执行：

   ```bash
   tar -xzf JunPanel-0.1.1-linux.tar.gz && sudo bash junpanel/install.sh
   ```

   安装器自动选择架构、验证程序校验和、检查现有服务、创建随机面板密码并启动两个 systemd 服务。记下终端显示的 `admin` 账号与随机密码。**新安装没有默认密码**。

</details>

### 打开面板

**在线安装完成后，直接打开终端最后显示的“登录链接”**，使用同一处显示的账号与密码登录。无需手填 IP 或重复配置。

**已有旧安装，只配置访问入口：** 在服务器执行下面这一条命令，自动识别 IP、配置 HTTPS 并显示登录链接：

```bash
curl -fsSL https://raw.githubusercontent.com/prettylove520/JunPanel/main/public-access.sh | sudo bash
```

随后使用脚本显示的链接和原有面板账号登录。支持现有 0.1.1 安装，使用系统 Caddy 申请和续签 IP 证书；要求 **Caddy 2.10.1 或更新版本、公网 80/443 可达**。不需要域名。证书签发需要时间；脚本会报告入口是否已就绪，失败可查看其提示的日志。[直连说明与关闭方式](PUBLIC_ACCESS.md)

<details>
<summary>方式二：通过 SSH 隧道访问（无需公开面板）</summary>

安装完后，在**你电脑上**另开一个终端，粘贴安装器显示的 SSH 连接命令并保持运行。在线安装器会根据当前 SSH 连接填写地址、用户与端口；若通过跳板机或内网连接，请改用你平时能登录服务器的地址。手动填写示例：

   ```powershell
   ssh -N -o ExitOnForwardFailure=yes -L 127.0.0.1:8787:127.0.0.1:8787 你的SSH用户@服务器IP
   ```

用你现有的 SSH 密码登录，然后在浏览器打开 [http://127.0.0.1:8787](http://127.0.0.1:8787)。面板登录后可修改面板密码并开启两步验证。

</details>

在线安装默认配置公网 HTTPS 管理入口；选择 `JUNPANEL_ACCESS=local` 会关闭面板专用的公网入口并保留本机访问。完整安装包内的手动 `install.sh` 仍只安装本机服务。SSH 密码登录方式保持原样。

你的现有 SSH 密码登录方式、22 端口、Fail2ban、nftables、防火墙策略、Swap、自动安全更新和 Docker 日志限制不会由安装器配置或修改。面板不创建定时备份。

安装器会给现有 `/etc/caddy/Caddyfile` 末尾追加一条 `import /etc/caddy/junpanel.d/*.caddy`，校验后重载；保留原有站点块。安装或变更失败时会尝试恢复本次修改，不提供对外部并发编辑的事务保证。安装过程中请勿同时编辑 Caddyfile。

## 使用范围

| 模块 | 本版实际支持 |
| --- | --- |
| 服务器总览 | CPU、内存、Swap、系统根分区、网络速率与累计流量的后端数据；界面展示主要指标、Caddy/Docker 状态；5 秒采样、内存保留 120 点，约 10 分钟趋势 |
| 网站 | 创建、编辑、启停、移除静态网站及反向代理；多域名；域名冲突检查；Caddy 校验与重载；失败回退；现有域名只读概览 |
| 应用中心 | 独立编写的 WordPress、Gitea、Uptime Kuma、PostgreSQL、MySQL、Redis 六种精选模板；安装、启停、重启、重新部署、卸载保留数据、恢复部署、绑定域名 |
| Docker | 现有容器列表与状态、启停重启、最近日志、镜像列表、镜像拉取及非强制删除 |
| Compose | 从 Docker 标签识别现有项目，分组展示容器；对现有非一次性容器执行整组启停重启 |
| 文件 | 限定 `/srv/junpanel`；浏览、新建文件夹/文本、上传、下载、UTF-8 编辑、版本冲突检查、重命名/移动、回收站与恢复 |
| 数据库 | 应用中心创建的 PostgreSQL/MySQL/WordPress 内部 MariaDB；执行 SQL、创建数据库、手动导出；Redis 的 INFO/DBSIZE/PING；再次验证面板密码后查看连接凭据 |
| 容器终端 | 运行中容器的 `/bin/sh` 基础行终端、输入输出、Ctrl+C、断开；一次性连接票据与并发限制 |
| 任务记录 | 应用/镜像/容器/项目任务结果与有界输出；应用部署与镜像拉取提供进行中的命令输出 |
| 账号安全 | 单管理员、密码哈希、登录限速、服务器端会话、闲置/绝对过期、CSRF、同源校验、TOTP 两步验证 |

本版不提供全盘文件管理、宿主机 Web Shell、任意 Compose 编辑/导入、Docker 网络/卷编辑、现有外部数据库纳管、SQL 文件导入、Redis 任意键编辑或整套生产级数据库迁移工具。GitHub 自动部署、告警、多用户授权和更多应用模板可在此结构上扩展，当前界面没有占位操作入口。

## 从应用到域名

1. **应用中心 → 应用商店 → 安装**，填名称、标识和空闲的本机端口。
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

更新时上传新完整安装包，解压到另一个目录，再执行其中的 `install.sh`。安装器识别自己的安装标记，保留账号、站点登记、应用凭据和数据，不重装应用镜像。更新会短暂重启面板，关闭面板终端与内存会话/任务记录，不停止现有应用和 Caddy。请等后台任务结束后更新。

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
| `/srv/junpanel/.trash` | root 私有回收站，不设自动清理策略 |
| `/etc/caddy/junpanel.d/managed.caddy` | 面板拥有的站点片段；请通过网站模块编辑 |

网页服务使用 `junpanel-web` 低权限用户；它不加入 docker 组，不直接挂载或访问 Docker socket。独立 root 代理连接系统 Docker，并执行范围固定的管理接口。**代理仍拥有 root 与 Docker 的高权限，网页服务被攻破后也可能滥用代理允许的能力；这一拆分不是强沙箱，也不使 Docker 管理变成低权限操作。** 面板管理员等同于服务器管理员信任级别。

网页程序固定监听 `127.0.0.1`；在线安装器使用 `public-access.sh` 通过现有系统 Caddy 提供公网 IPv4 HTTPS 入口，校验原始 Origin 后适配本机接口，并给会话 Cookie 添加 Secure；请使用这套完整配置，而非仅增加一条普通反向代理。可显式选择本机模式并通过 SSH 隧道访问。公网入口启用后任何人都可到达登录页，仍必须通过管理员认证；登录失败锁定为全局共享，公开后也可能被外部请求触发。

Cookie 是 HttpOnly、SameSite=Strict，令牌仅存服务器内存，界面不将登录凭据写入 localStorage。公网访问使用 HTTPS 和 Secure Cookie；SSH 隧道访问使用本机 HTTP，远程链路由 SSH 加密。请在可信电脑上使用面板。

文件操作使用 Go `os.Root` 限定路径，拒绝 `..` 与逃逸符号链接。它不是针对宿主机 root 的隔离机制，不能防御管理员在目录内预置特殊挂载点或硬链接。容器终端也可能经现有容器的高权限/宿主机挂载影响主机，请只进入可信容器。

会话闲置 30 分钟、最长 8 小时；后台轮询不延长闲置期。5 次连续验证失败会锁定 10 分钟；状态存于网页进程内存，重启后清空。TOTP 支持前后各一个 30 秒时间窗口，需要服务器时钟准确。终端最多并发 2 个，连接最长 30 分钟；客户端无活动时约 5 分钟后断开。已有终端连接在账号变更时不即时中止，应同时重启面板服务以立即关闭。

## 当前限制与验证状态

- 管理界面中文优先，个别容器/数据库的原生命令输出保留原文。
- 任务最多并行 2 个、保留 80 项，输出保留末尾 128 KiB；任务和指标历史不写入长期数据库。代理重启后会提示核对被中断的应用操作。
- 文本编辑 1 MiB，文件上传 4 MiB、下载 8 MiB，目录最多 5000 项。回收站保留在本机，不是备份；没有自动永久清理。
- 数据库查询与导出结果上限 8 MiB，过大时拒绝，避免把截断结果当成完整导出。大规模导入导出、整站迁移请用原生工具。
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
