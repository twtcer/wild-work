package monkeycode

import "testing"

// TestToolResultKeepsImage 工具结果里的图片必须转成 Anthropic image 块带过去，
// 不能只拼文本——否则 view_image 这类工具的结果到上游是「空结果」，模型看不到图。
// 对照：zcode / minimax 的 tool_result 早已保留图片（同款 imageBlockOf），
// 本渠道此前遗漏，是把「消息体里的图能过、工具结果里的图被丢」这个不对称补齐。
func TestToolResultKeepsImage(t *testing.T) {
	m := map[string]any{
		"role":         "tool",
		"tool_call_id": "call_1",
		"content": []any{
			map[string]any{"type": "text", "text": "截图如下"},
			map[string]any{"type": "image_url", "image_url": map[string]any{
				"url": "data:image/png;base64,iVBORw0KGgo=",
			}},
		},
	}
	out := toolResultBlocks(m)
	if len(out) != 1 {
		t.Fatalf("应产出单个 tool_result，实际 %d", len(out))
	}
	blk, _ := out[0].(map[string]any)
	if blk["type"] != "tool_result" || blk["tool_use_id"] != "call_1" {
		t.Fatalf("tool_result 骨架不符：%v", blk)
	}
	arr, ok := blk["content"].([]any)
	if !ok {
		t.Fatalf("含图时 content 应为块数组（Anthropic 允许 text+image 混合），实际 %T", blk["content"])
	}
	var sawText, sawImage bool
	for _, b := range arr {
		bm, _ := b.(map[string]any)
		switch bm["type"] {
		case "text":
			sawText = true
		case "image":
			sawImage = true
			src, _ := bm["source"].(map[string]any)
			if src["type"] != "base64" || src["media_type"] != "image/png" {
				t.Fatalf("image 块 source 不符：%v", src)
			}
		}
	}
	if !sawText || !sawImage {
		t.Fatalf("应同时含 text 与 image 块（text=%v image=%v）：%v", sawText, sawImage, arr)
	}
}

// TestToolResultTextOnlyUnchanged 无图片时 content 必须仍是 string——
// 与原行为逐字一致，不给上游多余结构（避免改变既有正常请求的形状）。
func TestToolResultTextOnlyUnchanged(t *testing.T) {
	out := toolResultBlocks(map[string]any{
		"role": "tool", "tool_call_id": "c1", "content": "纯文本结果",
	})
	blk, _ := out[0].(map[string]any)
	if s, ok := blk["content"].(string); !ok || s != "纯文本结果" {
		t.Fatalf("无图时 content 应为原字符串，实际 %T %v", blk["content"], blk["content"])
	}
}

// TestToolResultImageOnlyGetsPlaceholder 纯图片结果要补文本占位，
// 否则部分上游把「只有图片」判成空 content 直接 400。
func TestToolResultImageOnlyGetsPlaceholder(t *testing.T) {
	out := toolResultBlocks(map[string]any{
		"role": "tool", "tool_call_id": "c2",
		"content": []any{map[string]any{"type": "image_url", "image_url": "https://example.com/a.png"}},
	})
	blk0, _ := out[0].(map[string]any)
	arr, ok := blk0["content"].([]any)
	if !ok {
		t.Fatalf("应产出块数组")
	}
	if len(arr) < 2 {
		t.Fatalf("纯图片结果应有占位文本 + 图片，实际 %d 块", len(arr))
	}
	first, _ := arr[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "[image]" {
		t.Fatalf("首块应为 [image] 占位文本，实际 %v", first)
	}
}

// TestToolResultDropsUnusableImage 无法解析的图片（非 data:/http(s)）静默丢弃，
// 但文本与 tool_result 骨架必须照常在（宁缺勿错，不为一张坏图整块失败）。
func TestToolResultDropsUnusableImage(t *testing.T) {
	out := toolResultBlocks(map[string]any{
		"role": "tool", "tool_call_id": "c3",
		"content": []any{
			map[string]any{"type": "text", "text": "有文本"},
			map[string]any{"type": "image_url", "image_url": "ftp://bad/x.png"},
		},
	})
	blk, _ := out[0].(map[string]any)
	if s, ok := blk["content"].(string); !ok || s != "有文本" {
		t.Fatalf("坏图应被丢弃、退回纯文本 string，实际 %T %v", blk["content"], blk["content"])
	}
}
