package traework

import (
	"encoding/json"
	"strings"
	"testing"
)

// sanitizeBody 造一个 system 里带内容的请求体，prefix 作为 system 文本。
func sanitizeBody(systemText string) []byte {
	return []byte(`{"model":"kimi-k2","messages":[{"role":"system","content":"` + systemText + `"},{"role":"user","content":"hi"}]}`)
}

// systemText 取改写后 system 消息的 text（content 已被 PrepareBody 改成 block 数组）。
func systemText(t *testing.T, out []byte) string {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, out)
	}
	msgs, _ := obj["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatalf("无 messages: %s", out)
	}
	m0, _ := msgs[0].(map[string]any)
	blocks, _ := m0["content"].([]any)
	if len(blocks) == 0 {
		t.Fatalf("system content 为空块: %s", out)
	}
	b, _ := blocks[0].(map[string]any)
	s, _ := b["text"].(string)
	return s
}

// TestPrepareBodyStripsFingerprint 脱敏必须挂在 PrepareBody 链路上（与 workbuddyai 同款，wb_utils.go）。
//
// 这是 R34 家族「声明了却没接线」的回归：PrepareBody 末尾若漏调 sanitize.Messages，
// 本测试即失败——否则删掉脱敏调用也不报任何错，直到上游按逐字指纹拦截 11128 才暴露。
//
// 脱敏分两类处置，断言须分别对待（不变量 16：只做文本替换不改语义）：
//   - 换词改写（identity 句 / 11128）：插入冠词或缩写，破坏逐字匹配、保留可读性 ⇒ 断言新词出现；
//   - 整段剥离（header 键值段）：从键名剥到 ; 或行尾 ⇒ 断言键名消失、后续文本保留。
func TestPrepareBodyStripsFingerprint(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		asserts func(t *testing.T, got string)
	}{
		{
			// DSH 身份句：上游按 system 开头逐字锚定，插冠词 the 绕过（实测 200）
			"DSH 身份句插冠词",
			"You are an AI agent powered by DeepSeek Harness running",
			func(t *testing.T, got string) {
				if strings.Contains(got, "powered by DeepSeek Harness running") {
					t.Fatalf("DSH 逐字串未被改写（脱敏未接线？）: %q", got)
				}
				if !strings.Contains(got, "the DeepSeek Harness") {
					t.Fatalf("DSH 句应插冠词 the 绕过逐字锚定: %q", got)
				}
			},
		},
		{
			// Claude Code 身份句：换词 CLI → CLI tool，前缀保留（断言新词而非原文消失）
			"Claude Code 身份句换词",
			"You are Claude Code, Anthropic's official CLI for Claude",
			func(t *testing.T, got string) {
				if !strings.Contains(got, "official CLI tool for Claude") {
					t.Fatalf("Claude Code 句未换词为 CLI tool: %q", got)
				}
				if strings.Contains(got, "official CLI for Claude") {
					t.Fatalf("原文 CLI for Claude 应已被换词: %q", got)
				}
			},
		},
		{
			// billing header 键值段：整段剥离（; 终止，后续文本应保留）
			"billing header 键值段剥离",
			"x-anthropic-billing-header: abc; keep-this",
			func(t *testing.T, got string) {
				if strings.Contains(got, "x-anthropic-billing-header") {
					t.Fatalf("billing header 键名未被剥离: %q", got)
				}
				if !strings.Contains(got, "keep-this") {
					t.Fatalf("header 段之后的正常文本被误伤: %q", got)
				}
			},
		},
		{
			// 裸 11128：插连字符破坏整单拦截，保留可读性
			"裸 11128 插连字符",
			"upstream rejected with code 11128 here",
			func(t *testing.T, got string) {
				if strings.Contains(got, "11128") {
					t.Fatalf("裸 11128 未被改写（整单拦截仍在）: %q", got)
				}
				if !strings.Contains(got, "11-128") {
					t.Fatalf("11128 应改写为 11-128: %q", got)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := PrepareBody(sanitizeBody(c.in), "solo_work_lite")
			c.asserts(t, systemText(t, out))
		})
	}
}

// TestPrepareBodyNoFingerprintZeroChange 无指纹请求走零分配快速路径：脱敏不引入任何改写。
// 锁死不变量 16（脱敏只做文本替换、不改语义）与「预检不命中零分配」——非指纹文本逐字保留。
func TestPrepareBodyNoFingerprintZeroChange(t *testing.T) {
	out := PrepareBody(sanitizeBody("普通中文提问，无任何模板句"), "solo_work_lite")
	if got := systemText(t, out); got != "普通中文提问，无任何模板句" {
		t.Fatalf("非指纹文本被改动（预检应零分配原样返回）: %q", got)
	}
}

// TestPrepareBodySanitizeKeepsStructure 脱敏只碰文本，不得破坏 traework 的改写产物
// （stream/role 归一/config_name/function 等字段不受影响）。
func TestPrepareBodySanitizeKeepsStructure(t *testing.T) {
	out := PrepareBody(sanitizeBody("You are Claude Code, Anthropic's official CLI for Claude"), "solo_agent")
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	if obj["stream"] != true {
		t.Fatalf("stream 未被置 true: %v", obj["stream"])
	}
	if obj["function"] != "solo_agent" {
		t.Fatalf("function 未透传: %v", obj["function"])
	}
	if obj["config_name"] != "kimi-k2" {
		t.Fatalf("config_name 未取 model: %v", obj["config_name"])
	}
}
