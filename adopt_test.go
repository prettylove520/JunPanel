package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAdoptableProjects(t *testing.T) {
	all := []container{
		{ID: "1", Name: "ollama", Image: "ollama/ollama", State: "running", Project: "ai", Ports: []portInfo{{IP: "127.0.0.1", Public: 11434, Private: 11434, Protocol: "tcp"}}},
		{ID: "2", Name: "open-webui", Image: "webui", State: "exited", Project: "ai"},
		{ID: "3", Name: "junpanel-notes-app-1", State: "running", Project: "junpanel-notes"},
		{ID: "4", Name: "web", State: "running", Project: "mine", Stack: "mine"},
		{ID: "5", Name: "solo", State: "running"},
		{ID: "6", Name: "taken", State: "running", Project: "done"},
		{ID: "7", Name: "once", State: "exited", Project: "jobs", Oneoff: true},
	}
	apps := []appRecord{{ID: "done", Adopted: true, Project: "done"}}
	got := adoptableProjects(all, apps)
	if len(got) != 1 || got[0].Project != "ai" || got[0].Running != 1 || got[0].Port != 11434 || len(got[0].Containers) != 2 {
		t.Fatalf("adoptable: %+v", got)
	}
	if got := adoptableProjects(all, []appRecord{{ID: "ai", Adopted: true, Project: "ai", Removed: false}, {ID: "done", Adopted: true, Project: "done"}}); len(got) != 0 {
		t.Fatalf("an adopted project is not offered again: %+v", got)
	}
}

func TestAdoptedID(t *testing.T) {
	apps := []appRecord{{ID: "ai"}, {ID: "ai-2"}}
	for project, want := range map[string]string{"ai": "ai-3", "My_App.v2": "my-app-v2", "9lives": "p-9lives", "x": "x-x", "_": "p-"} {
		if got := adoptedID(project, apps); got != want {
			t.Errorf("adoptedID(%q) = %q, want %q", project, got, want)
		}
		if e := requireName(adoptedID(project, apps)); e != nil {
			t.Errorf("adoptedID(%q) is not a valid app id: %v", project, e)
		}
	}
}

func TestAdoptEndpointsAndActions(t *testing.T) {
	a := testAgent(t)
	dock := &fakeDocker{rows: []fakeContainer{
		{ID: cid('a'), Name: "ollama", State: "running", Status: "Up 18 hours", Project: "ai"},
		{ID: cid('b'), Name: "open-webui", State: "exited", Status: "Exited (0) 3 weeks ago", Project: "ai"},
	}}
	a.docker = dock.client()
	h := a.routes()
	r := request(h, "GET", "/apps/adoptable", "", nil, nil)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"project":"ai"`) {
		t.Fatalf("adoptable: %d %s", r.Code, r.Body.String())
	}
	if r = request(h, "POST", "/apps/adopt", `{"project":"nothing"}`, nil, nil); r.Code != 409 {
		t.Fatalf("unknown project: %d", r.Code)
	}
	if r = request(h, "POST", "/apps/adopt", `{"project":"ai","name":"AI 工具"}`, nil, nil); r.Code != 200 {
		t.Fatalf("adopt: %d %s", r.Code, r.Body.String())
	}
	if r = request(h, "POST", "/apps/adopt", `{"project":"ai"}`, nil, nil); r.Code != 409 {
		t.Fatalf("adopting twice: %d", r.Code)
	}
	rows, _ := a.readApps()
	if len(rows) != 1 || !rows[0].Adopted || rows[0].Project != "ai" || rows[0].ID != "ai" || rows[0].Name != "AI 工具" {
		t.Fatalf("record: %+v", rows)
	}
	r = request(h, "GET", "/apps", "", nil, nil)
	var listed []map[string]any
	json.Unmarshal(r.Body.Bytes(), &listed)
	if len(listed) != 1 || listed[0]["project"] != "ai" || listed[0]["adopted"] != true || listed[0]["containers"] != float64(2) || listed[0]["running"] != float64(1) {
		t.Fatalf("list: %s", r.Body.String())
	}
	for _, action := range []string{"deploy", "remove", "upgrade", "purge", "restore"} {
		if r = request(h, "POST", "/apps/ai/action", `{"action":"`+action+`"}`, nil, nil); r.Code != 409 {
			t.Fatalf("%s must be refused for an adopted project: %d", action, r.Code)
		}
	}
	if r = request(h, "GET", "/apps/ai/settings", "", nil, nil); r.Code != 409 {
		t.Fatalf("settings: %d", r.Code)
	}
	if r = request(h, "POST", "/apps/ai/action", `{"action":"restart"}`, nil, nil); r.Code != 202 {
		t.Fatalf("restart: %d %s", r.Code, r.Body.String())
	}
	waitIdle(t, a, "ai")
	if r = request(h, "POST", "/apps/ai/action", `{"action":"release"}`, nil, nil); r.Code != 202 {
		t.Fatalf("release: %d %s", r.Code, r.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rows, _ = a.readApps()
		if len(rows) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the record was not removed: %+v", rows)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if r = request(h, "POST", "/apps/ai/action", `{"action":"release"}`, nil, nil); r.Code != 404 {
		t.Fatalf("release twice: %d", r.Code)
	}
	if r = request(h, "GET", "/apps/adoptable", "", nil, nil); !strings.Contains(r.Body.String(), `"project":"ai"`) {
		t.Fatalf("a released project can be adopted again: %s", r.Body.String())
	}
}

func waitIdle(t *testing.T, a *agent, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		rows, _ := a.readApps()
		if len(rows) == 1 && !rows[0].Busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("app %s stayed busy: %+v", id, rows)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
