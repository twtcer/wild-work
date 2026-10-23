package server

import (
	"testing"
	"time"

	"wild-work/internal/auth"
	"wild-work/internal/pool"
	"wild-work/internal/provider"
)

// newStickyTestHandler 两账号（A 余额高、B 余额低）的 GLM 渠道，无上游调用。
func newStickyTestHandler(t *testing.T) *Handler {
	t.Helper()
	p := pool.New(t.TempDir() + "/state.json")
	p.Add(&auth.Auth{Kind: "glm", AccessToken: "at-A", ExpiresAt: 4102444800, UID: "A"})
	p.Add(&auth.Auth{Kind: "glm", AccessToken: "at-B", ExpiresAt: 4102444800, UID: "B"})
	p.SetCreditDetail("A", 1000, 0, 0, time.Time{})
	p.SetCreditDetail("B", 500, 0, 0, time.Time{})
	return NewHandler(Config{
		Runtimes: map[provider.Kind]*Runtime{
			provider.GLM: {Kind: provider.GLM, Pool: p, Upstream: &kindedUpstream{},
				StaticModels: []provider.ModelInfo{{ID: "m1"}, {ID: "m2"}}},
		},
		HardCooldown: time.Hour, SoftCooldown: time.Minute,
		ErrThreshold: 3, ErrCooldown: time.Minute,
	})
}

// TestStickyKeyIsPerModel 粘性记录的 key 必须含模型名（2026-10-23 升维）：
// 同一渠道不同模型各自独立记账，否则 A 在模型 X 上的粘性会串到模型 Y。
func TestStickyKeyIsPerModel(t *testing.T) {
	h := NewHandler(Config{Runtimes: map[provider.Kind]*Runtime{}})

	k1 := h.stickyKey(provider.GLM, "glm-5.2")
	k2 := h.stickyKey(provider.GLM, "glm-5.3")
	if k1 == k2 {
		t.Fatalf("不同模型必须得到不同的粘性 key，实际都是 %q", k1)
	}
	if k1 != "glm/glm-5.2" {
		t.Fatalf("key 形态应为 <kind>/<model>，实际 %q", k1)
	}
	// model 为空 ⇒ 退化为渠道级（GLM images 等无模型的路径）
	if got := h.stickyKey(provider.GLM, ""); got != "glm" {
		t.Fatalf("空 model 应退化为渠道级 key，实际 %q", got)
	}
}

// TestStickyIsolatedBetweenModels 模型级冷却后，另一模型不得被牵连——
// 这是升维的核心收益：A 在 m1 饱和冷却，请求 m2 时仍可粘在 A（不必换号）。
func TestStickyIsolatedBetweenModels(t *testing.T) {
	h := newStickyTestHandler(t)
	rt := h.cfg.Runtimes[provider.GLM]

	// m1 首挑：A 余额高，选中 A 并建立 m1 的粘性记录
	if acct := h.pickWithSticky(rt, "m1"); acct == nil || acct.UID != "A" {
		t.Fatalf("m1 首挑应为 A，实际 %v", acct)
	}
	// m2 首挑：独立记录，同样是 A（余额高）
	if acct := h.pickWithSticky(rt, "m2"); acct == nil || acct.UID != "A" {
		t.Fatalf("m2 首挑应为 A，实际 %v", acct)
	}

	// A 在 m1 上模型级冷却
	rt.Pool.CooldownModel("A", "m1", time.Minute, "test")

	// m1 再挑：A 被 modelCooled 拦住 ⇒ 落到 B
	if acct := h.pickWithSticky(rt, "m1"); acct == nil || acct.UID != "B" {
		t.Fatalf("m1 在 A 冷却后应换到 B，实际 %v", acct)
	}
	// m2 再挑：m1 的冷却不影响 m2 ⇒ 仍应粘在 A
	// （若粘性 key 仍是渠道级，上一步选 B 已覆写记录，这里就会错误地返回 B）
	if acct := h.pickWithSticky(rt, "m2"); acct == nil || acct.UID != "A" {
		t.Fatalf("❌ m2 被 m1 的模型级冷却牵连（粘性仍未按模型隔离）：实际 %v", acct)
	}
}

// TestStickyClearByModel 按模型清理只影响该模型；空 model 清理整个渠道。
func TestStickyClearByModel(t *testing.T) {
	h := newStickyTestHandler(t)
	rt := h.cfg.Runtimes[provider.GLM]

	h.pickWithSticky(rt, "m1")
	h.pickWithSticky(rt, "m2")
	if len(h.sticky) != 2 {
		t.Fatalf("应有两份粘性记录，实际 %d", len(h.sticky))
	}

	h.stickyClear(rt, "m1")
	if _, ok := h.sticky[h.stickyKey(provider.GLM, "m1")]; ok {
		t.Fatal("m1 的粘性记录应被清除")
	}
	if _, ok := h.sticky[h.stickyKey(provider.GLM, "m2")]; !ok {
		t.Fatal("m2 的粘性记录不应被 m1 的清理波及")
	}

	h.stickyClear(rt, "") // 渠道级：清空该渠道下所有模型
	if len(h.sticky) != 0 {
		t.Fatalf("空 model 应清空整个渠道的粘性记录，剩余 %d 条", len(h.sticky))
	}
}

// TestStickySnapshotAggregatesByChannel 快照仍按渠道聚合（面板「使用中」列语义不变），
// 多模型各持记录时取成功计数最大的那条。
func TestStickySnapshotAggregatesByChannel(t *testing.T) {
	h := newStickyTestHandler(t)
	rt := h.cfg.Runtimes[provider.GLM]

	h.pickWithSticky(rt, "m1") // A
	h.pickWithSticky(rt, "m2") // A
	// 让 m2 的记录计数更高
	h.stickyMu.Lock()
	h.sticky[h.stickyKey(provider.GLM, "m2")].reqCount = 7
	h.stickyMu.Unlock()

	snap := h.StickySnapshot()
	if len(snap) != 1 {
		t.Fatalf("快照应按渠道聚合为 1 条，实际 %d 条：%v", len(snap), snap)
	}
	st, ok := snap["glm"]
	if !ok {
		t.Fatalf("快照 key 应为渠道名 glm，实际 %v", snap)
	}
	if st.UID != "A" || st.Count != 7 {
		t.Fatalf("应取计数最大的记录（A/7），实际 %+v", st)
	}
}
