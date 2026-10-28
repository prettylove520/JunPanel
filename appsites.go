package main

import (
	"fmt"
	"io"
	"net/url"
	"strconv"
	"time"
)

// A site whose upstream is an installed app's local port stops working when the app is uninstalled:
// Caddy keeps answering the domain and every request becomes a 502. So uninstalling pauses those sites,
// remembering which app paused them, and restoring the app switches exactly those sites back on.

// siteTargetsPort reports whether a proxy site forwards to a port on this machine.
func siteTargetsPort(s site, port int) bool {
	if s.Kind != "proxy" || port <= 0 {
		return false
	}
	u, e := url.Parse(s.Upstream)
	if e != nil {
		return false
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
	default:
		return false
	}
	p, e := strconv.Atoi(u.Port())
	return e == nil && p == port
}

// pauseAppSites switches off the enabled sites that forward to the app; it returns their domains.
// The caller holds a.mu.
func (a *agent) pauseAppSites(app appRecord, out io.Writer) {
	rows, e := a.readSites()
	if e != nil {
		fmt.Fprintf(out, "读取网站失败，未处理绑定的域名：%v\n", e)
		return
	}
	paused := []string{}
	for i := range rows {
		if rows[i].Enabled && siteTargetsPort(rows[i], app.Port) {
			rows[i].Enabled, rows[i].PausedByApp = false, app.ID
			paused = append(paused, rows[i].Domains...)
		}
	}
	if len(paused) == 0 {
		return
	}
	if e = a.applySites(rows); e != nil {
		fmt.Fprintf(out, "停用绑定的域名失败，请到“网站”页手动停用：%v\n", e)
		return
	}
	fmt.Fprintf(out, "已停用指向此应用的网站：%v；恢复部署后会自动重新启用。\n", paused)
}

// resumeAppSites switches back on the sites this app paused. The caller holds a.mu.
func (a *agent) resumeAppSites(app appRecord, out io.Writer) {
	rows, e := a.readSites()
	if e != nil {
		fmt.Fprintf(out, "读取网站失败，请到“网站”页手动启用：%v\n", e)
		return
	}
	resumed := []string{}
	for i := range rows {
		if rows[i].PausedByApp == app.ID {
			rows[i].Enabled, rows[i].PausedByApp = true, ""
			resumed = append(resumed, rows[i].Domains...)
		}
	}
	if len(resumed) == 0 {
		return
	}
	if e = a.applySites(rows); e != nil {
		fmt.Fprintf(out, "重新启用网站失败，请到“网站”页手动启用：%v\n", e)
		return
	}
	fmt.Fprintf(out, "已重新启用网站：%v\n", resumed)
}

// dropAppSchedules removes the scheduled backups and restarts aimed at an app that no longer exists.
func (a *agent) dropAppSchedules(id string, out io.Writer) {
	a.schedMu.Lock()
	defer a.schedMu.Unlock()
	rows, e := a.readSchedules()
	if e != nil {
		return
	}
	kept, dropped := rows[:0], 0
	for _, s := range rows {
		if s.Target == id && s.Type != "site" {
			dropped++
			continue
		}
		kept = append(kept, s)
	}
	if dropped == 0 {
		return
	}
	if e = a.writeSchedules(kept); e != nil {
		fmt.Fprintf(out, "清理该应用的计划任务失败，请到“计划任务”页手动删除：%v\n", e)
		return
	}
	fmt.Fprintf(out, "已删除 %d 个针对此应用的计划任务。\n", dropped)
}

// enabledDomains is the set of domains the given sites serve.
func enabledDomains(rows []site) map[string]bool {
	out := map[string]bool{}
	for _, s := range rows {
		if s.Enabled {
			for _, d := range s.Domains {
				out[d] = true
			}
		}
	}
	return out
}

// siteListChanged runs after the sites were applied. The cached Caddy host list is dropped so the checks
// stop probing a domain that is gone at once, and the alerts for domains that are no longer served are
// cleared instead of waiting out the cache and the recovery polls. It runs in the background because the
// caller holds a.mu and the alert check takes it too.
func (a *agent) siteListChanged(before, after []site) {
	was, now := enabledDomains(before), enabledDomains(after)
	gone := []string{}
	for d := range was {
		if !now[d] {
			gone = append(gone, d)
		}
	}
	a.certMu.Lock()
	a.certAt = time.Time{}
	a.certMu.Unlock()
	if len(gone) > 0 {
		go a.dropSiteAlerts(gone)
	}
}

// dropSiteAlerts removes the active alerts of the given domains.
func (a *agent) dropSiteAlerts(domains []string) {
	a.alertRun.Lock()
	defer a.alertRun.Unlock()
	a.alertMu.Lock()
	defer a.alertMu.Unlock()
	st, e := a.readAlertState()
	if e != nil {
		return
	}
	changed := false
	for _, d := range domains {
		key := "site:" + d
		if _, ok := st.Active[key]; ok {
			delete(st.Active, key)
			changed = true
		}
		delete(a.streak, key)
		delete(a.okStreak, key)
	}
	if changed {
		atomicJSON(a.alertStatePath(), st, 0600)
	}
}
