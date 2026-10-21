package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestAuditLogRecordsWithoutSecrets(t *testing.T) {
	auth, pw := testAccount(t)
	web, e := newWeb(auth.Path, "unused", 8787, false)
	if e != nil {
		t.Fatal(e)
	}
	web.proxy.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/conflict") {
			return fakeResponse(409, `{"error":"x"}`), nil
		}
		return fakeResponse(200, `{"ok":true}`), nil
	})
	h := web.routes()
	origin := map[string]string{"Origin": "http://127.0.0.1:8787", "Content-Type": "application/json"}
	if r := request(h, "POST", "/api/login", `{"user":"admin","password":"wrong-pass-1234"}`, origin, nil); r.Code != 403 && r.Code != 401 {
		t.Fatalf("wrong login: %d", r.Code)
	}
	r := request(h, "POST", "/api/login", `{"user":"admin","password":"`+pw+`"}`, origin, nil)
	if r.Code != 200 {
		t.Fatalf("login: %d %s", r.Code, r.Body.String())
	}
	var session struct{ CSRF string }
	json.Unmarshal(r.Body.Bytes(), &session)
	cookie := r.Result().Cookies()[0]
	headers := map[string]string{"Origin": "http://127.0.0.1:8787", "X-CSRF-Token": session.CSRF, "Content-Type": "application/json"}

	request(h, "POST", "/api/databases/blog/query", `{"database":"wp","sql":"SELECT secret_marker_sql"}`, headers, cookie)
	request(h, "POST", "/api/files/save", `{"path":"/sites/a/index.html","text":"secret_marker_text","revision":""}`, headers, cookie)
	request(h, "POST", "/api/stacks", `{"name":"web","source":"services: {x: {environment: {PW: secret_marker_yaml}}}","create":true}`, headers, cookie)
	request(h, "POST", "/api/apps/notes/action", `{"action":"restart"}`, headers, cookie)
	request(h, "DELETE", "/api/backups/conflict", "", headers, cookie)
	request(h, "GET", "/api/backups/b_00000000000000000000/download", "", headers, cookie)
	request(h, "POST", "/api/heartbeat", `{}`, headers, cookie)
	request(h, "POST", "/api/stacks/check", `{"name":"web","source":"x"}`, headers, cookie)
	request(h, "GET", "/api/overview", "", headers, cookie)
	request(h, "POST", "/api/security", `{"action":"password","password":"wrong-current-5678","new_password":"secret-new-password"}`, headers, cookie)
	request(h, "POST", "/api/credentials", `{"id":"blog","password":"wrong-reauth-9012"}`, headers, cookie)

	var ops []auditEntry
	r = request(h, "GET", "/api/audit?kind=operation", "", headers, cookie)
	json.Unmarshal(r.Body.Bytes(), &ops)
	got := []string{}
	for _, op := range ops {
		got = append(got, op.Action+"|"+op.Detail+"|"+op.Result)
	}
	want := []string{
		"查看连接信息|id=blog|failed",
		"修改密码||failed",
		"下载备份||ok",
		"删除备份||failed",
		"应用操作|action=restart|ok",
		"保存编排|name=web|ok",
		"保存文件|path=/sites/a/index.html|ok",
		"执行 SQL|database=wp|ok",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("operations, newest first:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if ops[3].Status != 409 || ops[0].IP == "" {
		t.Fatalf("status or address not kept: %+v", ops[3])
	}
	var logins []auditEntry
	json.Unmarshal(request(h, "GET", "/api/audit?kind=login", "", headers, cookie).Body.Bytes(), &logins)
	actions := []string{}
	for _, l := range logins {
		actions = append(actions, l.Action)
	}
	if strings.Join(actions, ",") != "再次验证密码失败,登录成功,登录失败" {
		t.Fatalf("logins: %v", actions)
	}
	if r = request(h, "GET", "/api/audit?kind=everything", "", headers, cookie); r.Code != 400 {
		t.Fatalf("bad kind: %d", r.Code)
	}
	if r = request(h, "GET", "/api/audit?kind=login", "", nil, nil); r.Code != 401 {
		t.Fatalf("audit without a session: %d", r.Code)
	}
	raw, _ := os.ReadFile(web.audit.path)
	for _, secret := range []string{pw, "wrong-pass-1234", "secret_marker_sql", "secret_marker_text", "secret_marker_yaml", "wrong-current-5678", "secret-new-password", "wrong-reauth-9012", session.CSRF, cookie.Value} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("audit log contains %q", secret)
		}
	}
	if st, _ := os.Stat(web.audit.path); st.Mode().Perm() != 0600 {
		t.Fatalf("audit log mode %v", st.Mode().Perm())
	}
	request(h, "POST", "/api/logout", `{}`, headers, cookie)
	if l := web.audit.list("login", 1); len(l) != 1 || l[0].Action != "退出登录" {
		t.Fatalf("logout not recorded: %+v", l)
	}
}

func TestAuditLogRotates(t *testing.T) {
	l := newAuditLog(t.TempDir() + "/audit.log")
	l.limit = 4 << 10
	for i := 0; i < 400; i++ {
		l.add(auditEntry{Kind: "operation", Action: "保存网站", Detail: "name=x" + itoa(i), Result: "ok"})
	}
	cur, _ := os.Stat(l.path)
	old, e := os.Stat(l.path + ".1")
	if e != nil || cur.Size() > l.limit || old.Size() > l.limit {
		t.Fatalf("not rotated within the limit: %v", e)
	}
	got := l.list("operation", 1000)
	if len(got) == 0 || got[0].Detail != "name=x399" {
		t.Fatalf("newest entry lost: %+v", got[:1])
	}
	if len(l.list("operation", 5)) != 5 {
		t.Fatal("limit ignored")
	}
}
