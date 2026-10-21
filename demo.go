package main

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

func demoContainers() []container {
	return []container{
		{ID: strings.Repeat("a", 64), Name: "junpanel-studio-app-1", Image: "wordpress:php8.3-apache", State: "running", Status: "Up 3 days", Project: "junpanel-studio", Service: "app", Ports: []portInfo{{"127.0.0.1", 8080, 80, "tcp"}}},
		{ID: strings.Repeat("b", 64), Name: "junpanel-studio-database-1", Image: "mariadb:11.4", State: "running", Status: "Up 3 days (healthy)", Project: "junpanel-studio", Service: "database", Ports: []portInfo{}},
		{ID: strings.Repeat("c", 64), Name: "junpanel-notes-app-1", Image: "docker.gitea.com/gitea:1", State: "running", Status: "Up 1 day", Project: "junpanel-notes", Service: "app", Ports: []portInfo{{"127.0.0.1", 3000, 3000, "tcp"}}},
		{ID: strings.Repeat("d", 64), Name: "playground-web-1", Image: "nginx:alpine", State: "exited", Status: "Exited (0) 4 hours ago", Project: "playground", Service: "web", Ports: []portInfo{}},
	}
}
func demoSites() []site {
	a, b, c, d := "s_"+strings.Repeat("a", 20), "s_"+strings.Repeat("b", 20), "s_"+strings.Repeat("c", 20), "s_"+strings.Repeat("d", 20)
	return []site{
		{ID: a, Name: "个人主页", Domains: []string{"www.example.com"}, Kind: "static", Root: "/srv/junpanel/sites/" + a, Enabled: true, Created: time.Now().Add(-72 * time.Hour)},
		{ID: b, Name: "工作室博客", Domains: []string{"blog.example.com"}, Kind: "proxy", Upstream: "http://127.0.0.1:8080", Enabled: true, Created: time.Now().Add(-48 * time.Hour)},
		{ID: c, Name: "代码仓库", Domains: []string{"git.example.com"}, Kind: "proxy", Upstream: "http://127.0.0.1:3000", Enabled: true, Created: time.Now().Add(-24 * time.Hour), AuthUser: "team", AuthHash: "demo", AllowIPs: []string{"203.0.113.0/24"}},
		{ID: d, Name: "旧域名跳转", Domains: []string{"old.example.com"}, Kind: "redirect", Target: "https://www.example.com", Permanent: true, KeepPath: true, Enabled: true, Created: time.Now().Add(-12 * time.Hour)},
	}
}
func demoPublicSites() []map[string]any {
	out := []map[string]any{}
	for _, s := range demoSites() {
		out = append(out, publicSite(s))
	}
	return out
}
func demoApps() []map[string]any {
	return []map[string]any{{"id": "studio", "name": "工作室博客", "template": "wordpress", "port": 8080, "created": time.Now().Add(-72 * time.Hour), "removed": false, "busy": false, "project": "junpanel-studio", "category": "网站应用", "image": "wordpress:php8.3-apache", "database": "", "containers": 2, "running": 2, "docker_available": true}, {"id": "notes", "name": "代码仓库", "template": "gitea", "port": 3000, "created": time.Now().Add(-24 * time.Hour), "removed": false, "busy": false, "project": "junpanel-notes", "category": "开发工具", "image": "docker.gitea.com/gitea:1", "database": "", "containers": 1, "running": 1, "docker_available": true}}
}
func demoJobs() []job {
	ended := time.Now().Add(-15 * time.Minute)
	return []job{{ID: "demo-01", Title: "应用 · 工作室博客 · deploy", State: "success", Started: ended.Add(-2 * time.Minute), Ended: &ended, Output: "[演示数据]\n镜像准备完成\n数据库健康检查通过\n应用已启动在 127.0.0.1:8080\n可通过网站管理绑定域名。"}, {ID: "demo-02", Title: "容器 · restart", State: "success", Started: ended.Add(-2 * time.Hour), Ended: &ended, Output: "[演示数据] 容器重启完成"}}
}
func demoHost() hostInfo {
	now := time.Now()
	cpu := 13.5 + math.Sin(float64(now.Unix())/30)*4
	rx, tx := 23100.0, 8100.0
	history := []chartPoint{}
	for i := 0; i < 120; i++ {
		x := float64(i)
		history = append(history, chartPoint{now.Add(time.Duration(i-119) * 5 * time.Second), 13 + 4*math.Sin(x/7) + 2*math.Cos(x/3), 38 + 3*math.Sin(x/23), 21000 + 9000*math.Sin(x/12), 7000 + 2000*math.Cos(x/9)})
	}
	return hostInfo{Hostname: "de-fra-01", OS: "Debian GNU/Linux 13 · 演示", Kernel: "6.x", Cores: 2, CPU: &cpu, MemTotal: 2 << 30, MemUsed: 810 << 20, SwapTotal: 2 << 30, SwapUsed: 32 << 20, Load: []float64{.17, .12, .09}, Uptime: 298440, RX: &rx, TX: &tx, RXTotal: 128 << 30, TXTotal: 42 << 30, Interfaces: []string{"ens3"}, History: history, Disk: map[string]uint64{"total": 40 << 30, "used": 12 << 30, "free": 28 << 30}}
}
func demoAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api")
	var value any
	switch path {
	case "/overview":
		value = map[string]any{"host": demoHost(), "host_error": "", "containers": demoContainers(), "docker_error": "", "sites": demoPublicSites(), "external_hosts": []string{"legacy.example.com", "company.example.com", "monitor.example.com"}, "caddy_sources": demoCaddySources(), "apps": demoApps(), "caddy_state": "active", "alerts_active": 2, "update_available": "9.9.9", "jobs": demoJobs(), "time": time.Now(), "version": version, "demo": true}
	case "/containers":
		value = demoContainers()
	case "/projects":
		value = grouped(demoContainers())
	case "/sites":
		value = map[string]any{"sites": demoPublicSites(), "external_hosts": []string{"legacy.example.com", "company.example.com", "monitor.example.com"}, "caddy_sources": demoCaddySources(), "existing_hosts": []string{"www.example.com", "blog.example.com", "git.example.com", "legacy.example.com"}, "error": "", "state": "active", "root": "/srv/junpanel"}
	case "/access":
		value = map[string]any{"entries": []map[string]string{{"kind": "domain", "address": "panel.example.com"}, {"kind": "ip", "address": "203.0.113.10"}}, "managed": true, "running": false, "supported": true}
	case "/stacks":
		value = map[string]any{"root": "/srv/junpanel/stacks", "stacks": []map[string]any{{"name": "uptime", "services": []string{"kuma"}, "containers": []map[string]any{{"id": strings.Repeat("9", 64), "name": "uptime-kuma-1", "image": "louislam/uptime-kuma:1", "state": "running", "status": "Up 3 hours"}}, "running": 1, "updated": time.Now().Add(-5 * time.Hour), "busy": false}}}
	case "/volumes":
		value = []map[string]any{{"name": "junpanel-blog_db", "driver": "local", "created": time.Now().Add(-72 * time.Hour).Format(time.RFC3339), "project": "junpanel-blog", "panel_app": true, "size": 214 << 20, "in_use": true, "containers": 1}, {"name": "old-cache", "driver": "local", "created": time.Now().Add(-400 * time.Hour).Format(time.RFC3339), "project": "", "panel_app": false, "size": 12 << 20, "in_use": false, "containers": 0}}
	case "/networks":
		value = []map[string]any{{"id": strings.Repeat("1", 64), "name": "bridge", "driver": "bridge", "scope": "local", "internal": false, "subnets": []string{"172.17.0.0/16"}, "containers": 0, "builtin": true, "project": ""}, {"id": strings.Repeat("2", 64), "name": "junpanel-blog_default", "driver": "bridge", "scope": "local", "internal": false, "subnets": []string{"172.18.0.0/16"}, "containers": 2, "builtin": false, "project": "junpanel-blog"}, {"id": strings.Repeat("3", 64), "name": "backend", "driver": "bridge", "scope": "local", "internal": true, "subnets": []string{"172.19.0.0/16"}, "containers": 0, "builtin": false, "project": ""}}
	case "/catalog":
		value = appCatalog
	case "/apps":
		value = demoApps()
	case "/jobs":
		value = demoJobs()
	case "/databases":
		value = []map[string]any{{"id": "studio", "name": "工作室博客", "kind": "mariadb", "database": "wordpress", "port": 0, "busy": false}}
	case "/images":
		value = []map[string]any{{"id": strings.Repeat("e", 64), "tags": []string{"wordpress:php8.3-apache"}, "size": 653 << 20, "created": time.Now().Add(-72 * time.Hour).Unix()}, {"id": strings.Repeat("f", 64), "tags": []string{"mariadb:11.4"}, "size": 332 << 20, "created": time.Now().Add(-72 * time.Hour).Unix()}}
	case "/files":
		current := r.URL.Query().Get("path")
		entries := []fileEntry{}
		if current == "" || current == "/" {
			entries = append(entries, fileEntry{Name: "sites", Path: "/sites", Dir: true, Modified: time.Now().Add(-72 * time.Hour), Mode: "drwxr-xr-x"})
		} else if current == "/sites" {
			for _, s := range demoSites() {
				if s.Kind == "static" {
					entries = append(entries, fileEntry{Name: s.ID, Path: "/sites/" + s.ID, Dir: true, Modified: s.Created, Mode: "drwxr-xr-x"})
				}
			}
		} else {
			entries = append(entries, fileEntry{Name: "index.html", Path: strings.TrimSuffix(current, "/") + "/index.html", Size: 2048, Modified: time.Now().Add(-3 * time.Hour), Mode: "-rw-r--r--"}, fileEntry{Name: "assets", Path: strings.TrimSuffix(current, "/") + "/assets", Dir: true, Modified: time.Now().Add(-24 * time.Hour), Mode: "drwxr-xr-x"})
		}
		value = map[string]any{"path": current, "root": "/srv/junpanel", "entries": entries}
	case "/files/content":
		value = map[string]string{"text": "<!doctype html>\n<html lang=\"zh-CN\">\n<head><meta charset=\"utf-8\"><title>我的网站</title></head>\n<body>\n  <h1>你好，这里是我的个人主页。</h1>\n  <p>此处为JunPanel面板的演示文件。</p>\n</body>\n</html>", "revision": "demo"}
	case "/trash":
		value = []trashRecord{}
	case "/upgrade":
		value = map[string]any{"current": version, "available": true, "supported": true, "can_upgrade": true, "auto_check": true, "blockers": []string{}, "log": "", "checked_at": time.Now().Add(-40 * time.Minute),
			"latest":    map[string]any{"version": "9.9.9", "notes": "演示数据：这里会显示新版本的更新说明。"},
			"notes_url": "https://github.com/prettylove520/JunPanel/releases", "release_url": "https://github.com/prettylove520/JunPanel/releases"}
	case "/alerts":
		value = map[string]any{
			"config":  alertView{Format: "wecom", URLSet: true, URLHost: "qyapi.weixin.qq.com", Notify: true, DiskOn: true, DiskPercent: 90, CertOn: true, CertDays: 14, ContainerOn: true, ServiceOn: true, RepeatHours: 12},
			"formats": alertFormats,
			"active": []activeAlert{
				{Key: "disk:/", Title: "磁盘空间不足：/", Detail: "已用 92%，剩余 6.4 GB（阈值 90%）", Severity: "warning", Since: time.Now().Add(-3 * time.Hour)},
				{Key: "container:x", Title: "容器已停止：blog-app-1", Detail: "退出码 137（Exited (137) 12 minutes ago），镜像 wordpress:php8.3-apache", Severity: "critical", Since: time.Now().Add(-12 * time.Minute)},
			},
			"history": []alertEvent{
				{Time: time.Now().Add(-12 * time.Minute), Key: "container:x", Kind: "triggered", Title: "容器已停止：blog-app-1", Detail: "退出码 137"},
				{Time: time.Now().Add(-3 * time.Hour), Key: "disk:/", Kind: "triggered", Title: "磁盘空间不足：/", Detail: "已用 92%"},
				{Time: time.Now().Add(-30 * time.Hour), Key: "cert:blog.example.com", Kind: "resolved", Title: "证书即将过期：blog.example.com", Detail: "已恢复正常"},
			},
			"delivery": deliveryResult{At: time.Now().Add(-12 * time.Minute), OK: true, Message: "已发送"},
		}
	case "/certificates":
		value = map[string]any{"found": true, "certificates": []map[string]any{
			{"name": "blog.example.com", "issuer": "R12", "not_after": time.Now().Add(61 * 24 * time.Hour), "days_left": 61, "warn": false},
			{"name": "git.example.com", "issuer": "R12", "not_after": time.Now().Add(11 * 24 * time.Hour), "days_left": 11, "warn": true},
		}}
	case "/schedules":
		value = map[string]any{"zone": "CST +0800", "now": time.Now(), "schedules": []schedule{
			{ID: "sc_" + strings.Repeat("1", 16), Name: "每晚备份博客", Action: "backup", Type: "app", Target: "blog", Keep: 7, Every: "daily", At: "03:00", Enabled: true, Created: time.Now().Add(-9 * 24 * time.Hour), Next: time.Now().Add(9 * time.Hour), Last: &scheduleRun{At: time.Now().Add(-15 * time.Hour), Status: "success", Message: "已备份，保留最近 7 份（已清理 1 份旧备份）"}},
			{ID: "sc_" + strings.Repeat("2", 16), Name: "每周重启监控", Action: "restart", Target: "kuma", Every: "weekly", Weekday: 1, At: "04:30", Enabled: true, Created: time.Now().Add(-20 * 24 * time.Hour), Next: time.Now().Add(80 * time.Hour), Last: &scheduleRun{At: time.Now().Add(-88 * time.Hour), Status: "failed", Message: "应用操作失败；容器状态可能已变化，请查看任务输出并核对"}},
		}}
	case "/backups":
		value = map[string]any{"directory": "/var/lib/junpanel-agent/backups", "free": 38 << 30, "backups": []backupMeta{
			{ID: "b_" + strings.Repeat("a", 20), Type: "app", Target: "blog", Name: "我的博客", File: "x.tar.gz", Size: 214 << 20, SHA256: strings.Repeat("a", 64), Created: time.Now().Add(-26 * time.Hour)},
			{ID: "b_" + strings.Repeat("b", 20), Type: "site", Target: "s_" + strings.Repeat("1", 20), Name: "个人主页", File: "y.tar.gz", Size: 3 << 20, SHA256: strings.Repeat("b", 64), Created: time.Now().Add(-50 * time.Hour), Note: "恢复前自动备份"},
			{ID: "b_" + strings.Repeat("c", 20), Type: "database", Target: "blog", Name: "我的博客", Database: "wordpress", File: "z.sql.gz", Size: 18 << 20, SHA256: strings.Repeat("c", 64), Created: time.Now().Add(-72 * time.Hour)},
		}}
	default:
		if strings.HasPrefix(path, "/containers/") && strings.HasSuffix(path, "/logs") {
			value = map[string]string{"text": fmt.Sprintf("[演示日志 · 模拟数据]\n%s GET / 200 2ms\n%s 健康检查正常\n", time.Now().Add(-10*time.Second).Format(time.RFC3339), time.Now().Format(time.RFC3339))}
		} else {
			fail(w, bad(404, "演示接口不存在"))
			return
		}
	}
	send(w, 200, value)
}

func demoCaddySources() []caddyConfig {
	return []caddyConfig{
		{Config: "/etc/caddy/Caddyfile", Adapter: "caddyfile", PIDs: []int{1201}, Hosts: []string{"www.example.com", "blog.example.com", "git.example.com", "legacy.example.com"}, Primary: true},
		{Config: "/etc/caddy/company.caddy", Adapter: "caddyfile", PIDs: []int{1202}, Hosts: []string{"company.example.com"}},
		{Config: "/etc/monitoring/production.caddy", Adapter: "caddyfile", PIDs: []int{1203}, Hosts: []string{"monitor.example.com"}},
	}
}

func demoAudit(kind string) []auditEntry {
	now := time.Now().UTC()
	if kind == "login" {
		return []auditEntry{
			{Time: now.Add(-2 * time.Minute), Kind: "login", IP: "203.0.113.24", Action: "登录成功", Result: "ok"},
			{Time: now.Add(-3 * time.Minute), Kind: "login", IP: "198.51.100.7", Action: "登录失败", Result: "failed"},
			{Time: now.Add(-26 * time.Hour), Kind: "login", IP: "203.0.113.24", Action: "退出登录", Result: "ok"},
		}
	}
	return []auditEntry{
		{Time: now.Add(-1 * time.Minute), Kind: "operation", IP: "203.0.113.24", Action: "应用操作", Detail: "action=restart", Method: "POST", Path: "/api/apps/notes/action", Status: 202, Result: "ok"},
		{Time: now.Add(-40 * time.Minute), Kind: "operation", IP: "203.0.113.24", Action: "新建备份", Detail: "type=app target=blog", Method: "POST", Path: "/api/backups", Status: 202, Result: "ok"},
		{Time: now.Add(-3 * time.Hour), Kind: "operation", IP: "203.0.113.24", Action: "保存网站", Detail: "name=个人主页 kind=static", Method: "POST", Path: "/api/sites", Status: 409, Result: "failed"},
	}
}
