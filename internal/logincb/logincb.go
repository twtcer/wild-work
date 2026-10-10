// Package logincb 描述「本机 OAuth 回调」的监听地址与对外可见主机。
//
// 背景（issue：Docker 形态下千问办公/TraeWork 浏览器登录回调失败）：
// traework 与 qwenwork 的登录要把回调地址写进授权 URL（redirect_uri /
// auth_callback_url），旧实现固定 `net.Listen("tcp", "127.0.0.1:0")` +
// 回调 URL 恒用 `127.0.0.1:<随机端口>`。桌面形态没问题——浏览器与进程同机；
// 但容器内这 3 个值分别是：
//
//	监听：容器自身环回（宿主够不着）
//	端口：随机（宿主无从发布）
//	浏览器重定向的 127.0.0.1：宿主机环回（那里没人监听）
//
// ⇒ 浏览器拿到 code 后撞 connection refused，登录 5 分钟后超时。
//
// 修法：容器内固定端口 + 监听 0.0.0.0 + compose 发布该端口；
// redirect 仍写 127.0.0.1（浏览器在宿主机，命中已发布端口即转发进容器）。
// 桌面默认值不变（127.0.0.1:0 + 127.0.0.1），零行为差异。
//
// 两个字段职责不同、**不可合并**：
//
//	Listen 监听地址（进程侧，容器内须 0.0.0.0 才能被端口转发命中）
//	Host   写进 URL 的主机（浏览器侧，必须是浏览器能连的地址）
package logincb

import (
	"net"
	"strconv"
	"strings"
)

// 默认值 = 旧行为：环回 + 随机端口。
const (
	defaultListen = "127.0.0.1:0"
	defaultHost   = "127.0.0.1"
)

// Options 回调监听配置。零值即桌面默认（环回 + 随机端口 + 127.0.0.1）。
type Options struct {
	// Listen 是 net.Listen 的地址串，如 "127.0.0.1:0"（随机端口）/
	// "0.0.0.0:43903"（容器内固定端口）。空 = "127.0.0.1:0"。
	Listen string
	// Host 是回调 URL 中的主机（**不带端口**），如 "127.0.0.1"。
	// 从别的机器打开面板时可设为面板所在机器的 IP/域名——前提是上游接受
	// 该非环回 redirect_uri（环回任意端口已实测通过，非环回未实测）。
	Host string
}

// ListenAddr 监听地址串。
func (o Options) ListenAddr() string {
	if s := strings.TrimSpace(o.Listen); s != "" {
		return s
	}
	return defaultListen
}

// AdvertiseHost 回调 URL 中的主机。
func (o Options) AdvertiseHost() string {
	if s := strings.TrimSpace(o.Host); s != "" {
		return s
	}
	return defaultHost
}

// RedirectURL 拼出写进 redirect_uri / auth_callback_url 的完整回调地址。
// port 取自**实际监听**的端口（Listen 里写 0 时也正确）；path 以 "/" 开头。
// 用 net.JoinHostPort，故 IPv6 主机会自动加方括号。
func (o Options) RedirectURL(port int, path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "http://" + net.JoinHostPort(o.AdvertiseHost(), strconv.Itoa(port)) + path
}
