package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The worked example from the AWS Signature Version 4 documentation (GET Object with a Range header).
func TestS3SignatureMatchesAWSExample(t *testing.T) {
	headers := map[string]string{
		"host": "examplebucket.s3.amazonaws.com", "range": "bytes=0-9",
		"x-amz-content-sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "x-amz-date": "20130524T000000Z",
	}
	sig, signed := s3Signature("GET", "/test.txt", headers, headers["x-amz-content-sha256"], "20130524T000000Z", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1")
	if sig != "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41" || signed != "host;range;x-amz-content-sha256;x-amz-date" {
		t.Fatalf("signature %s (%s)", sig, signed)
	}
}

type recorded struct{ method, path, auth, body string }

func fakeStore(t *testing.T, status func(method string) int) (*httptest.Server, *[]recorded) {
	var mu sync.Mutex
	var log []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		mu.Lock()
		log = append(log, recorded{r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization"), string(data)})
		mu.Unlock()
		w.WriteHeader(status(r.Method))
	}))
	t.Cleanup(srv.Close)
	return srv, &log
}

func TestOffsiteWebDAVAndS3(t *testing.T) {
	srv, log := fakeStore(t, func(string) int { return 201 })
	dav := offsiteConfig{Type: "webdav", URL: srv.URL + "/dav", User: "me", Secret: "pw", Prefix: "junpanel/main"}
	if e := dav.validate(); e != nil {
		t.Fatal(e)
	}
	if e := dav.put(t.Context(), "a.tar.gz", strings.NewReader("hello"), 5); e != nil {
		t.Fatal(e)
	}
	l := *log
	if len(l) != 3 || l[0].method != "MKCOL" || l[0].path != "/dav/junpanel/" || l[1].path != "/dav/junpanel/main/" || l[2].method != "PUT" || l[2].path != "/dav/junpanel/main/a.tar.gz" || l[2].body != "hello" || !strings.HasPrefix(l[2].auth, "Basic ") {
		t.Fatalf("webdav requests: %+v", l)
	}
	*log = nil
	s3 := offsiteConfig{Type: "s3", URL: srv.URL, User: "AKID", Secret: "secret", Bucket: "my-bucket", Region: "eu-west-1", Prefix: "bk"}
	if e := s3.validate(); e != nil {
		t.Fatal(e)
	}
	if e := s3.put(t.Context(), "a b.tar.gz", strings.NewReader("data"), 4); e != nil {
		t.Fatal(e)
	}
	l = *log
	if len(l) != 1 || l[0].method != "PUT" || l[0].path != "/my-bucket/bk/a%20b.tar.gz" || l[0].body != "data" ||
		!strings.HasPrefix(l[0].auth, "AWS4-HMAC-SHA256 Credential=AKID/") || !strings.Contains(l[0].auth, "/eu-west-1/s3/aws4_request") {
		t.Fatalf("s3 request: %+v", l)
	}
	if strings.Contains(l[0].auth, "secret") {
		t.Fatal("secret leaked into the header")
	}
}

func TestOffsiteErrorsAndValidation(t *testing.T) {
	srv, _ := fakeStore(t, func(string) int { return 403 })
	c := offsiteConfig{Type: "webdav", URL: srv.URL, User: "me", Secret: "bad"}
	if e := c.put(t.Context(), "x", strings.NewReader("1"), 1); e == nil || !strings.Contains(e.Error(), "账号、密码或权限") {
		t.Fatalf("403: %v", e)
	}
	for name, cfg := range map[string]offsiteConfig{
		"scheme":   {Type: "webdav", URL: "ftp://h/x", User: "u", Secret: "p"},
		"userinfo": {Type: "webdav", URL: "https://a:b@h/x", User: "u", Secret: "p"},
		"no user":  {Type: "webdav", URL: "https://h/x"},
		"dotdot":   {Type: "webdav", URL: "https://h/x", User: "u", Secret: "p", Prefix: "a/../b"},
		"bucket":   {Type: "s3", URL: "https://h", User: "u", Secret: "p", Bucket: "Bad_Name", Region: "us-east-1"},
		"region":   {Type: "s3", URL: "https://h", User: "u", Secret: "p", Bucket: "good-name", Region: "x y"},
		"type":     {Type: "ftp", URL: "https://h", User: "u", Secret: "p"},
	} {
		if cfg.validate() == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestOffsiteSaveKeepsSecretAndUploadsAfterBackup(t *testing.T) {
	a := testAgent(t)
	h := a.routes()
	srv, log := fakeStore(t, func(m string) int {
		if m == "DELETE" {
			return 204
		}
		return 201
	})
	save := `{"type":"webdav","url":"` + srv.URL + `","user":"me","secret":"pw","prefix":"bk","auto":true}`
	if r := request(h, "POST", "/offsite", save, nil, nil); r.Code != 200 {
		t.Fatalf("save: %d %s", r.Code, r.Body.String())
	}
	r := request(h, "POST", "/offsite", `{"type":"webdav","url":"`+srv.URL+`","user":"me","secret":"","prefix":"bk","auto":true}`, nil, nil)
	if r.Code != 200 || a.offsiteConfig().Secret != "pw" {
		t.Fatalf("blank secret must keep the stored one: %d", r.Code)
	}
	if r = request(h, "GET", "/offsite", "", nil, nil); strings.Contains(r.Body.String(), "pw") || !strings.Contains(r.Body.String(), `"has_secret":true`) {
		t.Fatalf("secret exposed: %s", r.Body.String())
	}
	if r = request(h, "POST", "/offsite/test", `{"type":"webdav","url":"`+srv.URL+`","user":"me","secret":"","prefix":"bk"}`, nil, nil); r.Code != 200 {
		t.Fatalf("test: %d %s", r.Code, r.Body.String())
	}
	meta, e := a.writeBackup(backupMeta{Type: "site", Target: "blog"}, "tar.gz", func(w io.Writer) error { _, e := w.Write([]byte("payload")); return e })
	if e != nil {
		t.Fatal(e)
	}
	var out strings.Builder
	*log = nil
	a.afterBackup(t.Context(), &out, meta)
	if !strings.Contains(out.String(), "已上传到异地备份") {
		t.Fatalf("output: %q", out.String())
	}
	got, _ := a.readBackup(meta.ID)
	if got.Remote == nil || got.RemoteError != "" {
		t.Fatalf("upload not recorded: %+v", got)
	}
	// a failing target keeps the local backup and records why
	srv.Close()
	out.Reset()
	a.afterBackup(t.Context(), &out, meta)
	got, _ = a.readBackup(meta.ID)
	if !strings.Contains(out.String(), "异地上传失败") || got.RemoteError == "" || got.Remote == nil {
		t.Fatalf("failure handling: %q %+v", out.String(), got)
	}
	// turning it off removes the stored config
	request(h, "POST", "/offsite", `{"type":""}`, nil, nil)
	if a.offsiteConfig().Type != "" || a.offsiteConfig().Secret != "" {
		t.Fatal("config not cleared")
	}
}
