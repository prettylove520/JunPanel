package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSiteTargetsPort(t *testing.T) {
	for upstream, want := range map[string]bool{
		"http://127.0.0.1:8103":     true,
		"http://localhost:8103":     true,
		"http://127.0.0.1:8103/":    true,
		"http://127.0.0.1:8104":     false,
		"http://203.0.113.9:8103":   false,
		"https://127.0.0.1:8103/ui": true,
		"not a url":                 false,
	} {
		if got := siteTargetsPort(site{Kind: "proxy", Upstream: upstream}, 8103); got != want {
			t.Errorf("%s: %v", upstream, got)
		}
	}
	if siteTargetsPort(site{Kind: "static", Upstream: "http://127.0.0.1:8103"}, 8103) || siteTargetsPort(site{Kind: "proxy", Upstream: "http://127.0.0.1:8103"}, 0) {
		t.Error("static site or port 0 matched")
	}
}

// Uninstalling an app pauses the sites that forward to it; restoring it brings back only those.
func TestUninstallPausesBoundSitesAndRestoreResumesThem(t *testing.T) {
	a := testAgent(t)
	os.WriteFile(a.cfg.Caddy, []byte("import "+filepath.ToSlash(a.cfg.Fragments)+"/*.caddy\n"), 0644)
	a.docker = &dockerClient{HTTP: &http.Client{Transport: (&fakeEngine{id: strings.Repeat("e", 64)}).transport()}}
	a.stream = func(context.Context, string, io.Writer, []string, string, ...string) error { return nil }
	a.storeApps([]appRecord{{ID: "llm", Name: "llmfit", Template: "llmfit", Port: 8103}, {ID: "other", Name: "Other", Template: "memos", Port: 8104}})
	h := a.routes()
	for _, body := range []string{
		`{"name":"llm","domains":["llm.example.com"],"kind":"proxy","upstream":"http://127.0.0.1:8103"}`,
		`{"name":"other","domains":["other.example.com"],"kind":"proxy","upstream":"http://127.0.0.1:8104"}`,
		`{"name":"manual","domains":["manual.example.com"],"kind":"proxy","upstream":"http://127.0.0.1:8103"}`,
	} {
		if r := request(h, "POST", "/sites", body, nil, nil); r.Code != 200 {
			t.Fatalf("site: %d %s", r.Code, r.Body.String())
		}
	}
	rows, _ := a.readSites()
	enabled := func() map[string]bool {
		out := map[string]bool{}
		rows, _ := a.readSites()
		for _, s := range rows {
			out[s.Name] = s.Enabled
		}
		return out
	}
	// A site the owner switched off by hand must stay off after a restore.
	for _, s := range rows {
		if s.Name == "manual" {
			request(h, "POST", "/sites/"+s.ID+"/toggle", `{}`, nil, nil)
		}
	}
	if got := enabled(); !got["llm"] || !got["other"] || got["manual"] {
		t.Fatalf("before: %v", got)
	}
	r := request(h, "POST", "/apps/llm/action", `{"action":"remove"}`, nil, nil)
	if j := waitOutcome(t, a, r.Body.Bytes()); j.State == "failed" || !strings.Contains(j.Output, "llm.example.com") {
		t.Fatalf("remove: %+v", j)
	}
	if got := enabled(); got["llm"] || !got["other"] || got["manual"] {
		t.Fatalf("after uninstall: %v", got)
	}
	r = request(h, "POST", "/apps/llm/action", `{"action":"restore"}`, nil, nil)
	if j := waitOutcome(t, a, r.Body.Bytes()); j.State == "failed" {
		t.Fatalf("restore: %+v", j)
	}
	if got := enabled(); !got["llm"] || !got["other"] || got["manual"] {
		t.Fatalf("after restore: %v", got)
	}
}

func TestPurgeNeedsAnUninstalledApp(t *testing.T) {
	a := testAgent(t)
	a.docker = &dockerClient{HTTP: &http.Client{Transport: (&fakeEngine{id: strings.Repeat("f", 64)}).transport()}}
	var args []string
	a.stream = func(_ context.Context, _ string, _ io.Writer, _ []string, _ string, a2 ...string) error {
		args = a2
		return nil
	}
	a.storeApps([]appRecord{{ID: "live", Name: "Live", Template: "memos", Port: 8110}, {ID: "gone", Name: "Gone", Template: "memos", Port: 8111, Removed: true}})
	os.MkdirAll(a.appDir("gone"), 0700)
	os.WriteFile(filepath.Join(a.appDir("gone"), "secret.env"), []byte("x"), 0600)
	if err := a.writeSchedules([]schedule{{ID: "sc1", Name: "nightly", Action: "backup", Type: "app", Target: "gone"}, {ID: "sc2", Name: "keep", Action: "backup", Type: "app", Target: "live"}}); err != nil {
		t.Fatal(err)
	}
	h := a.routes()
	if r := request(h, "POST", "/apps/live/action", `{"action":"purge"}`, nil, nil); r.Code != 409 {
		t.Fatalf("purged a running app: %d", r.Code)
	}
	r := request(h, "POST", "/apps/gone/action", `{"action":"purge"}`, nil, nil)
	if j := waitOutcome(t, a, r.Body.Bytes()); j.State == "failed" {
		t.Fatalf("purge: %+v", j)
	}
	if !strings.Contains(strings.Join(args, " "), "down --volumes") {
		t.Fatalf("compose args: %v", args)
	}
	rows, _ := a.readApps()
	if len(rows) != 1 || rows[0].ID != "live" {
		t.Fatalf("apps after purge: %+v", rows)
	}
	if _, e := os.Stat(a.appDir("gone")); !os.IsNotExist(e) {
		t.Fatal("project directory kept")
	}
	if sc, _ := a.readSchedules(); len(sc) != 1 || sc[0].Target != "live" {
		t.Fatalf("schedules after purge: %+v", sc)
	}
}

// Removing or switching off a site clears its alert straight away, instead of leaving it up until the
// cached host list expires and the recovery polls pass.
func TestRemovingASiteClearsItsAlert(t *testing.T) {
	a := testAgent(t)
	os.WriteFile(a.cfg.Caddy, []byte("import "+filepath.ToSlash(a.cfg.Fragments)+"/*.caddy\n"), 0644)
	h := a.routes()
	for _, body := range []string{
		`{"name":"llm","domains":["llm.example.com"],"kind":"proxy","upstream":"http://127.0.0.1:8103"}`,
		`{"name":"keep","domains":["keep.example.com"],"kind":"proxy","upstream":"http://127.0.0.1:8104"}`,
		`{"name":"off","domains":["off.example.com"],"kind":"proxy","upstream":"http://127.0.0.1:8105"}`,
	} {
		if r := request(h, "POST", "/sites", body, nil, nil); r.Code != 200 {
			t.Fatalf("site: %d %s", r.Code, r.Body.String())
		}
	}
	st := &alertState{Active: map[string]*activeAlert{
		"site:llm.example.com":  {Key: "site:llm.example.com", Title: "网站后端异常：llm.example.com"},
		"site:keep.example.com": {Key: "site:keep.example.com", Title: "网站后端异常：keep.example.com"},
		"site:off.example.com":  {Key: "site:off.example.com", Title: "网站后端异常：off.example.com"},
		"disk:/":                {Key: "disk:/", Title: "磁盘空间不足"},
	}, Resolved: map[string]time.Time{}}
	if e := atomicJSON(a.alertStatePath(), st, 0600); e != nil {
		t.Fatal(e)
	}
	a.certAt = time.Now()
	rows, _ := a.readSites()
	id := map[string]string{}
	for _, s := range rows {
		id[s.Name] = s.ID
	}
	if r := request(h, "DELETE", "/sites/"+id["llm"], "", nil, nil); r.Code != 200 {
		t.Fatalf("delete: %d", r.Code)
	}
	if r := request(h, "POST", "/sites/"+id["off"]+"/toggle", `{}`, nil, nil); r.Code != 200 {
		t.Fatalf("toggle: %d", r.Code)
	}
	var left map[string]*activeAlert
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		cur, _ := a.readAlertState()
		if left = cur.Active; len(left) == 2 {
			break
		}
	}
	if len(left) != 2 || left["site:keep.example.com"] == nil || left["disk:/"] == nil {
		t.Fatalf("alerts left: %v", left)
	}
	if !a.certAt.IsZero() {
		t.Fatal("the cached host list was not dropped")
	}
}
