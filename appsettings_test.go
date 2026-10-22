package main

import (
	"strings"
	"testing"
)

func TestAppSettings(t *testing.T) {
	a := testAgent(t)
	h := a.routes()
	rows := []appRecord{
		{ID: "vw", Name: "密码", Template: "vaultwarden", Port: 18001, Env: map[string]string{"API_TOKEN": "s3cret"}},
		{ID: "other", Name: "别的", Template: "memos", Port: 18002},
		{ID: "gone", Name: "已卸载", Template: "memos", Port: 18003, Removed: true},
		{ID: "busy", Name: "忙", Template: "memos", Port: 18004, Busy: true},
	}
	if e := a.storeApps(rows); e != nil {
		t.Fatal(e)
	}
	r := request(h, "GET", "/apps/vw/settings", "", nil, nil)
	if r.Code != 200 || strings.Contains(r.Body.String(), "s3cret") || !strings.Contains(r.Body.String(), maskedValue) || !strings.Contains(r.Body.String(), "SIGNUPS_ALLOWED") {
		t.Fatalf("settings view: %d %s", r.Code, r.Body.String())
	}
	if r = request(h, "GET", "/apps/nope/settings", "", nil, nil); r.Code != 404 {
		t.Fatalf("missing app: %d", r.Code)
	}
	for name, c := range map[string]struct {
		id, body string
		code     int
	}{
		"port low":    {"vw", `{"port":80,"env":{}}`, 400},
		"port taken":  {"vw", `{"port":18002,"env":{}}`, 409},
		"bad name":    {"vw", `{"port":18001,"env":{"a b":"1"}}`, 400},
		"protected":   {"vw", `{"port":18001,"env":{"POSTGRES_PASSWORD":"x"}}`, 400},
		"newline":     {"vw", `{"port":18001,"env":{"A":"x\ny"}}`, 400},
		"removed app": {"gone", `{"port":18003,"env":{}}`, 409},
		"busy app":    {"busy", `{"port":18004,"env":{}}`, 409},
		"unknown app": {"nope", `{"port":18005,"env":{}}`, 404},
	} {
		if r = request(h, "POST", "/apps/"+c.id+"/settings", c.body, nil, nil); r.Code != c.code {
			t.Fatalf("%s: got %d want %d: %s", name, r.Code, c.code, r.Body.String())
		}
	}
	got, _ := a.readApps()
	if got[0].Port != 18001 || got[0].Env["API_TOKEN"] != "s3cret" {
		t.Fatalf("a rejected change altered the record: %+v", got[0])
	}
}

func TestComposeUsesSettings(t *testing.T) {
	for _, tpl := range appCatalog {
		rec := appRecord{ID: "x", Template: tpl.Key, Port: 18100, Password: "pw", DBPassword: "dbpw",
			Env: map[string]string{"MY_FLAG": "on", "POSTGRES_PASSWORD": "hijack", "PASSWORD": "hijack"}}
		raw, e := composeFor(rec)
		if e != nil {
			t.Fatalf("%s: %v", tpl.Key, e)
		}
		s := string(raw)
		if !strings.Contains(s, `"MY_FLAG": "on"`) || strings.Contains(s, "hijack") {
			t.Fatalf("%s: settings not applied safely:\n%s", tpl.Key, s)
		}
	}
}
