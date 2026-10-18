package main

import (
	"crypto/hmac"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	return &webServer{auth: a, port: port, demo: demo, proxy: proxy, tickets: map[string]terminalTicket{}, slots: make(chan struct{}, 24), logins: make(chan struct{}, 2)}, nil
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
		if file != "index.html" && file != "app.js" && file != "style.css" && file != "junpanel.css" && file != "logo.svg" && file != "favicon.svg" && file != "logo-preview.svg" {
			http.NotFound(out, r)
			return
		}
		data, e := fs.ReadFile(w.assets, file)
		if e != nil {
			http.NotFound(out, r)
			return
		}
		out.Header().Set("Content-Type", mime.TypeByExtension(file[strings.LastIndex(file, "."):]))
		out.Write(data)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/api/meta" {
		send(out, 200, map[string]any{"demo": w.demo, "version": version})
		return
	}
	r.Body = http.MaxBytesReader(out, r.Body, 12<<20)
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
			fail(out, e)
			return
		}
		http.SetCookie(out, &http.Cookie{Name: "junpanel_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		log.Printf("login result=success ip=%s", ip)
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
			e = w.auth.change(req.Password, req.NewPassword, req.OTP)
		case "totp_setup":
			secret, e = w.auth.setupOTP(token, req.Password)
		case "totp_enable":
			e = w.auth.enableOTP(token, req.OTP)
		case "totp_disable":
			e = w.auth.disableOTP(req.Password, req.OTP)
		default:
			e = bad(400, "未知安全操作")
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
