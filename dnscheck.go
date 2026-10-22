package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Where a site's domain points, next to whether it runs: a domain that resolves to a
// different machine is normal behind a relay or CDN, but it is the first thing to look at
// when a certificate cannot be issued.

type dnsInfo struct {
	State string   `json:"state"` // local, other, none, error
	IPs   []string `json:"ips,omitempty"`
}

const (
	dnsTimeout = 3 * time.Second
	dnsTTL     = 60 * time.Second
	dnsMax     = 60
)

type dnsCache struct {
	mu   sync.Mutex
	at   time.Time
	data map[string]dnsInfo
}

func lookupAddrs(ctx context.Context, host string) ([]string, error) {
	addrs, e := net.DefaultResolver.LookupIPAddr(ctx, host)
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP.String())
	}
	return out, e
}

// localAddrs are the addresses of this machine: its interfaces and the IP entries the panel
// was set up with (a public address may sit on the provider's NAT, not on an interface).
func (a *agent) localAddrs() map[string]bool {
	out := map[string]bool{}
	if addrs, e := net.InterfaceAddrs(); e == nil {
		for _, x := range addrs {
			if ipnet, ok := x.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				out[ipnet.IP.String()] = true
			}
		}
	}
	if entries, _ := a.accessEntries(); entries != nil {
		for _, e := range entries {
			if e.Kind == "ip" {
				out[e.Address] = true
			}
		}
	}
	return out
}

func (a *agent) resolveHost(ctx context.Context, host string, local map[string]bool) dnsInfo {
	ctx, cancel := context.WithTimeout(ctx, dnsTimeout)
	defer cancel()
	lookup := a.lookup
	if lookup == nil {
		lookup = lookupAddrs
	}
	ips, e := lookup(ctx, host)
	if len(ips) == 0 {
		var dns *net.DNSError
		if errors.As(e, &dns) && dns.IsNotFound {
			return dnsInfo{State: "none"}
		}
		if e == nil {
			return dnsInfo{State: "none"}
		}
		return dnsInfo{State: "error"}
	}
	// IPv4 first: it is what people compare against.
	sort.SliceStable(ips, func(i, j int) bool { return !strings.Contains(ips[i], ":") && strings.Contains(ips[j], ":") })
	if len(ips) > 4 {
		ips = ips[:4]
	}
	for _, ip := range ips {
		if local[ip] {
			return dnsInfo{State: "local", IPs: ips}
		}
	}
	return dnsInfo{State: "other", IPs: ips}
}

func (a *agent) siteDNSStatus(w http.ResponseWriter, r *http.Request) error {
	hosts, ok := a.monitoredHosts()
	if !ok {
		return bad(502, "无法读取网站列表")
	}
	if len(hosts) > dnsMax {
		hosts = hosts[:dnsMax]
	}
	a.dns.mu.Lock()
	if a.dns.data != nil && a.now().Sub(a.dns.at) < dnsTTL {
		all := true
		for _, h := range hosts {
			if _, ok := a.dns.data[h]; !ok {
				all = false
			}
		}
		if all {
			cached := a.dns.data
			a.dns.mu.Unlock()
			send(w, 200, map[string]any{"hosts": cached})
			return nil
		}
	}
	a.dns.mu.Unlock()
	local := a.localAddrs()
	out := map[string]dnsInfo{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for _, h := range hosts {
		wg.Add(1)
		slots <- struct{}{}
		go func(host string) {
			defer wg.Done()
			defer func() { <-slots }()
			v := a.resolveHost(r.Context(), host, local)
			mu.Lock()
			out[host] = v
			mu.Unlock()
		}(h)
	}
	wg.Wait()
	a.dns.mu.Lock()
	a.dns.at, a.dns.data = a.now(), out
	a.dns.mu.Unlock()
	send(w, 200, map[string]any{"hosts": out})
	return nil
}
