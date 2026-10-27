package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Sites that were in the main Caddyfile before the panel can be adopted: the panel reads each
// top-level site block, and when it is made only of things the panel can represent (one reverse proxy,
// redirect or static folder, compression), it moves the site into its own list and removes the block
// from the Caddyfile in the same step. Anything else is explained and left to the Caddyfile editor.
type caddyBlock struct {
	ID        string   `json:"id"`
	File      string   `json:"file"`
	Hosts     []string `json:"hosts"`
	Kind      string   `json:"kind,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Info      string   `json:"info,omitempty"`
	Adoptable bool     `json:"adoptable"`
	Reason    string   `json:"reason,omitempty"`
	start     int
	end       int
	site      site
}

var caddyPlaceholder = regexp.MustCompile(`\{[A-Za-z0-9_.$>\[\]\-]+\}`)

// codeOnly drops a trailing comment and masks quoted text and {placeholders}, leaving what decides the braces.
func codeOnly(line string) string {
	var b strings.Builder
	quote := rune(0)
	prev := ' '
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
			b.WriteRune('_')
		case r == '"' || r == '`':
			quote = r
			b.WriteRune('_')
		case r == '#' && (prev == ' ' || prev == '\t'):
			return caddyPlaceholder.ReplaceAllString(b.String(), "_")
		default:
			b.WriteRune(r)
		}
		prev = r
	}
	return caddyPlaceholder.ReplaceAllString(b.String(), "_")
}

func braceDelta(line string) int {
	return strings.Count(line, "{") - strings.Count(line, "}")
}

// parseCaddyBlocks finds the top-level site blocks of a Caddyfile and judges each one.
func (a *agent) parseCaddyBlocks(content string) []caddyBlock {
	lines := strings.Split(content, "\n")
	var out []caddyBlock
	for i := 0; i < len(lines); i++ {
		head := strings.TrimSpace(codeOnly(lines[i]))
		if head == "" || !strings.HasSuffix(head, "{") || braceDelta(head) != 1 {
			continue
		}
		depth, j := 1, i+1
		for ; j < len(lines); j++ {
			depth += braceDelta(codeOnly(lines[j]))
			if depth <= 0 {
				break
			}
		}
		if depth != 0 {
			break // unbalanced: stop rather than guess
		}
		addr := strings.TrimSpace(strings.TrimSuffix(head, "{"))
		if addr != "" && !strings.HasPrefix(addr, "(") && !strings.HasPrefix(addr, "@") {
			block := a.judgeCaddyBlock(addr, lines[i+1:j])
			block.start, block.end = i, j
			block.Info = describeCaddyBody(lines[i+1 : j])
			sum := sha256.Sum256([]byte(strings.Join(lines[i:j+1], "\n")))
			block.ID = hex.EncodeToString(sum[:8])
			out = append(out, block)
		}
		i = j
	}
	return out
}

func (a *agent) judgeCaddyBlock(addr string, body []string) caddyBlock {
	b := caddyBlock{}
	no := func(reason string) caddyBlock { b.Reason = reason; return b }
	// Hosts are kept as bare names so they match what the panel lists; what makes an address something
	// the panel cannot represent (plain http, a port, a wildcard) is remembered and reported below.
	problem := ""
	for _, tok := range strings.FieldsFunc(addr, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		h := strings.ToLower(tok)
		switch {
		case strings.HasPrefix(h, "https://"):
			h = strings.TrimPrefix(h, "https://")
		case strings.HasPrefix(h, "http://"):
			h = strings.TrimPrefix(h, "http://")
			problem = "地址是 http://（不走 HTTPS）：" + truncate(tok, 40)
		}
		h = strings.TrimSuffix(h, "/")
		if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h, "]") {
			h = h[:i]
			problem = "地址带有端口：" + truncate(tok, 40)
		}
		if h == "" {
			continue
		}
		if strings.HasPrefix(h, "*") || strings.ContainsAny(h, "*{}/") {
			problem = "地址含有通配符或占位符：" + truncate(tok, 40)
		}
		b.Hosts = append(b.Hosts, h)
	}
	if len(b.Hosts) == 0 {
		return no("没有域名")
	}
	if problem != "" {
		return no(problem)
	}
	for _, h := range b.Hosts {
		if !domainName.MatchString(h) {
			return no("地址不是完整域名：" + truncate(h, 40))
		}
	}
	var main, upstream, target, root string
	compress, permanent, keepPath, noCache := false, false, false, false
	// A path-only named matcher answered by exactly one "respond @name 404" is a blocked-path rule.
	matchers, responds, order := map[string][]string{}, map[string]int{}, []string{}
	for _, raw := range body {
		line := strings.TrimSpace(codeOnly(raw))
		if line == "" {
			continue
		}
		if strings.ContainsAny(line, "{}") {
			return no("含有嵌套的配置块")
		}
		f := strings.Fields(strings.TrimSpace(stripCaddyComment(raw)))
		if len(f) == 0 {
			continue
		}
		if len(f) == 3 && f[0] == "header" && strings.EqualFold(f[1], "Cache-Control") && f[2] == `"no-cache"` {
			f[2] = "no-cache"
		}
		if strings.ContainsAny(strings.Join(f, " "), "\"`") {
			return no("含有带引号的参数（" + f[0] + "）")
		}
		if strings.HasPrefix(f[0], "@") {
			if len(f) < 3 || f[1] != "path" || matchers[f[0]] != nil {
				return no("含有面板没有对应设置的匹配规则（" + truncate(f[0], 30) + "）")
			}
			matchers[f[0]], order = f[2:], append(order, f[0])
			continue
		}
		switch f[0] {
		case "respond":
			if len(f) != 3 || !strings.HasPrefix(f[1], "@") || f[2] != "404" {
				return no("respond 不是“屏蔽路径返回 404”的形式")
			}
			responds[f[1]]++
		case "reverse_proxy":
			if len(f) != 2 || main != "" {
				return no("reverse_proxy 带有多个目标或参数")
			}
			main, upstream = "proxy", f[1]
		case "redir":
			if len(f) < 2 || len(f) > 3 || main != "" || !strings.HasPrefix(f[1], "http") {
				return no("redir 不是“跳转到完整网址”的形式")
			}
			main, target = "redirect", f[1]
			if len(f) == 3 {
				switch f[2] {
				case "permanent", "301":
					permanent = true
				case "temporary", "302":
				default:
					return no("redir 的跳转方式只支持 301 或 302")
				}
			}
		case "root":
			if len(f) != 3 || f[1] != "*" {
				return no("root 带有匹配条件")
			}
			root = f[2]
		case "file_server":
			if len(f) != 1 || main != "" {
				return no("file_server 带有参数")
			}
			main = "static"
		case "encode":
			for _, x := range f[1:] {
				if x != "gzip" && x != "zstd" {
					return no("encode 使用了面板不支持的算法（" + truncate(x, 20) + "）")
				}
			}
			compress = true
		case "header":
			switch {
			case len(f) == 3 && strings.EqualFold(f[1], "X-Content-Type-Options") && f[2] == "nosniff":
			case len(f) == 3 && strings.EqualFold(f[1], "Cache-Control") && f[2] == "no-cache":
				noCache = true
			default:
				return no("含有 header 指令")
			}
		default:
			return no("含有面板没有对应设置的指令：" + truncate(f[0], 30))
		}
	}
	if main == "" {
		return no("没有找到 reverse_proxy、redir 或 file_server")
	}
	if main != "static" && root != "" {
		return no("root 只能和 file_server 一起使用")
	}
	var blocked []string
	for _, name := range order {
		if responds[name] != 1 {
			return no("匹配规则 " + truncate(name, 30) + " 不是只用于“返回 404”")
		}
		blocked = append(blocked, matchers[name]...)
	}
	for name := range responds {
		if matchers[name] == nil {
			return no("respond 用到了没有定义的匹配规则（" + truncate(name, 30) + "）")
		}
	}
	if main == "redirect" && (len(blocked) > 0 || noCache) {
		return no("跳转网站不支持屏蔽路径或禁止缓存")
	}
	s := site{Name: truncate(b.Hosts[0], 60), Domains: append([]string{}, b.Hosts...), Kind: main, NoCompress: !compress, Enabled: true, BlockPaths: blocked, NoCache: noCache}
	switch main {
	case "proxy":
		if !strings.Contains(upstream, "://") {
			upstream = "http://" + upstream
		}
		s.Upstream = upstream
		b.Summary = "反向代理 → " + upstream
	case "redirect":
		if strings.HasSuffix(target, "{uri}") {
			target, keepPath = strings.TrimSuffix(target, "{uri}"), true
		}
		s.Target, s.Permanent, s.KeepPath = target, permanent, keepPath
		b.Summary = "跳转 → " + target
		if keepPath {
			b.Summary += "（保留路径）"
		}
	case "static":
		clean := filepath.Clean(root)
		if root == "" || !filepath.IsAbs(clean) || a.sensitiveRoot(clean) {
			return no("静态网站的目录不允许由面板接管（" + truncate(root, 60) + "）")
		}
		if st, e := os.Stat(clean); e != nil || !st.IsDir() {
			return no("静态网站的目录不存在")
		}
		s.Root = clean
		b.Summary = "静态网站 → " + clean
	}
	if e := validSite(&s); e != nil {
		return no(cleanText(e.Error()))
	}
	b.Kind, b.Adoptable, b.site = main, true, s
	return b
}

// stripCaddyComment cuts a trailing comment but leaves quoted text alone.
// sensitiveRoot refuses folders that must never be published as a website: top-level folders,
// system and secret locations, and the panel's own data outside its sites area.
func (a *agent) sensitiveRoot(path string) bool {
	clean := filepath.Clean(path)
	if strings.Count(strings.Trim(filepath.ToSlash(clean), "/"), "/") < 1 {
		return true // "/" and "/srv", "/home"...
	}
	sites := filepath.Join(a.cfg.Root, "sites")
	if clean == sites {
		return true
	}
	if under(clean, sites) {
		return false
	}
	deny := []string{"/etc", "/root", "/boot", "/proc", "/sys", "/dev", "/run", "/usr", "/bin", "/sbin", "/lib", "/lib64", "/var/lib", "/var/log",
		a.cfg.Root, a.cfg.State, a.cfg.Fragments, a.cfg.CaddyData, filepath.Dir(a.cfg.Caddy)}
	for _, d := range deny {
		if d != "" && (clean == filepath.Clean(d) || under(clean, d)) {
			return true
		}
	}
	return false
}

func under(path, dir string) bool {
	return strings.HasPrefix(filepath.Clean(path)+string(filepath.Separator), filepath.Clean(dir)+string(filepath.Separator)) && filepath.Clean(path) != filepath.Clean(dir)
}

func stripCaddyComment(line string) string {
	quote := rune(0)
	prev := ' '
	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '`':
			quote = r
		case r == '#' && (prev == ' ' || prev == '\t'):
			return line[:i]
		}
		prev = r
	}
	return line
}

// caddyBlocks reads every editable Caddy file and lists its site blocks, each tagged with its file.
func (a *agent) caddyBlocks() ([]caddyBlock, map[string][]byte, string, error) {
	files := a.caddyFiles()
	contents, digest, e := a.caddyFilesDigest(files)
	if e != nil {
		return nil, nil, "", e
	}
	var out []caddyBlock
	for _, f := range files {
		for _, b := range a.parseCaddyBlocks(string(contents[f])) {
			b.File = f
			out = append(out, b)
		}
	}
	return out, contents, digest, nil
}

func (a *agent) caddyAdoptable(w http.ResponseWriter, r *http.Request) error {
	all, _, digest, e := a.caddyBlocks()
	if e != nil {
		return e
	}
	panelHosts := map[string]bool{}
	sites, _ := a.readSites()
	for _, s := range sites {
		for _, d := range s.Domains {
			panelHosts[d] = true
		}
	}
	blocks := []caddyBlock{}
	editable := true
	for _, b := range all {
		if len(b.Hosts) > 0 && panelHosts[b.Hosts[0]] {
			continue
		}
		if b.Adoptable && !caddyEditable(b.File) {
			editable = false
		}
		blocks = append(blocks, b)
	}
	send(w, 200, map[string]any{"sha": digest, "blocks": blocks, "linked": a.readLinks(), "editable": editable && caddyEditable(a.cfg.Caddy)})
	return nil
}

type fileChange struct {
	path     string
	old, new []byte
}

func (a *agent) caddyAdopt(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		SHA string   `json:"sha"`
		IDs []string `json:"ids"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if len(req.IDs) == 0 || len(req.IDs) > 20 {
		return bad(400, "请选择 1–20 个要接管的网站")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	all, contents, digest, e := a.caddyBlocks()
	if e != nil {
		return e
	}
	if req.SHA != digest {
		return bad(409, "Caddy 配置在你打开之后被改动过，请刷新页面再试")
	}
	chosen := map[string]bool{}
	for _, id := range req.IDs {
		chosen[id] = true
	}
	byFile := map[string][]caddyBlock{}
	var picked []caddyBlock
	for _, b := range all {
		if chosen[b.ID] {
			if !b.Adoptable {
				return bad(409, strings.Join(b.Hosts, "、")+" 暂不能接管："+b.Reason)
			}
			picked = append(picked, b)
			byFile[b.File] = append(byFile[b.File], b)
			delete(chosen, b.ID)
		}
	}
	if len(chosen) > 0 || len(picked) == 0 {
		return bad(404, "找不到要接管的网站，请刷新页面再试")
	}
	sites, e := a.readSites()
	if e != nil {
		return e
	}
	taken := map[string]bool{}
	for _, s := range sites {
		for _, d := range s.Domains {
			taken[d] = true
		}
	}
	now := time.Now()
	next := append([]site{}, sites...)
	for _, b := range picked {
		for _, d := range b.site.Domains {
			if taken[d] {
				return bad(409, d+" 已经是面板里的网站")
			}
			taken[d] = true
		}
		s := b.site
		s.ID, s.Created = "s_"+randomID(20), now
		next = append(next, s)
	}
	var changes []fileChange
	for _, f := range a.caddyFiles() {
		blocks := byFile[f]
		if len(blocks) == 0 {
			continue
		}
		lines := strings.Split(string(contents[f]), "\n")
		// Replace the blocks, last one first so earlier line numbers stay valid.
		for i := len(blocks) - 1; i >= 0; i-- {
			b := blocks[i]
			note := "# 已转入 JunPanel 管理：" + strings.Join(b.Hosts, ", ") + "（原配置在“Caddy 配置历史版本”里）"
			lines = append(lines[:b.start], append([]string{note}, lines[b.end+1:]...)...)
		}
		updated := strings.Join(lines, "\n")
		if f == a.cfg.Caddy && !a.caddyLinked(updated) {
			return bad(409, "接管后会丢失面板的接入行，已取消")
		}
		changes = append(changes, fileChange{path: f, old: contents[f], new: []byte(updated)})
	}
	if e = a.adoptTransaction(changes, next); e != nil {
		return e
	}
	// An adopted host is now an ordinary panel site; it no longer needs its link.
	if links := a.readLinks(); len(links) > 0 {
		keep := []string{}
		for _, h := range links {
			if !taken[h] {
				keep = append(keep, h)
			}
		}
		if len(keep) != len(links) {
			atomicJSON(a.linksPath(), keep, 0600)
		}
	}
	send(w, 200, map[string]any{"ok": true, "adopted": len(picked)})
	return nil
}

// adoptTransaction changes the Caddy files and the panel's own fragment together, so no host is ever
// defined twice or not at all; a failure anywhere puts every file (and the running Caddy) back.
func (a *agent) adoptTransaction(changes []fileChange, sites []site) error {
	managed := filepath.Join(a.cfg.Fragments, "managed.caddy")
	oldManaged, e := os.ReadFile(managed)
	existed := e == nil
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	for _, c := range changes {
		if !caddyEditable(c.path) {
			return readOnlyFault(syscall.EROFS)
		}
		if e = a.saveCaddyBackup(c.path, c.old); e != nil {
			return bad(500, "无法保存旧配置的备份，已取消修改")
		}
	}
	restore := func() error {
		var failed error
		if existed {
			failed = atomicBytes(managed, oldManaged, 0644)
		} else {
			os.Remove(managed)
		}
		for _, c := range changes {
			if e := writeConfigFile(c.path, c.old); e != nil {
				failed = e
			}
		}
		return failed
	}
	reloadBack := func(message string) error {
		if restore() != nil {
			return bad(500, message+"，且无法还原；请立即通过 SSH 检查 /etc/caddy 下的配置")
		}
		if _, e := a.reloadCaddy(); e != nil {
			return bad(500, message+"，还原后的重载也失败；请立即通过 SSH 检查")
		}
		return nil
	}
	if e = atomicBytes(managed, renderSites(sites, a.authDirective()), 0644); e != nil {
		restore()
		return e
	}
	for _, c := range changes {
		if e = writeConfigFile(c.path, c.new); e != nil {
			restore()
			return e
		}
	}
	if out, e := a.caddyValidate(); e != nil {
		if r := restore(); r != nil {
			return bad(500, "校验失败，且无法还原；请立即通过 SSH 检查 /etc/caddy 下的配置")
		}
		return bad(409, withReason("Caddy 校验未通过，已还原，网站没有变化", out))
	}
	if out, e := a.reloadCaddy(); e != nil {
		if r := reloadBack("重载失败"); r != nil {
			return r
		}
		return bad(409, withReason("Caddy 重载失败，已还原", out))
	}
	if e = atomicJSON(filepath.Join(a.cfg.State, "sites.json"), sites, 0600); e != nil {
		if r := reloadBack("登记失败"); r != nil {
			return r
		}
		return bad(500, "登记失败，已还原")
	}
	return nil
}
