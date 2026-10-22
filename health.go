package main

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sort"
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
