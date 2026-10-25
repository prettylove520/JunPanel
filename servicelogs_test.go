package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestServiceLogs(t *testing.T) {
	a := testAgent(t)
	var gotName string
	var gotArgs []string
	journal := "-- Journal begins at Mon --\n2026-10-22T10:00:00+0800 host caddy[1]: serving\n2026-10-22T10:00:01+0800 host caddy[1]: ERROR tls handshake failed\n\x1b[31m2026-10-22T10:00:02+0800 host caddy[1]: other line\x1b[0m\n"
	a.runner = func(_ time.Duration, name string, args ...string) (string, error) {
		gotName, gotArgs = name, args
		return journal, nil
	}
	get := func(query string) (int, map[string]any) {
		r := request(a.routes(), "GET", "/logs?"+query, "", nil, nil)
		var v map[string]any
		json.Unmarshal(r.Body.Bytes(), &v)
		return r.Code, v
	}
	code, v := get("unit=caddy")
	if code != 200 || gotName != "/usr/bin/journalctl" || !strings.Contains(strings.Join(gotArgs, " "), "--unit caddy.service") {
		t.Fatalf("%d %v %q %v", code, v, gotName, gotArgs)
	}
	text := v["text"].(string)
	if strings.Contains(text, "Journal begins") || strings.Contains(text, "\x1b") || !strings.Contains(text, "serving") || v["lines"] != float64(3) {
		t.Fatalf("text: %q lines=%v", text, v["lines"])
	}
	// Search is case-insensitive and applied to a larger read.
	code, v = get("unit=caddy&search=tls")
	if code != 200 || v["lines"] != float64(1) || !strings.Contains(v["text"].(string), "handshake failed") || !strings.Contains(strings.Join(gotArgs, " "), "--lines 3000") {
		t.Fatalf("search: %d %v %v", code, v, gotArgs)
	}
	// Level maps to a journal priority.
	if get("unit=agent&level=err"); !strings.Contains(strings.Join(gotArgs, " "), "--priority err") || !strings.Contains(strings.Join(gotArgs, " "), "junpanel-agent.service") {
		t.Fatalf("level: %v", gotArgs)
	}
	// Only the listed units, sane numbers, no option injection through any parameter.
	for _, q := range []string{"unit=sshd", "unit=caddy.service", "unit=", "unit=caddy&lines=5", "unit=caddy&lines=100000", "unit=caddy&lines=abc", "unit=caddy&level=debug", "unit=caddy&level=--since", "unit=caddy&search=" + strings.Repeat("a", 101), "unit=--help"} {
		gotName = ""
		if code, _ := get(q); code != 400 || gotName != "" {
			t.Fatalf("%q accepted (%d), journalctl run: %q", q, code, gotName)
		}
	}
	// A search starting with a dash is only text, never an option.
	if code, _ := get("unit=caddy&search=--since"); code != 200 || strings.Contains(strings.Join(gotArgs, " "), "--since") {
		t.Fatalf("search as option: %d %v", code, gotArgs)
	}
	// No journalctl: a clear error.
	a.runner = func(time.Duration, string, ...string) (string, error) { return "", errors.New("not found") }
	if code, _ := get("unit=caddy"); code != 502 {
		t.Fatalf("missing journalctl: %d", code)
	}
	// Large output is cut from the front, on a line boundary.
	a.runner = func(time.Duration, string, ...string) (string, error) {
		return strings.Repeat(strings.Repeat("x", 1999)+"\n", 200), nil
	}
	if code, v := get("unit=caddy&lines=1000"); code != 200 || v["trimmed"] != true || len(v["text"].(string)) > logMaxResponse {
		t.Fatalf("large output: %d %v", code, v["trimmed"])
	}
}
