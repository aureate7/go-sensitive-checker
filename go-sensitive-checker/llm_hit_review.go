package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// 逐命中 LLM 语境复核的三级结论。
const (
	hitVerdictConfirm = "confirm" // 维持命中
	hitVerdictDemote  = "demote"  // 疑似误报（语境为正常引用/无风险）
	hitVerdictReview  = "review"  // 不确定，建议人工复核
)

const hitReviewSystemPrompt = `你是内容安全审核助手。规则引擎在文本中命中了敏感词，请结合上下文逐条判断该命中是否为真实违规。
只输出 JSON 对象，格式如下，不要输出多余说明：
{
  "reviews": [
    {"index": 0, "verdict": "confirm|demote|review", "reason": "一句简洁中文说明", "confidence": 0到1之间的小数}
  ]
}
判定标准：
1) confirm：该词在语境中确实构成违规或风险含义。
2) demote：命中是误报，例如新闻转述、学术讨论、正常词形巧合（如"扫描"命中"淫"类规则以外的误报）。
3) review：语境不足或边界模糊，建议人工复核。
4) index 必须与输入条目的 index 一致，不得遗漏。`

// HitLLMReview 是单条命中的 LLM 复核结论。
type HitLLMReview struct {
	Index      int     `json:"index"`
	Word       string  `json:"word"`
	Category   string  `json:"category"`
	MatchType  string  `json:"match_type"`
	Verdict    string  `json:"verdict"`
	Reason     string  `json:"reason,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type hitReviewInput struct {
	Index     int    `json:"index"`
	Word      string `json:"word"`
	Category  string `json:"category"`
	MatchType string `json:"match_type"`
	Context   string `json:"context"`
}

type hitVerdict struct {
	Index      int     `json:"index"`
	Verdict    string  `json:"verdict"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

type hitReviewOutput struct {
	Reviews []hitVerdict `json:"reviews"`
}

// hitReviewCache 是"上下文+词+类别"维度的结论缓存。
type hitReviewCache struct {
	mu      sync.Mutex
	entries map[string]HitLLMReview
	order   []string // FIFO 淘汰
	max     int
}

func newHitReviewCache(max int) *hitReviewCache {
	if max <= 0 {
		max = 4096
	}
	return &hitReviewCache{entries: map[string]HitLLMReview{}, max: max}
}

func hitReviewCacheKey(word, category, matchType, context string) string {
	sum := sha256.Sum256([]byte(category + "\x00" + word + "\x00" + matchType + "\x00" + context))
	return hex.EncodeToString(sum[:])
}

func (c *hitReviewCache) get(key string) (HitLLMReview, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok
}

func (c *hitReviewCache) put(key string, review HitLLMReview) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists {
		c.order = append(c.order, key)
		for len(c.order) > c.max {
			delete(c.entries, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.entries[key] = review
}

// hitReviewer 逐命中复核器：选样 → 截上下文 → 查缓存 → 批量请求 → 解析结论。
type hitReviewer struct {
	client         *llmAssistClient
	cache          *hitReviewCache
	maxHitsPerCall int
	dailyLimit     int
	mu             sync.Mutex
	quotaDay       string
	quotaUsed      int
}

func newHitReviewer(client *llmAssistClient, dailyLimit, maxHitsPerCall int) *hitReviewer {
	if dailyLimit <= 0 {
		dailyLimit = 1000
	}
	if maxHitsPerCall <= 0 || maxHitsPerCall > 20 {
		maxHitsPerCall = 20
	}
	return &hitReviewer{
		client:         client,
		cache:          newHitReviewCache(4096),
		maxHitsPerCall: maxHitsPerCall,
		dailyLimit:     dailyLimit,
	}
}

// shouldReviewHit 决定哪些证据需要 LLM 复核：高风险类别交规则引擎，精确原文命中的高置信命中不再问。
func shouldReviewHit(ev HitEvidence) bool {
	if ev.RiskLevel == "high" {
		return false
	}
	return true
}

const hitReviewContextRunes = 60

func extractHitContext(text string, start, end int) string {
	runes := []rune(text)
	lo := start - hitReviewContextRunes
	if lo < 0 {
		lo = 0
	}
	hi := end + hitReviewContextRunes
	if hi > len(runes) {
		hi = len(runes)
	}
	if lo >= hi {
		return ""
	}
	return string(runes[lo:hi])
}

// consumeQuota 消耗一次调用配额；当日配额耗尽返回 false（熔断）。
func (h *hitReviewer) consumeQuota() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	day := time.Now().UTC().Format("2006-01-02")
	if h.quotaDay != day {
		h.quotaDay = day
		h.quotaUsed = 0
	}
	if h.quotaUsed >= h.dailyLimit {
		return false
	}
	h.quotaUsed++
	return true
}

// ReviewHits 对给定证据做逐命中复核，返回结论列表（不含缓存外未获得结论的条目）。
// 配额熔断或调用失败时返回 error，调用方降级为纯规则结果。
func (h *hitReviewer) ReviewHits(ctx context.Context, text string, evidences []HitEvidence) ([]HitLLMReview, error) {
	if h == nil || h.client == nil {
		return nil, fmt.Errorf("hit reviewer not initialized")
	}

	// 1. 选样 + 截上下文 + 查缓存
	var pending []hitReviewInput
	results := make(map[int]HitLLMReview)
	for i, ev := range evidences {
		if !shouldReviewHit(ev) {
			continue
		}
		contextText := extractHitContext(text, ev.Start, ev.End)
		if contextText == "" {
			continue
		}
		key := hitReviewCacheKey(ev.Word, ev.Category, ev.MatchType, contextText)
		if cached, ok := h.cache.get(key); ok {
			cached.Index = i
			results[i] = cached
			continue
		}
		pending = append(pending, hitReviewInput{
			Index:     i,
			Word:      ev.Word,
			Category:  ev.Category,
			MatchType: ev.MatchType,
			Context:   limitRunes(contextText, h.client.maxTextRunes),
		})
	}

	// 2. 按批请求
	for start := 0; start < len(pending); start += h.maxHitsPerCall {
		if !h.consumeQuota() {
			return nil, fmt.Errorf("llm hit review daily quota exhausted")
		}
		end := start + h.maxHitsPerCall
		if end > len(pending) {
			end = len(pending)
		}
		batch := pending[start:end]
		verdicts, err := h.callLLM(ctx, batch)
		if err != nil {
			return nil, err
		}
		for _, v := range verdicts {
			if v.Index < 0 || v.Index >= len(batch) {
				continue
			}
			in := batch[v.Index]
			review := HitLLMReview{
				Index:      in.Index,
				Word:       in.Word,
				Category:   in.Category,
				MatchType:  in.MatchType,
				Verdict:    normalizeHitVerdict(v.Verdict),
				Reason:     v.Reason,
				Confidence: v.Confidence,
			}
			results[review.Index] = review
			h.cache.put(hitReviewCacheKey(in.Word, in.Category, in.MatchType, in.Context), review)
		}
	}

	out := make([]HitLLMReview, 0, len(results))
	for i := range evidences {
		if r, ok := results[i]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func (h *hitReviewer) callLLM(ctx context.Context, batch []hitReviewInput) ([]hitVerdict, error) {
	payloadJSON, err := json.Marshal(map[string]any{"hits": batch})
	if err != nil {
		return nil, fmt.Errorf("marshal hit review payload failed: %w", err)
	}
	content, err := h.client.chat(ctx, hitReviewSystemPrompt,
		"请对以下命中列表逐条复核并按要求输出 JSON：\n"+string(payloadJSON))
	if err != nil {
		return nil, err
	}
	var output hitReviewOutput
	if err := json.Unmarshal([]byte(extractJSONObject(content)), &output); err != nil {
		return nil, fmt.Errorf("parse hit review content failed: %w", err)
	}
	return output.Reviews, nil
}

func normalizeHitVerdict(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case hitVerdictConfirm:
		return hitVerdictConfirm
	case hitVerdictDemote:
		return hitVerdictDemote
	default:
		return hitVerdictReview
	}
}
