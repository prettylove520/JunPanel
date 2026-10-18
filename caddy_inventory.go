package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type caddyConfig struct {
	Config  string   `json:"config"`
	Adapter string   `json:"adapter"`
	PIDs    []int    `json:"pids"`
	Hosts   []string `json:"hosts"`
	Error   string   `json:"error,omitempty"`
	Primary bool     `json:"primary"`
}

func parseCaddyCommand(args []string, cwd string) (caddyConfig, bool) {
	if len(args) < 2 || path.Base(args[0]) != "caddy" || args[1] != "run" {
		return caddyConfig{}, false
	}
	config, adapter := "", ""
	for i := 2; i < len(args); i++ {
		arg := args[i]
		if arg == "--resume" {
			return caddyConfig{}, false
		} // A resumed process need not use its on-disk config.
		switch {
		case arg == "--config" || arg == "-c":
			if i+1 >= len(args) {
				return caddyConfig{}, false
			}
			i++
			config = args[i]
		case strings.HasPrefix(arg, "--config="):
			config = strings.TrimPrefix(arg, "--config=")
		case arg == "--adapter" || arg == "-a":
			if i+1 >= len(args) {
				return caddyConfig{}, false
			}
			i++
			adapter = args[i]
		case strings.HasPrefix(arg, "--adapter="):
			adapter = strings.TrimPrefix(arg, "--adapter=")
		}
	}
	if config == "" {
		config = "Caddyfile"
	}
	if !path.IsAbs(config) {
		if cwd == "" {
			return caddyConfig{}, false
		}
		config = path.Join(cwd, config)
	}
	if adapter == "" && strings.ToLower(path.Ext(config)) != ".json" {
		adapter = "caddyfile"
	}
	return caddyConfig{Config: path.Clean(config), Adapter: adapter, PIDs: []int{}, Hosts: []string{}}, true
}

// Discover only running copies of the system Caddy binary. Do not read process
// environments, follow supplied commands, or execute a discovered executable.
func runningCaddyConfigs() ([]caddyConfig, error) {
	return runningCaddyConfigsAt("/proc")
}

func runningCaddyConfigsAt(procRoot string) ([]caddyConfig, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("无法读取运行中的 Caddy 进程")
	}
	out := []caddyConfig{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		base := filepath.Join(procRoot, entry.Name())
		binary, err := os.Readlink(filepath.Join(base, "exe"))
		if err != nil {
			continue
		}
		if strings.TrimSuffix(binary, " (deleted)") != "/usr/bin/caddy" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(base, "cmdline"))
		if err != nil || len(raw) > 65536 {
			continue
		}
		cwd, _ := os.Readlink(filepath.Join(base, "cwd"))
		args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
		if cfg, ok := parseCaddyCommand(args, cwd); ok {
			cfg.PIDs = []int{pid}
			out = append(out, cfg)
		}
	}
	return out, nil
}

// Hosts come from HTTP route matchers, not similarly named fields in plugins.
func caddyMatcherHosts(raw []byte) ([]string, error) {
	var value struct {
		Apps struct {
			HTTP struct {
				Servers map[string]struct {
					Routes []any `json:"routes"`
				} `json:"servers"`
			} `json:"http"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("Caddy 转换结果无效")
	}
	hosts := map[string]bool{}
	var routes func([]any)
	routes = func(list []any) {
		for _, item := range list {
			route, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if matches, ok := route["match"].([]any); ok {
				for _, match := range matches {
					if m, ok := match.(map[string]any); ok {
						if list, ok := m["host"].([]any); ok {
							for _, h := range list {
								if host, ok := h.(string); ok {
									hosts[strings.ToLower(host)] = true
								}
							}
						}
					}
				}
			}
			if handlers, ok := route["handle"].([]any); ok {
				for _, handler := range handlers {
					if h, ok := handler.(map[string]any); ok {
						if nested, ok := h["routes"].([]any); ok {
							routes(nested)
						}
					}
				}
			}
		}
	}
	for _, server := range value.Apps.HTTP.Servers {
		routes(server.Routes)
	}
	out := []string{}
	for h := range hosts {
		out = append(out, h)
	}
	sort.Strings(out)
	return out, nil
}

func (a *agent) caddyInventory() ([]string, []caddyConfig, error) {
	configs := []caddyConfig{{Config: a.cfg.Caddy, Adapter: "caddyfile", Primary: true, PIDs: []int{}, Hosts: []string{}}}
	discover := a.processConfigs
	if discover == nil {
		discover = runningCaddyConfigs
	}
	processes, discoveryErr := discover()
	for _, cfg := range processes {
		found := false
		for i := range configs {
			if configs[i].Config == cfg.Config && configs[i].Adapter == cfg.Adapter {
				configs[i].PIDs = append(configs[i].PIDs, cfg.PIDs...)
				found = true
				break
			}
		}
		if !found {
			configs = append(configs, cfg)
		}
	}
	if len(configs) > 32 {
		configs = configs[:32]
		discoveryErr = fmt.Errorf("Caddy 配置超过 32 份，列表不完整")
	}
	all := map[string]bool{}
	problems := []string{}
	if discoveryErr != nil {
		problems = append(problems, discoveryErr.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for i := range configs {
		cfg := &configs[i]
		sort.Ints(cfg.PIDs)
		var raw []byte
		var err error
		if cfg.Adapter == "" {
			var file *os.File
			file, err = os.Open(cfg.Config)
			if err == nil {
				raw, err = io.ReadAll(io.LimitReader(file, (8<<20)+1))
				file.Close()
			}
		} else if cfg.Adapter != "caddyfile" {
			err = fmt.Errorf("不支持自定义配置适配器")
		} else if ctx.Err() != nil {
			err = ctx.Err()
		} else if a.capture != nil {
			deadline, _ := ctx.Deadline()
			timeout := min(10*time.Second, time.Until(deadline))
			var text string
			text, err = a.capture(timeout, "/usr/bin/caddy", "adapt", "--config", cfg.Config, "--adapter", "caddyfile")
			raw = []byte(text)
		} else {
			commandCtx, stop := context.WithTimeout(ctx, 10*time.Second)
			raw, err = exec.CommandContext(commandCtx, "/usr/bin/caddy", "adapt", "--config", cfg.Config, "--adapter", "caddyfile").Output()
			stop()
		}
		if err == nil && len(raw) > 8<<20 {
			err = fmt.Errorf("配置过大")
		}
		if err == nil {
			cfg.Hosts, err = caddyMatcherHosts(raw)
		}
		if err != nil {
			cfg.Error = "配置读取失败；请核对文件、导入路径、适配器与服务环境变量"
			problems = append(problems, cfg.Config+"："+cfg.Error)
			continue
		}
		for _, h := range cfg.Hosts {
			all[h] = true
		}
	}
	hosts := []string{}
	for h := range all {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	if len(problems) > 0 {
		return hosts, configs, bad(502, strings.Join(problems, "；"))
	}
	return hosts, configs, nil
}

func (a *agent) caddyHosts() ([]string, error) {
	hosts, _, err := a.caddyInventory()
	return hosts, err
}
