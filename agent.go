package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type agentConfig struct{ State, Root, Caddy, Fragments, Socket string }
type agent struct {
	cfg     agentConfig
	docker  *dockerClient
	jobs    *jobQueue
	mu      sync.Mutex
	metrics *hostSampler
	runner  func(time.Duration, string, ...string) (string, error)
	capture func(time.Duration, string, ...string) (string, error)
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
	a := &agent{cfg: cfg, docker: newDocker("/var/run/docker.sock"), jobs: newQueue(), metrics: &hostSampler{}, runner: command}
	rows, e := a.readApps()
	if e != nil {
		return nil, e
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
	return a, nil
}
func (a *agent) routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /overview", endpoint(a.overview))
	m.HandleFunc("GET /containers", endpoint(a.containers))
	m.HandleFunc("GET /containers/{id}/logs", endpoint(a.containerLogs))
	m.HandleFunc("POST /containers/{id}/action", endpoint(a.containerAction))
	m.HandleFunc("GET /images", endpoint(a.images))
	m.HandleFunc("POST /images/pull", endpoint(a.imagePull))
	m.HandleFunc("DELETE /images/{id}", endpoint(a.imageDelete))
	m.HandleFunc("GET /projects", endpoint(a.projects))
	m.HandleFunc("POST /projects/{name}/action", endpoint(a.projectAction))
	m.HandleFunc("GET /sites", endpoint(a.siteList))
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
	m.HandleFunc("GET /catalog", endpoint(a.catalog))
	m.HandleFunc("GET /apps", endpoint(a.appList))
	m.HandleFunc("POST /apps", endpoint(a.appInstall))
	m.HandleFunc("POST /apps/{id}/action", endpoint(a.appAction))
	m.HandleFunc("GET /apps/{id}/credentials", endpoint(a.appCredentials))
	m.HandleFunc("GET /databases", endpoint(a.databaseList))
	m.HandleFunc("POST /databases/{id}/query", endpoint(a.databaseQuery))
	m.HandleFunc("POST /databases/{id}/create", endpoint(a.databaseCreate))
	m.HandleFunc("GET /databases/{id}/export", endpoint(a.databaseExport))
	m.HandleFunc("GET /jobs", endpoint(func(w http.ResponseWriter, r *http.Request) error { send(w, 200, a.jobs.list()); return nil }))
	m.HandleFunc("GET /terminal", a.terminal)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
		if r.Method != "GET" {
			log.Printf("agent_request method=%s path=%s", r.Method, r.URL.Path)
		}
		m.ServeHTTP(w, r)
	})
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
	sites, _ := a.readSites()
	apps, _ := a.readApps()
	state, _ := a.runner(3*time.Second, "/usr/bin/systemctl", "is-active", "caddy.service")
	send(w, 200, map[string]any{"host": host, "host_error": hostErr, "containers": containers, "docker_error": dockerErr, "sites": sites, "apps": publicApps(apps), "caddy_state": state, "jobs": a.jobs.list(), "time": time.Now(), "version": version, "demo": false})
	return nil
}
func decodeObject(b []byte, v any) error {
	if e := json.Unmarshal(b, v); e != nil {
		return bad(502, "Docker 返回格式错误")
	}
	return nil
}
