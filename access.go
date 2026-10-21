package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// The panel's public entry (a domain and/or an IP served by Caddy) is managed by
// public-access.sh, the same script the installer uses. It changes the main Caddyfile,
// which the agent's sandbox may not write, so it runs as its own transient unit.

//go:embed public-access.sh
var publicAccessScript []byte

var accessDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

type accessEntry struct {
	Kind    string `json:"kind"`
	Address string `json:"address"`
}

// accessEntries reads the addresses the panel's entry file serves.
func (a *agent) accessEntries() ([]accessEntry, bool) {
	data, e := os.ReadFile(filepath.Join(a.cfg.Fragments, "panel-access.caddy"))
	if e != nil || !strings.HasPrefix(string(data), "# JunPanel public HTTPS access - managed by public-access.sh\n") {
		return []accessEntry{}, false
	}
	out := []accessEntry{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != "{" || !strings.HasPrefix(fields[0], "https://") {
			continue
		}
		host := strings.TrimPrefix(fields[0], "https://")
		if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
			out = append(out, accessEntry{"ip", host})
		} else if accessDomain.MatchString(host) {
			out = append(out, accessEntry{"domain", host})
		}
	}
	return out, true
}

type accessState struct {
	mu      sync.Mutex
	running bool
}

func (a *agent) accessStatus(w http.ResponseWriter, r *http.Request) error {
	entries, ours := a.accessEntries()
	a.access.mu.Lock()
	running := a.access.running
	a.access.mu.Unlock()
	_, systemd := os.Stat(a.upSystemd)
	send(w, 200, map[string]any{"entries": entries, "managed": ours, "running": running, "supported": systemd == nil})
	return nil
}

// accessArgs turns a request into public-access.sh arguments, checking every value.
func accessArgs(action, domain, ip string, only bool) ([]string, error) {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	ip = strings.TrimSpace(ip)
	if domain != "" && (len(domain) > 253 || !accessDomain.MatchString(domain) || net.ParseIP(domain) != nil) {
		return nil, bad(400, "域名格式无效：请填写例如 panel.example.com，不要带 https:// 或端口")
	}
	if ip != "" {
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.To4() == nil || parsed.String() != ip {
			return nil, bad(400, "IP 需为公网 IPv4，例如 203.0.113.5")
		}
	}
	switch action {
	case "set":
		if domain == "" && ip == "" {
			return nil, bad(400, "请填写域名或 IP")
		}
		args := []string{}
		if domain != "" {
			args = append(args, "--domain", domain)
		}
		if ip != "" {
			args = append(args, ip)
		}
		if only {
			args = append(args, "--only")
		}
		return args, nil
	case "remove-domain", "remove-ip", "disable":
		if domain != "" || ip != "" || only {
			return nil, bad(400, "此操作不需要填写地址")
		}
		return []string{"--" + action}, nil
	}
	return nil, bad(400, "操作无效")
}

// runAccess runs the script outside the agent's sandbox and returns its exit code.
func runAccess(ctx context.Context, systemdRun string, script []byte, env []string, out io.Writer, args ...string) (int, error) {
	unit := "junpanel-access-" + randomID(8)
	full := []string{"--unit", unit, "--description", "JunPanel public access", "--collect", "--quiet", "--wait", "--pipe"}
	for _, e := range env {
		full = append(full, "--setenv="+e)
	}
	full = append(full, "/bin/bash", "-s", "--")
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, systemdRun, full...)
	cmd.Stdin = bytes.NewReader(script)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	e := cmd.Run()
	var exit *exec.ExitError
	if errors.As(e, &exit) {
		return exit.ExitCode(), nil
	}
	if e != nil {
		return -1, e
	}
	return 0, nil
}

func (a *agent) accessApply(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Action  string `json:"action"`
		Domain  string `json:"domain"`
		IP      string `json:"ip"`
		Only    bool   `json:"only"`
		SkipDNS bool   `json:"skip_dns"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	args, e := accessArgs(req.Action, req.Domain, req.IP, req.Only)
	if e != nil {
		return e
	}
	if _, e := os.Stat(a.upSystemd); e != nil {
		return bad(409, "此服务器没有 systemd-run，请通过 SSH 运行 public-access.sh")
	}
	env := []string{}
	if req.SkipDNS {
		env = append(env, "JUNPANEL_SKIP_DNS_CHECK=1")
	}
	a.access.mu.Lock()
	if a.access.running {
		a.access.mu.Unlock()
		return bad(409, "公网入口正在调整中，请等待当前任务完成")
	}
	a.access.running = true
	a.access.mu.Unlock()
	j, e := a.jobs.startWithin(10*time.Minute, "公网入口 · "+strings.Join(args, " "), func(ctx context.Context, out *limitedBuffer) error {
		defer func() {
			a.access.mu.Lock()
			a.access.running = false
			a.access.mu.Unlock()
		}()
		code, e := a.accessRun(ctx, a.upSystemd, publicAccessScript, env, out, args...)
		switch {
		case e != nil:
			return bad(502, "无法启动入口配置："+e.Error())
		case code == 0:
			return nil
		case code == 2:
			fmt.Fprintln(out, "\n配置已生效，但证书还在申请中，稍后再访问；如长时间不可用，请查看上方提示。")
			return nil
		}
		return bad(502, "入口配置未完成，原入口已恢复；请查看上方输出")
	})
	if e != nil {
		a.access.mu.Lock()
		a.access.running = false
		a.access.mu.Unlock()
		return e
	}
	send(w, 202, j)
	return nil
}
