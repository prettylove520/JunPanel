package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const adoptFixture = `{
	email admin@example.com
}

(security) {
	header X-Frame-Options DENY
}

# 监控
monitor.example.com {
	reverse_proxy 127.0.0.1:3001
	encode gzip zstd
}

example.com, www.example.com {
	redir https://new.example.com{uri} permanent
}

store.example.com {
	reverse_proxy 127.0.0.1:9000 {
		header_up Host {host}
	}
}

api.example.com {
	reverse_proxy localhost:8081
	log {
		output stdout
	}
}

odd.example.com:8443 {
	reverse_proxy 127.0.0.1:9100
}

plain.example.com {
	reverse_proxy https://backend.internal:8443
}

https://explicit.example.com {
	reverse_proxy 127.0.0.1:7000
}

http://tls.example.com {
	reverse_proxy 127.0.0.1:7001
}

# JunPanel managed sites
import %s/*.caddy
`

func adoptTestAgent(t *testing.T) (*agent, string) {
	t.Helper()
	a := testAgent(t)
	content := strings.Replace(adoptFixture, "%s", filepath.ToSlash(a.cfg.Fragments), 1)
	if e := os.WriteFile(a.cfg.Caddy, []byte(content), 0644); e != nil {
		t.Fatal(e)
	}
	return a, content
}

func TestCaddyBlocksAreJudged(t *testing.T) {
	a, content := adoptTestAgent(t)
	got := map[string]caddyBlock{}
	for _, b := range a.parseCaddyBlocks(content) {
		got[b.Hosts[0]] = b
	}
	if len(got) != 8 {
		t.Fatalf("%d blocks found: %v", len(got), got)
	}
	m := got["monitor.example.com"]
	if !m.Adoptable || m.Kind != "proxy" || m.site.Upstream != "http://127.0.0.1:3001" || m.site.NoCompress {
		t.Fatalf("monitor: %+v", m)
	}
	r := got["example.com"]
	if !r.Adoptable || r.Kind != "redirect" || r.site.Target != "https://new.example.com" || !r.site.KeepPath || !r.site.Permanent || len(r.site.Domains) != 2 || !r.site.NoCompress {
		t.Fatalf("redirect: %+v", r)
	}
	if s := got["plain.example.com"]; !s.Adoptable || s.site.Upstream != "https://backend.internal:8443" {
		t.Fatalf("https upstream: %+v", s)
	}
	if e := got["explicit.example.com"]; !e.Adoptable || e.site.Upstream != "http://127.0.0.1:7000" {
		t.Fatalf("explicit https:// address: %+v", e)
	}
	for host, want := range map[string]string{"store.example.com": "嵌套", "api.example.com": "嵌套", "odd.example.com": "端口", "tls.example.com": "http://"} {
		b, ok := got[host]
		if !ok || b.Adoptable || !strings.Contains(b.Reason, want) {
			t.Fatalf("%s: %+v", host, b)
		}
	}
}

func TestCaddyAdoptMovesSitesAtomically(t *testing.T) {
	a, content := adoptTestAgent(t)
	h := a.routes()
	var list struct {
		SHA    string
		Blocks []caddyBlock
	}
	if r := request(h, "GET", "/caddy/adoptable", "", nil, nil); r.Code != 200 {
		t.Fatalf("list: %d %s", r.Code, r.Body.String())
	} else {
		json.Unmarshal(r.Body.Bytes(), &list)
	}
	ids := map[string]string{}
	for _, b := range list.Blocks {
		ids[b.Hosts[0]] = b.ID
	}
	post := func(sha string, hosts ...string) int {
		var chosen []string
		for _, host := range hosts {
			chosen = append(chosen, ids[host])
		}
		raw, _ := json.Marshal(map[string]any{"sha": sha, "ids": chosen})
		return request(h, "POST", "/caddy/adopt", string(raw), nil, nil).Code
	}
	if post("stale", "monitor.example.com") != 409 {
		t.Fatal("stale view accepted")
	}
	if post(list.SHA, "store.example.com") != 409 {
		t.Fatal("block with nested config adopted")
	}
	if now, _ := os.ReadFile(a.cfg.Caddy); string(now) != content {
		t.Fatal("a refused adoption changed the Caddyfile")
	}

	// A failing validation leaves both files and the site list as they were.
	a.runner = func(_ time.Duration, name string, args ...string) (string, error) {
		for _, x := range args {
			if x == "validate" {
				return "Error: ambiguous site definition", errors.New("exit 1")
			}
		}
		return "", nil
	}
	if post(list.SHA, "monitor.example.com") != 409 {
		t.Fatal("invalid result accepted")
	}
	managed := filepath.Join(a.cfg.Fragments, "managed.caddy")
	if now, _ := os.ReadFile(a.cfg.Caddy); string(now) != content {
		t.Fatal("Caddyfile not restored")
	}
	if _, e := os.Stat(managed); e == nil {
		t.Fatal("managed fragment left behind")
	}
	if sites, _ := a.readSites(); len(sites) != 0 {
		t.Fatal("site registered despite the failure")
	}

	// A good adoption moves two blocks: gone from the Caddyfile, present in the fragment and the list.
	a.runner = func(time.Duration, string, ...string) (string, error) { return "", nil }
	if post(list.SHA, "monitor.example.com", "example.com") != 200 {
		t.Fatal("adoption refused")
	}
	now, _ := os.ReadFile(a.cfg.Caddy)
	if strings.Contains(string(now), "reverse_proxy 127.0.0.1:3001") || strings.Contains(string(now), "redir https://new.example.com") || !strings.Contains(string(now), "已转入 JunPanel 管理") {
		t.Fatalf("blocks not replaced:\n%s", now)
	}
	if !strings.Contains(string(now), "store.example.com") || !a.caddyLinked(string(now)) {
		t.Fatal("other blocks or the panel import line were damaged")
	}
	frag, _ := os.ReadFile(managed)
	if !strings.Contains(string(frag), "monitor.example.com {") || !strings.Contains(string(frag), "reverse_proxy http://127.0.0.1:3001") || !strings.Contains(string(frag), "redir https://new.example.com{uri} 301") {
		t.Fatalf("fragment:\n%s", frag)
	}
	if sites, _ := a.readSites(); len(sites) != 2 || sites[0].Kind != "proxy" || !sites[0].Enabled {
		t.Fatalf("sites: %+v", sites)
	}
	if len(a.caddyBackups()) == 0 {
		t.Fatal("the old Caddyfile was not kept")
	}
	// They are now ordinary panel sites, so they no longer appear as adoptable.
	if r := request(h, "GET", "/caddy/adoptable", "", nil, nil); strings.Contains(r.Body.String(), `"monitor.example.com"`) {
		t.Fatal("adopted site still offered")
	}
}
