package qodercn

import (
	"net/http"
	"testing"
	"time"
)

// TestStreamHTTPHasResponseHeaderTimeout StreamHTTP 无总超时（R35）后，Transport
// 必须给「等响应头」兜底，否则上游连响应头都不回时会无限挂（R58 已知限制的缺口）。
// 该字段只约束等头阶段、不碰读 body，故与 R35 不冲突。
func TestStreamHTTPHasResponseHeaderTimeout(t *testing.T) {
	c := New()
	tr, ok := c.StreamHTTP.Transport.(*http.Transport)
	if !ok {
		t.Fatal("StreamHTTP.Transport 应为 *http.Transport")
	}
	if tr.ResponseHeaderTimeout <= 0 {
		t.Fatal("StreamHTTP 的 Transport 未设 ResponseHeaderTimeout —— 无总超时又无等头兜底会无限挂")
	}
	// 下限 180s：不得比非流式默认总超时更激进（qoder 上游有排队，太短会误伤）
	if tr.ResponseHeaderTimeout < 180*time.Second {
		t.Fatalf("ResponseHeaderTimeout=%s 应 ≥180s（排队场景下限）", tr.ResponseHeaderTimeout)
	}
	// 与非流式总超时同源：默认 New() 两者都由 180s 派生
	if c.HTTP.Timeout > 0 && tr.ResponseHeaderTimeout < c.HTTP.Timeout {
		t.Fatalf("等头上限 %s 不应小于非流式总超时 %s", tr.ResponseHeaderTimeout, c.HTTP.Timeout)
	}
}

// TestHeaderTimeoutGrowsWithConfig 配置调大时等头上限跟随放宽（只放大不压低，raccoon R118 同款）。
func TestHeaderTimeoutGrowsWithConfig(t *testing.T) {
	c := NewWithTimeout(600 * time.Second)
	tr := c.StreamHTTP.Transport.(*http.Transport)
	if tr.ResponseHeaderTimeout != 600*time.Second {
		t.Fatalf("注入 600s 时等头上限应跟随为 600s，实际 %s", tr.ResponseHeaderTimeout)
	}
	// 注入更小值时不得压低到 180s 下限以下
	c2 := NewWithTimeout(30 * time.Second)
	tr2 := c2.StreamHTTP.Transport.(*http.Transport)
	if tr2.ResponseHeaderTimeout != 180*time.Second {
		t.Fatalf("注入 30s 时等头上限应被下限钳到 180s，实际 %s", tr2.ResponseHeaderTimeout)
	}
}
