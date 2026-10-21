package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type dockerClient struct {
	HTTP *http.Client
	mu   sync.Mutex
	API  string
}

func newDocker(path string) *dockerClient {
	return &dockerClient{HTTP: &http.Client{Transport: unixTransport(path), Timeout: 40 * time.Second}}
}
func (d *dockerClient) wire(method, path string, value any, max int) ([]byte, error) {
	var b io.Reader
	if value != nil {
		raw, e := json.Marshal(value)
		if e != nil {
			return nil, e
		}
		b = bytes.NewReader(raw)
	}
	req, e := http.NewRequest(method, "http://docker"+path, b)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := d.HTTP.Do(req)
	if e != nil {
		return nil, bad(502, "Docker 未响应；操作结果请刷新核对")
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, int64(max+1)))
	if e != nil {
		return nil, bad(502, "读取 Docker 响应失败")
	}
	if len(raw) > max {
		return nil, bad(502, "Docker 响应过大，请缩小日志范围")
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 && resp.StatusCode != 204 && resp.StatusCode != 304 {
		return nil, bad(502, fmt.Sprintf("Docker 拒绝操作（HTTP %d），请经 SSH 检查服务", resp.StatusCode))
	}
	return raw, nil
}

// negotiate returns the versioned API prefix, probing the engine once.
func (d *dockerClient) negotiate() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.API == "" {
		raw, e := d.wire("GET", "/version", nil, 64<<10)
		if e != nil {
			return "", e
		}
		var v struct{ ApiVersion, MinAPIVersion string }
		if e = decodeObject(raw, &v); e != nil {
			return "", e
		}
		parse := func(s string) int {
			if !regexp.MustCompile(`^1\.\d{2,3}$`).MatchString(s) {
				return 0
			}
			n, _ := strconv.Atoi(strings.TrimPrefix(s, "1."))
			return n
		}
		max, min := parse(v.ApiVersion), parse(v.MinAPIVersion)
		if max < 41 {
			return "", bad(502, "需要 Docker API 1.41 或更高版本")
		}
		if max > 56 {
			max = 56
		}
		if min > max {
			return "", bad(502, "Docker 最低 API 版本超出面板支持范围")
		}
		d.API = fmt.Sprintf("/v1.%d", max)
	}
	return d.API, nil
}
func (d *dockerClient) call(method, path string, value any) ([]byte, error) {
	prefix, e := d.negotiate()
	if e != nil {
		return nil, e
	}
	return d.wire(method, prefix+path, value, 8<<20)
}

type container struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Image     string     `json:"image"`
	State     string     `json:"state"`
	Status    string     `json:"status"`
	Project   string     `json:"project"`
	Service   string     `json:"service"`
	Ports     []portInfo `json:"ports"`
	Protected bool       `json:"protected"`
	Oneoff    bool       `json:"oneoff"`
	Stack     string     `json:"stack,omitempty"`
}
type portInfo struct {
	IP       string `json:"ip"`
	Public   int    `json:"public"`
	Private  int    `json:"private"`
	Protocol string `json:"protocol"`
}

func (d *dockerClient) list() ([]container, error) {
	raw, e := d.call("GET", "/containers/json?all=1", nil)
	if e != nil {
		return nil, e
	}
	var rows []struct {
		Id                   string
		Names                []string
		Image, State, Status string
		Labels               map[string]string
		Ports                []struct {
			IP                      string
			PublicPort, PrivatePort int
			Type                    string
		}
	}
	if e = decodeObject(raw, &rows); e != nil {
		return nil, e
	}
	out := make([]container, 0, len(rows))
	for _, row := range rows {
		if !containerID.MatchString(row.Id) {
			return nil, bad(502, "Docker 返回的容器 ID 无效")
		}
		name := row.Id[:12]
		if len(row.Names) > 0 {
			name = strings.TrimPrefix(row.Names[0], "/")
		}
		c := container{ID: row.Id, Name: name, Image: row.Image, State: row.State, Status: row.Status, Project: row.Labels["com.docker.compose.project"], Service: row.Labels["com.docker.compose.service"], Protected: strings.EqualFold(row.Labels["junpanel.protected"], "true"), Oneoff: strings.EqualFold(row.Labels["com.docker.compose.oneoff"], "true"), Stack: row.Labels["junpanel.stack"], Ports: []portInfo{}}
		for _, p := range row.Ports {
			c.Ports = append(c.Ports, portInfo{p.IP, p.PublicPort, p.PrivatePort, p.Type})
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (d *dockerClient) find(id string) (container, error) {
	if !containerID.MatchString(id) {
		return container{}, bad(400, "容器 ID 无效")
	}
	rows, e := d.list()
	if e != nil {
		return container{}, e
	}
	for _, c := range rows {
		if c.ID == id {
			return c, nil
		}
	}
	return container{}, bad(404, "容器不存在")
}
func (d *dockerClient) action(id, action string) error {
	if action != "start" && action != "stop" && action != "restart" {
		return bad(400, "仅支持启动、停止、重启")
	}
	c, e := d.find(id)
	if e != nil {
		return e
	}
	if c.Protected {
		return bad(403, "此容器带有保护标签")
	}
	if c.State != "running" && c.State != "exited" && c.State != "created" {
		return bad(409, "请先通过 SSH 诊断该容器状态")
	}
	if action == "start" && c.State == "running" || action == "stop" && c.State != "running" {
		return nil
	}
	suffix := ""
	if action != "start" {
		suffix = "?t=10"
	}
	_, e = d.call("POST", "/containers/"+id+"/"+action+suffix, nil)
	return e
}
func (d *dockerClient) inspect(id string) (map[string]any, error) {
	if !containerID.MatchString(id) {
		return nil, bad(400, "容器 ID 无效")
	}
	b, e := d.call("GET", "/containers/"+id+"/json", nil)
	if e != nil {
		return nil, e
	}
	var v map[string]any
	e = decodeObject(b, &v)
	return v, e
}
func decodeDockerLogs(b []byte, tty bool) (string, error) {
	if tty {
		return cleanText(string(b)), nil
	}
	var out bytes.Buffer
	for len(b) > 0 {
		if len(b) < 8 || b[0] > 2 || !bytes.Equal(b[1:4], []byte{0, 0, 0}) {
			return "", bad(502, "日志流格式无效")
		}
		size := int(binary.BigEndian.Uint32(b[4:8]))
		b = b[8:]
		if size > len(b) {
			return "", bad(502, "日志流不完整")
		}
		out.Write(b[:size])
		b = b[size:]
	}
	return cleanText(out.String()), nil
}
func (a *agent) containers(w http.ResponseWriter, r *http.Request) error {
	v, e := a.docker.list()
	if e != nil {
		return e
	}
	send(w, 200, v)
	return nil
}
func (a *agent) containerLogs(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if _, e := a.docker.find(id); e != nil {
		return e
	}
	tail := r.URL.Query().Get("tail")
	if tail != "50" && tail != "100" && tail != "200" && tail != "500" {
		return bad(400, "日志行数仅支持 50/100/200/500")
	}
	info, e := a.docker.inspect(id)
	if e != nil {
		return e
	}
	tty := false
	if config, ok := info["Config"].(map[string]any); ok {
		tty, _ = config["Tty"].(bool)
	}
	b, e := a.docker.call("GET", "/containers/"+id+"/logs?stdout=1&stderr=1&timestamps=1&follow=0&tail="+tail, nil)
	if e != nil {
		return e
	}
	text, e := decodeDockerLogs(b, tty)
	if e != nil {
		return e
	}
	send(w, 200, map[string]string{"text": text})
	return nil
}
func (a *agent) containerAction(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Action string `json:"action"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	id := r.PathValue("id")
	if _, e := a.docker.find(id); e != nil {
		return e
	}
	if req.Action == "stop" || req.Action == "restart" {
		a.expectStop(id)
	}
	j, e := a.jobs.start("容器 · "+req.Action, func(ctx context.Context, out *limitedBuffer) error {
		if e := a.docker.action(id, req.Action); e != nil {
			return e
		}
		out.Write([]byte("操作完成，请刷新状态核对。\n"))
		return nil
	})
	if e != nil {
		return e
	}
	send(w, 202, j)
	return nil
}

type project struct {
	Name       string      `json:"name"`
	Containers []container `json:"containers"`
	Running    int         `json:"running"`
}

func grouped(rows []container) []project {
	groups := map[string]*project{}
	for _, c := range rows {
		if c.Project == "" {
			continue
		}
		if groups[c.Project] == nil {
			groups[c.Project] = &project{Name: c.Project, Containers: []container{}}
		}
		p := groups[c.Project]
		p.Containers = append(p.Containers, c)
		if c.State == "running" {
			p.Running++
		}
	}
	out := make([]project, 0, len(groups))
	for _, p := range groups {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (a *agent) projects(w http.ResponseWriter, r *http.Request) error {
	rows, e := a.docker.list()
	if e != nil {
		return e
	}
	send(w, 200, grouped(rows))
	return nil
}
func (a *agent) projectAction(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`).MatchString(name) {
		return bad(400, "项目名无效")
	}
	var req struct {
		Action string `json:"action"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if req.Action != "start" && req.Action != "stop" && req.Action != "restart" {
		return bad(400, "操作无效")
	}
	rows, e := a.docker.list()
	if e != nil {
		return e
	}
	targets := []container{}
	for _, c := range rows {
		if c.Project == name && !c.Oneoff {
			if c.Protected {
				return bad(403, "项目中有受保护容器，整组操作已拒绝")
			}
			targets = append(targets, c)
		}
	}
	if len(targets) == 0 || len(targets) > 30 {
		return bad(400, "只支持包含 1–30 个常规容器的已有项目")
	}
	if req.Action == "stop" || req.Action == "restart" {
		for _, c := range targets {
			a.expectStop(c.ID)
		}
	}
	j, e := a.jobs.start("项目 · "+name, func(ctx context.Context, out *limitedBuffer) error {
		for i, c := range targets {
			select {
			case <-ctx.Done():
				return bad(504, "项目操作超时")
			default:
			}
			fmt.Fprintf(out, "[%d/%d] %s: %s\n", i+1, len(targets), c.Name, req.Action)
			if e := a.docker.action(c.ID, req.Action); e != nil {
				return bad(502, "项目部分完成，停止后续操作："+e.Error())
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	send(w, 202, j)
	return nil
}
func (a *agent) images(w http.ResponseWriter, r *http.Request) error {
	b, e := a.docker.call("GET", "/images/json", nil)
	if e != nil {
		return e
	}
	var rows []struct {
		Id       string
		RepoTags []string
		Size     int64
		Created  int64
	}
	if e = decodeObject(b, &rows); e != nil {
		return e
	}
	out := []map[string]any{}
	for _, i := range rows {
		out = append(out, map[string]any{"id": strings.TrimPrefix(i.Id, "sha256:"), "tags": i.RepoTags, "size": i.Size, "created": i.Created})
	}
	send(w, 200, out)
	return nil
}
func (a *agent) imagePull(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Image string `json:"image"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if len(req.Image) > 200 || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:@-]+$`).MatchString(req.Image) {
		return bad(400, "镜像名称无效")
	}
	j, e := a.jobs.start("拉取镜像 · "+req.Image, func(ctx context.Context, out *limitedBuffer) error {
		cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		e := a.stream(cmdCtx, "", out, nil, "docker", "pull", req.Image)
		if e != nil {
			return bad(502, "镜像拉取失败，请检查名称与网络")
		}
		return nil
	})
	if e != nil {
		return e
	}
	send(w, 202, j)
	return nil
}
func (a *agent) imageDelete(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !containerID.MatchString(id) {
		return bad(400, "镜像 ID 无效")
	}
	_, e := a.docker.call("DELETE", "/images/sha256:"+url.PathEscape(id)+"?force=0", nil)
	if e != nil {
		return e
	}
	send(w, 200, map[string]any{"ok": true})
	return nil
}
