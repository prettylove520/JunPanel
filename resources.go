package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Docker volumes and networks: list, create and remove. Removal is never forced, so
// Docker itself refuses anything a container still uses.

var networkName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)
var networkID = regexp.MustCompile(`^[a-f0-9]{12,64}$`)

func (a *agent) volumeList(w http.ResponseWriter, r *http.Request) error {
	raw, e := a.docker.call("GET", "/volumes", nil)
	if e != nil {
		return e
	}
	var list struct {
		Volumes []struct {
			Name, Driver, CreatedAt, Scope string
			Labels                         map[string]string
		}
	}
	if e = decodeObject(raw, &list); e != nil {
		return e
	}
	usage := map[string][2]int64{}
	if raw, e := a.docker.call("GET", "/system/df?type=volume", nil); e == nil {
		var df struct {
			Volumes []struct {
				Name      string
				UsageData *struct{ Size, RefCount int64 }
			}
		}
		if decodeObject(raw, &df) == nil {
			for _, v := range df.Volumes {
				if v.UsageData != nil {
					usage[v.Name] = [2]int64{v.UsageData.Size, v.UsageData.RefCount}
				}
			}
		}
	}
	out := []map[string]any{}
	for _, v := range list.Volumes {
		u, known := usage[v.Name]
		row := map[string]any{"name": v.Name, "driver": v.Driver, "created": v.CreatedAt, "project": v.Labels["com.docker.compose.project"], "panel_app": strings.HasPrefix(v.Labels["com.docker.compose.project"], "junpanel-")}
		if known {
			row["size"] = u[0]
			row["in_use"] = u[1] > 0
			row["containers"] = u[1]
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) < out[j]["name"].(string) })
	send(w, 200, out)
	return nil
}

func (a *agent) volumeCreate(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name string `json:"name"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if !volumeName.MatchString(req.Name) {
		return bad(400, "卷名需以字母或数字开头，只含字母、数字、下划线、点和中划线")
	}
	if _, code, e := a.docker.request("GET", "/volumes/"+url.PathEscape(req.Name), nil); e != nil {
		return e
	} else if code == 200 {
		return bad(409, "已有同名的卷")
	}
	raw, code, e := a.docker.request("POST", "/volumes/create", map[string]any{"Name": req.Name, "Labels": map[string]string{"junpanel.created": "true"}})
	if e != nil {
		return e
	}
	if code != 201 {
		return bad(502, "创建卷失败："+dockerMessage(raw))
	}
	send(w, 200, map[string]any{"ok": true, "name": req.Name})
	return nil
}

func (a *agent) volumeDelete(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	if !volumeName.MatchString(name) {
		return bad(400, "卷名无效")
	}
	raw, code, e := a.docker.request("DELETE", "/volumes/"+url.PathEscape(name), nil)
	if e != nil {
		return e
	}
	switch code {
	case 204:
		send(w, 200, map[string]any{"ok": true})
		return nil
	case 404:
		return bad(404, "卷不存在")
	case 409:
		return bad(409, "此卷仍被容器使用（包括已停止的容器），请先删除相关容器")
	}
	return bad(502, "删除卷失败："+dockerMessage(raw))
}

func (a *agent) networkList(w http.ResponseWriter, r *http.Request) error {
	raw, e := a.docker.call("GET", "/networks", nil)
	if e != nil {
		return e
	}
	var list []struct {
		Name, Id, Driver, Scope, Created string
		Internal                         bool
		Labels                           map[string]string
		IPAM                             struct {
			Config []struct{ Subnet, Gateway string }
		}
	}
	if e = decodeObject(raw, &list); e != nil {
		return e
	}
	if len(list) > 200 {
		list = list[:200]
	}
	out := []map[string]any{}
	for _, n := range list {
		subnets := []string{}
		for _, c := range n.IPAM.Config {
			if c.Subnet != "" {
				subnets = append(subnets, c.Subnet)
			}
		}
		users, _ := a.networkUsers(n.Name)
		builtin := n.Name == "bridge" || n.Name == "host" || n.Name == "none"
		out = append(out, map[string]any{"id": n.Id, "name": n.Name, "driver": n.Driver, "scope": n.Scope, "internal": n.Internal, "subnets": subnets, "containers": len(users), "builtin": builtin, "project": n.Labels["com.docker.compose.project"], "created": n.Created})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) < out[j]["name"].(string) })
	send(w, 200, out)
	return nil
}

// networkUsers names every container attached to a network, stopped ones included:
// Docker removes a network a stopped container still needs, and that container then
// cannot start again. Docker matches stopped containers by network name only.
func (a *agent) networkUsers(name string) ([]string, error) {
	filter, _ := json.Marshal(map[string][]string{"network": {name}})
	raw, e := a.docker.call("GET", "/containers/json?all=1&filters="+url.QueryEscape(string(filter)), nil)
	if e != nil {
		return nil, e
	}
	var rows []struct{ Names []string }
	if e = decodeObject(raw, &rows); e != nil {
		return nil, e
	}
	out := []string{}
	for _, r := range rows {
		if len(r.Names) > 0 {
			out = append(out, strings.TrimPrefix(r.Names[0], "/"))
		}
	}
	sort.Strings(out)
	return out, nil
}

func (a *agent) networkCreate(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name     string `json:"name"`
		Internal bool   `json:"internal"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	if !networkName.MatchString(req.Name) || req.Name == "bridge" || req.Name == "host" || req.Name == "none" {
		return bad(400, "网络名需以字母或数字开头，只含字母、数字、下划线、点和中划线")
	}
	raw, code, e := a.docker.request("POST", "/networks/create", map[string]any{"Name": req.Name, "Driver": "bridge", "Internal": req.Internal, "CheckDuplicate": true, "Labels": map[string]string{"junpanel.created": "true"}})
	if e != nil {
		return e
	}
	if code == 409 {
		return bad(409, "已有同名的网络")
	}
	if code != 201 {
		return bad(502, "创建网络失败："+dockerMessage(raw))
	}
	send(w, 200, map[string]any{"ok": true, "name": req.Name})
	return nil
}

func (a *agent) networkDelete(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !networkID.MatchString(id) {
		return bad(400, "网络 ID 无效")
	}
	raw, code, e := a.docker.request("GET", "/networks/"+id, nil)
	if e != nil {
		return e
	}
	if code == 404 {
		return bad(404, "网络不存在")
	}
	var detail struct{ Name string }
	if code != 200 || json.Unmarshal(raw, &detail) != nil || detail.Name == "" {
		return bad(502, "读取网络信息失败")
	}
	if detail.Name == "bridge" || detail.Name == "host" || detail.Name == "none" {
		return bad(403, "这是 Docker 预置的网络，不能删除")
	}
	users, e := a.networkUsers(detail.Name)
	if e != nil {
		return e
	}
	if len(users) > 0 {
		return bad(409, "此网络仍有容器连接（包括已停止的容器）："+truncate(strings.Join(users, "、"), 300)+"。请先删除这些容器或把它们移出此网络")
	}
	raw, code, e = a.docker.request("DELETE", "/networks/"+id, nil)
	if e != nil {
		return e
	}
	switch code {
	case 204, 200:
		send(w, 200, map[string]any{"ok": true})
		return nil
	case 404:
		return bad(404, "网络不存在")
	case 403, 409:
		if msg := strings.ToLower(dockerMessage(raw)); strings.Contains(msg, "pre-defined") || strings.Contains(msg, "predefined") {
			return bad(403, "这是 Docker 预置的网络，不能删除")
		}
		return bad(409, "此网络仍有容器连接，请先断开或删除这些容器")
	}
	return bad(502, "删除网络失败："+dockerMessage(raw))
}
