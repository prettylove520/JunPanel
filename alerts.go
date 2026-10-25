package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	alertStart       = 45 * time.Second
	alertInterval    = 30 * time.Second
	alertCertEvery   = certCacheTTL
	flapWindow       = 30 * time.Minute
	expectedFor      = 10 * time.Minute
	deliveryRetry    = 5 * time.Minute
	deliveryTries    = 3
	maxPending       = 50
	maxHistory       = 100
	recoverPolls     = 2
	diskHysteresis   = 3
	dockerFailPolls  = 3
	serviceFailPolls = 2
	siteFailPolls    = 4 // 30 s apart: a site must be down for about two minutes
)

type alertConfig struct {
	Format      string `json:"format"`
	URL         string `json:"url"`
	Notify      bool   `json:"notify"`
	DiskOn      bool   `json:"disk_on"`
	DiskPercent int    `json:"disk_percent"`
	CertOn      bool   `json:"cert_on"`
	CertDays    int    `json:"cert_days"`
	ContainerOn bool   `json:"container_on"`
	ServiceOn   bool   `json:"service_on"`
	SiteOn      bool   `json:"site_on"`
	RepeatHours int    `json:"repeat_hours"`
}

func defaultAlertConfig() alertConfig {
	return alertConfig{Format: "generic", DiskOn: true, DiskPercent: 90, CertOn: true, CertDays: 14, ContainerOn: true, ServiceOn: true, SiteOn: true, RepeatHours: 12}
}

type activeAlert struct {
	Key      string    `json:"key"`
	Title    string    `json:"title"`
	Detail   string    `json:"detail"`
	Severity string    `json:"severity"`
	Since    time.Time `json:"since"`
	LastSent time.Time `json:"last_sent"`
}
type alertEvent struct {
	Time   time.Time `json:"time"`
	Key    string    `json:"key"`
	Kind   string    `json:"kind"`
	Title  string    `json:"title"`
	Detail string    `json:"detail,omitempty"`
}
type deliveryResult struct {
	At      time.Time `json:"at"`
	OK      bool      `json:"ok"`
	Message string    `json:"message"`
}
type alertState struct {
	Active   map[string]*activeAlert `json:"active"`
	Running  []string                `json:"running"`
	Resolved map[string]time.Time    `json:"resolved"`
	History  []alertEvent            `json:"history"`
	Delivery *deliveryResult         `json:"delivery,omitempty"`
}

// finding is one problem seen right now. Need is how many checks in a row must
// see it before it counts, to ride out momentary glitches.
type finding struct {
	Key, Title, Detail, Severity string
	Need                         int
}

func (a *agent) alertConfigPath() string { return filepath.Join(a.cfg.State, "alerts.json") }
func (a *agent) alertStatePath() string  { return filepath.Join(a.cfg.State, "alerts-state.json") }
func (a *agent) readAlertConfig() (alertConfig, error) {
	cfg := defaultAlertConfig()
	e := loadJSON(a.alertConfigPath(), &cfg)
	if os.IsNotExist(e) {
		e = nil
	}
	return cfg, e
}
func (a *agent) readAlertState() (*alertState, error) {
	st := &alertState{}
	e := loadJSON(a.alertStatePath(), st)
	if os.IsNotExist(e) {
		e = nil
	}
	if st.Active == nil {
		st.Active = map[string]*activeAlert{}
	}
	if st.Resolved == nil {
		st.Resolved = map[string]time.Time{}
	}
	return st, e
}
func (a *agent) hostname() string {
	if a.host != "" {
		return a.host
	}
	name, _ := os.Hostname()
	return name
}

// expectStop records that the panel itself is stopping these containers, so
// their exit is not reported as a failure.
func (a *agent) expectStop(ids ...string) {
	a.expMu.Lock()
	defer a.expMu.Unlock()
	until := a.now().Add(expectedFor)
	for _, id := range ids {
		a.expected[id] = until
	}
}
func (a *agent) takeExpected(id string, now time.Time) bool {
	a.expMu.Lock()
	defer a.expMu.Unlock()
	until, ok := a.expected[id]
	if ok {
		delete(a.expected, id)
	}
	for k, t := range a.expected {
		if now.After(t) {
			delete(a.expected, k)
		}
	}
	return ok && now.Before(until)
}
func (a *agent) expectApp(id string) {
	rows, e := a.docker.list()
	if e != nil {
		return
	}
	var ids []string
	for _, c := range rows {
		if c.Project == "junpanel-"+id {
			ids = append(ids, c.ID)
		}
	}
	a.expectStop(ids...)
}

var unitState = regexp.MustCompile(`^[a-z-]{3,20}$`)
var exitCode = regexp.MustCompile(`^(?:Exited|Restarting) \((-?\d+)\)`)

func (a *agent) diskFindings(cfg alertConfig, st *alertState) []finding {
	type usage struct {
		path        string
		total, used uint64
	}
	var seen []usage
	for _, p := range []string{"/", a.cfg.State, a.cfg.Root} {
		d := a.diskFn(p)
		if d["total"] == 0 {
			continue
		}
		dup := false
		for _, s := range seen {
			if s.total == d["total"] && s.used == d["used"] {
				dup = true
			}
		}
		if !dup {
			seen = append(seen, usage{p, d["total"], d["used"]})
		}
	}
	var out []finding
	for _, s := range seen {
		key := "disk:" + s.path
		percent := float64(s.used) / float64(s.total) * 100
		limit := float64(cfg.DiskPercent)
		if st.Active[key] != nil {
			limit -= diskHysteresis
		}
		if percent >= limit {
			out = append(out, finding{Key: key, Title: "磁盘空间不足：" + s.path, Detail: fmt.Sprintf("已用 %.0f%%，剩余 %s（阈值 %d%%）", percent, bytesText(s.total-s.used), cfg.DiskPercent), Severity: "warning", Need: 1})
		}
	}
	return out
}
func bytesText(n uint64) string {
	f, i := float64(n), 0
	for f >= 1024 && i < 4 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", f, []string{"B", "KB", "MB", "GB", "TB"}[i])
}

// certificates returns the certificates Caddy holds for hosts it serves,
// refreshed at most every certCacheTTL.
func (a *agent) certificates(now time.Time, force bool) ([]certInfo, error) {
	a.certMu.Lock()
	defer a.certMu.Unlock()
	if !force && !a.certAt.IsZero() && now.Sub(a.certAt) < alertCertEvery {
		return a.certList, a.certErr
	}
	certs, e := readCerts(a.cfg.CaddyData)
	// The host list is read once per period and shared: the site checks use it too.
	hosts, sources, he := a.caddyInventory()
	a.invHosts, a.invSources, a.invErr = hosts, sources, he
	if e == nil && he == nil {
		// Certificates of sites that were removed stay on disk, so only those for hosts
		// Caddy still serves count. If the host list cannot be read, keep them all.
		kept := certs[:0:0]
		for _, c := range certs {
			if certServes(c, hosts) {
				kept = append(kept, c)
			}
		}
		certs = kept
	}
	a.certAt, a.certList, a.certErr = now, certs, e
	return certs, e
}
func (a *agent) certFindings(cfg alertConfig, now time.Time) []finding {
	certs, e := a.certificates(now, false)
	if e != nil {
		return nil
	}
	var out []finding
	for _, c := range certs {
		remaining := c.NotAfter.Sub(now)
		if remaining > warnWindow(c, cfg.CertDays) {
			continue
		}
		f := finding{Key: "cert:" + c.Name, Need: 1, Severity: "warning"}
		if remaining <= 0 {
			f.Title, f.Severity = "证书已过期："+c.Name, "critical"
			f.Detail = "到期时间 " + c.NotAfter.Local().Format("2006-01-02 15:04")
		} else {
			f.Title = "证书即将过期：" + c.Name
			f.Detail = fmt.Sprintf("剩余 %s，到期时间 %s；Caddy 通常会自动续期，若长期未续期请检查域名解析与 80/443 端口", remainingText(remaining), c.NotAfter.Local().Format("2006-01-02 15:04"))
		}
		out = append(out, f)
	}
	return out
}
func remainingText(d time.Duration) string {
	if d >= 48*time.Hour {
		return fmt.Sprintf("%d 天", int(d.Hours()/24))
	}
	return fmt.Sprintf("%d 小时", int(d.Hours()))
}

// containerFindings reports containers that were running at the previous check
// and are not now, unless the panel stopped them or they exited cleanly. It
// updates st.Running and returns keys that should be dropped without a notice.
func (a *agent) containerFindings(st *alertState, now time.Time) (out []finding, dropped []string, ok bool) {
	rows, e := a.docker.list()
	if e != nil {
		return nil, nil, false
	}
	apps, _ := a.readApps()
	busy := map[string]bool{}
	for _, r := range apps {
		if r.Busy {
			busy["junpanel-"+r.ID] = true
		}
	}
	current := map[string]container{}
	for _, c := range rows {
		current[c.ID] = c
	}
	describe := func(c container) finding {
		title, detail := "容器已停止："+c.Name, c.Status
		if c.State == "restarting" {
			title = "容器反复重启：" + c.Name
		}
		if code := exitCode.FindStringSubmatch(c.Status); code != nil {
			detail = "退出码 " + code[1] + "（" + c.Status + "）"
		}
		return finding{Key: "container:" + c.ID, Title: title, Detail: detail + "，镜像 " + c.Image, Severity: "critical", Need: 1}
	}
	failed := func(c container) bool {
		switch c.State {
		case "exited":
			code := exitCode.FindStringSubmatch(c.Status)
			return code == nil || code[1] != "0"
		case "dead", "restarting":
			return true
		}
		return false
	}
	reported := map[string]bool{}
	for _, id := range st.Running {
		c, exists := current[id]
		if !exists || c.State == "running" || !failed(c) {
			continue
		}
		if busy[c.Project] || a.takeExpected(id, now) {
			continue
		}
		out = append(out, describe(c))
		reported[id] = true
	}
	for _, c := range rows {
		if (c.State == "restarting" || c.State == "dead") && !c.Oneoff && !busy[c.Project] && !reported[c.ID] {
			out = append(out, describe(c))
			reported[c.ID] = true
		}
	}
	for key := range st.Active {
		id, isContainer := strings.CutPrefix(key, "container:")
		if !isContainer || reported[id] {
			continue
		}
		c, exists := current[id]
		switch {
		case !exists:
			dropped = append(dropped, key)
		case c.State == "running":
		case failed(c) && !busy[c.Project]:
			out = append(out, describe(c))
		case busy[c.Project]:
			dropped = append(dropped, key)
		}
	}
	st.Running = st.Running[:0]
	for _, c := range rows {
		if c.State == "running" && !c.Oneoff {
			st.Running = append(st.Running, c.ID)
		}
	}
	sort.Strings(st.Running)
	return out, dropped, true
}
func (a *agent) serviceFindings() []finding {
	var out []finding
	if _, e := a.docker.list(); e != nil {
		out = append(out, finding{Key: "service:docker", Title: "Docker 无法访问", Detail: e.Error(), Severity: "critical", Need: dockerFailPolls})
	}
	state, _ := a.runner(3*time.Second, "/usr/bin/systemctl", "is-active", "caddy.service")
	if state, _, _ = strings.Cut(strings.TrimSpace(state), "\n"); state != "active" {
		if !unitState.MatchString(state) {
			state = "未知"
		}
		out = append(out, finding{Key: "service:caddy", Title: "Caddy 未运行", Detail: "caddy.service 状态：" + state + "，网站可能无法访问", Severity: "critical", Need: serviceFailPolls})
	}
	return out
}

var ruleOf = map[string]func(alertConfig) bool{
	"disk:":      func(c alertConfig) bool { return c.DiskOn },
	"cert:":      func(c alertConfig) bool { return c.CertOn },
	"container:": func(c alertConfig) bool { return c.ContainerOn },
	"service:":   func(c alertConfig) bool { return c.ServiceOn },
	"site:":      func(c alertConfig) bool { return c.SiteOn },
}

// alertTick runs one check, updates the active alerts and sends notifications.
func (a *agent) alertTick(now time.Time) (*alertState, error) {
	a.alertRun.Lock()
	defer a.alertRun.Unlock()
	a.alertMu.Lock()
	cfg, e := a.readAlertConfig()
	var st *alertState
	if e == nil {
		st, e = a.readAlertState()
	}
	a.alertMu.Unlock()
	if e != nil {
		return nil, e
	}
	var findings []finding
	var dropped []string
	if !cfg.ContainerOn {
		st.Running = nil
	}
	if cfg.DiskOn {
		findings = append(findings, a.diskFindings(cfg, st)...)
	}
	if cfg.CertOn {
		findings = append(findings, a.certFindings(cfg, now)...)
	}
	if cfg.ContainerOn {
		f, d, ok := a.containerFindings(st, now)
		if ok {
			findings, dropped = append(findings, f...), d
		} else {
			for key := range st.Active {
				if strings.HasPrefix(key, "container:") {
					findings = append(findings, finding{Key: key, Title: st.Active[key].Title, Detail: st.Active[key].Detail, Severity: st.Active[key].Severity, Need: 1})
				}
			}
		}
	}
	if cfg.ServiceOn {
		findings = append(findings, a.serviceFindings()...)
	}
	if cfg.SiteOn {
		findings = append(findings, a.siteFindings(st)...)
	}
	notices := a.reconcile(cfg, st, findings, dropped, now)
	a.alertMu.Lock()
	e = atomicJSON(a.alertStatePath(), st, 0600)
	a.alertMu.Unlock()
	if e != nil {
		return st, e
	}
	a.dispatch(cfg, st, notices, now)
	return st, nil
}

// reconcile turns the current findings into notifications: a new alert once it has
// been seen Need times in a row, a reminder after RepeatHours, and a recovery
// after recoverPolls clean checks.
func (a *agent) reconcile(cfg alertConfig, st *alertState, findings []finding, dropped []string, now time.Time) []notice {
	var notices []notice
	record := func(key, kind, title, detail string) {
		st.History = append([]alertEvent{{Time: now, Key: key, Kind: kind, Title: title, Detail: detail}}, st.History...)
		if len(st.History) > maxHistory {
			st.History = st.History[:maxHistory]
		}
	}
	for key := range st.Active {
		enabled := true
		for prefix, on := range ruleOf {
			if strings.HasPrefix(key, prefix) {
				enabled = on(cfg)
			}
		}
		if !enabled {
			delete(st.Active, key)
		}
	}
	for _, key := range dropped {
		delete(st.Active, key)
		delete(a.okStreak, key)
	}
	present := map[string]bool{}
	for _, f := range findings {
		present[f.Key] = true
		delete(a.okStreak, f.Key)
		a.streak[f.Key]++
		if cur := st.Active[f.Key]; cur != nil {
			cur.Title, cur.Detail, cur.Severity = f.Title, f.Detail, f.Severity
			if now.Sub(cur.LastSent) >= time.Duration(cfg.RepeatHours)*time.Hour {
				cur.LastSent = now
				notices = append(notices, notice{Kind: "reminder", Key: f.Key, Title: f.Title, Detail: f.Detail, Severity: f.Severity})
				record(f.Key, "reminder", f.Title, f.Detail)
			}
			continue
		}
		if a.streak[f.Key] < max(f.Need, 1) {
			continue
		}
		st.Active[f.Key] = &activeAlert{Key: f.Key, Title: f.Title, Detail: f.Detail, Severity: f.Severity, Since: now, LastSent: now}
		if at, ok := st.Resolved[f.Key]; ok && now.Sub(at) < flapWindow {
			record(f.Key, "triggered", f.Title, f.Detail+"（刚恢复不久又出现，已抑制这次通知）")
			continue
		}
		notices = append(notices, notice{Kind: "triggered", Key: f.Key, Title: f.Title, Detail: f.Detail, Severity: f.Severity})
		record(f.Key, "triggered", f.Title, f.Detail)
	}
	for key := range a.streak {
		if !present[key] {
			delete(a.streak, key)
		}
	}
	for key, cur := range st.Active {
		if present[key] {
			continue
		}
		if a.okStreak[key]++; a.okStreak[key] >= recoverPolls {
			delete(st.Active, key)
			delete(a.okStreak, key)
			st.Resolved[key] = now
			notices = append(notices, notice{Kind: "resolved", Key: key, Title: cur.Title, Detail: "已恢复正常", Severity: "info"})
			record(key, "resolved", cur.Title, "已恢复正常")
		}
	}
	for key, at := range st.Resolved {
		if now.Sub(at) > 24*time.Hour {
			delete(st.Resolved, key)
		}
	}
	return notices
}

type pendingBatch struct {
	notices  []notice
	tries    int
	nextTry  time.Time
	firstAt  time.Time
	explicit bool
}

// dispatch sends the new notices together, retrying a failed batch a few times.
func (a *agent) dispatch(cfg alertConfig, st *alertState, notices []notice, now time.Time) {
	a.sendMu.Lock()
	defer a.sendMu.Unlock()
	if len(notices) > 0 && cfg.Notify && cfg.URL != "" && len(a.pending) < maxPending {
		a.pending = append(a.pending, &pendingBatch{notices: notices, firstAt: now})
	}
	if !cfg.Notify || cfg.URL == "" {
		a.pending = nil
	}
	var rest []*pendingBatch
	for _, batch := range a.pending {
		if now.Before(batch.nextTry) {
			rest = append(rest, batch)
			continue
		}
		e := a.send(cfg, batch.notices, now)
		result := &deliveryResult{At: now, OK: e == nil, Message: "已发送"}
		switch {
		case e == nil:
		case batch.tries+1 >= deliveryTries:
			result.Message = "发送失败，已放弃：" + e.Error()
		default:
			batch.tries++
			batch.nextTry = now.Add(deliveryRetry)
			result.Message = fmt.Sprintf("发送失败（%s），%d 分钟后重试", e.Error(), int(deliveryRetry.Minutes()))
			rest = append(rest, batch)
		}
		if e == nil {
			log.Printf("alert_notify result=sent notices=%d", len(batch.notices))
		} else {
			log.Printf("alert_notify result=failed")
		}
		st.Delivery = result
		a.alertMu.Lock()
		atomicJSON(a.alertStatePath(), st, 0600)
		a.alertMu.Unlock()
	}
	a.pending = rest
}

func (a *agent) startAlerts(stop <-chan struct{}) {
	go func() {
		select {
		case <-stop:
			return
		case <-time.After(alertStart):
		}
		tick := time.NewTicker(alertInterval)
		defer tick.Stop()
		for {
			if _, e := a.alertTick(a.now()); e != nil {
				log.Printf("alert_check result=failed")
			}
			select {
			case <-stop:
				return
			case <-tick.C:
			}
		}
	}()
}

type alertView struct {
	Format      string `json:"format"`
	URLSet      bool   `json:"url_set"`
	URLHost     string `json:"url_host"`
	Notify      bool   `json:"notify"`
	DiskOn      bool   `json:"disk_on"`
	DiskPercent int    `json:"disk_percent"`
	CertOn      bool   `json:"cert_on"`
	CertDays    int    `json:"cert_days"`
	ContainerOn bool   `json:"container_on"`
	ServiceOn   bool   `json:"service_on"`
	SiteOn      bool   `json:"site_on"`
	RepeatHours int    `json:"repeat_hours"`
}

// view never includes the webhook address: it often embeds a secret key.
func (c alertConfig) view() alertView {
	v := alertView{Format: c.Format, URLSet: c.URL != "", Notify: c.Notify, DiskOn: c.DiskOn, DiskPercent: c.DiskPercent, CertOn: c.CertOn, CertDays: c.CertDays, ContainerOn: c.ContainerOn, ServiceOn: c.ServiceOn, SiteOn: c.SiteOn, RepeatHours: c.RepeatHours}
	if u, e := url.Parse(c.URL); e == nil {
		v.URLHost = u.Host
	}
	return v
}

func (a *agent) alertList(w http.ResponseWriter, r *http.Request) error {
	a.alertMu.Lock()
	cfg, e := a.readAlertConfig()
	var st *alertState
	if e == nil {
		st, e = a.readAlertState()
	}
	a.alertMu.Unlock()
	if e != nil {
		return e
	}
	active := make([]*activeAlert, 0, len(st.Active))
	for _, v := range st.Active {
		active = append(active, v)
	}
	sort.Slice(active, func(i, j int) bool { return active[i].Since.After(active[j].Since) })
	history := st.History
	if history == nil {
		history = []alertEvent{}
	}
	formats := map[string]string{}
	for k, v := range alertFormats {
		formats[k] = v
	}
	send(w, 200, map[string]any{"config": cfg.view(), "active": active, "history": history, "delivery": st.Delivery, "formats": formats})
	return nil
}
func (a *agent) alertSave(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		alertView
		URL      string `json:"url"`
		ClearURL bool   `json:"clear_url"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	a.alertMu.Lock()
	defer a.alertMu.Unlock()
	cfg, e := a.readAlertConfig()
	if e != nil {
		return e
	}
	if _, ok := alertFormats[req.Format]; !ok {
		return bad(400, "通知格式无效")
	}
	if req.DiskPercent < 50 || req.DiskPercent > 99 {
		return bad(400, "磁盘阈值需为 50–99%")
	}
	if req.CertDays < 1 || req.CertDays > 60 {
		return bad(400, "证书提前天数需为 1–60")
	}
	if req.RepeatHours < 1 || req.RepeatHours > 168 {
		return bad(400, "重复提醒间隔需为 1–168 小时")
	}
	switch {
	case req.ClearURL:
		cfg.URL = ""
	case strings.TrimSpace(req.URL) != "":
		raw := strings.TrimSpace(req.URL)
		if _, e = validWebhook(raw); e != nil {
			return e
		}
		cfg.URL = raw
	}
	if req.Notify && cfg.URL == "" {
		return bad(400, "开启通知前请先设置 Webhook 地址")
	}
	cfg.Format, cfg.Notify = req.Format, req.Notify
	cfg.DiskOn, cfg.DiskPercent, cfg.CertOn, cfg.CertDays = req.DiskOn, req.DiskPercent, req.CertOn, req.CertDays
	cfg.ContainerOn, cfg.ServiceOn, cfg.SiteOn, cfg.RepeatHours = req.ContainerOn, req.ServiceOn, req.SiteOn, req.RepeatHours
	if e = atomicJSON(a.alertConfigPath(), cfg, 0600); e != nil {
		return e
	}
	send(w, 200, cfg.view())
	return nil
}
func (a *agent) alertTest(w http.ResponseWriter, r *http.Request) error {
	a.alertMu.Lock()
	cfg, e := a.readAlertConfig()
	a.alertMu.Unlock()
	if e != nil {
		return e
	}
	now := a.now()
	if e = a.send(cfg, []notice{{Kind: "test", Key: "test", Title: "这是一条测试通知", Detail: "收到说明告警通知配置正确", Severity: "info"}}, now); e != nil {
		return e
	}
	send(w, 200, map[string]any{"ok": true})
	return nil
}
func (a *agent) alertCheck(w http.ResponseWriter, r *http.Request) error {
	if _, e := a.alertTick(a.now()); e != nil {
		return e
	}
	return a.alertList(w, r)
}
func (a *agent) certHandler(w http.ResponseWriter, r *http.Request) error {
	now := a.now()
	certs, e := a.certificates(now, true)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	cfg, _ := a.readAlertConfig()
	type row struct {
		certInfo
		DaysLeft int  `json:"days_left"`
		Warn     bool `json:"warn"`
	}
	out := make([]row, 0, len(certs))
	for _, c := range certs {
		out = append(out, row{c, int(c.NotAfter.Sub(now).Hours() / 24), c.NotAfter.Sub(now) <= warnWindow(c, cfg.CertDays)})
	}
	send(w, 200, map[string]any{"certificates": out, "found": e == nil})
	return nil
}
func (a *agent) activeAlertCount() int {
	a.alertMu.Lock()
	defer a.alertMu.Unlock()
	st, e := a.readAlertState()
	if e != nil {
		return 0
	}
	return len(st.Active)
}
