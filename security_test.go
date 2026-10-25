package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testAccount(t *testing.T) (*authStore, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "account.json")
	password := "TestPasswordForJunPanel!"
	if e := atomicJSON(p, newAccount("admin", password), 0600); e != nil {
		t.Fatal(e)
	}
	a, e := newAuth(p, false)
	if e != nil {
		t.Fatal(e)
	}
	return a, password
}
func TestAuthenticationLifecycle(t *testing.T) {
	a, pw := testAccount(t)
	token, s, e := a.login("127.0.0.1", "admin", pw, "")
	if e != nil || len(token) != 64 || len(s.CSRF) != 64 {
		t.Fatalf("login: %v", e)
	}
	if a.Account.Hash == pw || !hmac.Equal([]byte(a.Account.Hash), []byte(derive(pw, a.Account.Salt))) {
		t.Fatal("hash derivation")
	}
	if second := newAccount("admin", pw); second.Hash == a.Account.Hash || second.Salt == a.Account.Salt {
		t.Fatal("salts reused")
	}
	before := s.Seen
	a.Sessions[token] = loginSession{s.CSRF, s.Revision, s.Born, before.Add(-time.Minute)}
	_, _ = a.get(token, false)
	if !a.Sessions[token].Seen.Equal(before.Add(-time.Minute)) {
		t.Fatal("polling extended session")
	}
	_, _ = a.get(token, true)
	if !a.Sessions[token].Seen.After(before.Add(-time.Minute)) {
		t.Fatal("heartbeat did not touch")
	}
	old := a.Sessions[token]
	old.Born = time.Now().Add(-9 * time.Hour)
	a.Sessions[token] = old
	if _, e = a.get(token, true); e == nil {
		t.Fatal("absolute lifetime bypass")
	}
	token, _, _ = a.login("127.0.0.1", "admin", pw, "")
	old = a.Sessions[token]
	old.Seen = time.Now().Add(-31 * time.Minute)
	a.Sessions[token] = old
	a.OTPSetup[token] = "pending"
	if _, e = a.get(token, false); e == nil || a.OTPSetup[token] != "" {
		t.Fatal("idle expiry / setup cleanup")
	}
	token, _, _ = a.login("127.0.0.1", "admin", pw, "")
	if e = a.change(pw, "汉字密码仅七字", ""); e == nil {
		t.Fatal("password minimum counted bytes instead of characters")
	}
	if e = a.change(pw, "Jun2026!", ""); e != nil {
		t.Fatal(e)
	}
	if _, e = a.get(token, false); e == nil {
		t.Fatal("password change retained session")
	}
	token, _, e = a.login("127.0.0.1", "admin", "Jun2026!", "")
	if e != nil {
		t.Fatal(e)
	}
	a.logout(token)
	if _, e = a.get(token, false); e == nil {
		t.Fatal("logout retained session")
	}
}
func TestLockoutAndExternalReset(t *testing.T) {
	a, pw := testAccount(t)
	for i := 0; i < 5; i++ {
		if _, _, e := a.login("127.0.0.1", "admin", "incorrect", ""); e == nil {
			t.Fatal("accepted invalid password")
		}
	}
	if _, _, e := a.login("127.0.0.1", "admin", pw, ""); e == nil || e.(*fault).Code != 429 {
		t.Fatal("lockout not enforced")
	}
	if _, _, e := a.login("203.0.113.9", "admin", pw, ""); e != nil {
		t.Fatalf("lockout of one address blocked another: %v", e)
	}
	a.Clients["127.0.0.1"].Locked = time.Now().Add(-time.Second)
	token, _, e := a.login("127.0.0.1", "admin", pw, "")
	if e != nil {
		t.Fatal(e)
	}
	if e = atomicJSON(a.Path, newAccount("admin", "ResetPassword2026!"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = a.get(token, false); e == nil {
		t.Fatal("external reset did not invalidate session")
	}
}
func TestPerAddressAndGlobalLimits(t *testing.T) {
	a, pw := testAccount(t)
	if _, _, e := a.login("198.51.100.7", "admin", pw, ""); e != nil {
		t.Fatal(e)
	}
	// Each failure runs the full key derivation, so start just below the ceiling.
	a.Global = attempts{Failures: globalFailures - 1, First: time.Now()}
	if _, _, e := a.login("10.0.0.1", "admin", "incorrect", ""); e == nil {
		t.Fatal("accepted invalid password")
	}
	if _, _, e := a.login("192.0.2.200", "admin", pw, ""); e == nil || e.(*fault).Code != 429 {
		t.Fatal("global ceiling not enforced for a new address")
	}
	if _, _, e := a.login("198.51.100.7", "admin", pw, ""); e != nil {
		t.Fatalf("recently trusted address locked out: %v", e)
	}
	if limitKey("2001:db8:1:2:3:4:5:6") != limitKey("2001:db8:1:2:ffff::1") || limitKey("2001:db8:1:2::1") == limitKey("2001:db8:1:3::1") {
		t.Fatal("IPv6 addresses are not grouped by /64")
	}
	for i := 0; i < maxClients+10; i++ {
		a.client(fmt.Sprintf("2001:db8:%x::1", i), time.Now())
	}
	if len(a.Clients) > maxClients {
		t.Fatal("client table unbounded")
	}
	cases := []struct{ remote, forwarded, want string }{
		{"127.0.0.1:5000", "", "127.0.0.1"},
		{"127.0.0.1:5000", "203.0.113.5", "203.0.113.5"},
		{"127.0.0.1:5000", "1.1.1.1, 203.0.113.5", "203.0.113.5"},
		{"127.0.0.1:5000", "not-an-ip", "127.0.0.1"},
		{"192.0.2.1:5000", "203.0.113.5", "192.0.2.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "http://127.0.0.1:8787/", nil)
		r.RemoteAddr = c.remote
		if c.forwarded != "" {
			r.Header.Set("X-Forwarded-For", c.forwarded)
		}
		if got := clientIP(r); got != c.want {
			t.Fatalf("clientIP(%q, %q) = %q", c.remote, c.forwarded, got)
		}
	}
}
func currentOTP(secret string) string { return otpAt(secret, time.Now()) }
func otpAt(secret string, at time.Time) string {
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(at.Unix()/30))
	h := hmac.New(sha1.New, key)
	h.Write(counter[:])
	sum := h.Sum(nil)
	off := sum[len(sum)-1] & 15
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1000000)
}
func TestTOTPEnableDisable(t *testing.T) {
	// RFC 6238 SHA-1 test vector at 59 seconds, truncated to six digits.
	if !validOTP("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", "287082", time.Unix(59, 0)) {
		t.Fatal("TOTP vector")
	}
	a, pw := testAccount(t)
	token, _, _ := a.login("127.0.0.1", "admin", pw, "")
	secret, e := a.setupOTP(token, pw)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.enableOTP(token, currentOTP(secret)); e != nil {
		t.Fatal(e)
	}
	if _, e = a.get(token, false); e == nil {
		t.Fatal("TOTP enable retained session")
	}
	if _, _, e = a.login("127.0.0.1", "admin", pw, ""); e == nil {
		t.Fatal("TOTP optional after enabling")
	}
	code := currentOTP(secret)
	if _, _, e = a.login("127.0.0.1", "admin", pw, code); e != nil {
		t.Fatal(e)
	}
	if _, _, e = a.login("127.0.0.1", "admin", pw, code); e == nil {
		t.Fatal("TOTP code replayed at login")
	}
	if e = a.confirm("127.0.0.1", pw, code); e == nil {
		t.Fatal("TOTP code replayed at reauthentication")
	}
	if e = a.disableOTP(pw, code); e == nil {
		t.Fatal("TOTP code replayed to disable")
	}
	if e = a.disableOTP(pw, otpAt(secret, time.Now().Add(30*time.Second))); e != nil {
		t.Fatal(e)
	}
	if a.Account.TOTP != "" {
		t.Fatal("TOTP not disabled")
	}
}
func request(h http.Handler, method, path, content string, headers map[string]string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, strings.NewReader(content))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestHTTPBoundary(t *testing.T) {
	web, e := newWeb("", "unused", 8787, true)
	if e != nil {
		t.Fatal(e)
	}
	web.assets, _ = fs.Sub(assets, "web")
	h := web.routes()
	w := request(h, "GET", "/api/overview", "", nil, nil)
	if w.Code != 401 {
		t.Fatalf("unauthenticated overview %d", w.Code)
	}
	r := httptest.NewRequest("GET", "http://evil.test:8787/", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("Host rebinding")
	}
	w = request(h, "POST", "/api/login", `{"user":"demo","password":"JunPanelPreview2026!"}`, nil, nil)
	if w.Code != 403 {
		t.Fatal("missing origin accepted")
	}
	headers := map[string]string{"Origin": "http://127.0.0.1:8787", "Content-Type": "application/json"}
	w = request(h, "POST", "/api/login", `{"user":"demo","password":"JunPanelPreview2026!","otp":""}`, headers, nil)
	if w.Code != 200 {
		t.Fatalf("login %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie flags")
	}
	cookie := cookies[0]
	var session struct{ CSRF string }
	json.Unmarshal(w.Body.Bytes(), &session)
	if w = request(h, "POST", "/api/logout", `{}`, headers, cookie); w.Code != 403 {
		t.Fatal("missing CSRF")
	}
	headers["X-CSRF-Token"] = session.CSRF
	if w = request(h, "POST", "/api/sites", `{}`, headers, cookie); w.Code != 403 || !strings.Contains(w.Body.String(), "演示") {
		t.Fatal("demo accepted mutation")
	}
	if w = request(h, "GET", "/api/overview", "", nil, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"demo":true`) {
		t.Fatal("demo overview")
	}
	cross := map[string]string{"Sec-Fetch-Site": "same-site"}
	if w = request(h, "GET", "/api/overview", "", cross, cookie); w.Code != 403 {
		t.Fatal("same-site cross-origin read accepted")
	}
	if w = request(h, "GET", "/app.js", "", nil, nil); w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("asset / CSP")
	}
	if w = request(h, "GET", "/style.css", "", nil, nil); w.Code != 200 {
		t.Fatal("JunPanel design stylesheet missing")
	}
	if w = request(h, "GET", "/style.css?v="+version, "", nil, nil); w.Code != 200 || w.Header().Get("X-JunPanel-Version") != version {
		t.Fatal("versioned asset URL or version header")
	}
	if w = request(h, "GET", "/", "", nil, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `<meta name="junpanel-version" content="`+version+`">`) || !strings.Contains(w.Body.String(), `src="/app.js?v=`+version+`"`) || !strings.Contains(w.Body.String(), `href="/style.css?v=`+version+`"`) || strings.Contains(w.Body.String(), `"/app.js"`) {
		t.Fatal("page does not carry its version")
	}
	if w = request(h, "GET", "/api/meta", "", nil, nil); w.Header().Get("X-JunPanel-Version") != version {
		t.Fatal("API response without version header")
	}
	if w = request(h, "GET", "/fonts/geist-variable.woff2", "", nil, nil); w.Code != 200 || w.Header().Get("Content-Type") != "font/woff2" || w.Body.Len() < 1000 {
		t.Fatalf("bundled font not served: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	for _, path := range []string{"/fonts/LICENSE.txt", "/fonts/../app.js", "/fonts/%2e%2e/account.json", "/fonts/nope.woff2", "/fonts/a/b.woff2", "/fonts/"} {
		if w = request(h, "GET", path, "", nil, nil); w.Code == 200 && path != "/fonts/../app.js" {
			t.Fatalf("%s served", path)
		}
	}
	if w = request(h, "GET", "/account.json", "", nil, cookie); w.Code != 404 {
		t.Fatal("arbitrary asset served")
	}
	if w = request(h, "POST", "/api/logout", `{}`, headers, cookie); w.Code != 200 {
		t.Fatal("logout")
	}
	if w = request(h, "GET", "/api/session", "", nil, cookie); w.Code != 401 {
		t.Fatal("logout retained web session")
	}
}
func TestTerminalTicketBoundToSession(t *testing.T) {
	a, pw := testAccount(t)
	w, e := newWeb(a.Path, "unused", 8787, false)
	if e != nil {
		t.Fatal(e)
	}
	h := w.routes()
	t1, s1, _ := w.auth.login("127.0.0.1", "admin", pw, "")
	t2, _, _ := w.auth.login("127.0.0.1", "admin", pw, "")
	headers := map[string]string{"Origin": "http://127.0.0.1:8787", "X-CSRF-Token": s1.CSRF}
	r := request(h, "POST", "/api/terminal/ticket", `{"container":"`+strings.Repeat("a", 64)+`"}`, headers, &http.Cookie{Name: "junpanel_session", Value: t1})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var ticket struct{ Ticket string }
	json.Unmarshal(r.Body.Bytes(), &ticket)
	headers["Upgrade"] = "websocket"
	r = request(h, "GET", "/api/terminal?ticket="+ticket.Ticket, "", headers, &http.Cookie{Name: "junpanel_session", Value: t2})
	if r.Code != 401 {
		t.Fatal("ticket crossed sessions")
	}
	if _, exists := w.tickets[ticket.Ticket]; exists {
		t.Fatal("ticket was not consumed")
	}
}
func testAgent(t *testing.T) *agent {
	t.Helper()
	dir := t.TempDir()
	cfg := agentConfig{State: filepath.Join(dir, "state"), Root: filepath.Join(dir, "root"), Caddy: filepath.Join(dir, "Caddyfile"), Fragments: filepath.Join(dir, "fragments")}
	if e := os.MkdirAll(cfg.Fragments, 0755); e != nil {
		t.Fatal(e)
	}
	a, e := newAgent(cfg)
	if e != nil {
		t.Fatal(e)
	}
	a.runner = func(time.Duration, string, ...string) (string, error) { return "active", nil }
	a.capture = func(time.Duration, string, ...string) (string, error) { return `{"apps":{"http":{"servers":{}}}}`, nil }
	a.processConfigs = func() ([]caddyConfig, error) { return nil, nil }
	return a
}
func TestFilesContainmentConflictRecycle(t *testing.T) {
	a := testAgent(t)
	for _, p := range []string{"../secret", "a/../x", "/.trash/x", "/.junpanel-tmp", "a\\b"} {
		if _, e := virtualPath(p); e == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	if e := a.writeVirtual("/index.html", []byte("one"), "", false); e != nil {
		t.Fatal(e)
	}
	if e := a.writeVirtual("/index.html", []byte("overwrite"), "", false); e == nil {
		t.Fatal("upload overwrote existing file")
	}
	if e := a.writeVirtual("/index.html", []byte("two"), fileDigest([]byte("wrong")), true); e == nil {
		t.Fatal("stale edit overwrote file")
	}
	if e := a.writeVirtual("/index.html", []byte("two"), fileDigest([]byte("one")), true); e != nil {
		t.Fatal(e)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("outside"), 0600)
	if e := os.Symlink(outside, filepath.Join(a.cfg.Root, "link")); e == nil {
		if _, _, e = a.readVirtual("/link", 100); e == nil {
			t.Fatal("symlink escaped root")
		}
	} else if runtime.GOOS != "windows" {
		t.Fatal(e)
	}
	h := a.routes()
	r := request(h, "POST", "/files/trash", `{"path":"/index.html"}`, nil, nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var record trashRecord
	json.Unmarshal(r.Body.Bytes(), &record)
	if _, e := os.Stat(filepath.Join(a.cfg.Root, "index.html")); !os.IsNotExist(e) {
		t.Fatal("trash did not move source")
	}
	if r = request(h, "POST", "/trash/"+record.ID+"/restore", `{}`, nil, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	b, e := os.ReadFile(filepath.Join(a.cfg.Root, "index.html"))
	if e != nil || string(b) != "two" {
		t.Fatal("restore content")
	}
	if r = request(h, "POST", "/files/rename", `{"path":"/sites","to":"/renamed"}`, nil, nil); r.Code != 403 {
		t.Fatal("moved site root")
	}
	if e = os.WriteFile(filepath.Join(a.cfg.Root, "huge"), make([]byte, (1<<20)+1), 0644); e != nil {
		t.Fatal(e)
	}
	if _, _, e = a.readVirtual("huge", 1<<20); e == nil {
		t.Fatal("read size bound")
	}
}
func TestTrashPurge(t *testing.T) {
	a := testAgent(t)
	h := a.routes()
	trash := func(name string) trashRecord {
		t.Helper()
		if e := os.MkdirAll(filepath.Join(a.cfg.Root, name, "inner"), 0755); e != nil {
			t.Fatal(e)
		}
		os.WriteFile(filepath.Join(a.cfg.Root, name, "inner", "f.txt"), []byte("x"), 0644)
		r := request(h, "POST", "/files/trash", `{"path":"/`+name+`"}`, nil, nil)
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
		var record trashRecord
		json.Unmarshal(r.Body.Bytes(), &record)
		return record
	}
	listed := func() int {
		var rows []trashRecord
		json.Unmarshal(request(h, "GET", "/trash", "", nil, nil).Body.Bytes(), &rows)
		return len(rows)
	}
	one, two := trash("one"), trash("two")
	if r := request(h, "DELETE", "/trash/t_zz", "", nil, nil); r.Code != 400 {
		t.Fatal("invalid trash id accepted")
	}
	if r := request(h, "DELETE", "/trash/"+one.ID, "", nil, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if _, e := os.Lstat(filepath.Join(a.cfg.Root, ".trash", one.ID+".data")); !os.IsNotExist(e) || listed() != 1 {
		t.Fatal("permanent delete left data behind")
	}
	if r := request(h, "DELETE", "/trash/"+one.ID, "", nil, nil); r.Code != 404 {
		t.Fatal("deleted record reported again")
	}
	// Age the remaining record past retention; only it should be cleaned.
	meta := filepath.Join(a.cfg.Root, ".trash", two.ID+".json")
	two.At = time.Now().Add(-trashRetention - time.Hour)
	b, _ := json.Marshal(two)
	os.WriteFile(meta, b, 0600)
	three := trash("three")
	if n, e := a.emptyTrash(time.Now().Add(-trashRetention)); e != nil || n != 1 || listed() != 1 {
		t.Fatalf("retention cleanup removed %d: %v", n, e)
	}
	if r := request(h, "POST", "/trash/empty", `{}`, nil, nil); r.Code != 200 || !strings.Contains(r.Body.String(), `"removed":1`) {
		t.Fatal(r.Body.String())
	}
	entries, _ := os.ReadDir(filepath.Join(a.cfg.Root, ".trash"))
	if len(entries) != 0 || listed() != 0 {
		t.Fatalf("empty left %d entries (%s)", len(entries), three.ID)
	}
	if _, e := os.Stat(a.cfg.Root); e != nil {
		t.Fatal("root removed")
	}
}
func TestCaddyTransactionRollbackAndPreservation(t *testing.T) {
	a := testAgent(t)
	root := a.cfg.Caddy
	oldRoot := "# existing user config\nlegacy.example.com { respond \"hello\" }\nimport " + filepath.ToSlash(a.cfg.Fragments) + "/*.caddy\n"
	os.WriteFile(root, []byte(oldRoot), 0644)
	fragment := filepath.Join(a.cfg.Fragments, "managed.caddy")
	old := []byte("# old managed configuration\n")
	os.WriteFile(fragment, old, 0644)
	failed := true
	var reloads int
	a.runner = func(_ time.Duration, name string, args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "validate") && failed {
			return "bad", errors.New("validation")
		}
		if len(args) > 0 && args[0] == "reload" {
			reloads++
		}
		return "active", nil
	}
	rows := []site{{ID: "s_" + strings.Repeat("a", 20), Name: "Test", Kind: "proxy", Domains: []string{"new.example.com"}, Upstream: "http://127.0.0.1:8080", Enabled: true, Created: time.Now()}}
	if e := a.applySites(rows); e == nil {
		t.Fatal("invalid candidate accepted")
	}
	b, _ := os.ReadFile(fragment)
	if !bytes.Equal(b, old) || reloads != 1 {
		t.Fatal("rollback did not restore old fragment")
	}
	b, _ = os.ReadFile(root)
	if string(b) != oldRoot {
		t.Fatal("primary Caddyfile overwritten")
	}
	failed = false
	if e := a.applySites(rows); e != nil {
		t.Fatal(e)
	}
	saved, _ := a.readSites()
	if len(saved) != 1 {
		t.Fatal("registry commit")
	}
	r := request(a.routes(), "DELETE", "/sites/"+rows[0].ID, "", nil, nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	b, _ = os.ReadFile(root)
	if string(b) != oldRoot {
		t.Fatal("delete altered primary config")
	}
	os.WriteFile(root, []byte("# no import"), 0644)
	if e := a.applySites(rows); e == nil {
		t.Fatal("applied without explicit import")
	}
}
func TestSiteValidationAndConflict(t *testing.T) {
	for _, d := range []string{"example.com { respond hacked }", "*.example.com", "127.0.0.1", "bad_host.com"} {
		s := site{Name: "test", Kind: "static", Domains: []string{d}}
		if validSite(&s) == nil {
			t.Fatalf("domain injection %q", d)
		}
	}
	for _, u := range []string{"http://localhost:80\nrespond hacked", "http://a:b@example.com:80", "http://127.0.0.1:80/path", "unix//run/docker.sock", "http://127.0.0.1:70000"} {
		s := site{Name: "test", Kind: "proxy", Domains: []string{"example.com"}, Upstream: u}
		if validSite(&s) == nil {
			t.Fatalf("upstream injection %q", u)
		}
	}
	a := testAgent(t)
	a.capture = func(time.Duration, string, ...string) (string, error) {
		return `{"apps":{"http":{"servers":{"srv0":{"routes":[{"match":[{"host":["*.example.com"]}]}]}}}}}`, nil
	}
	r := request(a.routes(), "POST", "/sites", `{"id":"","name":"test","domains":["www.example.com"],"kind":"proxy","upstream":"http://localhost:8080"}`, nil, nil)
	if r.Code != 409 {
		t.Fatalf("existing wildcard collision %d %s", r.Code, r.Body.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeResponse(code int, s string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(s))}
}
func TestDockerNegotiationProtectedAndFrames(t *testing.T) {
	id := strings.Repeat("a", 64)
	var mutations int
	d := &dockerClient{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/version":
			return fakeResponse(200, `{"ApiVersion":"1.60","MinAPIVersion":"1.44"}`), nil
		case "/v1.56/containers/json":
			return fakeResponse(200, `[{"Id":"`+id+`","Names":["/critical"],"State":"running","Labels":{"junpanel.protected":"true"}}]`), nil
		default:
			mutations++
			return fakeResponse(204, ""), nil
		}
	})}}
	if e := d.action(id, "stop"); e == nil || mutations != 0 {
		t.Fatal("protected container mutated")
	}
	if d.API != "/v1.56" {
		t.Fatal("API negotiation")
	}
	a := testAgent(t)
	a.docker = d
	d.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1.56/containers/json" {
			return fakeResponse(200, `[{"Id":"`+id+`","State":"running","Labels":{"junpanel.protected":"true","com.docker.compose.project":"junpanel-protected-app"}}]`), nil
		}
		mutations++
		return fakeResponse(204, ""), nil
	})
	if e := a.storeApps([]appRecord{{ID: "protected-app", Template: "redis"}}); e != nil {
		t.Fatal(e)
	}
	r := request(a.routes(), "POST", "/apps/protected-app/action", `{"action":"remove"}`, nil, nil)
	if r.Code != 403 || len(a.jobs.list()) != 0 {
		t.Fatal("app bypassed protected container")
	}
	d.API = ""
	d.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return fakeResponse(200, `{"ApiVersion":"1.70","MinAPIVersion":"1.60"}`), nil
	})
	if _, e := d.list(); e == nil {
		t.Fatal("unsupported engine API accepted")
	}
	frame := []byte{1, 0, 0, 0, 0, 0, 0, 5}
	frame = append(frame, []byte("hello")...)
	if s, e := decodeDockerLogs(frame, false); e != nil || s != "hello" {
		t.Fatal("demux")
	}
	if _, e := decodeDockerLogs(frame[:10], false); e == nil {
		t.Fatal("truncated Docker frame accepted")
	}
}

func TestDatabaseExportPreservesStdout(t *testing.T) {
	frame := func(channel byte, text string) []byte {
		out := make([]byte, 8)
		out[0] = channel
		binary.BigEndian.PutUint32(out[4:8], uint32(len(text)))
		return append(out, []byte(text)...)
	}
	sql := "INSERT INTO t VALUES ('\r\x1b literal');\n"
	raw := append(frame(1, sql), frame(2, "warning: native tool notice\n")...)
	stdout, stderr, e := decodeExecOutput(raw)
	if e != nil || stdout != sql || !strings.Contains(stderr, "warning") {
		t.Fatal("export altered data or combined stderr")
	}
	if _, _, e = decodeExecOutput(raw[:len(raw)-2]); e == nil {
		t.Fatal("truncated export accepted")
	}
}
func TestTemplatesAndDatabaseArguments(t *testing.T) {
	r := appRecord{ID: "test-app", Name: "test", Password: "SuperSecret12345", DBPassword: "DbSecret12345", Port: 18080}
	for _, template := range appCatalog {
		r.Template = template.Key
		b, e := composeFor(r)
		if e != nil {
			t.Fatal(e)
		}
		var cfg map[string]any
		if json.Unmarshal(b, &cfg) != nil {
			t.Fatal("invalid compose JSON")
		}
		// A listen address inside the container (linkding) is fine; published ports are checked below.
		if strings.Contains(string(b), "0.0.0.0:") || strings.Contains(string(b), "docker.sock") || strings.Contains(string(b), "privileged") {
			t.Fatal("overbroad template")
		}
		services := cfg["services"].(map[string]any)
		for _, v := range services {
			service := v.(map[string]any)
			if ports, ok := service["ports"].([]any); ok {
				for _, p := range ports {
					if !strings.HasPrefix(p.(string), "127.0.0.1:") {
						t.Fatal("public app port")
					}
				}
			}
		}
	}
	visible, _ := json.Marshal(publicApps([]appRecord{r}))
	if strings.Contains(string(visible), r.Password) || strings.Contains(string(visible), r.DBPassword) {
		t.Fatal("secret exposed")
	}
	query := "SELECT ';$(touch /tmp/never)';"
	args, env, e := databaseArgs(r, "postgres", "junpanel", query)
	if e != nil || args[len(args)-1] != query || !strings.Contains(env[0], r.Password) {
		t.Fatal("SQL argument handling")
	}
	if _, _, e = databaseArgs(r, "postgres", "bad;drop", query); e == nil {
		t.Fatal("database identifier injection")
	}
}
func TestJobsLiveOutputAndConcurrency(t *testing.T) {
	q := newQueue()
	release := make(chan struct{})
	ready := make(chan struct{}, 2)
	fn := func(_ context.Context, out *limitedBuffer) error {
		fmt.Fprintln(out, "live line")
		ready <- struct{}{}
		<-release
		return nil
	}
	j, e := q.start("one", fn)
	if e != nil {
		t.Fatal(e)
	}
	<-ready
	if _, e = q.start("two", fn); e != nil {
		t.Fatal(e)
	}
	<-ready
	if _, e = q.start("three", fn); e == nil {
		t.Fatal("unbounded job concurrency")
	}
	found := false
	for _, v := range q.list() {
		if v.ID == j.ID && strings.Contains(v.Output, "live line") {
			found = true
		}
	}
	if !found {
		t.Fatal("job output unavailable while running")
	}
	close(release)
}
func TestRedactionAcrossWrites(t *testing.T) {
	b := &limitedBuffer{Max: 100000}
	r := &redactor{out: b, secrets: []string{"secret-value"}}
	r.Write([]byte("download secret-"))
	r.Write([]byte("value complete\n"))
	r.flush()
	if strings.Contains(b.String(), "secret-value") || !strings.Contains(b.String(), "密钥已隐藏") {
		t.Fatal("split secret leaked")
	}
}
func TestRecoveryOfInterruptedApp(t *testing.T) {
	a := testAgent(t)
	if e := a.storeApps([]appRecord{{ID: "sample", Busy: true, Password: "secret"}}); e != nil {
		t.Fatal(e)
	}
	next, e := newAgent(a.cfg)
	if e != nil {
		t.Fatal(e)
	}
	rows, e := next.readApps()
	if e != nil || rows[0].Busy || rows[0].LastError == "" {
		t.Fatal("busy app locked after restart")
	}
}
func TestMetricsCounterReset(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "net"), 0755)
	write := func(name, text string) {
		if e := os.WriteFile(filepath.Join(root, name), []byte(text), 0644); e != nil {
			t.Fatal(e)
		}
	}
	write("stat", "cpu 10 0 10 80 0 0 0 0\n")
	write("meminfo", "MemTotal: 1000 kB\nMemAvailable: 400 kB\nSwapTotal: 2000 kB\nSwapFree: 1800 kB\n")
	write("net/dev", "ens3: 1000 0 0 0 0 0 0 0 500 0\nveth0: 999999 0 0 0 0 0 0 0 99999 0\n")
	now := time.Now()
	h := &hostSampler{Proc: root, Clock: func() time.Time { return now }}
	_, e := h.read()
	if e != nil {
		t.Fatal(e)
	}
	now = now.Add(5 * time.Second)
	write("stat", "cpu 20 0 20 160 0 0 0 0\n")
	write("net/dev", "ens3: 10 0 0 0 0 0 0 0 5 0\n")
	info, e := h.read()
	if e != nil || info.CPU == nil || math.Abs(*info.CPU-20) > 0.001 || *info.RX != 0 || len(info.Interfaces) != 1 {
		t.Fatalf("metrics reset handling: %#v %v", info, e)
	}
}

// A real Unix transport test runs on Linux; Windows has no target daemon socket.
func TestHTTPConcurrentSessionReads(t *testing.T) {
	a, pw := testAccount(t)
	token, _, _ := a.login("127.0.0.1", "admin", pw, "")
	var done atomic.Int32
	for i := 0; i < 8; i++ {
		go func() { a.info(); a.get(token, false); done.Add(1) }()
	}
	deadline := time.Now().Add(time.Second)
	for done.Load() != 8 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if done.Load() != 8 {
		t.Fatal("session access blocked")
	}
}

func TestWebAgentUnixIntegration(t *testing.T) {
	a := testAgent(t)
	socket := filepath.Join(t.TempDir(), "a.sock")
	listener, e := net.Listen("unix", socket)
	if e != nil {
		t.Skipf("Unix socket unavailable on this host: %v", e)
	}
	server := &http.Server{Handler: a.routes()}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close(); os.Remove(socket) })
	auth, pw := testAccount(t)
	web, e := newWeb(auth.Path, socket, 8787, false)
	if e != nil {
		t.Fatal(e)
	}
	token, session, e := web.auth.login("127.0.0.1", "admin", pw, "")
	if e != nil {
		t.Fatal(e)
	}
	headers := map[string]string{"Origin": "http://127.0.0.1:8787", "X-CSRF-Token": session.CSRF}
	cookie := &http.Cookie{Name: "junpanel_session", Value: token}
	r := request(web.routes(), "POST", "/api/files/save", `{"path":"/integration.txt","text":"hello from web","revision":""}`, headers, cookie)
	if r.Code != 200 {
		t.Fatalf("web -> Unix agent -> file: %d %s", r.Code, r.Body.String())
	}
	r = request(web.routes(), "GET", "/api/files/content?path=/integration.txt", "", nil, cookie)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "hello from web") {
		t.Fatal("read through Unix proxy")
	}
	if e = a.storeApps([]appRecord{{ID: "db-test", Template: "postgres", Password: "private-db-secret", Port: 15432}}); e != nil {
		t.Fatal(e)
	}
	r = request(web.routes(), "GET", "/api/apps/db-test/credentials", "", nil, cookie)
	if r.Code != 404 || strings.Contains(r.Body.String(), "private-db-secret") {
		t.Fatal("direct credential GET exposed secret")
	}
	r = request(web.routes(), "POST", "/api/credentials", `{"id":"db-test","password":"wrong","otp":""}`, headers, cookie)
	if r.Code != 403 {
		t.Fatal("credentials without reauthentication")
	}
	r = request(web.routes(), "POST", "/api/credentials", `{"id":"db-test","password":"`+pw+`","otp":""}`, headers, cookie)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "private-db-secret") {
		t.Fatalf("reauthenticated credentials %d %s", r.Code, r.Body.String())
	}
	// Credentials must not travel from the public-facing request to the privileged agent.
	server.Close()
	listener, e = net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	probe := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("X-CSRF-Token") != "" {
			t.Error("browser credentials forwarded to agent")
		}
		send(w, 200, map[string]bool{"ok": true})
	})}
	go probe.Serve(listener)
	t.Cleanup(func() { probe.Close() })
	web.proxy.Transport.(*http.Transport).CloseIdleConnections()
	headers["Authorization"] = "Bearer should-not-forward"
	if r = request(web.routes(), "POST", "/api/probe", `{}`, headers, cookie); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
}
