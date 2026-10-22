package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Monitoring history: one averaged sample per minute, kept for a week in a private file so the
// charts survive restarts. The live 5-second chart on the overview page is unchanged.

const (
	historyKeep    = 7 * 24 * time.Hour
	historyEvery   = time.Minute
	historyCompact = 2000 // appended lines before the file is rewritten
	historyBuckets = 240
)

type histPoint struct {
	At     time.Time `json:"at"`
	CPU    float64   `json:"cpu"`
	Memory float64   `json:"memory"`
	Disk   float64   `json:"disk"`
	RX     float64   `json:"rx"`
	TX     float64   `json:"tx"`
}

type historyStore struct {
	mu       sync.Mutex
	path     string
	points   []histPoint
	appended int
	// running average of the samples since the last stored point
	n                      int
	cpu, mem, disk, rx, tx float64
	last                   time.Time
}

func newHistoryStore(path string) *historyStore {
	h := &historyStore{path: path}
	f, e := os.Open(path)
	if e != nil {
		return h
	}
	defer f.Close()
	cutoff := time.Now().Add(-historyKeep)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var p histPoint
		if json.Unmarshal(sc.Bytes(), &p) == nil && p.At.After(cutoff) && (len(h.points) == 0 || p.At.After(h.points[len(h.points)-1].At)) {
			h.points = append(h.points, p)
		}
	}
	return h
}

// observe adds one live sample; once a minute has passed it stores their average.
func (h *historyStore) observe(now time.Time, info hostInfo) {
	if info.CPU == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.last.IsZero() {
		h.last = now
	}
	h.n++
	h.cpu += *info.CPU
	if info.MemTotal > 0 {
		h.mem += float64(info.MemUsed) / float64(info.MemTotal) * 100
	}
	if info.Disk["total"] > 0 {
		h.disk += float64(info.Disk["used"]) / float64(info.Disk["total"]) * 100
	}
	if info.RX != nil {
		h.rx += *info.RX
		h.tx += *info.TX
	}
	if now.Sub(h.last) < historyEvery {
		return
	}
	n := float64(h.n)
	p := histPoint{At: now.UTC(), CPU: h.cpu / n, Memory: h.mem / n, Disk: h.disk / n, RX: h.rx / n, TX: h.tx / n}
	h.n, h.cpu, h.mem, h.disk, h.rx, h.tx, h.last = 0, 0, 0, 0, 0, 0, now
	h.store(p)
}

func (h *historyStore) store(p histPoint) {
	h.points = append(h.points, p)
	cutoff := p.At.Add(-historyKeep)
	drop := 0
	for drop < len(h.points) && h.points[drop].At.Before(cutoff) {
		drop++
	}
	h.points = h.points[drop:]
	h.appended++
	if h.appended >= historyCompact || drop > 0 && h.appended%60 == 0 {
		h.rewrite()
		return
	}
	f, e := os.OpenFile(h.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return
	}
	defer f.Close()
	line, _ := json.Marshal(p)
	f.Write(append(line, '\n'))
}

func (h *historyStore) rewrite() {
	tmp := h.path + ".tmp"
	f, e := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return
	}
	w := bufio.NewWriter(f)
	for _, p := range h.points {
		line, _ := json.Marshal(p)
		w.Write(append(line, '\n'))
	}
	if w.Flush() != nil || f.Close() != nil || os.Rename(tmp, h.path) != nil {
		os.Remove(tmp)
		return
	}
	h.appended = 0
}

// query returns the points of the last span, averaged into at most historyBuckets buckets.
func (h *historyStore) query(now time.Time, span time.Duration) ([]histPoint, time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	from := now.Add(-span)
	var sel []histPoint
	for _, p := range h.points {
		if !p.At.Before(from) {
			sel = append(sel, p)
		}
	}
	out := []histPoint{}
	if len(sel) == 0 {
		return out, historyEvery
	}
	step := span / historyBuckets
	if step < historyEvery {
		step = historyEvery
	}
	for i := 0; i < len(sel); {
		start := sel[i].At.Truncate(step)
		var sum histPoint
		n := 0
		for i < len(sel) && sel[i].At.Truncate(step).Equal(start) {
			sum.CPU += sel[i].CPU
			sum.Memory += sel[i].Memory
			sum.Disk += sel[i].Disk
			sum.RX += sel[i].RX
			sum.TX += sel[i].TX
			sum.At = sel[i].At
			n++
			i++
		}
		f := float64(n)
		out = append(out, histPoint{At: sum.At, CPU: sum.CPU / f, Memory: sum.Memory / f, Disk: sum.Disk / f, RX: sum.RX / f, TX: sum.TX / f})
	}
	return out, step
}

var historySpans = map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": historyKeep}

func (a *agent) historyGet(w http.ResponseWriter, r *http.Request) error {
	span, ok := historySpans[r.URL.Query().Get("range")]
	if !ok {
		return bad(400, "范围需为 1h、6h、24h 或 7d")
	}
	points, step := a.history.query(a.now(), span)
	send(w, 200, map[string]any{"points": points, "step": int(step.Seconds()), "range": r.URL.Query().Get("range")})
	return nil
}

func historyPath(state string) string { return filepath.Join(state, "history.jsonl") }
