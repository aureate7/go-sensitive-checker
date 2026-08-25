package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func performJSONRequest(router http.Handler, method, path string, payload any, headers map[string]string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(payload)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestReadinessFailsForEmptyWordList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	detector := NewDetector(t.TempDir())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	newRouter(newDetectorService(detector), serverConfig{}).ServeHTTP(recorder, request)
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
	newRouter(newDetectorService(detector), serverConfig{MaxTextRunes: 2, MaxBodyBytes: 1024, MaxConcurrent: 1}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	newRouter(newDetectorService(NewDetector(t.TempDir())), serverConfig{}).ServeHTTP(recorder, request)
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("unexpected security header: %q", got)
	}
}

func TestDetectRejectsUnknownCategoryWithStableError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newRouter(newDetectorService(NewDetector(setupTestWordRepo(t))), serverConfig{})
	recorder := performJSONRequest(router, http.MethodPost, "/api/detect", detectReq{Text: "测试", Categories: []string{"unknown"}}, nil)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "INVALID_CATEGORY" || payload.Error.RequestID == "" {
		t.Fatalf("unexpected error payload: %+v", payload.Error)
	}
}

func TestWordListReloadKeepsCurrentDetectorOnFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := setupTestWordRepo(t)
	service := newDetectorService(NewDetector(base))
	before := service.detector()
	before.basePath = t.TempDir()
	router := newRouter(service, serverConfig{ReloadToken: "test-secret"})
	recorder := performJSONRequest(router, http.MethodPost, "/api/admin/wordlist/reload", nil, map[string]string{"Authorization": "Bearer test-secret"})
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected failed reload status, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if service.detector() != before {
		t.Fatal("failed reload replaced the active detector")
	}
}

func TestStatusReportsCapabilitiesAndLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newRouter(newDetectorService(NewDetector(setupTestWordRepo(t))), serverConfig{MaxTextRunes: 123, ReloadToken: "enabled"})
	recorder := performJSONRequest(router, http.MethodGet, "/api/status", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"max_text_runes":123`)) || !bytes.Contains(recorder.Body.Bytes(), []byte(`"hot_reload":true`)) {
		t.Fatalf("unexpected status: %s", recorder.Body.String())
	}
}
