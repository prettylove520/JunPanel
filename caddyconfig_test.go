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

func caddyTestAgent(t *testing.T) (*agent, string) {
	t.Helper()
	a := testAgent(t)
	link := a.caddyLinkLine()
	original := "old.example.com {\n\treverse_proxy 127.0.0.1:9000\n}\n\n# JunPanel managed sites\n" + link + "\n"
	if e := os.WriteFile(a.cfg.Caddy, []byte(original), 0644); e != nil {
		t.Fatal(e)
	}
	return a, original
}

func caddyGet(t *testing.T, a *agent) (content, sha string, editable bool, backups int) {
	t.Helper()
	r := request(a.routes(), "GET", "/caddy/config", "", nil, nil)
	if r.Code != 200 {
		t.Fatalf("get: %d %s", r.Code, r.Body.String())
	}
	var out struct {
		Content  string
		SHA      string
		Editable bool
		Backups  []caddyBackup
	}
	json.Unmarshal(r.Body.Bytes(), &out)
	return out.Content, out.SHA, out.Editable, len(out.Backups)
}

func TestCaddyConfigEditKeepsBackupAndRollsBack(t *testing.T) {
	a, original := caddyTestAgent(t)
	h := a.routes()
	content, sha, editable, backups := caddyGet(t, a)
	if content != original || !editable || backups != 0 {
		t.Fatalf("get: editable=%v backups=%d", editable, backups)
	}
	post := func(body map[string]string) int {
		raw, _ := json.Marshal(body)
		return request(h, "POST", "/caddy/config", string(raw), nil, nil).Code
	}
	changed := strings.Replace(original, "old.example.com", "new.example.com", 1)

	// A stale view, a missing import line and an unchanged file are refused before anything is written.
	if post(map[string]string{"content": changed, "sha": "stale"}) != 409 {
		t.Fatal("stale edit accepted")
	}
	if post(map[string]string{"content": "new.example.com {\n}\n", "sha": sha}) != 400 {
		t.Fatal("removing the panel import line was accepted")
	}
	if post(map[string]string{"content": original, "sha": sha}) != 400 {
		t.Fatal("unchanged file accepted")
	}
	if now, _ := os.ReadFile(a.cfg.Caddy); string(now) != original {
		t.Fatal("a refused edit changed the file")
	}

	// Validation failure puts the old file back (and nothing is reloaded).
	var reloads int
	a.runner = func(_ time.Duration, name string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "reload" {
			reloads++
			return "", nil
		}
		for _, x := range args {
			if x == "validate" {
				return "Error: adapting config: syntax error", errors.New("exit 1")
			}
		}
		return "", nil
	}
	if post(map[string]string{"content": changed, "sha": sha}) != 409 {
		t.Fatal("invalid config accepted")
	}
	if now, _ := os.ReadFile(a.cfg.Caddy); string(now) != original || reloads != 0 {
		t.Fatalf("invalid config left behind or reloaded (%d)", reloads)
	}

	// A reload failure also restores the old file, and reloads once more to return to it.
	reloads = 0
	a.runner = func(_ time.Duration, name string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "reload" {
			reloads++
			if reloads == 1 {
				return "reload failed", errors.New("exit 1")
			}
		}
		return "", nil
	}
	if post(map[string]string{"content": changed, "sha": sha}) != 409 || reloads != 2 {
		t.Fatalf("failed reload not undone (%d reloads)", reloads)
	}
	if now, _ := os.ReadFile(a.cfg.Caddy); string(now) != original {
		t.Fatal("failed reload left the new file")
	}

	// A good edit is written (with a trailing newline), reloaded, and the previous file is kept.
	reloads = 0
	a.runner = func(_ time.Duration, name string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "reload" {
			reloads++
		}
		return "", nil
	}
	trimmed := strings.TrimSuffix(changed, "\n")
	if post(map[string]string{"content": trimmed, "sha": sha}) != 200 || reloads != 1 {
		t.Fatal("valid edit refused")
	}
	if now, _ := os.ReadFile(a.cfg.Caddy); string(now) != changed {
		t.Fatalf("file is %q", now)
	}
	list := a.caddyBackups(a.cfg.Caddy)
	if len(list) == 0 {
		t.Fatal("no backup kept")
	}
	_, kept, e := a.caddyBackupRead(list[0].ID)
	if e != nil || string(kept) != original {
		t.Fatalf("backup is %q", kept)
	}

	// The backup can be restored, which itself keeps the file it replaces.
	_, sha2, _, _ := caddyGet(t, a)
	raw, _ := json.Marshal(map[string]string{"id": list[0].ID, "sha": sha2})
	if r := request(h, "POST", "/caddy/config/restore", string(raw), nil, nil); r.Code != 200 {
		t.Fatalf("restore: %d %s", r.Code, r.Body.String())
	}
	if now, _ := os.ReadFile(a.cfg.Caddy); string(now) != original {
		t.Fatal("restore did not bring the old file back")
	}
	if r := request(h, "GET", "/caddy/config/backups/..%2Fx", "", nil, nil); r.Code == 200 {
		t.Fatal("odd backup name accepted")
	}
}

func TestCaddyBackupsAreLimited(t *testing.T) {
	a, original := caddyTestAgent(t)
	for i := 0; i < keepCaddyBackups+5; i++ {
		a.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC) }
		if e := a.saveCaddyBackup(a.cfg.Caddy, []byte(original)); e != nil {
			t.Fatal(e)
		}
	}
	if n := len(a.caddyBackups(a.cfg.Caddy)); n != keepCaddyBackups {
		t.Fatalf("%d backups kept", n)
	}
}

func TestImportedCaddyFilesCanBeEditedButNothingElse(t *testing.T) {
	a, _ := caddyTestAgent(t)
	dir := filepath.Dir(a.cfg.Caddy)
	site := filepath.Join(dir, "shop.caddy")
	os.WriteFile(site, []byte("shop.example.com {\n\treverse_proxy 127.0.0.1:9000\n}\n"), 0644)
	outside := filepath.Join(t.TempDir(), "elsewhere.caddy")
	os.WriteFile(outside, []byte("x.example.com {\n}\n"), 0644)
	main, _ := os.ReadFile(a.cfg.Caddy)
	os.WriteFile(a.cfg.Caddy, []byte("import shop.caddy\nimport "+outside+"\nimport security\n"+string(main)), 0644)
	files := a.caddyFiles()
	if len(files) != 2 || files[1] != site {
		t.Fatalf("files %v", files)
	}
	h := a.routes()
	if r := request(h, "GET", "/caddy/config?file="+outside, "", nil, nil); r.Code != 404 {
		t.Fatalf("file outside the Caddy folder offered: %d", r.Code)
	}
	r := request(h, "GET", "/caddy/config?file="+site, "", nil, nil)
	var got struct {
		Content string
		SHA     string
		Main    bool
	}
	json.Unmarshal(r.Body.Bytes(), &got)
	if r.Code != 200 || got.Main || !strings.Contains(got.Content, "shop.example.com") {
		t.Fatalf("imported file: %d %s", r.Code, r.Body.String())
	}
	// The imported file has no panel import line to keep, and its backup restores to it.
	raw, _ := json.Marshal(map[string]string{"file": site, "content": "shop.example.com {\n\treverse_proxy 127.0.0.1:9001\n}\n", "sha": got.SHA})
	if r := request(h, "POST", "/caddy/config", string(raw), nil, nil); r.Code != 200 {
		t.Fatalf("save imported: %d %s", r.Code, r.Body.String())
	}
	backups := a.caddyBackups(site)
	if len(backups) != 1 || backups[0].File != site {
		t.Fatalf("backups %+v", backups)
	}
	if path, kept, e := a.caddyBackupRead(backups[0].ID); e != nil || path != site || !strings.Contains(string(kept), "9000") {
		t.Fatalf("backup %s %q %v", path, kept, e)
	}
}
