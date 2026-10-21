package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAccessArgs(t *testing.T) {
	for _, c := range []struct {
		action, domain, ip string
		only               bool
		want               string
	}{
		{"set", "Panel.Example.com.", "", false, "--domain panel.example.com"},
		{"set", "", "203.0.113.5", false, "203.0.113.5"},
		{"set", "panel.example.com", "203.0.113.5", false, "--domain panel.example.com 203.0.113.5"},
		{"set", "panel.example.com", "", true, "--domain panel.example.com --only"},
		{"remove-domain", "", "", false, "--remove-domain"},
		{"remove-ip", "", "", false, "--remove-ip"},
		{"disable", "", "", false, "--disable"},
	} {
		got, e := accessArgs(c.action, c.domain, c.ip, c.only)
		if e != nil || strings.Join(got, " ") != c.want {
			t.Fatalf("%+v: %q %v", c, got, e)
		}
	}
	for _, c := range [][4]string{
		{"set", "", "", ""}, {"set", "https://x.com", "", ""}, {"set", "x.com:8443", "", ""}, {"set", "x.com --disable", "", ""},
		{"set", "x.com; rm -rf /", "", ""}, {"set", "1.2.3.4", "", ""}, {"set", "", "2001:db8::1", ""}, {"set", "", "1.2.3.4 --only", ""},
		{"set", "", "01.2.3.4", ""}, {"disable", "x.com", "", ""}, {"remove-ip", "", "1.2.3.4", ""}, {"explode", "", "", ""}, {"--disable", "", "", ""},
	} {
		if _, e := accessArgs(c[0], c[1], c[2], false); e == nil {
			t.Fatalf("accepted %q", c)
		}
	}
}

func TestAccessEntries(t *testing.T) {
	a := testAgent(t)
	if got, ours := a.accessEntries(); len(got) != 0 || ours {
		t.Fatal("no file")
	}
	os.WriteFile(filepath.Join(a.cfg.Fragments, "panel-access.caddy"), []byte(publicHeaderForTest+"https://panel.example.com {\n    route {\n    }\n}\n\nhttps://203.0.113.5 {\n}\nhttps://bad_name {\n}\n"), 0644)
	got, ours := a.accessEntries()
	if !ours || len(got) != 2 || got[0] != (accessEntry{"domain", "panel.example.com"}) || got[1] != (accessEntry{"ip", "203.0.113.5"}) {
		t.Fatalf("%+v", got)
	}
	os.WriteFile(filepath.Join(a.cfg.Fragments, "panel-access.caddy"), []byte("# someone else's\nhttps://x.example.com {\n}\n"), 0644)
	if got, ours := a.accessEntries(); len(got) != 0 || ours {
		t.Fatal("foreign file trusted")
	}
}

const publicHeaderForTest = "# JunPanel public HTTPS access - managed by public-access.sh\n"

func TestAccessApplyRunsTheScriptOutsideTheSandbox(t *testing.T) {
	a := testAgent(t)
	h := a.routes()
	a.upSystemd = filepath.Join(t.TempDir(), "systemd-run")
	os.WriteFile(a.upSystemd, []byte("#!/bin/sh\n"), 0755)
	var mu sync.Mutex
	var calls []string
	code := 0
	release := make(chan struct{})
	a.accessRun = func(ctx context.Context, systemdRun string, script []byte, env []string, out io.Writer, args ...string) (int, error) {
		mu.Lock()
		calls = append(calls, strings.Join(env, ",")+"|"+strings.Join(args, " "))
		c := code
		mu.Unlock()
		if !bytes.Equal(script, publicAccessScript) || systemdRun != a.upSystemd {
			t.Error("wrong script or runner")
		}
		<-release
		io.WriteString(out, "script output\n")
		return c, nil
	}
	r := request(h, "POST", "/access/apply", `{"action":"set","domain":"panel.example.com","ip":"203.0.113.5","skip_dns":true}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("apply: %d %s", r.Code, r.Body.String())
	}
	if r2 := request(h, "POST", "/access/apply", `{"action":"disable"}`, nil, nil); r2.Code != 409 {
		t.Fatalf("second run at once: %d", r2.Code)
	}
	var status map[string]any
	json.Unmarshal(request(h, "GET", "/access", "", nil, nil).Body.Bytes(), &status)
	if status["running"] != true || status["supported"] != true {
		t.Fatalf("status while running: %v", status)
	}
	release <- struct{}{}
	j := waitOutcome(t, a, r.Body.Bytes())
	if j.State != "success" || !strings.Contains(j.Output, "script output") {
		t.Fatalf("job: %+v", j)
	}
	if calls[0] != "JUNPANEL_SKIP_DNS_CHECK=1|--domain panel.example.com 203.0.113.5" {
		t.Fatalf("call: %q", calls[0])
	}
	for exit, want := range map[int]string{2: "success", 1: "failed"} {
		code = exit
		r = request(h, "POST", "/access/apply", `{"action":"remove-ip"}`, nil, nil)
		go func() { release <- struct{}{} }()
		j = waitOutcome(t, a, r.Body.Bytes())
		if j.State != want || exit == 2 && !strings.Contains(j.Output, "证书还在申请中") {
			t.Fatalf("exit %d: %s %s", exit, j.State, j.Output)
		}
	}
	if r = request(h, "POST", "/access/apply", `{"action":"set","domain":"x.com --disable"}`, nil, nil); r.Code != 400 {
		t.Fatalf("bad domain: %d", r.Code)
	}
	os.Remove(a.upSystemd)
	if r = request(h, "POST", "/access/apply", `{"action":"disable"}`, nil, nil); r.Code != 409 {
		t.Fatalf("no systemd-run: %d", r.Code)
	}
}

// The real runner hands the script over stdin and passes the exit code back.
func TestRunAccessInvocation(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "systemd-run")
	os.WriteFile(fake, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+dir+"/args\ncat > "+dir+"/stdin\necho running\nexit 2\n"), 0755)
	var out bytes.Buffer
	code, e := runAccess(context.Background(), fake, []byte("echo script\n"), []string{"JUNPANEL_SKIP_DNS_CHECK=1"}, &out, "--domain", "panel.example.com")
	if e != nil || code != 2 || !strings.Contains(out.String(), "running") {
		t.Fatalf("%d %v %q", code, e, out.String())
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	if lines[0] != "--unit" || !strings.HasPrefix(lines[1], "junpanel-access-") {
		t.Fatalf("unit: %q", lines)
	}
	want := "--description\nJunPanel public access\n--collect\n--quiet\n--wait\n--pipe\n--setenv=JUNPANEL_SKIP_DNS_CHECK=1\n/bin/bash\n-s\n--\n--domain\npanel.example.com"
	if strings.Join(lines[2:], "\n") != want {
		t.Fatalf("arguments:\n%s", strings.Join(lines[2:], "\n"))
	}
	if stdin, _ := os.ReadFile(filepath.Join(dir, "stdin")); string(stdin) != "echo script\n" {
		t.Fatalf("stdin: %q", stdin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, e := runAccess(ctx, filepath.Join(dir, "missing"), nil, nil, io.Discard); e == nil {
		t.Fatal("missing runner")
	}
}

func TestAccessNeedsThePasswordAgain(t *testing.T) {
	auth, pw := testAccount(t)
	web, e := newWeb(auth.Path, "unused", 8787, false)
	if e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	var forwarded []string
	web.proxy.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		forwarded = append(forwarded, r.Method+" "+r.URL.Path+" "+string(b))
		mu.Unlock()
		return fakeResponse(202, `{"id":"x","state":"running"}`), nil
	})
	token, session, e := web.auth.login("127.0.0.1", "admin", pw, "")
	if e != nil {
		t.Fatal(e)
	}
	h := web.routes()
	headers := map[string]string{"Origin": "http://127.0.0.1:8787", "X-CSRF-Token": session.CSRF, "Content-Type": "application/json"}
	cookie := &http.Cookie{Name: "junpanel_session", Value: token}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(forwarded) }
	if r := request(h, "POST", "/api/access", `{"password":"wrong","action":"disable"}`, headers, cookie); r.Code != 403 || count() != 0 {
		t.Fatalf("wrong password: %d, forwarded %d", r.Code, count())
	}
	for _, p := range []string{"/api/access/apply", "/api/access/apply/", "/api//access/apply", "/api/x/../access/apply", "/api/access/./apply"} {
		if r := request(h, "POST", p, `{"action":"disable"}`, headers, cookie); r.Code == 202 || count() != 0 {
			t.Fatalf("%s reached the agent: %d", p, r.Code)
		}
	}
	r := request(h, "POST", "/api/access", `{"password":"`+pw+`","action":"set","domain":"panel.example.com","only":true}`, headers, cookie)
	if r.Code != 202 || count() != 1 {
		t.Fatalf("right password: %d %d", r.Code, count())
	}
	if f := forwarded[0]; !strings.HasPrefix(f, "POST /access/apply ") || strings.Contains(f, pw) || !strings.Contains(f, `"domain":"panel.example.com"`) || !strings.Contains(f, `"only":true`) {
		t.Fatalf("forwarded: %s", f)
	}
}
