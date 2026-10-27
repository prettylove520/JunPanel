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
	// The folder lives outside the panel's file area, as /srv/kids-english does on the server.
	content = strings.Replace(content, "/srv/kids-english", filepath.ToSlash(externalSiteDir(t)), 1)
	if e := os.WriteFile(a.cfg.Caddy, []byte(content), 0644); e != nil {
		t.Fatal(e)
	}
	return a, content
}

func externalSiteDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "kids-english")
	if e := os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	return dir
}

func TestCaddyBlocksDescribeThemselves(t *testing.T) {
	a, content := linkTestAgent(t)
	got := map[string]caddyBlock{}
	for _, b := range a.parseCaddyBlocks(content) {
		if _, seen := got[b.Hosts[0]]; !seen { // the same host also has an http:// redirect block; keep the first
			got[b.Hosts[0]] = b
		}
	}
	abc := got["abc.example.com"]
	if !strings.Contains(abc.Info, "/kids-english") || !strings.Contains(abc.Info, "含路径规则") {
		t.Fatalf("abc info: %+v", abc)
	}
	// The path filter, the no-cache header and the outside folder are all things the panel can now keep.
	if !abc.Adoptable || abc.Kind != "static" || !strings.HasSuffix(abc.site.Root, "kids-english") || strings.Join(abc.site.BlockPaths, " ") != "/.git /.git/* *.sh" || !abc.site.NoCache || abc.site.NoCompress {
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

func TestAdoptRefusesUnsafeOrUnknownRules(t *testing.T) {
	a := testAgent(t)
	ext := filepath.ToSlash(externalSiteDir(t))
	for name, c := range map[string]struct{ block, want string }{
		"system folder":        {"x.example.com {\n\troot * /etc/caddy\n\tfile_server\n}", "目录"},
		"top-level folder":     {"x.example.com {\n\troot * /srv\n\tfile_server\n}", "目录"},
		"panel state":          {"x.example.com {\n\troot * " + filepath.ToSlash(a.cfg.State) + "\n\tfile_server\n}", "目录"},
		"respond 403":          {"x.example.com {\n\troot * " + ext + "\n\t@x path /a\n\trespond @x 403\n\tfile_server\n}", "respond"},
		"matcher used twice":   {"x.example.com {\n\troot * " + ext + "\n\t@x path /a\n\trespond @x 404\n\trespond @x 404\n\tfile_server\n}", "@x"},
		"matcher not path":     {"x.example.com {\n\troot * " + ext + "\n\t@x header Foo bar\n\trespond @x 404\n\tfile_server\n}", "@x"},
		"undefined matcher":    {"x.example.com {\n\troot * " + ext + "\n\trespond @y 404\n\tfile_server\n}", "@y"},
		"other cache header":   {"x.example.com {\n\troot * " + ext + "\n\theader Cache-Control \"max-age=60\"\n\tfile_server\n}", "引号"},
		"unsafe blocked path":  {"x.example.com {\n\troot * " + ext + "\n\t@x path /a{b}\n\trespond @x 404\n\tfile_server\n}", ""},
		"redirect with filter": {"x.example.com {\n\t@x path /a\n\trespond @x 404\n\tredir https://y.example.com 301\n}", "跳转"},
	} {
		blocks := a.parseCaddyBlocks(c.block + "\n")
		if len(blocks) != 1 || blocks[0].Adoptable || !strings.Contains(blocks[0].Reason, c.want) {
			t.Errorf("%s: %+v", name, blocks)
		}
	}
}

func TestAdoptRemovesTheLink(t *testing.T) {
	a, _ := linkTestAgent(t)
	h := a.routes()
	var list struct {
		SHA    string
		Blocks []caddyBlock
		Linked []string
	}
	load := func() {
		list.Linked = nil
		json.Unmarshal(request(h, "GET", "/caddy/adoptable", "", nil, nil).Body.Bytes(), &list)
	}
	post := func(path string, v any) int {
		raw, _ := json.Marshal(v)
		r := request(h, "POST", path, string(raw), nil, nil)
		if r.Code != 200 {
			t.Logf("%s: %d %s", path, r.Code, r.Body.String())
		}
		return r.Code
	}
	load()
	if post("/caddy/link", map[string]any{"sha": list.SHA, "hosts": []string{"abc.example.com", "gzfuyuan.example.com"}}) != 200 {
		t.Fatal("link refused")
	}
	load()
	id := ""
	for _, b := range list.Blocks {
		if b.Hosts[0] == "abc.example.com" {
			id = b.ID
		}
	}
	if post("/caddy/adopt", map[string]any{"sha": list.SHA, "ids": []string{id}}) != 200 {
		t.Fatal("adopt refused")
	}
	load()
	if strings.Join(list.Linked, " ") != "gzfuyuan.example.com" {
		t.Fatalf("links after adopt: %v", list.Linked)
	}
	sites, _ := a.readSites()
	if len(sites) != 1 || !a.externalRoot(sites[0]) || strings.Join(sites[0].BlockPaths, " ") != "/.git /.git/* *.sh" || !sites[0].NoCache {
		t.Fatalf("adopted site: %+v", sites)
	}
	frag, _ := os.ReadFile(filepath.Join(a.cfg.Fragments, "managed.caddy"))
	if !strings.Contains(string(frag), "@blocked path /.git /.git/* *.sh") || !strings.Contains(string(frag), "header Cache-Control no-cache") {
		t.Fatalf("fragment:\n%s", frag)
	}
}
