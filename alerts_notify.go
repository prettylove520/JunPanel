package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var alertFormats = map[string]string{"generic": "通用 JSON", "wecom": "企业微信机器人", "dingtalk": "钉钉机器人", "feishu": "飞书机器人"}

type notice struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Severity string `json:"severity"`
}

func noticeLine(n notice) string {
	label := map[string]string{"triggered": "告警", "reminder": "仍未解决", "resolved": "已恢复", "test": "测试"}[n.Kind]
	line := "【" + label + "】" + n.Title
	if n.Detail != "" {
		line += "\n    " + n.Detail
	}
	return line
}

func noticeText(host string, notices []notice, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "JunPanel · %s · %s", host, now.Format("2006-01-02 15:04:05"))
	for _, n := range notices {
		b.WriteString("\n" + noticeLine(n))
	}
	return b.String()
}

// buildPayload renders the notices in the body format the receiving service expects.
func buildPayload(format, host string, notices []notice, now time.Time) ([]byte, error) {
	text := noticeText(host, notices, now)
	switch format {
	case "wecom", "dingtalk":
		return json.Marshal(map[string]any{"msgtype": "text", "text": map[string]string{"content": text}})
	case "feishu":
		return json.Marshal(map[string]any{"msg_type": "text", "content": map[string]string{"text": text}})
	case "generic":
		return json.Marshal(map[string]any{"source": "junpanel", "host": host, "time": now.Format(time.RFC3339), "text": text, "alerts": notices})
	}
	return nil, bad(400, "通知格式无效")
}

func validWebhook(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 1000 || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" || strings.ContainsAny(raw, " \r\n\t") {
		return nil, bad(400, "Webhook 地址需为 http:// 或 https:// 开头的完整地址")
	}
	return u, nil
}

// webhookClient never follows redirects, so a configured address cannot bounce the
// request somewhere else.
func webhookClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// send posts one notification and reports whether the service accepted it. The
// Chinese robot services answer 200 with an error code in the body on failure.
func (a *agent) send(cfg alertConfig, notices []notice, now time.Time) error {
	if cfg.URL == "" {
		return bad(400, "尚未设置 Webhook 地址")
	}
	if _, e := validWebhook(cfg.URL); e != nil {
		return e
	}
	body, e := buildPayload(cfg.Format, a.hostname(), notices, now)
	if e != nil {
		return e
	}
	req, e := http.NewRequest("POST", cfg.URL, bytes.NewReader(body))
	if e != nil {
		return bad(400, "Webhook 地址无效")
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "JunPanel/"+version)
	resp, e := a.webhook.Do(req)
	if e != nil {
		return bad(502, "无法连接 Webhook："+hostError(e))
	}
	defer resp.Body.Close()
	reply, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return bad(502, fmt.Sprintf("Webhook 返回 HTTP %d", resp.StatusCode))
	}
	if cfg.Format == "wecom" || cfg.Format == "dingtalk" || cfg.Format == "feishu" {
		var answer struct {
			Errcode *int   `json:"errcode"`
			Errmsg  string `json:"errmsg"`
			Code    *int   `json:"code"`
			Msg     string `json:"msg"`
		}
		if json.Unmarshal(reply, &answer) == nil {
			if answer.Errcode != nil && *answer.Errcode != 0 {
				return bad(502, "机器人拒绝了消息："+truncate(cleanText(answer.Errmsg), 200))
			}
			if answer.Code != nil && *answer.Code != 0 {
				return bad(502, "机器人拒绝了消息："+truncate(cleanText(answer.Msg), 200))
			}
		}
	}
	return nil
}

// hostError keeps connection failures readable without leaking the full URL, which
// may contain a secret key.
func hostError(e error) string {
	text := e.Error()
	if ue, ok := e.(*url.Error); ok {
		text = ue.Err.Error()
	}
	return truncate(cleanText(text), 200)
}
