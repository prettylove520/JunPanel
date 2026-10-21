package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// restoreDatabase loads an SQL backup into a managed database. The target is dropped
// and created again, so the dump lands in an empty database; when the target already
// exists it is backed up first. The dump reaches the container as a file and is read
// by the database's own client, so no shell is involved.
func (a *agent) restoreDatabase(ctx context.Context, out io.Writer, meta backupMeta, target string) error {
	record, kind, e := a.databaseRecord(meta.Target)
	if e != nil {
		return e
	}
	if kind == "redis" {
		return bad(400, "Redis 请恢复整个应用的备份")
	}
	if !dbIdentifier.MatchString(target) || strings.EqualFold(target, "postgres") || strings.EqualFold(target, "mysql") || strings.EqualFold(target, "information_schema") || strings.EqualFold(target, "performance_schema") || strings.EqualFold(target, "sys") {
		return bad(400, "目标数据库名无效，也不能是系统数据库")
	}
	c, e := a.databaseContainer(record, kind)
	if e != nil {
		return e
	}
	// Administrative statements run from the system database, which always exists.
	admin := "mysql"
	if kind == "postgres" {
		admin = "postgres"
	}
	run := func(db, query string) (string, error) {
		args, env, e := databaseArgs(record, kind, db, query)
		if e != nil {
			return "", e
		}
		return a.docker.exec(c.ID, args, env)
	}
	// Check and unpack the backup first, so a damaged file changes nothing.
	f, e := a.openBackup(meta)
	if e != nil {
		return e
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return bad(409, "备份文件无法解压")
	}
	plain, e := os.CreateTemp(a.backupDir(), ".restore-*.sql")
	if e != nil {
		return e
	}
	defer os.Remove(plain.Name())
	defer plain.Close()
	size, e := io.Copy(plain, gz)
	if e != nil {
		return bad(409, "备份文件解压失败，可能已损坏")
	}
	if size == 0 {
		return bad(400, "备份内容为空")
	}
	if _, e = plain.Seek(0, io.SeekStart); e != nil {
		return e
	}
	check := "SHOW DATABASES LIKE '" + target + "'"
	if kind == "postgres" {
		check = "SELECT datname FROM pg_database WHERE datname = '" + target + "'"
	}
	existing, e := run(admin, check)
	if e != nil {
		return e
	}
	found := false
	for _, line := range strings.Split(existing, "\n") {
		found = found || strings.TrimSpace(line) == target
	}
	if found {
		fmt.Fprintf(out, "先备份当前的数据库 %s…\n", target)
		saved, e := a.backupDatabase(ctx, out, meta.Target, target, backupTag{Note: "恢复前自动备份"})
		if e != nil {
			return bad(502, "恢复前备份失败，数据库没有改动："+e.Error())
		}
		fmt.Fprintf(out, "已创建恢复前备份 %s。\n", saved.ID)
	}

	name := "junpanel-restore-" + randomID(12) + ".sql"
	fmt.Fprintf(out, "正在把 %s 的导出（%d 字节）传入数据库容器…\n", meta.Database, size)
	pr, pw := io.Pipe()
	go func() {
		tw := tar.NewWriter(pw)
		e := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: size, ModTime: time.Now()})
		if e == nil {
			_, e = io.Copy(tw, plain)
		}
		if e == nil {
			e = tw.Close()
		}
		pw.CloseWithError(e)
	}()
	if e = a.docker.putArchive(ctx, c.ID, "/tmp", pr); e != nil {
		pr.CloseWithError(e)
		return e
	}
	inside := filepath.Join("/tmp", name)
	defer a.docker.exec(c.ID, []string{"rm", "-f", inside}, nil)

	fmt.Fprintf(out, "正在重建数据库 %s…\n", target)
	drop, create := "DROP DATABASE IF EXISTS `"+target+"`", "CREATE DATABASE `"+target+"` CHARACTER SET utf8mb4"
	if kind == "postgres" {
		drop, create = `DROP DATABASE IF EXISTS "`+target+`" WITH (FORCE)`, `CREATE DATABASE "`+target+`"`
	}
	if _, e = run(admin, drop); e != nil {
		return e
	}
	if _, e = run(admin, create); e != nil {
		return bad(502, "重建数据库失败："+e.Error())
	}

	fmt.Fprintln(out, "正在导入…")
	var args, env []string
	switch kind {
	case "postgres":
		args = []string{"psql", "--no-psqlrc", "--quiet", "--username", "junpanel", "--dbname", target, "--set", "ON_ERROR_STOP=1", "--single-transaction", "--file", inside}
		env = []string{"PGPASSWORD=" + record.Password}
	default:
		client := "mysql"
		if kind == "mariadb" {
			client = "mariadb"
		}
		args = []string{client, "--user=root", "--database=" + target, "--execute=source " + inside}
		env = []string{"MYSQL_PWD=" + record.Password}
	}
	lines := &countWriter{}
	stderr, e := a.docker.execStream(ctx, c.ID, args, env, lines)
	if e != nil {
		return bad(502, "导入失败，数据库 "+target+" 可能不完整；可以用恢复前备份再恢复一次。"+truncate(stderr, 1500))
	}
	if stderr != "" {
		fmt.Fprintf(out, "原生工具提示：%s\n", truncate(stderr, 800))
	}
	fmt.Fprintf(out, "已恢复到数据库 %s。\n", target)
	return nil
}
