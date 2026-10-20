package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- helpers ----

type receiver struct {
	mu     sync.Mutex
	srv    *httptest.Server
	bodies []string
	status int
	reply  string
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{status: 200, reply: "ok"}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b := make([]byte, 1<<20)
		n, _ := req.Body.Read(b)
		r.mu.Lock()
		r.bodies = append(r.bodies, string(b[:n]))
		status, reply := r.status, r.reply
		r.mu.Unlock()
		w.WriteHeader(status)
		w.Write([]byte(reply))
	}))
	t.Cleanup(r.srv.Close)
	return r
}
func (r *receiver) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.bodies) }
func (r *receiver) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.bodies) == 0 {
		return ""
	}
	return r.bodies[len(r.bodies)-1]
}
func (r *receiver) set(status int, reply string) {
	r.mu.Lock()
	r.status, r.reply = status, reply
	r.mu.Unlock()
}

type fakeContainer struct {
	ID, Name, State, Status, Project string
	Oneoff                           bool
}
type fakeDocker struct {
	mu   sync.Mutex
	rows []fakeContainer
	fail bool
}

func (f *fakeDocker) client() *dockerClient {
	return &dockerClient{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/version" {
			return fakeResponse(200, `{"ApiVersion":"1.56","MinAPIVersion":"1.44"}`), nil
		}
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			if f.fail {
				return nil, errors.New("docker down")
			}
			var out []map[string]any
			for _, c := range f.rows {
				labels := map[string]string{}
				if c.Project != "" {
					labels["com.docker.compose.project"] = c.Project
				}
				if c.Oneoff {
					labels["com.docker.compose.oneoff"] = "True"
				}
				out = append(out, map[string]any{"Id": c.ID, "Names": []string{"/" + c.Name}, "Image": "img:1", "State": c.State, "Status": c.Status, "Labels": labels})
			}
			b, _ := json.Marshal(out)
			return fakeResponse(200, string(b)), nil
		}
		return fakeResponse(204, ""), nil
	})}}
}
func (f *fakeDocker) set(name, state, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].Name == name {
			f.rows[i].State, f.rows[i].Status = state, status
			return
		}
	}
	panic("no container " + name)
}
func (f *fakeDocker) remove(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].Name == name {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return
		}
	}
}
func (f *fakeDocker) id(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.rows {
		if c.Name == name {
			return c.ID
		}
	}
	return ""
}
func cid(c byte) string { return strings.Repeat(string(c), 64) }

type alertRig struct {
	t     *testing.T
	a     *agent
	now   time.Time
	hook  *receiver
	dock  *fakeDocker
	disk  uint64 // used percent of the fake disk
	caddy string
}

func newRig(t *testing.T) *alertRig {
	t.Helper()
	g := &alertRig{t: t, now: at(2026, 10, 20, 10, 0), hook: newReceiver(t), caddy: "active"}
	g.a = testAgent(t)
	g.a.cfg.CaddyData = t.TempDir()
	g.a.now = func() time.Time { return g.now }
	g.a.host = "testhost"
	g.dock = &fakeDocker{rows: []fakeContainer{
		{ID: cid('a'), Name: "web", State: "running", Status: "Up 2 hours"},
		{ID: cid('b'), Name: "db", State: "running", Status: "Up 2 hours"},
		{ID: cid('c'), Name: "batch", State: "running", Status: "Up 1 minute", Oneoff: true},
		{ID: cid('d'), Name: "old", State: "exited", Status: "Exited (137) 3 weeks ago"},
	}}
	g.a.docker = g.dock.client()
	g.a.runner = func(time.Duration, string, ...string) (string, error) { return g.caddy, nil }
	g.disk = 40
	g.a.diskFn = func(string) map[string]uint64 {
		return map[string]uint64{"total": 100 << 30, "used": g.disk << 30 / 1, "free": (100 - g.disk) << 30}
	}
	g.config(func(c *alertConfig) { c.URL, c.Notify = g.hook.srv.URL, true })
	return g
}
func (g *alertRig) config(edit func(*alertConfig)) {
	g.t.Helper()
	cfg, e := g.a.readAlertConfig()
	if e != nil {
		g.t.Fatal(e)
	}
	edit(&cfg)
	if e = atomicJSON(g.a.alertConfigPath(), cfg, 0600); e != nil {
		g.t.Fatal(e)
	}
}
func (g *alertRig) tick() *alertState {
	g.t.Helper()
	st, e := g.a.alertTick(g.now)
	if e != nil {
		g.t.Fatal(e)
	}
	return st
}
func (g *alertRig) advance(d time.Duration) { g.now = g.now.Add(d) }
func (g *alertRig) state() *alertState {
	g.t.Helper()
	st, e := g.a.readAlertState()
	if e != nil {
		g.t.Fatal(e)
	}
	return st
}
func (g *alertRig) kinds() []string {
	var out []string
	for _, ev := range g.state().History {
		out = append(out, ev.Kind+":"+ev.Key)
	}
	return out
}

func makeCert(t *testing.T, dir, issuer string, notBefore, notAfter time.Time, dns []string, ips []net.IP) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	name := "unnamed"
	if len(dns) > 0 {
		name = dns[0]
	} else if len(ips) > 0 {
		name = ips[0].String()
	}
	tpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, NotBefore: notBefore, NotAfter: notAfter, DNSNames: dns, IPAddresses: ips}
	parent := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: issuer}}
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, e := x509.CreateCertificate(rand.Reader, tpl, parent, &key.PublicKey, caKey)
	if e != nil {
		t.Fatal(e)
	}
	folder := filepath.Join(dir, "certificates", "acme-v02.api.letsencrypt.org-directory", strings.ReplaceAll(name, "*", "wildcard_"))
	os.MkdirAll(folder, 0755)
	file := filepath.Join(folder, name+".crt")
	if len(dns) > 0 && notAfter.Before(time.Now().Add(-time.Hour)) {
		file = filepath.Join(folder, name+"-old.crt")
	}
	os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
}
func servedHosts(g *alertRig, hosts ...string) {
	quoted := make([]string, len(hosts))
	for i, h := range hosts {
		quoted[i] = `"` + h + `"`
	}
	json := `{"apps":{"http":{"servers":{"srv0":{"routes":[{"match":[{"host":[` + strings.Join(quoted, ",") + `]}]}]}}}}}`
	g.a.capture = func(time.Duration, string, ...string) (string, error) { return json, nil }
}

// ---- notification delivery ----

func TestPayloadFormats(t *testing.T) {
	notices := []notice{{Kind: "triggered", Key: "disk:/", Title: "磁盘空间不足：/", Detail: "已用 93%", Severity: "warning"}, {Kind: "resolved", Key: "cert:a", Title: "证书即将过期：a", Detail: "已恢复正常", Severity: "info"}}
	now := at(2026, 10, 20, 10, 0)
	for format, check := range map[string]func(map[string]any) string{
		"generic":  func(m map[string]any) string { return m["text"].(string) },
		"wecom":    func(m map[string]any) string { return m["text"].(map[string]any)["content"].(string) },
		"dingtalk": func(m map[string]any) string { return m["text"].(map[string]any)["content"].(string) },
		"feishu":   func(m map[string]any) string { return m["content"].(map[string]any)["text"].(string) },
	} {
		raw, e := buildPayload(format, "srv1", notices, now)
		if e != nil {
			t.Fatalf("%s: %v", format, e)
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			t.Fatalf("%s: not JSON", format)
		}
		text := check(m)
		for _, want := range []string{"JunPanel", "srv1", "【告警】磁盘空间不足：/", "已用 93%", "【已恢复】证书即将过期：a"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s text lacks %q:\n%s", format, want, text)
			}
		}
		switch format {
		case "wecom", "dingtalk":
			if m["msgtype"] != "text" {
				t.Errorf("%s msgtype", format)
			}
		case "feishu":
			if m["msg_type"] != "text" {
				t.Error("feishu msg_type")
			}
		case "generic":
			if len(m["alerts"].([]any)) != 2 || m["host"] != "srv1" || m["source"] != "junpanel" {
				t.Errorf("generic body: %v", m)
			}
		}
	}
	if _, e := buildPayload("sms", "h", notices, now); e == nil {
		t.Fatal("unknown format accepted")
	}
}

func TestWebhookResponseHandling(t *testing.T) {
	g := newRig(t)
	n := []notice{{Kind: "test", Title: "t"}}
	send := func(format string) error {
		return g.a.send(alertConfig{Format: format, URL: g.hook.srv.URL + "/hook?key=secret-key-123"}, n, g.now)
	}
	g.hook.set(200, `{"errcode":0,"errmsg":"ok"}`)
	for _, f := range []string{"generic", "wecom", "dingtalk", "feishu"} {
		if e := send(f); e != nil {
			t.Errorf("%s success rejected: %v", f, e)
		}
	}
	g.hook.set(200, `{"errcode":93000,"errmsg":"invalid webhook url"}`)
	if e := send("wecom"); e == nil || !strings.Contains(e.Error(), "invalid webhook url") {
		t.Errorf("wecom error body ignored: %v", e)
	}
	if e := send("dingtalk"); e == nil {
		t.Error("dingtalk error body ignored")
	}
	g.hook.set(200, `{"code":19024,"msg":"Key Words Not Found"}`)
	if e := send("feishu"); e == nil || !strings.Contains(e.Error(), "Key Words Not Found") {
		t.Errorf("feishu error body ignored: %v", e)
	}
	if e := send("generic"); e != nil {
		t.Errorf("generic must not interpret the body: %v", e)
	}
	g.hook.set(500, "boom")
	if e := send("generic"); e == nil || !strings.Contains(e.Error(), "500") {
		t.Errorf("HTTP 500: %v", e)
	}
	// A redirect is a failure and is never followed.
	var followed bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer redirect.Close()
	if e := g.a.send(alertConfig{Format: "generic", URL: redirect.URL}, n, g.now); e == nil || followed {
		t.Errorf("redirect followed or accepted: %v followed=%v", e, followed)
	}
	// Connection errors do not repeat the address, which may hold a key.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := dead.URL + "/hook?key=secret-key-123"
	dead.Close()
	e := g.a.send(alertConfig{Format: "generic", URL: url}, n, g.now)
	if e == nil || strings.Contains(e.Error(), "secret-key-123") || strings.Contains(e.Error(), "/hook") {
		t.Errorf("connection error leaks the address: %v", e)
	}
	for _, bad := range []string{"", "ftp://x.example/hook", "javascript:alert(1)", "http://", "https://a b.example", "http://x.example/" + strings.Repeat("a", 1000)} {
		if e := g.a.send(alertConfig{Format: "generic", URL: bad}, n, g.now); e == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	// The request carries the panel's name and version.
	g.hook.set(200, "ok")
	send("generic")
}

func TestAlertConfigAPI(t *testing.T) {
	g := newRig(t)
	os.Remove(g.a.alertConfigPath())
	h := g.a.routes()
	var got struct {
		Config  alertView
		Active  []activeAlert
		Formats map[string]string
	}
	json.Unmarshal(request(h, "GET", "/alerts", "", nil, nil).Body.Bytes(), &got)
	if got.Config.Notify || got.Config.URLSet || got.Config.DiskPercent != 90 || got.Config.CertDays != 14 || got.Config.RepeatHours != 12 || !got.Config.DiskOn || len(got.Formats) != 4 || got.Active == nil {
		t.Fatalf("defaults: %+v", got)
	}
	ok := map[string]any{"format": "wecom", "url": "https://qyapi.example.com/send?key=SECRET-KEY-9", "notify": true, "disk_on": true, "disk_percent": 85, "cert_on": true, "cert_days": 10, "container_on": true, "service_on": false, "repeat_hours": 6}
	with := func(over map[string]any) string {
		m := map[string]any{}
		for k, v := range ok {
			m[k] = v
		}
		for k, v := range over {
			m[k] = v
		}
		b, _ := json.Marshal(m)
		return string(b)
	}
	r := request(h, "POST", "/alerts/config", with(nil), nil, nil)
	if r.Code != 200 {
		t.Fatalf("save: %d %s", r.Code, r.Body.String())
	}
	var view alertView
	json.Unmarshal(r.Body.Bytes(), &view)
	if !view.URLSet || view.URLHost != "qyapi.example.com" || view.Format != "wecom" || view.DiskPercent != 85 || view.ServiceOn {
		t.Fatalf("view: %+v", view)
	}
	for _, body := range []string{r.Body.String(), request(h, "GET", "/alerts", "", nil, nil).Body.String()} {
		if strings.Contains(body, "SECRET-KEY-9") || strings.Contains(body, "/send") {
			t.Fatalf("the webhook address leaked: %s", body)
		}
	}
	if st, _ := os.Stat(g.a.alertConfigPath()); st.Mode().Perm() != 0600 {
		t.Fatalf("config mode %v", st.Mode().Perm())
	}
	// An empty address keeps the stored one; clear_url removes it and needs notify off.
	if r = request(h, "POST", "/alerts/config", with(map[string]any{"url": "", "disk_percent": 80}), nil, nil); r.Code != 200 {
		t.Fatalf("keep url: %d %s", r.Code, r.Body.String())
	}
	if cfg, _ := g.a.readAlertConfig(); cfg.URL != "https://qyapi.example.com/send?key=SECRET-KEY-9" || cfg.DiskPercent != 80 {
		t.Fatalf("stored: %+v", cfg)
	}
	if r = request(h, "POST", "/alerts/config", with(map[string]any{"url": "", "clear_url": true}), nil, nil); r.Code != 400 {
		t.Fatalf("notify without an address: %d", r.Code)
	}
	if r = request(h, "POST", "/alerts/config", with(map[string]any{"url": "", "clear_url": true, "notify": false}), nil, nil); r.Code != 200 {
		t.Fatalf("clear: %d %s", r.Code, r.Body.String())
	}
	if cfg, _ := g.a.readAlertConfig(); cfg.URL != "" || cfg.Notify {
		t.Fatalf("not cleared: %+v", cfg)
	}
	for name, over := range map[string]map[string]any{
		"format":         {"format": "sms"},
		"scheme":         {"url": "ftp://x.example/hook"},
		"low disk":       {"disk_percent": 49},
		"high disk":      {"disk_percent": 100},
		"cert days zero": {"cert_days": 0},
		"cert days huge": {"cert_days": 61},
		"repeat zero":    {"repeat_hours": 0},
		"repeat huge":    {"repeat_hours": 169},
		"unknown field":  {"command": "x"},
	} {
		if r = request(h, "POST", "/alerts/config", with(over), nil, nil); r.Code != 400 {
			t.Errorf("%s: %d %s", name, r.Code, r.Body.String())
		}
	}
	// Test notification: uses the stored address, reports a failure as an error.
	g.config(func(c *alertConfig) { c.URL, c.Format = g.hook.srv.URL, "generic" })
	if r = request(h, "POST", "/alerts/test", `{}`, nil, nil); r.Code != 200 || !strings.Contains(g.hook.last(), "测试") {
		t.Fatalf("test notification: %d %s / %s", r.Code, r.Body.String(), g.hook.last())
	}
	g.hook.set(500, "no")
	if r = request(h, "POST", "/alerts/test", `{}`, nil, nil); r.Code != 502 {
		t.Fatalf("failed test: %d", r.Code)
	}
	g.config(func(c *alertConfig) { c.URL = "" })
	if r = request(h, "POST", "/alerts/test", `{}`, nil, nil); r.Code != 400 {
		t.Fatalf("test without an address: %d", r.Code)
	}
}

// ---- disk ----

func TestDiskAlertLifecycle(t *testing.T) {
	g := newRig(t)
	g.tick()
	if g.hook.count() != 0 || len(g.state().Active) != 0 {
		t.Fatal("alert on a healthy disk")
	}
	g.disk = 92
	g.advance(time.Minute)
	st := g.tick()
	if len(st.Active) != 1 || st.Active["disk:/"] == nil || g.hook.count() != 1 {
		t.Fatalf("not triggered once: %v posts=%d", st.Active, g.hook.count())
	}
	if body := g.hook.last(); !strings.Contains(body, "磁盘空间不足：/") || !strings.Contains(body, "已用 92%") || !strings.Contains(body, "阈值 90%") {
		t.Fatalf("message: %s", body)
	}
	// Still bad: no repeat inside the interval, one reminder after it.
	g.advance(time.Hour)
	g.tick()
	if g.hook.count() != 1 {
		t.Fatalf("repeated notification after 1h: %d", g.hook.count())
	}
	g.advance(12 * time.Hour)
	g.tick()
	if g.hook.count() != 2 || !strings.Contains(g.hook.last(), "仍未解决") {
		t.Fatalf("no reminder: %d %s", g.hook.count(), g.hook.last())
	}
	// Dropping just under the threshold is not a recovery; clearly below it takes two clean checks.
	g.disk = 88
	g.advance(time.Minute)
	g.tick()
	g.advance(time.Minute)
	if st = g.tick(); st.Active["disk:/"] == nil || g.hook.count() != 2 {
		t.Fatal("recovered inside the hysteresis band")
	}
	g.disk = 80
	g.advance(time.Minute)
	if st = g.tick(); st.Active["disk:/"] == nil || g.hook.count() != 2 {
		t.Fatal("recovered after a single clean check")
	}
	g.advance(time.Minute)
	if st = g.tick(); len(st.Active) != 0 || g.hook.count() != 3 || !strings.Contains(g.hook.last(), "【已恢复】磁盘空间不足") {
		t.Fatalf("no recovery: %v posts=%d %s", st.Active, g.hook.count(), g.hook.last())
	}
	if want := []string{"resolved:disk:/", "reminder:disk:/", "triggered:disk:/"}; fmt.Sprint(g.kinds()) != fmt.Sprint(want) {
		t.Fatalf("history: %v", g.kinds())
	}
	// Disabling the rule clears its alert without a message.
	g.disk = 95
	g.advance(2 * time.Hour)
	g.tick()
	n := g.hook.count()
	g.config(func(c *alertConfig) { c.DiskOn = false })
	g.advance(time.Minute)
	if st = g.tick(); len(st.Active) != 0 || g.hook.count() != n {
		t.Fatalf("disabled rule: %v", st.Active)
	}
}

func TestDiskPathsOnOneFilesystemCountOnce(t *testing.T) {
	g := newRig(t)
	g.disk = 96
	g.tick()
	if st := g.state(); len(st.Active) != 1 || g.hook.count() != 1 || strings.Count(g.hook.last(), "【告警】") != 1 {
		t.Fatalf("one filesystem reported %d times", len(st.Active))
	}
	// Different filesystems are reported separately.
	g.a.diskFn = func(p string) map[string]uint64 {
		if p == "/" {
			return map[string]uint64{"total": 100 << 30, "used": 95 << 30}
		}
		return map[string]uint64{"total": 200 << 30, "used": 190 << 30}
	}
	g.advance(time.Minute)
	if st := g.tick(); len(st.Active) != 2 {
		t.Fatalf("separate filesystems: %v", st.Active)
	}
	g.a.diskFn = func(string) map[string]uint64 { return map[string]uint64{} }
	g.advance(time.Minute)
	g.tick() // unreadable usage must not raise or clear anything unexpectedly
}

// ---- certificates ----

func TestCertificateAlerts(t *testing.T) {
	g := newRig(t)
	real := time.Now()
	g.now = real
	dir := g.a.cfg.CaddyData
	day := 24 * time.Hour
	makeCert(t, dir, "R3", real.Add(-85*day), real.Add(5*day), []string{"a.example.com"}, nil)
	makeCert(t, dir, "R3", real.Add(-50*day), real.Add(40*day), []string{"b.example.com"}, nil)
	makeCert(t, dir, "E5", real.Add(-2*day), real.Add(4*day), nil, []net.IP{net.ParseIP("203.0.113.9")}) // six-day certificate with 4 days left
	makeCert(t, dir, "R3", real.Add(-95*day), real.Add(-2*day), []string{"expired.example.com"}, nil)
	makeCert(t, dir, "R3", real.Add(-89*day), real.Add(1*day), []string{"removed.example.org"}, nil) // site no longer served
	makeCert(t, dir, "R3", real.Add(-100*day), real.Add(-10*day), []string{"renewed.example.com"}, nil)
	makeCert(t, dir, "R3", real.Add(-10*day), real.Add(80*day), []string{"renewed.example.com"}, nil)
	servedHosts(g, "a.example.com", "b.example.com", "203.0.113.9", "expired.example.com", "renewed.example.com")

	st := g.tick()
	var keys []string
	for k := range st.Active {
		keys = append(keys, k)
	}
	if len(st.Active) != 2 || st.Active["cert:a.example.com"] == nil || st.Active["cert:expired.example.com"] == nil {
		t.Fatalf("active alerts: %v", keys)
	}
	if st.Active["cert:expired.example.com"].Severity != "critical" || !strings.Contains(st.Active["cert:expired.example.com"].Title, "已过期") {
		t.Fatalf("expired: %+v", st.Active["cert:expired.example.com"])
	}
	if d := st.Active["cert:a.example.com"].Detail; !strings.Contains(d, "剩余 4 天") && !strings.Contains(d, "剩余 5 天") {
		t.Fatalf("detail: %s", d)
	}
	if g.hook.count() != 1 || !strings.Contains(g.hook.last(), "a.example.com") || !strings.Contains(g.hook.last(), "expired.example.com") {
		t.Fatalf("one batched message expected: %d %s", g.hook.count(), g.hook.last())
	}

	// The six-day certificate is flagged only in the last fifth of its life (about 29 hours).
	g.now = real.Add(4*day - 20*time.Hour)
	g.a.certAt = time.Time{}
	g.tick()
	if g.state().Active["cert:203.0.113.9"] == nil {
		t.Fatal("short-lived certificate with 20 hours left not flagged")
	}

	// A longer warning window flags b.example.com too; the setting is applied.
	g.now = real
	g.a.certAt = time.Time{}
	g.config(func(c *alertConfig) { c.CertDays = 60 })
	g.tick()
	if g.state().Active["cert:b.example.com"] == nil {
		t.Fatal("60-day window ignored")
	}

	// Results are cached; the host list is read once per cache period.
	calls := 0
	json := `{"apps":{"http":{"servers":{"s":{"routes":[{"match":[{"host":["a.example.com"]}]}]}}}}}`
	g.a.capture = func(time.Duration, string, ...string) (string, error) { calls++; return json, nil }
	g.a.certAt = time.Time{}
	for i := 0; i < 3; i++ {
		g.advance(time.Minute)
		g.tick()
	}
	if calls != 1 {
		t.Fatalf("caddy host list read %d times in 3 minutes", calls)
	}
	g.advance(11 * time.Minute)
	g.tick()
	if calls != 2 {
		t.Fatalf("cache not refreshed: %d", calls)
	}

	// If the host list cannot be read every certificate is considered, rather than none.
	g.a.capture = func(time.Duration, string, ...string) (string, error) { return "", errors.New("caddy adapt failed") }
	g.a.certAt = time.Time{}
	certs, e := g.a.certificates(g.now, true)
	if e != nil || len(certs) != 6 {
		t.Fatalf("unreadable host list: %d certs %v", len(certs), e)
	}
}

func TestCertificateEndpointAndHelpers(t *testing.T) {
	g := newRig(t)
	h := g.a.routes()
	var empty struct {
		Certificates []map[string]any
		Found        bool
	}
	json.Unmarshal(request(h, "GET", "/certificates", "", nil, nil).Body.Bytes(), &empty)
	if empty.Found || len(empty.Certificates) != 0 {
		t.Fatalf("no caddy directory: %+v", empty)
	}
	real := time.Now()
	day := 24 * time.Hour
	g.a.now = func() time.Time { return real }
	makeCert(t, g.a.cfg.CaddyData, "R3", real.Add(-85*day), real.Add(5*day), []string{"a.example.com"}, nil)
	makeCert(t, g.a.cfg.CaddyData, "R3", real.Add(-30*day), real.Add(60*day), []string{"*.wild.example.com"}, nil)
	servedHosts(g, "a.example.com", "x.wild.example.com")
	r := request(h, "GET", "/certificates", "", nil, nil)
	var got struct {
		Certificates []struct {
			Name     string
			Issuer   string
			DaysLeft int  `json:"days_left"`
			Warn     bool `json:"warn"`
		}
		Found bool
	}
	json.Unmarshal(r.Body.Bytes(), &got)
	if !got.Found || len(got.Certificates) != 2 || got.Certificates[0].Name != "a.example.com" || !got.Certificates[0].Warn || got.Certificates[0].DaysLeft != 4 || got.Certificates[1].Warn || got.Certificates[1].Issuer != "R3" {
		t.Fatalf("certificates: %s", r.Body.String())
	}
	for in, want := range map[string]string{"https://1.2.3.4": "1.2.3.4", "Example.COM:443": "example.com", "[::1]:8443": "::1", "*.x.com": "*.x.com", "http://a.b/path?q=1": "a.b", " a.b ": "a.b", "": ""} {
		if got := hostOnly(in); got != want {
			t.Errorf("hostOnly(%q) = %q, want %q", in, got, want)
		}
	}
	certs, _ := readCerts(g.a.cfg.CaddyData)
	var wild certInfo
	for _, c := range certs {
		if strings.HasPrefix(c.Name, "*.") {
			wild = c
		}
	}
	if !certServes(wild, []string{"https://deep.wild.example.com"}) || certServes(wild, []string{"other.example.com"}) || !certServes(wild, []string{"*.wild.example.com"}) || certServes(wild, nil) {
		t.Fatal("wildcard matching")
	}
	// Broken files and oversized trees are skipped, not fatal.
	os.WriteFile(filepath.Join(g.a.cfg.CaddyData, "certificates", "junk.crt"), []byte("not a certificate"), 0600)
	os.Mkdir(filepath.Join(g.a.cfg.CaddyData, "certificates", "dir.crt"), 0755)
	if c, e := readCerts(g.a.cfg.CaddyData); e != nil || len(c) != 2 {
		t.Fatalf("junk files: %d %v", len(c), e)
	}
}

// ---- containers ----

func TestContainerStoppedAlertLifecycle(t *testing.T) {
	g := newRig(t)
	st := g.tick() // baseline: nothing is reported, running containers are remembered
	if g.hook.count() != 0 || len(st.Active) != 0 {
		t.Fatal("alert on the first check")
	}
	if want := []string{cid('a'), cid('b')}; fmt.Sprint(g.state().Running) != fmt.Sprint(want) {
		t.Fatalf("running set: %v (one-off containers must not be tracked)", g.state().Running)
	}
	g.dock.set("web", "exited", "Exited (137) 3 seconds ago")
	g.advance(30 * time.Second)
	st = g.tick()
	key := "container:" + cid('a')
	if st.Active[key] == nil || g.hook.count() != 1 || !strings.Contains(g.hook.last(), "容器已停止：web") || !strings.Contains(g.hook.last(), "退出码 137") {
		t.Fatalf("stopped container: %v posts=%d %s", st.Active, g.hook.count(), g.hook.last())
	}
	if fmt.Sprint(g.state().Running) != fmt.Sprint([]string{cid('b')}) {
		t.Fatalf("running set after the stop: %v", g.state().Running)
	}
	g.advance(30 * time.Second)
	g.tick()
	g.advance(30 * time.Second)
	g.tick()
	if g.hook.count() != 1 {
		t.Fatalf("repeated notifications: %d", g.hook.count())
	}
	// Back up: two clean checks to recover.
	g.dock.set("web", "running", "Up 5 seconds")
	g.advance(30 * time.Second)
	if g.tick(); g.state().Active[key] == nil {
		t.Fatal("recovered after one check")
	}
	g.advance(30 * time.Second)
	if g.tick(); g.state().Active[key] != nil || g.hook.count() != 2 || !strings.Contains(g.hook.last(), "已恢复") {
		t.Fatalf("no recovery: %d %s", g.hook.count(), g.hook.last())
	}
	// It is tracked again, so a second crash is reported — but a quick relapse is not announced again.
	g.dock.set("web", "exited", "Exited (1) 2 seconds ago")
	g.advance(30 * time.Second)
	st = g.tick()
	if st.Active[key] == nil || g.hook.count() != 2 {
		t.Fatalf("relapse: active=%v posts=%d (flapping must be quiet)", st.Active[key] != nil, g.hook.count())
	}
	if h := g.state().History[0]; h.Kind != "triggered" || !strings.Contains(h.Detail, "抑制") {
		t.Fatalf("relapse history: %+v", h)
	}
	g.advance(13 * time.Hour)
	g.tick()
	if g.hook.count() != 3 || !strings.Contains(g.hook.last(), "仍未解决") {
		t.Fatalf("reminder: %d %s", g.hook.count(), g.hook.last())
	}
	// Removing the container clears its alert without a message.
	g.dock.remove("web")
	g.advance(time.Minute)
	if st = g.tick(); len(st.Active) != 0 || g.hook.count() != 3 {
		t.Fatalf("removed container: %v posts=%d", st.Active, g.hook.count())
	}
}

func TestContainerStopsThatAreNotFailures(t *testing.T) {
	g := newRig(t)
	g.tick()
	// A clean exit is not an alert.
	g.dock.set("db", "exited", "Exited (0) 1 second ago")
	g.advance(30 * time.Second)
	if g.tick(); len(g.state().Active) != 0 || g.hook.count() != 0 {
		t.Fatal("exit code 0 reported")
	}
	// Containers the panel stops are expected; the expectation is used up once.
	g.dock.set("db", "running", "Up 1 second")
	g.advance(30 * time.Second)
	g.tick()
	g.a.expectStop(cid('b'))
	g.dock.set("db", "exited", "Exited (137) 1 second ago")
	g.advance(30 * time.Second)
	if g.tick(); len(g.state().Active) != 0 || g.hook.count() != 0 {
		t.Fatal("panel-initiated stop reported")
	}
	// An expectation that was never used expires.
	g.dock.set("db", "running", "Up 1 second")
	g.advance(30 * time.Second)
	g.tick()
	g.a.expectStop(cid('b'))
	g.advance(expectedFor + time.Minute)
	g.dock.set("db", "exited", "Exited (137) 1 second ago")
	g.tick()
	if g.state().Active["container:"+cid('b')] == nil {
		t.Fatal("stale expectation hid a real failure")
	}
	// Apps being worked on (busy) are skipped, and forgotten, so the end of the operation is quiet too.
	g.dock.set("db", "running", "Up 1 second")
	for i := 0; i < 2; i++ {
		g.advance(30 * time.Second)
		g.tick()
	}
	g.dock.mu.Lock()
	g.dock.rows = append(g.dock.rows, fakeContainer{ID: cid('e'), Name: "shop-app", State: "running", Status: "Up 1 hour", Project: "junpanel-shop"})
	g.dock.mu.Unlock()
	g.a.storeApps([]appRecord{{ID: "shop", Name: "Shop", Template: "redis", Port: 7000}})
	g.advance(30 * time.Second)
	g.tick()
	g.a.storeApps([]appRecord{{ID: "shop", Name: "Shop", Template: "redis", Port: 7000, Busy: true}})
	g.dock.set("shop-app", "exited", "Exited (137) 1 second ago")
	g.advance(30 * time.Second)
	g.tick()
	g.a.storeApps([]appRecord{{ID: "shop", Name: "Shop", Template: "redis", Port: 7000}})
	g.advance(30 * time.Second)
	g.tick()
	if g.state().Active["container:"+cid('e')] != nil {
		t.Fatal("a stop during a panel operation was reported afterwards")
	}
	// Without the busy flag the same stop is a failure.
	g.dock.set("shop-app", "running", "Up 1 second")
	g.advance(30 * time.Second)
	g.tick()
	g.dock.set("shop-app", "exited", "Exited (137) 1 second ago")
	g.advance(30 * time.Second)
	g.tick()
	if g.state().Active["container:"+cid('e')] == nil {
		t.Fatal("real failure of an app container missed")
	}
}

func TestCrashLoopingContainerIsReportedWithoutBeingSeenRunning(t *testing.T) {
	g := newRig(t)
	g.dock.mu.Lock()
	g.dock.rows = append(g.dock.rows, fakeContainer{ID: cid('f'), Name: "flaky", State: "restarting", Status: "Restarting (1) 4 seconds ago"})
	g.dock.mu.Unlock()
	st := g.tick()
	a := st.Active["container:"+cid('f')]
	if a == nil || !strings.Contains(a.Title, "容器反复重启：flaky") || g.hook.count() != 1 {
		t.Fatalf("%+v posts=%d", a, g.hook.count())
	}
	g.dock.set("flaky", "dead", "Dead")
	g.advance(30 * time.Second)
	if g.tick(); g.state().Active["container:"+cid('f')] == nil {
		t.Fatal("dead container dropped")
	}
	// Containers that merely exist in a stopped state (never seen running) are not reported.
	if g.state().Active["container:"+cid('d')] != nil {
		t.Fatal("a long-stopped container was reported")
	}
}

func TestContainerAlertsSurviveAgentRestart(t *testing.T) {
	g := newRig(t)
	g.tick()
	// The agent is down while the container dies; the next agent still notices.
	g.dock.set("web", "exited", "Exited (139) 2 minutes ago")
	g.advance(5 * time.Minute)
	restarted, e := newAgent(g.a.cfg)
	if e != nil {
		t.Fatal(e)
	}
	restarted.docker, restarted.runner, restarted.diskFn, restarted.host = g.dock.client(), g.a.runner, g.a.diskFn, "testhost"
	restarted.now = func() time.Time { return g.now }
	restarted.capture, restarted.processConfigs = g.a.capture, g.a.processConfigs
	st, e := restarted.alertTick(g.now)
	if e != nil || st.Active["container:"+cid('a')] == nil || g.hook.count() != 1 {
		t.Fatalf("not noticed after restart: %v %v posts=%d", st.Active, e, g.hook.count())
	}
	// Another restart does not announce the same alert again.
	again, _ := newAgent(g.a.cfg)
	again.docker, again.runner, again.diskFn, again.host = g.dock.client(), g.a.runner, g.a.diskFn, "testhost"
	again.now = func() time.Time { return g.now.Add(time.Minute) }
	again.capture, again.processConfigs = g.a.capture, g.a.processConfigs
	if _, e = again.alertTick(g.now.Add(time.Minute)); e != nil || g.hook.count() != 1 {
		t.Fatalf("announced again after a restart: %v posts=%d", e, g.hook.count())
	}
}

// ---- services ----

func TestServiceAlerts(t *testing.T) {
	g := newRig(t)
	g.caddy = "inactive"
	g.tick()
	if len(g.state().Active) != 0 {
		t.Fatal("one failed check is not enough")
	}
	g.advance(30 * time.Second)
	if g.tick(); g.state().Active["service:caddy"] == nil || g.hook.count() != 1 || !strings.Contains(g.hook.last(), "Caddy 未运行") || !strings.Contains(g.hook.last(), "inactive") {
		t.Fatalf("caddy: %v posts=%d %s", g.state().Active, g.hook.count(), g.hook.last())
	}
	// systemctl error text (for example on a host without systemd) is not shown as a state.
	g.caddy = "System has not been booted with systemd as init system\nFailed to connect to bus: Host is down"
	g.advance(30 * time.Second)
	g.tick()
	if d := g.state().Active["service:caddy"].Detail; strings.Contains(d, "booted") || !strings.Contains(d, "未知") {
		t.Fatalf("raw systemctl output in the detail: %s", d)
	}
	g.caddy = "active"
	for i := 0; i < 2; i++ {
		g.advance(30 * time.Second)
		g.tick()
	}
	if g.state().Active["service:caddy"] != nil || g.hook.count() != 2 {
		t.Fatalf("caddy recovery: posts=%d", g.hook.count())
	}
	// Docker: three failed checks; container alerts stay as they were meanwhile.
	g.dock.set("web", "exited", "Exited (1) 1 second ago")
	g.advance(30 * time.Second)
	g.tick()
	posts := g.hook.count()
	g.dock.mu.Lock()
	g.dock.fail = true
	g.dock.mu.Unlock()
	for i := 1; i <= 3; i++ {
		g.advance(30 * time.Second)
		g.tick()
		active := g.state().Active
		if i < 3 && active["service:docker"] != nil {
			t.Fatalf("docker flagged after %d failed checks", i)
		}
		if active["container:"+cid('a')] == nil {
			t.Fatal("container alert lost while docker was unreachable")
		}
	}
	if g.state().Active["service:docker"] == nil || g.hook.count() != posts+1 || !strings.Contains(g.hook.last(), "Docker 无法访问") {
		t.Fatalf("docker: %v posts=%d", g.state().Active, g.hook.count())
	}
	g.dock.mu.Lock()
	g.dock.fail = false
	g.dock.mu.Unlock()
	for i := 0; i < 2; i++ {
		g.advance(30 * time.Second)
		g.tick()
	}
	if g.state().Active["service:docker"] != nil {
		t.Fatal("docker alert not cleared")
	}
}

// ---- delivery behaviour ----

func TestSeveralAlertsAreSentTogether(t *testing.T) {
	g := newRig(t)
	g.tick()
	g.disk = 95
	g.caddy = "inactive"
	g.dock.set("web", "exited", "Exited (1) 1 second ago")
	g.advance(30 * time.Second)
	g.tick()
	g.advance(30 * time.Second)
	g.tick()
	if g.hook.count() != 2 {
		t.Fatalf("%d messages, want a first batch (disk + container) and a second (caddy)", g.hook.count())
	}
	g.hook.mu.Lock()
	first := g.hook.bodies[0]
	g.hook.mu.Unlock()
	if !strings.Contains(first, "磁盘空间不足") || !strings.Contains(first, "容器已停止：web") {
		t.Fatalf("first batch: %s", first)
	}
}

func TestFailedDeliveryIsRetriedThenGivenUp(t *testing.T) {
	g := newRig(t)
	g.tick()
	g.hook.set(500, "down")
	g.disk = 95
	g.advance(30 * time.Second)
	g.tick()
	if g.hook.count() != 1 {
		t.Fatalf("first attempt: %d", g.hook.count())
	}
	if d := g.state().Delivery; d == nil || d.OK || !strings.Contains(d.Message, "重试") {
		t.Fatalf("delivery state: %+v", d)
	}
	g.advance(time.Minute)
	g.tick()
	if g.hook.count() != 1 {
		t.Fatal("retried too soon")
	}
	g.advance(5 * time.Minute)
	g.tick()
	g.advance(5 * time.Minute)
	g.tick()
	if g.hook.count() != deliveryTries {
		t.Fatalf("%d attempts, want %d", g.hook.count(), deliveryTries)
	}
	if d := g.state().Delivery; d.OK || !strings.Contains(d.Message, "放弃") {
		t.Fatalf("after giving up: %+v", d)
	}
	g.advance(time.Hour)
	g.tick()
	if g.hook.count() != deliveryTries {
		t.Fatal("kept retrying a dropped notification")
	}
	// A later notice goes out normally once the receiver is back.
	g.hook.set(200, "ok")
	g.disk = 40
	g.advance(time.Minute)
	g.tick()
	g.advance(time.Minute)
	g.tick()
	if d := g.state().Delivery; d == nil || !d.OK || g.hook.count() != deliveryTries+1 || !strings.Contains(g.hook.last(), "已恢复") {
		t.Fatalf("recovery notice: %+v posts=%d", d, g.hook.count())
	}
	// A transient failure that clears within the retries is delivered once.
	g.hook.set(500, "down")
	g.disk = 96
	g.advance(3 * time.Hour)
	g.tick()
	g.hook.set(200, "ok")
	n := g.hook.count()
	g.advance(6 * time.Minute)
	g.tick()
	g.advance(time.Hour)
	g.tick()
	if g.hook.count() != n+1 || !g.state().Delivery.OK {
		t.Fatalf("retry after recovery: posts %d -> %d", n, g.hook.count())
	}
}

func TestAlertsAreTrackedWithoutANotificationChannel(t *testing.T) {
	g := newRig(t)
	g.config(func(c *alertConfig) { c.Notify, c.URL = false, "" })
	g.disk = 95
	g.tick()
	st := g.state()
	if st.Active["disk:/"] == nil || g.hook.count() != 0 || len(st.History) != 1 {
		t.Fatalf("active=%v posts=%d history=%d", st.Active, g.hook.count(), len(st.History))
	}
	// Turning notifications on later does not replay what happened before.
	g.config(func(c *alertConfig) { c.Notify, c.URL = true, g.hook.srv.URL })
	g.advance(time.Minute)
	g.tick()
	if g.hook.count() != 0 {
		t.Fatal("old alerts replayed")
	}
}

func TestAlertStateSurvivesRestartWithoutRenotifying(t *testing.T) {
	g := newRig(t)
	g.disk = 95
	g.tick()
	if g.hook.count() != 1 {
		t.Fatal("setup")
	}
	restarted, e := newAgent(g.a.cfg)
	if e != nil {
		t.Fatal(e)
	}
	restarted.docker, restarted.runner, restarted.diskFn, restarted.host = g.dock.client(), g.a.runner, g.a.diskFn, "testhost"
	restarted.capture, restarted.processConfigs = g.a.capture, g.a.processConfigs
	g.advance(time.Minute)
	st, e := restarted.alertTick(g.now)
	if e != nil || st.Active["disk:/"] == nil || g.hook.count() != 1 {
		t.Fatalf("%v active=%v posts=%d", e, st.Active, g.hook.count())
	}
	if n := restarted.activeAlertCount(); n != 1 {
		t.Fatalf("active count %d", n)
	}
	if st, _ := os.Stat(restarted.alertStatePath()); st.Mode().Perm() != 0600 {
		t.Fatal("state file mode")
	}
}

func TestAlertCheckEndpointAndHistoryLimit(t *testing.T) {
	g := newRig(t)
	h := g.a.routes()
	g.disk = 95
	r := request(h, "POST", "/alerts/check", `{}`, nil, nil)
	var got struct {
		Active  []activeAlert
		History []alertEvent
	}
	json.Unmarshal(r.Body.Bytes(), &got)
	if r.Code != 200 || len(got.Active) != 1 || got.Active[0].Key != "disk:/" || len(got.History) != 1 {
		t.Fatalf("check: %d %s", r.Code, r.Body.String())
	}
	st, _ := g.a.readAlertState()
	for i := 0; i < maxHistory+30; i++ {
		g.advance(13 * time.Hour)
		g.tick()
	}
	st = g.state()
	if len(st.History) != maxHistory {
		t.Fatalf("history length %d", len(st.History))
	}
}
