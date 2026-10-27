# 原有网站真正接管到面板 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 de-web 上 4 个原有网站都能用面板的"编辑网站"表单维护：fuyuan 三站清理线上配置后用现有接管；面板静态站新增"屏蔽路径""禁止缓存"并允许接管带入外部目录，使 abc.ljaxm.com 也能接管。

**Architecture:** 站点模型 `site` 加 `BlockPaths`、`NoCache` 两个字段，渲染进 Caddy 片段；外部目录不加字段，由 `Root` 是否在 `<root>/sites/` 下推算。接管解析识别 `@x path…`+`respond @x 404` 和 `header Cache-Control no-cache`，并放宽静态站目录限制（敏感目录除外）。

**Tech Stack:** Go（标准库 + 现有代码），原生 JS 单文件前端，Caddy v2。

**Spec:** `docs/superpowers/specs/2026-10-27-adopt-existing-sites-design.md`

## Global Constraints

- 本机没有 Go：Go 编译与测试只在 GitHub Actions 上跑（`go test -race ./...`、真实 Caddy 测试）；前端只能 `node --check web/app.js`。
- 屏蔽路径：最多 20 条；每条 1–100 字符，以 `/` 或 `*` 开头，只含 `A-Za-z0-9._-/*~`；去重。
- 渲染：屏蔽路径为 `@blocked path <条目…>` + `respond @blocked 404`；禁止缓存为 `header Cache-Control no-cache`；跳转站两项都清空。
- 有访问限制（密码/IP）时，屏蔽路径放在 `route {}` 最前面。
- 外部目录只可能由接管带入；拒绝 `/`，以及等于或位于 `/etc /root /boot /proc /sys /dev /run /usr /bin /sbin /lib /lib64 /var/lib/junpanel-web /var/lib/junpanel-agent` 和面板 `cfg.State`、`cfg.Data`（若有）、`cfg.Fragments` 之下的目录。
- 无新选项的站点渲染输出必须逐字节不变（`TestRenderSitesUnchangedWithoutOptions`）。
- 提交署名 Li Jun <139833659+prettylove520@users.noreply.github.com>。

## Review Focus

1. 屏蔽路径与访问限制同时开启：命中屏蔽路径应直接 404，不先弹密码框。→ Task 2 真实 Caddy 用例 `blocked first`。
2. 屏蔽路径条目注入 Caddy 语法（换行、空格、花括号、引号）：必须被拒。→ Task 2 校验用例。
3. 接管带入 `/etc` 这类敏感目录：必须拒绝接管。→ Task 3 用例。
4. 外部目录站点被备份/恢复：应跳过，不能去打包 `sites/<id>` 不存在的目录或在恢复时建出它。→ Task 4 用例。
5. 编辑外部目录站点并保存：`Root` 保持原值，不被改回文件区。→ Task 2 `siteSave` 已保留旧 Root，Task 4 加断言。

---

### Task 1: 线上清理 fuyuan.caddy（运维，不改仓库）

**Files:** de-web 上 `/etc/caddy/fuyuan.caddy`

- [ ] **Step 1:** 备份：`ssh de-web 'set -e; d=/root/caddy-backup-$(date +%Y%m%d-%H%M%S); mkdir -p $d; cp -a /etc/caddy/fuyuan.caddy $d/'`
- [ ] **Step 2:** 写入设计文档"一"中的三段内容（只保留 `encode gzip` + `reverse_proxy` / `redir … 301`），整段 `http://…` 删除。
- [ ] **Step 3:** `caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile`，失败则从备份恢复并停止。
- [ ] **Step 4:** `systemctl reload caddy`。
- [ ] **Step 5:** 验证：`openssl s_client -connect 127.0.0.1:443 -servername gzfuyuan.com` 等三个域名，证书到期时间为 `04:47`（Caddy 自动管理）而非 `02:25`；`curl -sk -o /dev/null -w '%{http_code}' --resolve 域名:443:127.0.0.1 https://域名/` 与清理前状态码一致（清理前先记录）。

### Task 2: 站点模型、校验、渲染、保存接口

**Files:**
- Modify: `sites.go`（`site` 结构、`publicSite`、`validSite`、`renderSites`、`siteSave` 请求体）
- Test: `sites_test.go`

**Interfaces:**
- Produces: `site.BlockPaths []string` (json `block_paths,omitempty`)、`site.NoCache bool` (json `no_cache,omitempty`)；`publicSite` 多返回 `block_paths`、`no_cache`、`external`（bool，Root 非空且不在 `<root>/sites/` 下）；`validSite` 对两项做校验/规范化，跳转站清空；`POST /sites` 接受 `block_paths`、`no_cache`。
- `publicSite` 需要知道文件区路径才能算 `external`：签名改为 `publicSite(s site, siteRoot string) map[string]any`，调用处传 `filepath.Join(a.cfg.Root, "sites")`。

- [ ] **Step 1: 失败测试**
  - `TestSiteOptionsValidation` 追加：`BlockPaths` 为 `{"/a b"}`、`{"/a\nrespond x"}`、`{"/{x}"}`、`{"a"}`、`{"/\"x"}`、21 条、单条 101 字符时 `validSite` 报错；`{" /.git ", "/.git", "*.sh"}` 规范化为 `["/.git", "*.sh"]`；跳转站 `BlockPaths`、`NoCache` 被清空。
  - 新增 `TestRenderBlockPathsAndNoCache`：无限制静态站 `BlockPaths:["/.git","*.sh"], NoCache:true` 输出含 `  @blocked path /.git *.sh\n  respond @blocked 404\n` 和 `  header Cache-Control no-cache\n`；有 `AllowIPs` 的站，`route {` 后第一行是 `    @blocked path`。
  - `siteOptionsInCaddy` 增加站点 `http://hidden.test`（static，`BlockPaths:["/.git","/.git/*","*.sh"]`，`NoCache:true`）和 `http://hiddenauth.test`（同上再加密码），并在 `root` 下建 `.git/config` 与 `x.sh`。检查：`/.git/config`→404，`/x.sh`→404，`/`→200 且 `Cache-Control == "no-cache"`；`hiddenauth.test` 的 `/.git/config` 不带密码→404（`blocked first`）。
- [ ] **Step 2:** 推送分支，CI 上这些用例失败（编译失败亦可）。
- [ ] **Step 3: 实现**：按 Global Constraints 的规则在 `validSite` 校验（正则 `^[/*][A-Za-z0-9._/*~-]{0,99}$`）；`renderSites` 两个分支各插入；`siteSave` 请求体加两个字段并赋给 `value`；`siteList`/`overview` 等调用 `publicSite` 处传入文件区路径。
- [ ] **Step 4:** CI 上 `go test -race ./...` 与真实 Caddy 用例通过，`TestRenderSitesUnchangedWithoutOptions` 不变。
- [ ] **Step 5:** 提交 `站点支持屏蔽路径与禁止缓存`。

### Task 3: 接管解析与链接清理

**Files:**
- Modify: `caddyadopt.go`（`judgeCaddyBlock`、`caddyAdopt`）
- Test: `caddylink_test.go`（已有 kids-english 样例 `linkFixture`）、`caddyadopt_test.go`

**Interfaces:**
- Consumes: Task 2 的 `site.BlockPaths`、`site.NoCache`。
- Produces: `func (a *agent) sensitiveRoot(path string) bool`（caddyadopt.go），`true` 表示禁止作为站点目录。

- [ ] **Step 1: 失败测试**
  - 改 `TestCaddyBlocksDescribeThemselves`：`linkFixture` 里 abc 的 `root` 改为测试临时目录（fixture 用 `%s` 占位替换），断言 abc `Adoptable==true`、`Kind=="static"`、`site.Root==临时目录`、`site.BlockPaths==["/.git","/.git/*","*.sh"]`、`site.NoCache==true`、`site.NoCompress==false`。
  - 新增：root 为 `/etc/caddy` 的静态块 → `Adoptable==false`、`Reason` 含 `目录`；`@x path /a` 后跟 `respond @x 403` → 不可接管；`@x path /a` 被两条指令引用 → 不可接管；`header Cache-Control "max-age=60"` → 不可接管。
  - `TestLinkKeepsTheConfigUntouched` 之后新增 `TestAdoptRemovesTheLink`：先 `/caddy/link` 挂接 abc，再 `/caddy/adopt` 接管它，`/caddy/adoptable` 返回的 `linked` 不再含 abc。
- [ ] **Step 2:** CI 上失败。
- [ ] **Step 3: 实现**
  - 解析时先收集命名匹配器：`@名 path p1 p2…`（仅 `path`）记下；`respond @名 404` 且该名只被这一条引用 → 并入 `BlockPaths`；其它 `@` 开头行或其它 respond → 维持现有拒绝原因。
  - `header` 分支：`Cache-Control` + 去引号后等于 `no-cache` → `NoCache=true`；引号检查对这一种放行。
  - static 分支：去掉"必须在文件区"的限制，改为 `filepath.IsAbs`、目录存在、`!a.sensitiveRoot(clean)`，否则原因 `静态网站的目录不允许由面板接管（<目录>）`。
  - `caddyAdopt` 事务成功后，从 `links.json` 删去被接管站点的全部域名（用 `readLinks` / `atomicJSON`）。
- [ ] **Step 4:** CI 通过，原有接管用例不回归。
- [ ] **Step 5:** 提交 `接管识别屏蔽路径、禁止缓存与外部目录`。

### Task 4: 备份跳过外部目录站

**Files:**
- Modify: `backup.go`（`staticSite`、`backupItems`）
- Test: `backup_test.go`

**Interfaces:**
- Consumes: Task 2 `external` 的判断方式（`Root` 不在 `<root>/sites/` 下）；抽成 `func (a *agent) externalRoot(s site) bool` 放在 sites.go，`publicSite` 的 `external` 也由调用方用它算（若改动 Task 2 签名更简单，可在 Task 2 就引入此函数）。

- [ ] **Step 1: 失败测试** `TestBackupSkipsExternalSites`：sites.json 写入一个 `Root` 为临时外部目录的静态站，`backupItems` 结果不含它；对它的单站备份请求返回 400 且信息含 `外部目录`。另在 `TestSitePasswordIsHashedKeptAndHidden` 同文件新增保存断言：编辑外部目录站（改名、加屏蔽路径）后 `Root` 不变。
- [ ] **Step 2:** CI 上失败。
- [ ] **Step 3: 实现**：`staticSite` 对外部目录返回 `bad(400, "这个网站的文件在外部目录，由它自己的部署管理，面板不备份")`；`backupItems` 跳过。
- [ ] **Step 4:** CI 通过。
- [ ] **Step 5:** 提交 `备份跳过外部目录的网站`。

### Task 5: 前端表单与列表

**Files:**
- Modify: `web/app.js`（`siteForm`、`sitesPage` 静态站"文件"按钮、`adoptForm`）、`demo.go`（演示数据加一个外部目录静态站，带屏蔽路径）、`README.md`（网站一行补充说明）

- [ ] **Step 1:** `siteForm` 在 `compress` 后加：`blocks=field('屏蔽路径',(site?.block_paths||[]).join('\n'),{area:true,rows:2,required:false,placeholder:'/.git\n*.sh',help:'每行一条，访问这些路径一律返回 404。以 / 开头是路径，*.sh 这类表示按后缀匹配。'})`、`cache=field('浏览器缓存',site?.no_cache?'1':'0',{select:[['0','正常'],['1','禁止缓存（每次都向服务器确认）']]})`；跳转类型时隐藏；提交体加 `block_paths`、`no_cache`。外部目录站在表单顶部 note 追加"文件在 <root>，由原有部署管理，面板不管理其中文件"。
- [ ] **Step 2:** 网站列表：`s.kind==='static' && !s.external` 才显示"文件"按钮；外部目录站的"目录"列显示 `root` 后加"（外部目录）"。
- [ ] **Step 3:** `adoptForm`：若任一块 `b.file` 不等于主配置（`adopt.blocks` 已有 `file`；主配置路径从 `/sites` 的 `caddy_sources` 里 `primary` 项取），列表下追加设计文档里的 install.sh 提醒文字。
- [ ] **Step 4:** `node --check web/app.js` 通过。
- [ ] **Step 5:** 提交 `表单支持屏蔽路径与禁止缓存，外部目录站隐藏文件入口`。

### Task 6: PR、合并、发版

- [ ] **Step 1:** 推送 `claude/adopt-existing-sites`，开 PR，CI 全绿（真实失败先查日志修复；外部服务 503 之类重跑失败任务）。
- [ ] **Step 2:** 合并。
- [ ] **Step 3:** 版本 PR：`main.go`、README 版本号 1.5.6→1.5.7，写 `.github/release-notes/v1.5.7.md`；CI（轻量）绿后合并。
- [ ] **Step 4:** `gh workflow run release.yml -f version=1.5.7 -f prerelease=false`，确认正式版和三件资产。
- [ ] **Step 5:** 核对 `release/pin-v1.5.7` 只改 `quick-install.sh` 两行，开 PR，轻量 CI 绿后合并。
