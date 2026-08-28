package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	llmRiskSafe   = "safe"
	llmRiskLow    = "low"
	llmRiskMedium = "medium"
	llmRiskHigh   = "high"
)

const llmAssistSystemPrompt = `你是内容安全审核助手。请基于输入文本给出“辅助鉴别结论”，不要替代规则引擎。
必须只输出 JSON 对象，不要输出多余说明，字段如下：
{
  "risk_level": "safe|low|medium|high",
  "should_review": true/false,
  "reason": "一句简洁中文说明",
  "suspected_terms": ["可能需要人工复核的词或短语"],
  "confidence": 0 到 1 之间的小数
}
要求：
1) risk_level 必须是 safe/low/medium/high 之一。
2) suspected_terms 最多 8 项，避免重复。
3) 若文本明显无风险，risk_level= safe 且 should_review=false。`

type LLMAssistResult struct {
	Enabled        bool           `json:"enabled"`
	Used           bool           `json:"used"`
	Model          string         `json:"model,omitempty"`
	RiskLevel      string         `json:"risk_level,omitempty"`
	ShouldReview   bool           `json:"should_review"`
	Reason         string         `json:"reason,omitempty"`
	SuspectedTerms []string       `json:"suspected_terms,omitempty"`
	Confidence     float64        `json:"confidence,omitempty"`
	LatencyMS      int64          `json:"latency_ms,omitempty"`
	HitReviews     []HitLLMReview `json:"hit_reviews,omitempty"`
	Error          string         `json:"error,omitempty"`
}

type llmAssistClient struct {
	baseURL      string
	apiKey       string
	model        string
	maxTextRunes int
	httpClient   *http.Client
}

type llmAssistChatRequest struct {
	Model       string                 `json:"model"`
	Temperature float64                `json:"temperature"`
	Messages    []llmAssistChatMessage `json:"messages"`
}

type llmAssistChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llmAssistChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type llmAssistOutput struct {
	RiskLevel      string   `json:"risk_level"`
	ShouldReview   bool     `json:"should_review"`
	Reason         string   `json:"reason"`
	SuspectedTerms []string `json:"suspected_terms"`
	Confidence     float64  `json:"confidence"`
}

func newLLMAssistClient(cfg DetectorConfig) *llmAssistClient {
	timeout := time.Duration(cfg.LLMTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	maxRunes := cfg.LLMMaxTextRunes
	if maxRunes <= 0 {
		maxRunes = 1200
	}

	return &llmAssistClient{
		baseURL:      strings.TrimRight(strings.TrimSpace(cfg.LLMAPIBaseURL), "/"),
		apiKey:       strings.TrimSpace(cfg.LLMAPIKey),
		model:        strings.TrimSpace(cfg.LLMModel),
		maxTextRunes: maxRunes,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *llmAssistClient) Analyze(ctx context.Context, text string, categories []string, baseResp DetectResponse) (LLMAssistResult, error) {
	if c == nil {
		return LLMAssistResult{}, fmt.Errorf("llm assist client not initialized")
	}
	if c.baseURL == "" {
		return LLMAssistResult{}, fmt.Errorf("LLM base URL not configured")
	}
	if c.apiKey == "" {
		return LLMAssistResult{}, fmt.Errorf("LLM API key not configured")
	}
	if c.model == "" {
		return LLMAssistResult{}, fmt.Errorf("LLM model not configured")
	}

	startAt := time.Now()
	textForModel := limitRunes(strings.TrimSpace(text), c.maxTextRunes)
	payloadObj := map[string]any{
		"text":                textForModel,
		"selected_categories": categories,
		"rule_engine_summary": map[string]any{
			"has_sensitive":            baseResp.HasSensitive,
			"risk_level":               baseResp.RiskLevel,
			"total_hits":               baseResp.TotalCount,
			"total_occurrence_count":   baseResp.TotalOccurrenceCount,
			"top_evidence_suggestions": makeEvidenceSnapshot(baseResp.HitEvidences, 20),
		},
	}
	payloadJSON, err := json.Marshal(payloadObj)
	if err != nil {
		return LLMAssistResult{}, fmt.Errorf("marshal llm payload failed: %w", err)
	}

	content, err := c.chat(ctx, llmAssistSystemPrompt,
		"请对以下 JSON 输入做辅助鉴别并按要求输出 JSON：\n"+string(payloadJSON))
	if err != nil {
		return LLMAssistResult{}, err
	}
	if content == "" {
		return LLMAssistResult{}, fmt.Errorf("llm response content is empty")
	}

	var output llmAssistOutput
	if err := json.Unmarshal([]byte(extractJSONObject(content)), &output); err != nil {
		return LLMAssistResult{}, fmt.Errorf("parse llm content failed: %w", err)
	}

	output.RiskLevel = normalizeLLMRisk(output.RiskLevel)
	if output.Reason == "" {
		output.Reason = "大模型未提供详细说明。"
	}
	if output.RiskLevel != llmRiskSafe && !output.ShouldReview {
		output.ShouldReview = true
	}

	return LLMAssistResult{
		Enabled:        true,
		Used:           true,
		Model:          c.model,
		RiskLevel:      output.RiskLevel,
		ShouldReview:   output.ShouldReview,
		Reason:         strings.TrimSpace(output.Reason),
		SuspectedTerms: uniqueSortedTerms(output.SuspectedTerms, 8),
		Confidence:     clampConfidence(output.Confidence),
		LatencyMS:      time.Since(startAt).Milliseconds(),
	}, nil
}

// chat 执行一次 chat/completions 调用并返回模型文本内容，供全文辅助与逐命中复核共用。
func (c *llmAssistClient) chat(ctx context.Context, system, user string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("llm assist client not initialized")
	}
	if c.baseURL == "" {
		return "", fmt.Errorf("LLM base URL not configured")
	}
	if c.apiKey == "" {
		return "", fmt.Errorf("LLM API key not configured")
	}
	if c.model == "" {
		return "", fmt.Errorf("LLM model not configured")
	}

	reqBody := llmAssistChatRequest{
		Model:       c.model,
		Temperature: 0.1,
		Messages: []llmAssistChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}
	rawBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal llm request failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(rawBody))
	if err != nil {
		return "", fmt.Errorf("build llm request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read llm response failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm api returned status %d", resp.StatusCode)
	}

	var chatResp llmAssistChatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return "", fmt.Errorf("decode llm response failed: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("llm response contains no choices")
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("llm response content is empty")
	}
	return content, nil
}

func normalizeLLMRisk(level string) string {
	v := strings.ToLower(strings.TrimSpace(level))
	switch v {
	case llmRiskSafe, llmRiskLow, llmRiskMedium, llmRiskHigh:
		return v
	default:
		return llmRiskMedium
	}
}

func clampConfidence(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func extractJSONObject(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "{}"
	}
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}

	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}

func uniqueSortedTerms(terms []string, limit int) []string {
	if len(terms) == 0 {
		return nil
	}
	uniq := make(map[string]struct{}, len(terms))
	out := make([]string, 0, len(terms))
	for _, raw := range terms {
		term := strings.TrimSpace(raw)
		if term == "" {
			continue
		}
		if _, ok := uniq[term]; ok {
			continue
		}
		uniq[term] = struct{}{}
		out = append(out, term)
	}
	slices.Sort(out)
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func limitRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return text
	}
	rs := []rune(text)
	if len(rs) <= maxRunes {
		return text
	}
	return string(rs[:maxRunes])
}

func makeEvidenceSnapshot(evidences []HitEvidence, limit int) []map[string]any {
	if len(evidences) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	if len(evidences) > limit {
		evidences = evidences[:limit]
	}

	out := make([]map[string]any, 0, len(evidences))
	for _, ev := range evidences {
		out = append(out, map[string]any{
			"word":         ev.Word,
			"category":     ev.Category,
			"risk_level":   ev.RiskLevel,
			"matched_text": ev.MatchedText,
			"match_type":   ev.MatchType,
		})
	}
	return out
}
