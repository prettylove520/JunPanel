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
	return []site{{"s_" + strings.Repeat("a", 20), "个人主页", []string{"www.example.com"}, "static", "", "/srv/junpanel/sites/s_" + strings.Repeat("a", 20), true, time.Now().Add(-72 * time.Hour)}, {"s_" + strings.Repeat("b", 20), "工作室博客", []string{"blog.example.com"}, "proxy", "http://127.0.0.1:8080", "", true, time.Now().Add(-48 * time.Hour)}, {"s_" + strings.Repeat("c", 20), "代码仓库", []string{"git.example.com"}, "proxy", "http://127.0.0.1:3000", "", true, time.Now().Add(-24 * time.Hour)}}
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
		value = map[string]any{"host": demoHost(), "host_error": "", "containers": demoContainers(), "docker_error": "", "sites": demoSites(), "external_hosts": []string{"legacy.example.com"}, "apps": demoApps(), "caddy_state": "active", "jobs": demoJobs(), "time": time.Now(), "version": version, "demo": true}
	case "/containers":
		value = demoContainers()
	case "/projects":
		value = grouped(demoContainers())
	case "/sites":
		value = map[string]any{"sites": demoSites(), "external_hosts": []string{"legacy.example.com"}, "existing_hosts": []string{"www.example.com", "blog.example.com", "git.example.com", "legacy.example.com"}, "error": "", "state": "active", "root": "/srv/junpanel"}
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
