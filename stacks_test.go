package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stackConfig builds a normalized config with one service.
func stackConfig(svc map[string]any) map[string]any {
	base := map[string]any{"image": "nginx:alpine", "command": nil, "entrypoint": nil}
	for k, v := range svc {
		base[k] = v
	}
	return map[string]any{"name": "demo", "services": map[string]any{"web": base}}
}
func decoded(t *testing.T, s string) any {
	t.Helper()
	var v any
	if e := json.Unmarshal([]byte(s), &v); e != nil {
		t.Fatal(e)
	}
	return v
}

func TestCheckStackRefusesHostAccess(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "stacks", "demo"), 0755)
	outside := t.TempDir()
	for _, c := range []struct {
		name string
		svc  string
		top  string
		want string
	}{
		{"privileged", `{"privileged":true}`, "", "特权模式"},
		{"host network", `{"network_mode":"host"}`, "", "network_mode: host"},
		{"join another container", `{"network_mode":"container:abc"}`, "", "network_mode: container:abc"},
		{"host pid", `{"pid":"host"}`, "", "pid: host"},
		{"pid of a container", `{"pid":"container:x"}`, "", "pid: container:x"},
		{"host ipc", `{"ipc":"host"}`, "", "ipc: host"},
		{"host uts", `{"uts":"host"}`, "", "uts: host"},
		{"host userns", `{"userns_mode":"host"}`, "", "userns_mode: host"},
		{"host cgroup", `{"cgroup":"host"}`, "", "cgroup: host"},
		{"sys_admin", `{"cap_add":["SYS_ADMIN"]}`, "", "SYS_ADMIN"},
		{"all caps", `{"cap_add":["ALL"]}`, "", "ALL"},
		{"cap prefix", `{"cap_add":["CAP_NET_ADMIN"]}`, "", "CAP_NET_ADMIN"},
		{"devices", `{"devices":[{"source":"/dev/kvm","target":"/dev/kvm"}]}`, "", "设备"},
		{"device rules", `{"device_cgroup_rules":["c 1:3 mr"]}`, "", "设备"},
		{"gpu reservation", `{"deploy":{"resources":{"reservations":{"devices":[{"capabilities":["gpu"]}]}}}}`, "", "deploy.resources"},
		{"seccomp off", `{"security_opt":["seccomp=unconfined"]}`, "", "seccomp=unconfined"},
		{"apparmor off", `{"security_opt":["apparmor:unconfined"]}`, "", "apparmor:unconfined"},
		{"selinux off", `{"security_opt":["label=disable"]}`, "", "label=disable"},
		{"docker socket", `{"volumes":[{"type":"bind","source":"/var/run/docker.sock","target":"/s"}]}`, "", "/var/run/docker.sock"},
		{"host root", `{"volumes":[{"type":"bind","source":"/","target":"/host"}]}`, "", "不允许挂载 /"},
		{"outside dir", `{"volumes":[{"type":"bind","source":"` + outside + `","target":"/x"}]}`, "", outside},
		{"dot-dot", `{"volumes":[{"type":"bind","source":"` + root + `/../etc","target":"/x"}]}`, "", "/etc"},
		{"npipe", `{"volumes":[{"type":"npipe","source":"x","target":"/x"}]}`, "", "挂载类型 npipe"},
		{"volumes_from container", `{"volumes_from":["container:db"]}`, "", "container:db"},
		{"build", `{"build":{"context":"/root"}}`, "", "build"},
		{"env_file left over", `{"env_file":["/etc/x"]}`, "", "env_file"},
		{"api socket", `{"use_api_socket":true}`, "", "use_api_socket"},
		{"provider", `{"provider":{"type":"model"}}`, "", "provider"},
		{"cgroup parent", `{"cgroup_parent":"/system.slice"}`, "", "cgroup_parent"},
		{"gpus", `{"gpus":"all"}`, "", "gpus"},
		{"no image", `{"image":""}`, "", "image"},
		{"secret file", `{}`, `{"secrets":{"s":{"file":"/etc/shadow"}}}`, "/etc/shadow"},
		{"config file", `{}`, `{"configs":{"c":{"file":"/root/.ssh/id_rsa"}}}`, "/root/.ssh/id_rsa"},
		{"bind volume", `{}`, `{"volumes":{"v":{"driver":"local","driver_opts":{"type":"none","o":"bind","device":"/etc"}}}}`, "/etc"},
		{"host network by name", `{}`, `{"networks":{"n":{"name":"host","external":true}}}`, "宿主机网络"},
		{"models", `{}`, `{"models":{"m":{}}}`, "models"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := stackConfig(decoded(t, c.svc).(map[string]any))
			if c.top != "" {
				for k, v := range decoded(t, c.top).(map[string]any) {
					cfg[k] = v
				}
			}
			problems := checkStack(cfg, root)
			if !strings.Contains(strings.Join(problems, "\n"), c.want) {
				t.Fatalf("want a problem mentioning %q, got %q", c.want, problems)
			}
		})
	}
}

func TestCheckStackAcceptsOrdinaryStack(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "stacks", "demo", "html"), 0755)
	cfg := decoded(t, `{
	  "name": "demo",
	  "services": {
	    "web": {"image": "nginx:alpine", "restart": "unless-stopped", "command": null, "entrypoint": null,
	      "ports": [{"target": 80, "published": "8080", "protocol": "tcp", "host_ip": "127.0.0.1"}],
	      "environment": {"A": "1"}, "labels": {"x": "y"}, "cap_add": ["NET_BIND_SERVICE"], "cap_drop": ["ALL"],
	      "security_opt": ["no-new-privileges:true"], "healthcheck": {"test": ["CMD", "true"]}, "depends_on": {"db": {"condition": "service_started"}},
	      "networks": {"default": null}, "x-note": "kept",
	      "volumes": [
	        {"type": "bind", "source": "` + root + `/stacks/demo/html", "target": "/usr/share/nginx/html", "read_only": true},
	        {"type": "bind", "source": "` + root + `/stacks/demo/not-yet-created", "target": "/data"},
	        {"type": "volume", "source": "data", "target": "/var/lib/x"},
	        {"type": "tmpfs", "target": "/tmp"}
	      ]},
	    "db": {"image": "postgres:17-alpine", "environment": {"POSTGRES_PASSWORD": "x"}, "volumes": [{"type": "volume", "source": "pg", "target": "/var/lib/postgresql/data"}]}
	  },
	  "volumes": {"data": {}, "pg": {"driver": "local", "driver_opts": {"type": "none", "o": "bind", "device": "` + root + `/stacks/demo/pg"}}},
	  "networks": {"default": {"name": "demo_default"}},
	  "secrets": {"s": {"file": "` + root + `/stacks/demo/secret.txt"}, "e": {"environment": "X"}},
	  "x-common": {"a": 1}
	}`).(map[string]any)
	if problems := checkStack(cfg, root); len(problems) > 0 {
		t.Fatalf("ordinary stack refused: %q", problems)
	}
	labelStack(cfg, "demo")
	for _, svc := range cfg["services"].(map[string]any) {
		if svc.(map[string]any)["labels"].(map[string]any)["junpanel.stack"] != "demo" {
			t.Fatal("services not labelled")
		}
	}
}

func TestInsideRootFollowsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(root, "a"), 0755)
	if e := os.Symlink(outside, filepath.Join(root, "a", "escape")); e != nil {
		t.Skip("symlinks unavailable")
	}
	os.Symlink(filepath.Join(root, "a"), filepath.Join(root, "inner"))
	for p, want := range map[string]bool{
		root:                                         true,
		filepath.Join(root, "a"):                     true,
		filepath.Join(root, "a", "new", "deep"):      true,
		filepath.Join(root, "inner", "x"):            true,
		filepath.Join(root, "a", "escape"):           false,
		filepath.Join(root, "a", "escape", "x", "y"): false,
		root + "-sibling":                            false,
		filepath.Join(root, "..", "x"):               false,
		"relative/path":                              false,
		"/":                                          false,
	} {
		if got := insideRoot(root, p); got != want {
			t.Errorf("insideRoot(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestStackValidation(t *testing.T) {
	a := testAgent(t)
	h := a.routes()
	for _, body := range []string{
		`{"name":"Bad","source":"services: {}"}`,
		`{"name":"junpanel-x","source":"services: {}"}`,
		`{"name":"a","source":"services: {}"}`,
		`{"name":"../x","source":"services: {}"}`,
		`{"name":"ok-name","source":""}`,
		`{"name":"ok-name","source":"` + strings.Repeat("x", maxStackFile+1) + `"}`,
	} {
		if r := request(h, "POST", "/stacks/check", body, nil, nil); r.Code != 400 {
			t.Fatalf("%.60s: %d %s", body, r.Code, r.Body.String())
		}
	}
	for _, p := range []string{"/stacks/Bad", "/stacks/junpanel-x"} {
		if r := request(h, "GET", p, "", nil, nil); r.Code != 400 {
			t.Fatalf("%s: %d", p, r.Code)
		}
	}
	if r := request(h, "GET", "/stacks/missing", "", nil, nil); r.Code != 404 {
		t.Fatalf("missing: %d", r.Code)
	}
	if r := request(h, "POST", "/stacks/missing/action", `{"action":"start"}`, nil, nil); r.Code != 404 {
		t.Fatalf("missing action: %d", r.Code)
	}
	if r := request(h, "POST", "/stacks", `{"name":"missing","source":"x","create":false}`, nil, nil); r.Code != 404 {
		t.Fatalf("edit of missing: %d", r.Code)
	}
}

// The parser must not be able to read what only root can.
func TestStackParsedWithoutRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to switch to the nobody user")
	}
	if exec.Command("docker", "compose", "version").Run() != nil {
		t.Skip("docker compose not installed")
	}
	a := testAgent(t)
	secret := filepath.Join(t.TempDir(), "secret.env")
	os.WriteFile(secret, []byte("SECRET=root-only\n"), 0600)
	// t.TempDir's parents are private; this file must be readable by any user.
	shared, e := os.MkdirTemp("", "junpanel-public-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(shared) })
	os.Chmod(shared, 0755)
	public := filepath.Join(shared, "public.env")
	os.WriteFile(public, []byte("GREETING=hello\n"), 0644)
	ctx := context.Background()
	for _, src := range []string{
		"services:\n  a:\n    image: alpine\n    env_file: [" + secret + "]\n",
		"include:\n  - " + secret + "\nservices:\n  a:\n    image: alpine\n",
		"services:\n  a:\n    extends:\n      file: " + secret + "\n      service: x\n",
	} {
		_, e := a.normalizeStack(ctx, "demo", src)
		if e == nil || !strings.Contains(e.Error(), "permission denied") {
			t.Fatalf("root-only file was read: %v\n%s", e, src)
		}
		if strings.Contains(e.Error(), "root-only") {
			t.Fatal("secret leaked into the error")
		}
	}
	cfg, e := a.normalizeStack(ctx, "demo", "services:\n  a:\n    image: alpine\n    env_file: ["+public+"]\n    volumes: [\"./data:/data\"]\n")
	if e != nil {
		t.Fatal(e)
	}
	svc := cfg["services"].(map[string]any)["a"].(map[string]any)
	if svc["environment"].(map[string]any)["GREETING"] != "hello" {
		t.Fatalf("world-readable env_file not inlined: %v", svc["environment"])
	}
	if src := svc["volumes"].([]any)[0].(map[string]any)["source"]; src != filepath.Join(a.stackData("demo"), "data") {
		t.Fatalf("relative bind resolved to %v", src)
	}
	// End to end through the check: a socket mount and a build are refused.
	_, problems, e := a.prepareStack(ctx, "demo", "services:\n  a:\n    image: alpine\n    volumes: [\"/var/run/docker.sock:/s\"]\n  b:\n    build: .\n")
	if e != nil || len(problems) < 2 {
		t.Fatalf("unsafe stack: %v %q", e, problems)
	}
}

// stackAgent is a Docker test agent whose parser runs as the current user when the
// tests cannot switch to nobody.
func stackAgent(t *testing.T) *agent {
	a := dockerAgent(t)
	if os.Geteuid() != 0 {
		a.composeConfig = func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Dir, cmd.Env = dir, append(os.Environ(), env...)
			out, errOut := &strings.Builder{}, &strings.Builder{}
			cmd.Stdout, cmd.Stderr = out, errOut
			e := cmd.Run()
			return out.String(), errOut.String(), e
		}
	}
	return a
}

func TestDockerStackLifecycle(t *testing.T) {
	a := stackAgent(t)
	h := a.routes()
	name := "jptest-" + randomID(6)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		dockerCLI(ctx, "", io.Discard, "docker", "compose", "--project-name", name, "down", "--volumes", "--timeout", "1")
	})
	src := "services:\n  app:\n    image: alpine:3.20\n    command: [\"sh\", \"-c\", \"echo $$GREETING > /out/hello && sleep 600\"]\n    environment:\n      GREETING: hi\n    volumes:\n      - ./out:/out\n      - data:/data\nvolumes:\n  data: {}\n"
	body, _ := json.Marshal(map[string]any{"name": name, "source": src, "create": true, "deploy": true})
	r := request(h, "POST", "/stacks", string(body), nil, nil)
	if r.Code != 202 {
		t.Fatalf("create: %d %s", r.Code, r.Body.String())
	}
	waitJob(t, a, r.Body.Bytes(), 4*time.Minute)
	var hello []byte
	for i := 0; i < 30; i++ {
		if hello, _ = os.ReadFile(filepath.Join(a.stackData(name), "out", "hello")); len(hello) > 0 {
			break
		}
		time.Sleep(time.Second)
	}
	if strings.TrimSpace(string(hello)) != "hi" {
		t.Fatalf("stack did not run as written: %q", hello)
	}
	// A second create with the same name, and the same name as an outside project, are refused.
	if r = request(h, "POST", "/stacks", string(body), nil, nil); r.Code != 409 {
		t.Fatalf("duplicate: %d", r.Code)
	}
	var list struct {
		Stacks []stackInfo `json:"stacks"`
	}
	json.Unmarshal(request(h, "GET", "/stacks", "", nil, nil).Body.Bytes(), &list)
	if len(list.Stacks) != 1 || list.Stacks[0].Running != 1 || list.Stacks[0].Containers[0].Stack != name {
		t.Fatalf("list: %+v", list.Stacks)
	}
	// Editing to something unsafe is refused and leaves the deployed file alone.
	before, _ := os.ReadFile(filepath.Join(a.stackDir(name), "compose.json"))
	bad, _ := json.Marshal(map[string]any{"name": name, "source": "services:\n  app:\n    image: alpine:3.20\n    privileged: true\n", "deploy": true})
	if r = request(h, "POST", "/stacks", string(bad), nil, nil); r.Code != 400 || !strings.Contains(r.Body.String(), "特权") {
		t.Fatalf("unsafe edit: %d %s", r.Code, r.Body.String())
	}
	if after, _ := os.ReadFile(filepath.Join(a.stackDir(name), "compose.json")); string(after) != string(before) {
		t.Fatal("refused edit changed the deployed file")
	}
	if r = request(h, "GET", "/stacks/"+name, "", nil, nil); !strings.Contains(r.Body.String(), "GREETING") {
		t.Fatalf("source: %s", r.Body.String())
	}
	for _, action := range []string{"stop", "start", "restart", "pull"} {
		r = request(h, "POST", "/stacks/"+name+"/action", `{"action":"`+action+`"}`, nil, nil)
		if r.Code != 202 {
			t.Fatalf("%s: %d %s", action, r.Code, r.Body.String())
		}
		waitJob(t, a, r.Body.Bytes(), 3*time.Minute)
	}
	if r = request(h, "DELETE", "/stacks/"+name, "", nil, nil); r.Code != 202 {
		t.Fatalf("delete: %d %s", r.Code, r.Body.String())
	}
	waitJob(t, a, r.Body.Bytes(), 2*time.Minute)
	rows, _ := a.docker.list()
	for _, c := range rows {
		if c.Project == name {
			t.Fatalf("container left after delete: %s", c.Name)
		}
	}
	if _, e := os.Stat(a.stackDir(name)); !os.IsNotExist(e) {
		t.Fatal("stack definition left after delete")
	}
	if _, e := os.Stat(filepath.Join(a.stackData(name), "out", "hello")); e != nil {
		t.Fatal("delete removed the stack's files")
	}
	if out, _ := exec.Command("docker", "volume", "ls", "--format", "{{.Name}}").Output(); !strings.Contains(string(out), name+"_data") {
		t.Fatal("delete removed the data volume")
	}
}

func TestDockerVolumesAndNetworks(t *testing.T) {
	a := dockerAgent(t)
	h := a.routes()
	vol, net := "jptest-vol-"+randomID(6), "jptest-net-"+randomID(6)
	ctr := "jptest-ctr-" + randomID(6)
	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", ctr).Run()
		exec.Command("docker", "volume", "rm", "-f", vol).Run()
		exec.Command("docker", "network", "rm", net).Run()
	})
	if r := request(h, "POST", "/volumes", `{"name":"`+vol+`"}`, nil, nil); r.Code != 200 {
		t.Fatalf("volume create: %d %s", r.Code, r.Body.String())
	}
	if r := request(h, "POST", "/volumes", `{"name":"`+vol+`"}`, nil, nil); r.Code != 409 {
		t.Fatalf("duplicate volume: %d", r.Code)
	}
	if r := request(h, "POST", "/networks", `{"name":"`+net+`","internal":true}`, nil, nil); r.Code != 200 {
		t.Fatalf("network create: %d %s", r.Code, r.Body.String())
	}
	for _, body := range []string{`{"name":"../x"}`, `{"name":"-x"}`} {
		if r := request(h, "POST", "/volumes", body, nil, nil); r.Code != 400 {
			t.Fatalf("volume name %s: %d", body, r.Code)
		}
	}
	if r := request(h, "POST", "/networks", `{"name":"host"}`, nil, nil); r.Code != 400 {
		t.Fatalf("network named host: %d", r.Code)
	}
	if out, e := exec.Command("docker", "create", "--name", ctr, "-v", vol+":/v", "--network", net, "alpine:3.20", "true").CombinedOutput(); e != nil {
		t.Fatalf("docker create: %v %s", e, out)
	}
	var vols []map[string]any
	json.Unmarshal(request(h, "GET", "/volumes", "", nil, nil).Body.Bytes(), &vols)
	found := false
	for _, v := range vols {
		if v["name"] == vol {
			found = true
			if v["in_use"] != true {
				t.Fatalf("volume in use not reported: %v", v)
			}
		}
	}
	if !found {
		t.Fatal("volume not listed")
	}
	var nets []map[string]any
	json.Unmarshal(request(h, "GET", "/networks", "", nil, nil).Body.Bytes(), &nets)
	netID := ""
	for _, n := range nets {
		if n["name"] == net {
			netID = n["id"].(string)
			if n["internal"] != true || n["containers"].(float64) != 1 {
				t.Fatalf("network row: %v", n)
			}
		}
		if n["name"] == "bridge" && n["builtin"] != true {
			t.Fatal("bridge not marked built-in")
		}
	}
	if r := request(h, "DELETE", "/volumes/"+vol, "", nil, nil); r.Code != 409 || !strings.Contains(r.Body.String(), "仍被容器使用") {
		t.Fatalf("delete of a used volume: %d %s", r.Code, r.Body.String())
	}
	if r := request(h, "DELETE", "/networks/"+netID, "", nil, nil); r.Code != 409 || !strings.Contains(r.Body.String(), "仍有容器连接") {
		t.Fatalf("delete of a used network: %d %s", r.Code, r.Body.String())
	}
	for _, n := range nets {
		if n["name"] == "bridge" {
			if r := request(h, "DELETE", "/networks/"+n["id"].(string), "", nil, nil); r.Code != 403 {
				t.Fatalf("delete of bridge: %d %s", r.Code, r.Body.String())
			}
		}
	}
	exec.Command("docker", "rm", "-f", ctr).Run()
	if r := request(h, "DELETE", "/volumes/"+vol, "", nil, nil); r.Code != 200 {
		t.Fatalf("delete volume: %d %s", r.Code, r.Body.String())
	}
	if r := request(h, "DELETE", "/networks/"+netID, "", nil, nil); r.Code != 200 {
		t.Fatalf("delete network: %d %s", r.Code, r.Body.String())
	}
	if r := request(h, "DELETE", "/volumes/"+vol, "", nil, nil); r.Code != 404 {
		t.Fatalf("delete missing volume: %d", r.Code)
	}
}
