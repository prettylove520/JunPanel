package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// The `junpanel` command on the server: a menu and subcommands for what an administrator does over SSH.
// It is the same program as the services, linked into /usr/local/bin by the installer.

const (
	installDir      = "/opt/junpanel"
	binPath         = "/opt/junpanel/junpanel"
	linkPath        = "/usr/local/bin/junpanel"
	webData         = "/var/lib/junpanel-web"
	fragmentDir     = "/etc/caddy/junpanel.d"
	quickInstallURL = "https://raw.githubusercontent.com/" + updateRepo + "/main/quick-install.sh"
)

var cliCommands = map[string]bool{"menu": true, "info": true, "domain": true, "upgrade": true, "uninstall": true, "help": true}

const cliHelp = `JunPanel 命令行管理（需要 root，普通用户会自动改用 sudo）

  junpanel                    打开菜单
  junpanel info               查看面板信息：管理员账号、访问地址、版本、服务状态
  junpanel password           重置管理员密码并显示新密码（旧密码以哈希保存，无法查看）
  junpanel domain add 域名    添加或更换域名入口（先把域名的 A 记录指向本机）
  junpanel domain remove      移除域名入口
  junpanel upgrade [-y]       检查并升级到最新版本
  junpanel uninstall [-y]     卸载面板（保留数据、网站和应用）
  junpanel version            显示版本
`

func isInteractive() bool {
	st, e := os.Stdin.Stat()
	return e == nil && st.Mode()&os.ModeCharDevice != 0
}

// ensureRoot runs the command again under sudo when it was started by an ordinary user.
func ensureRoot() {
	if os.Geteuid() == 0 {
		return
	}
	sudo, e := exec.LookPath("sudo")
	exe, ee := os.Executable()
	if e != nil || ee != nil {
		fmt.Fprintln(os.Stderr, "需要 root 权限：请先切换到 root（su -）再执行 junpanel。")
		os.Exit(1)
	}
	syscall.Exec(sudo, append([]string{"sudo", exe}, os.Args[1:]...), os.Environ())
	fmt.Fprintln(os.Stderr, "无法使用 sudo，请切换到 root 后重试。")
	os.Exit(1)
}

func installed() bool {
	st, e := os.Lstat(filepath.Join(installDir, ".junpanel-install"))
	return e == nil && st.Mode().IsRegular()
}

func runCLI(cmd string, args []string) int {
	if cmd == "help" {
		fmt.Print(cliHelp)
		return 0
	}
	ensureRoot()
	if !installed() {
		fmt.Fprintln(os.Stderr, "未找到 JunPanel 安装标记，请先完成安装。")
		return 1
	}
	var e error
	switch cmd {
	case "menu":
		e = cliMenu()
	case "info":
		e = cliInfo()
	case "domain":
		e = cliDomain(args)
	case "upgrade":
		e = cliUpgrade(hasFlag(args, "-y"))
	case "uninstall":
		e = cliUninstall(hasFlag(args, "-y"))
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, "\n[JunPanel] "+e.Error())
		return 1
	}
	return 0
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

var stdin = bufio.NewReader(os.Stdin)

func ask(prompt string) string {
	fmt.Print(prompt)
	line, _ := stdin.ReadString('\n')
	return strings.TrimSpace(line)
}

func confirm(prompt string) bool {
	switch strings.ToLower(ask(prompt + " [y/N] ")) {
	case "y", "yes", "是":
		return true
	}
	return false
}

func systemState(unit string) string {
	out, _ := exec.Command("systemctl", "is-active", unit).Output()
	if s := strings.TrimSpace(string(out)); s != "" {
		return s
	}
	return "未知"
}

// entryLinks turns the entries of the panel's access file into login links.
func entryLinks(entries []accessEntry) []string {
	links := []string{}
	for _, e := range entries {
		links = append(links, "https://"+e.Address)
	}
	return links
}

func cliInfo() error {
	acct, e := readAccount(filepath.Join(webData, "account.json"))
	if e != nil {
		return fmt.Errorf("读取管理员配置失败：%v", e)
	}
	port := "8787"
	if b, e := os.ReadFile(filepath.Join(installDir, "port")); e == nil && strings.TrimSpace(string(b)) != "" {
		port = strings.TrimSpace(string(b))
	}
	entries, _ := panelEntries(fragmentDir)
	links := entryLinks(entries)
	fmt.Printf("\n  JunPanel %s\n  管理员账号：%s\n", version, acct.User)
	fmt.Println("  管理员密码：以加盐哈希保存，无法查看；忘记了请执行 junpanel password 重置")
	if acct.TOTP != "" {
		fmt.Println("  两步验证：已开启")
	} else {
		fmt.Println("  两步验证：未开启（建议登录后在设置里开启）")
	}
	if len(links) == 0 {
		fmt.Println("  公网入口：未配置（可用 junpanel domain add 域名 添加）")
	}
	for _, l := range links {
		fmt.Println("  登录地址：" + l)
	}
	fmt.Printf("  本机入口：http://127.0.0.1:%s（SSH 隧道使用）\n", port)
	fmt.Printf("  服务状态：网页 %s · 管理代理 %s · Caddy %s\n\n", systemState("junpanel-web.service"), systemState("junpanel-agent.service"), systemState("caddy.service"))
	return nil
}

// resetPassword runs the same reset as `junpanel password`, as the web service's own user so the
// account file keeps its owner.
func resetPassword() error {
	cmd := exec.Command("runuser", "-u", "junpanel-web", "--", binPath, "password", "--data", webData)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if e := cmd.Run(); e != nil {
		return fmt.Errorf("重置密码失败：%v", e)
	}
	fmt.Println("所有已登录的会话已失效，请用新密码重新登录。")
	return nil
}

// runAccessScript runs the embedded public-access.sh, which changes the panel's HTTPS entry in Caddy.
func runAccessScript(args ...string) error {
	cmd := exec.Command("/bin/bash", append([]string{"-s", "--"}, args...)...)
	cmd.Stdin = bytes.NewReader(publicAccessScript)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	return cmd.Run()
}

func cliDomain(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法：junpanel domain add 域名 | junpanel domain remove")
	}
	switch args[0] {
	case "add":
		name := ""
		if len(args) > 1 {
			name = args[1]
		} else if isInteractive() {
			name = ask("请输入域名（例如 panel.example.com；需已把 A 记录解析到本机）：")
		}
		script, e := accessArgs("set", name, "", false)
		if e != nil {
			return e
		}
		fmt.Println("正在配置域名入口并申请证书，请稍候…")
		if e = runAccessScript(script...); e != nil {
			return fmt.Errorf("域名入口配置未完成，请查看上方提示（原入口已恢复）")
		}
		return nil
	case "remove":
		script, _ := accessArgs("remove-domain", "", "", false)
		return runAccessScript(script...)
	}
	return fmt.Errorf("未知操作：%s（可用 add、remove）", args[0])
}

// latestVersion reads the version the repository's installer is pinned to.
func latestVersion() (string, error) {
	resp, e := upgradeClient().Get(fmt.Sprintf("%s?t=%d", quickInstallURL, time.Now().Unix()))
	if e != nil {
		return "", fmt.Errorf("无法连接更新服务器：%s", hostError(e))
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("更新服务器返回 HTTP %d", resp.StatusCode)
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, maxScriptBytes))
	if e != nil {
		return "", fmt.Errorf("读取更新信息失败")
	}
	v, _, e := parsePin(string(raw))
	return v, e
}

func cliUpgrade(yes bool) error {
	latest, e := latestVersion()
	if e != nil {
		return e
	}
	if compareVersions(latest, version) <= 0 {
		fmt.Printf("当前 v%s 已是最新版本。\n", version)
		return nil
	}
	fmt.Printf("发现新版本 v%s（当前 v%s）。升级会让面板重启约 1 分钟，应用和网站不受影响。\n", latest, version)
	if !yes && !confirm("现在升级吗？") {
		fmt.Println("已取消。")
		return nil
	}
	cmd := exec.Command("bash", "-c", `curl -fsSL "$0?t=$(date +%s)" | bash`, quickInstallURL)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	if e = cmd.Run(); e != nil {
		return fmt.Errorf("升级未完成，请查看上方提示")
	}
	return nil
}

func cliUninstall(yes bool) error {
	fmt.Println("将卸载 JunPanel：停止并移除面板服务与程序，移除面板的 HTTPS 入口。")
	fmt.Println("账号、应用数据、站点文件和 Caddy 里的网站配置都会保留，网站与应用继续运行；以后可重新安装恢复。")
	if !yes && !confirm("确认卸载？") {
		fmt.Println("已取消。")
		return nil
	}
	fmt.Println("移除 HTTPS 入口…")
	if e := runAccessScript("--disable"); e != nil {
		fmt.Println("入口未能自动移除，可稍后手动清理 /etc/caddy/junpanel.d/panel-access.caddy。")
	}
	fmt.Println("停止并移除服务…")
	exec.Command("systemctl", "disable", "--now", "junpanel-web.service", "junpanel-agent.service").Run()
	for _, f := range []string{"/etc/systemd/system/junpanel-web.service", "/etc/systemd/system/junpanel-agent.service", binPath, linkPath} {
		os.Remove(f)
	}
	exec.Command("systemctl", "daemon-reload").Run()
	fmt.Println("JunPanel 已卸载。重新安装只需再执行一键安装命令。")
	return nil
}

func cliMenu() error {
	for {
		fmt.Print(`
  JunPanel 管理菜单
  1) 查看面板信息（账号、地址、状态）
  2) 重置管理员密码
  3) 添加 / 更换域名
  4) 移除域名
  5) 检查并升级
  6) 卸载面板
  0) 退出
`)
		var e error
		switch ask("请选择：") {
		case "1":
			e = cliInfo()
		case "2":
			e = resetPassword()
		case "3":
			e = cliDomain([]string{"add"})
		case "4":
			e = cliDomain([]string{"remove"})
		case "5":
			e = cliUpgrade(false)
		case "6":
			if e = cliUninstall(false); e == nil {
				if _, err := os.Stat(binPath); err != nil {
					return nil
				}
			}
		case "0", "q", "":
			return nil
		default:
			fmt.Println("请输入菜单里的数字。")
		}
		if e != nil {
			fmt.Fprintln(os.Stderr, "[JunPanel] "+e.Error())
		}
	}
}

// resetAsWebUser reruns `junpanel password` as the web service's user when started as root, so the
// account file is never left owned by root.
func resetAsWebUser() (handled bool, code int) {
	if os.Geteuid() != 0 {
		return false, 0
	}
	if _, e := user.Lookup("junpanel-web"); e != nil {
		return false, 0
	}
	exe, e := os.Executable()
	if e != nil {
		return false, 0
	}
	cmd := exec.Command("runuser", append([]string{"-u", "junpanel-web", "--", exe}, os.Args[1:]...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if e = cmd.Run(); e != nil {
		return true, 1
	}
	return true, 0
}
