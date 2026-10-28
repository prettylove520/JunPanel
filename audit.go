package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// The audit log records who changed what and when signing in succeeded or failed.
// It is written by the web service, which sees the client address; request bodies are
// never stored, only a few named fields that cannot carry secrets.

type auditEntry struct {
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`
	IP     string    `json:"ip"`
	Action string    `json:"action"`
	Detail string    `json:"detail,omitempty"`
	Method string    `json:"method,omitempty"`
	Path   string    `json:"path,omitempty"`
	Status int       `json:"status,omitempty"`
	Result string    `json:"result"`
}

type auditLog struct {
	mu    sync.Mutex
	path  string
	limit int64
}

func newAuditLog(path string) *auditLog { return &auditLog{path: path, limit: 2 << 20} }

func (l *auditLog) add(e auditEntry) {
	if l == nil {
		return
	}
	e.Time = time.Now().UTC()
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if st, err := os.Stat(l.path); err == nil && st.Size()+int64(len(line)) > l.limit {
		os.Rename(l.path, l.path+".1")
	}
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		log.Printf("audit log not written: %v", err)
		return
	}
	defer f.Close()
	f.Write(append(line, '\n'))
}

// list returns the newest entries of one kind first.
func (l *auditLog) list(kind string, max int) []auditEntry {
	out := []auditEntry{}
	if l == nil {
		return out
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, p := range []string{l.path + ".1", l.path} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		s := bufio.NewScanner(f)
		s.Buffer(make([]byte, 64<<10), 64<<10)
		for s.Scan() {
			var e auditEntry
			if json.Unmarshal(s.Bytes(), &e) == nil && (kind == "" || e.Kind == kind) {
				out = append(out, e)
			}
		}
		f.Close()
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// Fields that name what an operation acted on. Passwords, codes, SQL, file text and
// Compose sources are deliberately absent.
var auditFields = []string{"action", "type", "kind", "target", "id", "name", "template", "database", "image", "container", "path", "to", "domain", "ip", "only"}

func summarize(raw []byte) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	parts := []string{}
	for _, k := range auditFields {
		v, ok := m[k]
		if !ok {
			continue
		}
		var s string
		switch x := v.(type) {
		case string:
			s = x
		case bool:
			if !x {
				continue
			}
			s = "是"
		case float64:
			s = fmt.Sprint(x)
		default:
			continue
		}
		if s == "" {
			continue
		}
		parts = append(parts, k+"="+truncate(cleanText(s), 120))
	}
	return strings.Join(parts, " ")
}

var auditActions = []struct {
	method string
	path   *regexp.Regexp
	label  string
}{
	{"POST", regexp.MustCompile(`^/api/sites$`), "保存网站"},
	{"POST", regexp.MustCompile(`^/api/sites/[^/]+/toggle$`), "启用或停用网站"},
	{"DELETE", regexp.MustCompile(`^/api/sites/[^/]+$`), "删除网站"},
	{"POST", regexp.MustCompile(`^/api/apps$`), "安装应用"},
	{"POST", regexp.MustCompile(`^/api/apps/[^/]+/action$`), "应用操作"},
	{"POST", regexp.MustCompile(`^/api/credentials$`), "查看连接信息"},
	{"POST", regexp.MustCompile(`^/api/containers/[^/]+/action$`), "容器操作"},
	{"POST", regexp.MustCompile(`^/api/images/pull$`), "拉取镜像"},
	{"DELETE", regexp.MustCompile(`^/api/images/[^/]+$`), "删除镜像"},
	{"POST", regexp.MustCompile(`^/api/projects/[^/]+/action$`), "Compose 项目操作"},
	{"POST", regexp.MustCompile(`^/api/stacks$`), "保存编排"},
	{"POST", regexp.MustCompile(`^/api/stacks/[^/]+/action$`), "编排操作"},
	{"DELETE", regexp.MustCompile(`^/api/stacks/[^/]+$`), "删除编排"},
	{"POST", regexp.MustCompile(`^/api/volumes$`), "创建卷"},
	{"DELETE", regexp.MustCompile(`^/api/volumes/[^/]+$`), "删除卷"},
	{"POST", regexp.MustCompile(`^/api/networks$`), "创建网络"},
	{"DELETE", regexp.MustCompile(`^/api/networks/[^/]+$`), "删除网络"},
	{"POST", regexp.MustCompile(`^/api/files/save$`), "保存文件"},
	{"POST", regexp.MustCompile(`^/api/files/upload$`), "上传文件"},
	{"POST", regexp.MustCompile(`^/api/files/mkdir$`), "新建文件夹"},
	{"POST", regexp.MustCompile(`^/api/files/rename$`), "重命名或移动文件"},
	{"POST", regexp.MustCompile(`^/api/files/trash$`), "删除文件（移入回收站）"},
	{"GET", regexp.MustCompile(`^/api/files/download$`), "下载文件"},
	{"POST", regexp.MustCompile(`^/api/trash/[^/]+/restore$`), "从回收站恢复"},
	{"DELETE", regexp.MustCompile(`^/api/trash/[^/]+$`), "永久删除回收站文件"},
	{"POST", regexp.MustCompile(`^/api/trash/empty$`), "清空回收站"},
	{"POST", regexp.MustCompile(`^/api/databases/[^/]+/query$`), "执行 SQL"},
	{"POST", regexp.MustCompile(`^/api/databases/[^/]+/create$`), "新建数据库"},
	{"POST", regexp.MustCompile(`^/api/backups$`), "新建备份"},
	{"POST", regexp.MustCompile(`^/api/backups/[^/]+/restore$`), "恢复备份"},
	{"DELETE", regexp.MustCompile(`^/api/backups/[^/]+$`), "删除备份"},
	{"GET", regexp.MustCompile(`^/api/backups/[^/]+/download$`), "下载备份"},
	{"POST", regexp.MustCompile(`^/api/firewall/rules$`), "添加防火墙规则"},
	{"DELETE", regexp.MustCompile(`^/api/firewall/rules/[^/]+$`), "删除防火墙规则"},
	{"POST", regexp.MustCompile(`^/api/schedules$`), "保存计划任务"},
	{"POST", regexp.MustCompile(`^/api/schedules/[^/]+/toggle$`), "启用或停用计划任务"},
	{"POST", regexp.MustCompile(`^/api/schedules/[^/]+/run$`), "立即运行计划任务"},
	{"DELETE", regexp.MustCompile(`^/api/schedules/[^/]+$`), "删除计划任务"},
	{"POST", regexp.MustCompile(`^/api/alerts/config$`), "修改告警设置"},
	{"POST", regexp.MustCompile(`^/api/alerts/test$`), "发送测试通知"},
	{"POST", regexp.MustCompile(`^/api/upgrade$`), "升级面板"},
	{"POST", regexp.MustCompile(`^/api/upgrade/config$`), "修改自动检查更新"},
	{"POST", regexp.MustCompile(`^/api/access$`), "修改公网入口"},
	{"POST", regexp.MustCompile(`^/api/terminal/ticket$`), "打开容器终端"},
}

// Requests that change nothing, even though they are POSTs.
var auditQuiet = map[string]bool{"/api/heartbeat": true, "/api/stacks/check": true, "/api/alerts/check": true, "/api/upgrade/check": true, "/api/logout": true, "/api/security": true}

func auditAction(method, path string) (string, bool) {
	if auditQuiet[path] {
		return "", false
	}
	for _, a := range auditActions {
		if a.method == method && a.path.MatchString(path) {
			return a.label, true
		}
	}
	if method == "GET" || method == "HEAD" {
		return "", false
	}
	return method + " " + path, true
}

// statusRecorder keeps the response status for the audit entry.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.code == 0 {
		s.code = code
	}
	s.ResponseWriter.WriteHeader(code)
}
func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.code == 0 {
		s.code = 200
	}
	return s.ResponseWriter.Write(b)
}
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func auditResult(code int) string {
	if code >= 200 && code < 400 {
		return "ok"
	}
	return "failed"
}

// auditRequest wraps an authenticated request so its outcome is recorded once it is
// handled. It reads at most a small JSON body to name the target, and puts it back.
func (w *webServer) auditRequest(out http.ResponseWriter, r *http.Request) (http.ResponseWriter, func()) {
	if w.audit == nil {
		return out, func() {}
	}
	label, ok := auditAction(r.Method, r.URL.Path)
	if !ok {
		return out, func() {}
	}
	detail := ""
	if r.URL.Path == "/api/files/download" || r.URL.Path == "/api/files/upload" {
		detail = "path=" + truncate(cleanText(r.URL.Query().Get("path")), 200)
	} else if r.Body != nil && r.ContentLength > 0 && r.ContentLength <= 256<<10 {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 256<<10))
		if err == nil {
			r.Body = io.NopCloser(bytes.NewReader(raw))
			detail = summarize(raw)
		}
	}
	rec := &statusRecorder{ResponseWriter: out}
	method, path, ip := r.Method, r.URL.Path, clientIP(r)
	return rec, func() {
		code := rec.code
		if code == 0 {
			code = 200
		}
		w.audit.add(auditEntry{Kind: "operation", IP: ip, Action: label, Detail: detail, Method: method, Path: path, Status: code, Result: auditResult(code)})
	}
}

func (w *webServer) auditLogin(r *http.Request, action, result string) {
	w.audit.add(auditEntry{Kind: "login", IP: clientIP(r), Action: action, Result: result})
}
