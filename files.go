package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type fileEntry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Dir      bool      `json:"dir"`
	Link     bool      `json:"link"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Mode     string    `json:"mode"`
}

func virtualPath(input string) (string, error) {
	if len(input) > 1000 || strings.ContainsAny(input, "\x00\\") {
		return "", bad(400, "路径无效")
	}
	for _, part := range strings.Split(input, "/") {
		if part == ".." || part == ".trash" || strings.HasPrefix(part, ".junpanel-") {
			return "", bad(403, "路径不在可管理范围")
		}
	}
	clean := strings.TrimPrefix(path.Clean("/"+input), "/")
	if clean == "" {
		clean = "."
	}
	return clean, nil
}
func (a *agent) rootFS() (*os.Root, error) { return os.OpenRoot(a.cfg.Root) }
func (a *agent) fileList(w http.ResponseWriter, r *http.Request) error {
	name, e := virtualPath(r.URL.Query().Get("path"))
	if e != nil {
		return e
	}
	root, e := a.rootFS()
	if e != nil {
		return e
	}
	defer root.Close()
	dir, e := root.Open(name)
	if e != nil {
		return bad(404, "目录不存在或不可访问")
	}
	defer dir.Close()
	entries, e := dir.ReadDir(5001)
	if e != nil && e != io.EOF {
		return bad(400, "该路径不是可读取目录")
	}
	if len(entries) > 5000 {
		return bad(413, "单目录超过 5000 项，请通过 SSH 整理")
	}
	out := []fileEntry{}
	for _, entry := range entries {
		if entry.Name() == ".trash" || strings.HasPrefix(entry.Name(), ".junpanel-") {
			continue
		}
		info, e := entry.Info()
		if e != nil {
			continue
		}
		out = append(out, fileEntry{Name: entry.Name(), Path: "/" + path.Join(name, entry.Name()), Dir: entry.IsDir(), Link: entry.Type()&os.ModeSymlink != 0, Size: info.Size(), Modified: info.ModTime(), Mode: info.Mode().String()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return out[i].Name < out[j].Name
	})
	send(w, 200, map[string]any{"path": "/" + strings.TrimPrefix(name, "."), "entries": out, "root": a.cfg.Root})
	return nil
}
func (a *agent) readVirtual(input string, limit int64) ([]byte, os.FileInfo, error) {
	name, e := virtualPath(input)
	if e != nil {
		return nil, nil, e
	}
	root, e := a.rootFS()
	if e != nil {
		return nil, nil, e
	}
	defer root.Close()
	f, e := root.Open(name)
	if e != nil {
		return nil, nil, bad(404, "文件不存在或越过管理目录")
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return nil, nil, bad(400, "仅支持普通文件")
	}
	if st.Size() > limit {
		return nil, nil, bad(413, "文件超过本操作大小限制")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil {
		return nil, nil, e
	}
	if int64(len(b)) > limit {
		return nil, nil, bad(413, "文件过大")
	}
	return b, st, nil
}
func fileDigest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func (a *agent) fileRead(w http.ResponseWriter, r *http.Request) error {
	b, _, e := a.readVirtual(r.URL.Query().Get("path"), 1<<20)
	if e != nil {
		return e
	}
	if !utf8.Valid(b) || strings.ContainsRune(string(b), '\x00') {
		return bad(415, "二进制文件请使用下载；文本编辑仅支持 UTF-8")
	}
	send(w, 200, map[string]string{"text": string(b), "revision": fileDigest(b)})
	return nil
}
func (a *agent) fileDownload(w http.ResponseWriter, r *http.Request) error {
	b, st, e := a.readVirtual(r.URL.Query().Get("path"), 8<<20)
	if e != nil {
		return e
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": st.Name()}))
	w.Write(b)
	return nil
}
func (a *agent) writeVirtual(name string, b []byte, expected string, overwrite bool) error {
	if len(b) > 4<<20 {
		return bad(413, "单文件上传上限 4 MiB")
	}
	clean, e := virtualPath(name)
	if e != nil {
		return e
	}
	if clean == "." {
		return bad(400, "不能写入根目录")
	}
	root, e := a.rootFS()
	if e != nil {
		return e
	}
	defer root.Close()
	if st, e := root.Lstat(clean); e == nil {
		if !st.Mode().IsRegular() {
			return bad(409, "仅允许覆盖普通文件，不能覆盖符号链接")
		}
		if !overwrite {
			return bad(409, "同名文件已存在；请先重命名或选择其它名称")
		}
		if expected == "" {
			return bad(409, "编辑时需要文件版本，重新打开后保存")
		}
		if st.Size() > 1<<20 {
			return bad(409, "原文件已超出文本编辑限制，请重新打开")
		}
		f, e := root.Open(clean)
		if e != nil {
			return e
		}
		old, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
		f.Close()
		if e != nil {
			return e
		}
		if len(old) > 1<<20 {
			return bad(409, "原文件超出编辑限制")
		}
		if fileDigest(old) != expected {
			return bad(409, "文件已被其它操作修改，请重新打开后再编辑")
		}
	} else if !os.IsNotExist(e) {
		return e
	} else if expected != "" {
		return bad(409, "原文件已被移除，请重新打开目录后操作")
	}
	temp := path.Join(path.Dir(clean), ".junpanel-"+randomID(24))
	f, e := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if e != nil {
		return bad(400, "目录不存在或不可写")
	}
	defer root.Remove(temp)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	f.Close()
	if e != nil {
		return e
	}
	if !overwrite {
		if e = root.Link(temp, clean); e != nil {
			return bad(409, "目标已存在或无法创建文件，未覆盖")
		}
		return nil
	}
	return root.Rename(temp, clean)
}
func (a *agent) fileSave(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Path     string `json:"path"`
		Text     string `json:"text"`
		Revision string `json:"revision"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if len(req.Text) > 1<<20 {
		return bad(413, "文本编辑上限 1 MiB")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.writeVirtual(req.Path, []byte(req.Text), req.Revision, true); e != nil {
		return e
	}
	send(w, 200, map[string]any{"ok": true, "revision": fileDigest([]byte(req.Text))})
	return nil
}
func (a *agent) fileUpload(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Path string `json:"path"`
		Data string `json:"data"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	b, e := base64.StdEncoding.DecodeString(req.Data)
	if e != nil {
		return bad(400, "文件编码无效")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if e = a.writeVirtual(req.Path, b, "", false); e != nil {
		return e
	}
	send(w, 200, map[string]any{"ok": true})
	return nil
}
func (a *agent) fileMkdir(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Path string `json:"path"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	name, e := virtualPath(req.Path)
	if e != nil {
		return e
	}
	if name == "." {
		return bad(409, "根目录已存在")
	}
	root, e := a.rootFS()
	if e != nil {
		return e
	}
	defer root.Close()
	if e = root.Mkdir(name, 0755); e != nil {
		return bad(409, "创建失败，请检查父目录和同名文件")
	}
	send(w, 200, map[string]any{"ok": true})
	return nil
}
func (a *agent) fileRename(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Path string `json:"path"`
		To   string `json:"to"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	from, e := virtualPath(req.Path)
	if e != nil {
		return e
	}
	to, e := virtualPath(req.To)
	if e != nil {
		return e
	}
	if from == "." || to == "." {
		return bad(403, "不能移动根目录")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if from == "sites" || strings.HasPrefix(from, "sites/") && len(strings.Split(from, "/")) == 2 {
		return bad(403, "请勿移动站点根目录；可操作其内部文件")
	}
	root, e := a.rootFS()
	if e != nil {
		return e
	}
	defer root.Close()
	if _, e = root.Lstat(to); e == nil {
		return bad(409, "目标名称已存在")
	}
	if e = root.Rename(from, to); e != nil {
		return bad(409, "移动失败，请检查目录是否有效")
	}
	send(w, 200, map[string]any{"ok": true})
	return nil
}

type trashRecord struct {
	ID   string    `json:"id"`
	Path string    `json:"path"`
	At   time.Time `json:"at"`
}

func (a *agent) fileTrash(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Path string `json:"path"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	name, e := virtualPath(req.Path)
	if e != nil {
		return e
	}
	if name == "." || name == "sites" {
		return bad(403, "不能移走管理根目录")
	}
	sites, e := a.readSites()
	if e != nil {
		return e
	}
	for _, s := range sites {
		if s.Root != "" {
			relative, _ := filepath.Rel(a.cfg.Root, s.Root)
			if name == filepath.ToSlash(relative) {
				return bad(409, "站点根目录请通过网站模块管理；可操作目录内文件")
			}
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	root, e := a.rootFS()
	if e != nil {
		return e
	}
	defer root.Close()
	id := "t_" + randomID(20)
	record := trashRecord{id, "/" + name, time.Now()}
	meta, e := jsonMarshal(record)
	if e != nil {
		return e
	}
	if e = root.WriteFile(".trash/"+id+".json", meta, 0600); e != nil {
		return e
	}
	if e = root.Rename(name, ".trash/"+id+".data"); e != nil {
		root.Remove(".trash/" + id + ".json")
		return bad(409, "移入回收站失败")
	}
	send(w, 200, record)
	return nil
}
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
func (a *agent) trashList(w http.ResponseWriter, r *http.Request) error {
	root, e := a.rootFS()
	if e != nil {
		return e
	}
	defer root.Close()
	dir, e := root.Open(".trash")
	if e != nil {
		return e
	}
	defer dir.Close()
	entries, _ := dir.ReadDir(5000)
	out := []trashRecord{}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			b, e := root.ReadFile(".trash/" + entry.Name())
			if e != nil {
				continue
			}
			var record trashRecord
			if json.Unmarshal(b, &record) == nil {
				out = append(out, record)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	send(w, 200, out)
	return nil
}
func (a *agent) trashRestore(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !regexp.MustCompile(`^t_[a-f0-9]{20}$`).MatchString(id) {
		return bad(400, "回收记录无效")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	root, e := a.rootFS()
	if e != nil {
		return e
	}
	defer root.Close()
	b, e := root.ReadFile(".trash/" + id + ".json")
	if e != nil {
		return bad(404, "回收记录不存在")
	}
	var record trashRecord
	if json.Unmarshal(b, &record) != nil {
		return bad(500, "回收记录损坏")
	}
	to, e := virtualPath(record.Path)
	if e != nil {
		return e
	}
	if _, e = root.Lstat(to); e == nil {
		return bad(409, "原位置已有同名项目，未覆盖")
	}
	if e = root.Rename(".trash/"+id+".data", to); e != nil {
		return bad(409, "恢复失败，请先检查原父目录是否存在")
	}
	root.Remove(".trash/" + id + ".json")
	send(w, 200, map[string]any{"ok": true})
	return nil
}
