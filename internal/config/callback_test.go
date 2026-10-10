package config

// 本机 OAuth 回调配置（Docker 形态）：env 覆盖 + 启动期校验。
// 见 internal/logincb 与 cmd/wild-work/docker_callback_test.go。

import (
	"strings"
	"testing"
)

func TestCallbackDefaultsAreDesktopBehavior(t *testing.T) {
	c := Default()
	if c.Callback.Listen != "127.0.0.1:0" || c.Callback.Host != "127.0.0.1" {
		t.Errorf("默认 callback = %+v，应为环回 + 随机端口（桌面零行为差异）", c.Callback)
	}
}

func TestCallbackEnvOverride(t *testing.T) {
	t.Setenv("WILDWORK_CALLBACK_LISTEN", "0.0.0.0:43903")
	t.Setenv("WILDWORK_CALLBACK_HOST", "192.168.1.10")
	c, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Callback.Listen != "0.0.0.0:43903" {
		t.Errorf("Listen=%q", c.Callback.Listen)
	}
	if c.Callback.Host != "192.168.1.10" {
		t.Errorf("Host=%q", c.Callback.Host)
	}
}

func TestCallbackValidation(t *testing.T) {
	// 写法错误必须在启动时就报错：否则只在用户点「添加账号」那一步才暴露。
	cases := []struct {
		name    string
		listen  string
		host    string
		wantErr string
	}{
		{"listen 缺端口", "0.0.0.0", "", "callback.listen"},
		{"listen 乱写", "not-an-addr", "", "callback.listen"},
		{"host 带端口", "", "127.0.0.1:7863", "callback.host"},
		{"host 带路径", "", "example.com/cb", "callback.host"},
		{"合法（默认）", "127.0.0.1:0", "127.0.0.1", ""},
		{"合法（容器）", "0.0.0.0:43903", "127.0.0.1", ""},
		{"合法（IPv6 主机）", "0.0.0.0:43903", "::1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WILDWORK_CALLBACK_LISTEN", tc.listen)
			t.Setenv("WILDWORK_CALLBACK_HOST", tc.host)
			_, err := Load("")
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("意外报错: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v，应包含 %q", err, tc.wantErr)
			}
		})
	}
}
