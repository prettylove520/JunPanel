package main

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

// Linking is the gentle sibling of adopting: a site whose block the panel cannot rewrite (a custom
// certificate, a path filter, a health endpoint...) stays exactly where it is in its Caddy file, and
// the panel only remembers its primary host. The file is never touched, so a script that regenerates it
// cannot clash with the panel; each read finds the block again by host name, not by line number.

const maxLinkedHosts = 100

func (a *agent) linksPath() string { return filepath.Join(a.cfg.State, "links.json") }

func (a *agent) readLinks() []string {
	out := []string{}
	if e := loadJSON(a.linksPath(), &out); e != nil {
		return []string{}
	}
	return out
}

// describeCaddyBody names what a block does in a few words, from the directives at its top level.
func describeCaddyBody(body []string) string {
	var parts []string
	n, depth := 0, 0
	proxy, redir, file, root, auth, custom, internal, rules := "", "", false, "", false, false, false, false
	for _, raw := range body {
		code := strings.TrimSpace(codeOnly(raw))
		if code == "" {
			continue
		}
		if depth == 0 {
			n++
			f := strings.Fields(strings.TrimSpace(stripCaddyComment(raw)))
			if len(f) > 0 {
				switch {
				case f[0] == "reverse_proxy" && len(f) > 1 && proxy == "":
					proxy = strings.TrimSuffix(f[1], "{")
				case f[0] == "redir" && len(f) > 1 && redir == "":
					redir = f[1]
				case f[0] == "file_server":
					file = true
				case f[0] == "root" && len(f) == 3:
					root = f[2]
				case f[0] == "tls" && len(f) >= 3:
					custom = true
				case f[0] == "tls" && len(f) == 2 && f[1] == "internal":
					internal = true
				case f[0] == "basic_auth" || f[0] == "basicauth":
					auth = true
				case strings.HasPrefix(f[0], "@") || f[0] == "respond" || f[0] == "route" || f[0] == "handle" || f[0] == "handle_path":
					rules = true
				}
			}
		}
		depth += braceDelta(code)
	}
	switch {
	case proxy != "":
		parts = append(parts, "反向代理 → "+proxy)
	case redir != "":
		parts = append(parts, "跳转 → "+redir)
	case file && root != "":
		parts = append(parts, "静态 → "+root)
	case file:
		parts = append(parts, "静态网站")
	}
	if custom {
		parts = append(parts, "自定义证书")
	}
	if internal {
		parts = append(parts, "内部证书")
	}
	if auth {
		parts = append(parts, "密码访问")
	}
	if rules {
		parts = append(parts, "含路径规则")
	}
	parts = append(parts, strconv.Itoa(n)+" 条指令")
	return truncate(strings.Join(parts, " · "), 160)
}

func (a *agent) caddyLink(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		SHA   string   `json:"sha"`
		Hosts []string `json:"hosts"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if len(req.Hosts) == 0 || len(req.Hosts) > 20 {
		return bad(400, "请选择 1–20 个要挂接的网站")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	blocks, _, digest, e := a.caddyBlocks()
	if e != nil {
		return e
	}
	if req.SHA != digest {
		return bad(409, "Caddy 配置在你打开之后被改动过，请刷新页面再试")
	}
	known := map[string]bool{}
	for _, b := range blocks {
		for _, h := range b.Hosts {
			known[h] = true
		}
	}
	managed := map[string]bool{}
	sites, e := a.readSites()
	if e != nil {
		return e
	}
	for _, s := range sites {
		for _, d := range s.Domains {
			managed[d] = true
		}
	}
	links := a.readLinks()
	have := map[string]bool{}
	for _, h := range links {
		have[h] = true
	}
	added := 0
	for _, h := range req.Hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		switch {
		case !known[h]:
			return bad(404, "找不到 "+truncate(h, 60)+" 的配置，请刷新页面再试")
		case managed[h]:
			return bad(409, h+" 已经是面板里的网站")
		case have[h]:
			continue
		}
		have[h] = true
		links = append(links, h)
		added++
	}
	if len(links) > maxLinkedHosts {
		return bad(400, "挂接的网站过多")
	}
	if added > 0 {
		if e = atomicJSON(a.linksPath(), links, 0600); e != nil {
			return e
		}
	}
	send(w, 200, map[string]any{"ok": true, "linked": added})
	return nil
}

func (a *agent) caddyUnlink(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Hosts []string `json:"hosts"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if len(req.Hosts) == 0 || len(req.Hosts) > maxLinkedHosts {
		return bad(400, "请选择要取消挂接的网站")
	}
	drop := map[string]bool{}
	for _, h := range req.Hosts {
		drop[strings.ToLower(strings.TrimSpace(h))] = true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	keep, removed := []string{}, 0
	for _, h := range a.readLinks() {
		if drop[h] {
			removed++
			continue
		}
		keep = append(keep, h)
	}
	if removed > 0 {
		if e := atomicJSON(a.linksPath(), keep, 0600); e != nil {
			return e
		}
	}
	send(w, 200, map[string]any{"ok": true, "unlinked": removed})
	return nil
}
