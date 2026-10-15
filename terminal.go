package main

import (
	"context"
	"encoding/base64"
	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

var terminalSlots = make(chan struct{}, 2)

func (a *agent) terminal(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("container")
	c, e := a.docker.find(id)
	if e != nil {
		fail(w, e)
		return
	}
	if c.State != "running" || c.Protected {
		fail(w, bad(403, "仅支持未受保护的运行中容器"))
		return
	}
	select {
	case terminalSlots <- struct{}{}:
		defer func() { <-terminalSlots }()
	default:
		fail(w, bad(429, "最多同时连接两个容器终端"))
		return
	}
	upgrade := websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(r *http.Request) bool { return true }}
	// No TCP listener exists on this agent; the public-facing web service validates
	// Origin, an authenticated cookie and a session-bound one-use ticket first.
	conn, e := upgrade.Upgrade(w, r, nil)
	if e != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(64 << 10)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "exec", "-it", id, "/bin/sh")
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "TERM=xterm-256color"}
	terminal, e := pty.StartWithSize(cmd, &pty.Winsize{Rows: 28, Cols: 100})
	if e != nil {
		conn.WriteJSON(map[string]string{"type": "error", "text": "容器内 /bin/sh 不可用，终端未启动"})
		return
	}
	defer terminal.Close()
	defer func() { cancel(); cmd.Wait() }()
	var writeMu sync.Mutex
	var closeOnce sync.Once
	closeAll := func() { closeOnce.Do(func() { cancel(); terminal.Close(); conn.Close() }) }
	go func() { <-ctx.Done(); closeAll() }()
	go func() {
		defer closeAll()
		buf := make([]byte, 8192)
		for {
			n, e := terminal.Read(buf)
			if n > 0 {
				writeMu.Lock()
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				err := conn.WriteJSON(map[string]string{"type": "output", "data": base64.StdEncoding.EncodeToString(buf[:n])})
				writeMu.Unlock()
				if err != nil {
					return
				}
			}
			if e != nil {
				return
			}
		}
	}()
	defer closeAll()
	for {
		conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		var message struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Rows uint16 `json:"rows"`
			Cols uint16 `json:"cols"`
		}
		if conn.ReadJSON(&message) != nil {
			return
		}
		switch message.Type {
		case "input":
			b, e := base64.StdEncoding.DecodeString(message.Data)
			if e != nil || len(b) > 8192 {
				return
			}
			if _, e = terminal.Write(b); e != nil {
				return
			}
		case "resize":
			if message.Rows >= 5 && message.Rows <= 120 && message.Cols >= 20 && message.Cols <= 300 {
				pty.Setsize(terminal, &pty.Winsize{Rows: message.Rows, Cols: message.Cols})
			}
		case "ping":
		default:
			return
		}
	}
}
