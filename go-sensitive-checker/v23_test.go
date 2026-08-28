package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLatinWordBoundary(t *testing.T) {
	cases := []struct {
		text, word string
		start, end int
		want       bool
	}{
		{"sex", "sex", 0, 3, true},
		{"say sex now", "sex", 4, 7, true},
		{"Sussex", "sex", 3, 6, false},
		{"sexology", "sex", 0, 3, false},
		{"敏感词测试", "敏感词", 0, 3, true},
	}
	for _, tc := range cases {
		if got := hasLatinWordBoundary(tc.text, tc.word, tc.start, tc.end); got != tc.want {
			t.Fatalf("boundary(%q,%q)=%v want %v", tc.text, tc.word, got, tc.want)
		}
	}
}

func TestGeneratedVariantCatalogAndLoad(t *testing.T) {
	base := t.TempDir()
	writeWordFile(t, base, "拉人广告敏感词/高敏感词.txt", "网枪\n")
	cfg := DefaultDetectorConfig(base)
	cfg.EnableLLMAssist = false
	cfg.VariantAliasPath = filepath.Join(base, "missing.json")
	d := NewDetectorWithConfig(base, cfg)
	catalog := generateVariants(d.sensitiveWords, d.pinyinMatcher)
	var found bool
	for _, v := range catalog.Variants {
		if v.Word == "网枪" && v.Alias == "網枪" && v.Type == "visual" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected visual variant")
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "generated.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	d.loadGeneratedVariants(path)
	resp := d.DetectWithOptions("網枪", []string{AdvertisingHigh}, &DetectOptions{FuzzyMatch: true})
	if !resp.HasSensitive {
		t.Fatal("generated alias was not detected")
	}
}

func TestContextRulesAdjustScoreAndAreValidated(t *testing.T) {
	policy := DetectionPolicy{
		ID: "context", Name: "context", Categories: []string{AdvertisingHigh},
		Options: DefaultDetectOptions(), MaxTextRunes: 1000, Enabled: true,
		ReviewThreshold: 25, BlockThreshold: 70,
		ContextRules: []ContextRule{{ID: "reporting", Name: "reporting context", Phrases: []string{"新闻报道"}, Window: 10, ScoreDelta: -20}},
	}
	if err := validatePolicy(policy); err != nil {
		t.Fatal(err)
	}
	resp := DetectResponse{RiskOccurrence: map[string]int{"high": 1}, HitEvidences: []HitEvidence{{Word: "测试", Category: AdvertisingHigh, Start: 5, End: 7}}}
	resp.ContextRuleHits = matchContextRules("新闻报道：测试", resp.HitEvidences, policy.ContextRules)
	applyRiskScore(&resp, policy)
	if len(resp.ContextRuleHits) != 1 {
		t.Fatalf("hits=%d", len(resp.ContextRuleHits))
	}
	if resp.RiskScore != 10 || resp.RecommendedAction != "mask" {
		t.Fatalf("score=%d action=%s", resp.RiskScore, resp.RecommendedAction)
	}

	policy.ContextRules[0].ScoreDelta = 0
	if err := validatePolicy(policy); err == nil {
		t.Fatal("expected zero delta validation failure")
	}
}

func writeWordFile(t *testing.T, base, relative, content string) {
	t.Helper()
	path := filepath.Join(base, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
