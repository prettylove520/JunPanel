package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func sample(cpu float64) hostInfo {
	rx, tx := 1000.0, 500.0
	return hostInfo{CPU: &cpu, MemTotal: 100, MemUsed: 40, RX: &rx, TX: &tx, Disk: map[string]uint64{"total": 100, "used": 25}}
}

func TestHistoryAveragesPersistsAndExpires(t *testing.T) {
	path := t.TempDir() + "/history.jsonl"
	h := newHistoryStore(path)
	start := time.Now().Add(-2 * time.Hour).Truncate(time.Minute)
	// two samples per stored minute: 10 and 30 average to 20
	for m := 0; m < 120; m++ {
		at := start.Add(time.Duration(m) * time.Minute)
		h.observe(at, sample(10))
		h.observe(at.Add(30*time.Second), sample(30))
	}
	if len(h.points) < 100 || len(h.points) > 121 {
		t.Fatalf("stored %d points", len(h.points))
	}
	for _, p := range h.points[1:] {
		if p.CPU < 10 || p.CPU > 30 || p.Memory != 40 || p.Disk != 25 || p.RX != 1000 {
			t.Fatalf("average wrong: %+v", p)
		}
	}
	// a restart keeps the history
	again := newHistoryStore(path)
	if len(again.points) != len(h.points) {
		t.Fatalf("reloaded %d of %d", len(again.points), len(h.points))
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0600 {
		t.Fatalf("history file mode %v", st.Mode())
	}
	// ranges select and thin out
	hour, _ := again.query(time.Now(), time.Hour)
	week, step := again.query(time.Now(), historyKeep)
	if len(hour) == 0 || len(hour) > 61 || len(week) > historyBuckets+1 || step < historyEvery {
		t.Fatalf("hour %d week %d step %v", len(hour), len(week), step)
	}
	// old points are dropped on load and when new ones arrive
	old := histPoint{At: time.Now().Add(-8 * 24 * time.Hour)}
	os.WriteFile(path, []byte(`{"at":"`+old.At.Format(time.RFC3339)+`","cpu":1}`+"\nnot json\n"), 0600)
	if e := newHistoryStore(path); len(e.points) != 0 {
		t.Fatalf("expired point kept: %+v", e.points)
	}
}

func TestHistoryEndpointAndCompaction(t *testing.T) {
	a := testAgent(t)
	h := a.routes()
	if r := request(h, "GET", "/history?range=1h", "", nil, nil); r.Code != 200 || !strings.Contains(r.Body.String(), `"points":[]`) {
		t.Fatalf("empty history: %d %s", r.Code, r.Body.String())
	}
	if r := request(h, "GET", "/history?range=1y", "", nil, nil); r.Code != 400 {
		t.Fatalf("bad range: %d", r.Code)
	}
	now := time.Now()
	for i := 0; i < historyCompact+5; i++ {
		a.history.mu.Lock()
		a.history.store(histPoint{At: now.Add(time.Duration(i-historyCompact-5) * time.Second), CPU: 1})
		a.history.mu.Unlock()
	}
	raw, _ := os.ReadFile(a.history.path)
	if n := strings.Count(string(raw), "\n"); n > historyCompact+5 || n < 1 {
		t.Fatalf("file has %d lines", n)
	}
	if got := len(newHistoryStore(a.history.path).points); got != historyCompact+5 {
		t.Fatalf("after compaction %d points", got)
	}
}
