//go:build !windows && !darwin

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InfoBox 其他平台：输出到 stderr。
func InfoBox(title, msg string) { Notify(title, msg) }

// Notify 其他平台：输出到 stderr。
func Notify(title, msg string) {
	_, _ = os.Stderr.WriteString(title + ": " + msg + "\n")
}

// AskYesNo 其他平台：默认否。
func AskYesNo(title, msg string) bool { return false }

// 本 fork 说明：CodeArts 授权走「无痕窗口 + 用户点授权」（internal/app/app.go 调
// DefaultBrowserIncognito/LaunchIncognito），故上游 1e25be4 的死代码清理在本分支不适用，
// 这组函数必须保留；否则 internal/app 编译失败。
// DefaultBrowserIncognito 其他平台：不探测，走系统默认。
func DefaultBrowserIncognito() (exePath, flag string, ok bool) { return "", "", false }

// LaunchIncognito 其他平台：直接 open url。
func LaunchIncognito(exe, flag, url string) error { return OpenURL(url) }

// OpenURL 其他平台：xdg-open。
func OpenURL(url string) error { return exec.Command("xdg-open", url).Start() }

// 开机自启（Linux）：写 systemd **用户级** unit（~/.config/systemd/user/）并 enable。
//
// 为什么用用户级而非系统级：本工具是桌面/半桌面常驻程序，数据在用户 HOME 下
// （auths/、data/），系统级 unit 要以 root 跑并额外处理 WANTED= 与用户上下文；
// 用户级与 macOS LaunchAgent 语义一致（登录用户会话启动）。
//
// 注意：用户在无 systemd 的环境（如容器、OpenRC）下 enable 会失败——如实返回错误，
// 不假装成功（与 Windows/macOS 同口径）。容器内本来也不该自启（R125）。
const autostartUnitName = "wild-work.service"

// autostartUnitPath 用户级 unit 路径（XDG_CONFIG_HOME 优先，回退 ~/.config）。
func autostartUnitPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user", autostartUnitName)
}

// autostartUnit 生成 systemd unit 文本。
//
// --no-tray：开机自启场景多半无图形会话接管托盘（R12）；无参启动在无 DBus
// 环境会 panic 退出，故显式带该参数保证起得来。
func autostartUnit(exe, wd string) string {
	return "[Unit]\n" +
		"Description=wild-work（多渠道账号聚合 daemon）\n" +
		"After=network-online.target\n" +
		"Wants=network-online.target\n" +
		"\n[Service]\n" +
		"Type=simple\n" +
		"WorkingDirectory=" + wd + "\n" +
		"ExecStart=\"" + exe + "\" --no-tray\n" +
		"Restart=on-failure\n" +
		"RestartSec=5\n" +
		"\n[Install]\n" +
		"WantedBy=default.target\n"
}

// SetAutostart Linux：写/删 systemd 用户 unit 并 enable/disable。
func SetAutostart(on bool) error {
	path := autostartUnitPath()
	if path == "" {
		return fmt.Errorf("无法确定用户配置目录（HOME 未设置？）")
	}
	if !on {
		// disable 失败不阻塞删除：unit 文件可能已不存在（手工删过）
		_ = exec.Command("systemctl", "--user", "disable", "--now", autostartUnitName).Run()
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	wd := workDirFor(exe)
	if err := os.MkdirAll(filepath.Join(wd, "data"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(autostartUnit(exe, wd)), 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload 失败（无 systemd？）：%v: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("systemctl", "--user", "enable", autostartUnitName).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable 失败：%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// workDirFor 推导数据/工作目录，规则与 cmd/wild-work 的 workDir() 保持一致：
// WILDWORK_HOME 优先（Docker 下为 /data），否则用可执行文件所在目录（便携版形态）。
// darwin 那份额外处理 .app bundle，Linux 无此形态。
func workDirFor(prog string) string {
	if v := os.Getenv("WILDWORK_HOME"); v != "" {
		return v
	}
	return filepath.Dir(prog)
}

// AutostartEnabled Linux：unit 文件存在且 ExecStart 指向当前可执行文件。
func AutostartEnabled() bool {
	path := autostartUnitPath()
	if path == "" {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), exe)
}

// OpenWithTextEditor 其他平台：xdg-open。
func OpenWithTextEditor(path string) error { return exec.Command("xdg-open", path).Start() }
