package main

import (
	"crypto/sha256"
	"encoding/hex"
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

// The administrator's own Caddy configuration can be edited here: the main Caddyfile and the files it
// imports from the same folder (where sites made before the panel usually live). Every save keeps a
// copy of the previous file, asks Caddy to validate the result before it is used, and puts the old file
// back if validation or the reload fails. The main file's line importing the panel's own fragments must
// stay, or the panel's sites would vanish.
const (
	maxCaddyfile     = 512 << 10
	keepCaddyBackups = 30
)

var caddyBackupName = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]{0,99})\.([0-9]{8}-[0-9]{6})(-[0-9a-f]{4})?\.bak$`)

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

// caddyFiles is the main Caddyfile followed by the regular files it imports from its own folder.
// Imports elsewhere, snippets, symlinks and the panel's fragment folder are not offered.
func (a *agent) caddyFiles() []string {
	main := a.cfg.Caddy
	out := []string{main}
	raw, e := os.ReadFile(main)
	if e != nil {
		return out
	}
	dir := filepath.Dir(main)
	seen := map[string]bool{main: true}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(stripCaddyComment(line))
		if len(f) != 2 || f[0] != "import" {
			continue
		}
		pattern := f[1]
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(dir, pattern)
		}
		matches := []string{pattern}
		if strings.ContainsAny(pattern, "*?[") {
			matches, _ = filepath.Glob(pattern)
		}
		sort.Strings(matches)
		for _, m := range matches {
			m = filepath.Clean(m)
			if filepath.Dir(m) != dir || seen[m] {
				continue
			}
			if info, e := os.Lstat(m); e != nil || !info.Mode().IsRegular() {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// caddyFile resolves a requested file: empty means the main Caddyfile; anything else must be one of caddyFiles.
func (a *agent) caddyFile(name string) (string, error) {
	if name == "" {
		return a.cfg.Caddy, nil
	}
	for _, f := range a.caddyFiles() {
		if f == name {
			return f, nil
		}
	}
	return "", bad(404, "只能编辑主 Caddyfile 及它在同一目录下引用的文件")
}

// caddyEditable says whether the agent may write the file. Installs made before this feature mount
// /etc/caddy read-only for the agent; the upgrade installer widens that.
func caddyEditable(path string) bool {
	f, e := os.OpenFile(path, os.O_WRONLY, 0)
	if e != nil {
		return false
	}
	f.Close()
	return true
}

func readOnlyFault(e error) error {
	if errors.Is(e, syscall.EROFS) || os.IsPermission(e) {
		return bad(409, "当前安装的服务权限不允许面板修改这个 Caddy 配置文件。请把面板升级到最新版本（升级会更新服务权限），或用 SSH 直接编辑")
	}
	return e
}

type caddyBackup struct {
	ID   string    `json:"id"`
	File string    `json:"file"`
	Time time.Time `json:"time"`
	Size int64     `json:"size"`
}

// caddyBackups lists the kept copies, newest first; file limits the list to one configuration file.
func (a *agent) caddyBackups(file string) []caddyBackup {
	entries, _ := os.ReadDir(a.caddyBackupDir())
	dir := filepath.Dir(a.cfg.Caddy)
	out := []caddyBackup{}
	for _, entry := range entries {
		m := caddyBackupName.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		path := filepath.Join(dir, m[1])
		if file != "" && path != file {
			continue
		}
		if info, e := entry.Info(); e == nil && info.Mode().IsRegular() {
			out = append(out, caddyBackup{ID: entry.Name(), File: path, Time: info.ModTime(), Size: info.Size()})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ti := caddyBackupName.FindStringSubmatch(out[i].ID)[2]
		tj := caddyBackupName.FindStringSubmatch(out[j].ID)[2]
		if ti != tj {
			return ti > tj
		}
		return out[i].ID > out[j].ID
	})
	return out
}

// saveCaddyBackup keeps the file that is about to be replaced and drops that file's oldest copies.
func (a *agent) saveCaddyBackup(path string, old []byte) error {
	dir := a.caddyBackupDir()
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	name := filepath.Base(path) + "." + a.now().Format("20060102-150405") + ".bak"
	if _, e := os.Stat(filepath.Join(dir, name)); e == nil {
		name = strings.TrimSuffix(name, ".bak") + "-" + randomID(4) + ".bak"
	}
	if !caddyBackupName.MatchString(name) {
		return bad(400, "配置文件名无法用于备份")
	}
	if e := atomicBytes(filepath.Join(dir, name), old, 0600); e != nil {
		return e
	}
	for i, b := range a.caddyBackups(path) {
		if i >= keepCaddyBackups {
			os.Remove(filepath.Join(dir, b.ID))
		}
	}
	return nil
}

// writeConfigFile rewrites a file in place, so its owner and permissions stay as they were.
func writeConfigFile(path string, data []byte) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
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

// normalizeCaddyText checks an edited file and gives it Unix line endings and a final newline.
func normalizeCaddyText(content string) (string, error) {
	if len(content) > maxCaddyfile || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return "", bad(400, "配置过大或含有无法保存的字符")
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content, nil
}

// applyConfigFile replaces one configuration file after the checks, or leaves the old one in place.
func (a *agent) applyConfigFile(path, content string, old []byte) error {
	content, e := normalizeCaddyText(content)
	if e != nil {
		return e
	}
	if path == a.cfg.Caddy && !a.caddyLinked(content) {
		return bad(400, "不能删除面板的接入行，否则面板创建的网站会失效。请保留这一行：\n"+a.caddyLinkLine())
	}
	if content == string(old) {
		return bad(400, "内容没有变化")
	}
	if e := a.saveCaddyBackup(path, old); e != nil {
		return bad(500, "无法保存旧配置的备份，已取消修改")
	}
	if e := writeConfigFile(path, []byte(content)); e != nil {
		writeConfigFile(path, old)
		return e
	}
	if out, e := a.caddyValidate(); e != nil {
		if writeConfigFile(path, old) != nil {
			return bad(500, "校验失败，且无法还原原配置；请立即通过 SSH 检查 "+path)
		}
		return bad(409, withReason("Caddy 校验未通过，原配置已保留", out))
	}
	if out, e := a.reloadCaddy(); e != nil {
		if writeConfigFile(path, old) != nil {
			return bad(500, "重载失败，且无法还原原配置；请立即通过 SSH 检查 "+path)
		}
		if _, e = a.reloadCaddy(); e != nil {
			return bad(500, "重载失败，还原后的重载也失败；请立即通过 SSH 检查")
		}
		return bad(409, withReason("Caddy 重载失败，原配置已还原", out))
	}
	return nil
}

func readConfigFile(path string) ([]byte, error) {
	raw, e := os.ReadFile(path)
	if e != nil {
		return nil, bad(500, "无法读取 "+path)
	}
	if len(raw) > maxCaddyfile || !utf8.Valid(raw) {
		return nil, bad(409, path+" 过大或不是 UTF-8 文本，面板不提供在线编辑")
	}
	return raw, nil
}

// caddyFilesDigest fingerprints every editable file at once, so an adoption based on what the page
// showed is refused if any of them changed in between.
func (a *agent) caddyFilesDigest(files []string) (map[string][]byte, string, error) {
	contents := map[string][]byte{}
	sum := sha256.New()
	for _, f := range files {
		raw, e := readConfigFile(f)
		if e != nil {
			return nil, "", e
		}
		contents[f] = raw
		sum.Write([]byte(f + "\x00"))
		sum.Write(raw)
		sum.Write([]byte{0})
	}
	return contents, hex.EncodeToString(sum.Sum(nil)), nil
}

func (a *agent) caddyConfigGet(w http.ResponseWriter, r *http.Request) error {
	path, e := a.caddyFile(r.URL.Query().Get("file"))
	if e != nil {
		return e
	}
	raw, e := readConfigFile(path)
	if e != nil {
		return e
	}
	main := path == a.cfg.Caddy
	link := ""
	if main {
		link = a.caddyLinkLine()
	}
	send(w, 200, map[string]any{"path": path, "main": main, "content": string(raw), "sha": fileDigest(raw), "editable": caddyEditable(path), "link": link, "files": a.caddyFiles(), "backups": a.caddyBackups(path)})
	return nil
}

func (a *agent) caddyConfigSave(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		File    string `json:"file"`
		Content string `json:"content"`
		SHA     string `json:"sha"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	path, e := a.caddyFile(req.File)
	if e != nil {
		return e
	}
	old, e := readConfigFile(path)
	if e != nil {
		return e
	}
	if req.SHA != fileDigest(old) {
		return bad(409, "配置文件在你打开之后被改动过，请关闭窗口重新打开再改")
	}
	if e = a.applyConfigFile(path, req.Content, old); e != nil {
		return e
	}
	send(w, 200, map[string]any{"ok": true})
	return nil
}

func (a *agent) caddyBackupRead(id string) (string, []byte, error) {
	m := caddyBackupName.FindStringSubmatch(id)
	if m == nil {
		return "", nil, bad(400, "备份标识无效")
	}
	raw, e := os.ReadFile(filepath.Join(a.caddyBackupDir(), id))
	if e != nil {
		return "", nil, bad(404, "备份不存在")
	}
	return filepath.Join(filepath.Dir(a.cfg.Caddy), m[1]), raw, nil
}

func (a *agent) caddyBackupGet(w http.ResponseWriter, r *http.Request) error {
	path, raw, e := a.caddyBackupRead(r.PathValue("id"))
	if e != nil {
		return e
	}
	send(w, 200, map[string]any{"id": r.PathValue("id"), "file": path, "content": string(raw)})
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
	target, saved, e := a.caddyBackupRead(req.ID)
	if e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	path, e := a.caddyFile(target)
	if e != nil {
		return bad(409, "这份备份对应的文件已不再被主 Caddyfile 引用，不能直接恢复")
	}
	old, e := readConfigFile(path)
	if e != nil {
		return e
	}
	if req.SHA != fileDigest(old) {
		return bad(409, "配置文件在你打开之后被改动过，请关闭窗口重新打开")
	}
	if e = a.applyConfigFile(path, string(saved), old); e != nil {
		return e
	}
	send(w, 200, map[string]any{"ok": true})
	return nil
}
