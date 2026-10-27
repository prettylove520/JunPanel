package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Both blocks are written the way a real server had them: a custom certificate and a health endpoint on
// one side, a path filter and a static folder outside the panel's file area on the other.
const linkFixture = `{
	default_sni 1.2.3.4
}

abc.example.com {
	root * /srv/kids-english
	@hidden path /.git /.git/* *.sh
	respond @hidden 404
	header Cache-Control "no-cache"
	encode gzip
	file_server
}

https://gzfuyuan.example.com {
	tls /etc/migration/tls/site.crt /etc/migration/tls/site.key
	@migration_health path /__migration_health
	respond @migration_health "active" 200
	encode gzip
	reverse_proxy 127.0.0.1:4321 {
		header_up X-Forwarded-Proto https
	}
}

http://gzfuyuan.example.com, http://www.gzfuyuan.example.com {
	redir https://{host}{uri} 308
}

# JunPanel managed sites
import %s/*.caddy
`

func linkTestAgent(t *testing.T) (*agent, string) {
	t.Helper()
	a := testAgent(t)
	content := strings.Replace(linkFixture, "%s", strings.ReplaceAll(a.cfg.Fragments, "\\", "/"), 1)
	if e := os.WriteFile(a.cfg.Caddy, []byte(content), 0644); e != nil {
		t.Fatal(e)
	}
	return a, content
}

func TestCaddyBlocksDescribeThemselves(t *testing.T) {
	a, content := linkTestAgent(t)
	got := map[string]caddyBlock{}
	for _, b := range a.parseCaddyBlocks(content) {
		got[b.Hosts[0]] = b
	}
	abc := got["abc.example.com"]
	if abc.Adoptable || !strings.Contains(abc.Info, "静态 → /srv/kids-english") || !strings.Contains(abc.Info, "含路径规则") {
		t.Fatalf("abc: %+v", abc)
	}
	fu := got["gzfuyuan.example.com"]
	if fu.Adoptable || !strings.Contains(fu.Info, "反向代理 → 127.0.0.1:4321") || !strings.Contains(fu.Info, "自定义证书") || !strings.Contains(fu.Info, "5 条指令") {
		t.Fatalf("gzfuyuan: %+v", fu)
	}
}

func TestLinkKeepsTheConfigUntouched(t *testing.T) {
	a, content := linkTestAgent(t)
	h := a.routes()
	var list struct {
		SHA    string
		Blocks []caddyBlock
		Linked []string
	}
	load := func() {
		list.Linked = nil
		if r := request(h, "GET", "/caddy/adoptable", "", nil, nil); r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &list) != nil {
			t.Fatalf("list: %d %s", r.Code, r.Body.String())
		}
	}
	post := func(path string, v any) int {
		raw, _ := json.Marshal(v)
		return request(h, "POST", path, string(raw), nil, nil).Code
	}
	load()
	if len(list.Linked) != 0 {
		t.Fatalf("linked from the start: %v", list.Linked)
	}
	if post("/caddy/link", map[string]any{"sha": strings.Repeat("0", 64), "hosts": []string{"abc.example.com"}}) != 409 {
		t.Fatal("stale digest accepted")
	}
	if post("/caddy/link", map[string]any{"sha": list.SHA, "hosts": []string{"nothere.example.com"}}) != 404 {
		t.Fatal("unknown host accepted")
	}
	if post("/caddy/link", map[string]any{"sha": list.SHA, "hosts": []string{}}) != 400 {
		t.Fatal("empty request accepted")
	}
	if post("/caddy/link", map[string]any{"sha": list.SHA, "hosts": []string{"abc.example.com", "GzFuyuan.example.com"}}) != 200 {
		t.Fatal("link refused")
	}
	// Linking twice is harmless and does not duplicate.
	if post("/caddy/link", map[string]any{"sha": list.SHA, "hosts": []string{"abc.example.com"}}) != 200 {
		t.Fatal("second link refused")
	}
	load()
	if len(list.Linked) != 2 || list.Linked[0] != "abc.example.com" || list.Linked[1] != "gzfuyuan.example.com" {
		t.Fatalf("linked: %v", list.Linked)
	}
	if now, _ := os.ReadFile(a.cfg.Caddy); string(now) != content {
		t.Fatal("linking changed the Caddyfile")
	}
	if len(a.caddyBackups(a.cfg.Caddy)) != 0 {
		t.Fatal("linking saved a backup, so it touched the file")
	}
	// A site the panel itself manages cannot be linked as well.
	if e := atomicJSON(filepath.Join(a.cfg.State, "sites.json"), []site{{ID: "s_" + strings.Repeat("a", 20), Name: "x", Domains: []string{"www.gzfuyuan.example.com"}, Kind: "proxy", Upstream: "http://127.0.0.1:1", Enabled: true}}, 0600); e != nil {
		t.Fatal(e)
	}
	load()
	if post("/caddy/link", map[string]any{"sha": list.SHA, "hosts": []string{"www.gzfuyuan.example.com"}}) != 409 {
		t.Fatal("a panel-managed host was linked")
	}
	if post("/caddy/unlink", map[string]any{"hosts": []string{"abc.example.com", "nothere.example.com"}}) != 200 {
		t.Fatal("unlink refused")
	}
	load()
	if len(list.Linked) != 1 || list.Linked[0] != "gzfuyuan.example.com" {
		t.Fatalf("after unlink: %v", list.Linked)
	}
}
