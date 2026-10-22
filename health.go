package main

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A site is "running" when the local Caddy answers for its host name. The check connects to
// this machine only (port 443, then 80) and names the site in the Host header and the TLS
// server name; it never goes out to the internet, so it also works behind a relay.

type siteHealth struct {
	State  string `json:"state"` // up, error, down, tls, unknown
	Code   int    `json:"code,omitempty"`
	Scheme string `json:"scheme,omitempty"`
}

const (
	healthTimeout  = 3 * time.Second
	healthMaxHosts = 60
	healthTTL      = 10 * time.Second
)

type healthCache struct {
	mu   sync.Mutex
	at   time.Time
	data map[string]siteHealth
}

// probeHost asks the local Caddy for "/" under one host name.
func (a *agent) probeHost(ctx context.Context, host string) siteHealth {
	if strings.ContainsAny(host, "*:/ ") || host == "" {
		return siteHealth{State: "unknown"}
	}
	try := func(scheme, addr string) (int, error) {
		client := &http.Client{
			Timeout: healthTimeout,
			Transport: &http.Transport{
				DisableKeepAlives: true,
				Proxy:             nil,
				DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
					return (&net.Dialer{Timeout: healthTimeout}).DialContext(ctx, network, addr)
				},
				// Only this machine is contacted, so the certificate (possibly not issued yet)
				// is not judged here.
				TLSClientConfig: &tls.Config{ServerName: host, InsecureSkipVerify: true},
			},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		req, e := http.NewRequestWithContext(ctx, "GET", scheme+"://"+host+"/", nil)
		if e != nil {
			return 0, e
		}
		req.Header.Set("User-Agent", "JunPanel-health")
		resp, e := client.Do(req)
		if e != nil {
			return 0, e
		}
		resp.Body.Close()
		return resp.StatusCode, nil
	}
	result := func(scheme string, code int) siteHealth {
		if code >= 500 {
			return siteHealth{State: "error", Code: code, Scheme: scheme}
		}
		return siteHealth{State: "up", Code: code, Scheme: scheme}
	}
	code, e := try("https", a.healthHTTPS)
	if e == nil {
		return result("https", code)
	}
	msg := strings.ToLower(e.Error())
	tlsProblem := !strings.Contains(msg, "connection refused") && (strings.Contains(msg, "tls") || strings.Contains(msg, "handshake") || strings.Contains(msg, "certificate") || strings.Contains(msg, "http response to https"))
	// A host served over plain HTTP only (an http:// address) answers on port 80.
	if code, e2 := try("http", a.healthHTTP); e2 == nil {
		return result("http", code)
	}
	if tlsProblem {
		return siteHealth{State: "tls"}
	}
	return siteHealth{State: "down"}
}

// siteHealthAll checks every site the panel knows about; results are reused for a few
// seconds so a page that refreshes does not hit the sites each time.
func (a *agent) siteHealthAll(ctx context.Context, hosts []string) map[string]siteHealth {
	a.health.mu.Lock()
	if a.health.data != nil && a.now().Sub(a.health.at) < healthTTL {
		cached := a.health.data
		a.health.mu.Unlock()
		out := map[string]siteHealth{}
		for _, h := range hosts {
			if v, ok := cached[h]; ok {
				out[h] = v
			}
		}
		if len(out) == len(hosts) {
			return out
		}
	} else {
		a.health.mu.Unlock()
	}
	sort.Strings(hosts)
	if len(hosts) > healthMaxHosts {
		hosts = hosts[:healthMaxHosts]
	}
	out := map[string]siteHealth{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for _, h := range hosts {
		wg.Add(1)
		slots <- struct{}{}
		go func(host string) {
			defer wg.Done()
			defer func() { <-slots }()
			v := a.probeHost(ctx, host)
			mu.Lock()
			out[host] = v
			mu.Unlock()
		}(h)
	}
	wg.Wait()
	a.health.mu.Lock()
	a.health.at, a.health.data = a.now(), out
	a.health.mu.Unlock()
	return out
}

func (a *agent) siteHealthStatus(w http.ResponseWriter, r *http.Request) error {
	a.mu.Lock()
	sites, e := a.readSites()
	hosts, _, _ := a.caddyInventory()
	external := a.externalHosts(sites, hosts)
	a.mu.Unlock()
	if e != nil {
		return e
	}
	seen := map[string]bool{}
	list := []string{}
	add := func(h string) {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" && !seen[h] {
			seen[h] = true
			list = append(list, h)
		}
	}
	for _, s := range sites {
		if s.Enabled {
			for _, d := range s.Domains {
				add(d)
			}
		}
	}
	for _, h := range external {
		add(h)
	}
	send(w, 200, map[string]any{"hosts": a.siteHealthAll(r.Context(), list), "checked": a.now()})
	return nil
}

// monitoredHosts are the sites whose state is watched: the panel's own sites that are on,
// and the sites in the main Caddyfile. Sites that only another Caddy instance serves are
// left out, because they may listen on ports this check does not look at.
func (a *agent) monitoredHosts() ([]string, bool) {
	a.mu.Lock()
	sites, e := a.readSites()
	a.mu.Unlock()
	if e != nil {
		return nil, false
	}
	// The Caddy host list comes from the shared, periodically refreshed inventory.
	a.certificates(a.now(), false)
	a.certMu.Lock()
	sources, he := a.invSources, a.invErr
	a.certMu.Unlock()
	seen := map[string]bool{}
	out := []string{}
	add := func(h string) {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" && !strings.ContainsAny(h, "*:/ ") && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	for _, s := range sites {
		if s.Enabled {
			for _, d := range s.Domains {
				add(d)
			}
		}
	}
	for _, src := range sources {
		if src.Primary && src.Error == "" {
			for _, h := range src.Hosts {
				add(h)
			}
		}
	}
	// Without a readable inventory the hosts from the main Caddyfile are unknown: keep
	// whatever was already being reported rather than calling those sites recovered.
	return out, he == nil || len(out) > 0
}

// siteFindings reports sites that do not answer. A site counts as down only after several
// checks in a row (the rule's Need), so a reload or a short glitch does not raise an alert.
func (a *agent) siteFindings(st *alertState) []finding {
	hosts, ok := a.monitoredHosts()
	if !ok {
		return a.keepSiteAlerts(st)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Always probe afresh: the alert's own timing, not the page cache, decides.
	a.health.mu.Lock()
	a.health.data = nil
	a.health.mu.Unlock()
	states := a.siteHealthAll(ctx, hosts)
	var out []finding
	for _, h := range hosts {
		v := states[h]
		var title, detail, severity string
		switch v.State {
		case "down":
			title, detail, severity = "网站无法访问："+h, "本机的 Caddy 没有应答这个域名，请检查 Caddy 和网站配置。", "critical"
		case "error":
			title, detail, severity = "网站后端异常："+h, "Caddy 在运行，但网站后端返回了 "+strconv.Itoa(v.Code)+"，请检查对应的应用或服务。", "critical"
		case "tls":
			title, detail, severity = "网站证书未就绪："+h, "HTTPS 握手失败，证书可能还没签发好或已失效。", "warning"
		default:
			continue
		}
		out = append(out, finding{Key: "site:" + h, Title: title, Detail: detail, Severity: severity, Need: siteFailPolls})
	}
	return out
}

// keepSiteAlerts carries the current site alerts over when the host list could not be read.
func (a *agent) keepSiteAlerts(st *alertState) []finding {
	var out []finding
	for key, v := range st.Active {
		if strings.HasPrefix(key, "site:") {
			out = append(out, finding{Key: key, Title: v.Title, Detail: v.Detail, Severity: v.Severity, Need: 1})
		}
	}
	return out
}
