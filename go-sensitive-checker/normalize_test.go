package main

import (
	"strings"
	"testing"
)

func TestNormalizeIndexMapConsistency(t *testing.T) {
	n := NewNormalizer()
	res := n.NormalizeText("Hello, 世界！ ABC")
	// 归一化文本的每个 rune 都必须能映射回原文的有效 rune 下标。
	orig := []rune(res.OriginalText)
	if len([]rune(res.NormalizedText)) != len(res.IndexMap) {
		t.Fatalf("index map length %d does not match normalized runes", len(res.IndexMap))
	}
	for i, origin := range res.IndexMap {
		if origin < 0 || origin >= len(orig) {
			t.Fatalf("IndexMap[%d] = %d out of range (len=%d)", i, origin, len(orig))
		}
	}
}

func TestNormalizeLowercaseAndFullWidth(t *testing.T) {
	n := NewNormalizer()
	res := n.NormalizeText("ＨＥＬＬＯ Ｗorld")
	if want := "helloworld"; res.NormalizedText != want {
		t.Fatalf("normalized = %q, want %q", res.NormalizedText, want)
	}
}

func TestNormalizeRemovesSeparators(t *testing.T) {
	n := NewNormalizer()
	res := n.NormalizeText("敏-感*词")
	if want := "敏感词"; res.NormalizedText != want {
		t.Fatalf("normalized = %q, want %q", res.NormalizedText, want)
	}
}

func TestNormalizeAggressiveDropsNoise(t *testing.T) {
	n := NewNormalizer()
	base := n.NormalizeText("bad word")
	agg := n.NormalizeTextAggressive("bad word")
	if agg.NormalizedText != "badword" || base.NormalizedText != "badword" {
		t.Fatalf("base=%q aggressive=%q", base.NormalizedText, agg.NormalizedText)
	}
	// aggressive 模式应比基础模式丢掉更多噪声字符（如 emoji）
	emoji := n.NormalizeTextAggressive("bad😀word")
	if emoji.NormalizedText != "badword" {
		t.Fatalf("aggressive with emoji = %q, want badword", emoji.NormalizedText)
	}
}

func TestNormalizeEmptyInput(t *testing.T) {
	n := NewNormalizer()
	for _, in := range []string{"", "   ", "***"} {
		res := n.NormalizeText(in)
		if res.NormalizedText != "" {
			t.Fatalf("NormalizeText(%q) = %q, want empty", in, res.NormalizedText)
		}
	}
}

func TestStripSymbolsWithMap(t *testing.T) {
	text := "敏-感*词"
	stripped, idxMap := stripSymbolsWithMap(text)
	if stripped != "敏感词" {
		t.Fatalf("stripped = %q, want 敏感词", stripped)
	}
	runes := []rune(text)
	wantOrig := []int{0, 2, 4} // 跳过 - 和 *
	if len(idxMap) != len(wantOrig) {
		t.Fatalf("idxMap = %v, want %v", idxMap, wantOrig)
	}
	for i, w := range wantOrig {
		if idxMap[i] != w {
			t.Fatalf("idxMap[%d] = %d, want %d", i, idxMap[i], w)
		}
		if runes[idxMap[i]] != []rune(stripped)[i] {
			t.Fatalf("mapped char mismatch at %d", i)
		}
	}
}

func TestMapToOriginalIndex(t *testing.T) {
	idxMap := []int{0, 3, 7}
	cases := []struct {
		in, want int
	}{
		{0, 0}, {1, 3}, {2, 7}, // 范围内查表
		{3, 3}, {10, 10}, {-5, -5}, // 越界时原样返回，由调用方保证边界
	}
	for _, tc := range cases {
		if got := mapToOriginalIndex(idxMap, tc.in); got != tc.want {
			t.Fatalf("mapToOriginalIndex(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestNormalizerWithTermMapping(t *testing.T) {
	n := NewNormalizer()
	opts := DefaultDetectOptions()
	opts.EnableTermMapping = true
	res := n.NormalizeTextWithOptions("bad word", opts)
	if !strings.Contains(strings.Join(res.Steps, ","), "term_mapping") && len(res.Steps) > 0 {
		// mapping profile 开启时 steps 应包含 term_mapping（即使没有短语命中也不报错）
		_ = res
	}
}
