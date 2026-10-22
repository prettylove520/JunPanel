package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The recent journal of the services the panel depends on, so that most problems can be
// read in the panel instead of over SSH. Only these units can be read.
var logUnits = map[string]struct{ unit, label string }{
	"caddy":   {"caddy.service", "Caddy"},
	"agent":   {"junpanel-agent.service", "面板管理代理"},
	"web":     {"junpanel-web.service", "面板网页服务"},
	"docker":  {"docker.service", "Docker"},
	"upgrade": {"junpanel-upgrade.service", "面板升级"},
}

const (
	logMaxLines    = 1000
	logDefault     = 200
	logScanLines   = 3000 // read this many, then filter by the search text
	logMaxResponse = 256 << 10
)

func (a *agent) serviceLogs(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	choice, ok := logUnits[q.Get("unit")]
	if !ok {
		return bad(400, "日志来源无效")
	}
	lines := logDefault
	if v := q.Get("lines"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 20 || n > logMaxLines {
			return bad(400, "行数需为 20–1000")
		}
		lines = n
	}
	level := q.Get("level")
	if level != "" && level != "warning" && level != "err" {
		return bad(400, "级别无效")
	}
	search := strings.TrimSpace(q.Get("search"))
	if len(search) > 100 {
		return bad(400, "搜索内容过长")
	}
	read := lines
	if search != "" {
		read = logScanLines
	}
	args := []string{"--unit", choice.unit, "--lines", strconv.Itoa(read), "--no-pager", "--output", "short-iso", "--quiet"}
	if level != "" {
		args = append(args, "--priority", level)
	}
	out, e := a.runner(15*time.Second, "/usr/bin/journalctl", args...)
	if e != nil && strings.TrimSpace(out) == "" {
		return bad(502, "读取日志失败：此服务器可能没有 journalctl，或没有权限")
	}
	text := cleanText(out)
	rows := []string{}
	for _, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "-- ") {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(search)) {
			continue
		}
		rows = append(rows, truncate(line, 2000))
	}
	if len(rows) > lines {
		rows = rows[len(rows)-lines:]
	}
	joined := strings.Join(rows, "\n")
	trimmed := false
	if len(joined) > logMaxResponse {
		joined, trimmed = joined[len(joined)-logMaxResponse:], true
		if i := strings.IndexByte(joined, '\n'); i >= 0 {
			joined = joined[i+1:]
		}
	}
	send(w, 200, map[string]any{"unit": q.Get("unit"), "label": choice.label, "lines": len(rows), "text": joined, "trimmed": trimmed, "time": time.Now()})
	return nil
}
