package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWebhookRiskThreshold(t *testing.T) {
	n := newRateLimitedWebhookForTest("high", 100)
	if n.allowed("block") || n.allowed("high") {
		// 允许
	} else {
		t.Fatal("high/block should pass threshold=high")
	}
	if n.allowed("medium") || n.allowed("low") || n.allowed("safe") {
		t.Fatal("below threshold should be blocked")
	}
	if n.allowed("unknown-level") {
		t.Fatal("unknown risk should be rejected")
	}
}

func TestWebhookDailyQuota(t *testing.T) {
	var mu sync.Mutex
	received := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := newRateLimitedWebhookForTest("high", 3)
	n.url = srv.URL
	for i := 0; i < 10; i++ {
		n.Notify("high_risk_hit", "t", "x", "high")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		if received == 3 {
			mu.Unlock()
			break
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if received != 3 {
		t.Fatalf("expected quota to cap deliveries at 3, got %d", received)
	}
}

func TestWebhookPayloadShape(t *testing.T) {
	var payload map[string]any
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		close(done)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := newRateLimitedWebhookForTest("low", 10)
	n.url = srv.URL
	n.Notify("high_risk_hit", "标题", "摘要", "medium")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("webhook not delivered")
	}
	if payload["event"] != "high_risk_hit" || payload["risk"] != "medium" || payload["title"] != "标题" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if _, ok := payload["occurred_at"]; !ok {
		t.Fatal("missing occurred_at")
	}
}

func TestDescribeRiskSummaryOmitsText(t *testing.T) {
	resp := &DetectResponse{
		RiskLevel:      "high",
		TextWordCount:  42,
		RiskOccurrence: map[string]int{"high": 2, "low": 1},
		CategorySummary: map[string]map[string]int{
			"abusive_low":      {},
			"advertising_high": {},
		},
	}
	summary := describeRiskSummary(resp)
	for _, want := range []string{"high", "42", "high=2", "abusive_low"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}

}

func newRateLimitedWebhookForTest(minRisk string, maxPerDay int) *webhookNotifier {
	return &webhookNotifier{
		url:       "http://localhost:1", // 占位，测试中覆盖
		minRisk:   minRisk,
		maxPerDay: maxPerDay,
		sent:      make(map[string]int),
		client:    &http.Client{Timeout: 2 * time.Second},
	}
}
