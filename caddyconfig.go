package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// The administrator's own part of the main Caddyfile can be edited here: the sites that existed before
// the panel, or anything the panel has no form for. Every save keeps a copy of the previous file, asks
// Caddy to validate the new one before it is used, and puts the old file back if validation or the reload
// fails. The line that imports the panel's own fragments must stay, or the panel's sites would vanish.
const (
	maxCaddyfile     = 512 << 10
	keepCaddyBackups = 30
)

var caddyBackupName = regexp.MustCompile(`^Caddyfile\.[0-9]{8}-[0-9]{6}(-[0-9a-f]{4})?\.bak$`)

func (a *agent) caddyBackupDir() string { return filepath.Join(a.cfg.State, "caddy-backups") }

// caddyLinkLine is the line the installer added so the main Caddyfile loads the panel's fragments.
func (a *agent) caddyLinkLine() string {
	return "import " + filepath.ToSlash(a.cfg.Fragments) + "/*.caddy"
}

func (a *agent) caddyLinked(content string) bool {
	want := a.caddyLinkLine()
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// caddyEditable says whether the agent may write the main Caddyfile at all. Installs made before this
// feature mount /etc/caddy read-only for the agent; the upgrade installer widens that.
func (a *agent) caddyEditable() bool {
	f, e := os.OpenFile(a.cfg.Caddy, os.O_WRONLY, 0)
	if e != nil {
		return false
	}
	f.Close()
	return true
}

func readOnlyFault(e error) error {
	if errors.Is(e, syscall.EROFS) || os.IsPermission(e) {
		return bad(409, "当前安装的服务权限不允许面板修改 Caddy 主配置。请把面板升级到最新版本（升级会更新服务权限），或用 SSH 直接编辑 /etc/caddy/Caddyfile")
	}
	return e
}

type caddyBackup struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
	Size int64     `json:"size"`
}

func (a *agent) caddyBackups() []caddyBackup {
	entries, _ := os.ReadDir(a.caddyBackupDir())
	out := []caddyBackup{}
	for _, entry := range entries {
		if !caddyBackupName.MatchString(entry.Name()) {
			continue
		}
		if info, e := entry.Info(); e == nil && info.Mode().IsRegular() {
			out = append(out, caddyBackup{ID: entry.Name(), Time: info.ModTime(), Size: info.Size()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// saveCaddyBackup keeps the file that is about to be replaced and drops the oldest copies.
func (a *agent) saveCaddyBackup(old []byte) error {
	dir := a.caddyBackupDir()
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	name := "Caddyfile." + a.now().Format("20060102-150405") + ".bak"
	if _, e := os.Stat(filepath.Join(dir, name)); e == nil {
		name = strings.TrimSuffix(name, ".bak") + "-" + randomID(4) + ".bak"
	}
	if e := atomicBytes(filepath.Join(dir, name), old, 0600); e != nil {
		return e
	}
	for i, b := range a.caddyBackups() {
		if i >= keepCaddyBackups {
			os.Remove(filepath.Join(dir, b.ID))
		}
	}
	return nil
}

// writeCaddyfile rewrites the file in place, so its owner and permissions stay as they were.
func (a *agent) writeCaddyfile(data []byte) error {
	f, e := os.OpenFile(a.cfg.Caddy, os.O_WRONLY|os.O_TRUNC, 0)
	if e != nil {
		return readOnlyFault(e)
	}
	_, e = f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	if c := f.Close(); e == nil {
		e = c
	}
	return e
}

func (a *agent) reloadCaddy() (string, error) {
	return a.runner(20*time.Second, "/usr/bin/systemctl", "reload", "caddy.service")
}

// applyCaddyfile replaces the main Caddyfile with content after the checks, or leaves the old one.
func (a *agent) applyCaddyfile(content string, old []byte) error {
	if len(content) > maxCaddyfile || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return bad(400, "配置过大或含有无法保存的字符")
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if !a.caddyLinked(content) {
		return bad(400, "不能删除面板的接入行，否则面板创建的网站会失效。请保留这一行：\n"+a.caddyLinkLine())
	}
	if content == string(old) {
		return bad(400, "内容没有变化")
	}
	if e := a.saveCaddyBackup(old); e != nil {
		return bad(500, "无法保存旧配置的备份，已取消修改")
	}
	if e := a.writeCaddyfile([]byte(content)); e != nil {
		a.writeCaddyfile(old)
		return e
	}
	if out, e := a.caddyValidate(); e != nil {
		if a.writeCaddyfile(old) != nil {
			return bad(500, "校验失败，且无法还原原配置；请立即通过 SSH 检查 /etc/caddy/Caddyfile")
		}
		return bad(409, withReason("Caddy 校验未通过，原配置已保留", out))
	}
	if out, e := a.reloadCaddy(); e != nil {
		if a.writeCaddyfile(old) != nil {
			return bad(500, "重载失败，且无法还原原配置；请立即通过 SSH 检查 /etc/caddy/Caddyfile")
		}
		if _, e = a.reloadCaddy(); e != nil {
			return bad(500, "重载失败，还原后的重载也失败；请立即通过 SSH 检查")
		}
		return bad(409, withReason("Caddy 重载失败，原配置已还原", out))
	}
	return nil
}

func (a *agent) readCaddyfile() ([]byte, error) {
	raw, e := os.ReadFile(a.cfg.Caddy)
	if e != nil {
		return nil, bad(500, "无法读取主 Caddyfile")
	}
	if len(raw) > maxCaddyfile || !utf8.Valid(raw) {
		return nil, bad(409, "主配置过大或不是 UTF-8 文本，面板不提供在线编辑")
	}
	return raw, nil
}

func (a *agent) caddyConfigGet(w http.ResponseWriter, r *http.Request) error {
	raw, e := a.readCaddyfile()
	if e != nil {
		return e
	}
	send(w, 200, map[string]any{"path": a.cfg.Caddy, "content": string(raw), "sha": fileDigest(raw), "editable": a.caddyEditable(), "link": a.caddyLinkLine(), "backups": a.caddyBackups()})
	return nil
}

func (a *agent) caddyConfigSave(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Content string `json:"content"`
		SHA     string `json:"sha"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old, e := a.readCaddyfile()
	if e != nil {
		return e
	}
	if req.SHA != fileDigest(old) {
		return bad(409, "配置文件在你打开之后被改动过，请关闭窗口重新打开再改")
	}
	if e = a.applyCaddyfile(req.Content, old); e != nil {
		return e
	}
	send(w, 200, map[string]any{"ok": true})
	return nil
}

func (a *agent) caddyBackupRead(id string) ([]byte, error) {
	if !caddyBackupName.MatchString(id) {
		return nil, bad(400, "备份标识无效")
	}
	raw, e := os.ReadFile(filepath.Join(a.caddyBackupDir(), id))
	if e != nil {
		return nil, bad(404, "备份不存在")
	}
	return raw, nil
}

func (a *agent) caddyBackupGet(w http.ResponseWriter, r *http.Request) error {
	raw, e := a.caddyBackupRead(r.PathValue("id"))
	if e != nil {
		return e
	}
	send(w, 200, map[string]any{"id": r.PathValue("id"), "content": string(raw)})
	return nil
}

func (a *agent) caddyConfigRestore(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		ID  string `json:"id"`
		SHA string `json:"sha"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	saved, e := a.caddyBackupRead(req.ID)
	if e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old, e := a.readCaddyfile()
	if e != nil {
		return e
	}
	if req.SHA != fileDigest(old) {
		return bad(409, "配置文件在你打开之后被改动过，请关闭窗口重新打开")
	}
	if e = a.applyCaddyfile(string(saved), old); e != nil {
		return e
	}
	send(w, 200, map[string]any{"ok": true})
	return nil
}
