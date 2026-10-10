package login_qwenwork

// 本测试锁定「浏览器登录回调链路」的三个事实（issue：Docker 形态下千问办公
// 添加账号失败，浏览器拿到 code 后 connection refused）：
//
//  1. redirect_uri 的主机来自 logincb.Options.Host，**不是**监听地址——
//     容器内监听 0.0.0.0（才可能被端口转发命中），而 0.0.0.0 不能写进 URL；
//  2. redirect_uri 里的端口是**实际监听**的那个端口（Listen 写 :0 时也一样），
//     浏览器去连它必须真能连上——这就是「发布端口后宿主进得来」的全部；
//  3. 固定端口形态下重复 Start 不得 address already in use：上一次登录被取消/
//     未完成时监听会残留，Start 自己负责关掉（见 Start 里的 Shutdown()）。

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"wild-work/internal/logincb"
)

// freePort 取一个空闲端口后立即释放，用于模拟容器里的固定端口。
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

func redirectURI(t *testing.T, authURL string) *url.URL {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("解析授权 URL: %v", err)
	}
	ru := u.Query().Get("redirect_uri")
	if ru == "" {
		t.Fatalf("授权 URL 缺 redirect_uri: %s", authURL)
	}
	parsed, err := url.Parse(ru)
	if err != nil {
		t.Fatalf("解析 redirect_uri %q: %v", ru, err)
	}
	return parsed
}

func TestCallbackRedirectHostComesFromOptionsNotListenAddr(t *testing.T) {
	state := filepath.Join(t.TempDir(), "login-state.json")
	// 监听地址的主机与 URL 的主机故意不同：URL 必须取后者。
	cb := logincb.Options{Listen: "127.0.0.1:0", Host: "callback.example.test"}
	authURL, err := Start(NewClient(), state, cb)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer Shutdown()

	ru := redirectURI(t, authURL)
	if ru.Hostname() != "callback.example.test" {
		t.Errorf("redirect_uri 主机 = %q，应取 Options.Host 而非监听地址", ru.Hostname())
	}
	if ru.Path != "/callback" {
		t.Errorf("redirect_uri 路径 = %q want /callback", ru.Path)
	}
	port, err := strconv.Atoi(ru.Port())
	if err != nil || port <= 0 {
		t.Fatalf("redirect_uri 端口非法 %q: %v", ru.Port(), err)
	}
	// 端口必须真的是本进程在听的那个（容器形态 = 宿主发布这个端口后进得来）
	ln, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("redirect_uri 端口 %d 未监听: %v", port, err)
	}
	_ = ln.Close()
}

// TestCallbackReachableEndToEnd 模拟「宿主浏览器」打到回调端口：
// 走 127.0.0.1 + redirect_uri 里的端口，必须收到回调页（= 拿 code 那一步成功）。
func TestCallbackReachableEndToEnd(t *testing.T) {
	state := filepath.Join(t.TempDir(), "login-state.json")
	authURL, err := Start(NewClient(), state, logincb.Options{Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer Shutdown()

	ru := redirectURI(t, authURL)
	if ru.Hostname() != "127.0.0.1" {
		t.Fatalf("零值 Host 应为 127.0.0.1，got %q", ru.Hostname())
	}
	resp, err := http.Get(ru.String() + "?code=e2e-test-code")
	if err != nil {
		t.Fatalf("回调端口不可达（Docker 形态就是这个症状）: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("回调 HTTP %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `class="card ok"`) {
		t.Errorf("回调页不是成功态，body: %.200s", body)
	}
}

// TestFixedPortRepeatedStartDoesNotConflict 锁定 Start 的自愈：
// 上一次登录的回调还挂着（取消/超时残留）时，再次 Start 必须先关掉它。
// 否则容器里固定端口会直接 listen: address in use，登录起不来。
func TestFixedPortRepeatedStartDoesNotConflict(t *testing.T) {
	port := freePort(t)
	listen := fmt.Sprintf("127.0.0.1:%d", port)
	cb := logincb.Options{Listen: listen}

	dir := t.TempDir()
	first, err := Start(NewClient(), filepath.Join(dir, "s1.json"), cb)
	if err != nil {
		t.Fatalf("Start #1: %v", err)
	}
	if got := redirectURI(t, first).Port(); got != fmt.Sprint(port) {
		t.Fatalf("Start #1 redirect 端口 = %s want %d", got, port)
	}
	// 故意不 Shutdown：模拟「上一次登录未收尾」
	second, err := Start(NewClient(), filepath.Join(dir, "s2.json"), cb)
	if err != nil {
		t.Fatalf("Start #2 固定端口冲突（Start 未先关上一次监听）: %v", err)
	}
	defer Shutdown()
	if got := redirectURI(t, second).Port(); got != fmt.Sprint(port) {
		t.Errorf("Start #2 redirect 端口 = %s want %d", got, port)
	}
	// 第二次 Start 后端口仍在监听（否则浏览器够不着）
	ln, err := net.Dial("tcp", listen)
	if err != nil {
		t.Fatalf("固定端口未在监听: %v", err)
	}
	_ = ln.Close()
}
