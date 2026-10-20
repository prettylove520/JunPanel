package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxSchedules  = 50
	retryDelay    = 10 * time.Minute
	maxTries      = 3
	staleRun      = backupTimeout + 10*time.Minute
	defaultKeep   = 7
	maxKeep       = 60
	scheduleStart = 20 * time.Second
	scheduleTick  = 30 * time.Second
)

var (
	scheduleID = regexp.MustCompile(`^sc_[a-f0-9]{16}$`)
	clockTime  = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)
)

type scheduleRun struct {
	At      time.Time `json:"at"`
	Status  string    `json:"status"`
	Message string    `json:"message,omitempty"`
	Backup  string    `json:"backup,omitempty"`
}

// schedule runs one built-in action on a timetable in the server's local time.
// There is deliberately no action that runs an arbitrary command.
type schedule struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Action   string       `json:"action"`
	Type     string       `json:"type,omitempty"`
	Target   string       `json:"target"`
	Database string       `json:"database,omitempty"`
	Keep     int          `json:"keep,omitempty"`
	Every    string       `json:"every"`
	At       string       `json:"at,omitempty"`
	Weekday  int          `json:"weekday"`
	Hours    int          `json:"hours,omitempty"`
	Enabled  bool         `json:"enabled"`
	Created  time.Time    `json:"created"`
	Next     time.Time    `json:"next"`
	Last     *scheduleRun `json:"last,omitempty"`
}

func (a *agent) schedulesPath() string { return filepath.Join(a.cfg.State, "schedules.json") }
func (a *agent) readSchedules() ([]schedule, error) {
	rows := []schedule{}
	e := loadJSON(a.schedulesPath(), &rows)
	if os.IsNotExist(e) {
		e = nil
	}
	return rows, e
}
func (a *agent) writeSchedules(rows []schedule) error {
	return atomicJSON(a.schedulesPath(), rows, 0600)
}

// nextRun is the first run strictly after the given time.
func nextRun(s schedule, after time.Time) time.Time {
	if s.Every == "interval" {
		return after.Add(time.Duration(s.Hours) * time.Hour)
	}
	var hour, minute int
	fmt.Sscanf(s.At, "%d:%d", &hour, &minute)
	year, month, day := after.Date()
	var t time.Time
	for i := 0; i <= 8; i++ {
		t = time.Date(year, month, day+i, hour, minute, 0, 0, after.Location())
		if t.After(after) && (s.Every == "daily" || int(t.Weekday()) == s.Weekday) {
			return t
		}
	}
	return t
}

func (a *agent) validSchedule(s *schedule) error {
	s.Name = strings.TrimSpace(s.Name)
	if n := len([]rune(s.Name)); n < 1 || n > 60 {
		return bad(400, "名称需为 1–60 字符")
	}
	switch s.Action {
	case "backup":
		switch s.Type {
		case "site":
			if !siteID.MatchString(s.Target) {
				return bad(400, "网站标识无效")
			}
			a.mu.Lock()
			_, e := a.staticSite(s.Target)
			a.mu.Unlock()
			if e != nil {
				return e
			}
			s.Database = ""
		case "app":
			if e := requireName(s.Target); e != nil {
				return e
			}
			rec, e := a.appRecordByID(s.Target)
			if e != nil {
				return e
			}
			if len(appData[rec.Template]) == 0 {
				return bad(400, "此应用类型暂不支持备份")
			}
			s.Database = ""
		case "database":
			if e := requireName(s.Target); e != nil {
				return e
			}
			record, kind, e := a.databaseRecord(s.Target)
			if e != nil {
				return e
			}
			if _, _, e = dumpArgs(record, kind, s.Database); e != nil {
				return e
			}
		default:
			return bad(400, "备份类型无效")
		}
		if s.Keep == 0 {
			s.Keep = defaultKeep
		}
		if s.Keep < 1 || s.Keep > maxKeep {
			return bad(400, fmt.Sprintf("保留份数需为 1–%d", maxKeep))
		}
	case "restart":
		if e := requireName(s.Target); e != nil {
			return e
		}
		if _, e := a.appRecordByID(s.Target); e != nil {
			return e
		}
		s.Type, s.Database, s.Keep = "", "", 0
	default:
		return bad(400, "任务类型无效")
	}
	switch s.Every {
	case "daily", "weekly":
		if !clockTime.MatchString(s.At) {
			return bad(400, "时间格式应为 HH:MM")
		}
		if s.Every == "weekly" && (s.Weekday < 0 || s.Weekday > 6) {
			return bad(400, "星期需为 0（周日）–6（周六）")
		}
		s.Hours = 0
		if s.Every == "daily" {
			s.Weekday = 0
		}
	case "interval":
		if s.Hours < 1 || s.Hours > 168 {
			return bad(400, "间隔需为 1–168 小时")
		}
		s.At, s.Weekday = "", 0
	default:
		return bad(400, "频率无效")
	}
	return nil
}
func (a *agent) appRecordByID(id string) (appRecord, error) {
	rows, e := a.readApps()
	if e != nil {
		return appRecord{}, e
	}
	for _, r := range rows {
		if r.ID == id {
			if r.Removed {
				return appRecord{}, bad(409, "应用已卸载，请先恢复部署")
			}
			return r, nil
		}
	}
	return appRecord{}, bad(404, "应用不存在")
}

// pruneBackups removes a schedule's oldest backups beyond keep. Manual and
// pre-restore backups carry no schedule and are never touched.
func (a *agent) pruneBackups(scheduleID string, keep int) int {
	if keep < 1 {
		return 0
	}
	entries, e := os.ReadDir(a.backupDir())
	if e != nil {
		return 0
	}
	var mine []backupMeta
	for _, entry := range entries {
		if id, ok := strings.CutSuffix(entry.Name(), ".json"); ok && backupID.MatchString(id) {
			if meta, e := a.readBackup(id); e == nil && meta.Schedule == scheduleID {
				mine = append(mine, meta)
			}
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].Created.After(mine[j].Created) })
	removed := 0
	for i := keep; i < len(mine); i++ {
		data := filepath.Join(a.backupDir(), mine[i].File)
		if e := os.Remove(data); e != nil && !os.IsNotExist(e) {
			continue
		}
		if os.Remove(filepath.Join(a.backupDir(), mine[i].ID+".json")) == nil {
			removed++
		}
	}
	return removed
}

// scheduleBusy reports a run still in flight; very old entries are treated as stale.
func (a *agent) scheduleBusy(id string, now time.Time) bool {
	started, ok := a.schedRunning[id]
	return ok && now.Sub(started) < staleRun
}

// tick starts every enabled schedule that is due. Each runs at most once per
// tick, so a long outage causes one catch-up run, not one per missed slot.
func (a *agent) tick(now time.Time) {
	a.schedMu.Lock()
	rows, e := a.readSchedules()
	var due []string
	if e == nil {
		for _, s := range rows {
			if s.Enabled && !s.Next.IsZero() && !s.Next.After(now) && !a.scheduleBusy(s.ID, now) {
				due = append(due, s.ID)
			}
		}
	}
	a.schedMu.Unlock()
	if e != nil {
		log.Printf("schedule_tick result=failed")
		return
	}
	for _, id := range due {
		if _, e := a.fire(id, now, true); e != nil {
			log.Printf("schedule id=%s result=not_started", id)
		}
	}
}

// fire starts one schedule's action as a job. Timed runs advance Next first;
// busy conditions are retried a few times, other failures wait for the next slot.
func (a *agent) fire(id string, now time.Time, timed bool) (job, error) {
	a.schedMu.Lock()
	rows, e := a.readSchedules()
	if e != nil {
		a.schedMu.Unlock()
		return job{}, e
	}
	index := -1
	for i := range rows {
		if rows[i].ID == id {
			index = i
		}
	}
	if index < 0 {
		a.schedMu.Unlock()
		return job{}, bad(404, "计划任务不存在")
	}
	if a.scheduleBusy(id, now) {
		a.schedMu.Unlock()
		return job{}, bad(409, "该计划任务正在运行")
	}
	s := rows[index]
	a.schedRunning[id] = now
	rows[index].Last = &scheduleRun{At: now, Status: "running"}
	if timed {
		rows[index].Next = nextRun(s, now)
	}
	e = a.writeSchedules(rows)
	a.schedMu.Unlock()
	if e != nil {
		a.finishRun(id, "", e)
		return job{}, e
	}
	var j job
	switch s.Action {
	case "backup":
		tag := backupTag{Note: "定时备份 · " + s.Name, Schedule: s.ID}
		j, e = a.startBackup(backupSpec{Type: s.Type, Target: s.Target, Database: s.Database, Tag: tag}, func(meta backupMeta, err error) {
			a.finishRun(id, meta.ID, err)
		})
	case "restart":
		j, e = a.appActionJob(s.Target, "restart", func(err error) { a.finishRun(id, "", err) })
	default:
		e = bad(400, "任务类型无效")
	}
	if e != nil {
		a.notStarted(id, now, e, timed)
		return job{}, e
	}
	return j, nil
}

func describeFailure(e error) string {
	text := cleanText(e.Error())
	return truncate(text, 300)
}

// notStarted records a run that could not begin and schedules a retry when the
// cause is transient (an app or the job queue was busy).
func (a *agent) notStarted(id string, now time.Time, cause error, timed bool) {
	a.schedMu.Lock()
	defer a.schedMu.Unlock()
	delete(a.schedRunning, id)
	rows, e := a.readSchedules()
	if e != nil {
		return
	}
	for i := range rows {
		if rows[i].ID != id {
			continue
		}
		message := describeFailure(cause)
		if f, ok := cause.(*fault); ok && f.Code == 409 && timed && a.schedTries[id] < maxTries {
			a.schedTries[id]++
			rows[i].Next = now.Add(retryDelay)
			message = fmt.Sprintf("%s（%d 分钟后重试，第 %d/%d 次）", message, int(retryDelay.Minutes()), a.schedTries[id], maxTries)
		} else {
			delete(a.schedTries, id)
		}
		rows[i].Last = &scheduleRun{At: now, Status: "failed", Message: message}
		a.writeSchedules(rows)
		return
	}
}

// finishRun records a job's result and applies backup retention.
func (a *agent) finishRun(id, backup string, result error) {
	a.schedMu.Lock()
	defer a.schedMu.Unlock()
	delete(a.schedRunning, id)
	delete(a.schedTries, id)
	rows, e := a.readSchedules()
	if e != nil {
		return
	}
	for i := range rows {
		if rows[i].ID != id {
			continue
		}
		run := rows[i].Last
		if run == nil {
			run = &scheduleRun{At: a.now()}
		}
		if result != nil {
			run.Status, run.Message = "failed", describeFailure(result)
		} else {
			run.Status = "success"
			if rows[i].Action == "backup" {
				removed := a.pruneBackups(id, rows[i].Keep)
				run.Backup, run.Message = backup, fmt.Sprintf("已备份，保留最近 %d 份", rows[i].Keep)
				if removed > 0 {
					run.Message += fmt.Sprintf("（已清理 %d 份旧备份）", removed)
				}
			} else {
				run.Message = "已重启"
			}
		}
		rows[i].Last = run
		a.writeSchedules(rows)
		log.Printf("schedule id=%s result=%s", id, run.Status)
		return
	}
}

// recoverSchedules marks runs interrupted by an agent restart as failed.
func (a *agent) recoverSchedules() error {
	a.schedMu.Lock()
	defer a.schedMu.Unlock()
	rows, e := a.readSchedules()
	if e != nil {
		return e
	}
	changed := false
	for i := range rows {
		if rows[i].Last != nil && rows[i].Last.Status == "running" {
			rows[i].Last.Status, rows[i].Last.Message = "failed", "管理代理曾重启，这次运行被中断，请核对结果"
			changed = true
		}
	}
	if changed {
		return a.writeSchedules(rows)
	}
	return nil
}

func (a *agent) startScheduler(stop <-chan struct{}) {
	go func() {
		select {
		case <-stop:
			return
		case <-time.After(scheduleStart):
		}
		tick := time.NewTicker(scheduleTick)
		defer tick.Stop()
		for {
			a.tick(a.now())
			select {
			case <-stop:
				return
			case <-tick.C:
			}
		}
	}()
}

func (a *agent) scheduleList(w http.ResponseWriter, r *http.Request) error {
	a.schedMu.Lock()
	rows, e := a.readSchedules()
	a.schedMu.Unlock()
	if e != nil {
		return e
	}
	now := a.now()
	send(w, 200, map[string]any{"schedules": rows, "zone": now.Format("MST -0700"), "now": now})
	return nil
}
func (a *agent) scheduleSave(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Action   string `json:"action"`
		Type     string `json:"type"`
		Target   string `json:"target"`
		Database string `json:"database"`
		Keep     int    `json:"keep"`
		Every    string `json:"every"`
		At       string `json:"at"`
		Weekday  int    `json:"weekday"`
		Hours    int    `json:"hours"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	value := schedule{Name: req.Name, Action: req.Action, Type: req.Type, Target: req.Target, Database: req.Database, Keep: req.Keep, Every: req.Every, At: req.At, Weekday: req.Weekday, Hours: req.Hours}
	if e := a.validSchedule(&value); e != nil {
		return e
	}
	a.schedMu.Lock()
	defer a.schedMu.Unlock()
	rows, e := a.readSchedules()
	if e != nil {
		return e
	}
	now := a.now()
	if req.ID == "" {
		if len(rows) >= maxSchedules {
			return bad(409, fmt.Sprintf("最多 %d 个计划任务", maxSchedules))
		}
		value.ID = "sc_" + randomID(16)
		value.Created, value.Enabled = now, true
		value.Next = nextRun(value, now)
		rows = append(rows, value)
	} else {
		if !scheduleID.MatchString(req.ID) {
			return bad(400, "计划任务标识无效")
		}
		found := false
		for i := range rows {
			if rows[i].ID == req.ID {
				value.ID, value.Created, value.Enabled, value.Last = req.ID, rows[i].Created, rows[i].Enabled, rows[i].Last
				if value.Enabled {
					value.Next = nextRun(value, now)
				}
				rows[i], found = value, true
			}
		}
		if !found {
			return bad(404, "计划任务不存在")
		}
	}
	if e = a.writeSchedules(rows); e != nil {
		return e
	}
	send(w, 200, value)
	return nil
}
func (a *agent) scheduleToggle(w http.ResponseWriter, r *http.Request) error {
	a.schedMu.Lock()
	defer a.schedMu.Unlock()
	rows, e := a.readSchedules()
	if e != nil {
		return e
	}
	for i := range rows {
		if rows[i].ID == r.PathValue("id") {
			rows[i].Enabled = !rows[i].Enabled
			rows[i].Next = time.Time{}
			if rows[i].Enabled {
				rows[i].Next = nextRun(rows[i], a.now())
			}
			delete(a.schedTries, rows[i].ID)
			if e = a.writeSchedules(rows); e != nil {
				return e
			}
			send(w, 200, rows[i])
			return nil
		}
	}
	return bad(404, "计划任务不存在")
}
func (a *agent) scheduleRunNow(w http.ResponseWriter, r *http.Request) error {
	j, e := a.fire(r.PathValue("id"), a.now(), false)
	if e != nil {
		return e
	}
	send(w, 202, j)
	return nil
}
func (a *agent) scheduleDelete(w http.ResponseWriter, r *http.Request) error {
	a.schedMu.Lock()
	defer a.schedMu.Unlock()
	rows, e := a.readSchedules()
	if e != nil {
		return e
	}
	for i := range rows {
		if rows[i].ID == r.PathValue("id") {
			rows = append(rows[:i], rows[i+1:]...)
			if e = a.writeSchedules(rows); e != nil {
				return e
			}
			delete(a.schedTries, r.PathValue("id"))
			send(w, 200, map[string]any{"ok": true, "message": "计划任务已删除，它已创建的备份保留"})
			return nil
		}
	}
	return bad(404, "计划任务不存在")
}
