package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// siteAnswers stands in for the local Caddy: it answers by Host header.
func siteAnswers() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Host {
		case "ok.example.com":
			w.WriteHeader(200)
		case "redirect.example.com":
			http.Redirect(w, r, "https://elsewhere.example.com/", 308)
		case "auth.example.com":
			w.WriteHeader(401)
		case "bad.example.com":
			w.WriteHeader(502)
		default:
			w.WriteHeader(404)
		}
	})
}

func closedAddr(t *testing.T) string {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func TestProbeHostStates(t *testing.T) {
	a := testAgent(t)
	secure := httptest.NewTLSServer(siteAnswers())
	defer secure.Close()
	plain := httptest.NewServer(siteAnswers())
	defer plain.Close()
	notTLS := httptest.NewServer(siteAnswers()) // answers plain HTTP on the "https" port
	defer notTLS.Close()
	hostPort := func(s *httptest.Server) string {
		return strings.TrimPrefix(strings.TrimPrefix(s.URL, "https://"), "http://")
	}
	ctx := context.Background()

	// A host that Caddy answers, whatever the status below 500: it is running.
	a.healthHTTPS, a.healthHTTP = hostPort(secure), closedAddr(t)
	for host, want := range map[string]siteHealth{
		"ok.example.com":       {State: "up", Code: 200, Scheme: "https"},
		"redirect.example.com": {State: "up", Code: 308, Scheme: "https"},
		"auth.example.com":     {State: "up", Code: 401, Scheme: "https"},
		"other.example.com":    {State: "up", Code: 404, Scheme: "https"},
		"bad.example.com":      {State: "error", Code: 502, Scheme: "https"},
		"*.example.com":        {State: "unknown"},
		"a b":                  {State: "unknown"},
	} {
		if got := a.probeHost(ctx, host); got != want {
			t.Fatalf("%s: %+v, want %+v", host, got, want)
		}
	}
	// A host served only over plain HTTP.
	a.healthHTTPS, a.healthHTTP = closedAddr(t), hostPort(plain)
	if got := a.probeHost(ctx, "ok.example.com"); got != (siteHealth{State: "up", Code: 200, Scheme: "http"}) {
		t.Fatalf("http only: %+v", got)
	}
	// Nothing listening.
	a.healthHTTPS, a.healthHTTP = closedAddr(t), closedAddr(t)
	if got := a.probeHost(ctx, "ok.example.com"); got.State != "down" {
		t.Fatalf("down: %+v", got)
	}
	// Port 443 speaks but the TLS handshake fails (no certificate yet) and nothing is on 80.
	a.healthHTTPS, a.healthHTTP = hostPort(notTLS), closedAddr(t)
	if got := a.probeHost(ctx, "ok.example.com"); got.State != "tls" {
		t.Fatalf("tls: %+v", got)
	}
	// The probe sends the site's own name as the server name.
	var sni string
	tlsServer := httptest.NewUnstartedServer(siteAnswers())
	tlsServer.TLS = &tls.Config{GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) { sni = h.ServerName; return nil, nil }}
	tlsServer.StartTLS()
	defer tlsServer.Close()
	a.healthHTTPS = hostPort(tlsServer)
	a.probeHost(ctx, "ok.example.com")
	if sni != "ok.example.com" {
		t.Fatalf("server name %q", sni)
	}
}

func TestSiteHealthEndpointCoversManagedAndExistingSites(t *testing.T) {
	a := testAgent(t)
	secure := httptest.NewTLSServer(siteAnswers())
	defer secure.Close()
	a.healthHTTPS, a.healthHTTP = strings.TrimPrefix(secure.URL, "https://"), closedAddr(t)
	os.WriteFile(a.cfg.Caddy, []byte("import "+filepath.ToSlash(a.cfg.Fragments)+"/*.caddy\n"), 0644)
	a.processConfigs = func() ([]caddyConfig, error) { return nil, nil }
	a.capture = func(time.Duration, string, ...string) (string, error) {
		return `{"apps":{"http":{"servers":{"srv":{"routes":[{"handle":[{"handler":"subroute","routes":[{"match":[{"host":["ok.example.com","bad.example.com"]}]}]}]}]}}}}}`, nil
	}
	rows := []site{
		{ID: "s_" + strings.Repeat("a", 20), Name: "On", Kind: "proxy", Domains: []string{"auth.example.com"}, Upstream: "http://127.0.0.1:8000", Enabled: true, Created: time.Now()},
		{ID: "s_" + strings.Repeat("b", 20), Name: "Off", Kind: "proxy", Domains: []string{"disabled.example.com"}, Upstream: "http://127.0.0.1:8001", Enabled: false, Created: time.Now()},
	}
	if e := atomicJSON(filepath.Join(a.cfg.State, "sites.json"), rows, 0600); e != nil {
		t.Fatal(e)
	}
	r := request(a.routes(), "GET", "/sites/health", "", nil, nil)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	var got struct {
		Hosts map[string]siteHealth `json:"hosts"`
	}
	json.Unmarshal(r.Body.Bytes(), &got)
	if got.Hosts["ok.example.com"].State != "up" || got.Hosts["bad.example.com"].State != "error" || got.Hosts["auth.example.com"].State != "up" {
		t.Fatalf("%+v", got.Hosts)
	}
	if _, ok := got.Hosts["disabled.example.com"]; ok {
		t.Fatal("a stopped site was checked")
	}
	// A second request within the cache window does not touch the sites again.
	secure.Close()
	if r := request(a.routes(), "GET", "/sites/health", "", nil, nil); r.Code != 200 || !strings.Contains(r.Body.String(), `"ok.example.com":{"state":"up"`) {
		t.Fatalf("cached: %d %s", r.Code, r.Body.String())
	}
}
