package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const minBackupFree = 512 << 20

var backupID = regexp.MustCompile(`^b_[a-f0-9]{20}$`)

type backupMeta struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Target   string    `json:"target"`
	Name     string    `json:"name"`
	Database string    `json:"database,omitempty"`
	Note     string    `json:"note,omitempty"`
	Schedule string    `json:"schedule,omitempty"`
	File     string    `json:"file"`
	Size     int64     `json:"size"`
	SHA256   string    `json:"sha256"`
	Created  time.Time `json:"created"`
}

// dataPart is a container directory holding an app's persistent state.
type dataPart struct {
	Service string `json:"service"`
	Path    string `json:"path"`
	Volume  string `json:"volume"`
}

var appData = map[string][]dataPart{
	"wordpress": {{"app", "/var/www/html", "web"}, {"database", "/var/lib/mysql", "database"}},
	"gitea":     {{"app", "/data", "data"}},
	"kuma":      {{"app", "/app/data", "data"}},
	"postgres":  {{"app", "/var/lib/postgresql/data", "data"}},
	"mysql":     {{"app", "/var/lib/mysql", "data"}},
	"redis":     {{"app", "/data", "data"}},
}

// backupTag labels a backup: a note for the list and the schedule that made it.
type backupTag struct{ Note, Schedule string }

type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

func (a *agent) backupDir() string { return filepath.Join(a.cfg.State, "backups") }

// writeBackup streams fill's output through gzip into a private file, then
// records its size and SHA-256. Nothing is listed until the file is complete.
func (a *agent) writeBackup(meta backupMeta, ext string, fill func(io.Writer) error) (backupMeta, error) {
	dir := a.backupDir()
	if e := os.MkdirAll(dir, 0700); e != nil {
		return meta, e
	}
	if free := diskSpace(dir)["free"]; free != 0 && free < minBackupFree {
		return meta, bad(507, "磁盘可用空间不足 512 MiB，已取消备份")
	}
	meta.ID = "b_" + randomID(20)
	meta.Created = time.Now().UTC()
	meta.File = meta.ID + "." + ext
	final := filepath.Join(dir, meta.File)
	partial := final + ".partial"
	f, e := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return meta, e
	}
	finished := false
	defer func() {
		if !finished {
			f.Close()
			os.Remove(partial)
		}
	}()
	sum, size := sha256.New(), &countWriter{}
	gz := gzip.NewWriter(io.MultiWriter(f, sum, size))
	if e = fill(gz); e != nil {
		return meta, e
	}
	if e = gz.Close(); e == nil {
		e = f.Sync()
	}
	if e != nil {
		return meta, bad(500, "写入备份文件失败，请检查磁盘空间")
	}
	finished = true
	if e = f.Close(); e != nil {
		os.Remove(partial)
		return meta, bad(500, "写入备份文件失败，请检查磁盘空间")
	}
	if e = os.Rename(partial, final); e != nil {
		os.Remove(partial)
		return meta, e
	}
	meta.Size, meta.SHA256 = size.n, hex.EncodeToString(sum.Sum(nil))
	if e = atomicJSON(filepath.Join(dir, meta.ID+".json"), meta, 0600); e != nil {
		os.Remove(final)
		return meta, e
	}
	return meta, nil
}
func (a *agent) readBackup(id string) (backupMeta, error) {
	var meta backupMeta
	if !backupID.MatchString(id) {
		return meta, bad(400, "备份编号无效")
	}
	if e := loadJSON(filepath.Join(a.backupDir(), id+".json"), &meta); e != nil {
		if os.IsNotExist(e) {
			return meta, bad(404, "备份不存在")
		}
		return meta, e
	}
	if meta.ID != id || meta.File != id+".tar.gz" && meta.File != id+".sql.gz" {
		return meta, bad(500, "备份记录损坏")
	}
	return meta, nil
}

// openBackup verifies the stored checksum before returning the file.
func (a *agent) openBackup(meta backupMeta) (*os.File, error) {
	f, e := os.Open(filepath.Join(a.backupDir(), meta.File))
	if e != nil {
		return nil, bad(404, "备份文件不存在")
	}
	sum := sha256.New()
	if _, e = io.Copy(sum, f); e != nil || hex.EncodeToString(sum.Sum(nil)) != meta.SHA256 {
		f.Close()
		return nil, bad(409, "备份文件校验失败，已损坏或被修改，已拒绝使用")
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}

// archiveName accepts only clean relative names that cannot leave the target directory.
func archiveName(name string) (string, error) {
	name = strings.TrimSuffix(name, "/")
	if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\x00\\") {
		return "", bad(409, "备份内容包含不安全的路径，已拒绝")
	}
	return name, nil
}

// tarTree writes regular files and directories of root; links and special files are skipped.
func tarTree(ctx context.Context, root *os.Root, tw *tar.Writer) (files, skipped int, err error) {
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == "." {
			return nil
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		if strings.HasPrefix(d.Name(), ".junpanel-") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !d.IsDir() && !info.Mode().IsRegular() {
			skipped++
			return nil
		}
		hdr, e := tar.FileInfoHeader(info, "")
		if e != nil {
			return e
		}
		hdr.Name, hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = p, 0, 0, "", ""
		if d.IsDir() {
			hdr.Name += "/"
		}
		if e = tw.WriteHeader(hdr); e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		f, e := root.Open(p)
		if e != nil {
			return e
		}
		defer f.Close()
		if _, e = io.CopyN(tw, f, hdr.Size); e != nil {
			return fmt.Errorf("文件 %s 在备份期间发生变化", p)
		}
		files++
		return nil
	})
	return
}

// untar restores regular files and directories into dst and rejects everything else.
func untar(ctx context.Context, tr *tar.Reader, dst *os.Root) (int, error) {
	files := 0
	for {
		hdr, e := tr.Next()
		if e == io.EOF {
			return files, nil
		}
		if e != nil {
			return files, bad(409, "备份内容无法读取，文件可能已损坏")
		}
		if e = ctx.Err(); e != nil {
			return files, e
		}
		name, e := archiveName(hdr.Name)
		if e != nil {
			return files, e
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if e = dst.MkdirAll(name, os.FileMode(hdr.Mode).Perm()|0700); e != nil {
				return files, e
			}
		case tar.TypeReg:
			if dir := path.Dir(name); dir != "." {
				if e = dst.MkdirAll(dir, 0755); e != nil {
					return files, e
				}
			}
			f, e := dst.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode).Perm())
			if e != nil {
				return files, e
			}
			_, e = io.CopyN(f, tr, hdr.Size)
			if ce := f.Close(); e == nil {
				e = ce
			}
			if e != nil {
				return files, e
			}
			dst.Chtimes(name, hdr.ModTime, hdr.ModTime)
			files++
		default:
			return files, bad(409, "备份内容包含不支持的文件类型，已拒绝")
		}
	}
}
func (a *agent) staticSite(id string) (site, error) {
	sites, e := a.readSites()
	if e != nil {
		return site{}, e
	}
	for _, s := range sites {
		if s.ID == id {
			if s.Kind != "static" {
				return site{}, bad(400, "只有静态网站有文件可备份")
			}
			return s, nil
		}
	}
	return site{}, bad(404, "网站不存在")
}
func (a *agent) backupSite(ctx context.Context, out io.Writer, s site, tag backupTag) (backupMeta, error) {
	root, e := a.rootFS()
	if e != nil {
		return backupMeta{}, e
	}
	defer root.Close()
	sub, e := root.OpenRoot("sites/" + s.ID)
	if e != nil {
		return backupMeta{}, bad(404, "网站目录不存在")
	}
	defer sub.Close()
	return a.writeBackup(backupMeta{Type: "site", Target: s.ID, Name: s.Name, Note: tag.Note, Schedule: tag.Schedule}, "tar.gz", func(w io.Writer) error {
		tw := tar.NewWriter(w)
		files, skipped, e := tarTree(ctx, sub, tw)
		if e != nil {
			return e
		}
		fmt.Fprintf(out, "已备份 %d 个文件", files)
		if skipped > 0 {
			fmt.Fprintf(out, "，跳过 %d 个符号链接或特殊文件", skipped)
		}
		fmt.Fprintln(out)
		return tw.Close()
	})
}
func (a *agent) restoreSite(ctx context.Context, out io.Writer, meta backupMeta) error {
	s, e := a.staticSite(meta.Target)
	if e != nil {
		return e
	}
	f, e := a.openBackup(meta)
	if e != nil {
		return e
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return bad(409, "备份文件无法读取")
	}
	fmt.Fprintln(out, "先为当前网站创建恢复前备份…")
	pre, e := a.backupSite(ctx, out, s, backupTag{Note: "恢复前自动备份"})
	if e != nil {
		return bad(502, "恢复前备份失败，已取消恢复，网站未被更改："+e.Error())
	}
	fmt.Fprintf(out, "恢复前备份：%s\n", pre.ID)
	root, e := a.rootFS()
	if e != nil {
		return e
	}
	defer root.Close()
	stage := "sites/.junpanel-restore-" + randomID(12)
	if e = root.Mkdir(stage, 0755); e != nil {
		return e
	}
	sub, e := root.OpenRoot(stage)
	if e != nil {
		root.RemoveAll(stage)
		return e
	}
	files, e := untar(ctx, tar.NewReader(gz), sub)
	sub.Close()
	if e != nil {
		root.RemoveAll(stage)
		return e
	}
	fmt.Fprintf(out, "已解出 %d 个文件，正在切换…\n", files)
	live, old := "sites/"+s.ID, "sites/.junpanel-old-"+randomID(12)
	// Hold the lock only for the two renames, not for deleting the old files.
	a.mu.Lock()
	e = root.Rename(live, old)
	if e == nil {
		if e = root.Rename(stage, live); e != nil {
			root.Rename(old, live)
			e = bad(500, "切换网站目录失败，已还原原有文件")
		}
	} else {
		e = bad(500, "切换网站目录失败，网站未被更改")
	}
	a.mu.Unlock()
	if e != nil {
		root.RemoveAll(stage)
		return e
	}
	root.RemoveAll(old)
	fmt.Fprintln(out, "网站文件已恢复。")
	return nil
}

func (a *agent) composeBase(id string) []string {
	return []string{"compose", "--project-directory", a.appDir(id), "--file", filepath.Join(a.appDir(id), "compose.json"), "--project-name", "junpanel-" + id}
}
func (a *agent) compose(ctx context.Context, out io.Writer, rec appRecord, args ...string) error {
	return a.stream(ctx, a.appDir(rec.ID), out, []string{rec.Password, rec.DBPassword}, "docker", append(a.composeBase(rec.ID), args...)...)
}

// markBusy reserves an app for a long operation; releaseApp frees it again.
func (a *agent) markBusy(id string) (appRecord, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rows, e := a.readApps()
	if e != nil {
		return appRecord{}, e
	}
	for i := range rows {
		if rows[i].ID != id {
			continue
		}
		if rows[i].Busy {
			return appRecord{}, bad(409, "此应用已有操作进行中")
		}
		if rows[i].Removed {
			return appRecord{}, bad(409, "应用已卸载，请先恢复部署")
		}
		rows[i].Busy = true
		if e = a.storeApps(rows); e != nil {
			return appRecord{}, e
		}
		return rows[i], nil
	}
	return appRecord{}, bad(404, "应用不存在")
}
func (a *agent) releaseApp(id, lastError string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rows, e := a.readApps()
	if e != nil {
		return
	}
	for i := range rows {
		if rows[i].ID == id {
			rows[i].Busy = false
			rows[i].LastError = lastError
		}
	}
	a.storeApps(rows)
}

// appContainers returns the project's containers, requiring every data service to exist.
func (a *agent) appContainers(rec appRecord, parts []dataPart) (map[string]container, bool, error) {
	rows, e := a.docker.list()
	if e != nil {
		return nil, false, e
	}
	byService, running := map[string]container{}, false
	for _, c := range rows {
		if c.Project != "junpanel-"+rec.ID || c.Oneoff {
			continue
		}
		if c.Protected {
			return nil, false, bad(403, "应用中有受保护容器，操作已拒绝")
		}
		byService[c.Service] = c
		running = running || c.State == "running"
	}
	for _, p := range parts {
		if _, ok := byService[p.Service]; !ok {
			return nil, false, bad(409, "未找到应用容器，请先部署应用")
		}
	}
	return byService, running, nil
}

// copyArchive re-roots a container tar stream under prefix inside tw.
func copyArchive(tw *tar.Writer, r io.Reader, prefix string) (int, error) {
	tr, files := tar.NewReader(r), 0
	for {
		hdr, e := tr.Next()
		if e == io.EOF {
			return files, nil
		}
		if e != nil {
			return files, bad(502, "读取容器数据失败")
		}
		name, e := archiveName(hdr.Name)
		if e != nil {
			return files, e
		}
		h := *hdr
		h.Name, h.Format = prefix+name, tar.FormatUnknown
		if hdr.Typeflag == tar.TypeLink {
			link, e := archiveName(hdr.Linkname)
			if e != nil {
				return files, e
			}
			h.Linkname = prefix + link
		}
		if hdr.Typeflag == tar.TypeDir {
			h.Name += "/"
		}
		if e = tw.WriteHeader(&h); e != nil {
			return files, e
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, e = io.Copy(tw, tr); e != nil {
				return files, bad(502, "读取容器数据失败")
			}
			files++
		}
	}
}

// backupApp stops the app so its files are consistent, archives its data
// directories, and starts it again if it was running.
func (a *agent) backupApp(ctx context.Context, out io.Writer, rec appRecord, tag backupTag) (backupMeta, error) {
	parts := appData[rec.Template]
	if len(parts) == 0 {
		return backupMeta{}, bad(400, "此应用类型暂不支持备份")
	}
	byService, running, e := a.appContainers(rec, parts)
	if e != nil {
		return backupMeta{}, e
	}
	if running {
		a.expectApp(rec.ID)
		fmt.Fprintln(out, "正在停止应用，以保证数据一致…")
		if e = a.compose(ctx, out, rec, "stop", "--timeout", "15"); e != nil {
			return backupMeta{}, bad(502, "停止应用失败，备份已取消")
		}
	}
	meta, e := a.writeBackup(backupMeta{Type: "app", Target: rec.ID, Name: rec.Name, Note: tag.Note, Schedule: tag.Schedule}, "tar.gz", func(w io.Writer) error {
		tw := tar.NewWriter(w)
		manifest, _ := json.Marshal(map[string]any{"version": 1, "app": rec.ID, "template": rec.Template, "created": time.Now().UTC(), "parts": parts})
		if e := tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(manifest)), ModTime: time.Now()}); e != nil {
			return e
		}
		if _, e := tw.Write(manifest); e != nil {
			return e
		}
		for _, p := range parts {
			rc, e := a.docker.getArchive(ctx, byService[p.Service].ID, p.Path)
			if e != nil {
				return e
			}
			files, e := copyArchive(tw, rc, "parts/"+p.Service+"/")
			rc.Close()
			if e != nil {
				return e
			}
			fmt.Fprintf(out, "已备份 %s（%d 个文件）\n", p.Path, files)
		}
		return tw.Close()
	})
	if running {
		fmt.Fprintln(out, "正在重新启动应用…")
		restart, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if re := a.compose(restart, out, rec, "start"); re != nil && e == nil {
			e = bad(502, "备份已完成，但应用重启失败，请到“应用商店 / 已安装”中手动启动")
		}
	}
	return meta, e
}

// putSink feeds one directory's tar entries to a container through a pipe.
type putSink struct {
	pw   *io.PipeWriter
	tw   *tar.Writer
	done chan error
}

func (a *agent) startPut(ctx context.Context, id, dir string) *putSink {
	pr, pw := io.Pipe()
	s := &putSink{pw: pw, tw: tar.NewWriter(pw), done: make(chan error, 1)}
	go func() {
		e := a.docker.putArchive(ctx, id, dir, pr)
		pr.CloseWithError(e)
		s.done <- e
	}()
	return s
}
func (s *putSink) finish(failure error) error {
	if failure != nil {
		s.pw.CloseWithError(failure)
		<-s.done
		return failure
	}
	e := s.tw.Close()
	s.pw.Close()
	if pe := <-s.done; pe != nil {
		return pe
	}
	return e
}

func (a *agent) restoreApp(ctx context.Context, out io.Writer, rec appRecord, meta backupMeta) error {
	parts := appData[rec.Template]
	if len(parts) == 0 || meta.Target != rec.ID {
		return bad(400, "此备份与该应用不匹配")
	}
	f, e := a.openBackup(meta)
	if e != nil {
		return e
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return bad(409, "备份文件无法读取")
	}
	tr := tar.NewReader(gz)
	hdr, e := tr.Next()
	if e != nil || hdr.Name != "manifest.json" || hdr.Size > 64<<10 {
		return bad(409, "备份缺少有效的清单，已拒绝")
	}
	var manifest struct {
		Version  int
		App      string
		Template string
		Parts    []dataPart
	}
	if e = json.NewDecoder(io.LimitReader(tr, hdr.Size)).Decode(&manifest); e != nil || manifest.Version != 1 || manifest.App != rec.ID || manifest.Template != rec.Template || len(manifest.Parts) != len(parts) {
		return bad(409, "备份清单与该应用不匹配，已拒绝")
	}
	for i, p := range manifest.Parts {
		if p != parts[i] {
			return bad(409, "备份的数据目录与当前应用模板不符，已拒绝")
		}
	}
	_, wasRunning, e := a.appContainers(rec, parts)
	if e != nil {
		return e
	}
	fmt.Fprintln(out, "先为当前应用创建恢复前备份…")
	pre, e := a.backupApp(ctx, out, rec, backupTag{Note: "恢复前自动备份"})
	if e != nil {
		return bad(502, "恢复前备份失败，已取消恢复，应用数据未被更改："+e.Error())
	}
	fmt.Fprintf(out, "恢复前备份：%s（恢复失败时可用它回滚）\n", pre.ID)
	failed := func(step string, e error) error {
		return bad(502, fmt.Sprintf("%s失败：%s。恢复前备份 %s 保存了恢复前的数据", step, e.Error(), pre.ID))
	}
	a.expectApp(rec.ID)
	fmt.Fprintln(out, "正在移除旧容器与数据卷…")
	if e = a.compose(ctx, out, rec, "down", "--timeout", "15"); e != nil {
		return failed("移除旧容器", e)
	}
	seen := map[string]bool{}
	for _, p := range parts {
		if !seen[p.Volume] {
			seen[p.Volume] = true
			if e = a.docker.removeVolume(ctx, "junpanel-"+rec.ID+"_"+p.Volume); e != nil {
				return failed("清空数据卷", e)
			}
		}
	}
	fmt.Fprintln(out, "正在重建容器并写回数据…")
	if e = a.compose(ctx, out, rec, "up", "--no-start", "--no-build", "--pull", "missing"); e != nil {
		return failed("重建容器", e)
	}
	byService, _, e := a.appContainers(rec, parts)
	if e != nil {
		return failed("重建容器", e)
	}
	var sink *putSink
	var current *dataPart
	finish := func(failure error) error {
		if sink == nil {
			return failure
		}
		e := sink.finish(failure)
		sink = nil
		return e
	}
	for {
		hdr, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return failed("读取备份", finish(bad(409, "备份内容无法读取，文件可能已损坏")))
		}
		name, e := archiveName(hdr.Name)
		if e != nil {
			return failed("读取备份", finish(e))
		}
		service, rest, ok := strings.Cut(strings.TrimPrefix(name, "parts/"), "/")
		if !strings.HasPrefix(name, "parts/") || !ok || rest == "" {
			continue
		}
		if current == nil || current.Service != service {
			if e = finish(nil); e != nil {
				return failed("写入数据", e)
			}
			current = nil
			for i := range parts {
				if parts[i].Service == service {
					current = &parts[i]
				}
			}
			if current == nil {
				return failed("读取备份", bad(409, "备份包含未知的数据目录，已拒绝"))
			}
			fmt.Fprintf(out, "正在写回 %s\n", current.Path)
			sink = a.startPut(ctx, byService[service].ID, path.Dir(current.Path))
		}
		if first, _, _ := strings.Cut(rest, "/"); first != path.Base(current.Path) {
			return failed("读取备份", finish(bad(409, "备份目录结构不符，已拒绝")))
		}
		h := *hdr
		h.Name, h.Format = rest, tar.FormatUnknown
		if hdr.Typeflag == tar.TypeDir {
			h.Name += "/"
		}
		if hdr.Typeflag == tar.TypeLink {
			link := strings.TrimPrefix(hdr.Linkname, "parts/"+service+"/")
			h.Linkname = link
		}
		if e = sink.tw.WriteHeader(&h); e == nil && hdr.Typeflag == tar.TypeReg {
			_, e = io.Copy(sink.tw, tr)
		}
		if e != nil {
			return failed("写入数据", finish(e))
		}
	}
	if e = finish(nil); e != nil {
		return failed("写入数据", e)
	}
	if wasRunning {
		fmt.Fprintln(out, "正在启动应用…")
		if e = a.compose(ctx, out, rec, "start"); e != nil {
			return failed("启动应用", e)
		}
	}
	fmt.Fprintln(out, "应用数据已恢复。")
	return nil
}

func dumpArgs(record appRecord, kind, db string) ([]string, []string, error) {
	if !dbIdentifier.MatchString(db) {
		return nil, nil, bad(400, "数据库名需为字母、数字与下划线")
	}
	switch kind {
	case "postgres":
		return []string{"pg_dump", "--username", "junpanel", "--dbname", db, "--no-owner", "--no-privileges"}, []string{"PGPASSWORD=" + record.Password}, nil
	case "mysql", "mariadb":
		bin := "mysqldump"
		if kind == "mariadb" {
			bin = "mariadb-dump"
		}
		return []string{bin, "--user=root", "--single-transaction", "--skip-lock-tables", db}, []string{"MYSQL_PWD=" + record.Password}, nil
	}
	return nil, nil, bad(400, "Redis 请通过 SSH 使用原生持久化工具，或备份整个应用")
}
func (a *agent) backupDatabase(ctx context.Context, out io.Writer, id, db string, tag backupTag) (backupMeta, error) {
	record, kind, e := a.databaseRecord(id)
	if e != nil {
		return backupMeta{}, e
	}
	args, env, e := dumpArgs(record, kind, db)
	if e != nil {
		return backupMeta{}, e
	}
	c, e := a.databaseContainer(record, kind)
	if e != nil {
		return backupMeta{}, e
	}
	return a.writeBackup(backupMeta{Type: "database", Target: id, Name: record.Name, Database: db, Note: tag.Note, Schedule: tag.Schedule}, "sql.gz", func(w io.Writer) error {
		size := &countWriter{}
		stderr, e := a.docker.execStream(ctx, c.ID, args, env, io.MultiWriter(w, size))
		if e != nil {
			return e
		}
		if size.n == 0 {
			return bad(400, "导出内容为空，请核对数据库名称")
		}
		if stderr != "" {
			fmt.Fprintf(out, "原生工具提示：%s\n", truncate(stderr, 500))
		}
		fmt.Fprintf(out, "已导出 %d 字节（压缩前）\n", size.n)
		return nil
	})
}

const backupTimeout = 3 * time.Hour

func (a *agent) backupList(w http.ResponseWriter, r *http.Request) error {
	entries, e := os.ReadDir(a.backupDir())
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	out := []backupMeta{}
	for _, entry := range entries {
		if id, ok := strings.CutSuffix(entry.Name(), ".json"); ok && backupID.MatchString(id) {
			if meta, e := a.readBackup(id); e == nil {
				out = append(out, meta)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	send(w, 200, map[string]any{"backups": out, "free": diskSpace(a.backupDir())["free"], "directory": a.backupDir()})
	return nil
}

// backupSpec says what to back up; Tag labels the result.
type backupSpec struct {
	Type     string `json:"type"`
	Target   string `json:"target"`
	Database string `json:"database"`
	Tag      backupTag
}

func (a *agent) backupCreate(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Type     string `json:"type"`
		Target   string `json:"target"`
		Database string `json:"database"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	j, e := a.startBackup(backupSpec{Type: req.Type, Target: req.Target, Database: req.Database}, nil)
	if e != nil {
		return e
	}
	send(w, 202, j)
	return nil
}

// startBackup validates the request and runs the backup as a job. done, if
// given, receives the finished backup (or the error) from inside the job.
func (a *agent) startBackup(spec backupSpec, done func(backupMeta, error)) (job, error) {
	finish := func(out *limitedBuffer, meta backupMeta, e error) error {
		if e == nil {
			fmt.Fprintf(out, "备份完成：%s\n", meta.ID)
		}
		if done != nil {
			done(meta, e)
		}
		return e
	}
	switch spec.Type {
	case "site":
		if !siteID.MatchString(spec.Target) {
			return job{}, bad(400, "网站标识无效")
		}
		a.mu.Lock()
		s, e := a.staticSite(spec.Target)
		a.mu.Unlock()
		if e != nil {
			return job{}, e
		}
		return a.jobs.startWithin(backupTimeout, "备份 · 网站 · "+s.Name, func(ctx context.Context, out *limitedBuffer) error {
			meta, e := a.backupSite(ctx, out, s, spec.Tag)
			return finish(out, meta, e)
		})
	case "app":
		if e := requireName(spec.Target); e != nil {
			return job{}, e
		}
		rec, e := a.markBusy(spec.Target)
		if e != nil {
			return job{}, e
		}
		j, e := a.jobs.startWithin(backupTimeout, "备份 · 应用 · "+rec.Name, func(ctx context.Context, out *limitedBuffer) error {
			meta, e := a.backupApp(ctx, out, rec, spec.Tag)
			if e != nil {
				a.releaseApp(rec.ID, "上次备份未完成，请检查任务日志")
			} else {
				a.releaseApp(rec.ID, "")
			}
			return finish(out, meta, e)
		})
		if e != nil {
			a.releaseApp(rec.ID, "")
			return job{}, e
		}
		return j, nil
	case "database":
		if e := requireName(spec.Target); e != nil {
			return job{}, e
		}
		record, kind, e := a.databaseRecord(spec.Target)
		if e != nil {
			return job{}, e
		}
		if _, _, e = dumpArgs(record, kind, spec.Database); e != nil {
			return job{}, e
		}
		if _, e = a.databaseContainer(record, kind); e != nil {
			return job{}, e
		}
		return a.jobs.startWithin(backupTimeout, "备份 · 数据库 · "+record.Name+" / "+spec.Database, func(ctx context.Context, out *limitedBuffer) error {
			meta, e := a.backupDatabase(ctx, out, spec.Target, spec.Database, spec.Tag)
			return finish(out, meta, e)
		})
	}
	return job{}, bad(400, "备份类型无效")
}
func (a *agent) backupDownload(w http.ResponseWriter, r *http.Request) error {
	meta, e := a.readBackup(r.PathValue("id"))
	if e != nil {
		return e
	}
	f, e := os.Open(filepath.Join(a.backupDir(), meta.File))
	if e != nil {
		return bad(404, "备份文件不存在")
	}
	defer f.Close()
	name := fmt.Sprintf("%s-%s-%s%s", meta.Type, meta.Target, meta.Created.Format("20060102-150405"), strings.TrimPrefix(meta.File, meta.ID))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-SHA256", meta.SHA256)
	http.ServeContent(w, r, name, meta.Created, f)
	return nil
}
func (a *agent) backupDelete(w http.ResponseWriter, r *http.Request) error {
	meta, e := a.readBackup(r.PathValue("id"))
	if e != nil {
		return e
	}
	if e = os.Remove(filepath.Join(a.backupDir(), meta.File)); e != nil && !os.IsNotExist(e) {
		return e
	}
	if e = os.Remove(filepath.Join(a.backupDir(), meta.ID+".json")); e != nil {
		return e
	}
	send(w, 200, map[string]any{"ok": true})
	return nil
}
func (a *agent) backupRestore(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if !req.Confirm {
		return bad(400, "恢复会覆盖现有数据，需要明确确认")
	}
	meta, e := a.readBackup(r.PathValue("id"))
	if e != nil {
		return e
	}
	var j job
	switch meta.Type {
	case "site":
		s, e := a.staticSite(meta.Target)
		if e != nil {
			return e
		}
		j, e = a.jobs.startWithin(backupTimeout, "恢复 · 网站 · "+s.Name, func(ctx context.Context, out *limitedBuffer) error {
			return a.restoreSite(ctx, out, meta)
		})
		if e != nil {
			return e
		}
	case "app":
		rec, e := a.markBusy(meta.Target)
		if e != nil {
			return e
		}
		j, e = a.jobs.startWithin(backupTimeout, "恢复 · 应用 · "+rec.Name, func(ctx context.Context, out *limitedBuffer) error {
			e := a.restoreApp(ctx, out, rec, meta)
			if e != nil {
				a.releaseApp(rec.ID, "上次恢复未完成，请检查任务日志")
			} else {
				a.releaseApp(rec.ID, "")
			}
			return e
		})
		if e != nil {
			a.releaseApp(rec.ID, "")
			return e
		}
	default:
		return bad(400, "数据库导出文件请使用数据库原生工具导入")
	}
	send(w, 202, j)
	return nil
}
