package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Off-site copies of backups: a WebDAV folder or an S3-compatible bucket. The upload runs after
// every new backup when "auto" is on, or on demand for one backup. Restores still use the local copy.

type offsiteConfig struct {
	Type   string `json:"type"` // "" (off), "webdav" or "s3"
	URL    string `json:"url"`  // WebDAV folder URL, or S3 endpoint
	User   string `json:"user"` // WebDAV user, or S3 access key id
	Secret string `json:"secret,omitempty"`
	Bucket string `json:"bucket,omitempty"`
	Region string `json:"region,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	Auto   bool   `json:"auto"`
}

var (
	bucketName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	regionName = regexp.MustCompile(`^[a-z0-9-]{2,30}$`)
	prefixPart = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)
)

func (a *agent) offsitePath() string { return filepath.Join(a.cfg.State, "offsite.json") }

func (a *agent) offsiteConfig() offsiteConfig {
	var c offsiteConfig
	loadJSON(a.offsitePath(), &c)
	return c
}

func (c offsiteConfig) segments() []string {
	out := []string{}
	for _, p := range strings.Split(c.Prefix, "/") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c offsiteConfig) validate() error {
	if c.Type == "" {
		return nil
	}
	if c.Type != "webdav" && c.Type != "s3" {
		return bad(400, "异地备份类型无效")
	}
	u, e := url.Parse(c.URL)
	if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return bad(400, "地址需为 https://主机/路径 形式，且不含账号、参数")
	}
	if c.User == "" || len(c.User) > 200 || len(c.Secret) > 500 {
		return bad(400, "请填写账号和密码（密钥）")
	}
	for _, p := range c.segments() {
		if !prefixPart.MatchString(p) || p == "." || p == ".." {
			return bad(400, "目录只能含字母、数字、点、下划线和连字符，用 / 分层")
		}
	}
	if c.Type == "s3" {
		if !bucketName.MatchString(c.Bucket) {
			return bad(400, "存储桶名称无效")
		}
		if !regionName.MatchString(c.Region) {
			return bad(400, "区域无效，例如 us-east-1")
		}
	}
	return nil
}

// ---- AWS Signature Version 4 (payload not signed) ----

func hmacSum(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func s3Escape(path string) string {
	var b strings.Builder
	for _, c := range []byte(path) {
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' || c == '/' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// s3Signature returns the signature for a request with the given headers (names lower-case).
func s3Signature(method, path string, headers map[string]string, payloadHash, amzDate, secret, region string) (signature, signedHeaders string) {
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canon strings.Builder
	for _, k := range names {
		canon.WriteString(k + ":" + strings.TrimSpace(headers[k]) + "\n")
	}
	signedHeaders = strings.Join(names, ";")
	request := strings.Join([]string{method, s3Escape(path), "", canon.String(), signedHeaders, payloadHash}, "\n")
	scope := amzDate[:8] + "/" + region + "/s3/aws4_request"
	hash := sha256.Sum256([]byte(request))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(hash[:])
	key := hmacSum(hmacSum(hmacSum(hmacSum([]byte("AWS4"+secret), amzDate[:8]), region), "s3"), "aws4_request")
	return hex.EncodeToString(hmacSum(key, toSign)), signedHeaders
}

// ---- requests ----

func (c offsiteConfig) newRequest(ctx context.Context, method string, parts []string, data io.Reader, size int64) (*http.Request, error) {
	u, e := url.Parse(c.URL)
	if e != nil {
		return nil, e
	}
	base := strings.TrimRight(u.Path, "/")
	if c.Type == "s3" {
		base += "/" + c.Bucket
	}
	path := base + "/" + strings.Join(parts, "/")
	path = strings.TrimRight(path, "/")
	if strings.HasSuffix(strings.Join(parts, "/"), "/") {
		path += "/"
	}
	u.Path, u.RawPath = path, ""
	if c.Type == "s3" {
		u.RawPath = s3Escape(path)
	}
	req, e := http.NewRequestWithContext(ctx, method, u.String(), data)
	if e != nil {
		return nil, e
	}
	if data != nil {
		req.ContentLength = size
	}
	if c.Type == "webdav" {
		req.SetBasicAuth(c.User, c.Secret)
		return req, nil
	}
	amzDate := time.Now().UTC().Format("20060102T150405Z")
	headers := map[string]string{"host": u.Host, "x-amz-content-sha256": "UNSIGNED-PAYLOAD", "x-amz-date": amzDate}
	sig, signed := s3Signature(method, path, headers, "UNSIGNED-PAYLOAD", amzDate, c.Secret, c.Region)
	req.Header.Set("x-amz-content-sha256", "UNSIGNED-PAYLOAD")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s/%s/s3/aws4_request, SignedHeaders=%s, Signature=%s", c.User, amzDate[:8], c.Region, signed, sig))
	return req, nil
}

func (c offsiteConfig) do(req *http.Request, ok ...int) (*http.Response, error) {
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("无法连接：%s", cleanText(truncate(e.Error(), 200)))
	}
	for _, code := range ok {
		if resp.StatusCode == code {
			return resp, nil
		}
	}
	defer resp.Body.Close()
	text, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	switch resp.StatusCode {
	case 401, 403:
		return nil, fmt.Errorf("被拒绝（HTTP %d）：账号、密码或权限不对", resp.StatusCode)
	case 301, 302, 307, 308:
		return nil, fmt.Errorf("服务器要求跳转到别的地址（HTTP %d），请填写最终地址", resp.StatusCode)
	}
	return nil, fmt.Errorf("服务器返回 HTTP %d %s", resp.StatusCode, cleanText(truncate(strings.TrimSpace(string(text)), 160)))
}

// put uploads one file below the configured folder, creating WebDAV folders as needed.
func (c offsiteConfig) put(ctx context.Context, name string, data io.Reader, size int64) error {
	if c.Type == "webdav" {
		dir := []string{}
		for _, p := range c.segments() {
			dir = append(dir, p)
			req, e := c.newRequest(ctx, "MKCOL", append(append([]string{}, dir...), ""), nil, 0)
			if e != nil {
				return e
			}
			resp, e := c.do(req, 200, 201, 204, 301, 405, 409)
			if e != nil {
				return e
			}
			resp.Body.Close()
		}
	}
	req, e := c.newRequest(ctx, "PUT", append(c.segments(), name), data, size)
	if e != nil {
		return e
	}
	resp, e := c.do(req, 200, 201, 204)
	if e != nil {
		return e
	}
	resp.Body.Close()
	return nil
}

func (c offsiteConfig) remove(ctx context.Context, name string) error {
	req, e := c.newRequest(ctx, "DELETE", append(c.segments(), name), nil, 0)
	if e != nil {
		return e
	}
	resp, e := c.do(req, 200, 202, 204, 404)
	if e != nil {
		return e
	}
	resp.Body.Close()
	return nil
}

func remoteName(m backupMeta) string {
	return unsafeName.ReplaceAllString(fmt.Sprintf("%s-%s-%s-%s%s", m.Type, m.Target, m.Created.Format("20060102-150405"), m.ID[2:10], strings.TrimPrefix(m.File, m.ID)), "_")
}

// uploadBackup sends the verified local file to the off-site target and records the result.
func (a *agent) uploadBackup(ctx context.Context, cfg offsiteConfig, meta backupMeta) error {
	f, e := a.openBackup(meta)
	if e != nil {
		return e
	}
	defer f.Close()
	e = cfg.put(ctx, remoteName(meta), f, meta.Size)
	if cur, err := a.readBackup(meta.ID); err == nil { // the backup may have been deleted meanwhile
		if e == nil {
			now := time.Now().UTC()
			cur.Remote, cur.RemoteError = &now, ""
		} else {
			cur.RemoteError = truncate(e.Error(), 200)
		}
		atomicJSON(filepath.Join(a.backupDir(), cur.ID+".json"), cur, 0600)
	}
	return e
}

// afterBackup is called from inside the backup job once the local copy is complete.
func (a *agent) afterBackup(ctx context.Context, out io.Writer, meta backupMeta) {
	cfg := a.offsiteConfig()
	if cfg.Type == "" || !cfg.Auto || meta.ID == "" {
		return
	}
	if e := a.uploadBackup(ctx, cfg, meta); e != nil {
		fmt.Fprintf(out, "异地上传失败（本地备份已保存）：%v\n", e)
		return
	}
	fmt.Fprintf(out, "已上传到异地备份：%s\n", remoteName(meta))
}

// ---- handlers ----

func (a *agent) offsiteGet(w http.ResponseWriter, r *http.Request) error {
	c := a.offsiteConfig()
	has := c.Secret != ""
	c.Secret = ""
	send(w, 200, map[string]any{"config": c, "has_secret": has})
	return nil
}

func (a *agent) offsiteSave(w http.ResponseWriter, r *http.Request) error {
	var c offsiteConfig
	if e := body(r, &c); e != nil {
		return e
	}
	c.URL, c.User, c.Bucket, c.Region = strings.TrimSpace(c.URL), strings.TrimSpace(c.User), strings.TrimSpace(c.Bucket), strings.TrimSpace(c.Region)
	c.Prefix = strings.Trim(strings.TrimSpace(c.Prefix), "/")
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.offsiteConfig()
	if c.Secret == "" && c.Type != "" {
		c.Secret = old.Secret // blank keeps the stored secret
	}
	if c.Type == "" {
		c = offsiteConfig{}
	}
	if e := c.validate(); e != nil {
		return e
	}
	if e := atomicJSON(a.offsitePath(), c, 0600); e != nil {
		return e
	}
	send(w, 200, map[string]any{"ok": true})
	return nil
}

// offsiteTest uploads and removes a small file to prove the settings work.
func (a *agent) offsiteTest(w http.ResponseWriter, r *http.Request) error {
	var c offsiteConfig
	if e := body(r, &c); e != nil {
		return e
	}
	c.URL, c.User, c.Bucket, c.Region = strings.TrimSpace(c.URL), strings.TrimSpace(c.User), strings.TrimSpace(c.Bucket), strings.TrimSpace(c.Region)
	c.Prefix = strings.Trim(strings.TrimSpace(c.Prefix), "/")
	if c.Secret == "" {
		c.Secret = a.offsiteConfig().Secret
	}
	if c.Type == "" {
		return bad(400, "请先选择异地备份类型")
	}
	if e := c.validate(); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	name := ".junpanel-test-" + randomID(6)
	data := "JunPanel connection test\n"
	if e := c.put(ctx, name, strings.NewReader(data), int64(len(data))); e != nil {
		return bad(502, e.Error())
	}
	if e := c.remove(ctx, name); e != nil {
		return bad(502, "能上传但无法删除测试文件："+e.Error())
	}
	send(w, 200, map[string]any{"ok": true})
	return nil
}

func (a *agent) backupUpload(w http.ResponseWriter, r *http.Request) error {
	meta, e := a.readBackup(r.PathValue("id"))
	if e != nil {
		return e
	}
	cfg := a.offsiteConfig()
	if cfg.Type == "" {
		return bad(409, "还没有设置异地备份")
	}
	j, e := a.jobs.startWithin(backupTimeout, "异地上传 · "+meta.ID, func(ctx context.Context, out *limitedBuffer) error {
		if e := a.uploadBackup(ctx, cfg, meta); e != nil {
			return e
		}
		fmt.Fprintf(out, "已上传到异地备份：%s\n", remoteName(meta))
		return nil
	})
	if e != nil {
		return e
	}
	send(w, 202, j)
	return nil
}
