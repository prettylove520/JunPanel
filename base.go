package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

type fault struct {
	Code int
	Text string
}

func (e *fault) Error() string        { return e.Text }
func bad(code int, text string) error { return &fault{code, text} }
func randomID(n int) string {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)[:n]
}
func atomicJSON(path string, value any, mode os.FileMode) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	return atomicBytes(path, b, mode)
}
func atomicBytes(path string, b []byte, mode os.FileMode) error {
	if st, e := os.Lstat(path); e == nil && !st.Mode().IsRegular() {
		return bad(409, "拒绝覆盖非普通文件")
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".junpanel-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	closed := f.Close()
	if e != nil {
		return e
	}
	if closed != nil {
		return closed
	}
	return os.Rename(f.Name(), path)
}
func loadJSON(path string, value any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if len(b) > 8<<20 {
		return bad(500, "状态文件过大")
	}
	return json.Unmarshal(b, value)
}
func send(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, e error) {
	if f, ok := e.(*fault); ok {
		send(w, f.Code, map[string]any{"error": f.Text})
		return
	}
	send(w, 500, map[string]any{"error": "操作未完成，请查看服务状态并核对结果"})
}
func body(r *http.Request, v any) error {
	reader := io.LimitReader(r.Body, 12<<20)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(v); e != nil {
		return bad(400, "请求参数格式错误")
	}
	var extra any
	if e := decoder.Decode(&extra); e != io.EOF {
		return bad(400, "请求只能包含一个 JSON 对象")
	}
	return nil
}
func endpoint(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				send(w, 500, map[string]any{"error": "处理请求时发生错误"})
			}
		}()
		if e := fn(w, r); e != nil {
			fail(w, e)
		}
	}
}
func unixTransport(path string) *http.Transport {
	return &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", path)
	}, MaxIdleConns: 8, MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 80 * time.Second}
}

type limitedBuffer struct {
	mu  sync.Mutex
	b   []byte
	Max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	b.b = append(b.b, p...)
	if len(b.b) > b.Max {
		b.b = b.b[len(b.b)-b.Max:]
	}
	return n, nil
}
func (b *limitedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.b) }
func runCommand(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C.UTF-8"}
	cmd.Env = append(cmd.Env, env...)
	out := &limitedBuffer{Max: 256 << 10}
	cmd.Stdout = out
	cmd.Stderr = out
	e := cmd.Run()
	return out.String(), e
}
func command(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return runCommand(ctx, "", nil, name, args...)
}

// Redact whole output lines so a secret split across process writes stays hidden.
type redactor struct {
	sync.Mutex
	out     io.Writer
	pending []byte
	secrets []string
}

func (r *redactor) emit(b []byte) {
	s := string(b)
	for _, secret := range r.secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[密钥已隐藏]")
		}
	}
	r.out.Write([]byte(s))
}
func (r *redactor) Write(p []byte) (int, error) {
	r.Lock()
	defer r.Unlock()
	r.pending = append(r.pending, p...)
	for {
		i := strings.IndexByte(string(r.pending), '\n')
		if i < 0 {
			break
		}
		r.emit(r.pending[:i+1])
		r.pending = r.pending[i+1:]
	}
	// Keep a bounded overlap, including enough bytes for every possible secret.
	if len(r.pending) > 32768 {
		keep := 256
		for _, s := range r.secrets {
			if len(s) > keep {
				keep = len(s)
			}
		}
		cut := len(r.pending) - keep
		for _, secret := range r.secrets {
			if secret == "" {
				continue
			}
			start := cut - len(secret)
			if start < 0 {
				start = 0
			}
			if i := strings.Index(string(r.pending[start:]), secret); i >= 0 && start+i < cut && start+i+len(secret) > cut {
				cut = start + i
			}
		}
		r.emit(r.pending[:cut])
		r.pending = r.pending[cut:]
	}
	return len(p), nil
}
func (r *redactor) flush() { r.Lock(); defer r.Unlock(); r.emit(r.pending); r.pending = nil }
func streamCommand(ctx context.Context, dir string, out io.Writer, secrets []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C.UTF-8"}
	writer := &redactor{out: out, secrets: secrets}
	cmd.Stdout = writer
	cmd.Stderr = writer
	e := cmd.Run()
	writer.flush()
	return e
}

var shortName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)
var containerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func requireName(s string) error {
	if !shortName.MatchString(s) {
		return bad(400, "名称需为 2–32 位小写字母、数字与中划线，以字母开头")
	}
	return nil
}
func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
func cleanText(s string) string {
	re := regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)
	s = re.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}
func problem(msg string, args ...any) error { return bad(400, fmt.Sprintf(msg, args...)) }
