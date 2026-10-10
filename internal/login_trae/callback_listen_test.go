package login_trae

// 本测试锁定 TraeWork 本机回调的容器形态语义（issue：Docker 下登录回调失败）：
//
//  1. auth_callback_url 的主机来自 logincb.Options.Host —— 监听 0.0.0.0 时
//     旧实现会把 `0.0.0.0:port` 直接写进 URL（浏览器无法连接）；
//  2. 固定端口下重复 Start 必须先关掉上一次的监听（否则 address already in use）；
//  3. Shutdown 后端口必须立刻可再绑定（取消登录后马上换渠道登录的场景）。

import (
	"fmt"
	"net"
	"path/filepath"
	"testing"

	"wild-work/internal/logincb"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("申请空闲端口: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestCallbackURLUsesAdvertisedHostOnFixedPort(t *testing.T) {
	port := freePort(t)
	cb := logincb.Options{Listen: fmt.Sprintf("127.0.0.1:%d", port)}
	state := filepath.Join(t.TempDir(), "login-state.json")

	authURL, err := Start(NewClient(), state, cb)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer Shutdown()

	got := extractQuery(t, authURL, "auth_callback_url")
	want := fmt.Sprintf("http://127.0.0.1:%d/authorize", port)
	if got != want {
		t.Errorf("auth_callback_url = %q want %q", got, want)
	}
	// 端口必须在听（否则宿主发布端口也进不来）
	if ln, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		t.Fatalf("回调端口未监听: %v", err)
	} else {
		_ = ln.Close()
	}
}

func TestFixedPortStartSelfHealsAndShutdownFreesPort(t *testing.T) {
	port := freePort(t)
	cb := logincb.Options{Listen: fmt.Sprintf("127.0.0.1:%d", port)}
	dir := t.TempDir()

	// 第一次 Start 后不关闭（模拟取消登录时残留的监听）
	if _, err := Start(NewClient(), filepath.Join(dir, "s1.json"), cb); err != nil {
		t.Fatalf("Start #1: %v", err)
	}
	if _, err := Start(NewClient(), filepath.Join(dir, "s2.json"), cb); err != nil {
		t.Fatalf("Start #2 固定端口冲突（Start 未先关上一次监听）: %v", err)
	}

	Shutdown()
	Shutdown() // 幂等

	// 端口必须立刻还回来，否则下一次登录（换渠道也一样）直接失败
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("Shutdown 后端口仍被占用: %v", err)
	}
	_ = ln.Close()
}
