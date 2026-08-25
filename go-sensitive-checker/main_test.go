package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestReadinessFailsForEmptyWordList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	detector := NewDetector(t.TempDir())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	newRouter(detector, serverConfig{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for empty word list, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestDetectRejectsTextOverLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	detector := NewDetector(setupTestWordRepo(t))
	body, err := json.Marshal(detectReq{Text: "超过限制"})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/detect", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	newRouter(detector, serverConfig{MaxTextRunes: 2, MaxBodyBytes: 1024, MaxConcurrent: 1}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	newRouter(NewDetector(t.TempDir()), serverConfig{}).ServeHTTP(recorder, request)
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("unexpected security header: %q", got)
	}
}
