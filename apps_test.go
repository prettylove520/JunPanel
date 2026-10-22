package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
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
	if len(appCatalog) != 24 {
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

// waitHTTP polls until the app answers with one of the wanted status codes.
func waitHTTP(t *testing.T, port int, path string, within time.Duration, want ...int) int {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	deadline, last := time.Now().Add(within), 0
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", "http://127.0.0.1:"+itoa(port)+path, nil)
		if resp, e := client.Do(req); e == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			last = resp.StatusCode
			for _, w := range want {
				if last == w {
					return last
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	return last
}

// Every plain template starts, answers on its port, and (where it keeps data) is backed up
// from the paths the backup reads. JUNPANEL_APPS limits the run to some templates.
func TestDockerPlainTemplates(t *testing.T) {
	a := dockerAgent(t)
	h := a.routes()
	checks := map[string]struct {
		path string
		want []int
	}{
		"nextcloud":   {"/status.php", []int{200}},
		"jellyfin":    {"/", []int{200, 302}},
		"navidrome":   {"/", []int{200, 302}},
		"trilium":     {"/", []int{200, 302}},
		"linkding":    {"/login/", []int{200}},
		"freshrss":    {"/", []int{200, 302}},
		"gotify":      {"/", []int{200}},
		"ntfy":        {"/", []int{200}},
		"stirlingpdf": {"/", []int{200, 302}},
		"drawio":      {"/", []int{200}},
		"codeserver":  {"/healthz", []int{200}},
		"librespeed":  {"/", []int{200}},
	}
	only := map[string]bool{}
	for _, k := range strings.Split(os.Getenv("JUNPANEL_APPS"), ",") {
		if k != "" {
			only[k] = true
		}
	}
	for key, c := range checks {
		if len(only) > 0 && !only[key] {
			continue
		}
		t.Run(key, func(t *testing.T) {
			rec := installApp(t, a, "t"+randomID(5), key)
			if code := waitHTTP(t, rec.Port, c.path, 6*time.Minute, c.want...); code == 0 || !contains(c.want, code) {
				t.Fatalf("%s: HTTP %d, want one of %v", key, code, c.want)
			}
			if key == "gotify" {
				req, _ := http.NewRequest("GET", "http://127.0.0.1:"+itoa(rec.Port)+"/current/user", nil)
				req.SetBasicAuth("admin", rec.Password)
				resp, e := http.DefaultClient.Do(req)
				if e != nil || resp.StatusCode != 200 {
					t.Fatalf("gotify login with the generated password failed: %v %v", e, resp)
				}
				resp.Body.Close()
			}
			if tpl, _ := findTemplate(key); tpl.Backup {
				r := request(h, "POST", "/backups", `{"type":"app","target":"`+rec.ID+`"}`, nil, nil)
				if r.Code != 202 {
					t.Fatalf("backup: %d %s", r.Code, r.Body.String())
				}
				j := waitJob(t, a, r.Body.Bytes(), 8*time.Minute)
				for _, part := range appData[key] {
					if !strings.Contains(j.Output, "已备份 "+part.Path) {
						t.Fatalf("%s not backed up:\n%s", part.Path, j.Output)
					}
				}
			}
		})
	}
}

func contains(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Templates only touch what they should: loopback ports, named volumes and the panel's own
// media folders; secrets reach an app only through its environment.
func TestPlainTemplateSafety(t *testing.T) {
	for key, spec := range appSpecs {
		if _, ok := findTemplate(key); !ok {
			t.Fatalf("spec for unknown template %q", key)
		}
		needsLogin := false
		for _, v := range spec.Env {
			needsLogin = needsLogin || strings.Contains(v, "{password}")
		}
		if needsLogin != (spec.Login != "") {
			t.Fatalf("%s: a generated password must come with a login name for the connection info", key)
		}
		for _, v := range spec.Volumes {
			src := strings.SplitN(v, ":", 2)[0]
			if strings.Contains(v, "docker.sock") || strings.HasPrefix(src, "/") && !strings.HasPrefix(src, "{root}/") {
				t.Fatalf("%s mounts %s", key, v)
			}
		}
		raw, e := composeForRoot(appRecord{ID: "demo", Template: key, Port: 9999, Password: "s3cret"}, "/data/root")
		if e != nil {
			t.Fatal(e)
		}
		for _, bad := range []string{"privileged", "network_mode", "cap_add", "docker.sock", "{root}", "{password}"} {
			if strings.Contains(string(raw), bad) {
				t.Fatalf("%s: compose contains %q:\n%s", key, bad, raw)
			}
		}
		if (strings.Contains(string(raw), "s3cret")) != (spec.Login != "") {
			t.Fatalf("%s: password in compose = %v, login %q", key, strings.Contains(string(raw), "s3cret"), spec.Login)
		}
	}
}
