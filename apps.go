package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type appTemplate struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Summary     string `json:"summary"`
	Image       string `json:"image"`
	Port        int    `json:"port"`
	ServicePort int    `json:"service_port"`
	Database    string `json:"database,omitempty"`
	Color       string `json:"color"`
	Note        string `json:"note,omitempty"`
	Backup      bool   `json:"backup"`
}

var appCatalog = []appTemplate{
	{"wordpress", "WordPress", "网站应用", "内容网站与博客，包含独立 MariaDB 服务", "wordpress:php8.3-apache", 8080, 80, "", "#6fa8ff", "", false},
	{"gitea", "Gitea", "开发工具", "轻量的私有 Git 服务，使用内置 SQLite", "docker.gitea.com/gitea:1", 3000, 3000, "", "#9bcf9b", "", false},
	{"kuma", "Uptime Kuma", "运维工具", "可视化服务可用性监控", "louislam/uptime-kuma:2", 3001, 3001, "", "#70d9b9", "", false},
	{"postgres", "PostgreSQL", "数据库", "PostgreSQL 17，可在面板中查询与导出", "postgres:17-alpine", 5432, 5432, "postgres", "#87aef0", "", false},
	{"mysql", "MySQL", "数据库", "MySQL 8.4，可在面板中查询与导出", "mysql:8.4", 3306, 3306, "mysql", "#efbe7c", "", false},
	{"redis", "Redis", "数据库", "Redis 7，仅启用密码认证的本机端口", "redis:7-alpine", 6379, 6379, "redis", "#e88e91", "", false},
	{"vaultwarden", "Vaultwarden", "效率工具", "兼容 Bitwarden 客户端的轻量密码管理器", "vaultwarden/server:latest", 8000, 80, "", "#175ddc", "网页端需要 HTTPS 才能使用，请先绑定域名。默认开放注册，建议注册自己的账号后关闭开放注册。", false},
	{"memos", "Memos", "效率工具", "轻量的自托管笔记与备忘录", "neosmemo/memos:stable", 5230, 5230, "", "#2f855a", "第一个注册的账号成为管理员。", false},
	{"n8n", "n8n", "效率工具", "可视化的工作流自动化平台", "docker.n8n.io/n8nio/n8n:latest", 5678, 5678, "", "#ea4b71", "默认只允许通过 HTTPS 登录，请先绑定域名再访问。", false},
	{"openwebui", "Open WebUI", "AI 工具", "连接本地或在线大模型的聊天界面", "ghcr.io/open-webui/open-webui:main", 3080, 8080, "", "#111827", "镜像较大（数 GB），首次部署需要较长时间。第一个注册的账号成为管理员。", false},
	{"ittools", "IT-Tools", "开发工具", "开发者常用的在线小工具合集，无需保存数据", "corentinth/it-tools:latest", 8090, 80, "", "#18a058", "", false},
	{"excalidraw", "Excalidraw", "效率工具", "手绘风格的在线白板，画布保存在浏览器中", "excalidraw/excalidraw:latest", 8091, 80, "", "#6965db", "画布只保存在访问者的浏览器里，服务器不保存数据。", false},
}

func init() {
	for i := range appCatalog {
		appCatalog[i].Backup = len(appData[appCatalog[i].Key]) > 0
	}
}

func findTemplate(key string) (appTemplate, bool) {
	for _, t := range appCatalog {
		if t.Key == key {
			return t, true
		}
	}
	return appTemplate{}, false
}

type appRecord struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Template   string    `json:"template"`
	Port       int       `json:"port"`
	Password   string    `json:"password"`
	DBPassword string    `json:"db_password,omitempty"`
	Created    time.Time `json:"created"`
	Removed    bool      `json:"removed"`
	Busy       bool      `json:"busy"`
	LastError  string    `json:"last_error,omitempty"`
}

func (a *agent) readApps() ([]appRecord, error) {
	rows := []appRecord{}
	e := loadJSON(filepath.Join(a.cfg.State, "apps.json"), &rows)
	if os.IsNotExist(e) {
		e = nil
	}
	return rows, e
}
func (a *agent) storeApps(rows []appRecord) error {
	return atomicJSON(filepath.Join(a.cfg.State, "apps.json"), rows, 0600)
}

func (a *agent) appCredentials(w http.ResponseWriter, r *http.Request) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rows, e := a.readApps()
	if e != nil {
		return e
	}
	for _, record := range rows {
		if record.ID != r.PathValue("id") {
			continue
		}
		values := map[string]any{"host": "127.0.0.1", "port": record.Port, "user": "", "database": "", "password": "", "note": "应用首次访问时自行创建管理员账号；界面端口仅监听本机。"}
		switch record.Template {
		case "wordpress":
			values["host"] = "database（应用项目内部网络）"
			values["port"] = 3306
			values["user"] = "wordpress"
			values["database"] = "wordpress"
			values["password"] = record.DBPassword
			values["note"] = "WordPress 的数据库只在项目内部开放。网站后台管理员需在首次访问网站时创建。"
		case "postgres":
			values["user"] = "junpanel"
			values["database"] = "junpanel"
			values["password"] = record.Password
			values["note"] = "通过 SSH 转发本机端口后，可使用数据库客户端连接。"
		case "mysql":
			values["user"] = "root"
			values["database"] = "junpanel"
			values["password"] = record.Password
			values["note"] = "此处为数据库管理员凭据，请妥善保管。"
		case "redis":
			values["user"] = "default"
			values["password"] = record.Password
			values["note"] = "通过 SSH 转发本机端口后，使用此密码连接。"
		}
		send(w, 200, values)
		return nil
	}
	return bad(404, "应用不存在")
}
func publicApps(rows []appRecord) []map[string]any {
	out := []map[string]any{}
	for _, r := range rows {
		template, _ := findTemplate(r.Template)
		out = append(out, map[string]any{"id": r.ID, "name": r.Name, "template": r.Template, "port": r.Port, "created": r.Created, "removed": r.Removed, "busy": r.Busy, "last_error": r.LastError, "project": "junpanel-" + r.ID, "category": template.Category, "image": template.Image, "database": template.Database})
	}
	return out
}
func composeFor(r appRecord) ([]byte, error) {
	t, ok := findTemplate(r.Template)
	if !ok {
		return nil, bad(400, "未知应用模板")
	}
	name := "junpanel-" + r.ID
	service := map[string]any{"image": t.Image, "restart": "unless-stopped", "ports": []string{fmt.Sprintf("127.0.0.1:%d:%d", r.Port, t.ServicePort)}, "labels": map[string]string{"junpanel.managed": "true", "junpanel.app": r.ID}}
	volumes := map[string]any{}
	services := map[string]any{"app": service}
	switch r.Template {
	case "wordpress":
		service["environment"] = map[string]string{"WORDPRESS_DB_HOST": "database:3306", "WORDPRESS_DB_NAME": "wordpress", "WORDPRESS_DB_USER": "wordpress", "WORDPRESS_DB_PASSWORD": r.DBPassword}
		service["volumes"] = []string{"web:/var/www/html"}
		service["depends_on"] = map[string]any{"database": map[string]string{"condition": "service_healthy"}}
		services["database"] = map[string]any{"image": "mariadb:11.4", "restart": "unless-stopped", "environment": map[string]string{"MARIADB_DATABASE": "wordpress", "MARIADB_USER": "wordpress", "MARIADB_PASSWORD": r.DBPassword, "MARIADB_ROOT_PASSWORD": r.Password}, "volumes": []string{"database:/var/lib/mysql"}, "healthcheck": map[string]any{"test": []string{"CMD", "healthcheck.sh", "--connect", "--innodb_initialized"}, "interval": "10s", "timeout": "5s", "retries": 12}, "labels": map[string]string{"junpanel.managed": "true", "junpanel.app": r.ID}}
		volumes["web"] = map[string]any{}
		volumes["database"] = map[string]any{}
	case "gitea":
		service["volumes"] = []string{"data:/data"}
		service["environment"] = map[string]string{"GITEA__server__DISABLE_SSH": "true"}
		volumes["data"] = map[string]any{}
	case "kuma":
		service["volumes"] = []string{"data:/app/data"}
		volumes["data"] = map[string]any{}
	case "postgres":
		service["environment"] = map[string]string{"POSTGRES_USER": "junpanel", "POSTGRES_PASSWORD": r.Password, "POSTGRES_DB": "junpanel"}
		service["volumes"] = []string{"data:/var/lib/postgresql/data"}
		volumes["data"] = map[string]any{}
	case "mysql":
		service["environment"] = map[string]string{"MYSQL_ROOT_PASSWORD": r.Password, "MYSQL_DATABASE": "junpanel"}
		service["volumes"] = []string{"data:/var/lib/mysql"}
		volumes["data"] = map[string]any{}
	case "redis":
		service["command"] = []string{"redis-server", "--appendonly", "yes", "--requirepass", r.Password}
		service["volumes"] = []string{"data:/data"}
		volumes["data"] = map[string]any{}
	case "vaultwarden", "memos", "n8n", "openwebui":
		part := appData[r.Template][0]
		service["volumes"] = []string{part.Volume + ":" + part.Path}
		volumes[part.Volume] = map[string]any{}
	}
	return json.MarshalIndent(map[string]any{"name": name, "services": services, "volumes": volumes}, "", "  ")
}
func (a *agent) appDir(id string) string { return filepath.Join(a.cfg.State, "projects", id) }
func (a *agent) writeCompose(record appRecord) error {
	dir := a.appDir(record.ID)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	b, e := composeFor(record)
	if e != nil {
		return e
	}
	return atomicBytes(filepath.Join(dir, "compose.json"), b, 0600)
}
func (a *agent) catalog(w http.ResponseWriter, r *http.Request) error {
	send(w, 200, appCatalog)
	return nil
}
func (a *agent) appList(w http.ResponseWriter, r *http.Request) error {
	a.mu.Lock()
	rows, e := a.readApps()
	a.mu.Unlock()
	if e != nil {
		return e
	}
	public := publicApps(rows)
	containers, de := a.docker.list()
	for _, p := range public {
		total, running := 0, 0
		for _, c := range containers {
			if c.Project == p["project"] {
				total++
				if c.State == "running" {
					running++
				}
			}
		}
		p["containers"] = total
		p["running"] = running
		p["docker_available"] = de == nil
	}
	send(w, 200, public)
	return nil
}
func availablePort(port int) error {
	if port < 1024 || port > 65535 {
		return bad(400, "应用端口需为 1024–65535")
	}
	l, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if e != nil {
		return bad(409, "该本机端口已被占用")
	}
	l.Close()
	return nil
}
func (a *agent) appInstall(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Template string `json:"template"`
		Port     int    `json:"port"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if e := requireName(req.ID); e != nil {
		return e
	}
	if _, ok := findTemplate(req.Template); !ok {
		return bad(400, "应用模板无效")
	}
	if len(req.Name) < 1 || len(req.Name) > 60 {
		return bad(400, "应用显示名称需为 1–60 字符")
	}
	if e := availablePort(req.Port); e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	rows, e := a.readApps()
	if e != nil {
		return e
	}
	for _, app := range rows {
		if app.ID == req.ID {
			return bad(409, "应用标识已使用，包括已卸载的数据保留记录")
		}
		if app.Port == req.Port && !app.Removed {
			return bad(409, "端口已分配给其它应用")
		}
	}
	record := appRecord{ID: req.ID, Name: req.Name, Template: req.Template, Port: req.Port, Password: randomID(32), DBPassword: randomID(32), Created: time.Now(), Busy: true}
	if e = a.writeCompose(record); e != nil {
		return e
	}
	rows = append(rows, record)
	if e = a.storeApps(rows); e != nil {
		return e
	}
	j, e := a.launchApp(record, "deploy")
	if e != nil {
		rows[len(rows)-1].Busy = false
		rows[len(rows)-1].LastError = e.Error()
		a.storeApps(rows)
		return e
	}
	send(w, 202, j)
	return nil
}
func (a *agent) launchApp(record appRecord, action string) (job, error) {
	return a.launchAppThen(record, action, nil)
}

// launchAppThen runs the action as a job and reports its outcome to done, if given.
func (a *agent) launchAppThen(record appRecord, action string, done func(error)) (job, error) {
	return a.jobs.start("应用 · "+record.Name+" · "+action, func(ctx context.Context, out *limitedBuffer) (result error) {
		if done != nil {
			defer func() { done(result) }()
		}
		args := a.composeBase(record.ID)
		switch action {
		case "deploy", "restore":
			args = append(args, "up", "--detach", "--no-build", "--pull", "missing")
		case "upgrade":
		case "start":
			args = append(args, "start")
		case "stop":
			args = append(args, "stop", "--timeout", "15")
		case "restart":
			args = append(args, "restart", "--timeout", "15")
		case "remove":
			args = append(args, "down", "--timeout", "15")
		default:
			return bad(400, "应用操作无效")
		}
		fmt.Fprintln(out, "正在调用现有 Docker Compose；镜像下载可能需要几分钟。")
		var e error
		if action == "upgrade" {
			e = a.upgradeApp(ctx, out, record)
		} else {
			e = a.stream(ctx, a.appDir(record.ID), out, []string{record.Password, record.DBPassword}, "docker", args...)
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		rows, loadErr := a.readApps()
		if loadErr != nil {
			return loadErr
		}
		for i := range rows {
			if rows[i].ID == record.ID {
				rows[i].Busy = false
				rows[i].LastError = ""
				if e != nil {
					rows[i].LastError = "上次部署或管理操作未完成，请检查任务日志"
				} else if action == "remove" {
					rows[i].Removed = true
				} else if action == "restore" || action == "deploy" {
					rows[i].Removed = false
				}
			}
		}
		if writeErr := a.storeApps(rows); writeErr != nil {
			return writeErr
		}
		if e != nil {
			return bad(502, "应用操作失败；容器状态可能已变化，请查看任务输出并核对")
		}
		if action == "remove" {
			fmt.Fprintln(out, "应用容器已卸载；数据卷和私有项目凭据保留，可恢复部署。")
		}
		return nil
	})
}
func (a *agent) appAction(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Action string `json:"action"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	j, e := a.appActionJob(r.PathValue("id"), req.Action, nil)
	if e != nil {
		return e
	}
	send(w, 202, j)
	return nil
}

// appActionJob validates an app action, marks the app busy and starts the job.
func (a *agent) appActionJob(id, action string, done func(error)) (job, error) {
	if action != "start" && action != "stop" && action != "restart" && action != "deploy" && action != "remove" && action != "restore" && action != "upgrade" {
		return job{}, bad(400, "操作无效")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	rows, e := a.readApps()
	if e != nil {
		return job{}, e
	}
	for i, record := range rows {
		if record.ID != id {
			continue
		}
		if record.Busy {
			return job{}, bad(409, "此应用已有操作进行中")
		}
		if record.Removed && action != "restore" {
			return job{}, bad(409, "应用已卸载，请先恢复部署")
		}
		containers, e := a.docker.list()
		if e != nil {
			return job{}, e
		}
		for _, c := range containers {
			if c.Project == "junpanel-"+record.ID && c.Protected {
				return job{}, bad(403, "应用中有受保护容器，操作已拒绝")
			}
		}
		rows[i].Busy = true
		if e = a.storeApps(rows); e != nil {
			return job{}, e
		}
		if action != "start" {
			a.expectApp(record.ID)
		}
		j, e := a.launchAppThen(record, action, done)
		if e != nil {
			rows[i].Busy = false
			a.storeApps(rows)
			return job{}, e
		}
		return j, nil
	}
	return job{}, bad(404, "应用不存在")
}

// upgradeApp backs the app's data up, pulls newer images for the tags it uses and
// recreates whatever changed. Without a backup nothing is pulled.
func (a *agent) upgradeApp(ctx context.Context, out io.Writer, record appRecord) error {
	if len(appData[record.Template]) > 0 {
		fmt.Fprintln(out, "升级前先备份应用数据（应用会短暂停止）…")
		meta, e := a.backupApp(ctx, out, record, backupTag{Note: "升级前自动备份"})
		if e != nil {
			fmt.Fprintf(out, "升级前备份失败：%v\n应用没有升级。\n", e)
			return e
		}
		fmt.Fprintf(out, "已创建升级前备份 %s，升级后如有问题可在“备份”页恢复。\n", meta.ID)
	} else {
		fmt.Fprintln(out, "此应用不保存数据，跳过升级前备份。")
	}
	fmt.Fprintln(out, "正在拉取最新镜像…")
	if e := a.compose(ctx, out, record, "pull"); e != nil {
		return e
	}
	fmt.Fprintln(out, "正在用新镜像重建容器（未变化的容器保持运行）…")
	return a.compose(ctx, out, record, "up", "--detach", "--no-build")
}
