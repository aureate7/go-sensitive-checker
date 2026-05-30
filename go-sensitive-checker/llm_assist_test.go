package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLLMAssistAnalyze(t *testing.T) {
	var captured llmAssistChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", got)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request body failed: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		payload := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "```json\n{\"risk_level\":\"low\",\"should_review\":true,\"reason\":\"疑似规避表达\",\"suspected_terms\":[\"sh@bi\",\"sh@bi\"],\"confidence\":0.87}\n```",
					},
				},
			},
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload failed: %v", err)
		}
		_, _ = w.Write(raw)
	}))
	defer server.Close()

	client := newLLMAssistClient(DetectorConfig{
		LLMAPIBaseURL:   server.URL,
		LLMAPIKey:       "test-key",
		LLMModel:        "unit-test-model",
		LLMTimeoutMS:    3000,
		LLMMaxTextRunes: 12,
	})

	resp, err := client.Analyze(context.Background(), "这是一段很长很长很长的测试文本", []string{AbusiveHigh}, DetectResponse{})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if captured.Model != "unit-test-model" {
		t.Fatalf("unexpected model: %s", captured.Model)
	}
	if len(captured.Messages) != 2 {
		t.Fatalf("unexpected messages len: %d", len(captured.Messages))
	}
	if !strings.Contains(captured.Messages[1].Content, "selected_categories") {
		t.Fatalf("request payload missing selected_categories: %s", captured.Messages[1].Content)
	}

	if !resp.Used {
		t.Fatalf("expected Used=true")
	}
	if resp.RiskLevel != "low" {
		t.Fatalf("unexpected risk level: %s", resp.RiskLevel)
	}
	if !resp.ShouldReview {
		t.Fatalf("expected ShouldReview=true")
	}
	if resp.Reason != "疑似规避表达" {
		t.Fatalf("unexpected reason: %s", resp.Reason)
	}
	if len(resp.SuspectedTerms) != 1 || resp.SuspectedTerms[0] != "sh@bi" {
		t.Fatalf("unexpected suspected terms: %#v", resp.SuspectedTerms)
	}
	if resp.Confidence != 0.87 {
		t.Fatalf("unexpected confidence: %v", resp.Confidence)
	}
}

func TestLLMAssistAnalyzeMissingKey(t *testing.T) {
	client := newLLMAssistClient(DetectorConfig{
		LLMAPIBaseURL: "https://example.com/v1",
		LLMAPIKey:     "",
		LLMModel:      "unit-test-model",
		LLMTimeoutMS:  1000,
	})

	_, err := client.Analyze(context.Background(), "test", nil, DetectResponse{})
	if err == nil {
		t.Fatal("expected error when api key is missing")
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Fatalf("unexpected error: %v", err)
	}
}
