package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Compose stacks the panel creates and edits. A stack file is first read by
// `docker compose config` running as nobody, so it can only pull in files any user
// could read (env_file, include, extends, secrets). The normalized result is then
// checked against an allow-list and deployed exactly as checked, which keeps a panel
// login from turning into root on the host.

const maxStackFile = 256 << 10

func (a *agent) stackDir(name string) string  { return filepath.Join(a.cfg.State, "stacks", name) }
func (a *agent) stackData(name string) string { return filepath.Join(a.cfg.Root, "stacks", name) }

// runAsNobody runs a command as the nobody user with a minimal environment and returns
// stdout and stderr separately.
func runAsNobody(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, error) {
	u, e := user.Lookup("nobody")
	if e != nil {
		return "", "", bad(500, "系统缺少 nobody 用户，无法安全解析 Compose 文件")
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}, env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}}}
	stdout := &limitedBuffer{Max: 4 << 20}
	stderr := &limitedBuffer{Max: 16 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	e = cmd.Run()
	return stdout.String(), stderr.String(), e
}

// normalizeStack resolves a stack file with docker compose, without root rights.
func (a *agent) normalizeStack(ctx context.Context, name, source string) (map[string]any, error) {
	tmp, e := os.MkdirTemp("", "junpanel-stack-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(tmp)
	if e = os.Chmod(tmp, 0755); e != nil {
		return nil, e
	}
	file := filepath.Join(tmp, "compose.yaml")
	if e = os.WriteFile(file, []byte(source), 0644); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stdout, stderr, e := a.composeConfig(ctx, tmp, []string{"HOME=" + tmp, "DOCKER_CONFIG=" + filepath.Join(tmp, ".docker")}, "docker", "compose", "--project-directory", a.stackData(name), "--file", file, "--project-name", name, "config", "--format", "json")
	if e != nil {
		lines := []string{}
		for _, line := range strings.Split(stderr, "\n") {
			if !strings.Contains(line, "level=warning") {
				lines = append(lines, line)
			}
		}
		msg := strings.TrimSpace(strings.ReplaceAll(cleanText(strings.Join(lines, "\n")), tmp+"/", ""))
		if msg == "" {
			msg = "docker compose 无法读取此文件"
		}
		if strings.Contains(msg, "permission denied") {
			msg += "（为安全起见，Compose 文件只能引用所有用户都可读的文件）"
		}
		return nil, bad(400, "配置无法解析："+truncate(msg, 1500))
	}
	var cfg map[string]any
	dec := json.NewDecoder(strings.NewReader(stdout))
	dec.UseNumber()
	if e = dec.Decode(&cfg); e != nil {
		return nil, bad(502, "docker compose 输出格式异常")
	}
	return cfg, nil
}

// insideRoot reports whether p lies within root, following any existing symlinks.
func insideRoot(root, p string) bool {
	if !filepath.IsAbs(p) {
		return false
	}
	p = filepath.Clean(p)
	if p != root && !strings.HasPrefix(p, root+string(filepath.Separator)) {
		return false
	}
	probe := p
	for {
		if _, e := os.Lstat(probe); e == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return false
		}
		probe = parent
	}
	real, e := filepath.EvalSymlinks(probe)
	if e != nil {
		return false
	}
	realRoot, e := filepath.EvalSymlinks(root)
	if e != nil {
		return false
	}
	return real == realRoot || strings.HasPrefix(real, realRoot+string(filepath.Separator))
}

// Service keys that cannot reach the host by themselves. Anything else is refused,
// so a future Compose option does not slip through unchecked.
var stackServiceKeys = map[string]bool{
	"annotations": true, "attach": true, "blkio_config": true, "cap_add": true, "cap_drop": true, "cgroup": true, "command": true, "configs": true,
	"container_name": true, "cpu_count": true, "cpu_percent": true, "cpu_period": true, "cpu_quota": true, "cpu_rt_period": true, "cpu_rt_runtime": true,
	"cpu_shares": true, "cpus": true, "cpuset": true, "depends_on": true, "deploy": true, "devices": true, "device_cgroup_rules": true, "dns": true,
	"dns_opt": true, "dns_search": true, "domainname": true, "entrypoint": true, "environment": true, "expose": true, "extra_hosts": true,
	"group_add": true, "healthcheck": true, "hostname": true, "image": true, "init": true, "ipc": true, "labels": true, "links": true,
	"logging": true, "mac_address": true, "mem_limit": true, "mem_reservation": true, "mem_swappiness": true, "memswap_limit": true,
	"network_mode": true, "networks": true, "oom_kill_disable": true, "oom_score_adj": true, "pid": true, "pids_limit": true, "platform": true,
	"ports": true, "post_start": true, "pre_stop": true, "privileged": true, "profiles": true, "pull_policy": true, "read_only": true,
	"restart": true, "scale": true, "secrets": true, "security_opt": true, "shm_size": true, "stdin_open": true, "stop_grace_period": true,
	"stop_signal": true, "storage_opt": true, "sysctls": true, "tmpfs": true, "tty": true, "ulimits": true, "user": true, "userns_mode": true,
	"uts": true, "volumes": true, "volumes_from": true, "working_dir": true,
}
var stackTopKeys = map[string]bool{"name": true, "services": true, "networks": true, "volumes": true, "secrets": true, "configs": true}

// Capabilities that are already in Docker's default set; listing them changes nothing.
var harmlessCaps = map[string]bool{"CHOWN": true, "DAC_OVERRIDE": true, "FOWNER": true, "FSETID": true, "KILL": true, "SETGID": true, "SETUID": true, "SETPCAP": true, "NET_BIND_SERVICE": true, "NET_RAW": true, "SYS_CHROOT": true, "MKNOD": true, "AUDIT_WRITE": true, "SETFCAP": true}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}
func str(v any) string { s, _ := v.(string); return s }

// checkStack lists every reason a normalized stack may not run under the panel.
func checkStack(cfg map[string]any, root string) []string {
	problems := []string{}
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	keys := func(m map[string]any) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	for _, k := range keys(cfg) {
		if !stackTopKeys[k] && !strings.HasPrefix(k, "x-") {
			add("不支持顶层配置项 %s", k)
		}
	}
	services, _ := cfg["services"].(map[string]any)
	if len(services) == 0 {
		add("至少需要一个服务（services）")
	}
	if len(services) > 30 {
		add("一个编排最多 30 个服务")
	}
	for _, name := range keys(services) {
		svc, ok := services[name].(map[string]any)
		if !ok {
			add("服务 %s 的格式无效", name)
			continue
		}
		where := "服务 " + name + "："
		for _, k := range keys(svc) {
			if !stackServiceKeys[k] && !strings.HasPrefix(k, "x-") {
				switch k {
				case "build":
					add("%s不支持在面板中构建镜像（build），请先构建并推送镜像，再用 image 引用", where)
				case "env_file":
					add("%s请把 env_file 的内容直接写进 environment", where)
				default:
					add("%s不支持配置项 %s", where, k)
				}
			}
		}
		if str(svc["image"]) == "" {
			add("%s需要指定镜像（image）", where)
		}
		if b, _ := svc["privileged"].(bool); b {
			add("%s不允许特权模式（privileged）", where)
		}
		for _, ns := range []string{"pid", "ipc", "uts", "userns_mode", "cgroup"} {
			v := str(svc[ns])
			if v == "host" || ns == "pid" && strings.HasPrefix(v, "container:") {
				add("%s不允许 %s: %s（会共享宿主机或其它容器的命名空间）", where, ns, v)
			}
		}
		if mode := str(svc["network_mode"]); mode == "host" || strings.HasPrefix(mode, "container:") {
			add("%s不允许 network_mode: %s", where, mode)
		}
		if caps, ok := svc["cap_add"].([]any); ok {
			for _, c := range caps {
				name := strings.TrimPrefix(strings.ToUpper(str(c)), "CAP_")
				if !harmlessCaps[name] {
					add("%s不允许增加权限 %s（cap_add）", where, str(c))
				}
			}
		}
		if !isEmpty(svc["devices"]) || !isEmpty(svc["device_cgroup_rules"]) {
			add("%s不允许直接访问宿主机设备（devices）", where)
		}
		if deploy, ok := svc["deploy"].(map[string]any); ok {
			if res, ok := deploy["resources"].(map[string]any); ok {
				if r, ok := res["reservations"].(map[string]any); ok && !isEmpty(r["devices"]) {
					add("%s不允许预留宿主机设备（deploy.resources.reservations.devices）", where)
				}
			}
		}
		if opts, ok := svc["security_opt"].([]any); ok {
			for _, o := range opts {
				s := strings.ToLower(str(o))
				if strings.Contains(s, "unconfined") || strings.Contains(s, "label=disable") || strings.Contains(s, "label:disable") {
					add("%s不允许关闭安全限制（security_opt: %s）", where, str(o))
				}
			}
		}
		if from, ok := svc["volumes_from"].([]any); ok {
			for _, f := range from {
				if strings.HasPrefix(str(f), "container:") {
					add("%s不允许从其它容器挂载卷（%s）", where, str(f))
				}
			}
		}
		if vols, ok := svc["volumes"].([]any); ok {
			for _, v := range vols {
				m, _ := v.(map[string]any)
				switch kind := str(m["type"]); kind {
				case "volume", "tmpfs", "image":
				case "bind":
					if src := str(m["source"]); !insideRoot(root, src) {
						add("%s只能挂载 %s 内的目录，不允许挂载 %s", where, root, src)
					}
				default:
					add("%s不支持的挂载类型 %s", where, kind)
				}
			}
		}
	}
	for _, section := range []string{"secrets", "configs"} {
		entries, _ := cfg[section].(map[string]any)
		for _, name := range keys(entries) {
			m, _ := entries[name].(map[string]any)
			if f := str(m["file"]); f != "" && !insideRoot(root, f) {
				add("%s %s：只能引用 %s 内的文件，不允许 %s", section, name, root, f)
			}
		}
	}
	vols, _ := cfg["volumes"].(map[string]any)
	for _, name := range keys(vols) {
		m, _ := vols[name].(map[string]any)
		if opts, ok := m["driver_opts"].(map[string]any); ok {
			if dev := str(opts["device"]); strings.HasPrefix(dev, "/") && !insideRoot(root, dev) {
				add("卷 %s：只能绑定 %s 内的目录，不允许 %s", name, root, dev)
			}
		}
	}
	nets, _ := cfg["networks"].(map[string]any)
	for _, name := range keys(nets) {
		m, _ := nets[name].(map[string]any)
		if str(m["name"]) == "host" || str(m["driver"]) == "host" {
			add("网络 %s：不允许使用宿主机网络", name)
		}
	}
	return problems
}

// labelStack marks every service with the stack it belongs to.
func labelStack(cfg map[string]any, name string) {
	services, _ := cfg["services"].(map[string]any)
	for _, v := range services {
		svc, _ := v.(map[string]any)
		if svc == nil {
			continue
		}
		labels, _ := svc["labels"].(map[string]any)
		if labels == nil {
			labels = map[string]any{}
		}
		labels["junpanel.stack"] = name
		svc["labels"] = labels
	}
}

type stackInfo struct {
	Name       string      `json:"name"`
	Updated    time.Time   `json:"updated"`
	Services   []string    `json:"services"`
	Containers []container `json:"containers"`
	Running    int         `json:"running"`
	Busy       bool        `json:"busy"`
	LastError  string      `json:"last_error,omitempty"`
}

func validStackName(name string) error {
	if !shortName.MatchString(name) || strings.HasPrefix(name, "junpanel") {
		return bad(400, "编排名称需为 2–32 位小写字母、数字或中划线，以字母开头，且不能以 junpanel 开头")
	}
	return nil
}

func (a *agent) stackNames() ([]string, error) {
	entries, e := os.ReadDir(filepath.Join(a.cfg.State, "stacks"))
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	out := []string{}
	for _, entry := range entries {
		if entry.IsDir() && validStackName(entry.Name()) == nil {
			out = append(out, entry.Name())
		}
	}
	return out, nil
}

// prepareStack resolves and checks a stack file and returns the config to deploy.
func (a *agent) prepareStack(ctx context.Context, name, source string) (map[string]any, []string, error) {
	if len(source) == 0 || len(source) > maxStackFile || strings.ContainsRune(source, 0) {
		return nil, nil, bad(400, "Compose 文件需为 1 字节到 256 KB 的文本")
	}
	cfg, e := a.normalizeStack(ctx, name, source)
	if e != nil {
		return nil, nil, e
	}
	if problems := checkStack(cfg, a.cfg.Root); len(problems) > 0 {
		return nil, problems, nil
	}
	labelStack(cfg, name)
	return cfg, nil, nil
}

func (a *agent) stackList(w http.ResponseWriter, r *http.Request) error {
	names, e := a.stackNames()
	if e != nil {
		return e
	}
	rows, de := a.docker.list()
	out := []stackInfo{}
	for _, name := range names {
		info := stackInfo{Name: name, Services: []string{}, Containers: []container{}}
		if st, e := os.Stat(filepath.Join(a.stackDir(name), "compose.yaml")); e == nil {
			info.Updated = st.ModTime()
		}
		var cfg map[string]any
		if loadJSON(filepath.Join(a.stackDir(name), "compose.json"), &cfg) == nil {
			services, _ := cfg["services"].(map[string]any)
			for s := range services {
				info.Services = append(info.Services, s)
			}
			sort.Strings(info.Services)
		}
		for _, c := range rows {
			if c.Project == name && c.Stack == name {
				info.Containers = append(info.Containers, c)
				if c.State == "running" {
					info.Running++
				}
			}
		}
		a.stackMu.Lock()
		info.Busy = a.stackBusy[name]
		info.LastError = a.stackErr[name]
		a.stackMu.Unlock()
		out = append(out, info)
	}
	result := map[string]any{"stacks": out, "root": filepath.Join(a.cfg.Root, "stacks")}
	if de != nil {
		result["docker_error"] = de.Error()
	}
	send(w, 200, result)
	return nil
}

func (a *agent) stackRead(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	if e := validStackName(name); e != nil {
		return e
	}
	b, e := os.ReadFile(filepath.Join(a.stackDir(name), "compose.yaml"))
	if os.IsNotExist(e) {
		return bad(404, "编排不存在")
	}
	if e != nil {
		return e
	}
	send(w, 200, map[string]any{"name": name, "source": string(b), "data": a.stackData(name)})
	return nil
}

func (a *agent) stackCheck(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if e := validStackName(req.Name); e != nil {
		return e
	}
	cfg, problems, e := a.prepareStack(r.Context(), req.Name, req.Source)
	if e != nil {
		return e
	}
	services := []string{}
	if cfg != nil {
		m, _ := cfg["services"].(map[string]any)
		for s := range m {
			services = append(services, s)
		}
		sort.Strings(services)
	}
	send(w, 200, map[string]any{"ok": len(problems) == 0, "problems": problems, "services": services})
	return nil
}

// stackSave stores a checked stack and, when asked, deploys it.
func (a *agent) stackSave(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name   string `json:"name"`
		Source string `json:"source"`
		Create bool   `json:"create"`
		Deploy bool   `json:"deploy"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if e := validStackName(req.Name); e != nil {
		return e
	}
	_, statErr := os.Stat(a.stackDir(req.Name))
	if req.Create && statErr == nil {
		return bad(409, "已有同名编排")
	}
	if !req.Create && os.IsNotExist(statErr) {
		return bad(404, "编排不存在")
	}
	if req.Create {
		rows, e := a.docker.list()
		if e != nil {
			return e
		}
		for _, c := range rows {
			if c.Project == req.Name {
				return bad(409, "Docker 中已有同名的 Compose 项目，请换一个名称")
			}
		}
	}
	cfg, problems, e := a.prepareStack(r.Context(), req.Name, req.Source)
	if e != nil {
		return e
	}
	if len(problems) > 0 {
		return bad(400, "配置未通过检查：\n"+strings.Join(problems, "\n"))
	}
	if !a.reserveStack(req.Name) {
		return bad(409, "此编排已有操作进行中")
	}
	reserved := true
	defer func() {
		if reserved {
			a.releaseStack(req.Name, "")
		}
	}()
	if e = os.MkdirAll(a.stackDir(req.Name), 0700); e != nil {
		return e
	}
	if e = os.MkdirAll(a.stackData(req.Name), 0755); e != nil {
		return e
	}
	if e = atomicJSON(filepath.Join(a.stackDir(req.Name), "compose.json"), cfg, 0600); e != nil {
		return e
	}
	if e = atomicBytes(filepath.Join(a.stackDir(req.Name), "compose.yaml"), []byte(req.Source), 0600); e != nil {
		return e
	}
	if !req.Deploy {
		send(w, 200, map[string]any{"ok": true})
		return nil
	}
	j, e := a.runStack(req.Name, "deploy")
	if e != nil {
		return e
	}
	reserved = false
	send(w, 202, j)
	return nil
}

func (a *agent) reserveStack(name string) bool {
	a.stackMu.Lock()
	defer a.stackMu.Unlock()
	if a.stackBusy[name] {
		return false
	}
	a.stackBusy[name] = true
	return true
}
func (a *agent) releaseStack(name, lastError string) {
	a.stackMu.Lock()
	defer a.stackMu.Unlock()
	delete(a.stackBusy, name)
	if lastError == "" {
		delete(a.stackErr, name)
	} else {
		a.stackErr[name] = lastError
	}
}

func (a *agent) stackCompose(name string) []string {
	return []string{"compose", "--project-name", name, "--project-directory", a.stackData(name), "--file", filepath.Join(a.stackDir(name), "compose.json")}
}

// runStack starts a stack action as a job; the stack must already be reserved.
func (a *agent) runStack(name, action string) (job, error) {
	title := map[string]string{"deploy": "部署", "start": "启动", "stop": "停止", "restart": "重启", "pull": "更新镜像", "down": "停用", "delete": "删除"}[action]
	if action == "stop" || action == "restart" || action == "down" || action == "delete" || action == "pull" || action == "deploy" {
		if rows, e := a.docker.list(); e == nil {
			for _, c := range rows {
				if c.Stack == name {
					a.expectStop(c.ID)
				}
			}
		}
	}
	j, e := a.jobs.start("编排 · "+name+" · "+title, func(ctx context.Context, out *limitedBuffer) (result error) {
		defer func() {
			msg := ""
			if result != nil {
				msg = "上次操作未完成，请查看任务日志"
			}
			a.releaseStack(name, msg)
		}()
		base := a.stackCompose(name)
		run := func(args ...string) error {
			return a.stream(ctx, a.stackData(name), out, nil, "docker", append(append([]string{}, base...), args...)...)
		}
		var e error
		switch action {
		case "deploy":
			fmt.Fprintln(out, "正在部署；首次下载镜像可能需要几分钟。")
			e = run("up", "--detach", "--remove-orphans", "--pull", "missing")
		case "pull":
			fmt.Fprintln(out, "正在拉取最新镜像…")
			if e = run("pull"); e == nil {
				e = run("up", "--detach", "--remove-orphans")
			}
		case "start":
			e = run("start")
		case "stop":
			e = run("stop", "--timeout", "15")
		case "restart":
			e = run("restart", "--timeout", "15")
		case "down":
			e = run("down", "--timeout", "15")
		case "delete":
			if e = run("down", "--timeout", "15"); e == nil {
				e = os.RemoveAll(a.stackDir(name))
				fmt.Fprintf(out, "编排已删除。数据卷与 %s 中的文件保留。\n", a.stackData(name))
			}
		default:
			return bad(400, "操作无效")
		}
		if e != nil {
			return bad(502, "编排操作失败，请查看上方输出")
		}
		return nil
	})
	return j, e
}

func (a *agent) stackAction(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	if e := validStackName(name); e != nil {
		return e
	}
	var req struct {
		Action string `json:"action"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	switch req.Action {
	case "deploy", "start", "stop", "restart", "pull", "down":
	default:
		return bad(400, "操作无效")
	}
	if _, e := os.Stat(filepath.Join(a.stackDir(name), "compose.json")); e != nil {
		return bad(404, "编排不存在")
	}
	if !a.reserveStack(name) {
		return bad(409, "此编排已有操作进行中")
	}
	j, e := a.runStack(name, req.Action)
	if e != nil {
		a.releaseStack(name, "")
		return e
	}
	send(w, 202, j)
	return nil
}

func (a *agent) stackDelete(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	if e := validStackName(name); e != nil {
		return e
	}
	if _, e := os.Stat(a.stackDir(name)); e != nil {
		return bad(404, "编排不存在")
	}
	if !a.reserveStack(name) {
		return bad(409, "此编排已有操作进行中")
	}
	j, e := a.runStack(name, "delete")
	if e != nil {
		a.releaseStack(name, "")
		return e
	}
	send(w, 202, j)
	return nil
}

// request is call without turning HTTP errors into one message, so callers can
// explain a conflict.
func (d *dockerClient) request(method, path string, value any) ([]byte, int, error) {
	prefix, e := d.negotiate()
	if e != nil {
		return nil, 0, e
	}
	var b io.Reader
	if value != nil {
		raw, e := json.Marshal(value)
		if e != nil {
			return nil, 0, e
		}
		b = bytes.NewReader(raw)
	}
	req, e := http.NewRequest(method, "http://docker"+prefix+path, b)
	if e != nil {
		return nil, 0, e
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := d.HTTP.Do(req)
	if e != nil {
		return nil, 0, bad(502, "Docker 未响应；操作结果请刷新核对")
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if e != nil {
		return nil, 0, bad(502, "读取 Docker 响应失败")
	}
	return raw, resp.StatusCode, nil
}

// dockerMessage pulls the reason out of a Docker error body.
func dockerMessage(raw []byte) string {
	var m struct{ Message string }
	if json.Unmarshal(raw, &m) == nil && m.Message != "" {
		return truncate(cleanText(m.Message), 300)
	}
	return ""
}
