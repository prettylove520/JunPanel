package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDiscoverRunningSystemCaddy(t *testing.T) {
	root := t.TempDir()
	for _, pid := range []string{"12000", "84968", "86745", "99999"} {
		dir := filepath.Join(root, pid)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		executable := "/usr/bin/caddy"
		if pid == "99999" {
			executable = "/tmp/caddy"
		}
		if err := os.Symlink(executable, filepath.Join(dir, "exe")); err != nil {
			if runtime.GOOS == "windows" {
				t.Skip("Windows symlink permission unavailable; Linux CI covers process discovery")
			}
			t.Fatal(err)
		}
		command := "/usr/bin/caddy\x00run\x00--adapter\x00caddyfile\x00--config\x00/etc/instance-" + pid + ".caddy\x00"
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(command), 0600); err != nil {
			t.Fatal(err)
		}
	}
	configs, err := runningCaddyConfigsAt(root)
	if err != nil || len(configs) != 3 {
		t.Fatalf("%+v %v", configs, err)
	}
	for _, cfg := range configs {
		if len(cfg.PIDs) != 1 || !strings.HasSuffix(cfg.Config, fmt.Sprintf("instance-%d.caddy", cfg.PIDs[0])) {
			t.Fatalf("wrong process provenance: %+v", cfg)
		}
	}
}

func TestParseRunningCaddyConfig(t *testing.T) {
	cases := []struct {
		args               []string
		cwd, path, adapter string
	}{
		{[]string{"/usr/bin/caddy", "run", "--environ", "--config", "/etc/caddy/Caddyfile"}, "", "/etc/caddy/Caddyfile", "caddyfile"},
		{[]string{"/usr/bin/caddy", "run", "--adapter", "caddyfile", "--config", "/etc/caddy/fuyuan-migration-preview.caddy"}, "", "/etc/caddy/fuyuan-migration-preview.caddy", "caddyfile"},
		{[]string{"/usr/bin/caddy", "run", "--adapter=caddyfile", "--config=/etc/fuyuan-cutover-20261012/production.caddy"}, "", "/etc/fuyuan-cutover-20261012/production.caddy", "caddyfile"},
	}
	for _, tc := range cases {
		cfg, ok := parseCaddyCommand(tc.args, tc.cwd)
		if !ok || cfg.Config != tc.path || cfg.Adapter != tc.adapter {
			t.Fatalf("%v: %+v", tc.args, cfg)
		}
	}
	for _, args := range [][]string{{"caddy", "adapt", "--config", "/tmp/not-running"}, {"other", "run", "--config", "/tmp/not-caddy"}, {"caddy", "run", "--config", "relative.caddy"}, {"caddy", "run", "--resume", "--config", "/etc/unused"}} {
		if _, ok := parseCaddyCommand(args, ""); ok {
			t.Fatalf("accepted unsuitable process %v", args)
		}
	}
}

func TestMultipleCaddyConfigsAndPartialFailure(t *testing.T) {
	a := testAgent(t)
	a.processConfigs = func() ([]caddyConfig, error) {
		return []caddyConfig{
			{Config: a.cfg.Caddy, Adapter: "caddyfile", PIDs: []int{12000}},
			{Config: "/etc/caddy/fuyuan-migration-preview.caddy", Adapter: "caddyfile", PIDs: []int{84968}},
			{Config: "/etc/fuyuan-cutover-20261012/production.caddy", Adapter: "caddyfile", PIDs: []int{86745}},
		}, nil
	}
	calls := []string{}
	failPreview := false
	a.capture = func(_ time.Duration, exe string, args ...string) (string, error) {
		if exe != "/usr/bin/caddy" || len(args) != 5 || args[0] != "adapt" || args[1] != "--config" {
			t.Fatalf("unsafe command: %s %v", exe, args)
		}
		path := args[2]
		calls = append(calls, path)
		host := "learning.example.com"
		if strings.Contains(path, "migration-preview") {
			if failPreview {
				return "", fmt.Errorf("unreadable")
			}
			host = "preview.example.com"
		}
		if strings.Contains(path, "production.caddy") {
			host = "company.example.com"
		}
		return fmt.Sprintf(`{"apps":{"http":{"servers":{"srv":{"routes":[{"handle":[{"handler":"subroute","routes":[{"match":[{"host":[%q,"monitor.example.com"]}]}]}]}]}}}}}`, host), nil
	}
	hosts, sources, err := a.caddyInventory()
	if err != nil || len(sources) != 3 || len(calls) != 3 || !reflect.DeepEqual(hosts, []string{"company.example.com", "learning.example.com", "monitor.example.com", "preview.example.com"}) {
		t.Fatalf("inventory: %v %v %v", hosts, sources, err)
	}
	if len(sources[0].PIDs) != 1 || sources[0].PIDs[0] != 12000 || !sources[0].Primary {
		t.Fatal("primary process not deduplicated")
	}
	failPreview = true
	hosts, sources, err = a.caddyInventory()
	if err == nil || sources[1].Error == "" || !reflect.DeepEqual(hosts, []string{"company.example.com", "learning.example.com", "monitor.example.com"}) {
		t.Fatalf("partial failure lost healthy sites: %v %v", hosts, err)
	}
	if len(a.jobs.list()) != 0 {
		t.Fatal("discovery changed service state")
	}
}

func TestCaddyHostsIgnorePluginFields(t *testing.T) {
	raw := []byte(`{"apps":{"http":{"servers":{"srv":{"routes":[{"match":[{"host":["real.example.com"]}],"handle":[{"handler":"reverse_proxy","host":["not-a-site.example.com"]}]}]}}}}}`)
	hosts, err := caddyMatcherHosts(raw)
	if err != nil || !reflect.DeepEqual(hosts, []string{"real.example.com"}) {
		t.Fatalf("%v %v", hosts, err)
	}
}
