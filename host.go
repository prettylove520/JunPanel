package main

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type chartPoint struct {
	At     time.Time `json:"at"`
	CPU    float64   `json:"cpu"`
	Memory float64   `json:"memory"`
	RX     float64   `json:"rx"`
	TX     float64   `json:"tx"`
}
type hostInfo struct {
	Hostname   string            `json:"hostname"`
	OS         string            `json:"os"`
	Kernel     string            `json:"kernel"`
	Cores      int               `json:"cores"`
	CPU        *float64          `json:"cpu"`
	MemTotal   uint64            `json:"mem_total"`
	MemUsed    uint64            `json:"mem_used"`
	SwapTotal  uint64            `json:"swap_total"`
	SwapUsed   uint64            `json:"swap_used"`
	Load       []float64         `json:"load"`
	Uptime     float64           `json:"uptime"`
	RX         *float64          `json:"rx"`
	TX         *float64          `json:"tx"`
	RXTotal    uint64            `json:"rx_total"`
	TXTotal    uint64            `json:"tx_total"`
	Interfaces []string          `json:"interfaces"`
	History    []chartPoint      `json:"history"`
	Disk       map[string]uint64 `json:"disk"`
}
type hostSampler struct {
	sync.Mutex
	Proc        string
	Clock       func() time.Time
	last        time.Time
	total, idle uint64
	net         map[string][2]uint64
	history     []chartPoint
	cached      hostInfo
}

func (h *hostSampler) read() (hostInfo, error) {
	h.Lock()
	defer h.Unlock()
	root := h.Proc
	if root == "" {
		root = "/proc"
	}
	now := time.Now()
	if h.Clock != nil {
		now = h.Clock()
	}
	if !h.last.IsZero() && now.Sub(h.last) < 2*time.Second {
		return h.cached, nil
	}
	read := func(name string) (string, error) { b, e := os.ReadFile(filepath.Join(root, name)); return string(b), e }
	stat, e := read("stat")
	if e != nil {
		return hostInfo{}, e
	}
	lines := strings.Split(stat, "\n")
	parts := strings.Fields(lines[0])
	if len(parts) < 6 {
		return hostInfo{}, bad(500, "CPU 数据无效")
	}
	var total, idle uint64
	for i := 1; i < len(parts) && i <= 8; i++ {
		n, _ := strconv.ParseUint(parts[i], 10, 64)
		total += n
		if i == 4 || i == 5 {
			idle += n
		}
	}
	info := hostInfo{OS: "Linux", Cores: runtime.NumCPU(), Interfaces: []string{}, History: []chartPoint{}, Load: []float64{}, Disk: diskSpace("/")}
	info.Hostname, _ = os.Hostname()
	if v, e := read("sys/kernel/osrelease"); e == nil {
		info.Kernel = strings.TrimSpace(v)
	}
	if f, e := os.Open("/etc/os-release"); e == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				info.OS = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			}
		}
		f.Close()
	}
	mem, e := read("meminfo")
	if e != nil {
		return hostInfo{}, e
	}
	memory := map[string]uint64{}
	for _, line := range strings.Split(mem, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			n, _ := strconv.ParseUint(fields[1], 10, 64)
			memory[strings.TrimSuffix(fields[0], ":")] = n * 1024
		}
	}
	info.MemTotal = memory["MemTotal"]
	if info.MemTotal >= memory["MemAvailable"] {
		info.MemUsed = info.MemTotal - memory["MemAvailable"]
	}
	info.SwapTotal = memory["SwapTotal"]
	if info.SwapTotal >= memory["SwapFree"] {
		info.SwapUsed = info.SwapTotal - memory["SwapFree"]
	}
	if v, e := read("uptime"); e == nil {
		fields := strings.Fields(v)
		if len(fields) > 0 {
			info.Uptime, _ = strconv.ParseFloat(fields[0], 64)
		}
	}
	if v, e := read("loadavg"); e == nil {
		fields := strings.Fields(v)
		for i := 0; i < len(fields) && i < 3; i++ {
			n, _ := strconv.ParseFloat(fields[i], 64)
			info.Load = append(info.Load, n)
		}
	}
	network, e := read("net/dev")
	if e != nil {
		return hostInfo{}, e
	}
	netNow := map[string][2]uint64{}
	for _, line := range strings.Split(network, "\n") {
		pair := strings.SplitN(line, ":", 2)
		if len(pair) != 2 {
			continue
		}
		name := strings.TrimSpace(pair[0])
		if name == "lo" || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") {
			continue
		}
		fields := strings.Fields(pair[1])
		if len(fields) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		netNow[name] = [2]uint64{rx, tx}
		info.RXTotal += rx
		info.TXTotal += tx
		info.Interfaces = append(info.Interfaces, name)
	}
	if !h.last.IsZero() {
		if total > h.total && idle >= h.idle {
			value := 100 * (1 - float64(idle-h.idle)/float64(total-h.total))
			if value < 0 {
				value = 0
			}
			if value > 100 {
				value = 100
			}
			info.CPU = &value
		}
		seconds := now.Sub(h.last).Seconds()
		rx, tx := float64(0), float64(0)
		for name, v := range netNow {
			if old, ok := h.net[name]; ok {
				if v[0] >= old[0] {
					rx += float64(v[0]-old[0]) / seconds
				}
				if v[1] >= old[1] {
					tx += float64(v[1]-old[1]) / seconds
				}
			}
		}
		info.RX = &rx
		info.TX = &tx
	}
	if info.CPU != nil {
		point := chartPoint{At: now, CPU: *info.CPU}
		if info.MemTotal > 0 {
			point.Memory = float64(info.MemUsed) / float64(info.MemTotal) * 100
		}
		if info.RX != nil {
			point.RX = *info.RX
			point.TX = *info.TX
		}
		h.history = append(h.history, point)
		if len(h.history) > 120 {
			h.history = h.history[len(h.history)-120:]
		}
	}
	info.History = append([]chartPoint{}, h.history...)
	h.last = now
	h.total = total
	h.idle = idle
	h.net = netNow
	h.cached = info
	return info, nil
}
func (a *agent) startMetrics(stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			if info, e := a.metrics.read(); e == nil {
				a.history.observe(a.now(), info)
			}
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()
}
