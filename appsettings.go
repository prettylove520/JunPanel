package main

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// Changing an installed app's port and environment variables from the panel. The values are
// kept in the app's record and written into its compose file, then the app is redeployed.

const (
	maskedValue    = "••••••••"
	maxEnvEntries  = 40
	maxEnvValueLen = 2000
)

var (
	envName   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]{0,63}$`)
	secretEnv = regexp.MustCompile(`(?i)(KEY|TOKEN|PASS|PASSWORD|SECRET|CREDENTIAL)`)
)

// protectedEnv are variables the panel generates for the app; changing them would lock the
// app out of its own database or administrator account.
var protectedEnv = map[string]bool{
	"POSTGRES_PASSWORD": true, "POSTGRES_USER": true, "POSTGRES_DB": true,
	"MYSQL_ROOT_PASSWORD": true, "MYSQL_DATABASE": true,
	"MARIADB_PASSWORD": true, "MARIADB_ROOT_PASSWORD": true, "MARIADB_USER": true, "MARIADB_DATABASE": true,
	"WORDPRESS_DB_HOST": true, "WORDPRESS_DB_NAME": true, "WORDPRESS_DB_USER": true, "WORDPRESS_DB_PASSWORD": true,
	"LD_SUPERUSER_NAME": true, "LD_SUPERUSER_PASSWORD": true, "LD_SERVER_HOST": true,
	"GOTIFY_DEFAULTUSER_NAME": true, "GOTIFY_DEFAULTUSER_PASS": true,
	"PASSWORD": true,
}

// appPreset is a common setting shown as a choice; an empty value means "not set".
type appPreset struct {
	Env     string         `json:"env"`
	Label   string         `json:"label"`
	Help    string         `json:"help,omitempty"`
	Options []presetOption `json:"options"`
}
type presetOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

var appPresets = map[string][]appPreset{
	"vaultwarden": {{Env: "SIGNUPS_ALLOWED", Label: "新用户注册", Help: "自己的账号注册好以后，建议关闭注册，避免别人用你的域名注册。", Options: []presetOption{{"允许（默认）", ""}, {"关闭注册", "false"}}}},
	"openwebui":   {{Env: "ENABLE_SIGNUP", Label: "新用户注册", Help: "管理员账号创建好以后，可以关闭注册。", Options: []presetOption{{"允许（默认）", ""}, {"关闭注册", "false"}}}},
	"gitea":       {{Env: "GITEA__service__DISABLE_REGISTRATION", Label: "新用户注册", Help: "个人使用的 Gitea 建议关闭注册。", Options: []presetOption{{"允许（默认）", ""}, {"关闭注册", "true"}}}},
}

func validEnv(env map[string]string) error {
	if len(env) > maxEnvEntries {
		return bad(400, fmt.Sprintf("环境变量最多 %d 个", maxEnvEntries))
	}
	for k, v := range env {
		if !envName.MatchString(k) {
			return bad(400, "变量名只能含字母、数字、下划线和点，并且不能以数字开头："+truncate(cleanText(k), 40))
		}
		if protectedEnv[k] {
			return bad(400, k+" 由面板生成并管理，不能修改")
		}
		if len(v) > maxEnvValueLen || strings.ContainsAny(v, "\x00\r\n") {
			return bad(400, k+" 的值过长或含有换行")
		}
	}
	return nil
}

func maskEnv(env map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		if secretEnv.MatchString(k) && v != "" {
			v = maskedValue
		}
		out[k] = v
	}
	return out
}

func (a *agent) appSettings(w http.ResponseWriter, r *http.Request) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rows, e := a.readApps()
	if e != nil {
		return e
	}
	for _, rec := range rows {
		if rec.ID != r.PathValue("id") {
			continue
		}
		presets := appPresets[rec.Template]
		if presets == nil {
			presets = []appPreset{}
		}
		spec := appSpecs[rec.Template]
		builtin := []string{}
		for k := range spec.Env {
			builtin = append(builtin, k)
		}
		sort.Strings(builtin)
		send(w, 200, map[string]any{"id": rec.ID, "name": rec.Name, "template": rec.Template, "port": rec.Port, "env": maskEnv(rec.Env), "presets": presets, "builtin_env": builtin, "removed": rec.Removed, "busy": rec.Busy})
		return nil
	}
	return bad(404, "应用不存在")
}

func (a *agent) appSettingsSave(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Port int               `json:"port"`
		Env  map[string]string `json:"env"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if req.Port < 1024 || req.Port > 65535 {
		return bad(400, "端口需为 1024–65535")
	}
	if e := validEnv(req.Env); e != nil {
		return e
	}
	id := r.PathValue("id")
	a.mu.Lock()
	rows, e := a.readApps()
	if e != nil {
		a.mu.Unlock()
		return e
	}
	idx := -1
	for i, rec := range rows {
		if rec.ID == id {
			idx = i
		}
	}
	if idx < 0 {
		a.mu.Unlock()
		return bad(404, "应用不存在")
	}
	old := rows[idx]
	if old.Busy {
		a.mu.Unlock()
		return bad(409, "此应用已有操作进行中")
	}
	if old.Removed {
		a.mu.Unlock()
		return bad(409, "应用已卸载，请先恢复部署")
	}
	for _, other := range rows {
		if other.ID != id && !other.Removed && other.Port == req.Port {
			a.mu.Unlock()
			return bad(409, fmt.Sprintf("端口 %d 已被应用“%s”使用", req.Port, other.Name))
		}
	}
	env := map[string]string{}
	for k, v := range req.Env {
		if v == maskedValue {
			v = old.Env[k] // unchanged secret
		}
		if v != "" {
			env[k] = v
		}
	}
	next := old
	next.Port, next.Env = req.Port, env
	if len(env) == 0 {
		next.Env = nil
	}
	rows[idx] = next
	if e = a.storeApps(rows); e != nil {
		a.mu.Unlock()
		return e
	}
	e = a.writeCompose(next)
	a.mu.Unlock()
	restore := func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if cur, err := a.readApps(); err == nil {
			for i := range cur {
				if cur[i].ID == id {
					cur[i].Port, cur[i].Env = old.Port, old.Env
				}
			}
			a.storeApps(cur)
		}
		a.writeCompose(old)
	}
	if e != nil {
		restore()
		return e
	}
	j, e := a.appActionJob(id, "deploy", nil)
	if e != nil {
		restore()
		return e
	}
	send(w, 202, j)
	return nil
}
