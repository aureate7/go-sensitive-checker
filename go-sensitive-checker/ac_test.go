package main

import (
	"reflect"
	"testing"
)

func TestACBasicMatch(t *testing.T) {
	ac := NewAC()
	ac.Build([]string{"敏感词", "暴力"})
	cases := []struct {
		text string
		want []string
	}{
		{"这是一段敏感词内容", []string{"敏感词"}},
		{"没有命中", nil},
		{"", nil},
	}
	for _, tc := range cases {
		got := ac.Search(tc.text)
		var words []string
		for _, m := range got {
			words = append(words, m.Word)
		}
		if tc.want == nil && len(words) != 0 {
			t.Fatalf("Search(%q) = %v, want none", tc.text, words)
		}
		if len(words) != len(tc.want) {
			t.Fatalf("Search(%q) = %v, want %v", tc.text, words, tc.want)
		}
		for i := range words {
			if words[i] != tc.want[i] {
				t.Fatalf("Search(%q)[%d] = %q, want %q", tc.text, i, words[i], tc.want[i])
			}
		}
	}
}

func TestACOverlapAndFailLinks(t *testing.T) {
	// "he" 与 "she" 共享后缀，验证 fail 指针链能同时输出两个词。
	ac := NewAC()
	ac.Build([]string{"he", "she", "his", "hers"})
	got := ac.Search("ushers")
	var found []string
	for _, m := range got {
		found = append(found, m.Word)
	}
	want := []string{"she", "he", "hers"}
	if !reflect.DeepEqual(found, want) {
		t.Fatalf("Search(ushers) = %v, want %v", found, want)
	}
}

func TestACPositionsAreRuneBased(t *testing.T) {
	ac := NewAC()
	ac.Build([]string{"敏感"})
	text := "中文敏感词" // 敏 at rune index 2, end exclusive 4
	matches := ac.Search(text)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	m := matches[0]
	if m.Start != 2 || m.End != 4 {
		t.Fatalf("got [%d,%d), want [2,4)", m.Start, m.End)
	}
	runes := []rune(text)
	if string(runes[m.Start:m.End]) != "敏感" {
		t.Fatalf("slice %q does not equal target word", string(runes[m.Start:m.End]))
	}
}

func TestACMultibyteNoFalsePositive(t *testing.T) {
	ac := NewAC()
	ac.Build([]string{"abc"})
	// emoji 及多字节字符不应导致错误命中
	if matches := ac.Search("\U0001F600\U0001F601abc好"); len(matches) != 1 || matches[0].Word != "abc" {
		t.Fatalf("unexpected matches: %+v", matches)
	}
	if matches := ac.Search("ＡＢＣ"); len(matches) != 0 {
		t.Fatalf("fullwidth ABC should not match without normalization: %+v", matches)
	}
}

func TestACEmptyAndDuplicateWords(t *testing.T) {
	ac := NewAC()
	ac.Build([]string{"", "", "重复", "重复"})
	if matches := ac.Search("重复重复"); len(matches) != 2 {
		t.Fatalf("expected 2 overlapping matches, got %d", len(matches))
	}
}

func TestACMaskCoversMatches(t *testing.T) {
	ac := NewAC()
	ac.Build([]string{"bad"})
	masked, matches := ac.Mask("one bad two bad", '*')
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if want := "one *** two ***"; masked != want {
		t.Fatalf("masked = %q, want %q", masked, want)
	}
}
