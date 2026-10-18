// JunPanel is an independently implemented, local-first server workbench.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

//go:embed web/*
var assets embed.FS
var version = "0.1.4"

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	if len(os.Args) < 2 {
		fmt.Println("JunPanel · server / agent / setup / password / version")
		return
	}
	cmd := os.Args[1]
	f := flag.NewFlagSet(cmd, flag.ExitOnError)
	data := f.String("data", "/var/lib/junpanel-web", "认证数据目录")
	state := f.String("state", "/var/lib/junpanel-agent", "管理数据目录")
	sock := f.String("socket", "/run/junpanel/agent.sock", "本机代理 socket")
	port := f.Int("port", 8787, "本机端口")
	root := f.String("root", "/srv/junpanel", "站点与文件根目录")
	caddy := f.String("caddy", "/etc/caddy/Caddyfile", "现有 Caddyfile")
	fragments := f.String("fragments", "/etc/caddy/junpanel.d", "面板 Caddy 片段目录")
	demo := f.Bool("demo", false, "仅模拟数据的本机预览")
	f.Parse(os.Args[2:])
	if *demo && cmd != "server" {
		log.Fatal("演示模式仅支持 server --demo，未执行此命令")
	}
	switch cmd {
	case "version":
		fmt.Println("JunPanel", version)
		return
	case "setup", "password":
		if e := os.MkdirAll(*data, 0750); e != nil {
			log.Fatal(e)
		}
		file := filepath.Join(*data, "account.json")
		if cmd == "setup" {
			if _, e := os.Stat(file); e == nil {
				fmt.Println("已有管理员配置已保留")
				return
			}
		}
		password := randomID(24)
		account := newAccount("admin", password)
		if cmd == "password" {
			if old, e := readAccount(file); e == nil {
				account.User = old.User
			}
		}
		if e := atomicJSON(file, account, 0600); e != nil {
			log.Fatal(e)
		}
		fmt.Printf("管理员：%s\n初始密码：%s\n首次登录后请在设置中修改密码。\n", account.User, password)
		return
	case "agent":
		cfg := agentConfig{State: *state, Root: *root, Caddy: *caddy, Fragments: *fragments, Socket: *sock}
		a, e := newAgent(cfg)
		if e != nil {
			log.Fatal(e)
		}
		if e = os.MkdirAll(filepath.Dir(*sock), 0750); e != nil {
			log.Fatal(e)
		}
		if st, e := os.Lstat(*sock); e == nil {
			if st.Mode()&os.ModeSocket == 0 {
				log.Fatal("拒绝覆盖非 socket 路径")
			}
			os.Remove(*sock)
		}
		listener, e := net.Listen("unix", *sock)
		if e != nil {
			log.Fatal(e)
		}
		defer os.Remove(*sock)
		if e = os.Chmod(*sock, 0660); e != nil {
			log.Fatal(e)
		}
		metricsStop := make(chan struct{})
		defer close(metricsStop)
		a.startMetrics(metricsStop)
		serve(&http.Server{Handler: a.routes(), ReadHeaderTimeout: 8 * time.Second, ReadTimeout: 30 * time.Second, MaxHeaderBytes: 32768}, listener)
	case "server":
		if *port < 1024 || *port > 65535 {
			log.Fatal("端口需在 1024–65535")
		}
		web, e := newWeb(filepath.Join(*data, "account.json"), *sock, *port, *demo)
		if e != nil {
			log.Fatal(e)
		}
		files, _ := fs.Sub(assets, "web")
		web.assets = files
		listener, e := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(*port))
		if e != nil {
			log.Fatal(e)
		}
		fmt.Printf("JunPanel %s · http://127.0.0.1:%d · 演示=%v\n", version, *port, *demo)
		if *demo {
			fmt.Println("演示账号 demo / JunPanelPreview2026!；所有变更被拒绝")
		}
		serve(&http.Server{Handler: web.routes(), ReadHeaderTimeout: 8 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32768}, listener)
	default:
		log.Fatal("未知命令")
	}
}
func serve(s *http.Server, l net.Listener) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		s.Shutdown(ctx)
	}()
	if e := s.Serve(l); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}
