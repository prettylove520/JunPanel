package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSiteDNSStates(t *testing.T) {
	a := testAgent(t)
	os.WriteFile(a.cfg.Caddy, []byte("import "+filepath.ToSlash(a.cfg.Fragments)+"/*.caddy\n"), 0644)
	os.WriteFile(filepath.Join(a.cfg.Fragments, "panel-access.caddy"), []byte("# JunPanel public HTTPS access - managed by public-access.sh\nhttps://8.8.4.4 {\n respond \"x\"\n}\n"), 0644)
	a.processConfigs = func() ([]caddyConfig, error) { return nil, nil }
	a.capture = func(time.Duration, string, ...string) (string, error) { return `{"apps":{}}`, nil }
	names := []string{"here.example.com", "relay.example.com", "gone.example.com", "slow.example.com", "empty.example.com"}
	var rows []site
	for i, n := range names {
		rows = append(rows, site{ID: "s_" + strings.Repeat(string(rune('a'+i)), 20), Name: n, Kind: "proxy", Domains: []string{n}, Upstream: "http://127.0.0.1:8000", Enabled: true, Created: time.Now()})
	}
	if e := atomicJSON(filepath.Join(a.cfg.State, "sites.json"), rows, 0600); e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	calls := 0
	a.lookup = func(_ context.Context, host string) ([]string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		switch host {
		case "here.example.com":
			return []string{"8.8.4.4"}, nil
		case "relay.example.com":
			return []string{"2606:4700::1", "1.1.1.1"}, nil
		case "gone.example.com":
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		case "slow.example.com":
			return nil, errors.New("i/o timeout")
		}
		return nil, nil
	}
	get := func() map[string]dnsInfo {
		r := request(a.routes(), "GET", "/sites/dns", "", nil, nil)
		if r.Code != 200 {
			t.Fatalf("%d %s", r.Code, r.Body.String())
		}
		var v struct{ Hosts map[string]dnsInfo }
		json.Unmarshal(r.Body.Bytes(), &v)
		return v.Hosts
	}
	got := get()
	if got["here.example.com"].State != "local" || got["relay.example.com"].State != "other" || got["gone.example.com"].State != "none" || got["slow.example.com"].State != "error" || got["empty.example.com"].State != "none" {
		t.Fatalf("%+v", got)
	}
	if ips := got["relay.example.com"].IPs; len(ips) != 2 || ips[0] != "1.1.1.1" {
		t.Fatalf("IPv4 should come first: %v", ips)
	}
	// Answers are reused for a minute and looked up again after that.
	before := calls
	get()
	if calls != before {
		t.Fatal("looked up again within the cache period")
	}
	a.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	get()
	if calls == before {
		t.Fatal("cache never expires")
	}
}
