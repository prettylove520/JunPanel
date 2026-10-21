package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests drive the real Docker engine and are skipped unless
// JUNPANEL_DOCKER_TEST=1 (they pull small images and create containers).
func dockerAgent(t *testing.T) *agent {
	t.Helper()
	if os.Getenv("JUNPANEL_DOCKER_TEST") != "1" {
		t.Skip("set JUNPANEL_DOCKER_TEST=1 to run against a real Docker engine")
	}
	a := testAgent(t)
	// The agent normally runs as root with HOME=/root; tests run as whoever invokes them.
	a.stream = func(ctx context.Context, dir string, out io.Writer, _ []string, name string, args ...string) error {
		return dockerCLI(ctx, dir, out, name, args...)
	}
	return a
}
func dockerCLI(ctx context.Context, dir string, out io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, out, out
	return cmd.Run()
}
func freePort(t *testing.T) int {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
func waitJob(t *testing.T, a *agent, body []byte, within time.Duration) job {
	t.Helper()
	var started job
	if e := json.Unmarshal(body, &started); e != nil || started.ID == "" {
		t.Fatalf("not a job: %s", body)
	}
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		for _, j := range a.jobs.list() {
			if j.ID == started.ID && j.State != "running" {
				if j.State != "success" {
					t.Fatalf("job %q failed: %s\n%s", j.Title, j.Error, j.Output)
				}
				return j
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("job %s did not finish", started.ID)
	return job{}
}
func installApp(t *testing.T, a *agent, id, template string) appRecord {
	t.Helper()
	h := a.routes()
	r := request(h, "POST", "/apps", `{"id":"`+id+`","name":"`+id+`","template":"`+template+`","port":`+itoa(freePort(t))+`}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("install: %d %s", r.Code, r.Body.String())
	}
	waitJob(t, a, r.Body.Bytes(), 8*time.Minute)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		dockerCLI(ctx, "", io.Discard, "docker", "compose", "--project-name", "junpanel-"+id, "down", "--volumes", "--timeout", "3")
	})
	rows, _ := a.readApps()
	for _, rec := range rows {
		if rec.ID == id {
			return rec
		}
	}
	t.Fatal("app record missing")
	return appRecord{}
}
func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
func (a *agent) running(t *testing.T, rec appRecord, service string) container {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		rows, _ := a.docker.list()
		for _, c := range rows {
			if c.Project == "junpanel-"+rec.ID && c.Service == service && c.State == "running" {
				return c
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%s/%s not running", rec.ID, service)
	return container{}
}
func redisCommand(t *testing.T, a *agent, rec appRecord, args ...string) string {
	t.Helper()
	c := a.running(t, rec, "app")
	var last error
	for i := 0; i < 30; i++ {
		out, e := a.docker.exec(c.ID, append([]string{"redis-cli"}, args...), []string{"REDISCLI_AUTH=" + rec.Password})
		if e == nil {
			return strings.TrimSpace(out)
		}
		last = e
		time.Sleep(time.Second)
	}
	t.Fatalf("redis-cli %s: %v", args[0], last)
	return ""
}

func TestDockerAppBackupAndRestore(t *testing.T) {
	a := dockerAgent(t)
	h := a.routes()
	rec := installApp(t, a, "bk-redis", "redis")
	redisCommand(t, a, rec, "SET", "before", "kept")
	redisCommand(t, a, rec, "SET", "bulk", strings.Repeat("x", 100000))

	r := request(h, "POST", "/backups", `{"type":"app","target":"bk-redis"}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("backup: %d %s", r.Code, r.Body.String())
	}
	waitJob(t, a, r.Body.Bytes(), 5*time.Minute)
	if got := redisCommand(t, a, rec, "GET", "before"); got != "kept" {
		t.Fatalf("app did not come back after backup: %q", got)
	}
	var list struct{ Backups []backupMeta }
	json.Unmarshal(request(h, "GET", "/backups", "", nil, nil).Body.Bytes(), &list)
	if len(list.Backups) != 1 || list.Backups[0].Type != "app" || list.Backups[0].Size == 0 || len(list.Backups[0].SHA256) != 64 {
		t.Fatalf("backup listing: %+v", list.Backups)
	}
	backup := list.Backups[0]

	redisCommand(t, a, rec, "SET", "after", "lost")
	redisCommand(t, a, rec, "DEL", "before")

	if r = request(h, "POST", "/backups/"+backup.ID+"/restore", `{}`, nil, nil); r.Code != 400 {
		t.Fatal("restore without confirmation accepted")
	}
	r = request(h, "POST", "/backups/"+backup.ID+"/restore", `{"confirm":true}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("restore: %d %s", r.Code, r.Body.String())
	}
	done := waitJob(t, a, r.Body.Bytes(), 8*time.Minute)
	if !strings.Contains(done.Output, "恢复前备份：b_") {
		t.Fatalf("no safety backup reported:\n%s", done.Output)
	}
	if got := redisCommand(t, a, rec, "GET", "before"); got != "kept" {
		t.Fatalf("restored data missing: %q", got)
	}
	if got := redisCommand(t, a, rec, "EXISTS", "after"); got != "0" {
		t.Fatalf("data written after the backup survived the restore: %q", got)
	}
	if got := redisCommand(t, a, rec, "STRLEN", "bulk"); got != "100000" {
		t.Fatalf("large value damaged: %q", got)
	}
	json.Unmarshal(request(h, "GET", "/backups", "", nil, nil).Body.Bytes(), &list)
	if len(list.Backups) != 2 {
		t.Fatalf("expected the safety backup to be listed, got %d", len(list.Backups))
	}
	rows, _ := a.readApps()
	if rows[0].Busy {
		t.Fatal("app left busy")
	}

	// A modified backup must be refused, and the app left alone.
	path := filepath.Join(a.backupDir(), backup.File)
	f, _ := os.OpenFile(path, os.O_WRONLY, 0)
	f.WriteAt([]byte{0xff}, 40)
	f.Close()
	if r = request(h, "POST", "/backups/"+backup.ID+"/restore", `{"confirm":true}`, nil, nil); r.Code != 202 {
		t.Fatal(r.Body.String())
	}
	var j job
	json.Unmarshal(r.Body.Bytes(), &j)
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		for _, x := range a.jobs.list() {
			if x.ID == j.ID && x.State == "failed" {
				if !strings.Contains(x.Error, "校验失败") {
					t.Fatalf("unexpected failure: %s", x.Error)
				}
				if got := redisCommand(t, a, rec, "GET", "before"); got != "kept" {
					t.Fatal("corrupt restore touched the app")
				}
				return
			}
		}
	}
	t.Fatal("corrupt backup was not refused")
}

func TestDockerDatabaseBackupStreams(t *testing.T) {
	a := dockerAgent(t)
	h := a.routes()
	rec := installApp(t, a, "bk-pg", "postgres")
	c := a.running(t, rec, "app")
	env := []string{"PGPASSWORD=" + rec.Password}
	var e error
	for i := 0; i < 60; i++ {
		_, e = a.docker.exec(c.ID, []string{"psql", "--username", "junpanel", "--dbname", "junpanel", "--command", "SELECT 1"}, env)
		if e == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if e != nil {
		t.Fatal(e)
	}
	// About 12 MB of dump: beyond the old 8 MiB response limit.
	if _, e = a.docker.exec(c.ID, []string{"psql", "--username", "junpanel", "--dbname", "junpanel", "--set", "ON_ERROR_STOP=1", "--command", "CREATE TABLE big AS SELECT g AS id, md5(g::text) || md5((g+1)::text) || md5((g+2)::text) AS payload FROM generate_series(1,150000) g"}, env); e != nil {
		t.Fatal(e)
	}
	r := request(h, "POST", "/backups", `{"type":"database","target":"bk-pg","database":"junpanel"}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	waitJob(t, a, r.Body.Bytes(), 5*time.Minute)
	var list struct{ Backups []backupMeta }
	json.Unmarshal(request(h, "GET", "/backups", "", nil, nil).Body.Bytes(), &list)
	if len(list.Backups) != 1 || list.Backups[0].Type != "database" {
		t.Fatalf("listing: %+v", list.Backups)
	}
	dl := request(h, "GET", "/backups/"+list.Backups[0].ID+"/download", "", nil, nil)
	if dl.Code != 200 || !strings.Contains(dl.Header().Get("Content-Disposition"), ".sql.gz") {
		t.Fatalf("download: %d %v", dl.Code, dl.Header())
	}
	gz, e := gzip.NewReader(dl.Body)
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := io.ReadAll(gz)
	if len(sql) < 9<<20 || !strings.Contains(string(sql), "CREATE TABLE public.big") || !strings.Contains(string(sql), "PostgreSQL database dump complete") {
		t.Fatalf("dump incomplete: %d bytes", len(sql))
	}
	if r = request(h, "POST", "/backups/"+list.Backups[0].ID+"/restore", `{"confirm":true}`, nil, nil); r.Code != 400 {
		t.Fatal("database dump restore should point to native tools")
	}
	if r = request(h, "POST", "/backups", `{"type":"database","target":"bk-pg","database":"junpanel; DROP"}`, nil, nil); r.Code != 400 {
		t.Fatal("bad database name accepted")
	}
}

// WordPress has two data directories in two containers; it pulls about 700 MB,
// so it also needs JUNPANEL_DOCKER_TEST_LARGE=1.
func TestDockerWordPressBackupAndRestore(t *testing.T) {
	a := dockerAgent(t)
	if os.Getenv("JUNPANEL_DOCKER_TEST_LARGE") != "1" {
		t.Skip("set JUNPANEL_DOCKER_TEST_LARGE=1 for the multi-container WordPress case")
	}
	h := a.routes()
	rec := installApp(t, a, "bk-wp", "wordpress")
	web, db := a.running(t, rec, "app"), a.running(t, rec, "database")
	sql := func(query string) string {
		t.Helper()
		var last error
		for i := 0; i < 90; i++ {
			out, e := a.docker.exec(db.ID, []string{"mariadb", "--user=root", "--batch", "--skip-column-names", "--database=wordpress", "--execute=" + query}, []string{"MYSQL_PWD=" + rec.Password})
			if e == nil {
				return strings.TrimSpace(out)
			}
			last = e
			time.Sleep(time.Second)
		}
		t.Fatalf("sql: %v", last)
		return ""
	}
	sql("CREATE TABLE marker (v VARCHAR(20)); INSERT INTO marker VALUES ('kept')")
	if _, e := a.docker.exec(web.ID, []string{"sh", "-c", "echo kept > /var/www/html/marker.txt"}, nil); e != nil {
		t.Fatal(e)
	}
	r := request(h, "POST", "/backups", `{"type":"app","target":"bk-wp"}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	waitJob(t, a, r.Body.Bytes(), 10*time.Minute)
	var list struct{ Backups []backupMeta }
	json.Unmarshal(request(h, "GET", "/backups", "", nil, nil).Body.Bytes(), &list)
	if len(list.Backups) != 1 {
		t.Fatalf("listing: %+v", list.Backups)
	}
	web, db = a.running(t, rec, "app"), a.running(t, rec, "database")
	sql("DROP TABLE marker")
	a.docker.exec(web.ID, []string{"rm", "/var/www/html/marker.txt"}, nil)
	r = request(h, "POST", "/backups/"+list.Backups[0].ID+"/restore", `{"confirm":true}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	waitJob(t, a, r.Body.Bytes(), 10*time.Minute)
	web, db = a.running(t, rec, "app"), a.running(t, rec, "database")
	if got := sql("SELECT v FROM marker"); got != "kept" {
		t.Fatalf("database not restored: %q", got)
	}
	out, e := a.docker.exec(web.ID, []string{"cat", "/var/www/html/marker.txt"}, nil)
	if e != nil || strings.TrimSpace(out) != "kept" {
		t.Fatalf("site files not restored: %q %v", out, e)
	}
}

func TestDockerScheduledBackupAndRestart(t *testing.T) {
	a := dockerAgent(t)
	h := a.routes()
	rec := installApp(t, a, "sc-redis", "redis")
	redisCommand(t, a, rec, "SET", "scheduled", "kept")

	backup := createSchedule(t, h, map[string]any{"name": "缓存备份", "action": "backup", "type": "app", "target": "sc-redis", "keep": 1, "every": "daily", "at": "03:00"})
	for i := 0; i < 2; i++ {
		if r := request(h, "POST", "/schedules/"+backup.ID+"/run", `{}`, nil, nil); r.Code != 202 {
			t.Fatalf("run %d: %d %s", i, r.Code, r.Body.String())
		}
		done := waitSchedule(t, a, backup.ID)
		if done.Last.Status != "success" {
			t.Fatalf("run %d: %+v", i, done.Last)
		}
		time.Sleep(20 * time.Millisecond)
	}
	backups := listBackups(t, h)
	if len(backups) != 1 || backups[0].Schedule != backup.ID || backups[0].Type != "app" {
		t.Fatalf("retention with a real app: %+v", backups)
	}
	if got := redisCommand(t, a, rec, "GET", "scheduled"); got != "kept" {
		t.Fatalf("data after scheduled backups: %q", got)
	}

	started := func() string {
		out, e := exec.Command("docker", "inspect", "--format", "{{.State.StartedAt}}", a.running(t, rec, "app").ID).Output()
		if e != nil {
			t.Fatal(e)
		}
		return strings.TrimSpace(string(out))
	}
	before := started()
	time.Sleep(1100 * time.Millisecond)
	restart := createSchedule(t, h, map[string]any{"name": "缓存重启", "action": "restart", "target": "sc-redis", "every": "interval", "hours": 24})
	if r := request(h, "POST", "/schedules/"+restart.ID+"/run", `{}`, nil, nil); r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	if done := waitSchedule(t, a, restart.ID); done.Last.Status != "success" {
		t.Fatalf("%+v", done.Last)
	}
	if after := started(); after == before {
		t.Fatal("container was not restarted")
	}
	if got := redisCommand(t, a, rec, "GET", "scheduled"); got != "kept" {
		t.Fatalf("data after restart: %q", got)
	}
}

func sqlLines(t *testing.T, a *agent, rec appRecord, db, query string) string {
	t.Helper()
	c := a.running(t, rec, "app")
	args, env, e := databaseArgs(rec, "postgres", db, query)
	if e != nil {
		t.Fatal(e)
	}
	text, e := a.docker.exec(c.ID, args, env)
	if e != nil {
		t.Fatalf("%s: %v", query, e)
	}
	return strings.TrimSpace(text)
}

// A database backup restores into an emptied database, after the current one has
// been backed up, and can also be restored under another name.
func TestDockerDatabaseRestore(t *testing.T) {
	a := dockerAgent(t)
	h := a.routes()
	rec := installApp(t, a, "pr"+randomID(4), "postgres")
	// The image runs a temporary server for its init scripts and then restarts, so
	// wait for queries to keep working rather than for the first answer.
	ok := 0
	args, env, _ := databaseArgs(rec, "postgres", "junpanel", "SELECT 1")
	for i := 0; i < 90 && ok < 2; i++ {
		if _, e := a.docker.exec(a.running(t, rec, "app").ID, args, env); e == nil {
			ok++
			time.Sleep(3 * time.Second)
			continue
		}
		ok = 0
		time.Sleep(time.Second)
	}
	if ok < 2 {
		t.Fatal("postgres did not start")
	}
	sqlLines(t, a, rec, "junpanel", "CREATE TABLE notes (body text); INSERT INTO notes VALUES ('before');")
	r := request(h, "POST", "/backups", `{"type":"database","target":"`+rec.ID+`","database":"junpanel"}`, nil, nil)
	waitJob(t, a, r.Body.Bytes(), 2*time.Minute)
	original := listBackups(t, h)[0]
	sqlLines(t, a, rec, "junpanel", "INSERT INTO notes VALUES ('after'); CREATE TABLE extra (x int);")

	r = request(h, "POST", "/backups/"+original.ID+"/restore", `{"confirm":true}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("restore: %d %s", r.Code, r.Body.String())
	}
	j := waitJob(t, a, r.Body.Bytes(), 3*time.Minute)
	if got := sqlLines(t, a, rec, "junpanel", "SELECT body FROM notes ORDER BY body"); got != "body\nbefore" {
		t.Fatalf("after restore: %q\n%s", got, j.Output)
	}
	if got := sqlLines(t, a, rec, "junpanel", "SELECT count(*) FROM pg_tables WHERE tablename = 'extra'"); got != "count\n0" {
		t.Fatalf("table created after the backup survived: %q", got)
	}
	var pre backupMeta
	for _, b := range listBackups(t, h) {
		if b.Note == "恢复前自动备份" {
			pre = b
		}
	}
	if pre.ID == "" {
		t.Fatal("no backup of the database before it was replaced")
	}
	// The pre-restore backup holds the state that was replaced.
	r = request(h, "POST", "/backups/"+pre.ID+"/restore", `{"confirm":true,"database":"copy_of_before"}`, nil, nil)
	waitJob(t, a, r.Body.Bytes(), 3*time.Minute)
	if got := sqlLines(t, a, rec, "copy_of_before", "SELECT body FROM notes ORDER BY body"); got != "body\nafter\nbefore" {
		t.Fatalf("restore under another name: %q", got)
	}
	if got := sqlLines(t, a, rec, "junpanel", "SELECT body FROM notes ORDER BY body"); got != "body\nbefore" {
		t.Fatalf("restoring elsewhere touched the original: %q", got)
	}
	// A damaged file is refused before anything is changed.
	path := filepath.Join(a.backupDir(), original.File)
	raw, _ := os.ReadFile(path)
	raw[len(raw)/2] ^= 0xff
	os.WriteFile(path, raw, 0600)
	before := len(listBackups(t, h))
	r = request(h, "POST", "/backups/"+original.ID+"/restore", `{"confirm":true}`, nil, nil)
	j = waitOutcome(t, a, r.Body.Bytes())
	if j.State != "failed" || !strings.Contains(j.Error, "校验失败") || len(listBackups(t, h)) != before {
		t.Fatalf("damaged backup: %s %s, backups %d -> %d", j.State, j.Error, before, len(listBackups(t, h)))
	}
	if got := sqlLines(t, a, rec, "junpanel", "SELECT body FROM notes ORDER BY body"); got != "body\nbefore" {
		t.Fatalf("damaged backup changed the database: %q", got)
	}
	// A dump that fails to load says so and names the way back.
	bad, e := a.writeBackup(backupMeta{Type: "database", Target: rec.ID, Name: rec.Name, Database: "junpanel"}, "sql.gz", func(w io.Writer) error {
		_, e := w.Write([]byte("CREATE TABLE ok (x int);\nTHIS IS NOT SQL;\n"))
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	r = request(h, "POST", "/backups/"+bad.ID+"/restore", `{"confirm":true,"database":"scratch"}`, nil, nil)
	j = waitOutcome(t, a, r.Body.Bytes())
	if j.State != "failed" || !strings.Contains(j.Error, "导入失败") {
		t.Fatalf("bad dump: %s %s", j.State, j.Error)
	}
	if got := sqlLines(t, a, rec, "scratch", "SELECT count(*) FROM pg_tables WHERE tablename = 'ok'"); got != "count\n0" {
		t.Fatalf("a failed load left part of the dump behind: %q", got)
	}
	rows, _ := a.readApps()
	for _, row := range rows {
		if row.ID == rec.ID && row.Busy {
			t.Fatal("app left busy")
		}
	}
}
