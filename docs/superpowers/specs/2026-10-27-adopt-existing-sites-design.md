# 原有网站真正接管到面板（设计）

日期：2026-10-27

## 目的

建站机（de-web）上 4 个在面板之前就建好的网站，要能像面板新建的网站一样，用同一张"编辑网站"表单维护：名称、域名、上游/目录、访问密码、IP 白名单、压缩、启停、移除。上一版做的"挂接"只能在列表里看到它们，点进去仍是原始配置文本，没达到目的。

用户以后对这几个站要做的是改域名、启停、加访问密码这类站点设置；abc.ljaxm.com 的文件由它自己的 git 同步管理，不需要面板管文件。

## 现状（2026-10-27 在 de-web 上只读核实）

| 站点 | 所在文件 | 面板表达不了的写法 | 判断 |
|---|---|---|---|
| gzfuyuan.com | /etc/caddy/fuyuan.caddy | `tls 手动证书`、`@migration_health`+`respond`、HSTS 头、`reverse_proxy {...header_up X-Forwarded-Proto https}` | 全是迁移遗留或多余 |
| www.gzfuyuan.com | 同上 | `tls 手动证书`、`@migration_health`+`respond` | 迁移遗留，其余是 301 跳转 |
| monitor.ljaxm.com | 同上 | 同 gzfuyuan.com | 同上 |
| （同文件）`http://三个域名` | 同上 | 整段 `redir https://{host}{uri} 308` | 多余，Caddy 默认就做 |
| abc.ljaxm.com | /etc/caddy/kids-english.caddy（由 /srv/kids-english/install.sh 生成） | 目录 /srv/kids-english 在面板文件区外；`@hidden path /.git /.git/* *.sh`+`respond @hidden 404`；`header Cache-Control "no-cache"` | 都是真需求 |

关键事实：
- 三个 fuyuan 站当前对外提供的是手动拷贝的 Let's Encrypt 证书（/etc/fuyuan-migration-20261012/tls/），**2027-01-10 到期，没有任何任务续期**。Caddy 自己在 2026-10-12 已为这三个域名签发了自动续期证书，只是被 `tls` 行盖住。
- `/__migration_health_20261012` 只在迁移切换配置及其备份里出现，没有定时任务或监控使用。
- /srv/kids-english 是 git 工作区，/etc/cron.d/kids-english 每 10 分钟 `git fetch` + `reset --hard`，所以必须挡住 `.git` 和 `*.sh`；它的 App 在线更新会读 `version.json`，`no-cache` 保证及时拿到新版本。
- fuyuan-migration-preview.caddy 没被主配置 import，不生效，不在本次范围。

## 目标 / 非目标 / 反目标

目标
- 4 个站都变成普通面板网站，用现有"编辑网站"表单维护，和 Vaultwarden 一样。
- 消除 2027-01-10 的证书到期隐患。
- 面板新增的能力是通用的（新建静态站也能用），不是为这 4 个站写死的特例。

非目标
- 不让面板管理 abc.ljaxm.com 的文件，不改 kids-english 的 install.sh 和定时任务。
- 不为 HSTS 新增表单项（理由见"方案"）。
- 不处理 fuyuan-migration-preview.caddy。

反目标（即使能跑也算失败）
- 接管后网站行为悄悄改变：abc 的 `.git` 被公开、`version.json` 被浏览器缓存、证书换成会失败的签发方式。
- 削弱面板对静态站目录的安全限制（例如允许在表单里把目录改成 /etc）。
- 改线上配置时网站中断。

## 方案选择

考虑过的做法：
1. **（选定）面板静态站补三样通用能力，abc 真接管；fuyuan 三站清理遗留后用现有接管。** 一套网站模型，一张表单。
2. abc 保持挂接，给挂接站加精简表单直接改原文件。两套模型长期并存；改的是脚本生成的文件，重跑 install.sh 会覆盖表单改动。
3. 改 kids-english 部署，把文件同步进面板文件区。仍需屏蔽路径和禁止缓存，还要改它的脚本和定时任务，多做事少得好处。

用户选择 1。

HSTS：三站始终走 HTTPS，浏览器已按 `max-age=31536000` 缓存了 HSTS，去掉响应头不影响访问；为此加表单项收益很小。这是实现方的判断，用户未反对。

## 设计

### 一、fuyuan 三站：线上清理（不改代码）

在 de-web 上编辑 /etc/caddy/fuyuan.caddy，清理后为：

```
https://gzfuyuan.com {
    encode gzip
    reverse_proxy 127.0.0.1:4321
}
https://www.gzfuyuan.com {
    redir https://gzfuyuan.com{uri} 301
}
https://monitor.ljaxm.com {
    encode gzip
    reverse_proxy 127.0.0.1:18080
}
```

步骤：备份原文件（cp 到 /root/caddy-backup-时间戳/）→ 写入 → `caddy validate --config /etc/caddy/Caddyfile` → 通过才 `systemctl reload caddy`，失败恢复备份。完成后验证：三个域名经 127.0.0.1:443 拿到的证书是 Caddy 自动管理的那张（到期时间 04:47 而不是 02:25）；`curl -I` 状态码与清理前一致。

这一步改线上服务器，用户批准本设计即视为批准这一步。清理后这三个站在面板"原有 Caddy 网站"里会显示为可接管，用现有"接管到面板"即可。

### 二、面板代码：静态网站的三项通用能力

站点模型（sites.go `site`）新增：
- `BlockPaths []string`（json `block_paths`）：屏蔽路径，命中返回 404。
- `NoCache bool`（json `no_cache`）：响应加 `Cache-Control: no-cache`。

不新增"外部目录"字段：外部与否由 `Root` 是否在 `<root>/sites/` 下推算。

**外部目录（只能由接管带入）**
- `siteSave` 已经保留旧 `Root`、新建时强制在文件区创建，表单本来就改不了目录，不需要新逻辑。
- 接管解析（caddyadopt.go `judgeCaddyBlock` 的 static 分支）放宽：目录在文件区外也可接管，但必须是存在的绝对路径目录，且不能是 `/`，也不能在 `/etc /root /boot /proc /sys /dev /run /usr /bin /sbin /lib /var/lib/junpanel-*` 及面板的 State、Data、Fragments 目录之下或等于它们。
- 外部目录的站：前端不显示"文件"按钮；一键备份和单站备份都跳过它（备份只针对 `sites/<id>`，外部目录的文件由它自己的部署管理）；表单显示目录为只读文字，注明"由原有部署管理，面板不管理其中文件"。

**屏蔽路径**
- 校验：最多 20 条；每条 1–100 字符，以 `/` 或 `*` 开头，只含 `A-Za-z0-9 . _ - / * ~`；去重。
- 渲染：`@blocked path <各条>` + `respond @blocked 404`。无访问限制的站放在站点块内；有密码/IP 限制的站放在 `route {}` 最前面（先于 IP 检查和密码），保证命中直接 404。
- 跳转网站不适用，保存时清空。

**禁止缓存**
- 渲染：`header Cache-Control no-cache`，静态和反向代理都可用，跳转网站清空。

**表单（web/app.js `siteForm`）**
- 新增"屏蔽路径"多行输入（每行一条，帮助文字举例 `/.git`、`*.sh`）和"浏览器缓存"选择（正常 / 禁止缓存）。放在"压缩传输"之后。跳转类型时隐藏。

**接管解析补充识别**
- 一对 `@名字 path ...` + `respond @名字 404`（该命名匹配器只含 `path`、只被这一条 respond 使用）→ `BlockPaths`。
- `header Cache-Control no-cache`（参数带引号或不带都认）→ `NoCache`。目前"含有带引号的参数"会整块拒绝，这一种放行，其他带引号的仍拒绝。
- abc.ljaxm.com 原来的 `encode gzip` 渲染后变为 `encode zstd gzip`，属于无害变化。

**接管确认框**
- 若被接管的站点块所在文件不是主 Caddyfile，提示："这段配置来自 <文件>，若有脚本会重新生成这个文件（例如应用的 install.sh），重跑后会出现域名重复、Caddy 拒绝重载。接管后请不要再重跑它，或去掉它生成 Caddy 配置的那一步。"

**挂接的收尾**
- 已挂接的站被接管后，从 links.json 去掉该域名（接管事务成功后清理）。挂接功能本身保留，给仍不能接管的站用。

### 三、测试

- 解析：用 kids-english 的原始写法做样例，接管后 `BlockPaths`=`/.git /.git/* *.sh`、`NoCache`=true、`Root`=/srv/kids-english（测试里用临时目录模拟外部目录）；敏感目录（如 /etc）仍拒绝。
- 渲染：有/无访问限制两种情况下屏蔽路径与禁止缓存的输出，顺序正确。
- 校验：非法屏蔽路径被拒；跳转站清空这两项。
- 备份：外部目录站不出现在一键备份项里。
- 现有接管、挂接测试不回归。

### 四、发布与上线顺序

1. 线上清理 fuyuan.caddy（第一部分），与代码无关，可先做。
2. 代码 PR → CI 绿后合并 → 发版 v1.5.7。
3. 用户在面板里升级到 v1.5.7，然后在"网站"页对 4 个站点"接管到面板"（需要面板登录，由用户操作）。

## 留给实现方的
- 表单两项的具体文案和位置细节。
- 敏感目录列表可按代码里已有的路径常量补充。
