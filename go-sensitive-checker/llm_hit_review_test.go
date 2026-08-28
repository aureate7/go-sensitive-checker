package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newMockLLMResponses 启动一个 OpenAI 兼容 mock 服务，按请求顺序返回预设内容。
func newMockLLMResponses(t *testing.T, contents []string, calls *atomic.Int64) *httptest.Server {
	t.Helper()
	var cursor atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		idx := int(cursor.Load())
		cursor.Store(int64(idx + 1))
		if idx >= len(contents) {
			idx = len(contents) - 1
		}
		body := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": contents[idx]}},
			},
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newHitTestReviewer(t *testing.T, srvURL string, daily int) *hitReviewer {
	t.Helper()
	client := &llmAssistClient{
		baseURL:      srvURL,
		apiKey:       "test-key",
		model:        "mock-model",
		maxTextRunes: 1200,
		httpClient:   &http.Client{Timeout: 5 * time.Second},
	}
	return newHitReviewer(client, daily, 20)
}

func sampleEvidences() []HitEvidence {
	return []HitEvidence{
		{Word: "扫描", Category: "violent_low", MatchType: "fuzzy", RiskLevel: "medium", Start: 2, End: 4},
		{Word: "高敏感词甲", Category: "political_high", MatchType: "exact_raw", RiskLevel: "high", Start: 6, End: 11},
		{Word: "笨蛋", Category: "abusive_low", MatchType: "exact_normalized", RiskLevel: "low", Start: 14, End: 16},
	}
}

func TestShouldReviewHitSkipsHighRisk(t *testing.T) {
	evidences := sampleEvidences()
	if shouldReviewHit(evidences[1]) {
		t.Fatal("high risk evidence should skip LLM review")
	}
	if !shouldReviewHit(evidences[0]) || !shouldReviewHit(evidences[2]) {
		t.Fatal("low/medium risk evidence should be reviewed")
	}
}

func TestExtractHitContextBounds(t *testing.T) {
	text := strings.Repeat("甲", 100) + "敏感词" + strings.Repeat("乙", 100)
	ctxText := extractHitContext(text, 100, 103)
	runes := []rune(ctxText)
	if len(runes) != 60+3+60 {
		t.Fatalf("context length = %d, want 123", len(runes))
	}
	short := extractHitContext("敏感词", 0, 3)
	if short != "敏感词" {
		t.Fatalf("short context = %q", short)
	}
}

func TestReviewHitsMockIntegration(t *testing.T) {
	var calls atomic.Int64
	response := `{"reviews":[{"index":0,"verdict":"demote","reason":"新闻转述语境","confidence":0.9},{"index":1,"verdict":"confirm","reason":"辱骂含义明确","confidence":0.95}]}`
	srv := newMockLLMResponses(t, []string{response}, &calls)
	reviewer := newHitTestReviewer(t, srv.URL, 100)

	evidences := sampleEvidences()
	// 移除 high 风险条目以匹配选样规则
	evidences = []HitEvidence{evidences[0], evidences[2]}
	reviews, err := reviewer.ReviewHits(context.Background(), "这是一段扫描和笨蛋混合的文本", evidences)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 LLM call, got %d", calls.Load())
	}
	if len(reviews) != 2 {
		t.Fatalf("expected 2 reviews, got %+v", reviews)
	}
	if reviews[0].Verdict != hitVerdictDemote || reviews[0].Index != 0 {
		t.Fatalf("unexpected first review: %+v", reviews[0])
	}

	// 缓存命中：同样输入不再调用 LLM
	before := calls.Load()
	if _, err := reviewer.ReviewHits(context.Background(), "这是一段扫描和笨蛋混合的文本", evidences); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Fatalf("cache should avoid extra calls: %d -> %d", before, calls.Load())
	}
}

func TestReviewHitsQuotaExhausted(t *testing.T) {
	var calls atomic.Int64
	srv := newMockLLMResponses(t, []string{`{"reviews":[]}`}, &calls)
	reviewer := newHitTestReviewer(t, srv.URL, 1) // 每日仅 1 次
	evidences := []HitEvidence{
		{Word: "扫描", Category: "violent_low", MatchType: "fuzzy", RiskLevel: "medium", Start: 0, End: 2},
	}
	// 不同上下文绕过缓存，第二次应触发熔断
	if _, err := reviewer.ReviewHits(context.Background(), "文本A扫描", evidences); err != nil {
		t.Fatalf("first call should pass: %v", err)
	}
	if _, err := reviewer.ReviewHits(context.Background(), "文本B扫描", evidences); err == nil {
		t.Fatal("second call should hit quota breaker")
	}
}

func TestNormalizeHitVerdict(t *testing.T) {
	cases := map[string]string{
		"confirm": hitVerdictConfirm,
		" DEMOTE": hitVerdictDemote,
		"review":  hitVerdictReview,
		"bogus":   hitVerdictReview,
	}
	for in, want := range cases {
		if got := normalizeHitVerdict(in); got != want {
			t.Fatalf("normalizeHitVerdict(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHitReviewCacheEviction(t *testing.T) {
	cache := newHitReviewCache(2)
	for i := 0; i < 3; i++ {
		cache.put(hitReviewCacheKey("w", "c", "m", string(rune('a'+i))), HitLLMReview{Word: string(rune('a' + i))})
	}
	if _, ok := cache.get(hitReviewCacheKey("w", "c", "m", "a")); ok {
		t.Fatal("oldest entry should be evicted")
	}
	if _, ok := cache.get(hitReviewCacheKey("w", "c", "m", "c")); !ok {
		t.Fatal("newest entry should remain")
	}
}
