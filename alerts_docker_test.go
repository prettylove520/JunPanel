package main

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Runs against the real Docker engine (JUNPANEL_DOCKER_TEST=1): a container that is
// killed from outside raises an alert, one stopped through the panel does not, and
// a container that comes back clears its alert.
func TestDockerContainerAlerts(t *testing.T) {
	a := dockerAgent(t)
	hook := newReceiver(t)
	now := time.Now()
	a.now = func() time.Time { return now }
	a.host = "docker-test"
	a.diskFn = func(string) map[string]uint64 { return map[string]uint64{"total": 100 << 30, "used": 10 << 30} }
	a.runner = func(time.Duration, string, ...string) (string, error) { return "active", nil }
	if e := atomicJSON(a.alertConfigPath(), alertConfig{Format: "generic", URL: hook.srv.URL, Notify: true, DiskOn: true, DiskPercent: 90, CertOn: false, CertDays: 14, ContainerOn: true, ServiceOn: true, RepeatHours: 12}, 0600); e != nil {
		t.Fatal(e)
	}
	docker := func(args ...string) string {
		t.Helper()
		out, e := exec.Command("docker", args...).CombinedOutput()
		if e != nil {
			t.Fatalf("docker %v: %v\n%s", args, e, out)
		}
		return strings.TrimSpace(string(out))
	}
	names := []string{"jp-alert-killed", "jp-alert-stopped"}
	for _, n := range names {
		exec.Command("docker", "rm", "-f", n).Run()
		docker("run", "-d", "--name", n, "--network", "none", "redis:7-alpine")
	}
	t.Cleanup(func() {
		for _, n := range names {
			exec.Command("docker", "rm", "-f", n).Run()
		}
	})
	idOf := func(name string) string { return docker("inspect", "--format", "{{.Id}}", name) }
	// "docker kill" returns once the signal is sent, not when the container has exited,
	// so wait for the state to change before checking.
	waitRunning := func(name string, want bool) {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			if docker("inspect", "--format", "{{.State.Running}}", name) == fmt.Sprint(want) {
				return
			}
		}
		t.Fatalf("%s did not reach running=%v", name, want)
	}
	killed, stopped := idOf(names[0]), idOf(names[1])
	tick := func() *alertState {
		t.Helper()
		now = now.Add(30 * time.Second)
		st, e := a.alertTick(now)
		if e != nil {
			t.Fatal(e)
		}
		return st
	}
	if st := tick(); st.Active["container:"+killed] != nil || hook.count() != 0 {
		t.Fatalf("alert on a healthy container: %v", st.Active)
	}
	docker("kill", names[0])
	waitRunning(names[0], false)
	st := tick()
	if st.Active["container:"+killed] == nil || hook.count() != 1 || !strings.Contains(hook.last(), "jp-alert-killed") || !strings.Contains(hook.last(), "退出码 137") {
		t.Fatalf("killed container: %v posts=%d %s", st.Active, hook.count(), hook.last())
	}
	// Stopping through the panel's own API is expected.
	h := a.routes()
	r := request(h, "POST", "/containers/"+stopped+"/action", `{"action":"stop"}`, nil, nil)
	if r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	waitJob(t, a, r.Body.Bytes(), time.Minute)
	if st = tick(); st.Active["container:"+stopped] != nil || hook.count() != 1 {
		t.Fatalf("a panel stop was reported: %v posts=%d", st.Active, hook.count())
	}
	docker("start", names[0])
	waitRunning(names[0], true)
	tick()
	st = tick()
	if st.Active["container:"+killed] != nil || hook.count() != 2 || !strings.Contains(hook.last(), "已恢复") {
		t.Fatalf("recovery: %v posts=%d %s", st.Active, hook.count(), hook.last())
	}
}
