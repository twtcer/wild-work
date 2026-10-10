package logincb

import "testing"

// 零值必须逐字等价于旧行为（桌面形态），否则升级会静默改变登录链路。
func TestZeroOptionsKeepsLegacyBehavior(t *testing.T) {
	var o Options
	if o.ListenAddr() != "127.0.0.1:0" {
		t.Errorf("ListenAddr()=%q want 127.0.0.1:0", o.ListenAddr())
	}
	if o.AdvertiseHost() != "127.0.0.1" {
		t.Errorf("AdvertiseHost()=%q want 127.0.0.1", o.AdvertiseHost())
	}
	if got := o.RedirectURL(43903, "/callback"); got != "http://127.0.0.1:43903/callback" {
		t.Errorf("RedirectURL()=%q", got)
	}
}

// 容器形态：监听 0.0.0.0（能被端口转发命中）但 URL 必须仍写 127.0.0.1
// （浏览器在宿主机）。两者是不同字段，这里锁死不得被「统一成一个值」。
func TestContainerShapeListenAndAdvertiseDiffer(t *testing.T) {
	o := Options{Listen: "0.0.0.0:43903"}
	if o.ListenAddr() != "0.0.0.0:43903" {
		t.Errorf("ListenAddr()=%q", o.ListenAddr())
	}
	if o.AdvertiseHost() != "127.0.0.1" {
		t.Errorf("AdvertiseHost()=%q want 127.0.0.1", o.AdvertiseHost())
	}
	if got := o.RedirectURL(43903, "/callback"); got != "http://127.0.0.1:43903/callback" {
		t.Errorf("RedirectURL()=%q", got)
	}
}

func TestAdvertiseHostOverrideAndPathNormalization(t *testing.T) {
	o := Options{Listen: "0.0.0.0:43903", Host: "192.168.1.10"}
	if got := o.RedirectURL(43903, "authorize"); got != "http://192.168.1.10:43903/authorize" {
		t.Errorf("RedirectURL()=%q（path 缺前导 / 应补上）", got)
	}
	// IPv6 主机必须带方括号，否则 URL 非法
	v6 := Options{Host: "::1"}
	if got := v6.RedirectURL(1234, "/callback"); got != "http://[::1]:1234/callback" {
		t.Errorf("IPv6 RedirectURL()=%q", got)
	}
}
