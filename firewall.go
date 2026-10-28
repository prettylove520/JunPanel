package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The firewall manages one nftables table of its own, "inet junpanel". Its input chain runs just
// before the usual filter chains and only ever drops: a blocked address or a closed port. An accept
// in it ends only this chain, so it never opens anything another firewall (ufw, firewalld, Docker)
// has closed. With no rules the table is removed, so the server behaves exactly as before.

const fwTable = "junpanel"

// nftCandidates lists where Debian and Ubuntu install nft; tests point it elsewhere.
var nftCandidates = []string{"/usr/sbin/nft", "/sbin/nft", "/usr/bin/nft"}

type fwRule struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"` // "block-ip" or "close-port"
	Source  string    `json:"source,omitempty"`
	Port    int       `json:"port,omitempty"`
	Proto   string    `json:"proto,omitempty"` // tcp, udp or both
	Note    string    `json:"note"`
	Created time.Time `json:"created"`
}

type fwPort struct {
	Proto   string `json:"proto"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Process string `json:"process"`
	Public  bool   `json:"public"`
}

func nftBinary() string {
	for _, p := range nftCandidates {
		if st, e := os.Stat(p); e == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func (a *agent) firewallPath() string { return filepath.Join(a.cfg.State, "firewall.json") }

func (a *agent) firewallRules() ([]fwRule, error) {
	rows := []fwRule{}
	b, e := os.ReadFile(a.firewallPath())
	if os.IsNotExist(e) {
		return rows, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(b, &rows); e != nil {
		return nil, fmt.Errorf("防火墙规则文件损坏：%w", e)
	}
	return rows, nil
}

// listeningPorts reads `ss -H -tulnp`: one socket per line, local address in the fifth field.
func listeningPorts(out string) []fwPort {
	seen := map[string]bool{}
	ports := []fwPort{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || (f[0] != "tcp" && f[0] != "udp") {
			continue
		}
		local := f[4]
		i := strings.LastIndex(local, ":")
		if i < 0 {
			continue
		}
		port, e := strconv.Atoi(local[i+1:])
		if e != nil || port <= 0 {
			continue
		}
		addr := strings.Trim(local[:i], "[]")
		if j := strings.Index(addr, "%"); j >= 0 {
			addr = addr[:j]
		}
		if addr == "*" {
			addr = "0.0.0.0"
		}
		process := ""
		if len(f) > 6 {
			if s := strings.Index(f[6], `(("`); s >= 0 {
				rest := f[6][s+3:]
				if end := strings.Index(rest, `"`); end >= 0 {
					process = rest[:end]
				}
			}
		}
		ip := net.ParseIP(addr)
		public := ip == nil || !ip.IsLoopback()
		key := f[0] + "|" + addr + "|" + strconv.Itoa(port)
		if seen[key] {
			continue
		}
		seen[key] = true
		ports = append(ports, fwPort{Proto: f[0], Address: addr, Port: port, Process: process, Public: public})
	}
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Public != ports[j].Public {
			return ports[i].Public
		}
		if ports[i].Port != ports[j].Port {
			return ports[i].Port < ports[j].Port
		}
		return ports[i].Proto+ports[i].Address < ports[j].Proto+ports[j].Address
	})
	return ports
}

// sshPorts are the TCP ports sshd listens on; closing one could lock the owner out, so it is refused.
// When nothing can be read, 22 is assumed.
func sshPorts(ports []fwPort) []int {
	set := map[int]bool{}
	for _, p := range ports {
		if p.Proto == "tcp" && p.Process == "sshd" {
			set[p.Port] = true
		}
	}
	if len(set) == 0 {
		set[22] = true
	}
	out := []int{}
	for p := range set {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// checkRule normalises a rule and refuses anything that could cut the owner off or is malformed.
func checkRule(r fwRule, existing []fwRule, protected []int) (fwRule, error) {
	r.Note = strings.TrimSpace(r.Note)
	if len([]rune(r.Note)) > 60 {
		return r, bad(400, "备注不超过 60 个字")
	}
	switch r.Kind {
	case "block-ip":
		src := strings.TrimSpace(r.Source)
		var ip net.IP
		if strings.Contains(src, "/") {
			addr, network, e := net.ParseCIDR(src)
			if e != nil {
				return r, bad(400, "地址格式无效：填写 IP（如 203.0.113.7）或网段（如 203.0.113.0/24）")
			}
			ones, bits := network.Mask.Size()
			if (bits == 32 && ones < 8) || (bits == 128 && ones < 32) {
				return r, bad(400, "网段过大：IPv4 至少 /8，IPv6 至少 /32，避免误封大量地址")
			}
			ip, src = addr, network.String()
		} else {
			ip = net.ParseIP(src)
			if ip == nil {
				return r, bad(400, "地址格式无效：填写 IP（如 203.0.113.7）或网段（如 203.0.113.0/24）")
			}
			src = ip.String()
		}
		if ip.IsLoopback() || ip.IsUnspecified() {
			return r, bad(400, "不能封禁本机回环地址或全部地址")
		}
		r.Source, r.Port, r.Proto = src, 0, ""
		for _, x := range existing {
			if x.Kind == "block-ip" && x.Source == src {
				return r, bad(409, "这个地址已经封禁")
			}
		}
	case "close-port":
		if r.Port < 1 || r.Port > 65535 {
			return r, bad(400, "端口应在 1 到 65535 之间")
		}
		if r.Proto != "tcp" && r.Proto != "udp" && r.Proto != "both" {
			return r, bad(400, "协议应为 TCP、UDP 或两者")
		}
		if r.Proto != "udp" {
			for _, p := range protected {
				if p == r.Port {
					return r, bad(409, fmt.Sprintf("端口 %d 是 SSH 登录端口，关闭后可能无法再登录服务器，已拒绝", r.Port))
				}
			}
		}
		r.Source = ""
		for _, x := range existing {
			if x.Kind == "close-port" && x.Port == r.Port && (x.Proto == r.Proto || x.Proto == "both" || r.Proto == "both") {
				return r, bad(409, "这个端口已经关闭")
			}
		}
	default:
		return r, bad(400, "规则类型无效")
	}
	if len(existing) >= 200 {
		return r, bad(409, "规则已达 200 条上限")
	}
	return r, nil
}

// nftScript replaces the table in one transaction: create it (so the delete never fails), delete it,
// and, when there are rules, define it again with them.
func nftScript(rules []fwRule) string {
	var b strings.Builder
	b.WriteString("table inet " + fwTable + " {\n}\ndelete table inet " + fwTable + "\n")
	if len(rules) == 0 {
		return b.String()
	}
	var v4, v6 []string
	tcp, udp := []string{}, []string{}
	for _, r := range rules {
		switch r.Kind {
		case "block-ip":
			if ip := net.ParseIP(strings.Split(r.Source, "/")[0]); ip != nil && ip.To4() != nil {
				v4 = append(v4, r.Source)
			} else {
				v6 = append(v6, r.Source)
			}
		case "close-port":
			if r.Proto != "udp" {
				tcp = append(tcp, strconv.Itoa(r.Port))
			}
			if r.Proto != "tcp" {
				udp = append(udp, strconv.Itoa(r.Port))
			}
		}
	}
	b.WriteString("table inet " + fwTable + " {\n\tchain input {\n\t\ttype filter hook input priority filter - 1; policy accept;\n\t\tiifname \"lo\" accept\n")
	if len(v4) > 0 {
		b.WriteString("\t\tip saddr { " + strings.Join(v4, ", ") + " } drop\n")
	}
	if len(v6) > 0 {
		b.WriteString("\t\tip6 saddr { " + strings.Join(v6, ", ") + " } drop\n")
	}
	b.WriteString("\t\tct state established,related accept\n")
	if len(tcp) > 0 {
		b.WriteString("\t\ttcp dport { " + strings.Join(tcp, ", ") + " } drop\n")
	}
	if len(udp) > 0 {
		b.WriteString("\t\tudp dport { " + strings.Join(udp, ", ") + " } drop\n")
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

func (a *agent) firewallApply(rules []fwRule) error {
	nft := nftBinary()
	if nft == "" {
		if len(rules) == 0 {
			return nil
		}
		return bad(409, "服务器没有安装 nftables（nft 命令），请先通过 SSH 运行 apt install nftables")
	}
	script := filepath.Join(a.cfg.State, "firewall.nft")
	if e := atomicBytes(script, []byte(nftScript(rules)), 0600); e != nil {
		return e
	}
	if out, e := a.runner(20*time.Second, nft, "-f", script); e != nil {
		return bad(500, "应用防火墙规则失败："+strings.TrimSpace(out))
	}
	return nil
}

// firewallRestore puts the saved rules back after a restart; nftables does not keep them itself.
func (a *agent) firewallRestore() {
	rules, e := a.firewallRules()
	if e != nil {
		log.Printf("firewall: %v", e)
		return
	}
	if len(rules) == 0 {
		return
	}
	if e = a.firewallApply(rules); e != nil {
		log.Printf("firewall: %v", e)
	}
}

func (a *agent) firewallPorts() []fwPort {
	out, e := a.runner(10*time.Second, "ss", "-H", "-tulnp")
	if e != nil {
		return []fwPort{}
	}
	return listeningPorts(out)
}

func (a *agent) firewallStatus(w http.ResponseWriter, r *http.Request) error {
	a.fwMu.Lock()
	defer a.fwMu.Unlock()
	rules, e := a.firewallRules()
	if e != nil {
		return e
	}
	nft := nftBinary()
	active := false
	if nft != "" {
		_, err := a.runner(10*time.Second, nft, "list", "table", "inet", fwTable)
		active = err == nil
	}
	ports := a.firewallPorts()
	send(w, 200, map[string]any{"supported": nft != "", "active": active, "rules": rules, "ports": ports, "ssh_ports": sshPorts(ports)})
	return nil
}

func (a *agent) firewallAdd(w http.ResponseWriter, r *http.Request) error {
	var req fwRule
	if e := body(r, &req); e != nil {
		return e
	}
	a.fwMu.Lock()
	defer a.fwMu.Unlock()
	rules, e := a.firewallRules()
	if e != nil {
		return e
	}
	rule, e := checkRule(req, rules, sshPorts(a.firewallPorts()))
	if e != nil {
		return e
	}
	rule.ID, rule.Created = "f_"+randomID(16), a.now().UTC()
	next := append(append([]fwRule{}, rules...), rule)
	if e = a.firewallApply(next); e != nil {
		return e
	}
	if e = atomicJSON(a.firewallPath(), next, 0600); e != nil {
		a.firewallApply(rules)
		return e
	}
	send(w, 200, rule)
	return nil
}

func (a *agent) firewallDelete(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	a.fwMu.Lock()
	defer a.fwMu.Unlock()
	rules, e := a.firewallRules()
	if e != nil {
		return e
	}
	next := []fwRule{}
	for _, x := range rules {
		if x.ID != id {
			next = append(next, x)
		}
	}
	if len(next) == len(rules) {
		return bad(404, "规则不存在")
	}
	if e = a.firewallApply(next); e != nil {
		return e
	}
	if e = atomicJSON(a.firewallPath(), next, 0600); e != nil {
		a.firewallApply(rules)
		return e
	}
	send(w, 200, map[string]any{"ok": true})
	return nil
}
