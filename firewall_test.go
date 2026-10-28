package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const ssSample = `tcp   LISTEN 0      128          0.0.0.0:2222      0.0.0.0:*    users:(("sshd",pid=611,fd=3))
tcp   LISTEN 0      128             [::]:2222         [::]:*    users:(("sshd",pid=611,fd=4))
tcp   LISTEN 0      4096               *:443             *:*    users:(("caddy",pid=702,fd=7))
udp   UNCONN 0      0      127.0.0.53%lo:53        0.0.0.0:*    users:(("systemd-resolve",pid=401,fd=13))
tcp   LISTEN 0      4096       127.0.0.1:8787      0.0.0.0:*    users:(("junpanel",pid=733,fd=5))`

func TestFirewallListeningPorts(t *testing.T) {
	ports := listeningPorts(ssSample)
	if len(ports) != 5 {
		t.Fatalf("ports: %+v", ports)
	}
	if !ports[0].Public || ports[0].Port != 443 || ports[0].Process != "caddy" || ports[0].Address != "0.0.0.0" {
		t.Fatalf("first public port: %+v", ports[0])
	}
	for _, p := range ports {
		if p.Port == 53 && (p.Public || p.Address != "127.0.0.53") {
			t.Fatalf("resolver: %+v", p)
		}
		if p.Port == 8787 && p.Public {
			t.Fatalf("panel is local only: %+v", p)
		}
	}
	if got := sshPorts(ports); len(got) != 1 || got[0] != 2222 {
		t.Fatalf("ssh ports: %v", got)
	}
	if got := sshPorts(nil); len(got) != 1 || got[0] != 22 {
		t.Fatalf("ssh fallback: %v", got)
	}
}

func TestFirewallCheckRule(t *testing.T) {
	existing := []fwRule{{Kind: "block-ip", Source: "203.0.113.7"}, {Kind: "close-port", Port: 3306, Proto: "tcp"}}
	for _, r := range []fwRule{
		{Kind: "block-ip", Source: "not-an-ip"},
		{Kind: "block-ip", Source: "127.0.0.1"},
		{Kind: "block-ip", Source: "0.0.0.0/0"},
		{Kind: "block-ip", Source: "10.0.0.0/7"},
		{Kind: "block-ip", Source: "203.0.113.7"},
		{Kind: "close-port", Port: 22, Proto: "tcp"},
		{Kind: "close-port", Port: 22, Proto: "both"},
		{Kind: "close-port", Port: 0, Proto: "tcp"},
		{Kind: "close-port", Port: 3306, Proto: "both"},
		{Kind: "close-port", Port: 80, Proto: "icmp"},
		{Kind: "open-all"},
	} {
		if _, e := checkRule(r, existing, []int{22}); e == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
	r, e := checkRule(fwRule{Kind: "block-ip", Source: " 198.51.100.9/24 ", Note: " 扫描 "}, existing, []int{22})
	if e != nil || r.Source != "198.51.100.0/24" || r.Note != "扫描" {
		t.Fatalf("cidr: %+v %v", r, e)
	}
	if _, e = checkRule(fwRule{Kind: "close-port", Port: 22, Proto: "udp"}, existing, []int{22}); e != nil {
		t.Fatalf("udp 22 is not ssh: %v", e)
	}
}

func TestFirewallScript(t *testing.T) {
	empty := nftScript(nil)
	if empty != "table inet junpanel {\n}\ndelete table inet junpanel\n" {
		t.Fatalf("empty script: %q", empty)
	}
	s := nftScript([]fwRule{{Kind: "block-ip", Source: "203.0.113.7"}, {Kind: "block-ip", Source: "2001:db8::/32"}, {Kind: "close-port", Port: 3306, Proto: "tcp"}, {Kind: "close-port", Port: 8443, Proto: "both"}})
	for _, want := range []string{"priority filter - 1; policy accept;", "ip saddr { 203.0.113.7 } drop", "ip6 saddr { 2001:db8::/32 } drop", "tcp dport { 3306, 8443 } drop", "udp dport { 8443 } drop"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in\n%s", want, s)
		}
	}
	// Blocked addresses are dropped before established connections are let through, closed ports after.
	if strings.Index(s, "ip saddr") > strings.Index(s, "ct state") || strings.Index(s, "tcp dport") < strings.Index(s, "ct state") {
		t.Fatalf("rule order:\n%s", s)
	}
}

func TestFirewallEndpoints(t *testing.T) {
	a := testAgent(t)
	nft := filepath.Join(t.TempDir(), "nft")
	if e := os.WriteFile(nft, []byte("#!/bin/sh\n"), 0755); e != nil {
		t.Fatal(e)
	}
	old := nftCandidates
	nftCandidates = []string{nft}
	defer func() { nftCandidates = old }()
	applied, fail := "", false
	a.runner = func(_ time.Duration, name string, args ...string) (string, error) {
		switch {
		case name == "ss":
			return ssSample, nil
		case name == nft && len(args) == 2 && args[0] == "-f":
			if fail {
				return "Error: syntax", errors.New("exit 1")
			}
			b, _ := os.ReadFile(args[1])
			applied = string(b)
			return "", nil
		case name == nft:
			return "", nil
		}
		return "", errors.New("unexpected " + name)
	}
	h := a.routes()
	if r := request(h, "POST", "/firewall/rules", `{"kind":"close-port","port":2222,"proto":"tcp"}`, nil, nil); r.Code != 409 {
		t.Fatalf("closed ssh: %d %s", r.Code, r.Body.String())
	}
	r := request(h, "POST", "/firewall/rules", `{"kind":"block-ip","source":"203.0.113.7","note":"暴力破解"}`, nil, nil)
	if r.Code != 200 || !strings.Contains(applied, "203.0.113.7") {
		t.Fatalf("add: %d %s\n%s", r.Code, r.Body.String(), applied)
	}
	var rule fwRule
	json.Unmarshal(r.Body.Bytes(), &rule)
	fail = true
	if r = request(h, "POST", "/firewall/rules", `{"kind":"close-port","port":3306,"proto":"tcp"}`, nil, nil); r.Code != 500 {
		t.Fatalf("failed apply: %d", r.Code)
	}
	fail = false
	rules, _ := a.firewallRules()
	if len(rules) != 1 {
		t.Fatalf("a failed apply must not be saved: %+v", rules)
	}
	r = request(h, "GET", "/firewall", "", nil, nil)
	var status struct {
		Supported, Active bool
		Rules             []fwRule
		Ports             []fwPort
		SSHPorts          []int `json:"ssh_ports"`
	}
	json.Unmarshal(r.Body.Bytes(), &status)
	if !status.Supported || !status.Active || len(status.Rules) != 1 || len(status.Ports) != 5 || len(status.SSHPorts) != 1 {
		t.Fatalf("status: %s", r.Body.String())
	}
	if r = request(h, "DELETE", "/firewall/rules/"+rule.ID, "", nil, nil); r.Code != 200 || !strings.HasSuffix(applied, "delete table inet junpanel\n") {
		t.Fatalf("delete: %d %q", r.Code, applied)
	}
	if r = request(h, "DELETE", "/firewall/rules/"+rule.ID, "", nil, nil); r.Code != 404 {
		t.Fatalf("delete twice: %d", r.Code)
	}
}
