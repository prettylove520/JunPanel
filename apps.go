package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	{"nextcloud", "Nextcloud", "网盘与协作", "私有网盘：文件同步、日历、联系人与在线协作，使用内置 SQLite", "nextcloud:apache", 8082, 80, "", "#0082c9", "镜像约 2 GB，首次启动需要几分钟。必须通过绑定好的 HTTPS 域名访问，并在首次打开的页面里创建管理员账号（数据库保持 SQLite 即可，个人使用足够）。", false},
	{"jellyfin", "Jellyfin", "影音娱乐", "自托管影音服务器：电影、剧集与音乐，自动整理海报与字幕", "jellyfin/jellyfin:latest", 8096, 8096, "", "#7b5bd6", "媒体文件放在“文件”页的 media 目录（/srv/junpanel/media），在首次设置向导里把媒体库路径选为 /media。备份只包含设置和库信息，不包含媒体文件。", false},
	{"navidrome", "Navidrome", "影音娱乐", "轻量的私人音乐流媒体服务，兼容 Subsonic 客户端", "deluan/navidrome:latest", 4533, 4533, "", "#0a8ea0", "音乐文件放在“文件”页的 music 目录（/srv/junpanel/music）。第一个创建的账号成为管理员。备份只包含库信息，不包含音乐文件。", false},
	{"trilium", "Trilium Notes", "效率工具", "层级化的知识库笔记，支持富文本、代码与关系图", "triliumnext/notes:latest", 8094, 8080, "", "#4a6cf7", "首次打开时设置密码。", false},
	{"linkding", "linkding", "效率工具", "简洁的书签管理器，支持标签与全文搜索", "sissbruecker/linkding:latest", 9090, 9090, "", "#5856e0", "管理员用户名 admin，密码在“连接信息”中查看，登录后可在设置里修改。", false},
	{"freshrss", "FreshRSS", "效率工具", "自托管的 RSS 订阅阅读器，兼容多种手机客户端", "freshrss/freshrss:latest", 8097, 80, "", "#e07b00", "首次打开会出现安装向导，数据库选 SQLite 即可。", false},
	{"gotify", "Gotify", "运维工具", "自托管的推送通知服务，可接收脚本和应用发来的消息", "gotify/server:latest", 8095, 80, "", "#4a90d9", "管理员用户名 admin，密码在“连接信息”中查看。", false},
	{"ntfy", "ntfy", "运维工具", "基于 HTTP 的简单推送通知服务，一条 curl 即可发消息", "binwiederhier/ntfy:latest", 8100, 80, "", "#338574", "默认任何人都可以向主题发布和订阅，请只绑定域名后给自己用，不要公开主题名。", false},
	{"stirlingpdf", "Stirling-PDF", "开发工具", "PDF 工具箱：合并、拆分、转换、压缩、加水印与 OCR", "stirlingtools/stirling-pdf:latest", 8093, 8080, "", "#e53935", "无需保存数据，镜像较大，运行时占用内存可能超过 1 GB。", false},
	{"drawio", "draw.io", "开发工具", "流程图、架构图与思维导图在线绘制工具，文件保存在浏览器或本地", "jgraph/drawio:latest", 8098, 8080, "", "#f08705", "文件只保存在访问者的浏览器或本机，服务器不保存数据。", false},
	{"codeserver", "code-server", "开发工具", "在浏览器里运行的 VS Code，随时随地写代码", "codercom/code-server:latest", 8088, 8080, "", "#2e7ff6", "登录密码在“连接信息”中查看。它带有容器内的终端，等同于一个可写代码的 Linux 环境，务必只通过 HTTPS 域名使用，并保管好密码。", false},
	{"audiobookshelf", "Audiobookshelf", "影音娱乐", "自托管的有声书与播客服务器，支持进度同步和手机客户端", "ghcr.io/advplyr/audiobookshelf:latest", 13378, 80, "", "#82612c", "有声书放在“文件”页的 audiobooks 目录（/srv/junpanel/audiobooks），播客放在 podcasts 目录。第一个创建的账号成为管理员。备份只包含设置和库信息，不包含音频文件。", false},
	{"kavita", "Kavita", "影音娱乐", "漫画、电子书与轻小说阅读服务器，支持网页阅读和阅读进度", "ghcr.io/kareadita/kavita:latest", 5004, 5000, "", "#4ac694", "书籍放在“文件”页的 books 目录（/srv/junpanel/books），创建管理员后在“库”里添加 /books 下的文件夹。备份只包含设置和库信息，不包含书籍文件。", false},
	{"mealie", "Mealie", "生活与财务", "自托管的食谱管理与每周菜单，支持从网页一键导入食谱", "ghcr.io/mealie-recipes/mealie:latest", 9925, 9000, "", "#e58325", "初始账号 changeme@example.com，密码 MyPassword，登录后请立刻在个人资料里修改。已关闭开放注册，需要时在管理页创建用户。内存占用约 500 MB。", false},
	{"actual", "Actual Budget", "生活与财务", "本地优先的个人记账与预算工具，数据可在多设备同步", "actualbudget/actual-server:latest", 5006, 5006, "", "#6b46c1", "必须通过绑定好的 HTTPS 域名访问，首次打开时设置服务器密码。", false},
	{"wallos", "Wallos", "生活与财务", "订阅与周期性支出管理，到期提醒，按月、年统计花费", "bellamy/wallos:latest", 8282, 80, "", "#3b82f6", "首次打开会让你注册管理员账号，之后可以在设置里关闭注册。", false},
	{"homebox", "Homebox", "生活与财务", "家庭物品与资产清单：位置、标签、保修与购买记录", "ghcr.io/sysadminsmedia/homebox:latest", 7745, 7745, "", "#5b7f67", "第一个注册的账号成为管理员；其他人也能注册，请自己注册好后在“已安装”页的设置里添加环境变量 HBOX_OPTIONS_ALLOW_REGISTRATION=false 关闭注册。", false},
	{"changedetection", "changedetection.io", "运维工具", "监控网页内容变化（价格、库存、公告），变化时发送通知", "ghcr.io/dgtlmoon/changedetection.io:latest", 5003, 5000, "", "#2563eb", "默认没有密码，打开后请先到“设置 → 密码”设置访问密码，再绑定域名。仅抓取普通网页，不含浏览器渲染。", false},
	{"pairdrop", "PairDrop", "网盘与协作", "在同一网络下的设备之间互传文件和文字，浏览器里直接用", "ghcr.io/schlagmichdoch/pairdrop:latest", 3010, 3000, "", "#4a63e7", "需要通过 HTTPS 域名访问。无需账号，也不保存文件，传输在设备之间直接进行。", false},
	{"cyberchef", "CyberChef", "开发工具", "数据编解码与分析工具箱：Base64、哈希、加解密、正则等", "ghcr.io/gchq/cyberchef:latest", 8101, 8080, "", "#8a6a3b", "无需账号，也不保存数据，所有处理都在浏览器里完成。", false},
	{"openwebui-ollama", "Open WebUI + Ollama", "AI 工具", "一个容器里同时带本地大模型运行环境（Ollama）和聊天界面，装好就能在本机跑模型", "ghcr.io/open-webui/open-webui:ollama", 3081, 8080, "", "#111827", "仅用 CPU 运行：建议内存 8 GB 以上，先拉取 1–3B 的小模型（如 qwen2.5:1.5b、llama3.2:3b）。镜像约 3.3 GB，模型文件另占磁盘（每个数 GB）。备份只含设置和聊天记录，不含模型。第一个注册的账号成为管理员。", false},
	{"newapi", "New API", "AI 工具", "大模型接口网关：把多家模型服务聚合成统一的 OpenAI 兼容接口，管理密钥、额度和用户", "calciumion/new-api:latest", 3002, 3000, "", "#0f766e", "首次打开按页面向导创建管理员账号。对外提供接口前请设置强密码，并通过 HTTPS 域名访问。", false},
	{"flowise", "Flowise", "AI 工具", "拖拽式搭建大模型应用和智能体工作流，可接知识库与各家模型", "flowiseai/flowise:3.1.3", 3003, 3000, "", "#3b5bdb", "固定在 3.1.3：最新的 3.1.4 镜像启动即报错退出（会话存储的 SQLite 错误），等上游修复后再更新。首次打开按页面提示完成管理员账号设置。镜像约 1.5 GB，运行占用内存约 1 GB。它带有能执行代码的节点，务必设置登录并只通过 HTTPS 域名使用。", false},
	{"llmfit", "llmfit", "AI 工具", "检测服务器的 CPU、内存和显卡，推荐能流畅运行的开源大模型，并估算占用和速度", "ghcr.io/alexsjones/llmfit:latest", 8103, 8787, "", "#e8590c", "在容器里检测，看到的是整台服务器的 CPU 和内存；容器里没有显卡驱动，所以不识别 GPU，纯 CPU 服务器的推荐是准的。没有登录，请只绑定域名后设置访问密码或 IP 白名单，或仅本机访问。", false},
	{"maxkb", "MaxKB", "AI 工具", "基于大模型的知识库问答系统：上传文档后自带向量检索与工作流，适合做内部资料问答", "1panel/maxkb:latest", 8102, 8080, "", "#3370ff", "内置数据库，建议内存 4 GB 以上，首次启动需要一两分钟。初始账号 admin、初始密码 MaxKB@123..（公开信息），登录后请立刻修改。", false},
}

// appSpec describes the plain single-container templates: a few settings and the
// data volumes. "{password}" in a value is replaced by the app's generated password; "{secret}" by a
// long value derived from it for apps that insist on a separate server secret.
// Volumes starting with "/" are folders of the panel's file area, created by Docker.
type appSpec struct {
	Env     map[string]string
	Command []string
	Volumes []string
	Login   string
}

var appSpecs = map[string]appSpec{
	"nextcloud":       {Env: map[string]string{"OVERWRITEPROTOCOL": "https"}, Volumes: []string{"data:/var/www/html"}},
	"jellyfin":        {Volumes: []string{"config:/config", "cache:/cache", "{root}/media:/media"}},
	"navidrome":       {Volumes: []string{"data:/data", "{root}/music:/music:ro"}},
	"trilium":         {Volumes: []string{"data:/home/node/trilium-data"}},
	"linkding":        {Env: map[string]string{"LD_SERVER_HOST": "0.0.0.0", "LD_SUPERUSER_NAME": "admin", "LD_SUPERUSER_PASSWORD": "{password}"}, Volumes: []string{"data:/etc/linkding/data"}, Login: "admin"},
	"freshrss":        {Env: map[string]string{"CRON_MIN": "13,43"}, Volumes: []string{"data:/var/www/FreshRSS/data", "extensions:/var/www/FreshRSS/extensions"}},
	"gotify":          {Env: map[string]string{"GOTIFY_DEFAULTUSER_NAME": "admin", "GOTIFY_DEFAULTUSER_PASS": "{password}"}, Volumes: []string{"data:/app/data"}, Login: "admin"},
	"ntfy":            {Command: []string{"serve"}},
	"stirlingpdf":     {},
	"drawio":          {},
	"codeserver":      {Env: map[string]string{"PASSWORD": "{password}"}, Volumes: []string{"data:/home/coder"}, Login: "coder"},
	"audiobookshelf":  {Volumes: []string{"config:/config", "metadata:/metadata", "{root}/audiobooks:/audiobooks", "{root}/podcasts:/podcasts"}},
	"kavita":          {Volumes: []string{"config:/kavita/config", "{root}/books:/books"}},
	"mealie":          {Env: map[string]string{"ALLOW_SIGNUP": "false", "MAX_WORKERS": "1", "WEB_CONCURRENCY": "1"}, Volumes: []string{"data:/app/data"}},
	"actual":          {Volumes: []string{"data:/data"}},
	"wallos":          {Env: map[string]string{"TZ": "Asia/Shanghai"}, Volumes: []string{"db:/var/www/html/db", "logos:/var/www/html/images/uploads/logos"}},
	"homebox":         {Env: map[string]string{"HBOX_AUTH_API_KEY_PEPPER": "{secret}"}, Volumes: []string{"data:/data"}},
	"changedetection": {Volumes: []string{"datastore:/datastore"}},
	"pairdrop":        {},
	"cyberchef":       {},
	// Models are downloaded into their own volume (several GB each) that backups leave out.
	"openwebui-ollama": {Volumes: []string{"data:/app/backend/data", "ollama:/root/.ollama"}},
	// The image's default command only prints a report; "serve" starts the web page and API.
	"llmfit": {Command: []string{"serve", "--host", "0.0.0.0", "--port", "8787"}},
	"newapi": {Env: map[string]string{"TZ": "Asia/Shanghai", "SESSION_SECRET": "{secret}"}, Volumes: []string{"data:/data"}},
	// The image's node user owns /home/node and nothing below it that a new volume could copy, so the volume goes there.
	"flowise": {Volumes: []string{"data:/home/node"}},
	"maxkb":   {Volumes: []string{"data:/opt/maxkb"}},
}

// appSecret is a stable 64-character secret for an app, derived from its generated password so a
// redeploy keeps it and nothing else has to be stored.
func appSecret(r appRecord) string {
	sum := sha256.Sum256([]byte("junpanel-app-secret:" + r.Template + ":" + r.ID + ":" + r.Password))
	return hex.EncodeToString(sum[:])
}

func init() {
	for i := range appCatalog {
		appCatalog[i].Backup = len(appData[appCatalog[i].Key]) > 0
	}
}

// multiInstance lists the templates that make sense more than once (several sites or
// database servers). Every other app is meant to be installed once, and the store shows
// it as installed after that.
var multiInstance = map[string]bool{"wordpress": true, "postgres": true, "mysql": true, "redis": true}

type catalogEntry struct {
	appTemplate
	Multi bool     `json:"multi"`
	Guide []string `json:"guide"`
}

func catalogView() []catalogEntry {
	out := make([]catalogEntry, 0, len(appCatalog))
	for _, t := range appCatalog {
		out = append(out, catalogEntry{t, multiInstance[t.Key], appGuides[t.Key]})
	}
	return out
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
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Template   string            `json:"template"`
	Port       int               `json:"port"`
	Password   string            `json:"password"`
	DBPassword string            `json:"db_password,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Created    time.Time         `json:"created"`
	Removed    bool              `json:"removed"`
	Busy       bool              `json:"busy"`
	LastError  string            `json:"last_error,omitempty"`
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
		case "nextcloud", "jellyfin", "navidrome", "trilium", "freshrss", "ntfy", "stirlingpdf", "drawio":
			values["note"] = "此应用没有预设的登录凭据；首次访问时按页面提示创建账号或设置密码。界面端口仅监听本机，请通过绑定的域名访问。"
		case "linkding", "gotify", "codeserver":
			values["user"] = appSpecs[record.Template].Login
			values["password"] = record.Password
			values["note"] = "这是此应用的初始登录账号。登录后建议在应用内修改密码；如在应用内修改，这里显示的仍是初始密码。"
		case "maxkb":
			values["user"] = "admin"
			values["password"] = "MaxKB@123.."
			values["note"] = "这是 MaxKB 公开的初始账号，所有人都知道，登录后请立刻在个人设置里修改密码。"
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
func composeFor(r appRecord) ([]byte, error) { return composeForRoot(r, "/srv/junpanel") }

func composeForRoot(r appRecord, root string) ([]byte, error) {
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
	default:
		spec, ok := appSpecs[r.Template]
		if !ok {
			break
		}
		if len(spec.Env) > 0 {
			env := map[string]string{}
			for k, v := range spec.Env {
				env[k] = strings.ReplaceAll(strings.ReplaceAll(v, "{password}", r.Password), "{secret}", appSecret(r))
			}
			service["environment"] = env
		}
		if len(spec.Command) > 0 {
			service["command"] = spec.Command
		}
		mounts := []string{}
		for _, v := range spec.Volumes {
			v = strings.ReplaceAll(v, "{root}", root)
			mounts = append(mounts, v)
			if !strings.HasPrefix(v, "/") {
				volumes[strings.SplitN(v, ":", 2)[0]] = map[string]any{}
			}
		}
		if len(mounts) > 0 {
			service["volumes"] = mounts
		}
	}
	// Settings the administrator added in the panel; generated secrets cannot be overridden.
	if len(r.Env) > 0 {
		env, _ := service["environment"].(map[string]string)
		if env == nil {
			env = map[string]string{}
		}
		for k, v := range r.Env {
			if !protectedEnv[k] && envName.MatchString(k) {
				env[k] = v
			}
		}
		service["environment"] = env
	}
	return json.MarshalIndent(map[string]any{"name": name, "services": services, "volumes": volumes}, "", "  ")
}
func (a *agent) appDir(id string) string { return filepath.Join(a.cfg.State, "projects", id) }
func (a *agent) writeCompose(record appRecord) error {
	dir := a.appDir(record.ID)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	b, e := composeForRoot(record, a.cfg.Root)
	if e != nil {
		return e
	}
	return atomicBytes(filepath.Join(dir, "compose.json"), b, 0600)
}
func (a *agent) catalog(w http.ResponseWriter, r *http.Request) error {
	send(w, 200, catalogView())
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
		case "purge":
			args = append(args, "down", "--volumes", "--timeout", "15")
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
		if e == nil && action == "purge" {
			kept := rows[:0]
			for _, r := range rows {
				if r.ID != record.ID {
					kept = append(kept, r)
				}
			}
			rows = kept
		}
		if writeErr := a.storeApps(rows); writeErr != nil {
			return writeErr
		}
		if e != nil {
			return bad(502, "应用操作失败；容器状态可能已变化，请查看任务输出并核对")
		}
		switch action {
		case "remove":
			a.pauseAppSites(record, out)
			fmt.Fprintln(out, "应用容器已卸载；数据卷和私有项目凭据保留，可恢复部署。")
		case "restore", "deploy":
			a.resumeAppSites(record, out)
		case "purge":
			if e := os.RemoveAll(a.appDir(record.ID)); e != nil {
				fmt.Fprintf(out, "应用已删除，但项目目录清理失败，可忽略或手动删除：%v\n", e)
			}
			a.dropAppSchedules(record.ID, out)
			fmt.Fprintln(out, "应用及其数据卷、私有项目凭据已彻底删除；备份文件不受影响，绑定的域名保持停用。")
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
	if action != "start" && action != "stop" && action != "restart" && action != "deploy" && action != "remove" && action != "restore" && action != "upgrade" && action != "purge" {
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
		if record.Removed && action != "restore" && action != "purge" {
			return job{}, bad(409, "应用已卸载，请先恢复部署")
		}
		if action == "purge" && !record.Removed {
			return job{}, bad(409, "请先卸载应用，再彻底删除")
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
