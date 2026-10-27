package main

import (
	"bytes"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type terminalTicket struct {
	Session, Container string
	Expires            time.Time
}
type webServer struct {
	auth    *authStore
	port    int
	demo    bool
	assets  fs.FS
	proxy   *httputil.ReverseProxy
	tickets map[string]terminalTicket
	mu      sync.Mutex
	slots   chan struct{}
	logins  chan struct{}
	audit   *auditLog
}

func newWeb(accountFile, socket string, port int, demo bool) (*webServer, error) {
	a, e := newAuth(accountFile, demo)
	if e != nil {
		return nil, e
	}
	target, _ := url.Parse("http://agent")
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = unixTransport(socket)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		send(w, 502, map[string]any{"error": "本机管理代理未连接，请检查 junpanel-agent 服务"})
	}
	w := &webServer{auth: a, port: port, demo: demo, proxy: proxy, tickets: map[string]terminalTicket{}, slots: make(chan struct{}, 24), logins: make(chan struct{}, 2)}
	if !demo && accountFile != "" {
		w.audit = newAuditLog(filepath.Join(filepath.Dir(accountFile), "audit.log"))
	}
	return w, nil
}
func (w *webServer) cookie(r *http.Request) string {
	c, e := r.Cookie("junpanel_session")
	if e != nil {
		return ""
	}
	return c.Value
}

// clientIP is the peer address, or the address appended by a reverse proxy on
// this host (Caddy replaces untrusted X-Forwarded-For values with the peer).
func clientIP(r *http.Request) string {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !peer.IsLoopback() {
		return host
	}
	forwarded := r.Header.Values("X-Forwarded-For")
	if len(forwarded) == 0 {
		return host
	}
	parts := strings.Split(forwarded[len(forwarded)-1], ",")
	if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
		return ip.String()
	}
	return host
}
func (w *webServer) routes() http.Handler { return http.HandlerFunc(w.serve) }
func (w *webServer) serve(out http.ResponseWriter, r *http.Request) {
	defer func() {
		if recover() != nil {
			send(out, 500, map[string]any{"error": "请求处理失败"})
		}
	}()
	select {
	case w.slots <- struct{}{}:
		defer func() { <-w.slots }()
	default:
		send(out, 503, map[string]any{"error": "请求较多，请稍后重试"})
		return
	}
	out.Header().Set("X-Content-Type-Options", "nosniff")
	out.Header().Set("X-Frame-Options", "DENY")
	out.Header().Set("Referrer-Policy", "no-referrer")
	out.Header().Set("Cache-Control", "no-store")
	// An open page compares this with the version it was loaded with and reloads after an upgrade.
	out.Header().Set("X-JunPanel-Version", version)
	out.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	expected := fmt.Sprintf("127.0.0.1:%d", w.port)
	local := fmt.Sprintf("localhost:%d", w.port)
	if r.Host != expected && r.Host != local {
		fail(out, bad(403, "仅接受本机域名与面板端口"))
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
		fail(out, bad(403, "拒绝跨站请求"))
		return
	}
	unsafe := r.Method != "GET" && r.Method != "HEAD"
	if unsafe || strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		if r.Header.Get("Origin") != "http://"+r.Host {
			fail(out, bad(403, "来源校验未通过"))
			return
		}
	}
	if r.Method == "GET" && !strings.HasPrefix(r.URL.Path, "/api/") {
		file := strings.TrimPrefix(r.URL.Path, "/")
		if file == "" {
			file = "index.html"
		}
		font, icon := fontFile.MatchString(file), iconFile.MatchString(file)
		if !font && !icon && file != "index.html" && file != "app.js" && file != "style.css" && file != "logo.svg" && file != "favicon.svg" && file != "logo-preview.svg" {
			http.NotFound(out, r)
			return
		}
		data, e := fs.ReadFile(w.assets, file)
		if e != nil {
			http.NotFound(out, r)
			return
		}
		if file == "index.html" {
			data = versionedIndex(data)
		}
		out.Header().Set("Content-Type", mime.TypeByExtension(file[strings.LastIndex(file, "."):]))
		if font || icon { // fonts and app icons never change under the same name; everything else stays no-store
			out.Header().Set("Cache-Control", "public, max-age=86400")
		}
		out.Write(data)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/api/meta" {
		send(out, 200, map[string]any{"demo": w.demo, "version": version})
		return
	}
	r.Body = http.MaxBytesReader(out, r.Body, bodyLimit(r.Method, r.URL.Path))
	if r.Method == "POST" && r.URL.Path == "/api/files/upload" {
		http.NewResponseController(out).SetReadDeadline(time.Now().Add(30 * time.Minute))
	}
	if r.URL.Path == "/api/login" && r.Method == "POST" {
		select {
		case w.logins <- struct{}{}:
			defer func() { <-w.logins }()
		default:
			fail(out, bad(429, "登录验证繁忙，请稍后重试"))
			return
		}
		var req struct {
			User     string `json:"user"`
			Password string `json:"password"`
			OTP      string `json:"otp"`
		}
		if e := body(r, &req); e != nil {
			fail(out, e)
			return
		}
		ip := clientIP(r)
		token, session, e := w.auth.login(ip, req.User, req.Password, req.OTP)
		if e != nil {
			result := "failed"
			if f, ok := e.(*fault); ok && f.Code == 429 {
				result = "throttled"
			}
			log.Printf("login result=%s ip=%s", result, ip)
			if result == "throttled" {
				w.auditLogin(r, "登录被限速", "failed")
			} else {
				w.auditLogin(r, "登录失败", "failed")
			}
			fail(out, e)
			return
		}
		http.SetCookie(out, &http.Cookie{Name: "junpanel_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		log.Printf("login result=success ip=%s", ip)
		w.auditLogin(r, "登录成功", "ok")
		user, totp := w.auth.info()
		send(out, 200, map[string]any{"csrf": session.CSRF, "user": user, "demo": w.demo, "totp": totp})
		return
	}
	token := w.cookie(r)
	session, e := w.auth.get(token, false)
	if e != nil {
		fail(out, e)
		return
	}
	if unsafe && !hmac.Equal([]byte(session.CSRF), []byte(r.Header.Get("X-CSRF-Token"))) {
		fail(out, bad(403, "CSRF 校验失败，请重新载入"))
		return
	}
	out, finish := w.auditRequest(out, r)
	defer finish()
	if r.URL.Path == "/api/audit" && r.Method == "GET" {
		kind := r.URL.Query().Get("kind")
		if kind != "operation" && kind != "login" {
			fail(out, bad(400, "日志类型无效"))
			return
		}
		if w.demo {
			send(out, 200, demoAudit(kind))
		} else {
			send(out, 200, w.audit.list(kind, 500))
		}
		return
	}
	if r.URL.Path == "/api/session" && r.Method == "GET" {
		user, totp := w.auth.info()
		send(out, 200, map[string]any{"csrf": session.CSRF, "user": user, "demo": w.demo, "totp": totp})
		return
	}
	if r.URL.Path == "/api/heartbeat" && r.Method == "POST" {
		w.auth.get(token, true)
		send(out, 200, map[string]any{"ok": true})
		return
	}
	if r.URL.Path == "/api/logout" && r.Method == "POST" {
		w.auditLogin(r, "退出登录", "ok")
		w.auth.logout(token)
		http.SetCookie(out, &http.Cookie{Name: "junpanel_session", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		send(out, 200, map[string]any{"ok": true})
		return
	}
	if w.demo {
		if unsafe {
			fail(out, bad(403, "演示模式不执行服务器变更"))
			return
		}
		demoAPI(out, r)
		return
	}
	if r.URL.Path == "/api/security" && r.Method == "POST" {
		var req struct {
			Action      string `json:"action"`
			Password    string `json:"password"`
			NewPassword string `json:"new_password"`
			OTP         string `json:"otp"`
		}
		if e = body(r, &req); e != nil {
			fail(out, e)
			return
		}
		var secret string
		switch req.Action {
		case "password":
			e = w.auth.change(clientIP(r), req.Password, req.NewPassword, req.OTP)
		case "totp_setup":
			secret, e = w.auth.setupOTP(clientIP(r), token, req.Password)
		case "totp_enable":
			e = w.auth.enableOTP(token, req.OTP)
		case "totp_disable":
			e = w.auth.disableOTP(clientIP(r), req.Password, req.OTP)
		default:
			e = bad(400, "未知安全操作")
		}
		if label, ok := map[string]string{"password": "修改密码", "totp_setup": "开始设置两步验证", "totp_enable": "开启两步验证", "totp_disable": "关闭两步验证"}[req.Action]; ok {
			result, code := "ok", 200
			if e != nil {
				result, code = "failed", 400
				if f, ok := e.(*fault); ok {
					code = f.Code
				}
			}
			w.audit.add(auditEntry{Kind: "operation", IP: clientIP(r), Action: label, Method: "POST", Path: "/api/security", Status: code, Result: result})
		}
		if e != nil {
			fail(out, e)
			return
		}
		send(out, 200, map[string]any{"ok": true, "secret": secret})
		return
	}
	// Only the explicit, reauthenticated POST can reach secret-bearing agent data.
	if r.URL.Path != "/api/credentials" && strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/credentials") {
		fail(out, bad(404, "接口不存在"))
		return
	}
	if r.URL.Path == "/api/credentials" && r.Method == "POST" {
		var req struct {
			ID       string `json:"id"`
			Password string `json:"password"`
			OTP      string `json:"otp"`
		}
		if e = body(r, &req); e != nil {
			fail(out, e)
			return
		}
		if e = requireName(req.ID); e != nil {
			fail(out, e)
			return
		}
		if e = w.auth.confirm(clientIP(r), req.Password, req.OTP); e != nil {
			log.Printf("reauth result=failed ip=%s", clientIP(r))
			w.auditLogin(r, "再次验证密码失败", "failed")
			fail(out, e)
			return
		}
		r.Method = "GET"
		r.URL.Path = "/api/apps/" + req.ID + "/credentials"
		r.Body = nil
		r.ContentLength = 0
		r.Header.Del("Content-Length")
		r.Header.Del("Content-Type")
	}
	// Installing a new version needs a signed-in administrator, whose session and CSRF
	// token are already checked above; it no longer asks for the password again. The
	// agent's start endpoint stays reachable only through here, so a request cannot
	// choose the version or address to install.
	if strings.HasPrefix(path.Clean(r.URL.Path), "/api/upgrade/start") {
		fail(out, bad(404, "接口不存在"))
		return
	}
	if r.URL.Path == "/api/upgrade" && r.Method == "POST" {
		log.Printf("upgrade requested ip=%s", clientIP(r))
		r.URL.Path = "/api/upgrade/start"
		r.Body = http.NoBody
		r.ContentLength = 0
		r.Header.Del("Content-Length")
		r.Header.Del("Content-Type")
	}
	// Changing the public entry can lock the browser out, so it needs the password
	// again; the agent's endpoint is reachable only through here, with no secrets.
	if strings.HasPrefix(path.Clean(r.URL.Path), "/api/access/apply") {
		fail(out, bad(404, "接口不存在"))
		return
	}
	if r.URL.Path == "/api/access" && r.Method == "POST" {
		var req struct {
			Password string `json:"password"`
			OTP      string `json:"otp"`
			Action   string `json:"action"`
			Domain   string `json:"domain"`
			IP       string `json:"ip"`
			Only     bool   `json:"only"`
			SkipDNS  bool   `json:"skip_dns"`
		}
		if e = body(r, &req); e != nil {
			fail(out, e)
			return
		}
		if e = w.auth.confirm(clientIP(r), req.Password, req.OTP); e != nil {
			log.Printf("reauth result=failed ip=%s", clientIP(r))
			w.auditLogin(r, "再次验证密码失败", "failed")
			fail(out, e)
			return
		}
		forward, _ := json.Marshal(map[string]any{"action": req.Action, "domain": req.Domain, "ip": req.IP, "only": req.Only, "skip_dns": req.SkipDNS})
		r.URL.Path = "/api/access/apply"
		r.Body = io.NopCloser(bytes.NewReader(forward))
		r.ContentLength = int64(len(forward))
		r.Header.Set("Content-Length", strconv.Itoa(len(forward)))
		r.Header.Set("Content-Type", "application/json")
	}
	if r.URL.Path == "/api/terminal/ticket" && r.Method == "POST" {
		var req struct {
			Container string `json:"container"`
		}
		if e = body(r, &req); e != nil {
			fail(out, e)
			return
		}
		if !containerID.MatchString(req.Container) {
			fail(out, bad(400, "容器 ID 无效"))
			return
		}
		w.mu.Lock()
		for key, t := range w.tickets {
			if time.Now().After(t.Expires) {
				delete(w.tickets, key)
			}
		}
		if len(w.tickets) >= 32 {
			w.mu.Unlock()
			fail(out, bad(429, "终端票据较多，请稍后重试"))
			return
		}
		ticket := randomID(64)
		w.tickets[ticket] = terminalTicket{token, req.Container, time.Now().Add(30 * time.Second)}
		w.mu.Unlock()
		send(out, 200, map[string]string{"ticket": ticket})
		return
	}
	if r.URL.Path == "/api/terminal" && r.Method == "GET" {
		key := r.URL.Query().Get("ticket")
		w.mu.Lock()
		t, ok := w.tickets[key]
		delete(w.tickets, key)
		w.mu.Unlock()
		if !ok || t.Session != token || time.Now().After(t.Expires) {
			fail(out, bad(401, "终端票据无效或已过期"))
			return
		}
		r.URL.RawQuery = "container=" + url.QueryEscape(t.Container)
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(out, r)
		return
	}
	r.Header.Del("Cookie")
	r.Header.Del("Authorization")
	r.Header.Del("X-CSRF-Token")
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
	w.proxy.ServeHTTP(out, r)
}

// fontFile matches the bundled web fonts: web/fonts/<name>.woff2 and nothing else.
var fontFile = regexp.MustCompile(`^fonts/[a-z0-9-]+\.woff2$`)

// iconFile matches the bundled app logos: web/icons/<template>.svg.
var iconFile = regexp.MustCompile(`^icons/[a-z0-9-]+\.svg$`)

func init() { mime.AddExtensionType(".woff2", "font/woff2") }

// versionedIndex records the version in the page and adds it to asset URLs, so a page always
// knows which release it came from and no cache in between can pair it with older files.
func versionedIndex(page []byte) []byte {
	v := html.EscapeString(version)
	text := strings.Replace(string(page), `<meta charset="utf-8">`, `<meta charset="utf-8">`+"\n  "+`<meta name="junpanel-version" content="`+v+`">`, 1)
	for _, asset := range []string{"/style.css", "/app.js", "/favicon.svg", "/logo.svg"} {
		text = strings.ReplaceAll(text, `"`+asset+`"`, `"`+asset+"?v="+v+`"`)
	}
	return []byte(text)
}
