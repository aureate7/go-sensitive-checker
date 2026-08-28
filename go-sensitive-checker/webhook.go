package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// webhookNotifier 在高风险命中或复核积压时向外部渠道推送通知。
// 通用 JSON 格式：{"event": "...", "title": "...", "text": "...", "occurred_at": ...}
// 兼容企业微信/钉钉/飞书的自定义机器人只需在网关侧适配 text 字段。
type webhookNotifier struct {
	mu        sync.Mutex
	url       string
	minRisk   string // 触发通知的最低风险级别（block/high）
	maxPerDay int
	sent      map[string]int // day -> count
	client    *http.Client
}

func newWebhookNotifier() *webhookNotifier {
	url := envStr("SENSITIVE_WEBHOOK_URL", "")
	if url == "" {
		return nil
	}
	maxPerDay := envInt("SENSITIVE_WEBHOOK_MAX_PER_DAY", 50)
	if maxPerDay <= 0 {
		maxPerDay = 50
	}
	minRisk := envStr("SENSITIVE_WEBHOOK_MIN_RISK", "high")
	return &webhookNotifier{
		url:       url,
		minRisk:   minRisk,
		maxPerDay: maxPerDay,
		sent:      make(map[string]int),
		client:    &http.Client{Timeout: 5 * time.Second},
	}
}

var webhookRiskOrder = map[string]int{"safe": 0, "low": 1, "medium": 2, "high": 3, "block": 4}

func (n *webhookNotifier) allowed(risk string) bool {
	want, ok := webhookRiskOrder[n.minRisk]
	if !ok {
		want = webhookRiskOrder["high"]
	}
	return webhookRiskOrder[risk] >= want
}

// Notify 风险达到阈值时异步推送；超过每日配额后静默丢弃并记日志一次。
func (n *webhookNotifier) Notify(event, title, text, risk string) {
	if n == nil || !n.allowed(risk) {
		return
	}
	n.mu.Lock()
	day := time.Now().UTC().Format("2006-01-02")
	count := n.sent[day] + 1
	if count > n.maxPerDay {
		n.mu.Unlock()
		return
	}
	n.sent[day] = count
	// 清理过期计数，防止 map 增长
	for d := range n.sent {
		if d != day {
			delete(n.sent, d)
		}
	}
	n.mu.Unlock()

	payload, err := json.Marshal(map[string]any{
		"event":       event,
		"title":       title,
		"text":        text,
		"risk":        risk,
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return
	}
	go func() {
		resp, err := n.client.Post(n.url, "application/json", bytes.NewReader(payload))
		if err != nil {
			log.Printf("webhook notify %s failed: %v", event, err)
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 300 {
			log.Printf("webhook notify %s got status %d", event, resp.StatusCode)
		}
	}()
}

// describeRiskSummary 生成不含原文的通知摘要（隐私：只含统计与类别分布）。
func describeRiskSummary(resp *DetectResponse) string {
	var b strings.Builder
	b.WriteString("风险级别: " + resp.RiskLevel)
	if resp.TextWordCount > 0 {
		b.WriteString("; 文本词数: " + strconv.Itoa(resp.TextWordCount))
	}
	if len(resp.RiskOccurrence) > 0 {
		b.WriteString("; 命中分布:")
		for _, level := range []string{"high", "medium", "low"} {
			if n := resp.RiskOccurrence[level]; n > 0 {
				b.WriteString(" " + level + "=" + strconv.Itoa(n))
			}
		}
	}
	if len(resp.CategorySummary) > 0 {
		cats := make([]string, 0, len(resp.CategorySummary))
		for cat := range resp.CategorySummary {
			cats = append(cats, cat)
		}
		sort.Strings(cats)
		b.WriteString("; 类别: " + strings.Join(cats, ","))
	}
	return b.String()
}
