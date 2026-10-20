package main

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"regexp"
	"strings"
)

func (a *agent) databaseRecord(id string) (appRecord, string, error) {
	rows, e := a.readApps()
	if e != nil {
		return appRecord{}, "", e
	}
	for _, r := range rows {
		if r.ID != id || r.Removed {
			continue
		}
		t, _ := findTemplate(r.Template)
		kind := t.Database
		if r.Template == "wordpress" {
			kind = "mariadb"
		}
		if kind != "" {
			return r, kind, nil
		}
	}
	return appRecord{}, "", bad(404, "这是面板未管理的数据库实例")
}
func (a *agent) databaseContainer(record appRecord, kind string) (container, error) {
	rows, e := a.docker.list()
	if e != nil {
		return container{}, e
	}
	service := "app"
	if record.Template == "wordpress" {
		service = "database"
	}
	for _, c := range rows {
		if c.Project == "junpanel-"+record.ID && c.Service == service {
			if c.State != "running" {
				return container{}, bad(409, "数据库容器尚未运行")
			}
			if c.Protected {
				return container{}, bad(403, "数据库容器受保护")
			}
			return c, nil
		}
	}
	return container{}, bad(404, "数据库容器未找到，请先部署应用")
}
func (a *agent) databaseList(w http.ResponseWriter, r *http.Request) error {
	rows, e := a.readApps()
	if e != nil {
		return e
	}
	out := []map[string]any{}
	for _, record := range rows {
		if record.Removed {
			continue
		}
		t, _ := findTemplate(record.Template)
		kind := t.Database
		port := record.Port
		db := "junpanel"
		if record.Template == "wordpress" {
			kind = "mariadb"
			port = 0
			db = "wordpress"
		}
		if kind == "" {
			continue
		}
		out = append(out, map[string]any{"id": record.ID, "name": record.Name, "kind": kind, "port": port, "database": db, "busy": record.Busy})
	}
	send(w, 200, out)
	return nil
}
func (d *dockerClient) exec(id string, args, env []string) (string, error) {
	if !containerID.MatchString(id) {
		return "", bad(400, "容器 ID 无效")
	}
	raw, e := d.call("POST", "/containers/"+id+"/exec", map[string]any{"AttachStdout": true, "AttachStderr": true, "Tty": false, "Cmd": args, "Env": env})
	if e != nil {
		return "", e
	}
	var created struct{ Id string }
	if e = decodeObject(raw, &created); e != nil {
		return "", e
	}
	if !containerID.MatchString(created.Id) {
		return "", bad(502, "Docker exec 标识无效")
	}
	raw, e = d.call("POST", "/exec/"+created.Id+"/start", map[string]any{"Detach": false, "Tty": false})
	if e != nil {
		return "", e
	}
	text, stderr, e := decodeExecOutput(raw)
	if e != nil {
		return "", e
	}
	raw, e = d.call("GET", "/exec/"+created.Id+"/json", nil)
	if e != nil {
		return "", e
	}
	var status struct {
		ExitCode int
		Running  bool
	}
	if e = decodeObject(raw, &status); e != nil {
		return "", e
	}
	if status.Running {
		return "", bad(409, "数据库命令仍在执行，请通过 SSH 检查")
	}
	if status.ExitCode != 0 {
		if stderr == "" {
			stderr = text
		}
		return "", bad(400, "数据库命令失败："+truncate(cleanText(stderr), 3000))
	}
	return text, nil
}

// Native exports must preserve stdout byte-for-byte and keep stderr separate.
func decodeExecOutput(raw []byte) (string, string, error) {
	var stdout, stderr bytes.Buffer
	for len(raw) > 0 {
		if len(raw) < 8 || raw[0] != 1 && raw[0] != 2 || !bytes.Equal(raw[1:4], []byte{0, 0, 0}) {
			return "", "", bad(502, "数据库输出流格式无效")
		}
		channel := raw[0]
		size := int(binary.BigEndian.Uint32(raw[4:8]))
		raw = raw[8:]
		if size > len(raw) {
			return "", "", bad(502, "数据库输出流不完整")
		}
		if channel == 1 {
			stdout.Write(raw[:size])
		} else {
			stderr.Write(raw[:size])
		}
		raw = raw[size:]
	}
	return stdout.String(), stderr.String(), nil
}

var dbIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

func databaseArgs(record appRecord, kind, db, query string) ([]string, []string, error) {
	if !dbIdentifier.MatchString(db) {
		return nil, nil, bad(400, "数据库名需为字母、数字与下划线")
	}
	switch kind {
	case "postgres":
		return []string{"psql", "--no-psqlrc", "--csv", "--username", "junpanel", "--dbname", db, "--set", "ON_ERROR_STOP=1", "--command", query}, []string{"PGPASSWORD=" + record.Password}, nil
	case "mysql", "mariadb":
		binary := "mysql"
		if kind == "mariadb" {
			binary = "mariadb"
		}
		return []string{binary, "--user=root", "--batch", "--raw", "--database=" + db, "--execute=" + query}, []string{"MYSQL_PWD=" + record.Password}, nil
	case "redis":
		return nil, nil, bad(400, "Redis 仅提供只读状态命令")
	}
	return nil, nil, bad(400, "数据库类型无效")
}
func (a *agent) databaseQuery(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Database string `json:"database"`
		SQL      string `json:"sql"`
		Command  string `json:"command"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	record, kind, e := a.databaseRecord(r.PathValue("id"))
	if e != nil {
		return e
	}
	c, e := a.databaseContainer(record, kind)
	if e != nil {
		return e
	}
	var args, env []string
	if kind == "redis" {
		if req.Command != "INFO" && req.Command != "DBSIZE" && req.Command != "PING" {
			return bad(400, "Redis 仅支持 INFO、DBSIZE、PING")
		}
		args = []string{"redis-cli", req.Command}
		env = []string{"REDISCLI_AUTH=" + record.Password}
	} else {
		if len(req.SQL) < 1 || len(req.SQL) > 50000 || strings.ContainsRune(req.SQL, '\x00') {
			return bad(400, "SQL 需为 1–50000 字符")
		}
		args, env, e = databaseArgs(record, kind, req.Database, req.SQL)
		if e != nil {
			return e
		}
	}
	text, e := a.docker.exec(c.ID, args, env)
	if e != nil {
		return e
	}
	send(w, 200, map[string]string{"text": text})
	return nil
}
func (a *agent) databaseCreate(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name string `json:"name"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if !dbIdentifier.MatchString(req.Name) {
		return bad(400, "库名仅允许字母、数字与下划线")
	}
	record, kind, e := a.databaseRecord(r.PathValue("id"))
	if e != nil {
		return e
	}
	if kind == "redis" {
		return bad(400, "Redis 不使用此建库操作")
	}
	c, e := a.databaseContainer(record, kind)
	if e != nil {
		return e
	}
	db := "junpanel"
	query := "CREATE DATABASE `" + req.Name + "` CHARACTER SET utf8mb4"
	if kind == "postgres" {
		db = "postgres"
		query = "CREATE DATABASE \"" + req.Name + "\""
	} else if record.Template == "wordpress" {
		db = "wordpress"
	}
	args, env, e := databaseArgs(record, kind, db, query)
	if e != nil {
		return e
	}
	text, e := a.docker.exec(c.ID, args, env)
	if e != nil {
		return e
	}
	send(w, 200, map[string]string{"text": text, "database": req.Name})
	return nil
}
