package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"
)

func writeSites(t *testing.T, a *agent, sites ...site) {
	t.Helper()
	if e := atomicJSON(filepath.Join(a.cfg.State, "sites.json"), sites, 0600); e != nil {
		t.Fatal(e)
	}
}
func seedSite(t *testing.T, a *agent, id string, files map[string]string) site {
	t.Helper()
	s := site{ID: id, Name: "示例", Domains: []string{"example.com"}, Kind: "static", Root: filepath.Join(a.cfg.Root, "sites", id), Enabled: true, Created: time.Now()}
	for name, text := range files {
		p := filepath.Join(s.Root, name)
		if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte(text), 0644); e != nil {
			t.Fatal(e)
		}
	}
	writeSites(t, a, s)
	return s
}
func listBackups(t *testing.T, h http.Handler) []backupMeta {
	t.Helper()
	var out struct{ Backups []backupMeta }
	json.Unmarshal(request(h, "GET", "/backups", "", nil, nil).Body.Bytes(), &out)
	return out.Backups
}
func waitOutcome(t *testing.T, a *agent, body []byte) job {
	t.Helper()
	var started job
	if e := json.Unmarshal(body, &started); e != nil || started.ID == "" {
		t.Fatalf("not a job: %s", body)
	}
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		for _, j := range a.jobs.list() {
			if j.ID == started.ID && j.State != "running" {
				return j
			}
		}
	}
	t.Fatal("job did not finish")
	return job{}
}
func tarNames(t *testing.T, gzipped []byte) []string {
	t.Helper()
	gz, e := gzip.NewReader(bytes.NewReader(gzipped))
	if e != nil {
		t.Fatal(e)
	}
	var names []string
	tr := tar.NewReader(gz)
	for {
		h, e := tr.Next()
		if e == io.EOF {
			return names
		}
		if e != nil {
			t.Fatal(e)
		}
		names = append(names, h.Name)
	}
}

func TestSiteBackupRestoreAndIntegrity(t *testing.T) {
	a := testAgent(t)
	h := a.routes()
	s := seedSite(t, a, "s_"+strings.Repeat("a", 20), map[string]string{"index.html": "original", "css/site.css": "body{}", ".junpanel-temp": "partial upload"})
	if e := os.Symlink("/etc/passwd", filepath.Join(s.Root, "link")); e != nil {
		t.Fatal(e)
	}
	r := request(h, "POST", "/backups", `{"type":"site","target":"`+s.ID+`"}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	if j := waitOutcome(t, a, r.Body.Bytes()); j.State != "success" || !strings.Contains(j.Output, "跳过 1 个符号链接") {
		t.Fatalf("backup job: %+v", j)
	}
	backups := listBackups(t, h)
	if len(backups) != 1 || backups[0].Type != "site" || backups[0].Target != s.ID || backups[0].SHA256 == "" {
		t.Fatalf("listing: %+v", backups)
	}
	dl := request(h, "GET", "/backups/"+backups[0].ID+"/download", "", nil, nil)
	if dl.Code != 200 || fileDigest(dl.Body.Bytes()) != backups[0].SHA256 || dl.Header().Get("X-Content-SHA256") != backups[0].SHA256 {
		t.Fatalf("download: %d", dl.Code)
	}
	joined := strings.Join(tarNames(t, dl.Body.Bytes()), " ")
	if !strings.Contains(joined, "index.html") || !strings.Contains(joined, "css/site.css") || strings.Contains(joined, "link") || strings.Contains(joined, ".junpanel-") {
		t.Fatalf("archive contents: %s", joined)
	}
	if r = request(h, "GET", "/backups/"+backups[0].ID+"/download", "", map[string]string{"Range": "bytes=0-9"}, nil); r.Code != 206 || r.Body.Len() != 10 {
		t.Fatalf("range download: %d", r.Code)
	}

	os.WriteFile(filepath.Join(s.Root, "index.html"), []byte("changed"), 0644)
	os.WriteFile(filepath.Join(s.Root, "extra.html"), []byte("added later"), 0644)
	if r = request(h, "POST", "/backups/"+backups[0].ID+"/restore", `{}`, nil, nil); r.Code != 400 {
		t.Fatal("restore without confirmation accepted")
	}
	r = request(h, "POST", "/backups/"+backups[0].ID+"/restore", `{"confirm":true}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	if j := waitOutcome(t, a, r.Body.Bytes()); j.State != "success" {
		t.Fatalf("restore job: %s\n%s", j.Error, j.Output)
	}
	if b, _ := os.ReadFile(filepath.Join(s.Root, "index.html")); string(b) != "original" {
		t.Fatalf("index.html = %q", b)
	}
	if _, e := os.Stat(filepath.Join(s.Root, "extra.html")); !os.IsNotExist(e) {
		t.Fatal("file added after the backup survived the restore")
	}
	if b, _ := os.ReadFile(filepath.Join(s.Root, "css", "site.css")); string(b) != "body{}" {
		t.Fatal("nested file missing")
	}
	entries, _ := os.ReadDir(filepath.Join(a.cfg.Root, "sites"))
	if len(entries) != 1 {
		t.Fatalf("temporary directories left behind: %v", entries)
	}
	backups = listBackups(t, h)
	if len(backups) != 2 || backups[0].Note != "恢复前自动备份" {
		t.Fatalf("safety backup missing: %+v", backups)
	}

	// A modified archive is refused before anything is touched.
	original := backups[1]
	path := filepath.Join(a.backupDir(), original.File)
	f, _ := os.OpenFile(path, os.O_WRONLY, 0)
	f.WriteAt([]byte{0xff}, 12)
	f.Close()
	os.WriteFile(filepath.Join(s.Root, "index.html"), []byte("keep me"), 0644)
	r = request(h, "POST", "/backups/"+original.ID+"/restore", `{"confirm":true}`, nil, nil)
	if j := waitOutcome(t, a, r.Body.Bytes()); j.State != "failed" || !strings.Contains(j.Error, "校验失败") {
		t.Fatalf("corrupt restore: %+v", j)
	}
	if b, _ := os.ReadFile(filepath.Join(s.Root, "index.html")); string(b) != "keep me" {
		t.Fatal("refused restore changed the site")
	}

	if r = request(h, "DELETE", "/backups/"+original.ID, "", nil, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("backup file left after delete")
	}
	if r = request(h, "GET", "/backups/"+original.ID+"/download", "", nil, nil); r.Code != 404 {
		t.Fatal("deleted backup still downloadable")
	}
}

func TestBackupRequestValidation(t *testing.T) {
	a := testAgent(t)
	h := a.routes()
	cases := []struct {
		method, path, body string
		code               int
	}{
		{"POST", "/backups", `{"type":"nope","target":"x"}`, 400},
		{"POST", "/backups", `{"type":"site","target":"../etc"}`, 400},
		{"POST", "/backups", `{"type":"site","target":"s_` + strings.Repeat("b", 20) + `"}`, 404},
		{"POST", "/backups", `{"type":"app","target":"Bad Name"}`, 400},
		{"POST", "/backups", `{"type":"app","target":"missing"}`, 404},
		{"POST", "/backups", `{"type":"database","target":"missing","database":"x"}`, 404},
		{"POST", "/backups", `{"type":"site","target":"x","extra":1}`, 400},
		{"GET", "/backups/..%2F..%2Fetc%2Fpasswd/download", "", 400},
		{"GET", "/backups/b_zz/download", "", 400},
		{"DELETE", "/backups/b_" + strings.Repeat("0", 20), "", 404},
		{"POST", "/backups/b_" + strings.Repeat("0", 20) + "/restore", `{"confirm":true}`, 404},
	}
	for _, c := range cases {
		if r := request(h, c.method, c.path, c.body, nil, nil); r.Code != c.code {
			t.Errorf("%s %s = %d, want %d (%s)", c.method, c.path, r.Code, c.code, r.Body.String())
		}
	}
	if e := a.storeApps([]appRecord{{ID: "busy-app", Name: "x", Template: "redis", Port: 7000, Busy: true}, {ID: "gone-app", Name: "y", Template: "redis", Port: 7001, Removed: true}}); e != nil {
		t.Fatal(e)
	}
	if r := request(h, "POST", "/backups", `{"type":"app","target":"busy-app"}`, nil, nil); r.Code != 409 {
		t.Errorf("busy app accepted: %d", r.Code)
	}
	if r := request(h, "POST", "/backups", `{"type":"app","target":"gone-app"}`, nil, nil); r.Code != 409 {
		t.Errorf("uninstalled app accepted: %d", r.Code)
	}
	// Proxy sites have no files to back up.
	proxy := site{ID: "s_" + strings.Repeat("c", 20), Name: "p", Domains: []string{"p.example.com"}, Kind: "proxy", Upstream: "http://127.0.0.1:9", Enabled: true}
	writeSites(t, a, proxy)
	if r := request(h, "POST", "/backups", `{"type":"site","target":"`+proxy.ID+`"}`, nil, nil); r.Code != 400 {
		t.Errorf("proxy site accepted: %d", r.Code)
	}
}

func TestBackupDatabaseRestoreRefused(t *testing.T) {
	a := testAgent(t)
	meta, e := a.writeBackup(backupMeta{Type: "database", Target: "x", Name: "x", Database: "db"}, "sql.gz", func(w io.Writer) error {
		_, e := w.Write([]byte("select 1;"))
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	if r := request(a.routes(), "POST", "/backups/"+meta.ID+"/restore", `{"confirm":true}`, nil, nil); r.Code != 400 || !strings.Contains(r.Body.String(), "原生工具") {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
}

func TestWriteBackupLeavesNothingOnFailure(t *testing.T) {
	a := testAgent(t)
	_, e := a.writeBackup(backupMeta{Type: "database", Target: "x"}, "sql.gz", func(w io.Writer) error {
		w.Write([]byte("partial output"))
		return bad(400, "dump failed")
	})
	if e == nil {
		t.Fatal("failure not reported")
	}
	if entries, _ := os.ReadDir(a.backupDir()); len(entries) != 0 {
		t.Fatalf("leftover files: %v", entries)
	}
	// An interrupted run's partial file is removed when the agent starts.
	stale := filepath.Join(a.backupDir(), "b_"+strings.Repeat("1", 20)+".tar.gz.partial")
	os.WriteFile(stale, []byte("x"), 0600)
	if _, e = newAgent(a.cfg); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(stale); !os.IsNotExist(e) {
		t.Fatal("stale partial file kept")
	}
	if st, _ := os.Stat(a.backupDir()); st.Mode().Perm() != 0700 {
		t.Fatalf("backup directory mode %v", st.Mode().Perm())
	}
}

func TestUntarRejectsUnsafeArchives(t *testing.T) {
	build := func(h tar.Header, data string) *tar.Reader {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		h.Size = int64(len(data))
		tw.WriteHeader(&h)
		tw.Write([]byte(data))
		tw.Close()
		return tar.NewReader(&buf)
	}
	cases := map[string]tar.Header{
		"parent traversal": {Name: "../evil", Typeflag: tar.TypeReg, Mode: 0644},
		"nested traversal": {Name: "a/../../evil", Typeflag: tar.TypeReg, Mode: 0644},
		"absolute path":    {Name: "/tmp/evil", Typeflag: tar.TypeReg, Mode: 0644},
		"backslash":        {Name: `a\evil`, Typeflag: tar.TypeReg, Mode: 0644},
		"symlink":          {Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		"hard link":        {Name: "hard", Typeflag: tar.TypeLink, Linkname: "other"},
		"device":           {Name: "dev", Typeflag: tar.TypeChar, Mode: 0644},
		"fifo":             {Name: "pipe", Typeflag: tar.TypeFifo, Mode: 0644},
	}
	for name, h := range cases {
		parent := t.TempDir()
		dst := filepath.Join(parent, "dst")
		os.Mkdir(dst, 0755)
		root, e := os.OpenRoot(dst)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = untar(context.Background(), build(h, "x"), root); e == nil {
			t.Errorf("%s accepted", name)
		}
		root.Close()
		var found []string
		filepath.WalkDir(parent, func(p string, d os.DirEntry, _ error) error {
			if p != parent && p != dst {
				found = append(found, p)
			}
			return nil
		})
		if len(found) != 0 {
			t.Errorf("%s left files behind: %v", name, found)
		}
	}
	// A normal archive extracts with its permissions.
	dst := t.TempDir()
	root, _ := os.OpenRoot(dst)
	defer root.Close()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Name: "d/", Typeflag: tar.TypeDir, Mode: 0755})
	tw.WriteHeader(&tar.Header{Name: "d/f.txt", Typeflag: tar.TypeReg, Mode: 0640, Size: 2, ModTime: time.Unix(1700000000, 0)})
	tw.Write([]byte("ok"))
	tw.Close()
	if n, e := untar(context.Background(), tar.NewReader(&buf), root); e != nil || n != 1 {
		t.Fatalf("%d %v", n, e)
	}
	st, e := os.Stat(filepath.Join(dst, "d", "f.txt"))
	if e != nil || st.Mode().Perm() != 0640 || st.ModTime().Unix() != 1700000000 {
		t.Fatalf("restored file: %v %v", st, e)
	}
}

func TestCopyArchiveRerootsAndRejects(t *testing.T) {
	src := func(entries ...tar.Header) io.Reader {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		for _, h := range entries {
			if h.Typeflag == tar.TypeReg {
				h.Size = 3
			}
			tw.WriteHeader(&h)
			if h.Typeflag == tar.TypeReg {
				tw.Write([]byte("abc"))
			}
		}
		tw.Close()
		return &buf
	}
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	n, e := copyArchive(tw, src(
		tar.Header{Name: "data/", Typeflag: tar.TypeDir, Mode: 0755},
		tar.Header{Name: "data/a", Typeflag: tar.TypeReg, Mode: 0600, Uid: 999, Gid: 999},
		tar.Header{Name: "data/hard", Typeflag: tar.TypeLink, Linkname: "data/a"},
		tar.Header{Name: "data/soft", Typeflag: tar.TypeSymlink, Linkname: "a"},
	), "parts/app/")
	tw.Close()
	if e != nil || n != 1 {
		t.Fatalf("%d %v", n, e)
	}
	tr := tar.NewReader(&out)
	var got []tar.Header
	for {
		h, e := tr.Next()
		if e != nil {
			break
		}
		got = append(got, *h)
	}
	if len(got) != 4 || got[0].Name != "parts/app/data/" || got[1].Name != "parts/app/data/a" || got[1].Uid != 999 || got[1].Mode != 0600 || got[2].Linkname != "parts/app/data/a" || got[3].Linkname != "a" {
		t.Fatalf("re-rooted entries: %+v", got)
	}
	if _, e = copyArchive(tar.NewWriter(io.Discard), src(tar.Header{Name: "../x", Typeflag: tar.TypeReg, Mode: 0600}), "parts/app/"); e == nil {
		t.Fatal("traversal accepted")
	}
	if _, e = copyArchive(tar.NewWriter(io.Discard), src(tar.Header{Name: "ok", Typeflag: tar.TypeLink, Linkname: "../../x"}), "parts/app/"); e == nil {
		t.Fatal("link traversal accepted")
	}
}

func TestDemuxStreams(t *testing.T) {
	frame := func(channel byte, text string) []byte {
		out := make([]byte, 8)
		out[0] = channel
		binary.BigEndian.PutUint32(out[4:], uint32(len(text)))
		return append(out, text...)
	}
	big := strings.Repeat("0123456789", 20000)
	raw := append(append(frame(1, "hello "), frame(2, "warn\n")...), append(frame(1, big), frame(1, "end")...)...)
	var stdout, stderr bytes.Buffer
	if e := demux(iotest.OneByteReader(bytes.NewReader(raw)), &stdout, &stderr); e != nil || stdout.String() != "hello "+big+"end" || stderr.String() != "warn\n" {
		t.Fatalf("demux: %v %d", e, stdout.Len())
	}
	for name, broken := range map[string][]byte{
		"truncated header":  raw[:5],
		"truncated payload": raw[:len(raw)-2],
		"bad channel":       append([]byte{7, 0, 0, 0, 0, 0, 0, 1}, 'x'),
		"bad padding":       append([]byte{1, 0, 9, 0, 0, 0, 0, 1}, 'x'),
	} {
		if e := demux(bytes.NewReader(broken), io.Discard, io.Discard); e == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestStreamedUploadAndDownload(t *testing.T) {
	a := testAgent(t)
	h := a.routes()
	payload := make([]byte, 20<<20)
	rand.Read(payload)
	post := func(path string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8787/files/upload?path="+path, bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := post("/big.bin", payload); w.Code != 200 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	if b, _ := os.ReadFile(filepath.Join(a.cfg.Root, "big.bin")); !bytes.Equal(b, payload) {
		t.Fatal("stored file differs")
	}
	if w := post("/big.bin", []byte("again")); w.Code != 409 {
		t.Fatalf("overwrite: %d", w.Code)
	}
	if w := post("/missing/dir/x.bin", []byte("x")); w.Code != 400 {
		t.Fatalf("missing directory: %d", w.Code)
	}
	if w := post("/../escape.bin", []byte("x")); w.Code != 403 {
		t.Fatalf("traversal: %d", w.Code)
	}
	if w := post("/.trash/x", []byte("x")); w.Code != 403 {
		t.Fatalf("trash path: %d", w.Code)
	}
	dl := request(h, "GET", "/files/download?path=/big.bin", "", nil, nil)
	if dl.Code != 200 || !bytes.Equal(dl.Body.Bytes(), payload) || dl.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("download: %d", dl.Code)
	}
	part := request(h, "GET", "/files/download?path=/big.bin", "", map[string]string{"Range": "bytes=100-199"}, nil)
	if part.Code != 206 || !bytes.Equal(part.Body.Bytes(), payload[100:200]) {
		t.Fatalf("range: %d", part.Code)
	}
	if w := request(h, "GET", "/files/download?path=/sites", "", nil, nil); w.Code != 400 {
		t.Fatalf("directory download: %d", w.Code)
	}
	if entries, _ := os.ReadDir(a.cfg.Root); len(entries) != 3 { // .trash, big.bin, sites
		t.Fatalf("unexpected entries: %v", entries)
	}
}

func TestUploadLimitLeavesNoTemporaryFile(t *testing.T) {
	a := testAgent(t)
	limited := http.MaxBytesReader(httptest.NewRecorder(), io.NopCloser(bytes.NewReader(make([]byte, 1000))), 100)
	if e := a.writeStream("/too-big.bin", limited); e == nil || e.(*fault).Code != 413 {
		t.Fatalf("limit not enforced: %v", e)
	}
	entries, _ := os.ReadDir(a.cfg.Root)
	for _, entry := range entries {
		if entry.Name() == "too-big.bin" || strings.HasPrefix(entry.Name(), ".junpanel-") {
			t.Fatalf("leftover: %s", entry.Name())
		}
	}
	if bodyLimit("POST", "/files/upload") != maxUpload || bodyLimit("POST", "/api/files/upload") != maxUpload || bodyLimit("POST", "/files/save") != 12<<20 {
		t.Fatal("body limits")
	}
}

// A fake Docker engine for the app flows that need no real containers.
type fakeEngine struct {
	mu       sync.Mutex
	id       string
	archive  []byte
	putError bool
	puts     []string
	volumes  []string
}

func (f *fakeEngine) transport() roundTripFunc {
	return func(r *http.Request) (*http.Response, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		path := strings.TrimPrefix(r.URL.Path, "/v1.56")
		switch {
		case r.URL.Path == "/version":
			return fakeResponse(200, `{"ApiVersion":"1.56","MinAPIVersion":"1.44"}`), nil
		case path == "/containers/json":
			return fakeResponse(200, `[{"Id":"`+f.id+`","Names":["/web"],"State":"running","Labels":{"com.docker.compose.project":"junpanel-shop","com.docker.compose.service":"app"}}]`), nil
		case r.Method == "GET" && strings.HasSuffix(path, "/archive"):
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(f.archive))}, nil
		case r.Method == "PUT" && strings.HasSuffix(path, "/archive"):
			io.Copy(io.Discard, r.Body)
			f.puts = append(f.puts, r.URL.Query().Get("path"))
			if f.putError {
				return fakeResponse(500, "boom"), nil
			}
			return fakeResponse(200, ""), nil
		case r.Method == "DELETE" && strings.HasPrefix(path, "/volumes/"):
			f.volumes = append(f.volumes, strings.TrimPrefix(path, "/volumes/"))
			return fakeResponse(204, ""), nil
		}
		return fakeResponse(404, ""), nil
	}
}
func containerTar(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Name: "data/", Typeflag: tar.TypeDir, Mode: 0755})
	tw.WriteHeader(&tar.Header{Name: "data/state.db", Typeflag: tar.TypeReg, Mode: 0600, Size: 5, Uid: 1000, Gid: 1000})
	tw.Write([]byte("hello"))
	tw.Close()
	return buf.Bytes()
}

func TestAppBackupAndRestoreFlowsWithFakeEngine(t *testing.T) {
	a := testAgent(t)
	engine := &fakeEngine{id: strings.Repeat("c", 64), archive: containerTar(t)}
	a.docker = &dockerClient{HTTP: &http.Client{Transport: engine.transport()}}
	var mu sync.Mutex
	var compose []string
	a.stream = func(ctx context.Context, dir string, out io.Writer, secrets []string, name string, args ...string) error {
		mu.Lock()
		defer mu.Unlock()
		compose = append(compose, strings.Join(args[len(a.composeBase("shop")):], " "))
		return nil
	}
	if e := a.storeApps([]appRecord{{ID: "shop", Name: "Shop", Template: "gitea", Port: 7100, Password: "pw-one", DBPassword: "pw-two"}}); e != nil {
		t.Fatal(e)
	}
	h := a.routes()
	r := request(h, "POST", "/backups", `{"type":"app","target":"shop"}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	if rows, _ := a.readApps(); !rows[0].Busy {
		t.Fatal("app not marked busy while backing up")
	}
	if r2 := request(h, "POST", "/backups", `{"type":"app","target":"shop"}`, nil, nil); r2.Code != 409 {
		t.Fatalf("second backup of a busy app: %d", r2.Code)
	}
	if j := waitOutcome(t, a, r.Body.Bytes()); j.State != "success" {
		t.Fatalf("%s\n%s", j.Error, j.Output)
	}
	if rows, _ := a.readApps(); rows[0].Busy || rows[0].LastError != "" {
		t.Fatalf("app left busy: %+v", rows[0])
	}
	if strings.Join(compose, "|") != "stop --timeout 15|start" {
		t.Fatalf("compose calls: %v", compose)
	}
	backups := listBackups(t, h)
	if len(backups) != 1 {
		t.Fatal("backup not listed")
	}
	f, _ := os.Open(filepath.Join(a.backupDir(), backups[0].File))
	gz, _ := gzip.NewReader(f)
	tr := tar.NewReader(gz)
	var names []string
	var manifest string
	for {
		hdr, e := tr.Next()
		if e != nil {
			break
		}
		names = append(names, hdr.Name)
		if hdr.Name == "manifest.json" {
			b, _ := io.ReadAll(tr)
			manifest = string(b)
		}
	}
	f.Close()
	if strings.Join(names, " ") != "manifest.json parts/app/data/ parts/app/data/state.db" {
		t.Fatalf("archive layout: %v", names)
	}
	if strings.Contains(manifest, "pw-one") || strings.Contains(manifest, "pw-two") || !strings.Contains(manifest, `"template":"gitea"`) {
		t.Fatalf("manifest: %s", manifest)
	}

	// Restore wipes the volume, then writes the data back into the data directory's parent.
	compose = nil
	r = request(h, "POST", "/backups/"+backups[0].ID+"/restore", `{"confirm":true}`, nil, nil)
	if j := waitOutcome(t, a, r.Body.Bytes()); j.State != "success" {
		t.Fatalf("restore: %s\n%s", j.Error, j.Output)
	}
	wantCalls := "stop --timeout 15|start|down --timeout 15|up --no-start --no-build --pull missing|start"
	if strings.Join(compose, "|") != wantCalls {
		t.Fatalf("compose calls: %v", compose)
	}
	if len(engine.volumes) != 1 || engine.volumes[0] != "junpanel-shop_data" || len(engine.puts) != 1 || engine.puts[0] != "/" {
		t.Fatalf("volumes %v puts %v", engine.volumes, engine.puts)
	}

	// A failed write leaves the app marked with the error and names the safety backup.
	engine.putError = true
	r = request(h, "POST", "/backups/"+backups[0].ID+"/restore", `{"confirm":true}`, nil, nil)
	j := waitOutcome(t, a, r.Body.Bytes())
	if j.State != "failed" || !strings.Contains(j.Error, "恢复前备份 b_") {
		t.Fatalf("failure not reported with the safety backup: %+v", j)
	}
	if rows, _ := a.readApps(); rows[0].Busy || !strings.Contains(rows[0].LastError, "恢复未完成") {
		t.Fatalf("app state after failure: %+v", rows[0])
	}

	// Protected containers block backups outright.
	engine.putError = false
	a.docker = &dockerClient{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			return fakeResponse(200, `[{"Id":"`+engine.id+`","State":"running","Labels":{"junpanel.protected":"true","com.docker.compose.project":"junpanel-shop","com.docker.compose.service":"app"}}]`), nil
		}
		return engine.transport()(r)
	})}}
	if r = request(h, "POST", "/backups", `{"type":"app","target":"shop"}`, nil, nil); r.Code != 202 {
		t.Fatal(r.Body.String())
	}
	if j = waitOutcome(t, a, r.Body.Bytes()); j.State != "failed" || !strings.Contains(j.Error, "受保护") {
		t.Fatalf("protected container backed up: %+v", j)
	}
}

func TestRestoreRejectsMismatchedManifest(t *testing.T) {
	a := testAgent(t)
	a.docker = &dockerClient{HTTP: &http.Client{Transport: (&fakeEngine{id: strings.Repeat("d", 64)}).transport()}}
	a.stream = func(context.Context, string, io.Writer, []string, string, ...string) error { return nil }
	a.storeApps([]appRecord{{ID: "shop", Name: "Shop", Template: "redis", Port: 7200}})
	meta, e := a.writeBackup(backupMeta{Type: "app", Target: "shop", Name: "Shop"}, "tar.gz", func(w io.Writer) error {
		tw := tar.NewWriter(w)
		manifest := []byte(`{"version":1,"app":"shop","template":"gitea","parts":[{"service":"app","path":"/data","volume":"data"}]}`)
		tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(manifest))})
		tw.Write(manifest)
		return tw.Close()
	})
	if e != nil {
		t.Fatal(e)
	}
	r := request(a.routes(), "POST", "/backups/"+meta.ID+"/restore", `{"confirm":true}`, nil, nil)
	if j := waitOutcome(t, a, r.Body.Bytes()); j.State != "failed" || !strings.Contains(j.Error, "不匹配") {
		t.Fatalf("mismatched manifest accepted: %+v", j)
	}
	if rows, _ := a.readApps(); rows[0].Busy {
		t.Fatal("app left busy")
	}
}
