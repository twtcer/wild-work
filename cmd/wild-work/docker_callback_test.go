package main

// 本测试锁定一个**真实 bug**（issue：Docker 打包后添加千问办公/TraeWork 账号
// 浏览器登录「回调失败」，浏览器拿到 code 后 connection refused）：
//
// 这两个渠道的登录要起一个**本机 HTTP 回调**收授权码。旧实现固定
// `net.Listen("tcp", "127.0.0.1:0")` + URL 恒写 `127.0.0.1:<随机端口>`——
// 桌面形态没问题，容器里三处同时错位：
//
//	监听：容器自身环回（宿主够不着）
//	端口：随机（宿主无从发布）
//	URL 里的 127.0.0.1：宿主机环回（那里没人监听）
//
// 修法是「容器内固定端口 + 绑 0.0.0.0 + 宿主发布该端口」——即三处必须同时接线：
//
//	1. compose 里 WILDWORK_CALLBACK_LISTEN 的端口 == ports 里发布的端口
//	   （不一致 = 浏览器被转发到没人监听的端口，症状与修复前一样）
//	2. Dockerfile 给出同样的容器内默认（否则 `docker run` 用户拿不到修复）
//	3. README 写清这一点（否则用户不知道该发布端口）
//
// 这与 R34（漏 `go xxxSch.Run()`）、R41（漏解析回调 flag）同类：
// **声明了却没接线**，不报错不崩溃，只是功能静默不可用。

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../../" + name)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", name, err)
	}
	return string(raw)
}

// containerCallbackPort 从 compose/Dockerfile 的 WILDWORK_CALLBACK_LISTEN 取容器内端口。
func containerCallbackPort(t *testing.T, content, where string) int {
	t.Helper()
	re := regexp.MustCompile(`WILDWORK_CALLBACK_LISTEN[=:]\s*"?([0-9a-zA-Z.\-\[\]:]+)"?`)
	m := re.FindStringSubmatch(content)
	if m == nil {
		t.Fatalf("%s 未设置 WILDWORK_CALLBACK_LISTEN —— 容器内回调会退回 127.0.0.1:<随机端口>，"+
			"宿主浏览器拿 code 后必然 connection refused（登录超时、账号加不进来）", where)
	}
	addr := m[1]
	if !strings.HasPrefix(addr, "0.0.0.0:") && !strings.HasPrefix(addr, ":") {
		t.Errorf("%s 的 WILDWORK_CALLBACK_LISTEN=%q 没绑 0.0.0.0 —— "+
			"容器内监听环回时，宿主经端口映射转发不进来", where, addr)
	}
	i := strings.LastIndex(addr, ":")
	port, err := strconv.Atoi(addr[i+1:])
	if err != nil || port <= 0 {
		t.Fatalf("%s 的 WILDWORK_CALLBACK_LISTEN=%q 端口非法", where, addr)
	}
	return port
}

func TestDockerCallbackPortIsWiredUp(t *testing.T) {
	compose := readRepoFile(t, "docker-compose.yml")
	composePort := containerCallbackPort(t, compose, "docker-compose.yml")

	// compose 必须把**同一个端口**发布到宿主 127.0.0.1（redirect_uri 写的就是它）
	want := regexp.MustCompile(`"127\.0\.0\.1:` + strconv.Itoa(composePort) + `:` + strconv.Itoa(composePort) + `"`)
	if !want.MatchString(compose) {
		t.Errorf("docker-compose.yml 未发布回调端口（期望出现 %q）——\n"+
			"容器内监听 :%d，宿主不发布这个端口就等于没人监听：\n"+
			"浏览器按 redirect_uri 打到 127.0.0.1:%d 会 connection refused，登录 5 分钟超时。",
			"127.0.0.1:"+strconv.Itoa(composePort)+":"+strconv.Itoa(composePort), composePort, composePort)
	}

	// Dockerfile：给出同样的默认（compose 之外的 `docker run` 用户也能用）
	dockerfile := readRepoFile(t, "Dockerfile")
	dockerPort := containerCallbackPort(t, dockerfile, "Dockerfile")
	if dockerPort != composePort {
		t.Errorf("Dockerfile 回调端口 %d 与 docker-compose.yml 的 %d 不一致", dockerPort, composePort)
	}
	if !regexp.MustCompile(`EXPOSE\s+` + strconv.Itoa(dockerPort) + `\b`).MatchString(dockerfile) {
		t.Errorf("Dockerfile 未 EXPOSE %d（端口含义未声明）", dockerPort)
	}

	// README：用户得知道要发布这个端口
	readme := readRepoFile(t, "README.md")
	if !strings.Contains(readme, strconv.Itoa(composePort)) {
		t.Errorf("README.md 未提到回调端口 %d —— Docker 用户无从得知要发布它", composePort)
	}
	if !strings.Contains(readme, "43903") && !strings.Contains(readme, "WILDWORK_CALLBACK") {
		t.Error("README.md 未说明 OAuth 回调端口的用途/配置方式")
	}
}
