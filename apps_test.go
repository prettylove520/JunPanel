package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCatalogTemplates(t *testing.T) {
	seen := map[string]bool{}
	ports := map[int]string{}
	for _, tpl := range appCatalog {
		if seen[tpl.Key] || !shortName.MatchString(tpl.Key) && tpl.Key != "n8n" {
			t.Fatalf("bad or duplicate key %q", tpl.Key)
		}
		seen[tpl.Key] = true
		if other, ok := ports[tpl.Port]; ok {
			t.Fatalf("%s and %s suggest the same port %d", tpl.Key, other, tpl.Port)
		}
		ports[tpl.Port] = tpl.Key
		if tpl.Backup != (len(appData[tpl.Key]) > 0) {
			t.Fatalf("%s: backup flag %v disagrees with appData", tpl.Key, tpl.Backup)
		}
		raw, e := composeFor(appRecord{ID: "demo", Template: tpl.Key, Port: tpl.Port, Password: "p", DBPassword: "d"})
		if e != nil {
			t.Fatal(e)
		}
		var cfg struct {
			Services map[string]struct {
				Image   string            `json:"image"`
				Ports   []string          `json:"ports"`
				Volumes []string          `json:"volumes"`
				Labels  map[string]string `json:"labels"`
			} `json:"services"`
			Volumes map[string]any `json:"volumes"`
		}
		json.Unmarshal(raw, &cfg)
		app := cfg.Services["app"]
		if app.Image != tpl.Image || len(app.Ports) != 1 || !strings.HasPrefix(app.Ports[0], "127.0.0.1:") || app.Labels["junpanel.app"] != "demo" {
			t.Fatalf("%s: %s", tpl.Key, raw)
		}
		// Every data directory a backup copies is actually a named volume of the app.
		for _, part := range appData[tpl.Key] {
			found := false
			for _, v := range cfg.Services[part.Service].Volumes {
				if v == part.Volume+":"+part.Path {
					found = true
				}
			}
			if !found || cfg.Volumes[part.Volume] == nil {
				t.Fatalf("%s: %s:%s not mounted: %s", tpl.Key, part.Volume, part.Path, raw)
			}
		}
		if !tpl.Backup && len(app.Volumes) != 0 {
			t.Fatalf("%s keeps data but cannot be backed up", tpl.Key)
		}
	}
	if len(appCatalog) != 12 {
		t.Fatalf("catalog has %d templates", len(appCatalog))
	}
}

func TestUpgradeIsAnAppAction(t *testing.T) {
	a := testAgent(t)
	if _, e := a.appActionJob("missing", "upgrade", nil); e == nil || !strings.Contains(e.Error(), "应用不存在") {
		t.Fatalf("upgrade not accepted as an action: %v", e)
	}
	if _, e := a.appActionJob("missing", "explode", nil); e == nil || !strings.Contains(e.Error(), "操作无效") {
		t.Fatalf("unknown action: %v", e)
	}
}

// An upgrade backs the data up first and keeps it; a stateless app skips the backup.
func TestDockerAppUpgrade(t *testing.T) {
	a := dockerAgent(t)
	h := a.routes()
	rec := installApp(t, a, "up"+randomID(4), "redis")
	redisCommand(t, a, rec, "SET", "kept", "yes")
	redisCommand(t, a, rec, "SAVE")
	r := request(h, "POST", "/apps/"+rec.ID+"/action", `{"action":"upgrade"}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("upgrade: %d %s", r.Code, r.Body.String())
	}
	j := waitJob(t, a, r.Body.Bytes(), 5*time.Minute)
	if !strings.Contains(j.Output, "已创建升级前备份") {
		t.Fatalf("no backup before the upgrade:\n%s", j.Output)
	}
	backups := listBackups(t, h)
	if len(backups) != 1 || backups[0].Target != rec.ID || backups[0].Note != "升级前自动备份" {
		t.Fatalf("backups: %+v", backups)
	}
	if got := redisCommand(t, a, rec, "GET", "kept"); strings.TrimSpace(got) != "yes" {
		t.Fatalf("data after upgrade: %q", got)
	}
	rows, _ := a.readApps()
	for _, row := range rows {
		if row.ID == rec.ID && (row.Busy || row.LastError != "") {
			t.Fatalf("app left busy or failed: %+v", row)
		}
	}
	tools := installApp(t, a, "it"+randomID(4), "ittools")
	r = request(h, "POST", "/apps/"+tools.ID+"/action", `{"action":"upgrade"}`, nil, nil)
	j = waitJob(t, a, r.Body.Bytes(), 5*time.Minute)
	if !strings.Contains(j.Output, "跳过升级前备份") || len(listBackups(t, h)) != 1 {
		t.Fatalf("stateless upgrade:\n%s", j.Output)
	}
	if code := httpStatus(t, tools.Port); code != 200 {
		t.Fatalf("IT-Tools after upgrade: HTTP %d", code)
	}
}

func httpStatus(t *testing.T, port int) int {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 40; i++ {
		resp, e := client.Get("http://127.0.0.1:" + itoa(port) + "/")
		if e == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return resp.StatusCode
		}
		time.Sleep(time.Second)
	}
	return 0
}

// Memos keeps its data where the backup looks for it.
func TestDockerMemosTemplate(t *testing.T) {
	a := dockerAgent(t)
	h := a.routes()
	rec := installApp(t, a, "mm"+randomID(4), "memos")
	if code := httpStatus(t, rec.Port); code != 200 {
		t.Fatalf("Memos: HTTP %d", code)
	}
	r := request(h, "POST", "/backups", `{"type":"app","target":"`+rec.ID+`"}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("backup: %d %s", r.Code, r.Body.String())
	}
	j := waitJob(t, a, r.Body.Bytes(), 3*time.Minute)
	if !strings.Contains(j.Output, "已备份 /var/opt/memos") {
		t.Fatalf("backup output:\n%s", j.Output)
	}
	b := listBackups(t, h)[0]
	raw := request(h, "GET", "/backups/"+b.ID+"/download", "", nil, nil).Body.Bytes()
	names := strings.Join(tarNames(t, raw), "\n")
	if !strings.Contains(names, "parts/app/memos/memos_prod.db") {
		t.Fatalf("Memos database not in the backup:\n%s", names)
	}
}
