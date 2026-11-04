package main

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A Compose project that was started before the panel existed can be adopted as an app. Adopting only
// writes a record: the containers, the volumes and the project's own files are never touched, and the
// panel's actions go to the containers themselves (start, stop, restart), not to a compose file it did
// not write. Releasing the app deletes the record again.

var composeProject = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

// appProject is the Compose project an app's containers belong to.
func appProject(r appRecord) string {
	if r.Adopted {
		return r.Project
	}
	return "junpanel-" + r.ID
}

type adoptable struct {
	Project    string   `json:"project"`
	Containers []string `json:"containers"`
	Running    int      `json:"running"`
	Images     []string `json:"images"`
	Port       int      `json:"port"`
}

// publishedPort is the first TCP port the project publishes on this machine, or 0.
func publishedPort(rows []container) int {
	for _, c := range rows {
		for _, p := range c.Ports {
			if p.Protocol == "tcp" && p.Public > 0 {
				return p.Public
			}
		}
	}
	return 0
}

// projectContainers are the regular containers of a project the panel may act on.
func projectContainers(all []container, project string) []container {
	out := []container{}
	for _, c := range all {
		if c.Project == project && !c.Oneoff {
			out = append(out, c)
		}
	}
	return out
}

// adoptableProjects lists the projects that the panel does not manage yet.
func adoptableProjects(all []container, apps []appRecord) []adoptable {
	taken := map[string]bool{}
	for _, r := range apps {
		if r.Adopted && !r.Removed {
			taken[r.Project] = true
		}
	}
	managed := map[string]bool{}
	for _, c := range all {
		if c.Stack != "" || c.Protected {
			managed[c.Project] = true
		}
	}
	out := []adoptable{}
	for _, p := range grouped(all) {
		if strings.HasPrefix(p.Name, "junpanel-") || taken[p.Name] || managed[p.Name] {
			continue
		}
		rows := projectContainers(all, p.Name)
		if len(rows) == 0 || len(rows) > 30 {
			continue
		}
		item := adoptable{Project: p.Name, Containers: []string{}, Images: []string{}, Port: publishedPort(rows)}
		seen := map[string]bool{}
		for _, c := range rows {
			item.Containers = append(item.Containers, c.Name)
			if c.State == "running" {
				item.Running++
			}
			if !seen[c.Image] {
				seen[c.Image] = true
				item.Images = append(item.Images, c.Image)
			}
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Project < out[j].Project })
	return out
}

var idUnsafe = regexp.MustCompile(`[^a-z0-9-]+`)

// adoptedID turns a project name into an app identifier that no other app uses.
func adoptedID(project string, apps []appRecord) string {
	id := strings.Trim(idUnsafe.ReplaceAllString(strings.ToLower(project), "-"), "-")
	if id == "" || id[0] < 'a' || id[0] > 'z' {
		id = "p-" + id
	}
	if len(id) > 28 {
		id = strings.TrimRight(id[:28], "-")
	}
	if len(id) < 2 {
		id += "-x"
	}
	used := map[string]bool{}
	for _, r := range apps {
		used[r.ID] = true
	}
	candidate := id
	for n := 2; used[candidate]; n++ {
		candidate = fmt.Sprintf("%s-%d", id, n)
	}
	return candidate
}

func (a *agent) appAdoptable(w http.ResponseWriter, r *http.Request) error {
	a.mu.Lock()
	apps, e := a.readApps()
	a.mu.Unlock()
	if e != nil {
		return e
	}
	all, e := a.docker.list()
	if e != nil {
		return e
	}
	send(w, 200, adoptableProjects(all, apps))
	return nil
}

func (a *agent) appAdopt(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Project string `json:"project"`
		Name    string `json:"name"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if !composeProject.MatchString(req.Project) {
		return bad(400, "项目名无效")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = req.Project
	}
	if len(name) > 60 {
		return bad(400, "应用显示名称需为 1–60 字符")
	}
	all, e := a.docker.list()
	if e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	apps, e := a.readApps()
	if e != nil {
		return e
	}
	var item *adoptable
	for _, p := range adoptableProjects(all, apps) {
		if p.Project == req.Project {
			p := p
			item = &p
		}
	}
	if item == nil {
		return bad(409, "这个项目不能接管：它不存在、已被接管，或由面板自己管理")
	}
	record := appRecord{ID: adoptedID(req.Project, apps), Name: name, Port: item.Port, Created: time.Now(), Adopted: true, Project: req.Project}
	if e = a.storeApps(append(apps, record)); e != nil {
		return e
	}
	send(w, 200, map[string]any{"id": record.ID, "name": record.Name})
	return nil
}

// adoptedJob runs an action on an adopted app. The caller holds a.mu and has checked that the app is idle.
func (a *agent) adoptedJob(rows []appRecord, i int, action string, done func(error)) (job, error) {
	record := rows[i]
	if action != "start" && action != "stop" && action != "restart" && action != "release" {
		return job{}, bad(409, "接管的项目由它自己的 compose 文件管理，面板只提供启动、停止、重启和取消接管")
	}
	all, e := a.docker.list()
	if e != nil {
		return job{}, e
	}
	targets := projectContainers(all, record.Project)
	for _, c := range targets {
		if c.Protected {
			return job{}, bad(403, "项目中有受保护容器，操作已拒绝")
		}
	}
	if action != "release" {
		if len(targets) == 0 {
			return job{}, bad(409, "这个项目的容器已经不存在，可以取消接管")
		}
		if action != "start" {
			ids := make([]string, 0, len(targets))
			for _, c := range targets {
				ids = append(ids, c.ID)
			}
			a.expectStop(ids...)
		}
	}
	rows[i].Busy = true
	if e = a.storeApps(rows); e != nil {
		return job{}, e
	}
	j, e := a.jobs.start("应用 · "+record.Name+" · "+action, func(ctx context.Context, out *limitedBuffer) (result error) {
		if done != nil {
			defer func() { done(result) }()
		}
		var failure error
		if action != "release" {
			for n, c := range targets {
				select {
				case <-ctx.Done():
					failure = bad(504, "操作超时")
				default:
				}
				if failure != nil {
					break
				}
				fmt.Fprintf(out, "[%d/%d] %s: %s\n", n+1, len(targets), c.Name, action)
				if e := a.docker.action(c.ID, action); e != nil {
					failure = bad(502, "部分完成，已停止后续操作："+e.Error())
				}
			}
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		cur, e := a.readApps()
		if e != nil {
			return e
		}
		kept := cur[:0]
		for _, r := range cur {
			if r.ID == record.ID {
				if action == "release" {
					continue
				}
				r.Busy, r.LastError = false, ""
				if failure != nil {
					r.LastError = "上次操作未完成，请检查任务日志"
				}
			}
			kept = append(kept, r)
		}
		if e = a.storeApps(kept); e != nil {
			return e
		}
		if action == "release" {
			a.dropAppSchedules(record.ID, out)
			fmt.Fprintf(out, "已取消接管，项目 %s 的容器和数据没有任何变化。\n", record.Project)
		}
		return failure
	})
	if e != nil {
		rows[i].Busy = false
		a.storeApps(rows)
		return job{}, e
	}
	return j, nil
}
