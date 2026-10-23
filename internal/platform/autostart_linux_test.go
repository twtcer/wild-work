//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAutostartUnitContent unit 文本必须满足 systemd 能加载的最小结构，
// 且带上 --no-tray（R12：开机自启场景常无图形会话，无参启动在无 DBus 环境会 panic）。
func TestAutostartUnitContent(t *testing.T) {
	unit := autostartUnit("/opt/wild-work/wild-work", "/opt/wild-work")

	for _, want := range []string{
		"[Unit]", "[Service]", "[Install]",
		"Type=simple",
		"WorkingDirectory=/opt/wild-work",
		`ExecStart="/opt/wild-work/wild-work" --no-tray`,
		"Restart=on-failure",
		"WantedBy=default.target",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit 缺少 %q\n---\n%s", want, unit)
		}
	}
}

// TestAutostartUnitQuotesExePath 路径可能含空格（如 ~/My Apps/），ExecStart 必须加引号，
// 否则 systemd 会把它拆成两个参数、启动失败。
func TestAutostartUnitQuotesExePath(t *testing.T) {
	unit := autostartUnit("/home/u/My Apps/wild-work", "/home/u/My Apps")
	if !strings.Contains(unit, `ExecStart="/home/u/My Apps/wild-work" --no-tray`) {
		t.Fatalf("含空格的路径必须被引号包裹：\n%s", unit)
	}
}

// TestAutostartUnitPathXdg unit 路径遵循 XDG_CONFIG_HOME，回退 ~/.config。
func TestAutostartUnitPathXdg(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdgtest")
	want := filepath.Join("/tmp/xdgtest", "systemd", "user", autostartUnitName)
	if got := autostartUnitPath(); got != want {
		t.Fatalf("XDG_CONFIG_HOME 生效时应为 %s，实际 %s", want, got)
	}

	// 未设 XDG_CONFIG_HOME：回退 $HOME/.config
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	want2 := filepath.Join(home, ".config", "systemd", "user", autostartUnitName)
	if got := autostartUnitPath(); got != want2 {
		t.Fatalf("回退路径应为 %s，实际 %s", want2, got)
	}
}

// TestAutostartEnabledReadsUnit AutostartEnabled 判据 = unit 文件存在且含当前 exe 路径。
// 不真调 systemctl（CI/开发机可能无 systemd），只验证文件判据这一层。
func TestAutostartEnabledReadsUnit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if AutostartEnabled() {
		t.Fatal("unit 文件不存在时应为 false")
	}

	exe, err := os.Executable()
	if err != nil {
		t.Skipf("取不到当前可执行文件路径：%v", err)
	}
	path := autostartUnitPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// 写入含当前 exe 的 unit（模拟已 enable 的状态）
	if err := os.WriteFile(path, []byte(autostartUnit(exe, "/tmp/wd")), 0o644); err != nil {
		t.Fatal(err)
	}
	if !AutostartEnabled() {
		t.Fatal("unit 文件含当前 exe 时应为 true")
	}

	// 换成指向别处的 unit ⇒ 视为未为「本可执行文件」启用
	if err := os.WriteFile(path, []byte(autostartUnit("/somewhere/else/wild-work", "/tmp/wd")), 0o644); err != nil {
		t.Fatal(err)
	}
	if AutostartEnabled() {
		t.Fatal("unit 指向其它 exe 时应为 false（避免误报已自启）")
	}
}
