package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestExistingCaddySitesAppearInOverviewAndList(t *testing.T) {
	a := testAgent(t)
	managed := []site{{ID: "s_0123456789abcdef0123", Name: "Managed", Domains: []string{"managed.example.com"}, Enabled: true}}
	if err := atomicJSON(filepath.Join(a.cfg.State, "sites.json"), managed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.cfg.Fragments, "panel-access.caddy"), []byte("# JunPanel public HTTPS access - managed by public-access.sh\nhttps://8.8.8.8 {\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	a.capture = func(time.Duration, string, ...string) (string, error) {
		return `{"apps":{"http":{"servers":{"srv0":{"routes":[{"match":[{"host":["legacy.example.com","managed.example.com","8.8.8.8","127.0.0.1","localhost"]}]}]}}}}}`, nil
	}
	a.docker = &dockerClient{API: "/v1.56", HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("all") != "1" {
			t.Error("existing stopped containers were excluded")
		}
		return fakeResponse(200, `[{"Id":"`+strings.Repeat("a", 64)+`","Names":["/existing-running"],"State":"running"},{"Id":"`+strings.Repeat("b", 64)+`","Names":["/existing-stopped"],"State":"exited"}]`), nil
	})}}
	for _, path := range []string{"/overview", "/sites"} {
		r := request(a.routes(), "GET", path, "", nil, nil)
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
		var v struct {
			External   []string    `json:"external_hosts"`
			Sites      []site      `json:"sites"`
			Containers []container `json:"containers"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(v.External, []string{"legacy.example.com"}) || len(v.Sites) != 1 {
			t.Fatalf("%s inventory: %+v", path, v)
		}
		if path == "/overview" && len(v.Containers) != 2 {
			t.Fatal("pre-existing containers missing")
		}
	}
	if len(a.jobs.list()) != 0 {
		t.Fatal("read-only discovery started a job")
	}
}

func TestOverviewPreservesInventoryErrors(t *testing.T) {
	a := testAgent(t)
	a.capture = func(time.Duration, string, ...string) (string, error) { return "", fmt.Errorf("unavailable") }
	a.docker = &dockerClient{API: "/v1.56", HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return fakeResponse(500, `{}`), nil })}}
	if err := os.WriteFile(filepath.Join(a.cfg.State, "sites.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.cfg.State, "apps.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	r := request(a.routes(), "GET", "/overview", "", nil, nil)
	var v map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"docker_error", "sites_error", "apps_error", "caddy_error"} {
		if v[key] == "" || v[key] == nil {
			t.Fatalf("%s failure hidden as empty inventory: %s", key, r.Body.String())
		}
	}
}
