package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	updateRepo       = "prettylove520/JunPanel"
	updateCheckEvery = 6 * time.Hour
	updateFirstCheck = 2 * time.Minute
	updateManualGap  = 20 * time.Second
	maxScriptBytes   = 1 << 20
	maxNotesBytes    = 256 << 10
	maxDownload      = 80 << 20
	maxUnpacked      = 400 << 20
	upgradeTimeout   = 15 * time.Minute
	notesLimit       = 6000
)

var (
	pinVersion  = regexp.MustCompile(`(?m)^[ \t]*local version=([0-9]+\.[0-9]+\.[0-9]+)[ \t]*\r?$`)
	pinExpected = regexp.MustCompile(`(?m)^[ \t]*local expected=([0-9a-f]{64})[ \t]*\r?$`)
	plainSemver = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

// parsePin reads the release the one-click installer is pinned to. That pin is only
// merged after a release has been published and checked, so it names the newest
// version that is safe to install, with the hash its package must have.
func parsePin(script string) (version, expected string, err error) {
	v, x := pinVersion.FindAllStringSubmatch(script, -1), pinExpected.FindAllStringSubmatch(script, -1)
	if len(v) != 1 || len(x) != 1 {
		return "", "", bad(502, "无法从安装脚本读取版本信息")
	}
	return v[0][1], x[0][1], nil
}

// compareVersions orders two X.Y.Z strings; both must already be validated.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

type upgradeInfo struct {
	Version   string    `json:"version"`
	Expected  string    `json:"expected"`
	Notes     string    `json:"notes,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}
type upgradeRun struct {
	State    string    `json:"state"`
	Target   string    `json:"target"`
	Message  string    `json:"message,omitempty"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitempty"`
}
type upgradeState struct {
	NoAutoCheck bool         `json:"no_auto_check"`
	Latest      *upgradeInfo `json:"latest,omitempty"`
	CheckError  string       `json:"check_error,omitempty"`
	CheckedAt   time.Time    `json:"checked_at,omitempty"`
	Run         *upgradeRun  `json:"run,omitempty"`
}

func (a *agent) upgradeDir() string       { return filepath.Join(a.cfg.State, "upgrade") }
func (a *agent) upgradeStatePath() string { return filepath.Join(a.cfg.State, "upgrade.json") }
func (a *agent) readUpgrade() (*upgradeState, error) {
	st := &upgradeState{}
	e := loadJSON(a.upgradeStatePath(), st)
	if os.IsNotExist(e) {
		e = nil
	}
	return st, e
}
func (a *agent) writeUpgrade(st *upgradeState) error {
	return atomicJSON(a.upgradeStatePath(), st, 0600)
}

func upgradeClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("refusing to leave https")
		}
		return nil
	}}
}

// fetch downloads a small text resource, refusing anything over limit.
func (a *agent) fetch(url string, limit int64) ([]byte, error) {
	req, e := http.NewRequest("GET", url, nil)
	if e != nil {
		return nil, bad(502, "更新地址无效")
	}
	req.Header.Set("User-Agent", "JunPanel/"+version)
	resp, e := a.upHTTP.Do(req)
	if e != nil {
		return nil, bad(502, "无法连接更新服务器："+hostError(e))
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, bad(502, fmt.Sprintf("更新服务器返回 HTTP %d", resp.StatusCode))
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if e != nil {
		return nil, bad(502, "读取更新信息失败")
	}
	if int64(len(raw)) > limit {
		return nil, bad(502, "更新信息过大，已拒绝")
	}
	return raw, nil
}

func (a *agent) releaseURL(v string) string {
	return a.upPage + "/releases/tag/v" + v
}

// checkUpdate asks the update server for the pinned version and stores the answer.
func (a *agent) checkUpdate() (*upgradeState, error) {
	a.upCheck.Lock()
	defer a.upCheck.Unlock()
	now := a.now()
	a.upMu.Lock()
	st, e := a.readUpgrade()
	a.upMu.Unlock()
	if e != nil {
		return nil, e
	}
	// Repeated clicks reuse the answer from a few seconds ago instead of hitting the server.
	if !st.CheckedAt.IsZero() && now.Sub(st.CheckedAt) < updateManualGap {
		return st, nil
	}
	info, checkErr := a.discoverRelease(now)
	a.upMu.Lock()
	defer a.upMu.Unlock()
	if st, e = a.readUpgrade(); e != nil {
		return nil, e
	}
	st.CheckedAt = now
	if checkErr != nil {
		st.CheckError = checkErr.Error()
	} else {
		st.CheckError, st.Latest = "", info
	}
	if e = a.writeUpgrade(st); e != nil {
		return nil, e
	}
	return st, nil
}
func (a *agent) discoverRelease(now time.Time) (*upgradeInfo, error) {
	script, e := a.fetch(a.upRaw+"/quick-install.sh", maxScriptBytes)
	if e != nil {
		return nil, e
	}
	v, expected, e := parsePin(string(script))
	if e != nil {
		return nil, e
	}
	info := &upgradeInfo{Version: v, Expected: expected, CheckedAt: now}
	if compareVersions(v, version) > 0 {
		// Release notes are a courtesy; a failure to fetch them does not matter.
		if raw, e := a.fetch(a.upAPI+"/releases/tags/v"+v, maxNotesBytes); e == nil {
			var release struct {
				Body string `json:"body"`
			}
			if json.Unmarshal(raw, &release) == nil {
				info.Notes = truncateRunes(strings.TrimSpace(cleanText(release.Body)), notesLimit)
			}
		}
	}
	return info, nil
}

func (a *agent) updateAvailable() string {
	a.upMu.Lock()
	defer a.upMu.Unlock()
	st, e := a.readUpgrade()
	if e != nil || st.Latest == nil || compareVersions(st.Latest.Version, version) <= 0 {
		return ""
	}
	return st.Latest.Version
}

// upgradeBlockers lists what must finish before the panel may restart itself.
func (a *agent) upgradeBlockers(now time.Time) []string {
	var out []string
	for _, j := range a.jobs.list() {
		if j.State == "running" {
			out = append(out, "有后台任务正在运行："+j.Title)
		}
	}
	apps, _ := a.readApps()
	for _, r := range apps {
		if r.Busy {
			out = append(out, "应用正在操作："+r.Name)
		}
	}
	a.schedMu.Lock()
	for id := range a.schedRunning {
		if a.scheduleBusy(id, now) {
			out = append(out, "有计划任务正在运行")
			break
		}
	}
	a.schedMu.Unlock()
	return out
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// runScript is the whole of what the transient unit executes. It only contains
// paths the agent chose itself, never anything taken from a request.
func runScript(logFile, exitFile, stage string) string {
	return "#!/bin/bash\n" +
		"set -u\n" +
		"exec >> " + shellQuote(logFile) + " 2>&1\n" +
		"echo \"[JunPanel upgrade] started $(date -u +%FT%TZ)\"\n" +
		"bash " + shellQuote(filepath.Join(stage, "junpanel", "install.sh")) + "\n" +
		"code=$?\n" +
		"echo \"[JunPanel upgrade] installer exit code $code\"\n" +
		"echo \"$code\" > " + shellQuote(exitFile) + "\n" +
		"rm -rf " + shellQuote(stage) + "\n" +
		"exit \"$code\"\n"
}

func (a *agent) setRun(run upgradeRun) {
	a.upMu.Lock()
	defer a.upMu.Unlock()
	if st, e := a.readUpgrade(); e == nil {
		st.Run = &run
		a.writeUpgrade(st)
	}
}

// runUpgrade downloads, verifies and unpacks the release, then starts its installer
// outside the agent's own control group so that stopping the agent cannot kill it.
func (a *agent) runUpgrade(ctx context.Context, out io.Writer, info upgradeInfo) error {
	target, started := info.Version, a.now()
	fail := func(message string, cause error) error {
		a.setRun(upgradeRun{State: "failed", Target: target, Message: message, Started: started, Finished: a.now()})
		if cause != nil {
			log.Printf("upgrade result=failed")
		}
		return bad(502, message)
	}
	if !plainSemver.MatchString(target) || len(info.Expected) != 64 {
		return fail("版本信息无效，已取消", nil)
	}
	dir := a.upgradeDir()
	if e := os.MkdirAll(dir, 0700); e != nil {
		return fail("无法创建升级目录", e)
	}
	stage := filepath.Join(dir, "stage-"+target)
	pkg := filepath.Join(dir, "package-"+target+".tar.gz")
	exitFile := filepath.Join(dir, "exit-code")
	logFile := filepath.Join(dir, "upgrade.log")
	script := filepath.Join(dir, "run-"+target+".sh")
	cleanup := func() { os.RemoveAll(stage); os.Remove(pkg); os.Remove(script) }
	cleanup()
	os.Remove(exitFile)
	os.Remove(logFile)
	defer func() {
		if _, e := os.Stat(script); e != nil {
			cleanup()
		}
		os.Remove(pkg)
	}()

	url := fmt.Sprintf("%s/v%s/JunPanel-%s-linux.tar.gz", a.upDL, target, target)
	fmt.Fprintf(out, "正在下载 v%s 安装包…\n", target)
	req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return fail("下载地址无效", e)
	}
	req.Header.Set("User-Agent", "JunPanel/"+version)
	resp, e := a.upHTTP.Do(req)
	if e != nil {
		return fail("下载失败："+hostError(e), e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fail(fmt.Sprintf("下载失败：HTTP %d", resp.StatusCode), nil)
	}
	f, e := os.OpenFile(pkg, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return fail("无法写入安装包", e)
	}
	sum := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, sum), io.LimitReader(resp.Body, a.upMax+1))
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e != nil {
		return fail("下载中断，请稍后重试", e)
	}
	if n > a.upMax {
		return fail("安装包超过大小上限，已拒绝", nil)
	}
	fmt.Fprintf(out, "已下载 %.1f MB，正在校验 SHA-256…\n", float64(n)/1e6)
	if hex.EncodeToString(sum.Sum(nil)) != info.Expected {
		return fail("安装包校验失败（与发布的固定值不一致），已拒绝使用", nil)
	}
	fmt.Fprintln(out, "校验通过，正在解包…")
	pf, e := os.Open(pkg)
	if e != nil {
		return fail("无法读取安装包", e)
	}
	defer pf.Close()
	gz, e := gzip.NewReader(pf)
	if e != nil {
		return fail("安装包格式无效", e)
	}
	if e = os.Mkdir(stage, 0700); e != nil {
		return fail("无法创建解包目录", e)
	}
	root, e := os.OpenRoot(stage)
	if e != nil {
		return fail("无法创建解包目录", e)
	}
	_, e = untar(ctx, tar.NewReader(io.LimitReader(gz, maxUnpacked)), root)
	root.Close()
	if e != nil {
		return fail("安装包内容不安全或已损坏，已拒绝："+e.Error(), e)
	}
	binary := filepath.Join(stage, "junpanel", "bin", "junpanel-linux-"+runtime.GOARCH)
	for _, need := range []string{filepath.Join(stage, "junpanel", "install.sh"), binary, filepath.Join(stage, "junpanel", "bin", "SHA256SUMS")} {
		if st, e := os.Lstat(need); e != nil || !st.Mode().IsRegular() {
			return fail("安装包缺少必要文件，已拒绝", nil)
		}
	}
	reported, e := a.runner(15*time.Second, binary, "version")
	if e != nil || strings.TrimSpace(reported) != "JunPanel "+target {
		return fail("安装包中的程序版本与预期不符，已拒绝", e)
	}
	fmt.Fprintln(out, "程序版本核对通过。")

	if e = os.WriteFile(script, []byte(runScript(logFile, exitFile, stage)), 0700); e != nil {
		return fail("无法写入升级脚本", e)
	}
	a.setRun(upgradeRun{State: "running", Target: target, Started: started})
	fmt.Fprintln(out, "正在启动安装器；面板和管理代理即将重启，约 1 分钟后恢复。")
	if _, e = a.runner(20*time.Second, a.upSystemd, "--unit", "junpanel-upgrade", "--description", "JunPanel upgrade to v"+target, "--collect", "/bin/bash", script); e != nil {
		os.Remove(script)
		return fail("无法启动升级程序（需要 systemd-run），未做任何更改。请改用一键安装命令升级", e)
	}
	return nil
}

func (a *agent) upgradeSupported() (bool, string) {
	if runtime.GOOS != "linux" {
		return false, "仅支持 Linux"
	}
	if st, e := os.Stat(a.upSystemd); e != nil || st.IsDir() {
		return false, "找不到 systemd-run，无法在面板内升级，请使用一键安装命令"
	}
	return true, ""
}

// refreshRun settles a run left "running": the installer reports its exit code in a
// file, and a new agent (the target version) means it worked.
func (a *agent) refreshRun(now time.Time) {
	a.upMu.Lock()
	defer a.upMu.Unlock()
	st, e := a.readUpgrade()
	if e != nil || st.Run == nil || st.Run.State != "running" {
		return
	}
	run := st.Run
	finish := func(state, message string) {
		run.State, run.Message, run.Finished = state, message, now
		a.writeUpgrade(st)
	}
	raw, readErr := os.ReadFile(filepath.Join(a.upgradeDir(), "exit-code"))
	code := strings.TrimSpace(string(raw))
	switch {
	case version == run.Target:
		finish("done", "已升级到 v"+run.Target)
	case readErr == nil && code != "0":
		finish("failed", "安装器失败（退出码 "+truncate(code, 10)+"），已回滚到原版本，详见升级日志")
	case readErr == nil:
		finish("failed", "安装器已结束，但版本仍是 v"+version+"，请查看升级日志")
	case now.Sub(run.Started) > upgradeTimeout:
		finish("failed", "升级超时或被中断，当前版本 v"+version+"，请查看升级日志")
	}
}

func (a *agent) upgradeLog() string {
	f, e := os.Open(filepath.Join(a.upgradeDir(), "upgrade.log"))
	if e != nil {
		return ""
	}
	defer f.Close()
	st, _ := f.Stat()
	const tail = 6000
	if st != nil && st.Size() > tail {
		f.Seek(st.Size()-tail, io.SeekStart)
	}
	raw, _ := io.ReadAll(io.LimitReader(f, tail))
	return cleanText(string(raw))
}

func (a *agent) startUpdateChecks(stop <-chan struct{}) {
	go func() {
		select {
		case <-stop:
			return
		case <-time.After(updateFirstCheck):
		}
		tick := time.NewTicker(updateCheckEvery)
		defer tick.Stop()
		for {
			a.upMu.Lock()
			st, e := a.readUpgrade()
			a.upMu.Unlock()
			if e == nil && !st.NoAutoCheck {
				if _, e := a.checkUpdate(); e != nil {
					log.Printf("update_check result=failed")
				}
			}
			a.refreshRun(a.now())
			select {
			case <-stop:
				return
			case <-tick.C:
			}
		}
	}()
}

func (a *agent) upgradeStatus(w http.ResponseWriter, r *http.Request) error {
	a.refreshRun(a.now())
	a.upMu.Lock()
	st, e := a.readUpgrade()
	a.upMu.Unlock()
	if e != nil {
		return e
	}
	supported, reason := a.upgradeSupported()
	available := st.Latest != nil && compareVersions(st.Latest.Version, version) > 0
	blockers := []string{}
	if available {
		blockers = append(blockers, a.upgradeBlockers(a.now())...)
	}
	running := st.Run != nil && st.Run.State == "running"
	send(w, 200, map[string]any{
		"current": version, "latest": st.Latest, "available": available, "checked_at": st.CheckedAt, "check_error": st.CheckError,
		"auto_check": !st.NoAutoCheck, "supported": supported, "unsupported_reason": reason, "blockers": blockers,
		"can_upgrade": available && supported && len(blockers) == 0 && !running, "run": st.Run, "log": a.upgradeLog(), "release_url": a.upPage + "/releases",
		"notes_url": func() string {
			if st.Latest != nil {
				return a.releaseURL(st.Latest.Version)
			}
			return ""
		}(),
	})
	return nil
}
func (a *agent) upgradeCheck(w http.ResponseWriter, r *http.Request) error {
	if _, e := a.checkUpdate(); e != nil {
		return e
	}
	return a.upgradeStatus(w, r)
}
func (a *agent) upgradeConfig(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		AutoCheck bool `json:"auto_check"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	a.upMu.Lock()
	st, e := a.readUpgrade()
	if e == nil {
		st.NoAutoCheck = !req.AutoCheck
		e = a.writeUpgrade(st)
	}
	a.upMu.Unlock()
	if e != nil {
		return e
	}
	send(w, 200, map[string]any{"ok": true, "auto_check": req.AutoCheck})
	return nil
}

// upgradeStart installs the version the agent itself found and verified; the request
// carries no version or address. The web service has already re-authenticated the admin.
func (a *agent) upgradeStart(w http.ResponseWriter, r *http.Request) error {
	if supported, reason := a.upgradeSupported(); !supported {
		return bad(409, reason)
	}
	now := a.now()
	a.refreshRun(now)
	a.upMu.Lock()
	st, e := a.readUpgrade()
	if e != nil {
		a.upMu.Unlock()
		return e
	}
	if st.Run != nil && st.Run.State == "running" {
		a.upMu.Unlock()
		return bad(409, "已有升级正在进行")
	}
	if st.Latest == nil || compareVersions(st.Latest.Version, version) <= 0 {
		a.upMu.Unlock()
		return bad(400, "没有可升级的新版本")
	}
	if blockers := a.upgradeBlockers(now); len(blockers) > 0 {
		a.upMu.Unlock()
		return bad(409, "请等这些任务结束后再升级："+strings.Join(blockers, "；"))
	}
	info := *st.Latest
	st.Run = &upgradeRun{State: "running", Target: info.Version, Started: now, Message: "正在下载并校验"}
	e = a.writeUpgrade(st)
	a.upMu.Unlock()
	if e != nil {
		return e
	}
	j, e := a.jobs.startWithin(upgradeTimeout, "升级 · v"+info.Version, func(ctx context.Context, out *limitedBuffer) error {
		return a.runUpgrade(ctx, out, info)
	})
	if e != nil {
		a.setRun(upgradeRun{State: "failed", Target: info.Version, Message: e.Error(), Started: now, Finished: a.now()})
		return e
	}
	send(w, 202, j)
	return nil
}
