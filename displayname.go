package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// The name shown at the top of the overview. Hosting providers hand out names such as kvm18023, so the
// owner can set a readable one here. It is only a label in the panel: the server's own hostname is not touched.

const displayNameMax = 40

func (a *agent) displayNamePath() string { return filepath.Join(a.cfg.State, "display.json") }

// displayName returns the saved name, or "" to fall back to the system hostname.
func (a *agent) displayName() string {
	b, e := os.ReadFile(a.displayNamePath())
	if e != nil {
		return ""
	}
	var v struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	return v.Name
}

// cleanDisplayName trims the name and refuses control characters and anything too long; "" resets it.
func cleanDisplayName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > displayNameMax {
		return "", bad(400, "名称不超过 40 个字")
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", bad(400, "名称不能包含控制字符")
		}
	}
	return s, nil
}

func (a *agent) displayNameSave(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name string `json:"name"`
	}
	if e := body(r, &req); e != nil {
		return e
	}
	name, e := cleanDisplayName(req.Name)
	if e != nil {
		return e
	}
	if name == "" {
		if e = os.Remove(a.displayNamePath()); e != nil && !os.IsNotExist(e) {
			return e
		}
	} else if e = atomicJSON(a.displayNamePath(), map[string]string{"name": name}, 0600); e != nil {
		return e
	}
	send(w, 200, map[string]any{"name": name})
	return nil
}
