package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDisplayName(t *testing.T) {
	for _, bad := range []string{strings.Repeat("长", 41), "a\nb", "a\x00b"} {
		if _, e := cleanDisplayName(bad); e == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if s, e := cleanDisplayName("  主站 de-web  "); e != nil || s != "主站 de-web" {
		t.Fatalf("clean: %q %v", s, e)
	}
	a := testAgent(t)
	h := a.routes()
	if a.displayName() != "" {
		t.Fatal("name before saving")
	}
	r := request(h, "POST", "/display-name", `{"name":"  建站机 "}`, nil, nil)
	if r.Code != 200 || a.displayName() != "建站机" {
		t.Fatalf("save: %d %s %q", r.Code, r.Body.String(), a.displayName())
	}
	if r = request(h, "POST", "/display-name", `{"name":"`+strings.Repeat("x", 41)+`"}`, nil, nil); r.Code != 400 || a.displayName() != "建站机" {
		t.Fatalf("too long: %d", r.Code)
	}
	r = request(h, "GET", "/overview", "", nil, nil)
	var ov struct {
		DisplayName string `json:"display_name"`
	}
	json.Unmarshal(r.Body.Bytes(), &ov)
	if ov.DisplayName != "建站机" {
		t.Fatalf("overview: %s", r.Body.String())
	}
	if r = request(h, "POST", "/display-name", `{"name":""}`, nil, nil); r.Code != 200 || a.displayName() != "" {
		t.Fatalf("reset: %d %q", r.Code, a.displayName())
	}
	if r = request(h, "POST", "/display-name", `{"name":""}`, nil, nil); r.Code != 200 {
		t.Fatalf("reset twice: %d", r.Code)
	}
}
