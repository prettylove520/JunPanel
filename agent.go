package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type agentConfig struct{ State, Root, Caddy, Fragments, Socket, CaddyData string }
type agent struct {
	cfg            agentConfig
	docker         *dockerClient
	jobs           *jobQueue
	mu             sync.Mutex
	metrics        *hostSampler
	history        *historyStore
	runner         func(time.Duration, string, ...string) (string, error)
	stream         func(ctx context.Context, dir string, out io.Writer, secrets []string, name string, args ...string) error
	capture        func(time.Duration, string, ...string) (string, error)
	processConfigs func() ([]caddyConfig, error)
	now            func() time.Time
	schedMu        sync.Mutex
	schedRunning   map[string]time.Time
	schedTries     map[string]int
	host           string
	diskFn         func(string) map[string]uint64
	webhook        *http.Client
	alertRun       sync.Mutex
	alertMu        sync.Mutex
	sendMu         sync.Mutex
	pending        []*pendingBatch
	streak         map[string]int
	okStreak       map[string]int
	expMu          sync.Mutex
	expected       map[string]time.Time
	certMu         sync.Mutex
	certAt         time.Time
	certList       []certInfo
	certErr        error
	upMu           sync.Mutex
	upCheck        sync.Mutex
	upHTTP         *http.Client
	upRaw          string
	upAPI          string
	upDL           string
	upPage         string
	upMax          int64
	upSystemd      string
	composeConfig  func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, error)
	stackMu        sync.Mutex
	stackBusy      map[string]bool
	stackErr       map[string]string
	access         accessState
	accessRun      func(ctx context.Context, systemdRun string, script []byte, env []string, out io.Writer, args ...string) (int, error)
	invHosts       []string
	invSources     []caddyConfig
	invErr         error
	lookup         func(ctx context.Context, host string) ([]string, error)
	dns            dnsCache
	healthHTTPS    string
	healthHTTP     string
	health         healthCache
}

func newAgent(cfg agentConfig) (*agent, error) {
	for _, path := range []string{cfg.State, cfg.Root, filepath.Join(cfg.Root, "sites"), filepath.Join(cfg.Root, ".trash")} {
		mode := os.FileMode(0755)
		if path == cfg.State || filepath.Base(path) == ".trash" {
			mode = 0700
		}
		if e := os.MkdirAll(path, mode); e != nil {
			return nil, e
		}
	}
	a := &agent{cfg: cfg, docker: newDocker("/var/run/docker.sock"), jobs: newQueue(), metrics: &hostSampler{}, runner: command, stream: streamCommand, now: time.Now, schedRunning: map[string]time.Time{}, schedTries: map[string]int{}, diskFn: diskSpace, webhook: webhookClient(), streak: map[string]int{}, okStreak: map[string]int{}, expected: map[string]time.Time{}, upHTTP: upgradeClient(), upRaw: "https://raw.githubusercontent.com/" + updateRepo + "/main", upAPI: "https://api.github.com/repos/" + updateRepo, upDL: "https://github.com/" + updateRepo + "/releases/download", upPage: "https://github.com/" + updateRepo, upMax: maxDownload, upSystemd: "/usr/bin/systemd-run", composeConfig: runAsNobody, stackBusy: map[string]bool{}, stackErr: map[string]string{}, accessRun: runAccess, healthHTTPS: "127.0.0.1:443", healthHTTP: "127.0.0.1:80"}
	rows, e := a.readApps()
	if e != nil {
		return nil, e
	}
	a.history = newHistoryStore(historyPath(cfg.State))
	if e = os.MkdirAll(filepath.Join(cfg.State, "backups"), 0700); e != nil {
		return nil, e
	}
	partial, _ := filepath.Glob(filepath.Join(cfg.State, "backups", "*.partial"))
	for _, p := range partial {
		os.Remove(p)
	}
	changed := false
	for i := range rows {
		if rows[i].Busy {
			rows[i].Busy = false
			rows[i].LastError = "管理代理曾重启，请核对容器现状后重试操作"
			changed = true
		}
	}
	if changed {
		if e = a.storeApps(rows); e != nil {
			return nil, e
		}
	}
	if e = a.recoverSchedules(); e != nil {
		return nil, e
	}
	a.refreshRun(a.now())
	return a, nil
}
func (a *agent) routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /overview", endpoint(a.overview))
	m.HandleFunc("GET /containers", endpoint(a.containers))
	m.HandleFunc("GET /containers/{id}/logs", endpoint(a.containerLogs))
	m.HandleFunc("POST /containers/{id}/action", endpoint(a.containerAction))
	m.HandleFunc("GET /images", endpoint(a.images))
	m.HandleFunc("GET /stacks", endpoint(a.stackList))
	m.HandleFunc("GET /stacks/{name}", endpoint(a.stackRead))
	m.HandleFunc("POST /stacks/check", endpoint(a.stackCheck))
	m.HandleFunc("POST /stacks", endpoint(a.stackSave))
	m.HandleFunc("POST /stacks/{name}/action", endpoint(a.stackAction))
	m.HandleFunc("DELETE /stacks/{name}", endpoint(a.stackDelete))
	m.HandleFunc("GET /volumes", endpoint(a.volumeList))
	m.HandleFunc("POST /volumes", endpoint(a.volumeCreate))
	m.HandleFunc("DELETE /volumes/{name}", endpoint(a.volumeDelete))
	m.HandleFunc("GET /networks", endpoint(a.networkList))
	m.HandleFunc("POST /networks", endpoint(a.networkCreate))
	m.HandleFunc("DELETE /networks/{id}", endpoint(a.networkDelete))
	m.HandleFunc("POST /images/pull", endpoint(a.imagePull))
	m.HandleFunc("DELETE /images/{id}", endpoint(a.imageDelete))
	m.HandleFunc("GET /projects", endpoint(a.projects))
	m.HandleFunc("POST /projects/{name}/action", endpoint(a.projectAction))
	m.HandleFunc("GET /sites", endpoint(a.siteList))
	m.HandleFunc("GET /sites/health", endpoint(a.siteHealthStatus))
	m.HandleFunc("GET /sites/dns", endpoint(a.siteDNSStatus))
	m.HandleFunc("GET /logs", endpoint(a.serviceLogs))
	m.HandleFunc("POST /sites", endpoint(a.siteSave))
	m.HandleFunc("POST /sites/{id}/toggle", endpoint(a.siteToggle))
	m.HandleFunc("DELETE /sites/{id}", endpoint(a.siteDelete))
	m.HandleFunc("GET /files", endpoint(a.fileList))
	m.HandleFunc("GET /files/content", endpoint(a.fileRead))
	m.HandleFunc("GET /files/download", endpoint(a.fileDownload))
	m.HandleFunc("POST /files/save", endpoint(a.fileSave))
	m.HandleFunc("POST /files/upload", endpoint(a.fileUpload))
	m.HandleFunc("POST /files/mkdir", endpoint(a.fileMkdir))
	m.HandleFunc("POST /files/rename", endpoint(a.fileRename))
	m.HandleFunc("POST /files/trash", endpoint(a.fileTrash))
	m.HandleFunc("GET /trash", endpoint(a.trashList))
	m.HandleFunc("POST /trash/{id}/restore", endpoint(a.trashRestore))
	m.HandleFunc("DELETE /trash/{id}", endpoint(a.trashDelete))
	m.HandleFunc("POST /trash/empty", endpoint(a.trashEmpty))
	m.HandleFunc("GET /catalog", endpoint(a.catalog))
	m.HandleFunc("GET /apps", endpoint(a.appList))
	m.HandleFunc("POST /apps", endpoint(a.appInstall))
	m.HandleFunc("POST /apps/{id}/action", endpoint(a.appAction))
	m.HandleFunc("GET /apps/{id}/settings", endpoint(a.appSettings))
	m.HandleFunc("POST /apps/{id}/settings", endpoint(a.appSettingsSave))
	m.HandleFunc("GET /apps/{id}/credentials", endpoint(a.appCredentials))
	m.HandleFunc("GET /databases", endpoint(a.databaseList))
	m.HandleFunc("POST /databases/{id}/query", endpoint(a.databaseQuery))
	m.HandleFunc("POST /databases/{id}/create", endpoint(a.databaseCreate))
	m.HandleFunc("GET /backups", endpoint(a.backupList))
	m.HandleFunc("POST /backups", endpoint(a.backupCreate))
	m.HandleFunc("POST /backups/all", endpoint(a.backupAll))
	m.HandleFunc("GET /backups/{id}/download", endpoint(a.backupDownload))
	m.HandleFunc("DELETE /backups/{id}", endpoint(a.backupDelete))
	m.HandleFunc("POST /backups/{id}/restore", endpoint(a.backupRestore))
	m.HandleFunc("POST /backups/{id}/upload", endpoint(a.backupUpload))
	m.HandleFunc("GET /history", endpoint(a.historyGet))
	m.HandleFunc("GET /offsite", endpoint(a.offsiteGet))
	m.HandleFunc("POST /offsite", endpoint(a.offsiteSave))
	m.HandleFunc("POST /offsite/test", endpoint(a.offsiteTest))
	m.HandleFunc("GET /schedules", endpoint(a.scheduleList))
	m.HandleFunc("POST /schedules", endpoint(a.scheduleSave))
	m.HandleFunc("POST /schedules/{id}/toggle", endpoint(a.scheduleToggle))
	m.HandleFunc("POST /schedules/{id}/run", endpoint(a.scheduleRunNow))
	m.HandleFunc("DELETE /schedules/{id}", endpoint(a.scheduleDelete))
	m.HandleFunc("GET /alerts", endpoint(a.alertList))
	m.HandleFunc("POST /alerts/config", endpoint(a.alertSave))
	m.HandleFunc("POST /alerts/test", endpoint(a.alertTest))
	m.HandleFunc("POST /alerts/check", endpoint(a.alertCheck))
	m.HandleFunc("GET /certificates", endpoint(a.certHandler))
	m.HandleFunc("GET /upgrade", endpoint(a.upgradeStatus))
	m.HandleFunc("POST /upgrade/check", endpoint(a.upgradeCheck))
	m.HandleFunc("POST /upgrade/config", endpoint(a.upgradeConfig))
	m.HandleFunc("POST /upgrade/start", endpoint(a.upgradeStart))
	m.HandleFunc("GET /access", endpoint(a.accessStatus))
	m.HandleFunc("POST /access/apply", endpoint(a.accessApply))
	m.HandleFunc("GET /jobs", endpoint(func(w http.ResponseWriter, r *http.Request) error { send(w, 200, a.jobs.list()); return nil }))
	m.HandleFunc("GET /terminal", a.terminal)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		r.Body = http.MaxBytesReader(w, r.Body, bodyLimit(r.Method, r.URL.Path))
		if r.Method != "GET" {
			log.Printf("agent_request method=%s path=%s", r.Method, r.URL.Path)
		}
		m.ServeHTTP(w, r)
	})
}

// bodyLimit allows large raw uploads on one route and small JSON elsewhere.
func bodyLimit(method, path string) int64 {
	if method == "POST" && (path == "/files/upload" || path == "/api/files/upload") {
		return maxUpload
	}
	return 12 << 20
}
func (a *agent) overview(w http.ResponseWriter, r *http.Request) error {
	host, e := a.metrics.read()
	hostErr := ""
	if e != nil {
		hostErr = "主机指标不可用"
	}
	containers, de := a.docker.list()
	dockerErr := ""
	if de != nil {
		dockerErr = de.Error()
	}
	a.mu.Lock()
	sites, se := a.readSites()
	apps, ae := a.readApps()
	hosts, sources, he := a.caddyInventory()
	external := a.externalHosts(sites, hosts)
	a.mu.Unlock()
	sitesErr, appsErr, caddyErr := "", "", ""
	if se != nil {
		sitesErr = "面板网站登记读取失败：" + se.Error()
	}
	if ae != nil {
		appsErr = "应用登记读取失败：" + ae.Error()
	}
	if he != nil {
		caddyErr = he.Error()
	}
	state, _ := a.runner(3*time.Second, "/usr/bin/systemctl", "is-active", "caddy.service")
	send(w, 200, map[string]any{"host": host, "host_error": hostErr, "containers": containers, "docker_error": dockerErr, "sites": sites, "sites_error": sitesErr, "external_hosts": external, "caddy_sources": sources, "caddy_error": caddyErr, "apps": publicApps(apps), "apps_error": appsErr, "caddy_state": state, "alerts_active": a.activeAlertCount(), "update_available": a.updateAvailable(), "jobs": a.jobs.list(), "time": time.Now(), "version": version, "demo": false})
	return nil
}
func decodeObject(b []byte, v any) error {
	if e := json.Unmarshal(b, v); e != nil {
		return bad(502, "Docker 返回格式错误")
	}
	return nil
}
